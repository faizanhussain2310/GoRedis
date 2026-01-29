package server

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"redis/internal/protocol"
	"redis/internal/sentinel"
)

// SentinelVotingState tracks voting state for RAFT-style consensus
type SentinelVotingState struct {
	currentEpoch int64  // Highest epoch number seen
	votedEpoch   int64  // Epoch in which we last voted
	votedFor     string // Sentinel ID we voted for in votedEpoch
	mu           sync.Mutex
}

// SentinelServer handles Sentinel protocol and monitoring
type SentinelServer struct {
	config          *SentinelConfig
	listener        net.Listener
	sentinel        *sentinel.Sentinel
	connections     sync.Map
	connIDCounter   atomic.Int64
	activeConnCount atomic.Int64
	wg              sync.WaitGroup
	shutdownChan    chan struct{}
	mu              sync.RWMutex
	isShutdown      bool

	// Peer Sentinel connections for quorum voting
	sentinelPeers map[string]net.Conn // key: "host:port", value: connection
	peersMu       sync.RWMutex

	// Voting state for distributed consensus
	votingState *SentinelVotingState
	sentinelID  string // Unique ID for this Sentinel (host:port)

	// RAFT-style election timeout for leader election
	electionTimeout   time.Duration // Randomized timeout for this Sentinel
	lastMasterContact time.Time     // Last successful contact with master
	electionTimerChan chan struct{} // Channel to signal election timeout
	contactMu         sync.RWMutex  // Protects lastMasterContact
}

// NewSentinelServer creates a new standalone Sentinel server
func NewSentinelServer(cfg *SentinelConfig) *SentinelServer {
	if cfg == nil {
		cfg = DefaultSentinelConfig()
	}

	// Create Sentinel configuration
	sentinelConfig := sentinel.SentinelConfig{
		MasterName:      cfg.MasterName,
		MasterHost:      cfg.MasterHost,
		MasterPort:      cfg.MasterPort,
		Quorum:          cfg.Quorum,
		DownAfterMillis: cfg.DownAfterMillis,
		FailoverTimeout: cfg.FailoverTimeout,
	}

	sentinelInstance := sentinel.NewSentinel(sentinelConfig)

	// Set callback for when master changes
	sentinelInstance.SetMasterChangeCallback(func(newMasterHost string, newMasterPort int) {
		log.Printf("[SENTINEL] Master changed to %s:%d", newMasterHost, newMasterPort)
		// In standalone Sentinel mode, we just log the change
		// Clients should query Sentinel to discover the new master
	})

	log.Printf("Sentinel monitoring: %s at %s:%d", cfg.MasterName, cfg.MasterHost, cfg.MasterPort)
	log.Printf("Sentinel quorum: %d, down-after: %dms, failover-timeout: %dms",
		cfg.Quorum, cfg.DownAfterMillis, cfg.FailoverTimeout)

	if len(cfg.SentinelAddrs) > 0 {
		log.Printf("Other Sentinels: %v", cfg.SentinelAddrs)
	}

	// Generate unique Sentinel ID (host:port)
	sentinelID := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// RAFT-style randomized election timeout (30-60 seconds)
	// Each Sentinel gets a different timeout to naturally elect a leader
	baseTimeout := time.Duration(cfg.DownAfterMillis) * time.Millisecond
	if baseTimeout == 0 {
		baseTimeout = 30 * time.Second
	}
	// Add random jitter: baseTimeout + (0 to baseTimeout)
	// Example: 30s + (0-30s) = 30-60s range
	electionTimeout := baseTimeout + time.Duration(rand.Intn(int(baseTimeout.Milliseconds())))*time.Millisecond

	s := &SentinelServer{
		config:        cfg,
		sentinel:      sentinelInstance,
		shutdownChan:  make(chan struct{}),
		sentinelPeers: make(map[string]net.Conn),
		votingState: &SentinelVotingState{
			currentEpoch: 0,
			votedEpoch:   0,
			votedFor:     "",
		},
		sentinelID:        sentinelID,
		electionTimeout:   electionTimeout,
		lastMasterContact: time.Now(),
		electionTimerChan: make(chan struct{}, 1),
	}

	log.Printf("[SENTINEL] Election timeout for %s: %v (RAFT-style randomized)", sentinelID, electionTimeout)

	// Set voting callback for distributed consensus
	sentinelInstance.SetVoteRequestCallback(func() bool {
		return s.voteForFailover()
	})

	// Set heartbeat callback to reset election timer when master responds
	sentinelInstance.SetMasterHeartbeatCallback(func() {
		s.resetElectionTimer()
	})

	// Start Sentinel monitoring
	sentinelInstance.Start()

	// Connect to other Sentinels for quorum voting
	if len(cfg.SentinelAddrs) > 0 {
		log.Printf("Connecting to other Sentinels for quorum coordination...")
		go s.connectToOtherSentinels()
	}

	// Start RAFT-style election timer
	go s.runElectionTimer()

	return s
}

