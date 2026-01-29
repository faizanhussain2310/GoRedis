package replication

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc64"
	"log"
	"net"
	"strings"
	"time"
)

// ==================== REPLICA CLIENT OPERATIONS ====================

// ConnectToMaster connects to a master server as a replica
func (rm *ReplicationManager) ConnectToMaster(host string, port int) error {
	rm.masterInfoMu.Lock()
	defer rm.masterInfoMu.Unlock()

	// Preserve replication ID and offset from previous connection (for partial resync)
	var savedReplID string
	var savedOffset int64

	if rm.masterInfo != nil {
		savedReplID = rm.masterInfo.MasterReplID
		savedOffset = rm.masterInfo.Offset

		// Close existing connection if any
		if rm.masterInfo.Conn != nil {
			rm.masterInfo.Conn.Close()
		}
	}

	// Create new master info, preserving replication state if available
	rm.masterInfo = &MasterInfo{
		Host:            host,
		Port:            port,
		State:           MasterStateConnecting,
		LastInteraction: time.Now(),
		MasterReplID:    savedReplID,
		Offset:          savedOffset,
	}

	// Connect to master
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		rm.masterInfo.State = MasterStateDisconnected
		return fmt.Errorf("failed to connect to master: %w", err)
	}

	rm.masterInfo.Conn = conn
	rm.masterInfo.Reader = bufio.NewReader(conn)
	rm.masterInfo.Writer = bufio.NewWriter(conn)

	// Enable TCP keepalive for dead connection detection
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	// Change role to replica
	rm.role = RoleReplica

	log.Printf("[REPLICATION] Connected to master %s, role changed to replica", addr)

	// Start handshake
	go rm.performHandshake()

	return nil
}

// performHandshake performs the replication handshake with master
func (rm *ReplicationManager) performHandshake() {
	rm.masterInfoMu.Lock()
	master := rm.masterInfo
	rm.masterInfoMu.Unlock()

	if master == nil {
		return
	}

	// Step 1: Send PING
	if err := rm.sendToMaster("PING\r\n"); err != nil {
		log.Printf("[REPLICATION] Handshake failed at PING: %v", err)
		rm.handleMasterDisconnect()
		return
	}

	resp, err := rm.readFromMaster()
	if err != nil || !strings.Contains(resp, "PONG") {
		log.Printf("[REPLICATION] Invalid PING response: %v", err)
		rm.handleMasterDisconnect()
		return
	}

	log.Printf("[REPLICATION] Handshake: PING OK")

	// Step 2: Send REPLCONF listening-port
	port := rm.GetListeningPort()
	if port == 0 {
		port = 6379 // Default port if not set
	}
	cmd := fmt.Sprintf("*3\r\n$8\r\nREPLCONF\r\n$14\r\nlistening-port\r\n$%d\r\n%d\r\n", len(fmt.Sprint(port)), port)
	if err := rm.sendToMaster(cmd); err != nil {
		log.Printf("[REPLICATION] Handshake failed at REPLCONF listening-port: %v", err)
		rm.handleMasterDisconnect()
		return
	}

	resp, err = rm.readFromMaster()
	if err != nil || !strings.Contains(resp, "OK") {
		log.Printf("[REPLICATION] Invalid REPLCONF listening-port response: %v", err)
		rm.handleMasterDisconnect()
		return
	}

	log.Printf("[REPLICATION] Handshake: REPLCONF listening-port OK")

	// Step 3: Send REPLCONF capa psync2
	cmd = "*3\r\n$8\r\nREPLCONF\r\n$4\r\ncapa\r\n$6\r\npsync2\r\n"
	if err := rm.sendToMaster(cmd); err != nil {
		log.Printf("[REPLICATION] Handshake failed at REPLCONF capa: %v", err)
		rm.handleMasterDisconnect()
		return
	}

	resp, err = rm.readFromMaster()
	if err != nil || !strings.Contains(resp, "OK") {
		log.Printf("[REPLICATION] Invalid REPLCONF capa response: %v", err)
		rm.handleMasterDisconnect()
		return
	}

	log.Printf("[REPLICATION] Handshake: REPLCONF capa OK")

	// Step 4: Send PSYNC (with replid and offset if we have them)
	// If we've synced before, try partial resync. Otherwise request full resync.
	rm.masterInfoMu.Lock()
	replID := rm.masterInfo.MasterReplID
	offset := rm.masterInfo.Offset
	rm.masterInfoMu.Unlock()

	if replID == "" {
		// First time sync - request full resync
		cmd = "*3\r\n$5\r\nPSYNC\r\n$1\r\n?\r\n$2\r\n-1\r\n"
		log.Printf("[REPLICATION] Sending PSYNC ? -1 (requesting full resync)")
	} else {
		// We have a previous replid - try partial resync
		offsetStr := fmt.Sprintf("%d", offset)
		cmd = fmt.Sprintf("*3\r\n$5\r\nPSYNC\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
			len(replID), replID, len(offsetStr), offsetStr)
		log.Printf("[REPLICATION] Sending PSYNC %s %d (requesting partial resync)", replID, offset)
	}

	if err := rm.sendToMaster(cmd); err != nil {
		log.Printf("[REPLICATION] Handshake failed at PSYNC: %v", err)
		rm.handleMasterDisconnect()
		return
	}

	resp, err = rm.readFromMaster()
	if err != nil {
		log.Printf("[REPLICATION] PSYNC response error: %v", err)
		rm.handleMasterDisconnect()
		return
	}

	log.Printf("[REPLICATION] PSYNC response: %s", resp)

	// Parse PSYNC response: +FULLRESYNC <replid> <offset>
	if strings.HasPrefix(resp, "+FULLRESYNC") {
		parts := strings.Fields(resp)
		if len(parts) >= 3 {
			rm.masterInfoMu.Lock()
			rm.masterInfo.MasterReplID = parts[1]
			fmt.Sscanf(parts[2], "%d", &rm.masterInfo.Offset)
			rm.masterInfo.State = MasterStateSyncing
			rm.masterInfoMu.Unlock()

			log.Printf("[REPLICATION] Full resync: replid=%s offset=%d", parts[1], rm.masterInfo.Offset)
		}
	} else if strings.HasPrefix(resp, "+CONTINUE") {
		log.Printf("[REPLICATION] Partial resync accepted")
		rm.masterInfoMu.Lock()
		rm.masterInfo.State = MasterStateConnected
		rm.masterInfoMu.Unlock()
	}

	// Start receiving replication stream
	go rm.receiveReplicationStream()

	// Start heartbeat to keep connection alive and sync offset
	go rm.sendReplicationHeartbeat()
}

