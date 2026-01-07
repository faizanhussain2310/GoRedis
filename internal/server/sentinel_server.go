package server

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"redis/internal/protocol"
	"redis/internal/sentinel"
)

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

	s := &SentinelServer{
		config:       cfg,
		sentinel:     sentinelInstance,
		shutdownChan: make(chan struct{}),
	}

	// Start Sentinel monitoring
	sentinelInstance.Start()

	// Connect to other Sentinels for quorum voting
	if len(cfg.SentinelAddrs) > 0 {
		log.Printf("Connecting to other Sentinels for quorum coordination...")
		go s.connectToOtherSentinels()
	}

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

			// Send periodic PING to keep connection alive
			s.maintainSentinelConnection(conn, addr)

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
				log.Printf("Sentinel %s reports master info: %v", addr, strings.TrimSpace(masterResponse)[:50])
			}
		}
	}
}

// voteForFailover coordinates with other Sentinels for failover voting
// Returns true if quorum is reached for failover
func (s *SentinelServer) voteForFailover() bool {
	votes := 1 // This Sentinel votes yes

	// In a full implementation, we would:
	// 1. Send failover proposal to all connected Sentinels
	// 2. Wait for responses (with timeout)
	// 3. Count votes
	// 4. Return true if votes >= quorum

	// For now, simplified single-Sentinel logic
	return votes >= s.config.Quorum
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
	default:
		return protocol.EncodeError(fmt.Sprintf("ERR Unknown sentinel subcommand '%s'", subcmd))
	}
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
