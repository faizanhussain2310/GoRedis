package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"redis/internal/aof"
	"redis/internal/server"
)

func main() {
	// Parse command-line flags
	port := flag.Int("port", 6379, "Port to listen on")
	host := flag.String("host", "127.0.0.1", "Host to bind to")
	replicationRole := flag.String("replication-role", "master", "Replication role (master/replica)")
	replicationMasterHost := flag.String("replication-master-host", "", "Master host for replica")
	replicationMasterPort := flag.Int("replication-master-port", 6379, "Master port for replica")
	replicaPriority := flag.Int("replica-priority", 100, "Replica priority for failover")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &server.Config{
		Host:            *host,
		Port:            *port,
		MaxConnections:  10000,
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,

		// Pipeline configuration
		MaxPipelineCommands: 1000,
		SlowLogThreshold:    10 * time.Millisecond, // 10 milliseconds
		CommandTimeout:      30 * time.Second,      // 30 seconds
		ReadTimeout:         60 * time.Second,      // 60 seconds
		PipelineTimeout:     1 * time.Second,       // 1 second

		// AOF configuration
		AOF: aof.Config{
			Enabled:    true,
			Filepath:   "appendonly.aof",
			SyncPolicy: aof.SyncEverySecond,
			BufferSize: 4096,
		},

		// RDB configuration
		RDBFilepath: "dump.rdb",
		RDBSavePoint: server.RDBSavePoint{
			Seconds: 60,
			Changes: 1000,
		},

		// Replication defaults
		ReplicaPriority:       *replicaPriority,
		ReplicationRole:       *replicationRole,
		ReplicationMasterHost: *replicationMasterHost,
		ReplicationMasterPort: *replicationMasterPort,

		// Cluster defaults
		ClusterEnabled: false,        // Cluster mode disabled by default
		ClusterConfig:  "nodes.conf", // Default cluster config file
	}

	srv := server.NewRedisServer(cfg)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down server...")
		cancel()
		srv.Shutdown()
	}()

	log.Printf("Starting Redis server on %s:%d", cfg.Host, cfg.Port)
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
