package sentinel

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"redis/internal/storage"
)

// ==================== SENTINEL DATA STRUCTURES ====================

// Sentinel monitors Redis master and replicas for automatic failover
type Sentinel struct {
	// Configuration
	masterName   string
	masterHost   string
	masterPort   int
	quorum       int // Number of sentinels that need to agree master is down
	downAfter    time.Duration
	failoverTime time.Duration

	// State
	master             *MonitoredInstance
	replicas           map[string]*MonitoredInstance // key: "host:port"
	replicasMu         sync.RWMutex
	failoverInProgress bool
	failoverTriggered  bool // Track if failover already triggered for current master-down event
	failoverMu         sync.Mutex

	// Monitoring
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Callbacks
	onVoteRequest     func() bool // Called before failover to get quorum vote
	onMasterHeartbeat func()      // Called when master responds to PING (for election timer reset)

	// Pub/Sub for event notifications
	pubsub         *storage.PubSub
	onMasterChange func(newMasterHost string, newMasterPort int)
	callbackMu     sync.RWMutex
}

// MonitoredInstance represents a Redis instance being monitored
type MonitoredInstance struct {
	Host            string
	Port            int
	Role            string // "master" or "slave"
	LastPing        time.Time
	LastPingOK      bool
	IsDown          bool
	DownSince       time.Time
	LastDownLogTime time.Time // Last time we logged "Master down for..." message
	Priority        int       // For replica election (higher = better)
	ReplOffset      int64
	mu              sync.RWMutex
}

// SentinelConfig configuration for Sentinel
type SentinelConfig struct {
	MasterName      string
	MasterHost      string
	MasterPort      int
	Quorum          int // Number of sentinels for quorum (for now, 1 = single sentinel)
	DownAfterMillis int // Milliseconds before marking instance as down
	FailoverTimeout int // Milliseconds for failover timeout
}

// ==================== SENTINEL CREATION AND LIFECYCLE ====================

// NewSentinel creates a new Sentinel instance
func NewSentinel(config SentinelConfig) *Sentinel {
	downAfter := time.Duration(config.DownAfterMillis) * time.Millisecond
	if downAfter == 0 {
		downAfter = 30 * time.Second // Default 30 seconds
	}

	failoverTime := time.Duration(config.FailoverTimeout) * time.Millisecond
	if failoverTime == 0 {
		failoverTime = 180 * time.Second // Default 3 minutes
	}

	quorum := config.Quorum
	if quorum == 0 {
		quorum = 1 // Single sentinel mode
	}

	s := &Sentinel{
		masterName:   config.MasterName,
		masterHost:   config.MasterHost,
		masterPort:   config.MasterPort,
		quorum:       quorum,
		downAfter:    downAfter,
		pubsub:       storage.NewPubSub(),
		failoverTime: failoverTime,
		replicas:     make(map[string]*MonitoredInstance),
		stopChan:     make(chan struct{}),
	}

	s.master = &MonitoredInstance{
		Host:       config.MasterHost,
		Port:       config.MasterPort,
		Role:       "master",
		LastPing:   time.Now(),
		LastPingOK: true,
		IsDown:     false,
	}

	log.Printf("[SENTINEL] Initialized - monitoring master %s at %s:%d",
		config.MasterName, config.MasterHost, config.MasterPort)
	log.Printf("[SENTINEL] Down after: %v, Quorum: %d", downAfter, quorum)

	return s
}

// Start begins monitoring
func (s *Sentinel) Start() {
	s.wg.Add(2)
	go s.monitorMaster()
	go s.monitorReplicas()
	log.Printf("[SENTINEL] Started monitoring")
}

// Stop halts monitoring
func (s *Sentinel) Stop() {
	log.Printf("[SENTINEL] Stopping...")
	close(s.stopChan)
	s.wg.Wait()
	log.Printf("[SENTINEL] Stopped")
}

// SetMasterChangeCallback sets callback for when master changes
func (s *Sentinel) SetMasterChangeCallback(callback func(newMasterHost string, newMasterPort int)) {
	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	s.onMasterChange = callback
}

// SetVoteRequestCallback sets callback to request votes before failover
func (s *Sentinel) SetVoteRequestCallback(callback func() bool) {
	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	s.onVoteRequest = callback
}

// SetMasterHeartbeatCallback sets callback for when master responds to PING
func (s *Sentinel) SetMasterHeartbeatCallback(callback func()) {
	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	s.onMasterHeartbeat = callback
}

// GetPubSub returns the Sentinel's pub/sub instance for event subscriptions
func (s *Sentinel) GetPubSub() *storage.PubSub {
	return s.pubsub
}

