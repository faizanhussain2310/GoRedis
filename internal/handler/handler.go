package handler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"redis/internal/aof"
	"redis/internal/processor"
	"redis/internal/protocol"
	"redis/internal/replication"
	"redis/internal/storage"
)

// CommandFunc is a function type for command handlers
type CommandFunc func(cmd *protocol.Command) []byte

type Client struct {
	ID         int64
	Conn       net.Conn
	Subscriber *storage.Subscriber // Pub/Sub subscriber (nil if not in pub/sub mode)
	InPubSub   bool                // True if client is in pub/sub mode
}

// HandlerConfig holds all handler configuration
type HandlerConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
	Pipeline        PipelineConfig
}

// DefaultHandlerConfig returns default handler configuration
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		Pipeline: PipelineConfig{
			MaxCommands:     1000,
			SlowThreshold:   10 * time.Millisecond,
			CommandTimeout:  5 * time.Second,
			ReadTimeout:     5 * time.Second,
			PipelineTimeout: 1 * time.Millisecond,
		},
	}
}

type CommandHandler struct {
	processor       *processor.Processor
	readBufferSize  int
	writeBufferSize int
	commands        map[string]CommandFunc
	pipelineConfig  PipelineConfig
	slowLog         *SlowLog
	txManager       *TransactionManager
	blockingManager *BlockingManager
	aofWriter       *aof.Writer
	replicationMgr  interface{} // ReplicationManager interface (avoid circular import)
	serverPort      int         // Server's listening port
	onChange        func()      // Callback for tracking changes (for RDB auto-save)
}

func NewCommandHandler(proc *processor.Processor, config HandlerConfig, aofWriter *aof.Writer, replMgr interface{}, serverPort int) *CommandHandler {
	h := &CommandHandler{
		processor:       proc,
		readBufferSize:  config.ReadBufferSize,
		writeBufferSize: config.WriteBufferSize,
		pipelineConfig:  config.Pipeline,
		slowLog:         NewSlowLog(128, config.Pipeline.SlowThreshold),
		txManager:       NewTransactionManager(),
		blockingManager: NewBlockingManager(),
		aofWriter:       aofWriter,
		replicationMgr:  replMgr,
		serverPort:      serverPort,
	}
	h.registerCommands()
	return h
}

// SetChangeCallback sets the callback function to track write operations
// This is used for RDB auto-save to track how many keys have changed
func (h *CommandHandler) SetChangeCallback(callback func()) {
	h.onChange = callback
}

// GetSlowLog returns the slow log for external access
func (h *CommandHandler) GetSlowLog() *SlowLog {
	return h.slowLog
}

// LogToAOF logs a write command to the AOF file
// Called after successful command execution
func (h *CommandHandler) LogToAOF(command string, args []string) {
	if h.aofWriter == nil {
		return
	}

	// Only log write commands
	if !aof.IsWriteCommand(command) {
		return
	}

	// Track change for RDB auto-save
	if h.onChange != nil {
		h.onChange()
	}

	// Build full command args (command + arguments)
	fullArgs := make([]string, 0, len(args)+1)
	fullArgs = append(fullArgs, command)
	fullArgs = append(fullArgs, args...)

	// Write to AOF (errors are logged but don't fail the command)
	if err := h.aofWriter.WriteCommand(fullArgs); err != nil {
		log.Printf("AOF write error: %v", err)
	}
}

// registerCommands initializes the command map with all supported commands
func (h *CommandHandler) registerCommands() {
	h.commands = make(map[string]CommandFunc)

	// String/Basic commands
	h.registerStringCommands()

	// List commands
	h.registerListCommands()

	// Hash commands
	h.registerHashCommands()

	// Set commands
	h.registerSetCommands()

	// Sorted Set commands
	h.registerZSetCommands()

	// Geospatial commands
	h.registerGeoCommands()

	// Bloom Filter commands
	h.registerBloomCommands()

	// Pub/Sub commands
	h.registerPubSubCommands()

	// Transaction commands
	h.registerTransactionCommands()

	// Admin/Debug commands
	h.registerAdminCommands()
}

// registerAdminCommands registers admin and debug commands
func (h *CommandHandler) registerAdminCommands() {
	h.commands["SLOWLOG"] = h.handleSlowLog
	h.commands["BGREWRITEAOF"] = h.handleBGRewriteAOF
	h.commands["BGSAVE"] = h.handleBGSave
	// Note: SENTINEL commands removed - use standalone Sentinel server instead
	// Note: INFO, REPLICAOF, SLAVEOF are handled in replication_handlers.go via pipeline interception
}

