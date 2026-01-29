# How Replication Works: Master → Replica Command Flow

## Overview

When you execute a write command on the master (e.g., `SET key "value"`), it automatically gets replicated to all connected replicas in real-time.

## Complete Flow Diagram

```
┌──────────────────────────────────────────────────────────────────────┐
│                         CLIENT                                        │
│                           ↓                                          │
│                   SET key "value"                                    │
└──────────────────────────────────────────────────────────────────────┘
                              ↓
┌──────────────────────────────────────────────────────────────────────┐
│                      MASTER SERVER                                    │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 1. Pipeline (pipeline.go)                                       │ │
│  │    - Parse command                                              │ │
│  │    - Check for replication commands (INFO, PSYNC, etc.)        │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 2. Execute Command (pipeline_executor.go)                       │ │
│  │    - Execute on local storage                                   │ │
│  │    - Returns response to client                                 │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 3. Log to AOF (if enabled)                                      │ │
│  │    - Append command to AOF file                                 │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 4. Propagate to Replicas (pipeline_executor.go:216)            │ │
│  │    replMgr.PropagateCommand(cmd.Args)                          │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 5. Queue Command (replication.go:287-300)                      │ │
│  │    - Create Command{Args, Timestamp}                           │ │
│  │    - Send to commandChan (buffered channel, 1000 capacity)     │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 6. Background Goroutine: propagateCommands()                   │ │
│  │    - Continuously reads from commandChan                        │ │
│  │    - Calls propagateToReplicas() for each command             │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 7. Encode & Send (replication.go:320-370)                      │ │
│  │    a) Encode command in RESP format:                           │ │
│  │       *3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n         │ │
│  │    b) Add to replication backlog (for partial resync)         │ │
│  │    c) Update master offset                                     │ │
│  │    d) Send to ALL online replicas via TCP                      │ │
│  │    e) Flush buffers                                            │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
└─────────────────────────────┼─────────────────────────────────────────┘
                              ↓
                      ┌───────┴────────┐
                      │   TCP Stream   │
                      │  (net.Conn)    │
                      └───────┬────────┘
                              ↓
┌──────────────────────────────────────────────────────────────────────┐
│                      REPLICA SERVER(S)                                │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 0. TCP Keepalive Setup (replica.go:47-51)                      │ │
│  │    - Enable TCP keepalive on master connection                 │ │
│  │    - Period: 30 seconds (OS-level dead connection detection)   │ │
│  │    - Detects network/hardware failures automatically           │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 8. Receive Stream (replica.go:252)                             │ │
│  │    receiveReplicationStream() goroutine is running              │ │
│  │    - Continuously reads from master TCP connection              │ │
│  │    - Read deadline: 65s (prevents infinite blocking)            │ │
│  │    - Blocking read on reader.ReadString('\n')                  │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 9. Parse RESP (replica.go:276-295)                             │ │
│  │    a) Read array marker: *3                                     │ │
│  │    b) For each element:                                         │ │
│  │       - Read length: $3                                         │ │
│  │       - Read data: SET                                          │ │
│  │    c) Build args: ["SET", "key", "value"]                      │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 10. Execute Command (replica.go:294)                           │ │
│  │     executeReplicatedCommand(args)                             │ │
│  │     ↓                                                           │ │
│  │     Calls commandExecutor callback (set in server.go:110)      │ │
│  │     ↓                                                           │ │
│  │     CommandHandler.ExecuteCommand()                            │ │
│  │     ↓                                                           │ │
│  │     Processor.Execute() - Updates local storage                │ │
│  └──────────────────────────┬──────────────────────────────────────┘ │
│                             ↓                                         │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 11. Update Offset (replica.go:301)                             │ │
│  │     masterInfo.Offset++                                         │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │ 12. Heartbeat Goroutine (replica.go:380-406)                   │ │
│  │     sendReplicationHeartbeat() running in background            │ │
│  │     - Sends REPLCONF ACK <offset> every 1 second                │ │
│  │     - Keeps connection alive                                    │ │
│  │     - Master tracks replica lag via offset                      │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                       │
│  Now replica has: key = "value" ✅                                   │
└───────────────────────────────────────────────────────────────────────┘
```

## Detailed Step-by-Step Breakdown

### MASTER SIDE

#### Step 1-3: Command Execution
```go
// pipeline_executor.go (line 180-220)
result := h.executeWithTransaction(ctx, client, cmd, tx, timeout)

// Command executes successfully
response := processor.Execute(cmd)  // Updates master's storage
```