// ==================== MONITORING ====================

// monitorMaster continuously checks master health
func (s *Sentinel) monitorMaster() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Discover replicas on first run
	s.discoverReplicas()

	discoveryTicker := time.NewTicker(10 * time.Second)
	defer discoveryTicker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkMasterHealth()
		case <-discoveryTicker.C:
			// Periodically rediscover replicas
			s.discoverReplicas()
		case <-s.stopChan:
			return
		}
	}
}

// monitorReplicas continuously checks replica health
func (s *Sentinel) monitorReplicas() {
	defer s.wg.Done()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkReplicasHealth()
		case <-s.stopChan:
			return
		}
	}
}

// checkMasterHealth pings master and detects failure
func (s *Sentinel) checkMasterHealth() {
	s.master.mu.RLock()
	host := s.master.Host
	port := s.master.Port
	s.master.mu.RUnlock()

	// Try to connect and ping
	ok := s.pingInstance(host, port)

	s.master.mu.Lock()
	s.master.LastPing = time.Now()
	s.master.LastPingOK = ok

	if !ok {
		if !s.master.IsDown {
			// Just went down
			s.master.IsDown = true
			s.master.DownSince = time.Now()
			s.master.LastDownLogTime = time.Time{} // Reset log time
			log.Printf("[SENTINEL] Master %s:%d is DOWN", host, port)
			// Reset failover trigger flag when master goes down
			s.failoverMu.Lock()
			s.failoverTriggered = false
			s.failoverMu.Unlock()
		} else {
			// Still down - log periodically (not every second)
			downDuration := time.Since(s.master.DownSince)
			if downDuration >= s.downAfter {
				// Log only if: first time crossing threshold OR 30 seconds since last log
				timeSinceLastLog := time.Since(s.master.LastDownLogTime)
				if s.master.LastDownLogTime.IsZero() || timeSinceLastLog >= 30*time.Second {
					log.Printf("[SENTINEL] Master down for %v (threshold: %v)", downDuration, s.downAfter)
					s.master.LastDownLogTime = time.Now()
				}
			}
		}
	} else {
		if s.master.IsDown {
			// Came back up - reset failover trigger flag
			s.master.IsDown = false
			log.Printf("[SENTINEL] Master %s:%d is UP", host, port)
			s.failoverMu.Lock()
			s.failoverTriggered = false
			s.failoverMu.Unlock()
		}
		// Master is up - notify for election timer reset
		s.callbackMu.RLock()
		heartbeatCallback := s.onMasterHeartbeat
		s.callbackMu.RUnlock()
		if heartbeatCallback != nil {
			heartbeatCallback()
		}
	}

	isDown := s.master.IsDown
	downSince := s.master.DownSince
	s.master.mu.Unlock()

	// Trigger failover ONCE when master crosses down threshold
	// Don't spam every second - let election timer handle it
	if isDown && time.Since(downSince) >= s.downAfter {
		s.failoverMu.Lock()
		alreadyTriggered := s.failoverTriggered
		s.failoverMu.Unlock()

		if !alreadyTriggered {
			s.failoverMu.Lock()
			s.failoverTriggered = true
			s.failoverMu.Unlock()
			s.triggerFailover()
		}
	}
}

// checkReplicasHealth pings all replicas in parallel
func (s *Sentinel) checkReplicasHealth() {
	s.replicasMu.RLock()
	replicas := make([]*MonitoredInstance, 0, len(s.replicas))
	for _, replica := range s.replicas {
		replicas = append(replicas, replica)
	}
	s.replicasMu.RUnlock()

	// Use WaitGroup to wait for all parallel health checks
	var wg sync.WaitGroup

	for _, replica := range replicas {
		wg.Add(1)

		// Launch goroutine for each replica health check
		go func(r *MonitoredInstance) {
			defer wg.Done()

			r.mu.RLock()
			host := r.Host
			port := r.Port
			r.mu.RUnlock()

			ok := s.pingInstance(host, port)

			r.mu.Lock()
			r.LastPing = time.Now()
			r.LastPingOK = ok

			if !ok && !r.IsDown {
				r.IsDown = true
				r.DownSince = time.Now()
				log.Printf("[SENTINEL] Replica %s:%d is DOWN", host, port)
			} else if ok && r.IsDown {
				r.IsDown = false
				log.Printf("[SENTINEL] Replica %s:%d is UP", host, port)
			}
			r.mu.Unlock()
		}(replica)
	}

	// Wait for all health checks to complete
	wg.Wait()
}

