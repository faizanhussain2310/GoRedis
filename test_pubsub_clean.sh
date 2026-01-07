#!/bin/bash

echo "=== Testing Pub/Sub Implementation ==="
echo ""

# Test 1: Basic PUBLISH
echo "Test 1: PUBLISH with no subscribers"
echo -e '*3\r\n$7\r\nPUBLISH\r\n$4\r\ntest\r\n$7\r\nmessage\r\n' | nc localhost 6379 | head -1
echo ""

# Test 2: PUBSUB CHANNELS
echo "Test 2: PUBSUB CHANNELS (no active channels)"
echo -e '*2\r\n$6\r\nPUBSUB\r\n$8\r\nCHANNELS\r\n' | nc localhost 6379 | head -1
echo ""

# Test 3: PUBSUB NUMSUB
echo "Test 3: PUBSUB NUMSUB"
(echo -e '*3\r\n$6\r\nPUBSUB\r\n$6\r\nNUMSUB\r\n$4\r\ntest\r\n'; sleep 0.1) | nc localhost 6379 | head -5
echo ""

# Test 4: PUBSUB NUMPAT
echo "Test 4: PUBSUB NUMPAT"
(echo -e '*2\r\n$6\r\nPUBSUB\r\n$6\r\nNUMPAT\r\n'; sleep 0.1) | nc localhost 6379 | head -1
echo ""

echo "=== All tests completed ==="
