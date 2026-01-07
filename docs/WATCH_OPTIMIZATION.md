# WATCH Optimization: Redis-Style Reverse Index

## Overview

This document explains how we optimized the WATCH mechanism from O(N) at EXEC time to O(1) by using Redis's approach of maintaining a **reverse index** and **dirty flags**.

---

## The Problem with Naive Implementation

### Original Approach (Version Tracking)

```go
// Transaction stores version of each key at WATCH time
type Transaction struct {
    WatchedKeys map[string]int64  // key → version when watched
}

// TransactionManager tracks current version of each key
type TransactionManager struct {
    keyVersions map[string]int64  // key → current version
}
```

**WATCH operation:**
```go
func WatchKey(key string) {
    tx.WatchedKeys[key] = keyVersions[key]  // Store current version
}
// O(1) ✓
```

**Write operation (SET, etc.):**
```go
func OnKeyWrite(key string) {
    keyVersions[key]++  // Increment version
}
// O(1) ✓
```

**EXEC operation:**
```go
func CheckWatchedKeys(tx *Transaction) bool {
    for key, watchedVersion := range tx.WatchedKeys {  // O(N) loop!
        if keyVersions[key] != watchedVersion {
            return false  // Key was modified
        }
    }
    return true
}
// O(N) where N = number of watched keys ✗
```

### The Problem

```
Client watches 100 keys:
WATCH key1 key2 key3 ... key100
MULTI
SET foo bar
EXEC  ← Must check 100 keys! O(100)

If 1000 clients each watch 100 keys:
Total EXEC overhead = 1000 × 100 = 100,000 comparisons
```

---

## Redis's Solution: Reverse Index + Dirty Flag

### Key Insight

Instead of checking all watched keys at EXEC time, **mark transactions as dirty when keys are modified**.

```
Work at WATCH time:  O(1) per key
Work at WRITE time:  O(M) where M = clients watching this key (usually 0-2)
Work at EXEC time:   O(1) - just check a boolean flag!
```

### Data Structures

```go
// Transaction now has a simple dirty flag
type Transaction struct {
    WatchedKeys map[string]struct{}  // Just track which keys, no versions
    Dirty       bool                  // True if ANY watched key was modified
}

// TransactionManager has a REVERSE INDEX
type TransactionManager struct {
    transactions map[int64]*Transaction       // clientID → transaction
    keyWatchers  map[string]map[int64]struct{} // key → set of clientIDs watching
                 ↑
                 REVERSE INDEX!
}
```

### Visual Representation

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           FORWARD INDEX                                     │
│                    (Transaction → Watched Keys)                             │
│                                                                             │
│  Client 1's Transaction          Client 2's Transaction                    │
│  ┌─────────────────────┐        ┌─────────────────────┐                    │
│  │ WatchedKeys:        │        │ WatchedKeys:        │                    │
│  │   - "balance"       │        │   - "balance"       │                    │
│  │   - "inventory"     │        │   - "counter"       │                    │
│  │ Dirty: false        │        │ Dirty: false        │                    │
│  └─────────────────────┘        └─────────────────────┘                    │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                           REVERSE INDEX                                     │
│                    (Key → Watching Clients)                                 │
│                                                                             │
│  keyWatchers:                                                               │
│  ┌─────────────────────────────────────────────────┐                       │
│  │ "balance"   → {Client1, Client2}                │                       │
│  │ "inventory" → {Client1}                         │                       │
│  │ "counter"   → {Client2}                         │                       │
│  └─────────────────────────────────────────────────┘                       │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Operation Flows

### 1. WATCH Operation

```go
func WatchKey(clientID int64, key string) {
    // Add to transaction's watch list (forward direction)
    tx.WatchedKeys[key] = struct{}{}
    
    // Add to reverse index (key → clients)
    keyWatchers[key].add(clientID)
}
```

**Complexity: O(1)**