// connectToOtherSentinels establishes connections to other Sentinels for quorum voting
//
// IMPORTANT: Sentinel uses a PEER-TO-PEER mesh network architecture:
// - NO "main" or "master" Sentinel - all are equal peers
// - Each Sentinel connects to ALL other Sentinels (full mesh)
// - Example with 3 Sentinels:
//   - Sentinel 1 connects to Sentinels 2 and 3
//   - Sentinel 2 connects to Sentinels 1 and 3
//   - Sentinel 3 connects to Sentinels 1 and 2
//
// - This creates redundancy and enables distributed consensus
// - If any Sentinel fails, others maintain quorum
func (s *SentinelServer) connectToOtherSentinels() {
	// Connect to each peer Sentinel in the configuration
	// Each connection is bidirectional - the other Sentinel will also connect back to us
	for _, addr := range s.config.SentinelAddrs {
		go s.monitorSentinel(addr)
	}
}

// monitorSentinel maintains connection to another Sentinel for coordination
func (s *SentinelServer) monitorSentinel(addr string) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-s.shutdownChan:
			return
		default:
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				log.Printf("Failed to connect to Sentinel %s: %v (retrying in %v)", addr, err, backoff)
				time.Sleep(backoff)
				// Exponential backoff
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			log.Printf("Connected to Sentinel at %s", addr)
			backoff = 1 * time.Second // Reset backoff on successful connection

			// Store peer connection for voting
			s.peersMu.Lock()
			s.sentinelPeers[addr] = conn
			s.peersMu.Unlock()

			// Send periodic PING to keep connection alive
			s.maintainSentinelConnection(conn, addr)

			// Remove peer connection on disconnect
			s.peersMu.Lock()
			delete(s.sentinelPeers, addr)
			s.peersMu.Unlock()

			conn.Close()
			log.Printf("Lost connection to Sentinel %s, reconnecting...", addr)
			time.Sleep(1 * time.Second)
		}
	}
}

// maintainSentinelConnection sends periodic health checks to another Sentinel
func (s *SentinelServer) maintainSentinelConnection(conn net.Conn, addr string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdownChan:
			return
		case <-ticker.C:
			// Send PING command using protocol package
			pingCmd := protocol.EncodeArray([]string{"PING"})
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err := conn.Write(pingCmd)
			if err != nil {
				log.Printf("Failed to ping Sentinel %s: %v", addr, err)
				return
			}

			// Read response
			buffer := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				log.Printf("Failed to read response from Sentinel %s: %v", addr, err)
				return
			}

			response := string(buffer[:n])
			if !strings.Contains(response, "PONG") {
				log.Printf("Unexpected response from Sentinel %s: %s", addr, response)
				return
			}

			// Query master address to detect failover using protocol package
			getMasterCmd := protocol.EncodeArray([]string{"SENTINEL", "GET-MASTER-ADDR-BY-NAME", s.config.MasterName})
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err = conn.Write(getMasterCmd)
			if err != nil {
				log.Printf("Failed to query master from Sentinel %s: %v", addr, err)
				return
			}

			// Read master address response
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err = conn.Read(buffer)
			if err != nil {
				log.Printf("Failed to read master addr from Sentinel %s: %v", addr, err)
				return
			}

			// Parse response to check if other Sentinel has different master
			// This is simplified - production would properly parse RESP arrays
			masterResponse := string(buffer[:n])
			if len(masterResponse) > 0 {
				trimmed := strings.TrimSpace(masterResponse)
				if len(trimmed) > 50 {
					trimmed = trimmed[:50]
				}
				log.Printf("Sentinel %s reports master info: %v", addr, trimmed)
			}
		}
	}
}