// registerTransactionCommands registers transaction commands
func (h *CommandHandler) registerTransactionCommands() {
	h.commands["MULTI"] = h.handleMulti
	h.commands["EXEC"] = h.handleExec
	h.commands["DISCARD"] = h.handleDiscard
	h.commands["WATCH"] = h.handleWatch
	h.commands["UNWATCH"] = h.handleUnwatch
}

// registerStringCommands registers all string/basic commands
func (h *CommandHandler) registerStringCommands() {
	h.commands["PING"] = h.handlePing
	h.commands["ECHO"] = h.handleEcho
	h.commands["SET"] = h.handleSet
	h.commands["SETEX"] = h.handleSetEx
	h.commands["GET"] = h.handleGet
	h.commands["DEL"] = h.handleDel
	h.commands["EXISTS"] = h.handleExists
	h.commands["KEYS"] = h.handleKeys
	h.commands["FLUSHALL"] = h.handleFlushAll
	h.commands["COMMAND"] = h.handleCommand
	h.commands["EXPIRE"] = h.handleExpire
	h.commands["TTL"] = h.handleTTL
}

// registerListCommands registers all list commands
func (h *CommandHandler) registerListCommands() {
	h.commands["LPUSH"] = h.handleLPush
	h.commands["RPUSH"] = h.handleRPush
	h.commands["LPOP"] = h.handleLPop
	h.commands["RPOP"] = h.handleRPop
	h.commands["LLEN"] = h.handleLLen
	h.commands["LRANGE"] = h.handleLRange
	h.commands["LINDEX"] = h.handleLIndex
	h.commands["LSET"] = h.handleLSet
	h.commands["LREM"] = h.handleLRem
	h.commands["LTRIM"] = h.handleLTrim
	h.commands["LINSERT"] = h.handleLInsert
	// Note: Blocking commands (BLPOP, BRPOP, BLMOVE, BRPOPLPUSH) are handled
	// specially in the pipeline, not through the regular command map
}

// registerHashCommands registers all hash commands
func (h *CommandHandler) registerHashCommands() {
	h.commands["HSET"] = h.handleHSet
	h.commands["HGET"] = h.handleHGet
	h.commands["HMGET"] = h.handleHMGet
	h.commands["HDEL"] = h.handleHDel
	h.commands["HEXISTS"] = h.handleHExists
	h.commands["HLEN"] = h.handleHLen
	h.commands["HKEYS"] = h.handleHKeys
	h.commands["HVALS"] = h.handleHVals
	h.commands["HGETALL"] = h.handleHGetAll
	h.commands["HSETNX"] = h.handleHSetNX
	h.commands["HINCRBY"] = h.handleHIncrBy
	h.commands["HINCRBYFLOAT"] = h.handleHIncrByFloat
}

// registerSetCommands registers all set commands
func (h *CommandHandler) registerSetCommands() {
	h.commands["SADD"] = h.handleSAdd
	h.commands["SREM"] = h.handleSRem
	h.commands["SISMEMBER"] = h.handleSIsMember
	h.commands["SMEMBERS"] = h.handleSMembers
	h.commands["SCARD"] = h.handleSCard
	h.commands["SPOP"] = h.handleSPop
	h.commands["SRANDMEMBER"] = h.handleSRandMember
	h.commands["SUNION"] = h.handleSUnion
	h.commands["SINTER"] = h.handleSInter
	h.commands["SDIFF"] = h.handleSDiff
	h.commands["SMOVE"] = h.handleSMove
	h.commands["SUNIONSTORE"] = h.handleSUnionStore
	h.commands["SINTERSTORE"] = h.handleSInterStore
	h.commands["SDIFFSTORE"] = h.handleSDiffStore
}

// registerZSetCommands registers all sorted set commands
func (h *CommandHandler) registerZSetCommands() {
	h.commands["ZADD"] = h.handleZAdd
	h.commands["ZREM"] = h.handleZRem
	h.commands["ZSCORE"] = h.handleZScore
	h.commands["ZRANK"] = h.handleZRank
	h.commands["ZREVRANK"] = h.handleZRevRank
	h.commands["ZCARD"] = h.handleZCard
	h.commands["ZRANGE"] = h.handleZRange
	h.commands["ZREVRANGE"] = h.handleZRevRange
	h.commands["ZRANGEBYSCORE"] = h.handleZRangeByScore
	h.commands["ZREVRANGEBYSCORE"] = h.handleZRevRangeByScore
	h.commands["ZINCRBY"] = h.handleZIncrBy
	h.commands["ZCOUNT"] = h.handleZCount
	h.commands["ZPOPMIN"] = h.handleZPopMin
	h.commands["ZPOPMAX"] = h.handleZPopMax
	h.commands["ZREMRANGEBYSCORE"] = h.handleZRemRangeByScore
	h.commands["ZREMRANGEBYRANK"] = h.handleZRemRangeByRank
}

