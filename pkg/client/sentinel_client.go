package client

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"redis/internal/protocol"
)

// SentinelClient is a Redis client with Sentinel support and read-write splitting
type SentinelClient struct {
	// Sentinel configuration
	sentinelAddrs []string
	masterName    string

	// Connections (TCP connections persist until explicitly closed)
	// These connections are reused across multiple commands for efficiency
	masterConn   net.Conn   // Persistent connection to current master
	replicaConns []net.Conn // Persistent connections to replicas
	connMu       sync.RWMutex

	// Load balancing
	roundRobin int
	mu         sync.Mutex

	// Connection state
	masterAddr   string
	replicaAddrs []string

	// Options
	requireStrongConsistency bool
	healthCheckInterval      time.Duration
	stopHealthCheck          chan struct{}
}

// SentinelOptions configuration for Sentinel client
type SentinelOptions struct {
	SentinelAddrs            []string
	MasterName               string
	RequireStrongConsistency bool          // Verify connected to master before critical reads
	HealthCheckInterval      time.Duration // How often to verify master connection (0 = disabled)
}

// NewSentinelClient creates a new Sentinel-aware client
func NewSentinelClient(opts SentinelOptions) (*SentinelClient, error) {
	if len(opts.SentinelAddrs) == 0 {
		return nil, errors.New("at least one sentinel address required")
	}
	if opts.MasterName == "" {
		return nil, errors.New("master name required")
	}

	client := &SentinelClient{
		sentinelAddrs:            opts.SentinelAddrs,
		masterName:               opts.MasterName,
		requireStrongConsistency: opts.RequireStrongConsistency,
		healthCheckInterval:      opts.HealthCheckInterval,
		stopHealthCheck:          make(chan struct{}),
	}

	// Initial connection to master
	if err := client.reconnectToMaster(); err != nil {
		return nil, fmt.Errorf("failed to connect to master: %w", err)
	}

	// Discover and connect to replicas
	if err := client.discoverReplicas(); err != nil {
		// Log but don't fail - we can still use master for reads
		fmt.Printf("Warning: failed to discover replicas: %v\n", err)
	}

	// Start periodic health check if enabled
	if client.healthCheckInterval > 0 {
		go client.healthCheck()
	}

	return client, nil
}

// querySentinelForMaster queries Sentinel for current master address
func (c *SentinelClient) querySentinelForMaster() (string, error) {
	// Try each sentinel until one responds (failover mechanism)
	// Only ONE sentinel needs to respond - they all monitor the same master
	for _, sentinelAddr := range c.sentinelAddrs {
		conn, err := net.DialTimeout("tcp", sentinelAddr, 2*time.Second)
		if err != nil {
			continue // Try next sentinel if this one is down
		}
		defer conn.Close()

		// Send: SENTINEL GET-MASTER-ADDR-BY-NAME mymaster
		cmd := fmt.Sprintf("*3\r\n$8\r\nSENTINEL\r\n$22\r\nGET-MASTER-ADDR-BY-NAME\r\n$%d\r\n%s\r\n",
			len(c.masterName), c.masterName)
		if _, err := conn.Write([]byte(cmd)); err != nil {
			continue
		}

		// Read response
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			continue
		}

		// Parse array response: *2\r\n$9\r\n127.0.0.1\r\n$4\r\n6380\r\n
		if !strings.HasPrefix(line, "*2") {
			continue
		}

		// Read host bulk string
		reader.ReadString('\n') // Skip $<length>
		host, err := reader.ReadString('\n')
		if err != nil {
			continue
		}
		host = strings.TrimSpace(host)

		// Read port bulk string
		reader.ReadString('\n') // Skip $<length>
		port, err := reader.ReadString('\n')
		if err != nil {
			continue
		}
		port = strings.TrimSpace(port)

		return fmt.Sprintf("%s:%s", host, port), nil // Success - return immediately
	}

	return "", errors.New("all sentinels unreachable")
}

// querySentinelForReplicas queries Sentinel for replica addresses
func (c *SentinelClient) querySentinelForReplicas() ([]string, error) {
	// Try each sentinel until one responds
	// Only ONE sentinel needs to respond - they all monitor the same replicas
	for _, sentinelAddr := range c.sentinelAddrs {
		conn, err := net.DialTimeout("tcp", sentinelAddr, 2*time.Second)
		if err != nil {
			continue // Try next sentinel if this one is down
		}
		defer conn.Close()

		// Send: SENTINEL REPLICAS mymaster
		cmd := fmt.Sprintf("*3\r\n$8\r\nSENTINEL\r\n$8\r\nREPLICAS\r\n$%d\r\n%s\r\n",
			len(c.masterName), c.masterName)
		if _, err := conn.Write([]byte(cmd)); err != nil {
			continue
		}

		// Real implementation would parse RESP array response
		// For simplicity, return empty slice
		// FIX: This should parse the actual response from Sentinel
		return []string{}, nil // Success - return immediately after first response
	}

	return nil, errors.New("all sentinels unreachable")
}

