package handler

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc64"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"redis/internal/protocol"
	"redis/internal/replication"
	"redis/internal/storage"
)

// ==================== REPLICATION COMMAND HANDLERS ====================
// All replication-related commands are handled in this file:
// - PING: Used during replication handshake
// - REPLCONF: Replication configuration (listening-port, capa, getack, ack)
// - PSYNC: Full/partial synchronization (streams RDB over raw connection)
// - INFO: Display server and replication information
// - REPLICAOF/SLAVEOF: Make this server a replica of another master
//
// These handlers use bufio.Writer for direct RESP encoding and have access
// to the raw net.Conn when needed (e.g., PSYNC for RDB streaming).
// =====================================================================

// handlePing handles PING command (used in replication handshake)
func handlePing(writer *bufio.Writer, args []string) {
	if len(args) > 1 {
		writeError(writer, "ERR wrong number of arguments for 'ping' command")
		return
	}

	if len(args) == 1 {
		writeBulkString(writer, args[0])
	} else {
		writeSimpleString(writer, "PONG")
	}
}

// handleReplConf handles REPLCONF command (replication configuration)
func handleReplConf(conn net.Conn, writer *bufio.Writer, args []string, rm *replication.ReplicationManager, handler interface{}) {
	if len(args) < 2 {
		writeError(writer, "ERR wrong number of arguments for 'replconf' command")
		return
	}

	option := strings.ToLower(args[0])

	switch option {
	case "listening-port":
		// Replica is telling us its listening port
		port, err := strconv.Atoi(args[1])
		if err != nil {
			writeError(writer, "ERR invalid port")
			return
		}

		log.Printf("[REPLICATION] Replica listening on port %d", port)

		// Store temporarily - will be applied when replica is added during PSYNC
		if h, ok := handler.(*CommandHandler); ok {
			h.pendingPortsMu.Lock()
			h.pendingPorts[conn.RemoteAddr().String()] = port
			h.pendingPortsMu.Unlock()
			log.Printf("[REPLICATION] Stored pending port %d for %s", port, conn.RemoteAddr().String())
		}

		writeSimpleString(writer, "OK")

	case "capa":
		// Replica is telling us its capabilities
		capability := args[1]
		log.Printf("[REPLICATION] Replica capability: %s", capability)

		writeSimpleString(writer, "OK")

	case "getack":
		// Master is requesting acknowledgment with current offset
		// TODO: Send current replication offset
		writeArray(writer, []string{"REPLCONF", "ACK", "0"})

	case "ack":
		// Replica is acknowledging receipt up to offset
		if len(args) < 2 {
			writeError(writer, "ERR wrong number of arguments")
			return
		}

		offset, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			writeError(writer, "ERR invalid offset")
			return
		}

		// Find replica by connection address and update offset
		if replica, ok := rm.GetReplicaByAddr(conn.RemoteAddr().String()); ok {
			rm.UpdateReplicaOffset(replica.ID, offset)
			log.Printf("[REPLICATION] Replica %s ACK offset: %d", replica.ID, offset)
		}

		// Note: Master doesn't send a response to REPLCONF ACK (it's one-way)

	default:
		writeError(writer, fmt.Sprintf("ERR unknown REPLCONF option '%s'", option))
	}
}