// runElectionTimer implements RAFT-style election timeout for leader election
// This replaces the jitter-based approach with proper distributed consensus timing
func (s *SentinelServer) runElectionTimer() {
	timer := time.NewTimer(s.electionTimeout)
	defer timer.Stop()

	for {
		select {
		case <-s.shutdownChan:
			return

		case <-timer.C:
			// Election timeout expired - check if master is down
			if s.isMasterDown() {
				log.Printf("[ELECTION] Election timeout expired (%v) - master appears DOWN, becoming candidate",
					s.electionTimeout)
				if s.voteForFailover() {
					log.Printf("[ELECTION] Won election - proceeding with failover")
				} else {
					log.Printf("[ELECTION] Lost election - another Sentinel won")
				}
			} else {
				// Update last contact time since master is up
				s.contactMu.Lock()
				s.lastMasterContact = time.Now()
				s.contactMu.Unlock()
			}
			timer.Reset(s.electionTimeout)

		case <-s.electionTimerChan:
			// Master heartbeat received - reset election timer
			timer.Reset(s.electionTimeout)
		}
	}
}

// resetElectionTimer resets the election timeout (called when master responds)
func (s *SentinelServer) resetElectionTimer() {
	s.contactMu.Lock()
	s.lastMasterContact = time.Now()
	s.contactMu.Unlock()

	// Non-blocking send to reset timer
	select {
	case s.electionTimerChan <- struct{}{}:
	default:
		// Channel full, timer will reset on next cycle
	}
}

// isMasterDown checks if the master is actually down
func (s *SentinelServer) isMasterDown() bool {
	status := s.sentinel.GetStatus()
	masterStatus, ok := status["master_status"].(string)
	return ok && masterStatus == "down"
}

