#!/bin/bash

# Test script for RDB replication functionality
# This script tests that data is properly replicated from master to replica

echo "========================================="
echo "RDB Replication Test Script"
echo "========================================="

# Cleanup function
cleanup() {
    echo "Cleaning up..."
    kill $MASTER_PID 2>/dev/null
    kill $REPLICA_PID 2>/dev/null
    rm -f /tmp/redis-master.conf /tmp/redis-replica.conf
    rm -f /tmp/redis-master.aof /tmp/redis-replica.aof
    rm -f /tmp/redis-master.rdb /tmp/redis-replica.rdb
}

# Set trap to cleanup on exit
trap cleanup EXIT

# Start master on port 6379
echo "Starting master on port 6379..."
./redis-server --port 6379 --aof-enabled=false > /tmp/master.log 2>&1 &
MASTER_PID=$!
sleep 2

# Check if master started
if ! ps -p $MASTER_PID > /dev/null; then
    echo "ERROR: Master failed to start!"
    cat /tmp/master.log
    exit 1
fi
echo "Master started (PID: $MASTER_PID)"

# Populate master with different data types
echo ""
echo "Populating master with test data..."

# String values
redis-cli -p 6379 SET string1 "Hello World" > /dev/null
redis-cli -p 6379 SET string2 "Test Value" > /dev/null
redis-cli -p 6379 SET string3 "With Expiry" EX 3600 > /dev/null

# List
redis-cli -p 6379 RPUSH list1 "item1" "item2" "item3" > /dev/null

# Set
redis-cli -p 6379 SADD set1 "member1" "member2" "member3" > /dev/null

# Hash
redis-cli -p 6379 HSET hash1 field1 "value1" field2 "value2" field3 "value3" > /dev/null

echo "Master data populated:"
echo "  - 3 strings (1 with expiry)"
echo "  - 1 list with 3 items"
echo "  - 1 set with 3 members"
echo "  - 1 hash with 3 fields"

# Start replica on port 6380
echo ""
echo "Starting replica on port 6380..."
./redis-server --port 6380 --replicaof 127.0.0.1 6379 --aof-enabled=false > /tmp/replica.log 2>&1 &
REPLICA_PID=$!
sleep 3

# Check if replica started
if ! ps -p $REPLICA_PID > /dev/null; then
    echo "ERROR: Replica failed to start!"
    cat /tmp/replica.log
    exit 1
fi
echo "Replica started (PID: $REPLICA_PID)"

# Wait for replication to complete
echo ""
echo "Waiting for replication to complete..."
sleep 2

# Verify data on replica
echo ""
echo "========================================="
echo "Verification Results"
echo "========================================="

PASSED=0
FAILED=0

# Test string1
RESULT=$(redis-cli -p 6380 GET string1 2>/dev/null)
if [ "$RESULT" = "Hello World" ]; then
    echo "✓ String1 replicated correctly: $RESULT"
    PASSED=$((PASSED + 1))
else
    echo "✗ String1 FAILED: Expected 'Hello World', got '$RESULT'"
    FAILED=$((FAILED + 1))
fi

# Test string2
RESULT=$(redis-cli -p 6380 GET string2 2>/dev/null)
if [ "$RESULT" = "Test Value" ]; then
    echo "✓ String2 replicated correctly: $RESULT"
    PASSED=$((PASSED + 1))
else
    echo "✗ String2 FAILED: Expected 'Test Value', got '$RESULT'"
    FAILED=$((FAILED + 1))
fi

# Test string3 (with expiry)
RESULT=$(redis-cli -p 6380 GET string3 2>/dev/null)
if [ "$RESULT" = "With Expiry" ]; then
    echo "✓ String3 (with expiry) replicated correctly: $RESULT"
    PASSED=$((PASSED + 1))
else
    echo "✗ String3 FAILED: Expected 'With Expiry', got '$RESULT'"
    FAILED=$((FAILED + 1))
fi

# Test list
RESULT=$(redis-cli -p 6380 LRANGE list1 0 -1 2>/dev/null | tr '\n' ',' | sed 's/,$//')
EXPECTED="item1,item2,item3"
if [ "$RESULT" = "$EXPECTED" ]; then
    echo "✓ List replicated correctly: [$RESULT]"
    PASSED=$((PASSED + 1))
else
    echo "✗ List FAILED: Expected '$EXPECTED', got '$RESULT'"
    FAILED=$((FAILED + 1))
fi

# Test set
RESULT=$(redis-cli -p 6380 SMEMBERS set1 2>/dev/null | sort | tr '\n' ',' | sed 's/,$//')
# Can't guarantee order, so just check count
COUNT=$(redis-cli -p 6380 SCARD set1 2>/dev/null)
if [ "$COUNT" = "3" ]; then
    echo "✓ Set replicated correctly: $COUNT members"
    PASSED=$((PASSED + 1))
else
    echo "✗ Set FAILED: Expected 3 members, got $COUNT"
    FAILED=$((FAILED + 1))
fi

# Test hash
COUNT=$(redis-cli -p 6380 HLEN hash1 2>/dev/null)
if [ "$COUNT" = "3" ]; then
    FIELD1=$(redis-cli -p 6380 HGET hash1 field1 2>/dev/null)
    if [ "$FIELD1" = "value1" ]; then
        echo "✓ Hash replicated correctly: $COUNT fields"
        PASSED=$((PASSED + 1))
    else
        echo "✗ Hash FAILED: Field values incorrect"
        FAILED=$((FAILED + 1))
    fi
else
    echo "✗ Hash FAILED: Expected 3 fields, got $COUNT"
    FAILED=$((FAILED + 1))
fi

# Check replication info
echo ""
echo "========================================="
echo "Replication Status"
echo "========================================="
echo "Master INFO:"
redis-cli -p 6379 INFO replication 2>/dev/null | grep -E "(role|connected_slaves)" || echo "INFO command not fully implemented"

echo ""
echo "Replica INFO:"
redis-cli -p 6380 INFO replication 2>/dev/null | grep -E "(role|master_host|master_port)" || echo "INFO command not fully implemented"

# Summary
echo ""
echo "========================================="
echo "Test Summary"
echo "========================================="
echo "Passed: $PASSED"
echo "Failed: $FAILED"
echo ""

if [ $FAILED -eq 0 ]; then
    echo "✓ All tests passed! RDB replication is working correctly."
    exit 0
else
    echo "✗ Some tests failed. Check logs:"
    echo "  Master: /tmp/master.log"
    echo "  Replica: /tmp/replica.log"
    exit 1
fi