// handlePSync handles PSYNC command (partial/full synchronization)
func handlePSync(conn net.Conn, writer *bufio.Writer, args []string, rm *replication.ReplicationManager, handler interface{}) {
	if len(args) != 2 {
		writeError(writer, "ERR wrong number of arguments for 'psync' command")
		return
	}

	requestedReplID := args[0]
	requestedOffset := args[1]

	log.Printf("[REPLICATION] PSYNC requested: replid=%s offset=%s", requestedReplID, requestedOffset)

	info := rm.GetInfo()
	replID := info["master_repl_id"].(string)
	offset := info["master_repl_offset"].(int64)

	// Try partial resync if replication ID matches
	if requestedReplID != "?" && requestedReplID == replID {
		// Parse requested offset
		reqOffset, err := strconv.ParseInt(requestedOffset, 10, 64)
		if err == nil {
			// Try to get data from backlog
			backlogData, ok := rm.GetBacklogData(reqOffset)
			if ok {
				// Partial resync possible
				response := "+CONTINUE\r\n"
				writer.WriteString(response)
				writer.Flush()

				log.Printf("[REPLICATION] Partial resync: sending %d bytes from offset %d", len(backlogData), reqOffset)

				// Send backlog data
				writer.Write(backlogData)
				writer.Flush()

				// Generate replica ID and add to manager
				replicaID := fmt.Sprintf("replica-%s", conn.RemoteAddr().String())
				replica := rm.AddReplica(conn, replicaID)
				replica.State = replication.ReplicaStateOnline
				replica.Offset = offset

				log.Printf("[REPLICATION] Partial resync complete")
				return
			}
			log.Printf("[REPLICATION] Offset %d not in backlog, falling back to full resync", reqOffset)
		}
	}

	// Full resync

	// Send FULLRESYNC response
	response := fmt.Sprintf("+FULLRESYNC %s %d\r\n", replID, offset)
	writer.WriteString(response)
	writer.Flush()

	log.Printf("[REPLICATION] Sent FULLRESYNC response: replid=%s offset=%d", replID, offset)

	// Generate replica ID
	replicaID := fmt.Sprintf("replica-%s", conn.RemoteAddr().String())

	// Add replica to replication manager
	replica := rm.AddReplica(conn, replicaID)

	// Apply pending listening port if available
	if h, ok := handler.(*CommandHandler); ok {
		h.pendingPortsMu.Lock()
		if port, exists := h.pendingPorts[conn.RemoteAddr().String()]; exists {
			rm.SetReplicaListeningPort(replica.ID, port)
			delete(h.pendingPorts, conn.RemoteAddr().String())
			log.Printf("[REPLICATION] Applied pending port %d to replica %s", port, replica.ID)
		}
		h.pendingPortsMu.Unlock()
	}

	// Send RDB snapshot with actual data
	rdbData := generateRDB(rm)
	writer.WriteString(fmt.Sprintf("$%d\r\n", len(rdbData)))
	writer.Write(rdbData)
	writer.Flush()

	log.Printf("[REPLICATION] Sent RDB snapshot (%d bytes)", len(rdbData))

	// Mark replica as online
	replica.State = replication.ReplicaStateOnline

	// Keep connection alive for replication stream
	// The client's read loop will handle incoming REPLCONF ACK commands
}

// handleInfo handles INFO command with replication section
func handleInfo(writer *bufio.Writer, args []string, rm *replication.ReplicationManager) {
	section := "all"
	if len(args) > 0 {
		section = strings.ToLower(args[0])
	}

	var response strings.Builder

	// Replication section
	if section == "all" || section == "replication" {
		info := rm.GetInfo()

		response.WriteString("# Replication\r\n")
		response.WriteString(fmt.Sprintf("role:%s\r\n", info["role"]))

		if info["role"] == "master" {
			response.WriteString(fmt.Sprintf("connected_slaves:%d\r\n", info["connected_slaves"]))

			// List each slave
			if slaves, ok := info["slaves"].([]map[string]interface{}); ok {
				for i, slave := range slaves {
					response.WriteString(fmt.Sprintf("slave%d:ip=%s,port=%d,state=%s,offset=%d\r\n",
						i,
						slave["ip"],
						slave["port"],
						slave["state"],
						slave["offset"]))
				}
			}

			response.WriteString(fmt.Sprintf("master_repl_offset:%d\r\n", info["master_repl_offset"]))
			response.WriteString(fmt.Sprintf("repl_backlog_size:%d\r\n", info["repl_backlog_size"]))
		} else if info["role"] == "slave" {
			response.WriteString(fmt.Sprintf("master_host:%s\r\n", info["master_host"]))
			response.WriteString(fmt.Sprintf("master_port:%d\r\n", info["master_port"]))
			response.WriteString(fmt.Sprintf("master_link_status:%s\r\n", info["master_link_status"]))
			response.WriteString(fmt.Sprintf("slave_repl_offset:%d\r\n", info["slave_repl_offset"]))
			if replid, ok := info["master_replid"].(string); ok && replid != "" {
				response.WriteString(fmt.Sprintf("master_replid:%s\r\n", replid))
			}
		}
	}

	writeBulkString(writer, response.String())
}