// reconnectToMaster queries Sentinel and reconnects to current master
func (c *SentinelClient) reconnectToMaster() error {
	masterAddr, err := c.querySentinelForMaster()
	if err != nil {
		return err
	}

	// Connect to master
	conn, err := net.DialTimeout("tcp", masterAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to master %s: %w", masterAddr, err)
	}

	// Close old connection
	c.connMu.Lock()
	if c.masterConn != nil {
		c.masterConn.Close()
	}
	c.masterConn = conn
	c.masterAddr = masterAddr
	c.connMu.Unlock()

	fmt.Printf("Connected to master: %s\n", masterAddr)
	return nil
}

// discoverReplicas finds and connects to all healthy replicas
func (c *SentinelClient) discoverReplicas() error {
	replicaAddrs, err := c.querySentinelForReplicas()
	if err != nil {
		return err
	}

	var newConns []net.Conn
	for _, addr := range replicaAddrs {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			continue
		}
		newConns = append(newConns, conn)
	}

	// Close old replica connections
	c.connMu.Lock()
	for _, conn := range c.replicaConns {
		conn.Close()
	}
	c.replicaConns = newConns
	c.replicaAddrs = replicaAddrs
	c.connMu.Unlock()

	return nil
}

// healthCheck periodically verifies we're connected to current master
func (c *SentinelClient) healthCheck() {
	ticker := time.NewTicker(c.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			currentMaster, err := c.querySentinelForMaster()
			if err != nil {
				continue
			}

			c.connMu.RLock()
			connected := c.masterAddr
			c.connMu.RUnlock()

			if currentMaster != connected {
				fmt.Printf("Master changed from %s to %s, reconnecting...\n", connected, currentMaster)
				c.reconnectToMaster()
			}
		case <-c.stopHealthCheck:
			return
		}
	}
}

// verifyConnectedToMaster checks if we're actually connected to master
// Note: This reuses the existing connection (c.masterConn), doesn't create a new one
// It verifies the server we're connected to is still the master (not a demoted replica)
func (c *SentinelClient) verifyConnectedToMaster() bool {
	c.connMu.RLock()
	conn := c.masterConn // Reuse existing connection
	c.connMu.RUnlock()

	if conn == nil {
		return false
	}

	// Send INFO replication command using existing connection
	cmd := "*2\r\n$4\r\nINFO\r\n$11\r\nreplication\r\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return false // Connection broken
	}

	// Read response and check for "role:master"
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false // Read failed
	}

	return strings.Contains(response, "role:master")
}

// Set writes a key-value pair (always goes to master)
func (c *SentinelClient) Set(key, value string) error {
	return c.executeWriteCommand("SET", key, value)
}

// Get reads a value (uses replica if available, master otherwise)
func (c *SentinelClient) Get(key string) (string, error) {
	return c.executeReadCommand("GET", key)
}

// executeWriteCommand sends write command to master with auto-reconnect
func (c *SentinelClient) executeWriteCommand(cmd string, args ...string) error {
	return c.executeWriteCommandWithRetry(cmd, 3, args...)
}

// executeWriteCommandWithRetry sends write command with retry limit to prevent infinite loops
func (c *SentinelClient) executeWriteCommandWithRetry(cmd string, maxRetries int, args ...string) error {
	if maxRetries <= 0 {
		return errors.New("max retries exceeded - master may be unstable")
	}

	c.connMu.RLock()
	conn := c.masterConn
	c.connMu.RUnlock()

	if conn == nil {
		if err := c.reconnectToMaster(); err != nil {
			return fmt.Errorf("failed to connect to master: %w", err)
		}
		c.connMu.RLock()
		conn = c.masterConn
		c.connMu.RUnlock()
	}

	// Build RESP command using protocol package
	fullArgs := append([]string{cmd}, args...)
	respCmd := protocol.EncodeArray(fullArgs)

	_, err := conn.Write(respCmd)
	if err != nil {
		// Connection failed, re-query Sentinel and retry
		c.reconnectToMaster()
		return c.executeWriteCommandWithRetry(cmd, maxRetries-1, args...)
	}

	// Read and parse response using protocol package
	reader := bufio.NewReader(conn)
	response, err := protocol.ParseCommand(reader)
	if err != nil {
		c.reconnectToMaster()
		return c.executeWriteCommandWithRetry(cmd, maxRetries-1, args...)
	}

	// Check for READONLY error (connected to demoted master)
	// Error responses have the error message in Args[0]
	if len(response.Args) > 0 && strings.Contains(response.Args[0], "READONLY") {
		c.reconnectToMaster()
		return c.executeWriteCommandWithRetry(cmd, maxRetries-1, args...)
	}

	return nil
}