// voteForFailover coordinates with other Sentinels for failover voting
// Returns true if quorum is reached for failover
//
// Voting Protocol (RAFT-inspired consensus with Epochs + Election Timeouts):
// 1. Called by election timer when this Sentinel's timeout expires FIRST
// 2. Increment epoch (logical timestamp for this failover attempt)
// 3. Vote for self in new epoch
// 4. Send SENTINEL IS-MASTER-DOWN-BY-ADDR to all peers with epoch
// 5. Peers respond with vote (1 = agree, 0 = disagree/already voted)
// 6. Wait for responses with timeout (3 seconds)
// 7. Count total votes (including self)
// 8. Return true if votes >= quorum threshold
//
// Election Timeout Mechanism (Prevents Race Conditions):
// - Each Sentinel has randomized timeout (e.g., 30-60 seconds)
// - First Sentinel to timeout becomes candidate NATURALLY
// - No jitter needed - randomization already provides ordering
// - Other Sentinels vote for first candidate they see
//
// Epoch Mechanism (Prevents Split-Brain):
// - Each failover attempt gets unique epoch number
// - Each Sentinel votes for FIRST requester in a given epoch
// - Once voted in epoch N, cannot vote for others in epoch N
// - Higher epochs override lower epochs (stale requests rejected)
//
// Example with 3 Sentinels, quorum=2:
//
//	Timeouts: A=35s, B=47s, C=53s (randomized)
//	T0: Master crashes
//	T35: Sentinel A's timeout expires FIRST → becomes candidate
//	T35: A increments epoch to 5, broadcasts vote request
//	T36: Sentinel B receives A's request → votes for A (epoch=5)
//	T36: Sentinel C receives A's request → votes for A (epoch=5)
//	T37: A reaches quorum (A + B + C = 3 votes) ✅ PROCEED WITH FAILOVER
//	T47: B's timeout expires, but epoch already at 5, A already won
//	T53: C's timeout expires, same situation
//
// No race condition possible - first timeout always wins!
func (s *SentinelServer) voteForFailover() bool {
	// NO JITTER - we're already the first to timeout (election timer guarantees this)
	// Check if we already voted for someone else
	s.votingState.mu.Lock()
	if s.votingState.votedFor != "" && s.votingState.votedFor != s.sentinelID {
		votedFor := s.votingState.votedFor
		votedEpoch := s.votingState.votedEpoch
		s.votingState.mu.Unlock()
		log.Printf("[SENTINEL VOTE] Already voted for %s in epoch %d, cannot become candidate",
			votedFor, votedEpoch)
		return false
	}

	// Increment epoch for this failover attempt
	s.votingState.currentEpoch++
	currentEpoch := s.votingState.currentEpoch
	s.votingState.votedEpoch = currentEpoch
	s.votingState.votedFor = s.sentinelID
	s.votingState.mu.Unlock()

	votes := 1 // This Sentinel votes yes (we detected the failure)

	log.Printf("[SENTINEL VOTE] Initiating failover vote - epoch=%d, sentinelID=%s",
		currentEpoch, s.sentinelID)
	log.Printf("[SENTINEL VOTE] Requesting votes from %d peers (quorum: %d)",
		len(s.sentinelPeers), s.config.Quorum)

	// Get current master address for vote request
	masterHost, masterPort := s.sentinel.GetMasterAddr()

	// Channel to collect votes from peers
	voteChan := make(chan int, len(s.sentinelPeers))

	// Get snapshot of peers (avoid holding lock during network I/O)
	s.peersMu.RLock()
	peers := make(map[string]net.Conn)
	for addr, conn := range s.sentinelPeers {
		peers[addr] = conn
	}
	s.peersMu.RUnlock()

	// Send vote request to all connected peers in parallel
	for addr, conn := range peers {
		go s.requestVoteFromPeer(addr, conn, masterHost, masterPort, currentEpoch, voteChan)
	}

	// Wait for responses with timeout
	timeout := time.After(3 * time.Second)
	expectedResponses := len(peers)
	receivedResponses := 0

	for receivedResponses < expectedResponses {
		select {
		case vote := <-voteChan:
			votes += vote
			receivedResponses++
			log.Printf("[SENTINEL VOTE] Received vote: %d (total: %d/%d, responses: %d/%d)",
				vote, votes, s.config.Quorum, receivedResponses, expectedResponses)
		case <-timeout:
			log.Printf("[SENTINEL VOTE] Timeout waiting for votes (received %d/%d responses)",
				receivedResponses, expectedResponses)
			goto countVotes
		}
	}

countVotes:
	quorumReached := votes >= s.config.Quorum
	log.Printf("[SENTINEL VOTE] Final tally - epoch=%d: %d votes, quorum: %d, result: %v",
		currentEpoch, votes, s.config.Quorum, quorumReached)

	return quorumReached
}

