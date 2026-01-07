package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"redis/internal/server"
)

func main() {
	// Command-line flags for Sentinel configuration
	port := flag.Int("port", 26379, "Port for Sentinel to listen on")
	masterName := flag.String("master-name", "mymaster", "Name of the master to monitor")
	masterHost := flag.String("master-host", "127.0.0.1", "Host of the master to monitor")
	masterPort := flag.Int("master-port", 6379, "Port of the master to monitor")
	quorum := flag.Int("quorum", 2, "Number of Sentinels that need to agree")
	downAfter := flag.Int("down-after-ms", 30000, "Milliseconds before marking instance down")
	failoverTimeout := flag.Int("failover-timeout-ms", 180000, "Milliseconds for failover timeout")
	sentinelAddrs := flag.String("sentinel-addrs", "", "Comma-separated list of other Sentinel addresses (e.g., 'host1:26379,host2:26379')")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Parse Sentinel addresses
	var addrs []string
	if *sentinelAddrs != "" {
		addrs = strings.Split(*sentinelAddrs, ",")
		for i, addr := range addrs {
			addrs[i] = strings.TrimSpace(addr)
		}
	}

	// Create Sentinel configuration
	cfg := &server.SentinelConfig{
		Host:            "0.0.0.0",
		Port:            *port,
		MasterName:      *masterName,
		MasterHost:      *masterHost,
		MasterPort:      *masterPort,
		SentinelAddrs:   addrs,
		Quorum:          *quorum,
		DownAfterMillis: *downAfter,
		FailoverTimeout: *failoverTimeout,
		MaxConnections:  10000,
	}

	log.Printf("Starting Sentinel on port %d", *port)
	log.Printf("Monitoring master '%s' at %s:%d", *masterName, *masterHost, *masterPort)
	log.Printf("Quorum: %d, Down-after: %dms, Failover-timeout: %dms", *quorum, *downAfter, *failoverTimeout)
	if len(addrs) > 0 {
		log.Printf("Other Sentinels: %v", addrs)
		log.Printf("Total Sentinels in cluster: %d (including this one)", len(addrs)+1)
	} else {
		log.Printf("Warning: No other Sentinels configured (standalone mode)")
	}

	srv := server.NewSentinelServer(cfg)

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down Sentinel...")
		cancel()
		srv.Shutdown()
	}()

	// Start the Sentinel server
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("Sentinel failed: %v", err)
	}
}

func printUsage() {
	fmt.Println("Redis Sentinel - High Availability Monitoring")
	fmt.Println("\nUsage:")
	fmt.Println("  sentinel [options]")
	fmt.Println("\nOptions:")
	flag.PrintDefaults()
	fmt.Println("\nExample:")
	fmt.Println("  # Sentinel 1 (on machine A)")
	fmt.Println("  ./sentinel --port 26379 --master-name mymaster --master-host 127.0.0.1 --master-port 6379 \\")
	fmt.Println("    --quorum 2 --sentinel-addrs \"127.0.0.1:26380,127.0.0.1:26381\"")
	fmt.Println("\n  # Sentinel 2 (on machine B)")
	fmt.Println("  ./sentinel --port 26380 --master-name mymaster --master-host 127.0.0.1 --master-port 6379 \\")
	fmt.Println("    --quorum 2 --sentinel-addrs \"127.0.0.1:26379,127.0.0.1:26381\"")
	fmt.Println("\n  # Sentinel 3 (on machine C)")
	fmt.Println("  ./sentinel --port 26381 --master-name mymaster --master-host 127.0.0.1 --master-port 6379 \\")
	fmt.Println("    --quorum 2 --sentinel-addrs \"127.0.0.1:26379,127.0.0.1:26380\"")
}
