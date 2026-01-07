#!/bin/bash
# Example script to run a 3-Sentinel cluster for high availability

# This script demonstrates how to run 3 independent Sentinel processes
# Each Sentinel monitors the same Redis master and can initiate failover

set -e

echo "=== Redis Sentinel Cluster Setup ==="
echo "This will start 3 Sentinel instances monitoring Redis on localhost:6379"
echo ""

# Build the sentinel binary
echo "Building Sentinel binary..."
go build -o bin/redis-sentinel cmd/sentinel/main.go

echo ""
echo "Starting Sentinels..."
echo "Note: Each Sentinel runs in the background"
echo "      Use 'pkill redis-sentinel' to stop all Sentinels"
echo ""

# Start Sentinel 1 on port 26379
echo "[1/3] Starting Sentinel on port 26379..."
./bin/redis-sentinel \
    --port 26379 \
    --master-name mymaster \
    --master-host 127.0.0.1 \
    --master-port 6379 \
    --quorum 2 \
    --sentinel-addrs "127.0.0.1:26380,127.0.0.1:26381" \
    > logs/sentinel-26379.log 2>&1 &

sleep 1

# Start Sentinel 2 on port 26380
echo "[2/3] Starting Sentinel on port 26380..."
./bin/redis-sentinel \
    --port 26380 \
    --master-name mymaster \
    --master-host 127.0.0.1 \
    --master-port 6379 \
    --quorum 2 \
    --sentinel-addrs "127.0.0.1:26379,127.0.0.1:26381" \
    > logs/sentinel-26380.log 2>&1 &

sleep 1

# Start Sentinel 3 on port 26381
echo "[3/3] Starting Sentinel on port 26381..."
./bin/redis-sentinel \
    --port 26381 \
    --master-name mymaster \
    --master-host 127.0.0.1 \
    --master-port 6379 \
    --quorum 2 \
    --sentinel-addrs "127.0.0.1:26379,127.0.0.1:26380" \
    > logs/sentinel-26381.log 2>&1 &

sleep 1

echo ""
echo "=== Sentinel Cluster Started ==="
echo "Sentinel 1: localhost:26379 (log: logs/sentinel-26379.log)"
echo "Sentinel 2: localhost:26380 (log: logs/sentinel-26380.log)"
echo "Sentinel 3: localhost:26381 (log: logs/sentinel-26381.log)"
echo ""
echo "Monitoring: mymaster (127.0.0.1:6379)"
echo "Quorum: 2/3 Sentinels must agree for failover"
echo ""
echo "To view logs:"
echo "  tail -f logs/sentinel-26379.log"
echo ""
echo "To stop all Sentinels:"
echo "  pkill redis-sentinel"
echo ""
echo "To test failover:"
echo "  1. Make sure Redis master is running on port 6379"
echo "  2. Make sure replicas are running on ports 6380, 6381"
echo "  3. Kill the master process"
echo "  4. Watch Sentinels promote a replica to master"
echo "==========================="