// sendToMaster sends data to master
func (rm *ReplicationManager) sendToMaster(data string) error {
	rm.masterInfoMu.Lock()
	defer rm.masterInfoMu.Unlock()

	if rm.masterInfo == nil || rm.masterInfo.Conn == nil {
		return fmt.Errorf("not connected to master")
	}

	_, err := rm.masterInfo.Writer.WriteString(data)
	if err != nil {
		return err
	}

	err = rm.masterInfo.Writer.Flush()
	if err != nil {
		return err
	}

	rm.masterInfo.LastInteraction = time.Now()
	return nil
}

// readFromMaster reads a response from master
func (rm *ReplicationManager) readFromMaster() (string, error) {
	rm.masterInfoMu.Lock()
	defer rm.masterInfoMu.Unlock()

	if rm.masterInfo == nil || rm.masterInfo.Reader == nil {
		return "", fmt.Errorf("not connected to master")
	}

	line, err := rm.masterInfo.Reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	rm.masterInfo.LastInteraction = time.Now()
	return strings.TrimSpace(line), nil
}

// receiveReplicationStream continuously receives commands from master
func (rm *ReplicationManager) receiveReplicationStream() {
	log.Printf("[REPLICATION] Starting replication stream receiver")

	for {
		// Check if still connected
		rm.masterInfoMu.RLock()
		if rm.masterInfo == nil || rm.masterInfo.Conn == nil {
			rm.masterInfoMu.RUnlock()
			break
		}
		reader := rm.masterInfo.Reader
		conn := rm.masterInfo.Conn
		rm.masterInfoMu.RUnlock()

		// Set read deadline (65s - slightly longer than repl-timeout)
		// This prevents infinite blocking if master goes silent
		conn.SetReadDeadline(time.Now().Add(65 * time.Second))

		// Read RESP command
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("[REPLICATION] Error reading from master: %v", err)
			rm.handleMasterDisconnect()
			break
		}

		line = strings.TrimSpace(line)

		// Skip empty lines
		if line == "" {
			continue
		}

		// Handle RDB file transfer (for full sync)
		if strings.HasPrefix(line, "$") {
			// RDB file size
			var size int
			fmt.Sscanf(line, "$%d", &size)

			log.Printf("[REPLICATION] Receiving RDB file: %d bytes", size)

			// Read RDB data
			rdbData := make([]byte, size)
			_, err := reader.Read(rdbData)
			if err != nil {
				log.Printf("[REPLICATION] Error reading RDB: %v", err)
				rm.handleMasterDisconnect()
				break
			}

			log.Printf("[REPLICATION] RDB received, sync complete")

			rm.masterInfoMu.Lock()
			if rm.masterInfo != nil {
				rm.masterInfo.State = MasterStateConnected
			}
			rm.masterInfoMu.Unlock()

			// Load RDB into store
			if err := rm.loadRDBIntoStore(rdbData); err != nil {
				log.Printf("[REPLICATION] Error loading RDB: %v", err)
			} else {
				log.Printf("[REPLICATION] RDB loaded successfully")
			}
			continue
		}

		// Handle RESP array (commands)
		if strings.HasPrefix(line, "*") {
			// Parse array length
			var arrayLen int
			fmt.Sscanf(line, "*%d", &arrayLen)

			args := make([]string, arrayLen)
			for i := 0; i < arrayLen; i++ {
				// Read bulk string length
				lenLine, err := reader.ReadString('\n')
				if err != nil {
					log.Printf("[REPLICATION] Error reading command length: %v", err)
					rm.handleMasterDisconnect()
					return
				}

				var argLen int
				fmt.Sscanf(strings.TrimSpace(lenLine), "$%d", &argLen)

				// Read bulk string data
				argData := make([]byte, argLen)
				_, err = reader.Read(argData)
				if err != nil {
					log.Printf("[REPLICATION] Error reading command data: %v", err)
					rm.handleMasterDisconnect()
					return
				}

				args[i] = string(argData)

				// Read trailing \r\n
				reader.ReadString('\n')
			}

			// Process command
			log.Printf("[REPLICATION] Received command from master: %v", args)

			// Handle special replication commands
			if len(args) > 0 {
				cmdName := strings.ToUpper(args[0])

				// Respond to PING from master to keep connection alive
				if cmdName == "PING" {
					rm.sendToMaster("+PONG\r\n")
					continue
				}

				// Handle REPLCONF GETACK (master asking for offset)
				if cmdName == "REPLCONF" && len(args) > 1 && strings.ToUpper(args[1]) == "GETACK" {
					offset := rm.masterInfo.Offset
					offsetStr := fmt.Sprintf("%d", offset)
					resp := fmt.Sprintf("*3\r\n$8\r\nREPLCONF\r\n$3\r\nACK\r\n$%d\r\n%s\r\n", len(offsetStr), offsetStr)
					rm.sendToMaster(resp)
					continue
				}
			}

			// Execute command on local store
			if err := rm.executeReplicatedCommand(args); err != nil {
				log.Printf("[REPLICATION] Error executing replicated command %v: %v", args, err)
			}

			// Update offset
			rm.masterInfoMu.Lock()
			if rm.masterInfo != nil {
				rm.masterInfo.Offset++
			}
			rm.masterInfoMu.Unlock()
		}
	}

	log.Printf("[REPLICATION] Replication stream receiver stopped")
}

