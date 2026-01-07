# How redis-cli Communicates with Your Server

## Overview

This document explains how the `redis-cli` client communicates with your Redis server implementation. Understanding this helps you debug issues and understand the Redis protocol.

---

## What is redis-cli?

**redis-cli is a TCP client** that speaks the RESP (Redis Serialization Protocol).

```
redis-cli = TCP Client + RESP Encoder/Decoder + Terminal UI
```

**Components:**
- **TCP Client**: Opens network connections
- **RESP Encoder**: Converts commands to wire format
- **RESP Decoder**: Converts responses to human-readable text
- **Terminal UI**: Interactive shell with history, tab-completion, etc.

---

## Installation

```bash
brew install redis
```

**What gets installed:**
```
/opt/homebrew/bin/redis-cli       ← Client (what you use)
/opt/homebrew/bin/redis-server    ← Official Redis server (not used)
/opt/homebrew/bin/redis-benchmark ← Benchmarking tool
```

**Important:** redis-cli is **standalone** - it doesn't need redis-server to work!

---

## Connection Process

### 1. Starting redis-cli

```bash
# Default connection (127.0.0.1:6379)
redis-cli

# Explicit host and port
redis-cli -h 127.0.0.1 -p 6379

# Connect to different server
redis-cli -h 192.168.1.10 -p 8080
```

**Your server can run on ANY host:port** - just tell redis-cli where to connect!

### 2. TCP Handshake

```
┌─────────────┐                           ┌─────────────┐
│  redis-cli  │                           │ Your Server │
│             │                           │ (port:6379) │
└──────┬──────┘                           └──────┬──────┘
       │                                         │
       │ 1. TCP SYN (connect request)            │
       │────────────────────────────────────────►│
       │                                         │
       │ 2. TCP SYN-ACK (accepted)               │
       │◄────────────────────────────────────────│
       │                                         │
       │ 3. TCP ACK (connection established!)    │
       │────────────────────────────────────────►│
       │                                         │
       │ 4. COMMAND (redis-cli queries server)   │
       │────────────────────────────────────────►│
       │                                         │
       │ 5. Empty array response                 │
       │◄────────────────────────────────────────│
       │                                         │
       │ Now ready for user commands...          │
```

**What happens in Go:**

```go
// Your server (server/server.go)
listener, _ := net.Listen("tcp", "127.0.0.1:6379")  // Listening
conn, _ := listener.Accept()                         // Accepts connection

// redis-cli (internally in C)
int fd = socket(AF_INET, SOCK_STREAM, 0);
connect(fd, &server_addr, sizeof(server_addr));      // Connects
```

---

## Command Flow: Complete Cycle

### Example: User Types `SET key value`

