#!/bin/bash

# Test script for Sentinel functionality
# This script demonstrates how to:
# 1. Start Redis master and replicas
# 2. Start multiple Sentinel instances
# 3. Test Sentinel commands

set -e

echo "========================================="
echo "Redis Sentinel Test Suite"
echo "========================================="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to test if port is open
wait_for_port() {
    local port=$1
    local max_attempts=30
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if nc -z localhost $port 2>/dev/null; then
            echo -e "${GREEN}✓${NC} Port $port is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    
    echo -e "${RED}✗${NC} Port $port failed to start"
    return 1
}

# Function to test Sentinel command
test_sentinel_command() {
    local port=$1
    local command=$2
    echo -e "${YELLOW}Testing:${NC} $command on port $port"
    echo -e "$command" | nc localhost $port || true
    echo ""
}

echo "Step 1: Starting Redis Master (port 6379)"
echo "==========================================="
./bin/redis-server --port 6379 --replication-role master > logs/master.log 2>&1 &
MASTER_PID=$!
echo "Master PID: $MASTER_PID"
wait_for_port 6379
echo ""

echo "Step 2: Starting Redis Replica 1 (port 6380)"
echo "==========================================="
./bin/redis-server --port 6380 \
    --replication-role replica \
    --replication-master-host 127.0.0.1 \
    --replication-master-port 6379 \
    --replica-priority 100 > logs/replica1.log 2>&1 &
REPLICA1_PID=$!
echo "Replica 1 PID: $REPLICA1_PID"
wait_for_port 6380
echo ""

echo "Step 3: Starting Redis Replica 2 (port 6381)"
echo "==========================================="
./bin/redis-server --port 6381 \
    --replication-role replica \
    --replication-master-host 127.0.0.1 \
    --replication-master-port 6379 \
    --replica-priority 50 > logs/replica2.log 2>&1 &
REPLICA2_PID=$!
echo "Replica 2 PID: $REPLICA2_PID"
wait_for_port 6381
echo ""

sleep 2

echo "Step 4: Starting Sentinel 1 (port 26379)"
echo "==========================================="
./bin/redis-sentinel \
    --port 26379 \
    --master-name mymaster \
    --master-host 127.0.0.1 \
    --master-port 6379 \
    --quorum 2 \
    --sentinel-addrs "127.0.0.1:26380,127.0.0.1:26381" > logs/sentinel1.log 2>&1 &
SENTINEL1_PID=$!
echo "Sentinel 1 PID: $SENTINEL1_PID"
wait_for_port 26379
echo ""

echo "Step 5: Starting Sentinel 2 (port 26380)"
echo "==========================================="
./bin/redis-sentinel \
    --port 26380 \
    --master-name mymaster \
    --master-host 127.0.0.1 \
    --master-port 6379 \
    --quorum 2 \
    --sentinel-addrs "127.0.0.1:26379,127.0.0.1:26381" > logs/sentinel2.log 2>&1 &
SENTINEL2_PID=$!
echo "Sentinel 2 PID: $SENTINEL2_PID"
wait_for_port 26380
echo ""

echo "Step 6: Starting Sentinel 3 (port 26381)"
echo "==========================================="
./bin/redis-sentinel \
    --port 26381 \
    --master-name mymaster \
    --master-host 127.0.0.1 \
    --master-port 6379 \
    --quorum 2 \
    --sentinel-addrs "127.0.0.1:26379,127.0.0.1:26380" > logs/sentinel3.log 2>&1 &
SENTINEL3_PID=$!
echo "Sentinel 3 PID: $SENTINEL3_PID"
wait_for_port 26381
echo ""

sleep 3

echo "========================================="
echo "Testing Sentinel Commands"
echo "========================================="
echo ""

# Test PING
echo -e "${GREEN}Test 1: PING${NC}"
test_sentinel_command 26379 "*1\r\n\$4\r\nPING\r\n"

# Test SENTINEL GET-MASTER-ADDR-BY-NAME
echo -e "${GREEN}Test 2: SENTINEL GET-MASTER-ADDR-BY-NAME mymaster${NC}"
test_sentinel_command 26379 "*3\r\n\$8\r\nSENTINEL\r\n\$22\r\nGET-MASTER-ADDR-BY-NAME\r\n\$8\r\nmymaster\r\n"

# Test SENTINEL MASTERS
echo -e "${GREEN}Test 3: SENTINEL MASTERS${NC}"
test_sentinel_command 26379 "*2\r\n\$8\r\nSENTINEL\r\n\$7\r\nMASTERS\r\n"

# Test SENTINEL REPLICAS
echo -e "${GREEN}Test 4: SENTINEL REPLICAS mymaster${NC}"
test_sentinel_command 26379 "*3\r\n\$8\r\nSENTINEL\r\n\$8\r\nREPLICAS\r\n\$8\r\nmymaster\r\n"

# Test SENTINEL SENTINELS
echo -e "${GREEN}Test 5: SENTINEL SENTINELS mymaster${NC}"
test_sentinel_command 26379 "*3\r\n\$8\r\nSENTINEL\r\n\$9\r\nSENTINELS\r\n\$8\r\nmymaster\r\n"

# Test INFO
echo -e "${GREEN}Test 6: INFO${NC}"
test_sentinel_command 26379 "*1\r\n\$4\r\nINFO\r\n"

echo "========================================="
echo "All processes started successfully!"
echo "========================================="
echo ""
echo "Process IDs:"
echo "  Master:    $MASTER_PID (port 6379)"
echo "  Replica 1: $REPLICA1_PID (port 6380)"
echo "  Replica 2: $REPLICA2_PID (port 6381)"
echo "  Sentinel 1: $SENTINEL1_PID (port 26379)"
echo "  Sentinel 2: $SENTINEL2_PID (port 26380)"
echo "  Sentinel 3: $SENTINEL3_PID (port 26381)"
echo ""
echo "Logs are in: logs/"
echo ""
echo "To stop all processes:"
echo "  kill $MASTER_PID $REPLICA1_PID $REPLICA2_PID $SENTINEL1_PID $SENTINEL2_PID $SENTINEL3_PID"
echo ""
echo "Or save PIDs to file:"
cat > /tmp/redis_pids.txt <<EOF
$MASTER_PID
$REPLICA1_PID
$REPLICA2_PID
$SENTINEL1_PID
$SENTINEL2_PID
$SENTINEL3_PID
EOF
echo "  PIDs saved to /tmp/redis_pids.txt"
echo "  To kill all: kill \$(cat /tmp/redis_pids.txt)"
echo ""
echo "Press Ctrl+C to stop this script (processes will continue running)"
echo "Or wait 60 seconds for auto-cleanup..."

# Keep script running for 60 seconds
sleep 60

echo ""
echo "Cleaning up all processes..."
kill $MASTER_PID $REPLICA1_PID $REPLICA2_PID $SENTINEL1_PID $SENTINEL2_PID $SENTINEL3_PID 2>/dev/null || true
echo "Done!"