// handleMasterDisconnect handles disconnection from master
func (rm *ReplicationManager) handleMasterDisconnect() {
	rm.masterInfoMu.Lock()

	if rm.masterInfo == nil {
		rm.masterInfoMu.Unlock()
		return
	}

	host := rm.masterInfo.Host
	port := rm.masterInfo.Port

	if rm.masterInfo.Conn != nil {
		rm.masterInfo.Conn.Close()
	}
	rm.masterInfo.State = MasterStateDisconnected
	rm.masterInfoMu.Unlock()

	log.Printf("[REPLICATION] Disconnected from master")

	// Auto-reconnect after 5 seconds
	go func() {
		time.Sleep(5 * time.Second)

		log.Printf("[REPLICATION] Attempting to reconnect to master %s:%d", host, port)
		if err := rm.ConnectToMaster(host, port); err != nil {
			log.Printf("[REPLICATION] Reconnection failed: %v", err)
			// Will retry again after next disconnect
		}
	}()
}

// DisconnectFromMaster disconnects from master
func (rm *ReplicationManager) DisconnectFromMaster() {
	rm.masterInfoMu.Lock()
	defer rm.masterInfoMu.Unlock()

	if rm.masterInfo != nil {
		// Preserve replication ID and offset for potential partial resync later
		savedReplID := rm.masterInfo.MasterReplID
		savedOffset := rm.masterInfo.Offset

		// Close connection
		if rm.masterInfo.Conn != nil {
			rm.masterInfo.Conn.Close()
		}

		// Reset master info but preserve replication state for future reconnection
		rm.masterInfo = &MasterInfo{
			MasterReplID: savedReplID,
			Offset:       savedOffset,
			State:        MasterStateDisconnected,
		}

		log.Printf("[REPLICATION] Manually disconnected from master (preserved replid=%s, offset=%d)", savedReplID, savedOffset)
	}

	// Change role to master
	rm.role = RoleMaster
	log.Printf("[REPLICATION] Role changed to master")
}