// handleReplicaOf handles REPLICAOF/SLAVEOF command
func handleReplicaOf(writer *bufio.Writer, args []string, rm *replication.ReplicationManager) {
	if len(args) != 2 {
		writeError(writer, "ERR wrong number of arguments for 'replicaof' command")
		return
	}

	host := args[0]
	portStr := args[1]

	// Handle "REPLICAOF NO ONE" to become master
	if strings.ToUpper(host) == "NO" && strings.ToUpper(portStr) == "ONE" {
		rm.DisconnectFromMaster()
		writeSimpleString(writer, "OK")
		log.Printf("[REPLICATION] Became master (disconnected from replication)")
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		writeError(writer, "ERR invalid port number")
		return
	}

	// Connect to master
	err = rm.ConnectToMaster(host, port)
	if err != nil {
		writeError(writer, fmt.Sprintf("ERR failed to connect to master: %v", err))
		return
	}

	writeSimpleString(writer, "OK")
	log.Printf("[REPLICATION] Started replication from %s:%d", host, port)
}

// generateRDB generates an RDB file with actual database content
func generateRDB(rm *replication.ReplicationManager) []byte {
	buf := bytes.NewBuffer(nil)

	// Magic string "REDIS" + version "0009"
	buf.WriteString("REDIS0009")

	// Get store snapshot
	storeSnapshot := rm.GetStoreSnapshot()
	if storeSnapshot == nil {
		// No store available, return empty RDB
		return generateEmptyRDB()
	}

	// Type assert to storage.Store
	var data map[string]*storage.Value
	switch s := storeSnapshot.(type) {
	case *storage.Store:
		data = s.GetAllData()
		defer s.ReleaseSnapshot() // Release snapshot when done
	case map[string]*storage.Value:
		data = s
	default:
		// Unknown type, return empty RDB
		log.Printf("[REPLICATION] Unknown store type, generating empty RDB")
		return generateEmptyRDB()
	}

	// If no data, return empty RDB
	if len(data) == 0 {
		return generateEmptyRDB()
	}

	// Database selector (DB 0)
	buf.WriteByte(0xFE) // RDB_OPCODE_SELECTDB
	buf.WriteByte(0)    // Database number 0

	// Resize DB opcode (optional, for efficiency)
	buf.WriteByte(0xFB) // RDB_OPCODE_RESIZEDB
	writeLength(buf, len(data))
	writeLength(buf, 0) // Expires hash table size

	// Write all key-value pairs
	for key, value := range data {
		// Check if value has expiry
		if value.ExpiresAt != nil && value.ExpiresAt.After(time.Now()) {
			// Write expiry in milliseconds
			buf.WriteByte(0xFC) // RDB_OPCODE_EXPIRETIME_MS
			expiryMs := value.ExpiresAt.UnixNano() / int64(time.Millisecond)
			binary.Write(buf, binary.LittleEndian, uint64(expiryMs))
		}

		// Write value based on type
		switch value.Type {
		case storage.StringType:
			// String type
			buf.WriteByte(0) // RDB_TYPE_STRING
			writeString(buf, key)
			if str, ok := value.Data.(string); ok {
				writeString(buf, str)
			} else {
				writeString(buf, fmt.Sprintf("%v", value.Data))
			}

		case storage.ListType:
			// List type
			buf.WriteByte(1) // RDB_TYPE_LIST
			writeString(buf, key)
			if list, ok := value.Data.([]string); ok {
				writeLength(buf, len(list))
				for _, item := range list {
					writeString(buf, item)
				}
			}

		case storage.SetType:
			// Set type
			buf.WriteByte(2) // RDB_TYPE_SET
			writeString(buf, key)
			if set, ok := value.Data.(map[string]struct{}); ok {
				writeLength(buf, len(set))
				for member := range set {
					writeString(buf, member)
				}
			}

		case storage.HashType:
			// Hash type
			buf.WriteByte(4) // RDB_TYPE_HASH
			writeString(buf, key)
			if hash, ok := value.Data.(map[string]string); ok {
				writeLength(buf, len(hash))
				for field, val := range hash {
					writeString(buf, field)
					writeString(buf, val)
				}
			}

		case storage.ZSetType:
			// Sorted set type
			buf.WriteByte(3) // RDB_TYPE_ZSET
			writeString(buf, key)
			// For ZSet, we need special handling as it has scores
			// Simplified: just write count as 0 for now
			writeLength(buf, 0)

		default:
			// Unknown type, skip
			log.Printf("[REPLICATION] Skipping unknown type for key %s: %v", key, value.Type)
			continue
		}
	}

	// EOF opcode
	buf.WriteByte(0xFF)

	// Calculate CRC64 checksum of everything before EOF
	rdbData := buf.Bytes()
	checksum := calculateCRC64(rdbData[:len(rdbData)-1]) // Exclude EOF byte from checksum

	// Write CRC64 checksum (8 bytes, little-endian)
	checksumBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(checksumBytes, checksum)
	buf.Write(checksumBytes)

	return buf.Bytes()
}