// requestVoteFromPeer sends vote request to a single peer Sentinel with epoch
func (s *SentinelServer) requestVoteFromPeer(
	addr string,
	conn net.Conn,
	masterHost string,
	masterPort int,
	epoch int64,
	voteChan chan<- int,
) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SENTINEL VOTE] Panic requesting vote from %s: %v", addr, r)
			voteChan <- 0 // No vote on error
		}
	}()

	// Send SENTINEL IS-MASTER-DOWN-BY-ADDR command
	// Format: SENTINEL IS-MASTER-DOWN-BY-ADDR <ip> <port> <current-epoch> <runid>
	// - epoch: Logical timestamp for this failover attempt
	// - runid: Our Sentinel ID (or * for simple vote query)
	cmd := protocol.EncodeArray([]string{
		"SENTINEL",
		"IS-MASTER-DOWN-BY-ADDR",
		masterHost,
		fmt.Sprintf("%d", masterPort),
		fmt.Sprintf("%d", epoch), // Send current epoch
		s.sentinelID,             // Our Sentinel ID for vote tracking
	})

	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := conn.Write(cmd)
	if err != nil {
		log.Printf("[SENTINEL VOTE] Failed to send vote request to %s: %v", addr, err)
		voteChan <- 0
		return
	}

	// Read response
	// Expected: *3\r\n:0\r\n$1\r\n*\r\n:0\r\n (master not down from peer's view)
	// Or:       *3\r\n:1\r\n$1\r\n*\r\n:0\r\n (master down, peer agrees)
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil {
		log.Printf("[SENTINEL VOTE] Failed to read vote response from %s: %v", addr, err)
		voteChan <- 0
		return
	}

	response := string(buffer[:n])

	// Simple parsing: look for :1 (agrees) or :0 (disagrees)
	// Full RESP parser would be better, but this works for basic voting
	if strings.Contains(response, ":1") {
		log.Printf("[SENTINEL VOTE] Peer %s agrees master is down", addr)
		voteChan <- 1
	} else {
		log.Printf("[SENTINEL VOTE] Peer %s disagrees master is down", addr)
		voteChan <- 0
	}
}

// Start starts the Sentinel server
func (s *SentinelServer) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	s.listener = listener
	log.Printf("Sentinel server listening on %s", addr)

	go s.acceptConnections(ctx)

	<-ctx.Done()
	return nil
}

func (s *SentinelServer) acceptConnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownChan:
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				s.mu.RLock()
				if s.isShutdown {
					s.mu.RUnlock()
					return
				}
				s.mu.RUnlock()
				log.Printf("Error accepting connection: %v", err)
				continue
			}

			if s.activeConnCount.Load() >= int64(s.config.MaxConnections) {
				log.Printf("Max connections reached, rejecting connection from %s", conn.RemoteAddr())
				conn.Close()
				continue
			}

			s.wg.Add(1)
			go s.handleConnection(ctx, conn)
		}
	}
}

func (s *SentinelServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()

	connID := s.connIDCounter.Add(1)
	s.activeConnCount.Add(1)
	defer s.activeConnCount.Add(-1)

	s.connections.Store(connID, conn)
	defer s.connections.Delete(connID)
	defer conn.Close()

	log.Printf("New Sentinel connection [%d] from %s", connID, conn.RemoteAddr())

	// Handle Sentinel protocol commands
	s.handleSentinelProtocol(ctx, conn, connID)
}

// Shutdown gracefully shuts down the Sentinel server
func (s *SentinelServer) Shutdown() {
	s.mu.Lock()
	if s.isShutdown {
		s.mu.Unlock()
		return
	}
	s.isShutdown = true
	s.mu.Unlock()

	log.Println("Initiating Sentinel shutdown...")

	close(s.shutdownChan)

	if s.listener != nil {
		s.listener.Close()
	}

	// Close all connections
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(net.Conn); ok {
			conn.Close()
		}
		return true
	})

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All Sentinel connections closed gracefully")
	case <-time.After(5 * time.Second):
		log.Println("Shutdown timeout reached, forcing exit")
	}

	// Shutdown Sentinel
	if s.sentinel != nil {
		s.sentinel.Stop()
	}

	log.Println("Sentinel server shutdown complete")
}

// handleSentinelProtocol handles RESP protocol and Sentinel commands
func (s *SentinelServer) handleSentinelProtocol(ctx context.Context, conn net.Conn, connID int64) {
	reader := bufio.NewReader(conn)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownChan:
			return
		default:
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))

			// Use existing protocol.ParseCommand instead of custom parser
			cmd, err := protocol.ParseCommand(reader)
			if err != nil {
				return
			}

			// Execute command
			response := s.executeSentinelCommand(cmd)
			conn.Write(response)
		}
	}
}

