#!/usr/bin/env python3
"""
Test Redis Pub/Sub SUBSCRIBE/UNSUBSCRIBE functionality
"""

import socket
import time
import threading

def send_command(sock, *args):
    """Send a RESP command"""
    command = f"*{len(args)}\r\n"
    for arg in args:
        command += f"${len(str(arg))}\r\n{arg}\r\n"
    sock.sendall(command.encode())

def read_response(sock, timeout=1):
    """Read a RESP response"""
    sock.settimeout(timeout)
    try:
        response = b""
        while True:
            chunk = sock.recv(1024)
            if not chunk:
                break
            response += chunk
            # Simple check: if we got a complete response
            if response.endswith(b"\r\n"):
                time.sleep(0.01)  # Small delay to catch any remaining data
                try:
                    extra = sock.recv(1024)
                    if extra:
                        response += extra
                except socket.timeout:
                    break
                break
        return response.decode('utf-8', errors='ignore')
    except socket.timeout:
        return response.decode('utf-8', errors='ignore')

def test_subscribe_unsubscribe():
    """Test SUBSCRIBE and UNSUBSCRIBE commands"""
    print("=== Testing SUBSCRIBE/UNSUBSCRIBE ===\n")
    
    # Create subscriber connection
    subscriber = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    subscriber.connect(('127.0.0.1', 6379))
    print("✓ Subscriber connected")
    
    # Subscribe to a channel
    print("\n1. Testing SUBSCRIBE news...")
    send_command(subscriber, "SUBSCRIBE", "news")
    response = read_response(subscriber)
    print(f"Response: {repr(response[:100])}")
    if "subscribe" in response and "news" in response:
        print("✓ SUBSCRIBE successful")
    else:
        print("✗ SUBSCRIBE failed")
    
    # Create publisher connection
    publisher = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    publisher.connect(('127.0.0.1', 6379))
    print("\n✓ Publisher connected")
    
    # Publish a message
    print("\n2. Testing PUBLISH news 'Hello World'...")
    send_command(publisher, "PUBLISH", "news", "Hello World")
    pub_response = read_response(publisher)
    print(f"Publisher response: {repr(pub_response[:50])}")
    
    # Subscriber should receive the message
    msg_response = read_response(subscriber, timeout=2)
    print(f"Subscriber received: {repr(msg_response[:100])}")
    if "message" in msg_response and "Hello World" in msg_response:
        print("✓ Message received successfully")
    else:
        print("✗ Message not received")
    
    # Test PSUBSCRIBE
    print("\n3. Testing PSUBSCRIBE sports:*...")
    send_command(subscriber, "PSUBSCRIBE", "sports:*")
    response = read_response(subscriber)
    print(f"Response: {repr(response[:100])}")
    if "psubscribe" in response and "sports:*" in response:
        print("✓ PSUBSCRIBE successful")
    else:
        print("✗ PSUBSCRIBE failed")
    
    # Publish to pattern-matched channel
    print("\n4. Testing PUBLISH sports:football 'Goal!'...")
    send_command(publisher, "PUBLISH", "sports:football", "Goal!")
    pub_response = read_response(publisher)
    print(f"Publisher response: {repr(pub_response[:50])}")
    
    # Give a bit more time for message delivery
    time.sleep(0.1)
    
    # Subscriber should receive pattern message
    msg_response = read_response(subscriber, timeout=3)
    print(f"Subscriber received: {repr(msg_response[:150])}")
    if "pmessage" in msg_response and "Goal!" in msg_response:
        print("✓ Pattern message received successfully")
    else:
        print("✗ Pattern message not received")
        print(f"Full response: {repr(msg_response)}")
    
    # Test UNSUBSCRIBE
    print("\n5. Testing UNSUBSCRIBE news...")
    send_command(subscriber, "UNSUBSCRIBE", "news")
    response = read_response(subscriber)
    print(f"Response: {repr(response[:100])}")
    if "unsubscribe" in response and "news" in response:
        print("✓ UNSUBSCRIBE successful")
    else:
        print("✗ UNSUBSCRIBE failed")
    
    # Test PUNSUBSCRIBE
    print("\n6. Testing PUNSUBSCRIBE sports:*...")
    send_command(subscriber, "PUNSUBSCRIBE", "sports:*")
    response = read_response(subscriber)
    print(f"Response: {repr(response[:100])}")
    if "punsubscribe" in response:
        print("✓ PUNSUBSCRIBE successful")
    else:
        print("✗ PUNSUBSCRIBE failed")
    
    # Cleanup
    subscriber.close()
    publisher.close()
    print("\n=== Test completed ===")

if __name__ == "__main__":
    try:
        test_subscribe_unsubscribe()
    except Exception as e:
        print(f"\n✗ Test failed with error: {e}")
        import traceback
        traceback.print_exc()