```
┌──────────────────────────────────────────────────────────┐
│                    USER TERMINAL                          │
│                                                            │
│  127.0.0.1:6379> SET key value                           │
│                                                            │
└─────────────────────┬────────────────────────────────────┘
                      │
                      ▼
┌──────────────────────────────────────────────────────────┐
│                     redis-cli                             │
│                                                            │
│  Step 1: Parse user input                                │
│    Input: "SET key value"                                │
│    Tokens: ["SET", "key", "value"]                       │
│                                                            │
│  Step 2: Encode to RESP protocol                         │
│    *3\r\n                    ← Array with 3 elements     │
│    $3\r\nSET\r\n            ← Bulk string "SET"         │
│    $3\r\nkey\r\n            ← Bulk string "key"         │
│    $5\r\nvalue\r\n          ← Bulk string "value"       │
│                                                            │
│  Step 3: Send via TCP socket                             │
│    send(socket_fd, bytes, length, 0)                     │
│                                                            │
└─────────────────────┬────────────────────────────────────┘
                      │
                      │ Network (TCP)
                      ▼
┌──────────────────────────────────────────────────────────┐
│                   YOUR GO SERVER                          │
│                                                            │
│  Step 1: Accept connection                               │
│    conn, _ := listener.Accept()                          │
│                                                            │
│  Step 2: Read bytes from socket                          │
│    reader := bufio.NewReader(conn)                       │
│    // Receives: "*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n..."  │
│                                                            │
│  Step 3: Parse RESP protocol                             │
│    cmd, _ := protocol.ParseCommand(reader)               │
│    // Result: Command{Args: ["SET", "key", "value"]}     │
│                                                            │
│  Step 4: Route to handler                                │
│    response := h.executeCommand(cmd)                     │
│    // Routes to handleSet()                              │
│                                                            │
│  Step 5: Execute command                                 │
│    processor.Submit(SetCommand)                          │
│    store.Set("key", "value", nil)                        │
│                                                            │
│  Step 6: Encode response                                 │
│    response = protocol.EncodeSimpleString("OK")          │
│    // Result: "+OK\r\n"                                  │
│                                                            │
│  Step 7: Write to socket                                 │
│    writer.Write(response)                                │
│    writer.Flush()                                        │
│                                                            │
└─────────────────────┬────────────────────────────────────┘
                      │
                      │ Network (TCP)
                      ▼
┌──────────────────────────────────────────────────────────┐
│                     redis-cli                             │
│                                                            │
│  Step 1: Receive bytes from socket                       │
│    recv(socket_fd, buffer, size, 0)                      │
│    // Receives: "+OK\r\n"                                │
│                                                            │
│  Step 2: Decode RESP protocol                            │
│    Type: Simple String (prefix: +)                       │
│    Value: "OK"                                           │
│                                                            │
│  Step 3: Display to user                                 │
│    printf("OK\n")                                        │
│                                                            │
└─────────────────────┬────────────────────────────────────┘
                      │
                      ▼
┌──────────────────────────────────────────────────────────┐
│                    USER TERMINAL                          │
│                                                            │
│  127.0.0.1:6379> SET key value                           │
│  OK                                                       │
│  127.0.0.1:6379>                                         │
│                                                            │
└──────────────────────────────────────────────────────────┘
```

---

## RESP Protocol in Detail

### What redis-cli Sends (Commands)

**Example 1: PING**
```bash
User types: PING
```

**Wire format:**
```
*1\r\n          ← Array with 1 element
$4\r\n          ← Bulk string, length 4
PING\r\n        ← The command
```

**Example 2: SET key value**
```bash
User types: SET key value
```

**Wire format:**
```
*3\r\n          ← Array with 3 elements
$3\r\n          ← Bulk string, length 3
SET\r\n         ← First element
$3\r\n          ← Bulk string, length 3
key\r\n         ← Second element
$5\r\n          ← Bulk string, length 5
value\r\n       ← Third element
```

### What Your Server Sends (Responses)

**Simple String (success messages):**
```
+OK\r\n
+PONG\r\n
```

**Bulk String (actual data):**
```
$5\r\n          ← Length: 5 bytes
hello\r\n       ← Data
```

**Integer:**
```
:42\r\n         ← Number 42
:1\r\n          ← Number 1 (true)
:0\r\n          ← Number 0 (false)
```