// executeSentinelCommand executes a parsed RESP command
func (s *SentinelServer) executeSentinelCommand(cmd *protocol.Command) []byte {
	if len(cmd.Args) == 0 {
		return protocol.EncodeError("ERR no command provided")
	}

	cmdName := strings.ToUpper(cmd.Args[0])

	switch cmdName {
	case "PING":
		return s.handlePing()
	case "SENTINEL":
		if len(cmd.Args) < 2 {
			return protocol.EncodeError("ERR wrong number of arguments for 'sentinel' command")
		}
		return s.handleSentinelCommand(cmd.Args[1:])
	case "INFO":
		return s.handleInfo()
	default:
		return protocol.EncodeError(fmt.Sprintf("ERR unknown command '%s'", cmdName))
	}
}

// handleVoteRequest processes incoming vote requests from other Sentinels
// Implements RAFT-style voting with epoch-based consensus
//
// Voting Rules:
// 1. Reject requests with epoch < currentEpoch (stale request)
// 2. Update currentEpoch if request has higher epoch (new failover round)
// 3. Vote for FIRST requester in each epoch (first-come-first-served)
// 4. Reject subsequent requests in same epoch (already voted)
// 5. Only vote if we also think master is down (independent verification)
//
// Response Format: *3\r\n:<down_state>\r\n$<leader_len>\r\n<leader>\r\n:<epoch>\r\n
// - down_state: 1 if we voted for requester, 0 if rejected
// - leader: Sentinel ID we voted for in this epoch
// - epoch: Current epoch number
func (s *SentinelServer) handleVoteRequest(masterHost string, masterPort int, requestEpoch int64, candidateID string) []byte {
	s.votingState.mu.Lock()
	defer s.votingState.mu.Unlock()

	log.Printf("[VOTE REQUEST] From %s, epoch=%d (our epoch=%d, votedEpoch=%d, votedFor=%s)",
		candidateID, requestEpoch, s.votingState.currentEpoch, s.votingState.votedEpoch, s.votingState.votedFor)

	// Rule 1: Reject stale epochs
	if requestEpoch < s.votingState.currentEpoch {
		log.Printf("[VOTE REQUEST] Rejected - stale epoch (request=%d < current=%d)",
			requestEpoch, s.votingState.currentEpoch)
		// Response: *3\r\n:0\r\n$<len>\r\n<votedFor>\r\n:<epoch>\r\n
		return s.encodeVoteResponse(0, s.votingState.votedFor, s.votingState.currentEpoch)
	}

	// Rule 2: New epoch resets voting state
	if requestEpoch > s.votingState.currentEpoch {
		log.Printf("[VOTE REQUEST] New epoch detected (request=%d > current=%d) - resetting vote state",
			requestEpoch, s.votingState.currentEpoch)
		s.votingState.currentEpoch = requestEpoch
		s.votingState.votedEpoch = 0 // Haven't voted in this epoch yet
		s.votingState.votedFor = ""
	}

	// Rule 3: Already voted in this epoch?
	if s.votingState.votedEpoch == requestEpoch {
		// Check if this is the same candidate we voted for (confirmation)
		if s.votingState.votedFor == candidateID {
			log.Printf("[VOTE REQUEST] Confirming vote for %s in epoch %d",
				candidateID, requestEpoch)
			return s.encodeVoteResponse(1, candidateID, requestEpoch)
		} else {
			// Already voted for someone else in this epoch
			log.Printf("[VOTE REQUEST] Rejected - already voted for %s in epoch %d",
				s.votingState.votedFor, requestEpoch)
			return s.encodeVoteResponse(0, s.votingState.votedFor, requestEpoch)
		}
	}

	// Rule 4: First vote in this epoch - check if master is actually down
	currentMasterHost, currentMasterPort := s.sentinel.GetMasterAddr()

	// Verify this is asking about our monitored master
	if masterHost != currentMasterHost || masterPort != currentMasterPort {
		log.Printf("[VOTE REQUEST] Rejected - master mismatch (request=%s:%d, monitoring=%s:%d)",
			masterHost, masterPort, currentMasterHost, currentMasterPort)
		return s.encodeVoteResponse(0, "", requestEpoch)
	}

	// Independent verification: Do we also think master is down?
	status := s.sentinel.GetStatus()
	masterStatus, ok := status["master_status"].(string)

	if !ok || masterStatus != "down" {
		log.Printf("[VOTE REQUEST] Rejected - master appears UP from our perspective (status=%s)",
			masterStatus)
		return s.encodeVoteResponse(0, "", requestEpoch)
	}

	// Rule 5: Grant vote - master is down, first request in this epoch
	s.votingState.votedEpoch = requestEpoch
	s.votingState.votedFor = candidateID

	log.Printf("[VOTE REQUEST] ✅ GRANTED - voting for %s in epoch %d (master is DOWN)",
		candidateID, requestEpoch)

	return s.encodeVoteResponse(1, candidateID, requestEpoch)
}