// discoverReplicas queries master for connected replicas using INFO replication
func (s *Sentinel) discoverReplicas() {
	s.master.mu.RLock()
	host := s.master.Host
	port := s.master.Port
	isDown := s.master.IsDown
	s.master.mu.RUnlock()

	// Skip discovery if master is down
	if isDown {
		return
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Send INFO replication command
	_, err = conn.Write([]byte("*2\r\n$4\r\nINFO\r\n$11\r\nreplication\r\n"))
	if err != nil {
		return
	}

	// Read response
	buffer := make([]byte, 4096)
	n, err := conn.Read(buffer)
	if err != nil {
		return
	}

	response := string(buffer[:n])

	// Parse INFO replication response to find replicas
	// Format: slave0:ip=127.0.0.1,port=6380,state=online,offset=123,lag=0
	lines := strings.Split(response, "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "slave") {
			// Parse replica info
			parts := strings.Split(line, ":")
			if len(parts) < 2 {
				continue
			}

			info := parts[1]
			replicaHost := ""
			replicaPort := 0
			offset := int64(0)

			// Parse key=value pairs
			pairs := strings.Split(info, ",")
			for _, pair := range pairs {
				kv := strings.Split(pair, "=")
				if len(kv) != 2 {
					continue
				}

				switch kv[0] {
				case "ip":
					replicaHost = kv[1]
				case "port":
					fmt.Sscanf(kv[1], "%d", &replicaPort)
				case "offset":
					fmt.Sscanf(kv[1], "%d", &offset)
				}
			}

			if replicaHost != "" && replicaPort > 0 {
				// Add replica if not already known
				replicaKey := fmt.Sprintf("%s:%d", replicaHost, replicaPort)

				s.replicasMu.Lock()
				if _, exists := s.replicas[replicaKey]; !exists {
					s.replicas[replicaKey] = &MonitoredInstance{
						Host:       replicaHost,
						Port:       replicaPort,
						Role:       "slave",
						Priority:   100, // Default priority
						ReplOffset: offset,
						LastPing:   time.Now(),
						LastPingOK: true,
					}
					log.Printf("[SENTINEL] Discovered replica: %s:%d (offset=%d)", replicaHost, replicaPort, offset)
				} else {
					// Update offset for existing replica
					s.replicas[replicaKey].mu.Lock()
					s.replicas[replicaKey].ReplOffset = offset
					s.replicas[replicaKey].mu.Unlock()
				}
				s.replicasMu.Unlock()
			}
		}
	}
}

// pingInstance attempts to connect and send PING
func (s *Sentinel) pingInstance(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()

	// Set deadline for PING response
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Send PING command
	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		return false
	}

	// Read response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return false
	}

	response := string(buf[:n])
	// Check for +PONG or +OK response
	return len(response) > 0 && (response[0] == '+')
}

// ==================== FAILOVER ====================

// triggerFailover initiates automatic failover
func (s *Sentinel) triggerFailover() {
	s.failoverMu.Lock()
	if s.failoverInProgress {
		s.failoverMu.Unlock()
		return
	}
	s.failoverInProgress = true
	s.failoverMu.Unlock()

	log.Printf("[SENTINEL] ========================================")
	log.Printf("[SENTINEL] INITIATING AUTOMATIC FAILOVER")
	log.Printf("[SENTINEL] ========================================")

	// Run failover in background
	go s.performFailover()
}

