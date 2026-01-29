package replication

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

// ==================== REPLICATION DATA STRUCTURES ====================

// Role represents the server's role in replication
type Role string

const (
	RoleMaster  Role = "master"
	RoleReplica Role = "slave" // Redis uses "slave" in protocol
)

// ReplicaInfo represents a connected replica
type ReplicaInfo struct {
	Conn             net.Conn
	Writer           *bufio.Writer
	ID               string
	Addr             string
	ListeningPort    int // Port replica is listening on (from REPLCONF)
	ConnectedAt      time.Time
	LastPingAt       time.Time
	Offset           int64 // Replication offset
	State            ReplicaState
	CapabilityPSYNC2 bool // Supports partial resync
	mu               sync.Mutex
}

// ReplicaState represents the state of replica connection
type ReplicaState string

const (
	ReplicaStateConnecting ReplicaState = "connecting"
	ReplicaStateSyncing    ReplicaState = "syncing"
	ReplicaStateOnline     ReplicaState = "online"
	ReplicaStateOffline    ReplicaState = "offline"
)

// MasterInfo represents connection to master (when acting as replica)
type MasterInfo struct {
	Host            string
	Port            int
	Conn            net.Conn
	Writer          *bufio.Writer
	Reader          *bufio.Reader
	LastInteraction time.Time
	Offset          int64  // Our replication offset
	MasterReplID    string // Master's replication ID
	State           MasterState
	mu              sync.Mutex
}

// MasterState represents the state of master connection
type MasterState string

const (
	MasterStateDisconnected MasterState = "disconnected"
	MasterStateConnecting   MasterState = "connecting"
	MasterStateSyncing      MasterState = "syncing"
	MasterStateConnected    MasterState = "connected"
)

// ReplicationManager manages replication for both master and replica
type ReplicationManager struct {
	role   Role
	replID string // Our replication ID (40 char random string)
	offset int64  // Master replication offset

	// Master-specific fields
	replicas   map[string]*ReplicaInfo // Connected replicas (key = replica ID)
	replicasMu sync.RWMutex

	// Replica-specific fields
	masterInfo    *MasterInfo
	masterInfoMu  sync.RWMutex
	listeningPort int // Server's listening port (for REPLCONF)
	priority      int // Replica priority for Sentinel failover (0-100)

	// Backlog for partial resync
	backlog   *ReplicationBacklog
	backlogMu sync.RWMutex

	// Command propagation
	commandChan  chan *Command
	shutdownChan chan struct{}
	wg           sync.WaitGroup

	// Command execution (for replica)
	commandExecutor func([]string) error
	mu              sync.RWMutex // Protects commandExecutor

	// Store access (for RDB generation)
	storeGetter   func() interface{}
	storeGetterMu sync.RWMutex
}

// Command represents a command to be propagated to replicas
type Command struct {
	Args      []string
	Timestamp time.Time
}

// ReplicationBacklog is a circular buffer for storing recent commands
type ReplicationBacklog struct {
	buffer     []byte
	size       int
	offset     int64 // Starting offset of the backlog
	idx        int   // Current write position in buffer
	historyLen int   // Actual data length in buffer
}

// NewReplicationBacklog creates a new replication backlog
func NewReplicationBacklog(size int) *ReplicationBacklog {
	return &ReplicationBacklog{
		buffer:     make([]byte, size),
		size:       size,
		offset:     0,
		idx:        0,
		historyLen: 0,
	}
}

// Append adds data to the backlog
func (rb *ReplicationBacklog) Append(data []byte) {
	dataLen := len(data)

	// If data is larger than buffer, only keep the tail
	if dataLen >= rb.size {
		copy(rb.buffer, data[dataLen-rb.size:])
		rb.offset += int64(dataLen - rb.size)
		rb.idx = 0
		rb.historyLen = rb.size
		return
	}

	// Circular buffer logic
	for i := 0; i < dataLen; i++ {
		rb.buffer[rb.idx] = data[i]
		rb.idx = (rb.idx + 1) % rb.size

		if rb.historyLen < rb.size {
			rb.historyLen++
		} else {
			rb.offset++
		}
	}
}

