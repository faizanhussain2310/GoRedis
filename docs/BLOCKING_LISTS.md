# Blocking List Operations

## Overview

Blocking list operations (BLPOP, BRPOP, BLMOVE, BRPOPLPUSH) allow clients to wait for data to become available in a list, rather than polling repeatedly. This is essential for implementing efficient producer-consumer patterns and message queues.

---

## Supported Commands

| Command | Syntax | Description |
|---------|--------|-------------|
| **BLPOP** | `BLPOP key [key ...] timeout` | Block until an element is available, then pop from the left |
| **BRPOP** | `BRPOP key [key ...] timeout` | Block until an element is available, then pop from the right |
| **BLMOVE** | `BLMOVE source dest LEFT\|RIGHT LEFT\|RIGHT timeout` | Block until element available, then move between lists |
| **BRPOPLPUSH** | `BRPOPLPUSH source dest timeout` | Block RPOP from source, LPUSH to dest (deprecated) |

**Timeout:**
- `0` = block forever (until data arrives or client disconnects)
- `> 0` = block for that many seconds, then return nil

---

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           BLOCKING SYSTEM                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────┐     ┌─────────────────────┐     ┌──────────────────┐  │
│  │ BlockingManager │────>│ keyBlockedClients   │     │ BlockedClient    │  │
│  │                 │     │ (reverse index)     │     │                  │  │
│  │ - BlockClient() │     │                     │     │ - ClientID       │  │
│  │ - UnblockWith   │     │ "mylist" → [C1, C2] │     │ - Keys           │  │
│  │   Data()        │     │ "queue"  → [C3]     │     │ - Direction      │  │
│  │ - RemoveClient()│     │                     │     │ - Timeout        │  │
│  └─────────────────┘     └─────────────────────┘     │ - ResponseCh     │  │
│                                                       │ - DestKey        │  │
│                                                       └──────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Data Structures

```go
// BlockedClient represents a client waiting for data
type BlockedClient struct {
    ClientID   int64
    Keys       []string          // Keys being watched (priority order)
    Direction  BlockingDirection // LEFT or RIGHT
    Timeout    time.Duration
    StartTime  time.Time
    ResponseCh chan BlockingResult // Channel to send result
    DestKey    string            // For BLMOVE (destination)
    DestDir    BlockingDirection // For BLMOVE
}

// BlockingManager uses reverse index for O(1) lookup
type BlockingManager struct {
    // Reverse index: key → clients blocked on this key (FIFO order)
    keyBlockedClients map[string][]*BlockedClient
    
    // Forward index: clientID → BlockedClient (for cleanup)
    clientBlocked map[int64]*BlockedClient
}
```

---

## Flow Diagrams