// performFailover executes the failover process
func (s *Sentinel) performFailover() {
	defer func() {
		s.failoverMu.Lock()
		s.failoverInProgress = false
		s.failoverMu.Unlock()
	}()

	startTime := time.Now()

	// Step 0: Request votes from other Sentinels for quorum
	s.callbackMu.RLock()
	voteCallback := s.onVoteRequest
	s.callbackMu.RUnlock()

	if voteCallback != nil {
		log.Printf("[SENTINEL] Requesting quorum vote from peer Sentinels...")
		quorumReached := voteCallback()
		if !quorumReached {
			log.Printf("[SENTINEL] FAILOVER ABORTED: Quorum not reached")
			return
		}
		log.Printf("[SENTINEL] ✅ Quorum reached, proceeding with failover")
	} else {
		log.Printf("[SENTINEL] No voting callback set, proceeding without quorum check")
	}

	// Step 1: Select best replica
	bestReplica := s.selectBestReplica()
	if bestReplica == nil {
		log.Printf("[SENTINEL] FAILOVER FAILED: No suitable replica available")
		return
	}

	bestReplica.mu.RLock()
	newMasterHost := bestReplica.Host
	newMasterPort := bestReplica.Port
	bestReplica.mu.RUnlock()

	log.Printf("[SENTINEL] Selected replica %s:%d for promotion", newMasterHost, newMasterPort)

	// Step 2: Promote replica to master
	if !s.promoteReplicaToMaster(newMasterHost, newMasterPort) {
		log.Printf("[SENTINEL] FAILOVER FAILED: Could not promote replica")
		return
	}

	// Step 3: Update master reference
	s.master.mu.Lock()
	oldMasterHost := s.master.Host
	oldMasterPort := s.master.Port
	s.master.Host = newMasterHost
	s.master.Port = newMasterPort
	s.master.IsDown = false
	s.master.LastPingOK = true
	s.master.LastPing = time.Now()
	s.master.mu.Unlock()

	log.Printf("[SENTINEL] Updated master from %s:%d to %s:%d",
		oldMasterHost, oldMasterPort, newMasterHost, newMasterPort)

	// Step 4: Reconfigure other replicas
	s.reconfigureReplicas(newMasterHost, newMasterPort)

	// Step 5: Remove promoted replica from replicas list
	s.replicasMu.Lock()
	delete(s.replicas, fmt.Sprintf("%s:%d", newMasterHost, newMasterPort))
	s.replicasMu.Unlock()

	// Step 6: Add old master as replica (will be synced when it comes back)
	s.replicasMu.Lock()
	s.replicas[fmt.Sprintf("%s:%d", oldMasterHost, oldMasterPort)] = &MonitoredInstance{
		Host:       oldMasterHost,
		Port:       oldMasterPort,
		Role:       "slave",
		LastPing:   time.Now(),
		LastPingOK: false,
		IsDown:     true,
		DownSince:  time.Now(),
		Priority:   0,
	}
	s.replicasMu.Unlock()

	duration := time.Since(startTime)
	log.Printf("[SENTINEL] ========================================")
	log.Printf("[SENTINEL] FAILOVER COMPLETED in %v", duration)
	log.Printf("[SENTINEL] New master: %s:%d", newMasterHost, newMasterPort)
	log.Printf("[SENTINEL] ========================================")

	// Publish failover event to Sentinel pub/sub channel
	// Format: +switch-master <master-name> <old-ip> <old-port> <new-ip> <new-port>
	event := fmt.Sprintf("+switch-master %s %s %d %s %d",
		s.masterName, oldMasterHost, oldMasterPort, newMasterHost, newMasterPort)
	s.pubsub.Publish("__sentinel__:failover", event)

	log.Printf("[SENTINEL] Published event: %s", event)

	// Trigger callback
	log.Printf("[SENTINEL] ========================================")

	// Trigger callback
	s.callbackMu.RLock()
	callback := s.onMasterChange
	s.callbackMu.RUnlock()

	if callback != nil {
		callback(newMasterHost, newMasterPort)
	}
}

// selectBestReplica chooses the best replica for promotion
func (s *Sentinel) selectBestReplica() *MonitoredInstance {
	s.replicasMu.RLock()
	defer s.replicasMu.RUnlock()

	var bestReplica *MonitoredInstance
	var bestScore int64 = -1

	for _, replica := range s.replicas {
		replica.mu.RLock()
		isDown := replica.IsDown
		priority := replica.Priority
		offset := replica.ReplOffset
		replica.mu.RUnlock()

		// Skip down replicas
		if isDown {
			continue
		}

		// Calculate score: priority * 1000000 + offset
		// Higher priority and higher offset = better candidate
		score := int64(priority)*1000000 + offset

		if score > bestScore {
			bestScore = score
			bestReplica = replica
		}
	}

	return bestReplica
}

// promoteReplicaToMaster promotes a replica to master role
func (s *Sentinel) promoteReplicaToMaster(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Printf("[SENTINEL] Failed to connect to replica %s: %v", addr, err)
		return false
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send REPLICAOF NO ONE to make it a master
	cmd := "*3\r\n$9\r\nREPLICAOF\r\n$2\r\nNO\r\n$3\r\nONE\r\n"
	_, err = conn.Write([]byte(cmd))
	if err != nil {
		log.Printf("[SENTINEL] Failed to send REPLICAOF NO ONE: %v", err)
		return false
	}

	// Read response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("[SENTINEL] Failed to read response: %v", err)
		return false
	}

	response := string(buf[:n])
	if response[0] != '+' {
		log.Printf("[SENTINEL] Unexpected response: %s", response)
		return false
	}

	log.Printf("[SENTINEL] Successfully promoted %s:%d to master", host, port)
	return true
}