// GetMasterInfo returns master connection info
func (rm *ReplicationManager) GetMasterInfo() *MasterInfo {
	rm.masterInfoMu.RLock()
	defer rm.masterInfoMu.RUnlock()

	return rm.masterInfo
}

// sendReplicationHeartbeat sends REPLCONF ACK periodically to keep connection alive
func (rm *ReplicationManager) sendReplicationHeartbeat() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	log.Printf("[REPLICATION] Starting heartbeat sender")

	for range ticker.C {
		// Check if still connected
		rm.masterInfoMu.RLock()
		if rm.masterInfo == nil || rm.masterInfo.Conn == nil || rm.masterInfo.State != MasterStateConnected {
			rm.masterInfoMu.RUnlock()
			log.Printf("[REPLICATION] Stopping heartbeat - not connected")
			return
		}
		offset := rm.masterInfo.Offset
		rm.masterInfoMu.RUnlock()

		// Send REPLCONF ACK <offset>
		offsetStr := fmt.Sprintf("%d", offset)
		cmd := fmt.Sprintf("*3\r\n$8\r\nREPLCONF\r\n$3\r\nACK\r\n$%d\r\n%s\r\n", len(offsetStr), offsetStr)

		if err := rm.sendToMaster(cmd); err != nil {
			log.Printf("[REPLICATION] Failed to send heartbeat: %v", err)
			rm.handleMasterDisconnect()
			return
		}

		// Note: We don't wait for response from REPLCONF ACK - master doesn't reply
	}
}

