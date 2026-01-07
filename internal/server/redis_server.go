package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"redis/internal/aof"
	"redis/internal/handler"
	"redis/internal/processor"
	"redis/internal/protocol"
	"redis/internal/replication"
	"redis/internal/storage"
)

// RedisServer handles Redis protocol and data operations
type RedisServer struct {
	config          *Config
	listener        net.Listener
	processor       *processor.Processor
	handler         *handler.CommandHandler
	aofWriter       *aof.Writer
	replicationMgr  *replication.ReplicationManager
	connections     sync.Map
	connIDCounter   atomic.Int64
	activeConnCount atomic.Int64
	wg              sync.WaitGroup
	shutdownChan    chan struct{}
	mu              sync.RWMutex
	isShutdown      bool

	// RDB background save tracking
	changesSinceLastSave atomic.Int64
	lastSaveTime         time.Time
	saveMu               sync.Mutex
	rdbTicker            *time.Ticker
	rdbStopChan          chan struct{}
}

// NewRedisServer creates a new Redis server instance
func NewRedisServer(cfg *Config) *RedisServer {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	store := storage.NewStore()
	proc := processor.NewProcessor(store)

	// Create AOF writer
	var aofWriter *aof.Writer
	var err error
	if cfg.AOF.Enabled {
		aofWriter, err = aof.NewWriter(cfg.AOF)
		if err != nil {
			log.Printf("Warning: Failed to create AOF writer: %v", err)
			log.Printf("Continuing without AOF persistence")
			aofWriter = nil
		} else {
			log.Printf("AOF enabled: %s (sync: %s)", cfg.AOF.Filepath, syncPolicyName(cfg.AOF.SyncPolicy))
		}
	}

	// Initialize replication manager
	var replRole replication.Role
	if cfg.ReplicationRole == "replica" || cfg.ReplicationRole == "slave" {
		replRole = replication.RoleReplica
	} else {
		replRole = replication.RoleMaster
	}
	replMgr := replication.NewReplicationManager(replRole)
	log.Printf("Replication mode: %s", replRole)

	// Set replica priority from config
	if replRole == replication.RoleReplica {
		replMgr.SetPriority(cfg.ReplicaPriority)
		log.Printf("Replica priority set to: %d", cfg.ReplicaPriority)
	}

	// Set store getter for RDB generation
	replMgr.SetStoreGetter(func() interface{} {
		return proc.GetStore()
	})

	// Build handler config from server config
	handlerConfig := handler.HandlerConfig{
		ReadBufferSize:  cfg.ReadBufferSize,
		WriteBufferSize: cfg.WriteBufferSize,
		Pipeline: handler.PipelineConfig{
			MaxCommands:     cfg.MaxPipelineCommands,
			SlowThreshold:   cfg.SlowLogThreshold,
			CommandTimeout:  cfg.CommandTimeout,
			ReadTimeout:     cfg.ReadTimeout,
			PipelineTimeout: cfg.PipelineTimeout,
		},
	}
	cmdHandler := handler.NewCommandHandler(proc, handlerConfig, aofWriter, replMgr, cfg.Port)

	s := &RedisServer{
		config:         cfg,
		processor:      proc,
		handler:        cmdHandler,
		aofWriter:      aofWriter,
		replicationMgr: replMgr,
		shutdownChan:   make(chan struct{}),
		lastSaveTime:   time.Now(),
		rdbStopChan:    make(chan struct{}),
	}

	// Set change callback for RDB auto-save tracking
	cmdHandler.SetChangeCallback(func() {
		s.IncrementChanges()
	})

	// Set command executor for replica (to execute commands received from master)
	if replRole == replication.RoleReplica {
		replMgr.SetCommandExecutor(func(args []string) error {
			cmd := &protocol.Command{Args: args}
			response := cmdHandler.ExecuteCommand(cmd)
			// Check if response is an error
			if len(response) > 0 && response[0] == '-' {
				return fmt.Errorf("command failed: %s", string(response))
			}
			return nil
		})
	}

	// Set listening port for replication
	replMgr.SetListeningPort(cfg.Port)

	// Load persistence files (AOF takes priority, fallback to RDB)
	if cfg.AOF.Enabled {
		if err := s.loadAOF(); err != nil {
			log.Printf("Warning: Failed to load AOF: %v", err)
			// Try RDB as fallback
			if err := s.loadRDB(); err != nil {
				log.Printf("Warning: Failed to load RDB: %v", err)
				log.Printf("Starting with empty database")
			} else {
				log.Printf("Loaded data from RDB file")
			}
		}
	} else {
		// AOF disabled, try loading from RDB
		if err := s.loadRDB(); err != nil {
			log.Printf("Warning: Failed to load RDB: %v", err)
			log.Printf("Starting with empty database")
		}
	}

	// Start background RDB auto-save
	if cfg.RDBSavePoint.Seconds > 0 && cfg.RDBSavePoint.Changes > 0 {
		s.startBackgroundRDBSave()
	}

	// Connect to master if this is a replica
	if cfg.ReplicationRole == "replica" || cfg.ReplicationRole == "slave" {
		if cfg.ReplicationMasterHost != "" && cfg.ReplicationMasterPort > 0 {
			log.Printf("Connecting to master %s:%d...", cfg.ReplicationMasterHost, cfg.ReplicationMasterPort)
			if err := replMgr.ConnectToMaster(cfg.ReplicationMasterHost, cfg.ReplicationMasterPort); err != nil {
				log.Printf("Warning: Failed to connect to master: %v", err)
				log.Printf("Will continue as disconnected replica")
			} else {
				log.Printf("Successfully initiated connection to master")
			}
		}
	}

	return s
}