// reconfigureReplicas updates all replicas to follow new master
func (s *Sentinel) reconfigureReplicas(newMasterHost string, newMasterPort int) {
	s.replicasMu.RLock()
	replicas := make([]*MonitoredInstance, 0, len(s.replicas))
	for _, replica := range s.replicas {
		replicas = append(replicas, replica)
	}
	s.replicasMu.RUnlock()

	for _, replica := range replicas {
		replica.mu.RLock()
		host := replica.Host
		port := replica.Port
		isDown := replica.IsDown
		replica.mu.RUnlock()

		// Skip the promoted replica and down replicas
		if (host == newMasterHost && port == newMasterPort) || isDown {
			continue
		}

		s.reconfigureReplica(host, port, newMasterHost, newMasterPort)
	}
}

// reconfigureReplica tells a replica to follow new master
func (s *Sentinel) reconfigureReplica(replicaHost string, replicaPort int, masterHost string, masterPort int) bool {
	addr := fmt.Sprintf("%s:%d", replicaHost, replicaPort)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Printf("[SENTINEL] Failed to connect to replica %s: %v", addr, err)
		return false
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send REPLICAOF <new_master_host> <new_master_port>
	cmd := fmt.Sprintf("*3\r\n$9\r\nREPLICAOF\r\n$%d\r\n%s\r\n$%d\r\n%d\r\n",
		len(masterHost), masterHost, len(fmt.Sprintf("%d", masterPort)), masterPort)
	_, err = conn.Write([]byte(cmd))
	if err != nil {
		log.Printf("[SENTINEL] Failed to send REPLICAOF: %v", err)
		return false
	}

	// Read response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("[SENTINEL] Failed to read response: %v", err)
		return false
	}

	response := string(buf[:n])
	if response[0] != '+' {
		log.Printf("[SENTINEL] Unexpected response: %s", response)
		return false
	}

	log.Printf("[SENTINEL] Reconfigured replica %s:%d to follow %s:%d",
		replicaHost, replicaPort, masterHost, masterPort)
	return true
}

// ==================== REPLICA MANAGEMENT ====================

// AddReplica registers a replica for monitoring
func (s *Sentinel) AddReplica(host string, port int, priority int, offset int64) {
	s.replicasMu.Lock()
	defer s.replicasMu.Unlock()

	key := fmt.Sprintf("%s:%d", host, port)
	s.replicas[key] = &MonitoredInstance{
		Host:       host,
		Port:       port,
		Role:       "slave",
		LastPing:   time.Now(),
		LastPingOK: true,
		IsDown:     false,
		Priority:   priority,
		ReplOffset: offset,
	}

	log.Printf("[SENTINEL] Added replica %s:%d for monitoring (priority: %d)", host, port, priority)
}

// RemoveReplica removes a replica from monitoring
func (s *Sentinel) RemoveReplica(host string, port int) {
	s.replicasMu.Lock()
	defer s.replicasMu.Unlock()

	key := fmt.Sprintf("%s:%d", host, port)
	delete(s.replicas, key)

	log.Printf("[SENTINEL] Removed replica %s:%d from monitoring", host, port)
}

// GetMasterAddr returns current master address
func (s *Sentinel) GetMasterAddr() (string, int) {
	s.master.mu.RLock()
	defer s.master.mu.RUnlock()

	return s.master.Host, s.master.Port
}

// GetStatus returns sentinel status
func (s *Sentinel) GetStatus() map[string]interface{} {
	status := make(map[string]interface{})

	s.master.mu.RLock()
	status["master_host"] = s.master.Host
	status["master_port"] = s.master.Port
	status["master_status"] = s.getMasterStatus(s.master)
	s.master.mu.RUnlock()

	s.replicasMu.RLock()
	replicaList := make([]map[string]interface{}, 0, len(s.replicas))
	for _, replica := range s.replicas {
		replica.mu.RLock()
		replicaInfo := map[string]interface{}{
			"host":     replica.Host,
			"port":     replica.Port,
			"status":   s.getReplicaStatus(replica),
			"priority": replica.Priority,
			"offset":   replica.ReplOffset,
		}
		replica.mu.RUnlock()
		replicaList = append(replicaList, replicaInfo)
	}
	s.replicasMu.RUnlock()

	status["replicas"] = replicaList
	status["replicas_count"] = len(replicaList)

	s.failoverMu.Lock()
	status["failover_in_progress"] = s.failoverInProgress
	s.failoverMu.Unlock()

	return status
}

func (s *Sentinel) getMasterStatus(m *MonitoredInstance) string {
	if m.IsDown {
		return "down"
	}
	if m.LastPingOK {
		return "ok"
	}
	return "unknown"
}

func (s *Sentinel) getReplicaStatus(r *MonitoredInstance) string {
	if r.IsDown {
		return "down"
	}
	if r.LastPingOK {
		return "ok"
	}
	return "unknown"
}
