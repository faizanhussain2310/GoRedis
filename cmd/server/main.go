package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"redis/internal/aof"
	"redis/internal/server"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &server.Config{
		Host:            "127.0.0.1",
		Port:            6379,
		MaxConnections:  10000,
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,

		// Pipeline configuration
		MaxPipelineCommands: 1000,
		SlowLogThreshold:    10 * time.Millisecond, // 10 milliseconds
		CommandTimeout:      5 * time.Second,       // 5 seconds
		ReadTimeout:         5 * time.Second,       // 5 seconds
		PipelineTimeout:     1 * time.Millisecond,  // 1 millisecond

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
		ReplicaPriority: 100,      // Default priority for failover
		ReplicationRole: "master", // Default role is master
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