// calculateCRC64 calculates CRC64 checksum using Redis CRC64 variant
func calculateCRC64(data []byte) uint64 {
	// Use ECMA CRC64 table (same as Redis)
	table := crc64.MakeTable(crc64.ECMA)
	return crc64.Checksum(data, table)
}

// writeLength writes a length-encoded integer to RDB
func writeLength(buf *bytes.Buffer, length int) {
	if length < 64 {
		// 6-bit encoding: (length)
		buf.WriteByte(byte(length))
	} else if length < 16384 {
		// 14-bit encoding: (01|length)
		buf.WriteByte(byte(0x40 | (length >> 8)))
		buf.WriteByte(byte(length & 0xFF))
	} else {
		// 32-bit encoding: (10|0x00|length)
		buf.WriteByte(0x80)
		binary.Write(buf, binary.BigEndian, uint32(length))
	}
}

// writeString writes a length-prefixed string to RDB
func writeString(buf *bytes.Buffer, s string) {
	writeLength(buf, len(s))
	buf.WriteString(s)
}

// generateEmptyRDB generates an empty RDB file
// This is a minimal RDB file that represents an empty database
func generateEmptyRDB() []byte {
	// Redis RDB file format:
	// REDIS<version><database><EOF><checksum>

	// Magic string "REDIS" + version "0009"
	rdb := []byte("REDIS0009")

	// EOF opcode
	rdb = append(rdb, 0xFF)

	// CRC64 checksum (8 bytes) - using zeros for simplicity
	rdb = append(rdb, 0, 0, 0, 0, 0, 0, 0, 0)

	return rdb
}

// Helper functions for writing RESP responses

func writeArray(writer *bufio.Writer, elements []string) {
	resp := protocol.EncodeArray(elements)
	writer.Write(resp)
	writer.Flush()
}

func writeSimpleString(writer *bufio.Writer, s string) {
	writer.WriteString(fmt.Sprintf("+%s\r\n", s))
	writer.Flush()
}

func writeBulkString(writer *bufio.Writer, s string) {
	writer.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(s), s))
	writer.Flush()
}

func writeError(writer *bufio.Writer, err string) {
	writer.WriteString(fmt.Sprintf("-%s\r\n", err))
	writer.Flush()
}

func writeInteger(writer *bufio.Writer, n int64) {
	writer.WriteString(fmt.Sprintf(":%d\r\n", n))
	writer.Flush()
}

// HandleReplicationCommand routes all replication commands
// This is the single entry point for all replication-related commands
// Returns true if the command was handled
func HandleReplicationCommand(conn net.Conn, reader *bufio.Reader, writer *bufio.Writer,
	cmd string, args []string, rm *replication.ReplicationManager, handler interface{}) bool {

	switch strings.ToUpper(cmd) {
	case "PING":
		// PING during replication handshake
		handlePing(writer, args)
		return true

	case "REPLCONF":
		// Replication configuration (listening-port, capa, getack, ack)
		handleReplConf(conn, writer, args, rm, handler)
		return true

	case "PSYNC":
		// Full/partial synchronization (needs raw connection for RDB streaming)
		handlePSync(conn, writer, args, rm, handler)
		return true

	case "INFO":
		// Display server and replication information
		handleInfo(writer, args, rm)
		return true

	case "REPLICAOF", "SLAVEOF":
		// Make this server a replica of another master
		handleReplicaOf(writer, args, rm)
		return true

	default:
		// Not a replication command
		return false
	}
}