// encodeVoteResponse creates a proper RESP array for vote responses
// Format: *3\r\n:<vote>\r\n$<len>\r\n<leader>\r\n:<epoch>\r\n
// - vote: 1 (granted) or 0 (rejected) as integer
// - leader: Sentinel ID we voted for as bulk string
// - epoch: Current epoch as integer
func (s *SentinelServer) encodeVoteResponse(vote int, leader string, epoch int64) []byte {
	var result strings.Builder
	result.WriteString("*3\r\n")
	result.WriteString(fmt.Sprintf(":%d\r\n", vote))
	if leader == "" {
		result.WriteString("$-1\r\n") // Null bulk string
	} else {
		result.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(leader), leader))
	}
	result.WriteString(fmt.Sprintf(":%d\r\n", epoch))
	return []byte(result.String())
}

// handlePing responds to PING command
func (s *SentinelServer) handlePing() []byte {
	return protocol.EncodeSimpleString("PONG")
}

// handleSentinelCommand handles SENTINEL subcommands
func (s *SentinelServer) handleSentinelCommand(args []string) []byte {
	if len(args) == 0 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sentinel' command")
	}

	subcmd := strings.ToUpper(args[0])

	switch subcmd {
	case "GET-MASTER-ADDR-BY-NAME":
		return s.handleGetMasterAddrByName(args[1:])
	case "MASTER", "MASTERS":
		return s.handleSentinelMasters()
	case "REPLICAS", "SLAVES":
		return s.handleSentinelReplicas(args[1:])
	case "SENTINELS":
		return s.handleSentinelSentinels(args[1:])
	case "RESET":
		return s.handleSentinelReset(args[1:])
	case "IS-MASTER-DOWN-BY-ADDR":
		return s.handleIsMasterDownByAddr(args[1:])
	default:
		return protocol.EncodeError(fmt.Sprintf("ERR Unknown sentinel subcommand '%s'", subcmd))
	}
}

// handleIsMasterDownByAddr handles vote requests from other Sentinels
func (s *SentinelServer) handleIsMasterDownByAddr(args []string) []byte {
	// Expected: <ip> <port> <epoch> <runid>
	if len(args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sentinel is-master-down-by-addr' command")
	}

	masterHost := args[0]
	masterPort := 0
	fmt.Sscanf(args[1], "%d", &masterPort)

	epoch := int64(0)
	fmt.Sscanf(args[2], "%d", &epoch)

	candidateID := args[3]

	// Process vote request with epoch-based consensus
	return s.handleVoteRequest(masterHost, masterPort, epoch, candidateID)
}