```
Before WATCH "balance" by Client 1:
┌──────────────────────────────────────────┐
│ keyWatchers["balance"] = {}              │
│ Client1.WatchedKeys = {}                 │
└──────────────────────────────────────────┘

After WATCH "balance" by Client 1:
┌──────────────────────────────────────────┐
│ keyWatchers["balance"] = {Client1}       │
│ Client1.WatchedKeys = {"balance"}        │
└──────────────────────────────────────────┘

After WATCH "balance" by Client 2:
┌──────────────────────────────────────────┐
│ keyWatchers["balance"] = {Client1, Client2} │
│ Client1.WatchedKeys = {"balance"}        │
│ Client2.WatchedKeys = {"balance"}        │
└──────────────────────────────────────────┘
```

### 2. Write Operation (SET, INCR, etc.)

```go
func TouchKey(key string) {
    watchers := keyWatchers[key]
    
    // Mark ALL watching clients as dirty
    for clientID := range watchers {
        transactions[clientID].Dirty = true
    }
}
```

**Complexity: O(M) where M = number of clients watching this key**

```
SET balance 100 (modifies "balance"):

Step 1: Look up watchers
┌──────────────────────────────────────────┐
│ keyWatchers["balance"] = {Client1, Client2} │
└──────────────────────────────────────────┘

Step 2: Mark each watcher as dirty
┌──────────────────────────────────────────┐
│ Client1.Dirty = true  ✓                  │
│ Client2.Dirty = true  ✓                  │
└──────────────────────────────────────────┘

Done in O(2) = O(M) where M = watchers
```

### 3. EXEC Operation

```go
func IsTransactionDirty(tx *Transaction) bool {
    return tx.Dirty  // That's it! Just return the flag
}

func HandleExec(tx *Transaction) {
    if tx.Dirty {
        return nil  // Abort - watched key was modified
    }
    // Execute queued commands...
}
```

**Complexity: O(1)** - Just check a boolean!

```
EXEC by Client 1:

┌──────────────────────────────────────────┐
│ Check: Client1.Dirty == true?            │
│                                          │
│ If true  → Abort (return nil)            │
│ If false → Execute queued commands       │
└──────────────────────────────────────────┘

Single boolean check: O(1)!
```

---

## Complete Example Timeline

```
Time │ Client A                │ Client B              │ State
─────┼────────────────────────┼───────────────────────┼────────────────────────
  0  │ WATCH balance          │                       │ keyWatchers["balance"]
     │                        │                       │   = {A}
     │                        │                       │ A.Dirty = false
─────┼────────────────────────┼───────────────────────┼────────────────────────
  1  │ MULTI                  │                       │ A.State = TxStarted
─────┼────────────────────────┼───────────────────────┼────────────────────────
  2  │ GET balance (queued)   │                       │ A.Queue = [GET balance]
─────┼────────────────────────┼───────────────────────┼────────────────────────
  3  │                        │ SET balance 500       │ TouchKey("balance"):
     │                        │                       │   A.Dirty = true ←─┐
     │                        │                       │                    │
     │                        │                       │ Marked immediately!
─────┼────────────────────────┼───────────────────────┼────────────────────────
  4  │ SET balance 200 (queued)│                      │ A.Queue = [GET, SET]
─────┼────────────────────────┼───────────────────────┼────────────────────────
  5  │ EXEC                   │                       │ Check A.Dirty:
     │   → Check: A.Dirty?    │                       │   true → ABORT!
     │   → true! ABORT        │                       │
     │   → Return nil         │                       │ A.Reset()
     │                        │                       │ Clear A's watches
─────┼────────────────────────┼───────────────────────┼────────────────────────

Total work at EXEC: O(1) boolean check
Compare to naive: Would need to check ALL watched keys
```

---

## Complexity Comparison

| Operation | Naive (Version Check) | Optimized (Dirty Flag) |
|-----------|----------------------|------------------------|
| WATCH key | O(1) | O(1) |
| SET key | O(1) increment version | O(M) mark M watchers dirty |
| EXEC | **O(N) check N keys** | **O(1) check flag** |
| UNWATCH | O(1) | O(K) remove from K keys |

Where:
- **N** = number of keys watched by client
- **M** = number of clients watching modified key (typically 0-2)
- **K** = number of keys client was watching

### Why This is Better

**Typical workload:**
- Clients watch 1-10 keys
- Most keys have 0-1 watchers
- EXEC is called frequently

**Naive approach:**
```
1000 clients × 10 watched keys × 1000 EXEC/sec = 10,000,000 comparisons/sec
```