#### Step 4: Trigger Replication
```go
// pipeline_executor.go (line 216)
if len(response) > 0 && response[0] != '-' {
    h.LogToAOF(command, cmd.Args[1:])
    
    // THIS IS WHERE REPLICATION STARTS
    if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
        replMgr.PropagateCommand(cmd.Args)  // ["SET", "key", "value"]
    }
}
```

#### Step 5: Queue Command (Non-Blocking)
```go
// replication.go (line 287-300)
func (rm *ReplicationManager) PropagateCommand(args []string) {
    cmd := &Command{
        Args:      args,           // ["SET", "key", "value"]
        Timestamp: time.Now(),
    }
    
    select {
    case rm.commandChan <- cmd:     // Send to buffered channel
    default:
        log.Printf("Command queue full")  // Channel has 1000 capacity
    }
}
```

**Why use a channel?**
- Non-blocking: Client doesn't wait for replication
- Asynchronous: Replication happens in background
- Buffered: Can queue up to 1000 commands during network delays

#### Step 6-7: Background Propagation
```go
// replication.go (line 305-315)
func (rm *ReplicationManager) propagateCommands() {
    for {
        select {
        case cmd := <-rm.commandChan:
            rm.propagateToReplicas(cmd)  // Send to all replicas
        case <-rm.shutdownChan:
            return
        }
    }
}

// replication.go (line 320-370)
func (rm *ReplicationManager) propagateToReplicas(cmd *Command) {
    // 1. Encode to RESP format
    respData := encodeCommandRESP(cmd.Args)
    // Result: *3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n
    
    // 2. Add to backlog (circular buffer for partial resync)
    rm.backlog.Append(respData)
    rm.offset += int64(len(respData))
    
    // 3. Send to each online replica
    for _, replica := range replicas {
        replica.Writer.Write(respData)  // TCP write
        replica.Writer.Flush()          // Force send
        replica.Offset = currentOffset  // Track replica offset
    }
}
```

#### RESP Encoding Example
```
Command: ["SET", "key", "value"]

Encoded:
*3\r\n              ← Array with 3 elements
$3\r\n              ← Bulk string length 3
SET\r\n             ← Data
$3\r\n              ← Bulk string length 3
key\r\n             ← Data
$5\r\n              ← Bulk string length 5
value\r\n           ← Data
```

### TCP TRANSPORT

The encoded bytes flow over the existing TCP connection established during the replication handshake (PSYNC).

### REPLICA SIDE

#### Step 8: Continuous Listening
```go
// replica.go (line 252)
func (rm *ReplicationManager) receiveReplicationStream() {
    log.Printf("Starting replication stream receiver")
    
    for {
        // Blocking read - waits for data from master
        line, err := reader.ReadString('\n')
        if err != nil {
            rm.handleMasterDisconnect()
            break
        }
        
        // Process the received data...
    }
}
```

**This goroutine:**
- Starts after successful PSYNC handshake
- Runs continuously in the background
- Blocks on `ReadString('\n')` waiting for master to send data
- Each command from master wakes it up

#### Step 9: Parse RESP Array
```go
// replica.go (line 276-295)
if strings.HasPrefix(line, "*") {
    var arrayLen int
    fmt.Sscanf(line, "*%d", &arrayLen)  // Parse: *3 → arrayLen=3
    
    args := make([]string, arrayLen)
    for i := 0; i < arrayLen; i++ {
        // Read bulk string length: $3
        lenLine, _ := reader.ReadString('\n')
        var argLen int
        fmt.Sscanf(lenLine, "$%d", &argLen)
        
        // Read actual data: SET
        argData := make([]byte, argLen)
        reader.Read(argData)
        args[i] = string(argData)
        
        // Read trailing \r\n
        reader.ReadString('\n')
    }
    
    // Result: args = ["SET", "key", "value"]
    
    // Handle special replication commands (replica.go:318-337)
    if args[0] == "PING" {
        // Respond to master's keepalive PING
        rm.sendToMaster("+PONG\r\n")
        continue
    }
    if args[0] == "REPLCONF" && args[1] == "GETACK" {
        // Master asking for current offset
        rm.sendToMaster(fmt.Sprintf("REPLCONF ACK %d", offset))
        continue
    }
}
```

#### Step 10: Execute on Replica
```go
// replica.go (line 294)
if err := rm.executeReplicatedCommand(args); err != nil {
    log.Printf("Error executing: %v", err)
}

// replication.go (line 455-465)
func (rm *ReplicationManager) executeReplicatedCommand(args []string) error {
    rm.mu.RLock()
    executor := rm.commandExecutor  // Set in server.go during startup
    rm.mu.RUnlock()
    
    if executor != nil {
        return executor(args)  // Executes command on local storage
    }
    return nil
}
```