// syncPolicyName returns a human-readable name for the sync policy
func syncPolicyName(policy aof.SyncPolicy) string {
	switch policy {
	case aof.SyncAlways:
		return "always"
	case aof.SyncEverySecond:
		return "everysec"
	case aof.SyncNo:
		return "no"
	default:
		return "unknown"
	}
}

// loadAOF loads and replays commands from the AOF file
func (s *RedisServer) loadAOF() error {
	startTime := time.Now()

	reader, err := aof.NewReader(s.config.AOF.Filepath)
	if err != nil {
		return fmt.Errorf("failed to create AOF reader: %w", err)
	}
	if reader == nil {
		// File doesn't exist - first startup
		log.Println("No AOF file found, starting with empty database")
		return nil
	}
	defer reader.Close()

	log.Printf("Loading AOF file: %s", s.config.AOF.Filepath)

	// Load all commands from AOF file
	commands, err := reader.LoadAll()
	if err != nil {
		return fmt.Errorf("failed to load AOF commands: %w", err)
	}

	// Replay all commands
	errorCount := 0
	for _, cmd := range commands {
		if err := s.executeCommand(cmd); err != nil {
			log.Printf("AOF replay error for command %v: %v", cmd, err)
			errorCount++
			// Continue loading despite errors
		}
	}

	duration := time.Since(startTime)
	log.Printf("AOF loaded: %d commands replayed in %v", len(commands), duration)
	if errorCount > 0 {
		log.Printf("Warning: %d errors during AOF replay", errorCount)
	}

	return nil
}

// executeCommand executes a single command during AOF replay
func (s *RedisServer) executeCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}

	// Convert args to protocol.Command format
	cmd := &protocol.Command{Args: args}

	// Execute through handler
	response := s.handler.ExecuteCommand(cmd)

	// Check if result indicates an error
	if len(response) > 0 && response[0] == '-' {
		return fmt.Errorf("command failed: %s", string(response))
	}

	return nil
}

// Start starts the Redis server
func (s *RedisServer) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	s.listener = listener
	log.Printf("Redis server listening on %s", addr)

	go s.acceptConnections(ctx)

	<-ctx.Done()
	return nil
}

func (s *RedisServer) acceptConnections(ctx context.Context) {
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

func (s *RedisServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()

	connID := s.connIDCounter.Add(1)
	s.activeConnCount.Add(1)
	defer s.activeConnCount.Add(-1)

	s.connections.Store(connID, conn)
	defer s.connections.Delete(connID)
	defer conn.Close()

	log.Printf("New connection [%d] from %s", connID, conn.RemoteAddr())

	client := &handler.Client{
		ID:   connID,
		Conn: conn,
	}

	s.handler.Handle(ctx, client)

	log.Printf("Connection [%d] closed", connID)
}

// Shutdown gracefully shuts down the server
func (s *RedisServer) Shutdown() {
	s.mu.Lock()
	if s.isShutdown {
		s.mu.Unlock()
		return
	}
	s.isShutdown = true
	s.mu.Unlock()

	log.Println("Initiating graceful shutdown...")

	// Stop RDB auto-save ticker
	if s.rdbTicker != nil {
		s.rdbTicker.Stop()
		close(s.rdbStopChan)
	}

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
		log.Println("All connections closed gracefully")
	case <-time.After(5 * time.Second):
		log.Println("Shutdown timeout reached, forcing exit")
	}

	// Close AOF writer
	if s.aofWriter != nil {
		log.Println("Closing AOF writer...")
		if err := s.aofWriter.Close(); err != nil {
			log.Printf("Error closing AOF writer: %v", err)
		} else {
			log.Println("AOF writer closed successfully")
		}
	}

	if s.processor != nil {
		s.processor.Shutdown()
	}

	if s.replicationMgr != nil {
		s.replicationMgr.Shutdown()
	}

	log.Println("Redis server shutdown complete")
}
