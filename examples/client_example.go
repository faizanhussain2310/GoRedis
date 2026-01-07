package main

import (
	"fmt"
	"log"
	"time"

	"redis/pkg/client"
)

func main() {
	// Create Sentinel-aware client
	redisClient, err := client.NewSentinelClient(client.SentinelOptions{
		SentinelAddrs:            []string{"127.0.0.1:26379", "127.0.0.1:26380"},
		MasterName:               "mymaster",
		RequireStrongConsistency: false,
		HealthCheckInterval:      5 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer redisClient.Close()

	// Write operations (automatically go to master)
	fmt.Println("Writing data...")
	if err := redisClient.Set("user:1", "Alice"); err != nil {
		log.Printf("Write failed: %v", err)
	}
	if err := redisClient.Set("user:2", "Bob"); err != nil {
		log.Printf("Write failed: %v", err)
	}

	// Read operations (automatically distributed to replicas)
	fmt.Println("Reading data...")
	user1, err := redisClient.Get("user:1")
	if err != nil {
		log.Printf("Read failed: %v", err)
	} else {
		fmt.Printf("User 1: %s\n", user1)
	}

	user2, err := redisClient.Get("user:2")
	if err != nil {
		log.Printf("Read failed: %v", err)
	} else {
		fmt.Printf("User 2: %s\n", user2)
	}

	// Client automatically handles failover!
	// If master fails, it queries Sentinel and reconnects to new master
	fmt.Println("Client is running with automatic failover protection!")

	// Keep running to demonstrate health checks
	time.Sleep(30 * time.Second)
}
