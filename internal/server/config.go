package server

import (
	"time"

	"redis/internal/aof"
)

// RDBSavePoint defines automatic RDB save conditions (Redis-style)
type RDBSavePoint struct {
	Seconds int // Time interval in seconds
	Changes int // Minimum number of key changes
}

type Config struct {
	Host            string
	Port            int
	MaxConnections  int
	ReadBufferSize  int
	WriteBufferSize int

	// Pipeline configuration
	MaxPipelineCommands int           // Max commands in a single pipeline batch
	SlowLogThreshold    time.Duration // Commands slower than this are logged
	CommandTimeout      time.Duration // Max time for a single command before client disconnect
	ReadTimeout         time.Duration // Timeout for reading client data (idle timeout)
	PipelineTimeout     time.Duration // Short timeout for waiting for in-flight pipelined commands

	// AOF (Append-Only File) configuration
	AOF aof.Config

	// RDB (Redis Database) configuration
	RDBFilepath  string       // Path to RDB dump file
	RDBSavePoint RDBSavePoint // Automatic save conditions

	// Replication configuration
	ReplicationRole       string // "master" or "replica"
	ReplicationMasterHost string // Master host (if replica)
	ReplicationMasterPort int    // Master port (if replica)
	ReplicaPriority       int    // Priority for Sentinel failover (0-100, higher = preferred)
}

func DefaultConfig() *Config {
	return &Config{
		Host:            "0.0.0.0",
		Port:            6379,
		MaxConnections:  10000,
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,

		// Pipeline defaults
		MaxPipelineCommands: 1000,
		SlowLogThreshold:    10 * time.Millisecond, // Log commands slower than 10ms
		CommandTimeout:      5 * time.Second,       // Disconnect after 5s for a single command
		ReadTimeout:         5 * time.Second,       // 5 second read timeout for partial commands
		PipelineTimeout:     1 * time.Millisecond,  // Short timeout for waiting for in-flight pipelined commands

		// AOF defaults
		AOF: aof.DefaultConfig(),

		// RDB defaults (Redis-style: save after 60 seconds if 1000 keys changed)
		RDBFilepath: "dump.rdb",
		RDBSavePoint: RDBSavePoint{
			Seconds: 60,
			Changes: 1000,
		},

		// Replication defaults
		ReplicaPriority: 100,      // Default priority for failover
		ReplicationRole: "master", // Default role is master
	}
}
