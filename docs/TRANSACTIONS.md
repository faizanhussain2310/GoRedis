# Redis Transactions

## Table of Contents
1. [Why Transactions?](#why-transactions)
2. [Redis vs Relational Transactions](#redis-vs-relational-transactions)
3. [How Redis Transactions Work](#how-redis-transactions-work)
4. [WATCH - Optimistic Locking](#watch---optimistic-locking)
5. [Complete Examples](#complete-examples)
6. [Best Practices](#best-practices)

---

## Why Transactions?

### The Problem: Race Conditions

When multiple clients access shared data concurrently, race conditions can occur:

```
Time | Client A              | Client B              | Balance
-----|----------------------|----------------------|----------
  0  | GET balance → 100    |                      | 100
  1  |                      | GET balance → 100    | 100
  2  | (compute: 100+50)    |                      | 100
  3  |                      | (compute: 100+30)    | 100
  4  | SET balance 150      |                      | 150
  5  |                      | SET balance 130      | 130 ❌
```

**Lost Update!** Client A's addition of 50 was overwritten. The balance should be 180, not 130.

### The Solution: Atomic Operations

Redis transactions ensure multiple commands execute **atomically** (all-or-nothing, no interleaving):

```
Client A: MULTI → GET balance → SET balance 150 → EXEC
Client B: MULTI → GET balance → SET balance 130 → EXEC

One completes entirely before the other starts.
```

---

## Redis vs Relational Transactions

Redis transactions are **fundamentally different** from SQL transactions:

| Feature | Redis Transactions | Relational DB Transactions |
|---------|-------------------|---------------------------|
| **Atomicity** | ✅ Commands execute sequentially without interruption | ✅ All or nothing execution |
| **Consistency** | ❌ No consistency checks | ✅ Constraints enforced |
| **Isolation** | ❌ No isolation during queuing | ✅ Full isolation (ACID) |
| **Durability** | ⚠️ Depends on persistence config | ✅ Guaranteed with WAL |
| **Rollback** | ❌ No rollback on errors | ✅ Full rollback support |
| **Locking** | ✅ Optimistic (WATCH) | ✅ Pessimistic (row locks) |

### Key Differences Explained

#### 1. No Rollback

```redis
# Redis Transaction:
MULTI
SET key1 "value1"     # ✓ Success
INCR key2             # ❌ Error (key2 is a string, not integer)
SET key3 "value3"     # ✓ Still executes!
EXEC

Result: key1 and key3 are set, key2 fails
       No rollback - partial execution!
```

```sql
-- SQL Transaction:
BEGIN;
UPDATE accounts SET balance = 100 WHERE id = 1;  -- ✓
UPDATE accounts SET balance = 'invalid';         -- ❌ Error
UPDATE accounts SET balance = 200 WHERE id = 2;  -- Never runs
COMMIT;

Result: First UPDATE is rolled back automatically
        Nothing is committed
```

#### 2. No Isolation During Queuing

```redis
# Redis - Commands see CURRENT state, not transaction start state:
Client A: MULTI
Client A: GET balance    # Queued (not executed yet)
Client B: SET balance 200
Client A: EXEC           # Now GET executes, returns 200 (Client B's value!)
```

```sql
-- SQL - Reads see state at transaction start (REPEATABLE READ):
Client A: BEGIN;
Client A: SELECT balance FROM accounts WHERE id = 1;  -- Returns 100
Client B: UPDATE accounts SET balance = 200 WHERE id = 1;
Client A: SELECT balance FROM accounts WHERE id = 1;  -- Still returns 100!
Client A: COMMIT;
```

#### 3. Optimistic vs Pessimistic Locking

**Redis (Optimistic - WATCH):**
```redis
# Assume success, check at the end
WATCH balance
GET balance           # Read value
# Do computation
MULTI
SET balance 150
EXEC                  # Check if balance was modified
                      # If modified → abort, retry
                      # If not → commit
```

**SQL (Pessimistic - Row Locks):**
```sql
-- Lock immediately, prevent others from modifying
BEGIN;
SELECT balance FROM accounts WHERE id = 1 FOR UPDATE;  -- Lock row
-- Other transactions block here until this commits
UPDATE accounts SET balance = 150 WHERE id = 1;
COMMIT;  -- Release lock
```

---

## How Redis Transactions Work

### Command Flow

```
┌─────────────────────────────────────────────────────────────────┐
│ Client: MULTI                                                   │
│ Server: OK                                                      │
│         State = TxStarted                                       │
│         Queue = []                                              │
├─────────────────────────────────────────────────────────────────┤
│ Client: SET key1 value1                                         │
│ Server: QUEUED                                                  │
│         Queue = [SET key1 value1]                               │
├─────────────────────────────────────────────────────────────────┤
│ Client: GET key1                                                │
│ Server: QUEUED                                                  │
│         Queue = [SET key1 value1, GET key1]                     │
├─────────────────────────────────────────────────────────────────┤
│ Client: INCR counter                                            │
│ Server: QUEUED                                                  │
│         Queue = [SET key1 value1, GET key1, INCR counter]       │
├─────────────────────────────────────────────────────────────────┤
│ Client: EXEC                                                    │
│ Server: Execute all queued commands atomically                  │
│         Returns: [OK, "value1", 1]                              │
│         State = TxIdle                                          │
│         Queue = []                                              │
└─────────────────────────────────────────────────────────────────┘
```

### State Machine

```
                    MULTI
       TxIdle ──────────────> TxStarted
         ↑                        │
         │                        │ (commands get queued)
         │                        │
         │                        ↓
         │                    [Queue: cmd1, cmd2, cmd3]
         │                        │
         │     EXEC               │     DISCARD
         └────────────────────────┴────────────────
                (execute all)          (clear queue)
```

---

## WATCH - Optimistic Locking

### What is WATCH?

WATCH implements **check-and-set** semantics (optimistic concurrency control):

1. **Watch keys** before transaction
2. **Record their versions** (or values)
3. **Queue commands** in transaction
4. **Check if watched keys changed** before EXEC
5. **Abort if modified**, **commit if unchanged**

### Version Tracking

```go
// Internal implementation:
type TransactionManager struct {
    keyVersions map[string]uint64  // Track version of each key
}

// Every WRITE increments version:
SET key1 value     → keyVersions["key1"]++
INCR counter       → keyVersions["counter"]++
LPUSH list item    → keyVersions["list"]++

// WATCH records current version:
WATCH key1         → tx.WatchedKeys["key1"] = keyVersions["key1"]

// EXEC checks versions:
EXEC → for each watched key:
           if current_version != watched_version:
               abort transaction (return nil)
```

### Example: Safe Counter Increment

#### Without WATCH (Race Condition):

```redis
# Client A:
GET counter           # Returns 10
# Compute: 10 + 1 = 11
SET counter 11

# Client B (interleaved):
GET counter           # Returns 10 (before Client A's SET)
# Compute: 10 + 1 = 11
SET counter 11        # Overwrites Client A's value!

# Result: counter = 11 (should be 12) ❌
```

#### With WATCH (Safe):

```redis
# Client A:
WATCH counter              # Records version = 5
val = GET counter          # Returns 10
MULTI
SET counter 11             # Queued
EXEC                       # Check version before executing

# Client B (interleaved):
SET counter 100            # Version increments: 5 → 6

# Client A's EXEC:
# Check: current version (6) != watched version (5)
# ABORT! Return nil
# Client A retries:
WATCH counter
val = GET counter          # Returns 100
MULTI
SET counter 101
EXEC                       # Success! ✅
```

### Visual Timeline

```
Time | Client A                    | Client B         | Key Version | Value
-----|----------------------------|-----------------|-------------|-------
  0  | WATCH balance              |                 | version=1   | 100
     | (records version 1)        |                 |             |
  1  | val = GET balance (→100)   |                 | version=1   | 100
  2  | compute: 100 + 50 = 150    |                 | version=1   | 100
  3  | MULTI                      |                 | version=1   | 100
  4  | SET balance 150 (queued)   |                 | version=1   | 100
  5  |                            | SET balance 200 | version=2   | 200
     |                            | (increments!)   |             |
  6  | EXEC                       |                 | version=2   | 200
     | → Check: v2 ≠ v1           |                 |             |
     | → ABORT! Return nil        |                 |             |
  7  | RETRY:                     |                 | version=2   | 200
     | WATCH balance              |                 |             |
     | (records version 2)        |                 |             |
  8  | val = GET balance (→200)   |                 | version=2   | 200
  9  | compute: 200 + 50 = 250    |                 | version=2   | 200
 10  | MULTI                      |                 | version=2   | 200
 11  | SET balance 250 (queued)   |                 | version=2   | 200
 12  | EXEC                       |                 | version=3   | 250
     | → Check: v2 = v2 ✓         |                 |             |
     | → COMMIT! Execute commands |                 |             |
```

---

## Complete Examples

### Example 1: Bank Transfer

**Problem:** Transfer $50 from Account A to Account B atomically.

```redis
# WITHOUT Transaction (WRONG):
GET account:A:balance        # Returns 100
GET account:B:balance        # Returns 200
SET account:A:balance 50     # If this succeeds but next fails...
SET account:B:balance 250    # ...money is inconsistent!

# WITH Transaction (CORRECT):
MULTI
DECRBY account:A:balance 50  # Queued
INCRBY account:B:balance 50  # Queued
EXEC                         # Both execute atomically
# Returns: [50, 250]
# Either both succeed or neither (if commands are valid)
```

### Example 2: E-commerce Inventory

**Problem:** Check stock, decrement only if available.

```redis
# Using WATCH for optimistic locking:
WATCH product:123:stock

# Check current stock
stock = GET product:123:stock
if stock < 1:
    UNWATCH
    return "Out of stock"

# Proceed with purchase
MULTI
DECR product:123:stock
SADD order:456:items product:123
EXEC

result = EXEC
if result == nil:
    # Another customer bought it first!
    # Retry the entire operation
    retry()
else:
    # Success!
    return "Purchase confirmed"
```

### Example 3: Rate Limiting

**Problem:** Allow only 10 requests per minute per user.

```redis
key = "rate:user:123:" + current_minute

MULTI
INCR key
EXPIRE key 60               # Auto-delete after 60 seconds
EXEC

count = result[0]
if count > 10:
    return "Rate limit exceeded"
```

### Example 4: Conditional Update with WATCH

**Problem:** Update user profile only if it hasn't changed.

```redis
# Read current profile
WATCH user:456:profile
current = GET user:456:profile

# User modifies locally (takes time)
modified = update_locally(current)

# Save back only if not modified by others
MULTI
SET user:456:profile modified
EXEC

if result == nil:
    # Profile was modified by another client
    # Show conflict resolution UI
    return "Profile changed by another session"
else:
    return "Profile updated successfully"
```

---

## Best Practices

### ✅ DO:

1. **Keep transactions short** - Queued commands execute sequentially, blocking other operations
2. **Use WATCH for read-modify-write** - Prevents race conditions
3. **Handle nil responses** - EXEC returns nil when WATCH fails
4. **Retry on conflict** - Implement retry logic with exponential backoff
5. **Minimize watched keys** - Only watch keys you actually read

```redis
# Good: Short transaction
MULTI
INCR counter
EXPIRE counter 60
EXEC

# Good: WATCH for read-modify-write
WATCH balance
val = GET balance
MULTI
SET balance (val + 100)
EXEC
```

### ❌ DON'T:

1. **Don't nest transactions** - MULTI inside MULTI is invalid
2. **Don't rely on rollback** - Errors don't rollback successful commands
3. **Don't use for complex logic** - Use Lua scripts instead
4. **Don't watch unnecessary keys** - Increases conflict probability
5. **Don't keep transactions open** - Queue commands quickly, EXEC immediately

```redis
# Bad: Long transaction (blocks others)
MULTI
SET key1 value1
... 100 more commands ...
EXEC

# Bad: No WATCH on read-modify-write
GET counter            # Another client might modify here!
MULTI
SET counter (val + 1)  # Race condition!
EXEC
```

### When to Use Lua Scripts Instead

For **complex logic**, use Lua scripts (atomic + supports conditionals):

```lua
-- Lua script: Better than transaction for complex logic
local stock = redis.call('GET', KEYS[1])
if tonumber(stock) < tonumber(ARGV[1]) then
    return 0  -- Out of stock
end
redis.call('DECRBY', KEYS[1], ARGV[1])
redis.call('SADD', KEYS[2], ARGV[2])
return 1  -- Success
```

---

## Summary

| Feature | Purpose | Use Case |
|---------|---------|----------|
| **MULTI/EXEC** | Atomic command execution | Multiple related updates |
| **WATCH** | Optimistic locking | Read-modify-write operations |
| **DISCARD** | Cancel transaction | Abort without executing |
| **UNWATCH** | Clear watches | Change transaction scope |

**Key Takeaways:**
- Redis transactions provide **atomicity**, not full ACID
- Use **WATCH** to prevent race conditions in read-modify-write
- Transactions **don't rollback** - handle errors explicitly
- Keep transactions **short and simple**
- Consider **Lua scripts** for complex conditional logic

---

## Implementation Reference

See implementation files:
- [`internal/handler/transaction.go`](../internal/handler/transaction.go) - Transaction manager and state
- [`internal/handler/pipeline.go`](../internal/handler/pipeline.go) - Transaction command handlers
- [`internal/protocol/resp.go`](../internal/protocol/resp.go) - Protocol encoding for arrays