**Where was commandExecutor set?**
```go
// server.go (line 110-118)
if replRole == replication.RoleReplica {
    replMgr.SetCommandExecutor(func(args []string) error {
        cmd := &protocol.Command{Args: args}
        response := cmdHandler.ExecuteCommand(cmd)
        if len(response) > 0 && response[0] == '-' {
            return fmt.Errorf("command failed: %s", string(response))
        }
        return nil
    })
}
```

This callback:
- Converts args to `protocol.Command`
- Calls `ExecuteCommand()` which uses the normal command processor
- Updates replica's local storage
- Returns error if command fails

#### Step 11: Update Replica Offset
```go
// replica.go (line 301)
rm.masterInfoMu.Lock()
if rm.masterInfo != nil {
    rm.masterInfo.Offset++  // Track how much data received
}
rm.masterInfoMu.Unlock()
```

## Key Design Decisions

### 1. **Asynchronous Replication**
- Master doesn't wait for replicas to acknowledge
- Commands are queued in a buffered channel
- Clients get fast responses

**Trade-off:** Replicas may lag behind master

### 2. **Persistent TCP Connections**
- One long-lived connection per replica (established during PSYNC)
- Commands stream over this connection
- No new connection per command

**Benefit:** Low latency, efficient

### 3. **RESP Protocol**
- Same protocol Redis uses for client-server communication
- Self-describing format (includes lengths)
- Binary-safe

### 4. **Circular Backlog Buffer**
- Stores recent commands (default 1MB)
- Used for partial resync if replica disconnects briefly
- Avoids full RDB transfer on reconnection

### 5. **Offset Tracking**
- Master and replica track byte offsets
- Used to detect synchronization state
- Enables partial resync

## Example: Full Session

```bash
# Terminal 1: Master
$ redis-cli -p 6379
> SET user:1 "Alice"
OK
> SET user:2 "Bob"
OK
```

**What happens internally:**

```
Master:
  1. Execute: storage["user:1"] = "Alice"
  2. Encode: *3\r\n$3\r\nSET\r\n$6\r\nuser:1\r\n$5\r\nAlice\r\n
  3. Send to Replica 1 (TCP write)
  4. Send to Replica 2 (TCP write)
  5. Update offset: +35 bytes
  
  1. Execute: storage["user:2"] = "Bob"
  2. Encode: *3\r\n$3\r\nSET\r\n$6\r\nuser:2\r\n$3\r\nBob\r\n
  3. Send to replicas
  4. Update offset: +33 bytes

Replica (continuously running):
  receiveReplicationStream() {
    Read from TCP: *3\r\n$3\r\nSET\r\n...
    Parse: ["SET", "user:1", "Alice"]
    Execute: storage["user:1"] = "Alice"
    Offset++
    
    Read from TCP: *3\r\n$3\r\nSET\r\n...
    Parse: ["SET", "user:2", "Bob"]
    Execute: storage["user:2"] = "Bob"
    Offset++
  }
```

```bash
# Terminal 2: Replica
$ redis-cli -p 6380
> GET user:1
"Alice"
> GET user:2
"Bob"
```

## Performance Characteristics

### Latency
- **Client → Master:** ~1ms (command execution)
- **Master → Replica:** ~2-5ms (network + execution)
- **Client sees:** Only master latency (async replication)

### Throughput
- **Bottleneck:** Network bandwidth between master and replicas
- **Channel buffer:** 1000 commands (prevents blocking)
- **Multiple replicas:** Sent in parallel (separate goroutine per replica)

### Failure Handling
- **Replica offline:** Master continues, drops failed replica
- **Network hiccup:** Commands queue in channel buffer
- **Buffer full:** Commands dropped (logged as warning)

## Monitoring

Check replication status:
```bash
# On master
redis-cli -p 6379 INFO REPLICATION
# Shows: connected_slaves, each replica's offset

# On replica
redis-cli -p 6380 INFO REPLICATION  
# Shows: master_host, master_port, slave_repl_offset
```

## Summary

The replication stream is a **continuous, asynchronous, TCP-based** command propagation system:

1. ✅ **Master:** Queues commands → Background goroutine → Encodes RESP → TCP send
2. ✅ **Network:** Persistent TCP connection streams bytes
3. ✅ **Replica:** Background goroutine → Reads TCP → Parses RESP → Executes locally

It's like a **live command mirror** - every write on master instantly flows to replicas!

## Connection Resilience & Timeout Mechanisms

### Three Layers of Protection