### BLPOP Flow (No Data Available)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Client A: BLPOP mylist 30                                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│ Step 1: Check if data exists (non-blocking)                                │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ value, ok := processor.LPop("mylist")                               │    │
│ │ ok = false (list is empty or doesn't exist)                         │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│ Step 2: Register as blocked                                                 │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ blockingManager.BlockClient(clientID, ["mylist"], LEFT, 30s)        │    │
│ │                                                                     │    │
│ │ Creates BlockedClient:                                              │    │
│ │   Keys: ["mylist"]                                                  │    │
│ │   Direction: LEFT                                                   │    │
│ │   Timeout: 30s                                                      │    │
│ │   ResponseCh: new channel                                           │    │
│ │                                                                     │    │
│ │ Adds to reverse index:                                              │    │
│ │   keyBlockedClients["mylist"] = [ClientA]                          │    │
│ │                                                                     │    │
│ │ Starts timeout goroutine                                            │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│ Step 3: Wait for result                                                     │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ select {                                                            │    │
│ │ case result := <-ResponseCh:  // Blocks here                       │    │
│ │     return [key, value]                                             │    │
│ │ case <-ctx.Done():                                                  │    │
│ │     return nil                                                      │    │
│ │ }                                                                   │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### LPUSH Waking Up Blocked Client

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Client B: LPUSH mylist "hello"                                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│ Step 1: Execute LPUSH normally                                             │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ processor.LPush("mylist", ["hello"])                                │    │
│ │ // List now has: ["hello"]                                          │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│ Step 2: Notify blocked clients                                             │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ handler.NotifyListPush("mylist")                                    │    │
│ │                                                                     │    │
│ │ blockingManager.UnblockClientWithData("mylist", popFunc, pushFunc)  │    │
│ │                                                                     │    │
│ │   // Check reverse index                                           │    │
│ │   blockedClients = keyBlockedClients["mylist"]  // [ClientA]       │    │
│ │                                                                     │    │
│ │   // Get first client (FIFO)                                       │    │
│ │   client = blockedClients[0]  // ClientA                           │    │
│ │                                                                     │    │
│ │   // Pop value for that client                                     │    │
│ │   value = popFunc(LEFT)  // Returns "hello"                        │    │
│ │                                                                     │    │
│ │   // Send result to client                                         │    │
│ │   client.ResponseCh <- BlockingResult{Key: "mylist", Value: "hello"}│    │
│ │                                                                     │    │
│ │   // Remove client from data structures                            │    │
│ │   delete(keyBlockedClients["mylist"], ClientA)                     │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│ Step 3: Client A receives result                                           │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ Client A's select receives from ResponseCh                          │    │
│ │ Returns: ["mylist", "hello"]                                        │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Multiple Keys (BLPOP key1 key2 key3)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Client: BLPOP queue1 queue2 queue3 0                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│ Step 1: Try each key in order (non-blocking)                               │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ for _, key := range ["queue1", "queue2", "queue3"] {                │    │
│ │     value, ok := processor.LPop(key)                                │    │
│ │     if ok {                                                         │    │
│ │         return [key, value]  // Return immediately                  │    │
│ │     }                                                               │    │
│ │ }                                                                   │    │
│ │ // No data in any queue                                             │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│ Step 2: Register for ALL keys                                              │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ BlockedClient.Keys = ["queue1", "queue2", "queue3"]                 │    │
│ │                                                                     │    │
│ │ // Add to reverse index for each key                                │    │
│ │ keyBlockedClients["queue1"].append(Client)                         │    │
│ │ keyBlockedClients["queue2"].append(Client)                         │    │
│ │ keyBlockedClients["queue3"].append(Client)                         │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│ Step 3: When any key gets data                                             │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ // LPUSH queue2 "data"                                              │    │
│ │                                                                     │    │
│ │ UnblockClientWithData("queue2", ...)                               │    │
│ │   → Pops from queue2, sends to client                               │    │
│ │   → Removes client from ALL keys (queue1, queue2, queue3)          │    │
│ │                                                                     │    │
│ │ Client receives: ["queue2", "data"]                                 │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### BLMOVE Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Client: BLMOVE source dest RIGHT LEFT 10                                    │
│ (Pop from right of source, push to left of dest)                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│ When data arrives in source:                                               │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ UnblockClientWithData("source", popFunc, pushFunc)                  │    │
│ │                                                                     │    │
│ │ // Pop from source (RIGHT)                                          │    │
│ │ value = popFunc(RIGHT)                                              │    │
│ │                                                                     │    │
│ │ // Push to dest (LEFT) - only for BLMOVE                           │    │
│ │ if client.DestKey != "" {                                           │    │
│ │     pushFunc("dest", value, LEFT)                                   │    │
│ │ }                                                                   │    │
│ │                                                                     │    │
│ │ // Send just the value (not key-value pair like BLPOP)             │    │
│ │ client.ResponseCh <- BlockingResult{Value: value}                   │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Timeout Handling

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ BLPOP with timeout                                                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│ When client blocks:                                                         │
│ ┌─────────────────────────────────────────────────────────────────────┐    │
│ │ if timeout > 0 {                                                    │    │
│ │     go handleTimeout(blockedClient)                                 │    │
│ │ }                                                                   │    │
│ │                                                                     │    │
│ │ func handleTimeout(bc *BlockedClient) {                             │    │
│ │     timer := time.NewTimer(bc.Timeout)                              │    │
│ │     select {                                                        │    │
│ │     case <-timer.C:                                                 │    │
│ │         // Timeout! Remove from data structures                     │    │
│ │         removeBlockedClient(bc)                                     │    │
│ │         bc.ResponseCh <- BlockingResult{Err: ErrTimeout}           │    │
│ │     case <-bc.ResponseCh:                                           │    │
│ │         // Already served before timeout                            │    │
│ │         return                                                      │    │
│ │     }                                                               │    │
│ │ }                                                                   │    │
│ └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│ Client receives:                                                            │
│   - Data arrived: ["key", "value"]                                         │
│   - Timeout: nil (null array)                                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## FIFO Ordering

When multiple clients are blocked on the same key, they are served in **FIFO order**:

```
Time │ Action                          │ keyBlockedClients["myqueue"]
─────┼─────────────────────────────────┼──────────────────────────────
  0  │ Client A: BLPOP myqueue 0       │ [A]
  1  │ Client B: BLPOP myqueue 0       │ [A, B]
  2  │ Client C: BLPOP myqueue 0       │ [A, B, C]
  3  │ LPUSH myqueue "first"           │ [B, C]    (A gets "first")
  4  │ LPUSH myqueue "second"          │ [C]       (B gets "second")
  5  │ LPUSH myqueue "third"           │ []        (C gets "third")
```

---

## Integration with Transactions

**Blocking commands are NOT allowed inside transactions:**

```redis
MULTI
BLPOP myqueue 0    # ERR BLPOP is not allowed in a transaction
```

**Why?**
- Transactions queue commands without executing
- BLPOP needs to potentially block (wait for data)
- You can't queue "maybe wait, maybe not" - it's either immediate or blocking

**Alternative: Use WATCH + LPOP**

```redis
WATCH myqueue
val = LPOP myqueue
if val is nil:
    UNWATCH
    # Use blocking version outside transaction
    val = BLPOP myqueue 0
else:
    MULTI
    # Process val
    EXEC
```

---

## Complete Example: Producer-Consumer

### Producer

```go
// Producer pushes messages to queue
for msg := range messages {
    client.RPush("work-queue", msg)
}
```

### Consumer (Blocking)

```go
// Consumer blocks until work is available
for {
    result := client.BLPop("work-queue", 0)  // Block forever
    if result != nil {
        key := result[0]
        msg := result[1]
        processMessage(msg)
    }
}
```

### Multiple Queues (Priority)

```go
// Check high-priority first, then normal, then low
for {
    result := client.BLPop(
        "queue:high",
        "queue:normal", 
        "queue:low",
        0,  // Block forever
    )
    if result != nil {
        processMessage(result[0], result[1])
    }
}
```

---

## Performance Considerations

| Operation | Complexity | Notes |
|-----------|------------|-------|
| BLPOP (data available) | O(1) | Immediate pop, no blocking |
| BLPOP (block) | O(K) | K = number of keys to watch |
| Unblock client | O(K) | Remove from K key watchers |
| LPUSH with blocked client | O(1) | Wake first client only |

### Memory Usage

```
Per blocked client:
- BlockedClient struct: ~100 bytes
- ResponseCh channel: ~100 bytes
- Entry in each key's watcher list: ~8 bytes × K keys

Total per blocked client ≈ 200 + 8K bytes
```

### Best Practices

1. **Use reasonable timeouts** - Don't block forever in production
2. **Handle nil responses** - Timeout returns nil, not an error
3. **Multiple keys for priorities** - First key has highest priority
4. **Clean up on disconnect** - Server removes blocked state automatically
5. **Don't use in transactions** - Use LPOP + WATCH pattern instead

---

## Summary

| Aspect | Implementation |
|--------|----------------|
| **Blocking Mechanism** | Go channels + goroutines |
| **Client Lookup** | Reverse index: key → clients |
| **Wake Order** | FIFO (first blocked = first served) |
| **Timeout** | Timer goroutine per client |
| **Multi-key** | Register for all, serve from first with data |
| **Transaction Support** | Not allowed (error returned) |

Blocking list operations provide efficient event-driven processing without polling, making them ideal for:
- Message queues
- Task queues
- Real-time notifications
- Producer-consumer patterns