// registerGeoCommands registers all geospatial commands
func (h *CommandHandler) registerGeoCommands() {
	h.commands["GEOADD"] = h.handleGeoAdd
	h.commands["GEOPOS"] = h.handleGeoPos
	h.commands["GEODIST"] = h.handleGeoDist
	h.commands["GEOHASH"] = h.handleGeoHash
	h.commands["GEORADIUS"] = h.handleGeoRadius
	h.commands["GEORADIUSBYMEMBER"] = h.handleGeoRadiusByMember
}

// registerBloomCommands registers all Bloom filter commands
func (h *CommandHandler) registerBloomCommands() {
	h.commands["BF.RESERVE"] = h.handleBFReserve
	h.commands["BF.ADD"] = h.handleBFAdd
	h.commands["BF.MADD"] = h.handleBFMAdd
	h.commands["BF.EXISTS"] = h.handleBFExists
	h.commands["BF.MEXISTS"] = h.handleBFMExists
	h.commands["BF.INFO"] = h.handleBFInfo
}

// registerPubSubCommands registers all pub/sub commands
func (h *CommandHandler) registerPubSubCommands() {
	h.commands["PUBLISH"] = h.handlePublish
	h.commands["PUBSUB"] = h.handlePubSub
}

func (h *CommandHandler) Handle(ctx context.Context, client *Client) {
	// Use pipeline handler for all connections
	h.HandlePipeline(ctx, client, h.pipelineConfig)
}

// HandleLegacy handles commands one at a time (non-pipelined, kept for reference)
func (h *CommandHandler) HandleLegacy(ctx context.Context, client *Client) {
	reader := bufio.NewReaderSize(client.Conn, h.readBufferSize)
	writer := bufio.NewWriterSize(client.Conn, h.writeBufferSize)

	// Use read timeout from pipeline config, default to 30s
	readTimeout := h.pipelineConfig.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 30 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Set read deadline to prevent blocking forever on idle connections
			client.Conn.SetReadDeadline(time.Now().Add(readTimeout))

			cmd, err := protocol.ParseCommand(reader)
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Printf("Error parsing command: %v", err)
				response := protocol.EncodeError(fmt.Sprintf("ERR %v", err))
				writer.Write(response)
				writer.Flush()
				continue
			}

			// Clear deadline for command execution
			client.Conn.SetReadDeadline(time.Time{})

			response := h.executeCommand(cmd)
			writer.Write(response)
			writer.Flush()
		}
	}
}

func (h *CommandHandler) executeCommand(cmd *protocol.Command) []byte {
	if cmd == nil || len(cmd.Args) == 0 {
		return protocol.EncodeError("ERR empty command")
	}

	command := strings.ToUpper(cmd.Args[0])

	// Check if replica is trying to execute write command
	if h.isReplica() && IsWriteCommand(command) {
		return protocol.EncodeError("READONLY You can't write against a read only replica")
	}

	// Check for replication commands first
	if h.replicationMgr != nil {
		if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
			// Replication commands need special handling (they use the raw connection)
			// These are handled in the pipeline before reaching here
			// But we keep this for potential future use
			_ = replMgr
		}
	}

	if handler, exists := h.commands[command]; exists {
		return handler(cmd)
	}

	return protocol.EncodeError(fmt.Sprintf("ERR unknown command '%s'", command))
}

// ExecuteCommand is an exported wrapper for executeCommand
// Used during AOF replay to execute commands without networking
func (h *CommandHandler) ExecuteCommand(cmd *protocol.Command) []byte {
	return h.executeCommand(cmd)
}

// isReplica checks if server is currently running as a replica
func (h *CommandHandler) isReplica() bool {
	if h.replicationMgr == nil {
		return false
	}
	if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
		return replMgr.GetRole() == replication.RoleReplica
	}
	return false
}

// handleReplicationCommand handles all replication commands through a unified interface
// All replication commands (PING, REPLCONF, PSYNC, INFO, REPLICAOF, SLAVEOF) are handled in replication_handlers.go
// Returns true if the command was handled (and should not be processed further)
func (h *CommandHandler) handleReplicationCommand(conn net.Conn, reader *bufio.Reader, writer *bufio.Writer, cmd *protocol.Command) bool {
	if cmd == nil || len(cmd.Args) == 0 {
		return false
	}

	if h.replicationMgr == nil {
		return false
	}

	replMgr, ok := h.replicationMgr.(*replication.ReplicationManager)
	if !ok {
		return false
	}

	command := strings.ToUpper(cmd.Args[0])
	args := cmd.Args[1:]

	// Route all replication commands to HandleReplicationCommand in replication_handlers.go
	// This includes: PING, REPLCONF, PSYNC, INFO, REPLICAOF, SLAVEOF
	return HandleReplicationCommand(conn, reader, writer, command, args, replMgr)
}