// GetRange returns data from the backlog starting at offset
func (rb *ReplicationBacklog) GetRange(offset int64) ([]byte, bool) {
	// Check if offset is too old
	if offset < rb.offset {
		return nil, false
	}

	// Check if offset is too new
	if offset > rb.offset+int64(rb.historyLen) {
		return nil, false
	}

	// Calculate start position in circular buffer
	relativeOffset := offset - rb.offset
	startIdx := int(relativeOffset)
	length := rb.historyLen - startIdx

	result := make([]byte, length)

	// Handle circular buffer wrap-around
	if startIdx+length <= rb.size {
		copy(result, rb.buffer[startIdx:startIdx+length])
	} else {
		firstPart := rb.size - startIdx
		copy(result[:firstPart], rb.buffer[startIdx:])
		copy(result[firstPart:], rb.buffer[:length-firstPart])
	}

	return result, true
}

// NewReplicationManager creates a new replication manager
func NewReplicationManager(role Role) *ReplicationManager {
	rm := &ReplicationManager{
		role:         role,
		replID:       generateReplID(),
		offset:       0,
		replicas:     make(map[string]*ReplicaInfo),
		backlog:      NewReplicationBacklog(1024 * 1024), // 1MB backlog
		commandChan:  make(chan *Command, 1000),
		shutdownChan: make(chan struct{}),
		priority:     100, // Default priority
	}

	// Start command propagation goroutine for master
	if role == RoleMaster {
		rm.wg.Add(1)
		go rm.propagateCommands()
	}

	return rm
}

// SetListeningPort sets the server's listening port (used by replicas for REPLCONF)
func (rm *ReplicationManager) SetListeningPort(port int) {
	rm.listeningPort = port
}

// GetListeningPort returns the server's listening port
func (rm *ReplicationManager) GetListeningPort() int {
	return rm.listeningPort
}

// SetPriority sets the replica priority for Sentinel failover
func (rm *ReplicationManager) SetPriority(priority int) {
	rm.priority = priority
}

// GetPriority returns the replica priority
func (rm *ReplicationManager) GetPriority() int {
	return rm.priority
}

// GetRole returns the current role (master or replica)
func (rm *ReplicationManager) GetRole() Role {
	return rm.role
}

// generateReplID generates a random 40-character replication ID
// Uses crypto/rand for cryptographically secure random generation
func generateReplID() string {
	b := make([]byte, 20) // 20 bytes = 40 hex characters
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		log.Printf("[REPLICATION] WARNING: crypto/rand failed, using fallback: %v", err)
		return fmt.Sprintf("%040d", time.Now().UnixNano())
	}
	// Convert to 40-character hexadecimal string
	return fmt.Sprintf("%x", b)
}

// ==================== MASTER OPERATIONS ====================

// AddReplica adds a new replica connection
func (rm *ReplicationManager) AddReplica(conn net.Conn, id string) *ReplicaInfo {
	rm.replicasMu.Lock()
	defer rm.replicasMu.Unlock()

	replica := &ReplicaInfo{
		Conn:        conn,
		Writer:      bufio.NewWriter(conn),
		ID:          id,
		Addr:        conn.RemoteAddr().String(),
		ConnectedAt: time.Now(),
		LastPingAt:  time.Now(),
		Offset:      0,
		State:       ReplicaStateConnecting,
	}

	rm.replicas[id] = replica
	log.Printf("[REPLICATION] Replica connected: %s (%s)", id, replica.Addr)

	return replica
}

// RemoveReplica removes a replica connection
func (rm *ReplicationManager) RemoveReplica(id string) {
	rm.replicasMu.Lock()
	defer rm.replicasMu.Unlock()

	if replica, exists := rm.replicas[id]; exists {
		replica.Conn.Close()
		delete(rm.replicas, id)
		log.Printf("[REPLICATION] Replica disconnected: %s", id)
	}
}

// GetReplica returns a replica by ID
func (rm *ReplicationManager) GetReplica(id string) (*ReplicaInfo, bool) {
	rm.replicasMu.RLock()
	defer rm.replicasMu.RUnlock()

	replica, exists := rm.replicas[id]
	return replica, exists
}

// GetReplicaByAddr returns a replica by connection address
func (rm *ReplicationManager) GetReplicaByAddr(addr string) (*ReplicaInfo, bool) {
	rm.replicasMu.RLock()
	defer rm.replicasMu.RUnlock()

	for _, replica := range rm.replicas {
		if replica.Addr == addr {
			return replica, true
		}
	}
	return nil, false
}