// executeReadCommand sends read command (prefers replica, falls back to master)
func (c *SentinelClient) executeReadCommand(cmd string, args ...string) (string, error) {
	// If strong consistency required, verify we're on master
	if c.requireStrongConsistency {
		if !c.verifyConnectedToMaster() {
			c.reconnectToMaster()
		}
		return c.executeReadFromMaster(cmd, args...)
	}

	// Try replica first (round-robin load balancing)
	c.connMu.RLock()
	replicaCount := len(c.replicaConns)
	c.connMu.RUnlock()

	if replicaCount > 0 {
		result, err := c.executeReadFromReplica(cmd, args...)
		if err == nil {
			return result, nil
		}
		// Replica failed, fall back to master
	}

	return c.executeReadFromMaster(cmd, args...)
}

// executeReadFromReplica reads from replica with round-robin
func (c *SentinelClient) executeReadFromReplica(cmd string, args ...string) (string, error) {
	c.mu.Lock()
	c.connMu.RLock()

	if len(c.replicaConns) == 0 {
		c.connMu.RUnlock()
		c.mu.Unlock()
		return "", errors.New("no replicas available")
	}

	replica := c.replicaConns[c.roundRobin%len(c.replicaConns)]
	c.roundRobin++
	c.connMu.RUnlock()
	c.mu.Unlock()

	fullArgs := append([]string{cmd}, args...)
	respCmd := protocol.EncodeArray(fullArgs)

	if _, err := replica.Write(respCmd); err != nil {
		return "", err
	}

	reader := bufio.NewReader(replica)
	response, err := protocol.ParseCommand(reader)
	if err != nil {
		return "", err
	}

	// Response for GET command is a bulk string in Args[0]
	if len(response.Args) > 0 {
		return response.Args[0], nil
	}
	return "", nil
}

// executeReadFromMaster reads from master
func (c *SentinelClient) executeReadFromMaster(cmd string, args ...string) (string, error) {
	return c.executeReadFromMasterWithRetry(cmd, 3, args...)
}

// executeReadFromMasterWithRetry reads from master with retry limit
func (c *SentinelClient) executeReadFromMasterWithRetry(cmd string, maxRetries int, args ...string) (string, error) {
	if maxRetries <= 0 {
		return "", errors.New("max retries exceeded - master may be unstable")
	}

	c.connMu.RLock()
	conn := c.masterConn
	c.connMu.RUnlock()

	fullArgs := append([]string{cmd}, args...)
	respCmd := protocol.EncodeArray(fullArgs)

	if _, err := conn.Write(respCmd); err != nil {
		c.reconnectToMaster()
		return c.executeReadFromMasterWithRetry(cmd, maxRetries-1, args...)
	}

	reader := bufio.NewReader(conn)
	response, err := protocol.ParseCommand(reader)
	if err != nil {
		c.reconnectToMaster()
		return c.executeReadFromMasterWithRetry(cmd, maxRetries-1, args...)
	}

	// Response for GET command is a bulk string in Args[0]
	if len(response.Args) > 0 {
		return response.Args[0], nil
	}
	return "", nil
}

// Close closes all connections
func (c *SentinelClient) Close() {
	close(c.stopHealthCheck)

	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.masterConn != nil {
		c.masterConn.Close()
	}

	for _, conn := range c.replicaConns {
		conn.Close()
	}
}

// Example usage:
func ExampleUsage() {
	client, _ := NewSentinelClient(SentinelOptions{
		SentinelAddrs:            []string{"127.0.0.1:26379"},
		MasterName:               "mymaster",
		RequireStrongConsistency: false,
		HealthCheckInterval:      5 * time.Second,
	})
	defer client.Close()

	// Writes go to master
	client.Set("key1", "value1")

	// Reads go to replicas (round-robin)
	value, _ := client.Get("key1")
	fmt.Println(value)
}
