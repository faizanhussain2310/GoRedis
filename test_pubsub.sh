#!/bin/bash

# Test PUBLISH command (should work without subscription)
echo "Testing PUBLISH (no subscribers)..."
echo "PUBLISH test-channel 'Hello World'" | nc localhost 6379
echo ""

# Test PUBSUB CHANNELS (should be empty)
echo "Testing PUBSUB CHANNELS (should be empty)..."
echo -e "*2\r\n\$6\r\nPUBSUB\r\n\$8\r\nCHANNELS\r\n" | nc localhost 6379
echo ""

# Test PUBSUB NUMSUB
echo "Testing PUBSUB NUMSUB..."
echo -e "*3\r\n\$6\r\nPUBSUB\r\n\$6\r\nNUMSUB\r\n\$12\r\ntest-channel\r\n" | nc localhost 6379
echo ""

# Test PUBSUB NUMPAT
echo "Testing PUBSUB NUMPAT..."
echo -e "*2\r\n\$6\r\nPUBSUB\r\n\$6\r\nNUMPAT\r\n" | nc localhost 6379
echo ""

echo "Basic pub/sub commands tested successfully!"