The replication connection uses multiple timeout mechanisms to ensure reliability:

#### 1. TCP Keepalive (OS Level) - `replica.go:47-51`
```go
if tcpConn, ok := conn.(*net.TCPConn); ok {
    tcpConn.SetKeepAlive(true)
    tcpConn.SetKeepAlivePeriod(30 * time.Second)
}
```

**What it does:**
- OS sends TCP probe packets every 30 seconds when connection is idle
- Detects network/hardware failures (cable unplugged, router failure)
- If no response after ~9 probes (270s total), OS closes the socket
- Next read/write returns error → triggers `handleMasterDisconnect()`

**Detection time:** ~30-90 seconds for complete network failure

#### 2. Read Deadline (Application Level) - `replica.go:224-227`
```go
// Set read deadline (65s - slightly longer than repl-timeout)
conn.SetReadDeadline(time.Now().Add(65 * time.Second))
```

**What it does:**
- Prevents infinite blocking if master goes silent (frozen, hung)
- Deadline resets on every successful read
- If 65 seconds pass with no data from master → read returns timeout error
- Triggers automatic reconnection

**Detection time:** 65 seconds of silence

#### 3. Application Heartbeat (Redis Protocol) - `replica.go:380-406`
```go
func (rm *ReplicationManager) sendReplicationHeartbeat() {
    ticker := time.NewTicker(1 * time.Second)
    for range ticker.C {
        // Send REPLCONF ACK <offset> every second
        cmd := fmt.Sprintf("*3\r\n$8\r\nREPLCONF\r\n$3\r\nACK\r\n$%d\r\n%s\r\n", 
                          len(offsetStr), offsetStr)
        rm.sendToMaster(cmd)
    }
}
```

**What it does:**
- Sends `REPLCONF ACK <offset>` to master every 1 second
- Master can track replica lag and health
- Keeps connection active (prevents timeout)
- Master can detect slow/stuck replicas

**Benefit:** Real-time lag monitoring

#### 4. PING/PONG Handling - `replica.go:318-337`
```go
if cmdName == "PING" {
    rm.sendToMaster("+PONG\r\n")
    continue
}
if cmdName == "REPLCONF" && args[1] == "GETACK" {
    rm.sendToMaster(fmt.Sprintf("REPLCONF ACK %d", offset))
    continue
}
```

**What it does:**
- Responds to master's PING commands (master-initiated keepalive)
- Responds to REPLCONF GETACK (master asking for offset)
- Proves replica is alive and processing commands

#### 5. Auto-Reconnect - `replica.go:345-366`
```go
func (rm *ReplicationManager) handleMasterDisconnect() {
    // Close connection, mark as disconnected
    // ...
    
    // Auto-reconnect after 5 seconds
    go func() {
        time.Sleep(5 * time.Second)
        rm.ConnectToMaster(host, port)
    }()
}
```

**What it does:**
- Any disconnect triggers automatic reconnection after 5 seconds
- Resilient to temporary network issues
- Continues retrying until successful

### Timeline: How Timeouts Work Together

```
Time    Activity
────────────────────────────────────────────────────────────
0s      Connection established
        - TCP keepalive enabled (30s period)
        - Read deadline set (65s)
        - Heartbeat goroutine started (1s interval)

1s      → Replica sends REPLCONF ACK 0
2s      → Replica sends REPLCONF ACK 0  
3s      ← Master sends: SET key val
        ↳ Replica executes, updates offset to 45
4s      → Replica sends REPLCONF ACK 45
...     (heartbeats continue every 1s)

30s     ← OS sends TCP keepalive probe
        ↳ Master responds (connection alive)

--- Master freezes here (hung process) ---

31s     → Replica sends REPLCONF ACK 45 (no response)
32s     → Replica sends REPLCONF ACK 45 (no response)
...     (heartbeats continue, but no data received)

60s     ← OS sends another TCP keepalive probe
        (Connection still open at TCP level)

65s     ⚠️  Read deadline exceeded!
        → read() returns: i/o timeout
        → handleMasterDisconnect() called
        → Connection closed
        → Wait 5 seconds...

70s     → Attempt reconnection to master
```

### Why Multiple Layers?

| Scenario | Detection Method | Time to Detect |
|----------|-----------------|----------------|
| Cable unplugged | TCP Keepalive | 30-90s |
| Master process frozen | Read Deadline | 65s |
| Master alive but slow | REPLCONF ACK lag | 1s (master detects) |
| Network partition | Both TCP + Deadline | 65s |
| Master restarted | Read error | Immediate |

**Result:** No matter what goes wrong, replica detects it and reconnects automatically! 🛡️