**Null (key doesn't exist):**
```
$-1\r\n         ← Null bulk string
```

**Error:**
```
-ERR unknown command\r\n
```

**Array (multiple values):**
```
*2\r\n          ← Array with 2 elements
$4\r\n
key1\r\n
$4\r\n
key2\r\n
```

---

## Why It Works With Your Server

**redis-cli requirements:**
1. ✅ Server listens on TCP socket
2. ✅ Server speaks RESP protocol
3. ✅ Server responds to commands

**Your server provides:**
1. ✅ `net.Listen("tcp", "127.0.0.1:6379")` - TCP listener
2. ✅ `protocol.ParseCommand()` - RESP decoder
3. ✅ `protocol.Encode*()` functions - RESP encoder
4. ✅ Command handlers (SET, GET, etc.)

**Therefore: redis-cli works perfectly!** 🎉

---

## Code Mapping

### redis-cli Operations → Your Go Code

| redis-cli Action | Your Server Code |
|------------------|------------------|
| Connect to server | `listener.Accept()` in `server.go` |
| Send RESP bytes | `reader := bufio.NewReader(conn)` in `handler.go` |
| Parse command | `protocol.ParseCommand(reader)` in `resp.go` |
| Route command | `executeCommand()` in `handler.go` |
| Execute logic | `processor.Submit()` → `store.Set()` |
| Encode response | `protocol.EncodeSimpleString()` in `resp.go` |
| Send response | `writer.Write()` + `writer.Flush()` in `handler.go` |

---

## Alternative Clients

**Any client that speaks RESP works with your server!**

### 1. Python Client

```python
pip install redis

import redis
r = redis.Redis(host='127.0.0.1', port=6379)
r.set('key', 'value')
print(r.get('key'))  # b'value'
```

### 2. Node.js Client

```javascript
npm install redis

const redis = require('redis');
const client = redis.createClient({
  host: '127.0.0.1',
  port: 6379
});

await client.set('key', 'value');
console.log(await client.get('key'));  // 'value'
```

### 3. Go Client

```go
go get github.com/redis/go-redis/v9

import "github.com/redis/go-redis/v9"

client := redis.NewClient(&redis.Options{
    Addr: "127.0.0.1:6379",
})

client.Set(ctx, "key", "value", 0)
val, _ := client.Get(ctx, "key").Result()
fmt.Println(val)  // "value"
```

### 4. telnet (Manual RESP)

```bash
telnet localhost 6379

SET key value
+OK

GET key
$5
value
```

### 5. netcat (Scripting)

```bash
echo "PING" | nc localhost 6379
# +PONG

(echo "SET key value"; echo "GET key") | nc localhost 6379
# +OK
# $5
# value
```

**All of these work because they all speak RESP over TCP!**

---

## Common Questions

### Q: Is port 6379 required?

**No!** You can run on any port:

```go
// Your server
cfg := &server.Config{
    Host: "127.0.0.1",
    Port: 8080,  // Different port
}
```

```bash
# Connect with redis-cli
redis-cli -p 8080
```

### Q: Does redis-cli only work with Redis commands?

**Yes and no:**
- redis-cli will send ANY command you type
- But it only has special formatting/help for known Redis commands
- Your server can implement custom commands, redis-cli will send them

```bash
127.0.0.1:6379> MYCUSTOMCOMMAND arg1 arg2
# redis-cli sends: *3\r\n$15\r\nMYCUSTOMCOMMAND\r\n$4\r\narg1\r\n$4\r\narg2\r\n
# Your server receives and can handle it!
```

### Q: Can I test without redis-cli?

**Yes!** Use any of:
- telnet
- netcat (nc)
- Any programming language with socket library
- HTTP REST tools won't work (Redis uses RESP, not HTTP)

### Q: What happens if my server responds with invalid RESP?

redis-cli will show an error:
```bash
(error) Protocol error: invalid bulk length
```

Always use the `protocol.Encode*()` functions to ensure valid RESP!

### Q: Why does redis-cli send COMMAND on connect?

redis-cli queries the server for available commands to enable:
- Tab completion
- Command syntax help
- Validation

Your empty response is fine - redis-cli works without this info.

---

## Debugging Connection Issues

### Check if server is listening:
```bash
lsof -i :6379
# or
netstat -an | grep 6379
```

### Test basic connectivity:
```bash
telnet localhost 6379
# Should connect, then type:
PING
# Should see: +PONG
```

### Check server logs:
```bash
go run cmd/server/main.go
# Should see:
# Server listening on 127.0.0.1:6379
# New connection [1] from 127.0.0.1:xxxxx
```

### Verbose redis-cli:
```bash
redis-cli --verbose -p 6379
# Shows all sent/received data
```

---

## Key Takeaways

1. **redis-cli is just a TCP client** - nothing special about it
2. **RESP protocol is the common language** - both sides speak it
3. **Your server is protocol-compliant** - that's why it works
4. **Any RESP client works** - not limited to redis-cli
5. **Port 6379 is convention** - not requirement
6. **Two-way encoding** - commands and responses both use RESP
7. **Network is transparent** - just bytes over TCP

**The magic is in the protocol, not the tools!** ✨

Your server implementation correctly speaks RESP, so any RESP-compatible client (redis-cli, Python redis, Go redis, etc.) works seamlessly!