// UpdateReplicaOffset updates the offset for a replica
func (rm *ReplicationManager) UpdateReplicaOffset(id string, offset int64) {
	rm.replicasMu.Lock()
	defer rm.replicasMu.Unlock()

	if replica, exists := rm.replicas[id]; exists {
		replica.Offset = offset
		replica.LastPingAt = time.Now()
	}
}

// SetReplicaListeningPort sets the listening port for a replica
func (rm *ReplicationManager) SetReplicaListeningPort(id string, port int) {
	rm.replicasMu.Lock()
	defer rm.replicasMu.Unlock()

	if replica, exists := rm.replicas[id]; exists {
		replica.ListeningPort = port
		log.Printf("[REPLICATION] Set listening port %d for replica %s", port, id)
	}
}

// GetAllReplicas returns all connected replicas
func (rm *ReplicationManager) GetAllReplicas() []*ReplicaInfo {
	rm.replicasMu.RLock()
	defer rm.replicasMu.RUnlock()

	replicas := make([]*ReplicaInfo, 0, len(rm.replicas))
	for _, replica := range rm.replicas {
		replicas = append(replicas, replica)
	}

	return replicas
}

// PropagateCommand queues a command for propagation to replicas
func (rm *ReplicationManager) PropagateCommand(args []string) {
	if rm.role != RoleMaster {
		return
	}

	cmd := &Command{
		Args:      args,
		Timestamp: time.Now(),
	}

	select {
	case rm.commandChan <- cmd:
	default:
		log.Printf("[REPLICATION] WARNING: Command queue full, dropping command")
	}
}

// propagateCommands handles command propagation to all replicas
func (rm *ReplicationManager) propagateCommands() {
	defer rm.wg.Done()

	for {
		select {
		case cmd := <-rm.commandChan:
			rm.propagateToReplicas(cmd)
		case <-rm.shutdownChan:
			return
		}
	}
}

// propagateToReplicas sends a command to all connected replicas
func (rm *ReplicationManager) propagateToReplicas(cmd *Command) {
	// Encode command in RESP format
	respData := encodeCommandRESP(cmd.Args)

	// Add to backlog
	rm.backlogMu.Lock()
	rm.backlog.Append(respData)
	rm.offset += int64(len(respData))
	currentOffset := rm.offset
	rm.backlogMu.Unlock()

	// Send to all replicas
	rm.replicasMu.RLock()
	replicas := make([]*ReplicaInfo, 0, len(rm.replicas))
	for _, replica := range rm.replicas {
		if replica.State == ReplicaStateOnline {
			replicas = append(replicas, replica)
		}
	}
	rm.replicasMu.RUnlock()

	for _, replica := range replicas {
		replica.mu.Lock()
		_, err := replica.Writer.Write(respData)
		if err != nil {
			log.Printf("[REPLICATION] Error sending to replica %s: %v", replica.ID, err)
			replica.State = ReplicaStateOffline
			replica.mu.Unlock()
			rm.RemoveReplica(replica.ID)
			continue
		}

		err = replica.Writer.Flush()
		if err != nil {
			log.Printf("[REPLICATION] Error flushing to replica %s: %v", replica.ID, err)
			replica.State = ReplicaStateOffline
			replica.mu.Unlock()
			rm.RemoveReplica(replica.ID)
			continue
		}

		replica.Offset = currentOffset
		replica.mu.Unlock()
	}
}

// encodeCommandRESP encodes a command in RESP array format
func encodeCommandRESP(args []string) []byte {
	result := fmt.Sprintf("*%d\r\n", len(args))
	for _, arg := range args {
		result += fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg)
	}
	return []byte(result)
}

// parseAddr extracts IP and port from address string (e.g., "127.0.0.1:6380")
func parseAddr(addr string) (string, int) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0
	}
	return host, port
}