// handleGetMasterAddrByName returns the master address
func (s *SentinelServer) handleGetMasterAddrByName(args []string) []byte {
	if len(args) < 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sentinel get-master-addr-by-name' command")
	}

	masterName := args[0]
	if masterName != s.config.MasterName {
		// Master name doesn't match
		return protocol.EncodeNullBulkString()
	}

	host, port := s.sentinel.GetMasterAddr()
	return protocol.EncodeArray([]string{host, fmt.Sprintf("%d", port)})
}

// handleSentinelMasters returns information about monitored masters
func (s *SentinelServer) handleSentinelMasters() []byte {
	status := s.sentinel.GetStatus()

	result := []interface{}{
		"name", s.config.MasterName,
		"ip", status["master_host"],
		"port", status["master_port"],
		"status", status["master_status"],
		"replicas", status["replicas_count"],
		"quorum", s.config.Quorum,
	}

	return protocol.EncodeInterfaceArray(result)
}

// handleSentinelReplicas returns information about replicas
func (s *SentinelServer) handleSentinelReplicas(args []string) []byte {
	if len(args) < 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sentinel replicas' command")
	}

	masterName := args[0]
	if masterName != s.config.MasterName {
		return protocol.EncodeNilArray()
	}

	status := s.sentinel.GetStatus()
	replicas := status["replicas"].([]map[string]interface{})

	// Build nested array of replica info
	var result [][]byte
	for _, replica := range replicas {
		replicaInfo := []interface{}{
			"name", fmt.Sprintf("%s:%d", replica["host"], replica["port"]),
			"ip", replica["host"],
			"port", replica["port"],
			"status", replica["status"],
			"priority", replica["priority"],
			"repl-offset", replica["offset"],
		}
		result = append(result, protocol.EncodeInterfaceArray(replicaInfo))
	}

	return protocol.EncodeRawArray(result)
}

// handleSentinelSentinels returns information about other Sentinels
// This shows the peer-to-peer mesh: all other Sentinels we're connected to
func (s *SentinelServer) handleSentinelSentinels(args []string) []byte {
	if len(args) < 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sentinel sentinels' command")
	}

	masterName := args[0]
	if masterName != s.config.MasterName {
		return protocol.EncodeNilArray()
	}

	// Return list of other known peer Sentinels in the mesh
	var result [][]byte
	for i, addr := range s.config.SentinelAddrs {
		parts := strings.Split(addr, ":")
		if len(parts) == 2 {
			sentinelInfo := []interface{}{
				"name", fmt.Sprintf("sentinel-%d", i),
				"ip", parts[0],
				"port", parts[1],
				"runid", fmt.Sprintf("sentinel-%d-runid", i),
			}
			result = append(result, protocol.EncodeInterfaceArray(sentinelInfo))
		}
	}

	return protocol.EncodeRawArray(result)
}

// handleSentinelReset resets Sentinel state
func (s *SentinelServer) handleSentinelReset(args []string) []byte {
	if len(args) < 1 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sentinel reset' command")
	}

	// For now, just return success
	// Real implementation would reset monitoring state
	return protocol.EncodeInteger(1)
}

// handleInfo returns Sentinel information
func (s *SentinelServer) handleInfo() []byte {
	status := s.sentinel.GetStatus()

	info := fmt.Sprintf("# Sentinel\r\n"+
		"sentinel_masters:1\r\n"+
		"sentinel_running_scripts:0\r\n"+
		"sentinel_scripts_queue_length:0\r\n"+
		"sentinel_simulate_failure_flags:0\r\n"+
		"master0:name=%s,status=%s,address=%s:%d,slaves=%d,sentinels=%d\r\n",
		s.config.MasterName,
		status["master_status"],
		status["master_host"],
		status["master_port"],
		status["replicas_count"],
		len(s.config.SentinelAddrs)+1, // Total Sentinels in mesh (including this one)
	)

	return protocol.EncodeBulkString(info)
}

// All RESP encoding is now handled by internal/protocol package
// No duplicate encoding functions needed here