**Optimized approach:**
```
1000 EXEC/sec × O(1) check = 1,000 operations/sec (for EXEC)
+ Write overhead: minimal (most keys have 0 watchers)
```

---

## Code Implementation

### Transaction Structure

```go
type Transaction struct {
    State       TransactionState
    Queue       []QueuedCommand
    WatchedKeys map[string]struct{}  // Set of watched keys
    Dirty       bool                  // Dirty flag
}

func (t *Transaction) MarkDirty() {
    t.Dirty = true
}

func (t *Transaction) ClearWatches() {
    t.WatchedKeys = make(map[string]struct{})
    t.Dirty = false
}
```

### TransactionManager with Reverse Index

```go
type TransactionManager struct {
    mu           sync.RWMutex
    transactions map[int64]*Transaction
    keyWatchers  map[string]map[int64]struct{}  // REVERSE INDEX
}

// O(1) - Add to reverse index
func (tm *TransactionManager) WatchKey(clientID int64, key string) {
    tm.mu.Lock()
    defer tm.mu.Unlock()
    
    tx := tm.transactions[clientID]
    tx.WatchedKeys[key] = struct{}{}
    
    if _, ok := tm.keyWatchers[key]; !ok {
        tm.keyWatchers[key] = make(map[int64]struct{})
    }
    tm.keyWatchers[key][clientID] = struct{}{}
}

// O(M) - Mark all watchers dirty
func (tm *TransactionManager) TouchKey(key string) {
    tm.mu.Lock()
    defer tm.mu.Unlock()
    
    for clientID := range tm.keyWatchers[key] {
        if tx, ok := tm.transactions[clientID]; ok {
            tx.MarkDirty()
        }
    }
}

// O(1) - Just check the flag!
func (tm *TransactionManager) IsTransactionDirty(tx *Transaction) bool {
    return tx.Dirty
}
```

### Usage in Pipeline

```go
// On WATCH command
func handleWatchCommand(cmd *Command, client *Client, tx *Transaction) {
    for _, key := range cmd.Args[1:] {
        txManager.WatchKey(client.ID, key)
    }
}

// On any write command (SET, INCR, etc.)
func executeCommand(cmd *Command) {
    result := execute(cmd)
    
    // Touch watched keys - marks watchers dirty
    if writeKeys := GetWriteKeys(cmd); len(writeKeys) > 0 {
        txManager.TouchKeys(writeKeys)
    }
    
    return result
}

// On EXEC command
func handleExecCommand(tx *Transaction) {
    // O(1) check!
    if txManager.IsTransactionDirty(tx) {
        return nil  // Abort
    }
    
    // Execute queued commands...
}
```

---

## Memory Considerations

### Reverse Index Memory Usage

```
Memory per watched key = sizeof(map entry) × num_watchers
                       ≈ 8 bytes × M

Total memory = Σ (8 bytes × watchers per key)
```

For typical workloads (most keys have 0-1 watchers), this is negligible.

### Cleanup on Disconnect

When a client disconnects, we must:
1. Remove from all keyWatchers entries
2. Delete empty keyWatchers entries

```go
func RemoveClient(clientID int64) {
    tx := transactions[clientID]
    
    // Remove from reverse index
    for key := range tx.WatchedKeys {
        delete(keyWatchers[key], clientID)
        if len(keyWatchers[key]) == 0 {
            delete(keyWatchers, key)  // Cleanup empty entry
        }
    }
    
    delete(transactions, clientID)
}
```

---

## Summary

| Aspect | Before | After |
|--------|--------|-------|
| Data Structure | Version per key | Dirty flag + Reverse index |
| EXEC Complexity | O(N) | **O(1)** |
| Write Complexity | O(1) | O(M) |
| Space | O(K) versions | O(K + W) reverse index |
| Real-world Performance | Slow with many watches | Fast always |

Where:
- N = watched keys per client
- M = watchers per key (typically 0-2)
- K = total unique keys
- W = total watch relationships

**Key Insight:** Move work from EXEC time (frequent) to write time (spread out), and make EXEC O(1) by using a pre-computed dirty flag.

---

## References

- [Redis WATCH implementation](https://github.com/redis/redis/blob/unstable/src/multi.c)
- [Redis Transactions Documentation](https://redis.io/topics/transactions)