// GetInfo returns replication info
func (rm *ReplicationManager) GetInfo() map[string]interface{} {
	info := make(map[string]interface{})

	info["role"] = string(rm.role)
	info["master_repl_id"] = rm.replID
	info["master_repl_offset"] = rm.offset

	if rm.role == RoleMaster {
		rm.replicasMu.RLock()
		info["connected_slaves"] = len(rm.replicas)

		// Build slaves array for Sentinel
		slaves := make([]map[string]interface{}, 0, len(rm.replicas))
		i := 0
		for _, replica := range rm.replicas {
			ip, port := parseAddr(replica.Addr)

			// Use the listening port if available (sent via REPLCONF)
			// Otherwise fall back to the port from the connection address
			if replica.ListeningPort > 0 {
				port = replica.ListeningPort
			}

			slaveInfo := map[string]interface{}{
				"id":     replica.ID,
				"ip":     ip,
				"port":   port,
				"state":  string(replica.State),
				"offset": replica.Offset,
				"lag":    time.Since(replica.LastPingAt).Seconds(),
			}
			info[fmt.Sprintf("slave%d", i)] = slaveInfo
			slaves = append(slaves, slaveInfo)
			i++
		}
		info["slaves"] = slaves
		rm.replicasMu.RUnlock()
	} else {
		// Replica-specific info
		info["slave_priority"] = rm.priority // For Sentinel to discover
		rm.masterInfoMu.RLock()
		if rm.masterInfo != nil {
			info["master_host"] = rm.masterInfo.Host
			info["master_port"] = rm.masterInfo.Port
			info["master_link_status"] = string(rm.masterInfo.State)
			info["master_last_io_seconds_ago"] = time.Since(rm.masterInfo.LastInteraction).Seconds()
			info["master_sync_in_progress"] = rm.masterInfo.State == MasterStateSyncing
			info["slave_repl_offset"] = rm.masterInfo.Offset
			info["master_replid"] = rm.masterInfo.MasterReplID
		}
		rm.masterInfoMu.RUnlock()
	}

	return info
}

// GetBacklogData retrieves data from replication backlog starting at offset
// Returns the data and true if available, or nil and false if offset is too old
func (rm *ReplicationManager) GetBacklogData(offset int64) ([]byte, bool) {
	rm.backlogMu.RLock()
	defer rm.backlogMu.RUnlock()

	if rm.backlog == nil {
		return nil, false
	}

	return rm.backlog.GetRange(offset)
}

// Shutdown gracefully shuts down replication
func (rm *ReplicationManager) Shutdown() {
	log.Println("[REPLICATION] Starting graceful shutdown...")

	// Stop accepting new commands
	close(rm.shutdownChan)

	// Wait for command queue to drain
	rm.wg.Wait()
	log.Println("[REPLICATION] Command queue drained")

	// Flush and close all replica connections
	rm.replicasMu.Lock()
	for _, replica := range rm.replicas {
		replica.mu.Lock()

		// Flush any buffered data
		if err := replica.Writer.Flush(); err != nil {
			log.Printf("[REPLICATION] Error flushing replica %s: %v", replica.ID, err)
		}

		// Close TCP connection
		replica.Conn.Close()
		replica.mu.Unlock()

		log.Printf("[REPLICATION] Closed replica %s", replica.ID)
	}
	rm.replicasMu.Unlock()

	// Close master connection
	rm.masterInfoMu.Lock()
	if rm.masterInfo != nil && rm.masterInfo.Conn != nil {
		// Flush master connection if we're a replica
		if rm.masterInfo.Writer != nil {
			if err := rm.masterInfo.Writer.Flush(); err != nil {
				log.Printf("[REPLICATION] Error flushing master connection: %v", err)
			}
		}
		rm.masterInfo.Conn.Close()
		log.Println("[REPLICATION] Disconnected from master")
	}
	rm.masterInfoMu.Unlock()

	log.Println("[REPLICATION] Shutdown complete")
}

// SetCommandExecutor sets the callback for executing commands on replica
func (rm *ReplicationManager) SetCommandExecutor(executor func(args []string) error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.commandExecutor = executor
}

// SetStoreGetter sets the callback for getting store snapshot (for RDB generation)
func (rm *ReplicationManager) SetStoreGetter(getter func() interface{}) {
	rm.storeGetterMu.Lock()
	defer rm.storeGetterMu.Unlock()
	rm.storeGetter = getter
}

// GetStoreSnapshot gets a snapshot of the store for RDB generation
func (rm *ReplicationManager) GetStoreSnapshot() interface{} {
	rm.storeGetterMu.RLock()
	getter := rm.storeGetter
	rm.storeGetterMu.RUnlock()

	if getter == nil {
		return nil
	}

	return getter()
}

// executeReplicatedCommand executes a command received from master
func (rm *ReplicationManager) executeReplicatedCommand(args []string) error {
	rm.mu.RLock()
	executor := rm.commandExecutor
	rm.mu.RUnlock()

	if executor == nil {
		log.Printf("[REPLICATION] No command executor set, skipping command: %v", args)
		return nil
	}

	return executor(args)
}