// loadRDBIntoStore loads an RDB file into the store by executing commands
func (rm *ReplicationManager) loadRDBIntoStore(rdbData []byte) error {
	if len(rdbData) < 18 {
		return fmt.Errorf("RDB file too small: %d bytes (minimum 18 bytes)", len(rdbData))
	}

	// Check magic string
	magic := string(rdbData[0:5])
	if magic != "REDIS" {
		return fmt.Errorf("invalid RDB magic: %s", magic)
	}

	// Parse version
	version := string(rdbData[5:9])
	log.Printf("[REPLICATION] Loading RDB version %s", version)

	// Verify CRC64 checksum (last 8 bytes)
	if len(rdbData) >= 18 {
		// Extract checksum from end of file
		storedChecksum := binary.LittleEndian.Uint64(rdbData[len(rdbData)-8:])

		// Calculate checksum of data (excluding last 8 bytes)
		table := crc64.MakeTable(crc64.ECMA)
		calculatedChecksum := crc64.Checksum(rdbData[:len(rdbData)-8], table)

		if storedChecksum != calculatedChecksum {
			log.Printf("[REPLICATION] WARNING: CRC64 checksum mismatch: stored=0x%016x, calculated=0x%016x", storedChecksum, calculatedChecksum)
			// Continue loading anyway, but log the warning
			// In production, you might want to reject the RDB
		} else {
			log.Printf("[REPLICATION] CRC64 checksum verified: 0x%016x", storedChecksum)
		}
	}

	// Start parsing from byte 9
	pos := 9

	for pos < len(rdbData) {
		if pos >= len(rdbData) {
			break
		}

		opcode := rdbData[pos]
		pos++

		switch opcode {
		case 0xFE: // SELECTDB
			// Read database number
			if pos >= len(rdbData) {
				return fmt.Errorf("unexpected EOF reading database number")
			}
			dbNum := rdbData[pos]
			pos++
			log.Printf("[REPLICATION] Selecting database %d", dbNum)

		case 0xFB: // RESIZEDB
			// Read hash table sizes
			dbSize, n := readLength(rdbData, pos)
			pos += n
			expiresSize, n := readLength(rdbData, pos)
			pos += n
			log.Printf("[REPLICATION] DB size: %d, expires: %d", dbSize, expiresSize)

		case 0xFC: // EXPIRETIME_MS
			// Read expiry time in milliseconds
			if pos+8 > len(rdbData) {
				return fmt.Errorf("unexpected EOF reading expiry time")
			}
			expiryMs := int64(rdbData[pos]) | int64(rdbData[pos+1])<<8 |
				int64(rdbData[pos+2])<<16 | int64(rdbData[pos+3])<<24 |
				int64(rdbData[pos+4])<<32 | int64(rdbData[pos+5])<<40 |
				int64(rdbData[pos+6])<<48 | int64(rdbData[pos+7])<<56
			pos += 8

			// Read next opcode (should be value type)
			if pos >= len(rdbData) {
				return fmt.Errorf("unexpected EOF after expiry")
			}
			valueType := rdbData[pos]
			pos++

			// Read key
			key, n, err := readString(rdbData, pos)
			if err != nil {
				return fmt.Errorf("error reading key: %v", err)
			}
			pos += n

			// Read value based on type
			pos, err = rm.loadRDBValue(valueType, key, rdbData, pos, expiryMs)
			if err != nil {
				return err
			}

		case 0xFD: // EXPIRETIME (seconds)
			// Read expiry time in seconds
			if pos+4 > len(rdbData) {
				return fmt.Errorf("unexpected EOF reading expiry time")
			}
			expirySec := int64(rdbData[pos]) | int64(rdbData[pos+1])<<8 |
				int64(rdbData[pos+2])<<16 | int64(rdbData[pos+3])<<24
			pos += 4
			expiryMs := expirySec * 1000

			// Read next opcode (should be value type)
			if pos >= len(rdbData) {
				return fmt.Errorf("unexpected EOF after expiry")
			}
			valueType := rdbData[pos]
			pos++

			// Read key
			key, n, err := readString(rdbData, pos)
			if err != nil {
				return fmt.Errorf("error reading key: %v", err)
			}
			pos += n

			// Read value based on type
			pos, err = rm.loadRDBValue(valueType, key, rdbData, pos, expiryMs)
			if err != nil {
				return err
			}

		case 0xFF: // EOF
			log.Printf("[REPLICATION] Reached end of RDB file")
			return nil

		default:
			// This is a value type opcode (0-14)
			if opcode > 14 {
				return fmt.Errorf("unknown opcode: 0x%02X at position %d", opcode, pos-1)
			}

			// Read key
			key, n, err := readString(rdbData, pos)
			if err != nil {
				return fmt.Errorf("error reading key: %v", err)
			}
			pos += n

			// Read value based on type (no expiry)
			pos, err = rm.loadRDBValue(opcode, key, rdbData, pos, 0)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// loadRDBValue loads a single key-value pair from RDB
func (rm *ReplicationManager) loadRDBValue(valueType byte, key string, rdbData []byte, pos int, expiryMs int64) (int, error) {
	switch valueType {
	case 0: // String
		value, n, err := readString(rdbData, pos)
		if err != nil {
			return pos, fmt.Errorf("error reading string value: %v", err)
		}
		pos += n

		// Execute SET command
		args := []string{"SET", key, value}
		if expiryMs > 0 {
			// Calculate TTL in milliseconds
			now := time.Now().UnixNano() / int64(time.Millisecond)
			ttl := expiryMs - now
			if ttl > 0 {
				args = append(args, "PX", fmt.Sprintf("%d", ttl))
			}
		}
		rm.executeReplicatedCommand(args)

	case 1: // List
		length, n := readLength(rdbData, pos)
		pos += n

		// Read all list elements
		for i := 0; i < length; i++ {
			element, n, err := readString(rdbData, pos)
			if err != nil {
				return pos, fmt.Errorf("error reading list element: %v", err)
			}
			pos += n

			// Execute RPUSH command
			rm.executeReplicatedCommand([]string{"RPUSH", key, element})
		}

		// Set expiry if needed
		if expiryMs > 0 {
			now := time.Now().UnixNano() / int64(time.Millisecond)
			ttl := expiryMs - now
			if ttl > 0 {
				rm.executeReplicatedCommand([]string{"PEXPIRE", key, fmt.Sprintf("%d", ttl)})
			}
		}

	case 2: // Set
		length, n := readLength(rdbData, pos)
		pos += n

		// Read all set members
		for i := 0; i < length; i++ {
			member, n, err := readString(rdbData, pos)
			if err != nil {
				return pos, fmt.Errorf("error reading set member: %v", err)
			}
			pos += n

			// Execute SADD command
			rm.executeReplicatedCommand([]string{"SADD", key, member})
		}

		// Set expiry if needed
		if expiryMs > 0 {
			now := time.Now().UnixNano() / int64(time.Millisecond)
			ttl := expiryMs - now
			if ttl > 0 {
				rm.executeReplicatedCommand([]string{"PEXPIRE", key, fmt.Sprintf("%d", ttl)})
			}
		}

	case 3: // Sorted Set
		length, n := readLength(rdbData, pos)
		pos += n

		// Read all sorted set members with scores
		for i := 0; i < length; i++ {
			member, n, err := readString(rdbData, pos)
			if err != nil {
				return pos, fmt.Errorf("error reading zset member: %v", err)
			}
			pos += n

			score, n, err := readString(rdbData, pos)
			if err != nil {
				return pos, fmt.Errorf("error reading zset score: %v", err)
			}
			pos += n

			// Execute ZADD command
			rm.executeReplicatedCommand([]string{"ZADD", key, score, member})
		}

		// Set expiry if needed
		if expiryMs > 0 {
			now := time.Now().UnixNano() / int64(time.Millisecond)
			ttl := expiryMs - now
			if ttl > 0 {
				rm.executeReplicatedCommand([]string{"PEXPIRE", key, fmt.Sprintf("%d", ttl)})
			}
		}

	case 4: // Hash
		length, n := readLength(rdbData, pos)
		pos += n

		// Read all hash fields and values
		for i := 0; i < length; i++ {
			field, n, err := readString(rdbData, pos)
			if err != nil {
				return pos, fmt.Errorf("error reading hash field: %v", err)
			}
			pos += n

			value, n, err := readString(rdbData, pos)
			if err != nil {
				return pos, fmt.Errorf("error reading hash value: %v", err)
			}
			pos += n

			// Execute HSET command
			rm.executeReplicatedCommand([]string{"HSET", key, field, value})
		}

		// Set expiry if needed
		if expiryMs > 0 {
			now := time.Now().UnixNano() / int64(time.Millisecond)
			ttl := expiryMs - now
			if ttl > 0 {
				rm.executeReplicatedCommand([]string{"PEXPIRE", key, fmt.Sprintf("%d", ttl)})
			}
		}

	default:
		return pos, fmt.Errorf("unsupported value type: %d", valueType)
	}

	return pos, nil
}

// readLength reads a length-encoded integer from RDB
func readLength(data []byte, pos int) (int, int) {
	if pos >= len(data) {
		return 0, 0
	}

	first := data[pos]
	encType := (first & 0xC0) >> 6

	switch encType {
	case 0: // 6-bit length
		return int(first & 0x3F), 1

	case 1: // 14-bit length
		if pos+1 >= len(data) {
			return 0, 0
		}
		length := (int(first&0x3F) << 8) | int(data[pos+1])
		return length, 2

	case 2: // 32-bit length
		if pos+4 >= len(data) {
			return 0, 0
		}
		length := int(data[pos+1])<<24 | int(data[pos+2])<<16 |
			int(data[pos+3])<<8 | int(data[pos+4])
		return length, 5

	default:
		// Special encoding (not implemented)
		return 0, 0
	}
}

// readString reads a length-prefixed string from RDB
func readString(data []byte, pos int) (string, int, error) {
	length, n := readLength(data, pos)
	pos += n

	if pos+length > len(data) {
		return "", n, fmt.Errorf("string extends beyond data")
	}

	str := string(data[pos : pos+length])
	return str, n + length, nil
}
