# Redis Cluster Implementation

## Table of Contents
- [Overview](#overview)
- [What is Redis Cluster?](#what-is-redis-cluster)
- [Why Redis Uses Cluster](#why-redis-uses-cluster)
- [Why Not Consistent Hashing?](#why-not-consistent-hashing)
- [Hash Slots Architecture](#hash-slots-architecture)
- [Implementation Details](#implementation-details)
- [Cluster Redirects](#cluster-redirects)
- [Hash Tags](#hash-tags)
- [Commands](#commands)
- [Configuration](#configuration)
- [Best Practices](#best-practices)
- [Examples](#examples)

---

## Overview

**Redis Cluster** is a distributed implementation of Redis that provides:
- ✅ **Automatic sharding** across multiple Redis instances
- ✅ **High availability** through data replication
- ✅ **Horizontal scalability** by adding more nodes
- ✅ **No single point of failure** with proper replication

**Key Features:**
- 16384 hash slots for data partitioning
- Automatic failover
- Client-side routing with redirects
- Hash tags for multi-key operations

---

## What is Redis Cluster?

Redis Cluster is a **data sharding solution** that automatically splits data across multiple Redis instances (nodes). Unlike standalone Redis (single-server) or Sentinel (high-availability), Cluster provides both **scalability** and **availability**.

### Cluster vs Other Redis Deployments

| Feature | Standalone | Sentinel | Cluster |
|---------|-----------|----------|---------|
| **Scalability** | ❌ Single server | ❌ Single master | ✅ Multiple masters |
| **Availability** | ❌ No failover | ✅ Automatic failover | ✅ Automatic failover |
| **Data Sharding** | ❌ All data on one node | ❌ All data on master | ✅ Distributed across nodes |
| **Max Memory** | Limited to one server | Limited to one server | Sum of all nodes |
| **Write Throughput** | One server's limit | One server's limit | Scales with nodes |

### Cluster Topology Example

```
Client Application
       ↓
┌──────┴──────┬──────┬──────┐
│             │      │      │
Node 1        Node 2  Node 3
Slots:        Slots:  Slots:
0-5460        5461-   10923-
              10922   16383
```

Each node:
- Handles a subset of hash slots
- Can have replicas for failover
- Communicates with other nodes via cluster bus

---

## Why Redis Uses Cluster

### 1. **Memory Limitations** 💾

**Problem:** Single Redis instance limited by server RAM

```
Single Server:
  Max RAM: 64 GB
  Max Keys: ~100M keys (depends on data size)

Cluster (3 nodes):
  Total RAM: 192 GB (3 × 64 GB)
  Max Keys: ~300M keys
```

**Solution:** Distribute data across multiple nodes

---

### 2. **Write Scalability** 📈

**Problem:** Single Redis instance bottleneck for writes

```
Single Master:
  Writes/sec: ~100,000 (typical server)

Cluster (3 masters):
  Writes/sec: ~300,000 (scales linearly)
```

**Solution:** Each master handles its own write traffic

---

### 3. **High Availability** 🛡️

**Problem:** Server failure = total outage

```
Standalone:
  Node fails → Data loss + Downtime

Cluster (with replicas):
  Master fails → Replica promoted
  → Minimal downtime (~1-2 seconds)
  → No data loss (if replicated)
```

**Solution:** Automatic failover via replica promotion

---

### 4. **Geographic Distribution** 🌍

**Problem:** High latency for global users

```
Single datacenter:
  US users: 10ms latency
  EU users: 150ms latency  ❌

Multi-datacenter cluster:
  US users → US nodes: 10ms  ✅
  EU users → EU nodes: 10ms  ✅
```

**Solution:** Deploy nodes closer to users

---

## Sentinel vs Cluster - Mutually Exclusive

### They Are NOT Compatible

**Important:** You use **either** Sentinel **or** Cluster mode, **never both together**.

```
❌ CANNOT USE BOTH:
Sentinel + Cluster = INCOMPATIBLE

✅ CHOOSE ONE:
Sentinel (HA only) OR Cluster (HA + Sharding)
```

### Architecture Comparison

#### Sentinel Mode (Single Master)

```
┌────────────────┐
│  Sentinel 1    │ ←──┐
└────────────────┘    │
                      │ Monitor & Failover
┌────────────────┐    │
│  Sentinel 2    │ ←──┤
└────────────────┘    │
                      │
┌────────────────┐    │
│  Sentinel 3    │ ←──┤
└────────────────┘    │
                      ↓
              ┌──────────────┐
              │    Master    │ ← ALL data on ONE master
              │ All 16384    │   (no sharding)
              │    slots     │
              └──────────────┘
                   ↓     ↓
              ┌────┐   ┌────┐
              │Rep1│   │Rep2│
              └────┘   └────┘

Characteristics:
- Single master handles ALL writes
- ALL 16,384 slots on one master
- Separate Sentinel processes monitor health
- Sentinels vote for failover
- No data sharding/partitioning
- Scales reads (via replicas), NOT writes
```

#### Cluster Mode (Multiple Masters)

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│   Master 1   │  │   Master 2   │  │   Master 3   │
│   Slots:     │  │   Slots:     │  │   Slots:     │
│   0-5460     │  │   5461-10922 │  │  10923-16383 │
└──────────────┘  └──────────────┘  └──────────────┘
       ↓                 ↓                 ↓
   ┌──────┐          ┌──────┐          ┌──────┐
   │Rep 1A│          │Rep 2A│          │Rep 3A│
   └──────┘          └──────┘          └──────┘

Characteristics:
- Multiple masters (3+ nodes)
- Hash slots distributed across masters
- NO Sentinel needed (built-in failover)
- Nodes monitor each other (cluster bus)
- Data sharded/partitioned
- Scales BOTH reads AND writes
```

### Why They're Incompatible

| Aspect | Sentinel | Cluster | Can Combine? |
|--------|----------|---------|-------------|
| **Masters** | 1 | 3+ | ❌ Conflict |
| **Hash slots** | All on master | Distributed | ❌ Different model |
| **Failover mechanism** | External Sentinel | Built-in | ❌ Redundant |
| **Data distribution** | None (replicas copy all) | Sharded | ❌ Incompatible |
| **Client routing** | Simple (one master) | Redirects (MOVED/ASK) | ❌ Different protocol |
| **Write scaling** | No | Yes | ❌ Architectural difference |

### Detailed Differences

#### 1. **Master Count**

```
Sentinel:
  ONE master at any time
  - Handles 100% of writes
  - Replicas are read-only copies
  - When master fails, ONE replica promoted

Cluster:
  MULTIPLE masters (3 minimum)
  - Each handles 1/N of writes
  - Each owns subset of slots
  - Independent failover per master
```

#### 2. **Data Distribution**

```
Sentinel:
  Master: keys 1, 2, 3, 4, 5... ALL keys
  Replica 1: keys 1, 2, 3, 4, 5... (full copy)
  Replica 2: keys 1, 2, 3, 4, 5... (full copy)
  → No sharding, full replication

Cluster:
  Master 1: keys in slots 0-5460
  Master 2: keys in slots 5461-10922
  Master 3: keys in slots 10923-16383
  → Sharded, each master has different data
```

#### 3. **Failover Process**

```
Sentinel:
  Master fails
    ↓
  Sentinels detect (quorum vote)
    ↓
  Sentinels elect new master
    ↓
  One replica promoted
    ↓
  Clients redirected to new master
  
Cluster:
  Master 2 fails
    ↓
  Other nodes detect via cluster bus
    ↓
  Master 2's replica auto-promoted
    ↓
  Master 2's slots now served by replica
    ↓
  Other masters unaffected (continue serving)
```

#### 4. **Scaling Capabilities**

```
Sentinel:
  Data grows → Upgrade master RAM ✅
  Writes grow → Cannot scale writes ❌
  Reads grow → Add more replicas ✅
  
Cluster:
  Data grows → Add more masters ✅
  Writes grow → Add more masters ✅
  Reads grow → Add replicas per master ✅
```

### Decision Matrix: Which to Choose?

#### ✅ Use Sentinel When:

```
1. Data fits on one server (<64 GB)
   Example: 10GB dataset, 32GB RAM server

2. Write traffic manageable on one node
   Example: <50,000 writes/sec

3. Simpler operations preferred
   Example: Small team, simple deployment

4. Read scaling needed (not write)
   Example: 10:1 read:write ratio

5. Don't need sharding
   Example: All data accessed together
```

#### ✅ Use Cluster When:

```
1. Data exceeds single server
   Example: 200GB dataset across 4 nodes

2. Need write scaling
   Example: 500,000 writes/sec distributed

3. Horizontal scaling required
   Example: Start with 3 nodes, grow to 20

4. Geographic distribution
   Example: Nodes in US, EU, Asia

5. Future growth expected
   Example: Startup expecting 10x growth
```

### Migration Path

**Can you migrate from Sentinel to Cluster?**

```
Yes, but requires complete data migration:

1. Setup new Cluster (3+ nodes)
2. Assign slots to cluster nodes
3. Migrate data from Sentinel master to Cluster
   - Use MIGRATE command or redis-cli --cluster import
4. Update clients to use cluster protocol
5. Decommission Sentinel setup

⚠️ This is a major change, not a simple upgrade!
```

### Configuration Examples

#### Sentinel Configuration

```bash
# redis.conf (Master)
port 6379
bind 0.0.0.0

# sentinel.conf
sentinel monitor mymaster 127.0.0.1 6379 2
sentinel down-after-milliseconds mymaster 5000
sentinel parallel-syncs mymaster 1
sentinel failover-timeout mymaster 10000
```

#### Cluster Configuration

```bash
# redis.conf (Each node)
port 7000
cluster-enabled yes
cluster-config-file nodes-7000.conf
cluster-node-timeout 5000

# No sentinel.conf needed!
```

### Common Misconceptions

#### ❌ Myth 1: "Use Sentinel for monitoring Cluster"

```
Wrong! Cluster has built-in monitoring.
Each node monitors others via cluster bus.
Sentinel is unnecessary and won't work.
```

#### ❌ Myth 2: "Cluster is just Sentinel with sharding"

```
Wrong! Completely different architectures.
Sentinel: External monitoring process
Cluster: Distributed consensus built-in
```

#### ❌ Myth 3: "Can run both for extra reliability"

```
Wrong! They use different protocols.
Cluster mode disables Sentinel compatibility.
Choose one based on your needs.
```

### Summary Table

| Feature | Sentinel | Cluster |
|---------|----------|----------|
| **Purpose** | High Availability | HA + Horizontal Scaling |
| **Masters** | 1 | 3+ |
| **Data model** | Full replication | Sharded (hash slots) |
| **Failover** | External Sentinel | Built-in |
| **Max data** | Single server RAM | Sum of all masters |
| **Write scaling** | ❌ No | ✅ Yes |
| **Read scaling** | ✅ Yes (replicas) | ✅ Yes (replicas) |
| **Complexity** | Lower | Higher |
| **Use case** | Small-medium datasets | Large datasets, high throughput |
| **Can combine?** | ❌ **NO - MUTUALLY EXCLUSIVE** | ❌ **NO - MUTUALLY EXCLUSIVE** |

---

## Why Not Consistent Hashing?

Redis Cluster uses **hash slots** instead of **consistent hashing**. Here's why:

### Consistent Hashing Overview

```
Consistent hashing maps keys to a circular hash space:

          Node A
             │
    Key1─────┼─────Key2
             │
          Node B
```

**How it works:**
1. Hash nodes and keys to a circle (0-2^32)
2. Key belongs to the first node clockwise
3. Adding/removing nodes affects only adjacent keys

### Why Redis Chose Hash Slots Instead

#### ❌ Problem 1: **Uneven Distribution**

**Consistent hashing:**
```
Node A: 45% of keys  ⚠️ Unbalanced!
Node B: 30% of keys
Node C: 25% of keys
```

Nodes get random portions of the ring, leading to imbalance.

**Hash slots (Redis):**
```
Node A: 5461 slots (33.3%)  ✅ Balanced!
Node B: 5461 slots (33.3%)
Node C: 5462 slots (33.4%)
```

Slots can be evenly distributed by design.

---

#### ❌ Problem 2: **Manual Rebalancing**

**Consistent hashing:**
- Adding a node affects random key ranges
- Hard to predict which keys move
- Difficult to manually manage

**Hash slots:**
```
# Move specific slots from Node A to Node D
CLUSTER ADDSLOTS 0 1 2 3 4 ... 1365  # Exactly 1/4 of slots
```
- Explicit control over data movement
- Predictable migration
- Can move one slot at a time

---

#### ❌ Problem 3: **Replica Management**

**Consistent hashing:**
- Replicas must track the ring
- Complex replica placement logic
- Difficult to handle multi-datacenter

**Hash slots:**
```
Master A owns slots 0-5460
  ↓
Replica A1 mirrors slots 0-5460  (simple!)
```
- Replicas mirror exact slot ranges
- Clear ownership model

---

#### ❌ Problem 4: **Multi-Key Operations**

**Consistent hashing:**
```
MGET user:1 user:2 user:3
  user:1 → Node A
  user:2 → Node C  ❌ Can't execute atomically!
  user:3 → Node B
```

**Hash slots with hash tags:**
```
MGET {user}:1 {user}:2 {user}:3
  All hash to same slot → Same node  ✅
```

---

### Why Hash Slots Win

| Feature | Consistent Hashing | Hash Slots |
|---------|-------------------|------------|
| **Distribution** | Random (uneven) | Configurable (even) |
| **Rebalancing** | Automatic but unpredictable | Manual but precise |
| **Slot Count** | Infinite | 16384 (fixed) |
| **Multi-key** | Complex with tags | Simple with tags |
| **Migration** | Node-to-node | Slot-by-slot |
| **Overhead** | Low | Minimal (2KB slot map) |

**Redis chose hash slots for:**
1. **Predictability** - Know exactly which slots move
2. **Simplicity** - Fixed 16384 slots, easy to reason about
3. **Control** - Manual migration for zero downtime
4. **Performance** - Fast slot lookup (O(1))

---

## Hash Slots Architecture

### Overview

Redis Cluster divides the key space into **16384 hash slots** (numbered 0-16383).

```
Total slots: 16384
Slot calculation: CRC16(key) % 16384

Example:
  CRC16("user:1000") = 47892
  47892 % 16384 = 14100
  → Slot 14100
```

### Why 16384 Slots?

**Q:** Why not 16K (16,384) slots specifically?

**A:** Optimal tradeoff between:
1. **Memory overhead** (slot map size)
2. **Migration granularity** (how much data per slot)
3. **Rebalancing flexibility**

#### Memory Overhead

```
Slot map size = 16384 bits = 2048 bytes = 2 KB

With 1000 nodes:
  Each node stores cluster state: 2 KB  ✅ Tiny!

If we used 65536 slots (64K):
  Cluster state: 8 KB per node
  With 1000 nodes = 8 MB overhead  ⚠️
```

#### Migration Granularity

```
With 10M keys distributed across 16384 slots:
  ~610 keys per slot

Moving one slot = moving ~610 keys
  → Fine-grained control  ✅

With 1024 slots:
  ~9,765 keys per slot
  → Moving one slot = too many keys!  ❌
```

#### Scalability

```
Max useful nodes:
  16384 slots / 16384 nodes = 1 slot per node

Practical limit:
  16384 slots / 100 slots per node = 163 nodes

Real-world clusters:
  Typically 3-50 nodes
  → 16384 slots provides plenty of flexibility  ✅
```

---

### Slot Distribution Formula

**Recommended distribution for N nodes:**

```
Slots per node = 16384 / N (rounded)

Examples:
  3 nodes:  5461, 5461, 5462 slots
  4 nodes:  4096, 4096, 4096, 4096 slots
  5 nodes:  3277, 3277, 3276, 3277, 3277 slots
  10 nodes: 1638 slots each (with 4 extras)
```

### Slots Per Node Limits

| Nodes | Slots/Node | Use Case |
|-------|------------|----------|
| **1** | 16384 | Development only (not HA) |
| **3** | ~5461 | Minimum production (HA) |
| **6** | ~2731 | Small production (3 masters + 3 replicas) |
| **10** | ~1638 | Medium production |
| **20** | ~819 | Large production |
| **50** | ~327 | Very large deployment |
| **100** | ~163 | Extreme scale |
| **1000** | ~16 | Theoretical maximum |

**Practical recommendations:**
- **Minimum:** 3 nodes (for failover quorum)
- **Optimal:** 6-12 nodes (balance between overhead and scalability)
- **Maximum (practical):** ~100 nodes (beyond this, overhead grows)

---

## Implementation Details

### Core Components

#### 1. **Slot Calculation** (`slot.go`)

```go
// Calculate hash slot for a key
func KeyHashSlot(key string) int {
    hashKey := extractHashTag(key)  // Handle {tags}
    return int(crc16([]byte(hashKey)) % 16384)
}
```

**CRC16 Algorithm:**
- Uses XMODEM variant (same as Redis)
- Fast: ~1-2 CPU cycles per byte
- Good distribution across 16384 slots
- Lookup table for performance

**Hash tag extraction:**
```go
"user:1000"           → hash("user:1000")
"{user}:1000"         → hash("user")
"{user:1000}:profile" → hash("user:1000")
"user:{}"             → hash("user:{}")  (empty tag ignored)
```

---

#### 2. **Node Management** (`node.go`)

```go
type Node struct {
    ID      string   // 40-char hex ID
    Address string   // IP address
    Port    int      // Client port
    Slots   []int    // Owned slots
    Flags   []string // master, slave, myself, fail
}

type Cluster struct {
    MySelf   *Node
    Nodes    map[string]*Node
    SlotMap  [16384]string  // slot → nodeID
    State    ClusterState   // ok | fail
    Enabled  bool
}
```

**Slot ownership:**
```go
// Assign slots 0-5460 to current node
cluster.AssignSlotRange(0, 5460)

// Check if node owns a key
owns := cluster.IsKeyOwner("user:1000")
```

---

#### 3. **Redirect Logic** (`redirect.go`)

```go
type RedirectError struct {
    Type    RedirectType  // MOVED | ASK
    Slot    int
    Address string
    Port    int
}
```

**Redirect flow:**
```
Client → Node A: GET user:1000
         ↓
Node A checks slot ownership:
  - Owns slot? → Return value
  - Doesn't own? → Return redirect
         ↓
-MOVED 14100 192.168.1.2:6379
         ↓
Client → Node B: GET user:1000
         ↓
Node B: Returns value ✅
```

---

## Cluster Redirects

### MOVED Redirect

**When:** Slot has been **permanently migrated** to another node

```
Client: GET user:1000
Node A: -MOVED 14100 192.168.1.2:6379

Meaning:
  - Slot 14100 belongs to 192.168.1.2:6379
  - Client should UPDATE slot cache
  - Retry on correct node
```

**Client behavior:**
1. Parse redirect
2. Update local slot map: `slot 14100 → 192.168.1.2:6379`
3. Reconnect to new node
4. Retry command
5. Future requests for slot 14100 go directly to correct node

---

### ASK Redirect - Deep Dive

**When:** Slot is being **migrated** (temporary state during slot migration)

#### What is ASK?

ASK is a **temporary redirect** that occurs during slot migration when a key may have already been moved to the target node, but the migration is not yet complete.

```
Client: GET user:1000
Node A: -ASK 14100 192.168.1.2:6379

Meaning:
  - Slot 14100 is CURRENTLY MIGRATING to 192.168.1.2:6379
  - This key might be on the target node already
  - This is ONE-TIME redirect (don't cache)
  - Send ASKING before retry
  - Migration not complete yet
```

#### Migration States

During migration, slots exist in special states:

```
State 1: STABLE (before migration)
  Node A: slot 14100 = STABLE (owns it)
  Node B: slot 14100 = (doesn't know about it)
  
State 2: MIGRATING/IMPORTING (during migration)
  Node A: slot 14100 = MIGRATING to Node B
  Node B: slot 14100 = IMPORTING from Node A
  
State 3: STABLE (after migration)
  Node A: slot 14100 = (removed)
  Node B: slot 14100 = STABLE (owns it)
```

#### The ASK Flow

**Scenario:** Migrating slot 14100 from Node A to Node B

```redis
# Step 1: Start migration
Node A> CLUSTER SETSLOT 14100 MIGRATING <node-b-id>
OK

Node B> CLUSTER SETSLOT 14100 IMPORTING <node-a-id>
OK

# Step 2: Migrate keys one by one
Node A> MIGRATE 192.168.1.2 6379 "user:1000" 0 5000
OK  # user:1000 now exists on Node B

# Step 3: Client requests the migrated key
Client → Node A> GET user:1000
-ASK 14100 192.168.1.2:6379  # Key not found, probably migrated

# Step 4: Client follows redirect
Client → Node B> ASKING
OK

Client → Node B> GET user:1000
"John Doe"  ✅
```

#### Why ASKING Command?

Node B is in **IMPORTING** state - it doesn't officially own slot 14100 yet.

**Without ASKING:**
```redis
Client → Node B> GET user:1000
-MOVED 14100 192.168.1.1:6379  # Rejected! Points back to Node A
# Infinite loop! ❌
```

**With ASKING:**
```redis
Client → Node B> ASKING
OK  # Sets a one-time flag

Client → Node B> GET user:1000
"John Doe"  # Flag allows this request ✅

Client → Node B> GET user:2000
-MOVED 14100 192.168.1.1:6379  # Flag cleared, normal behavior
```

**ASKING is a one-time permission:** *"I know you don't own this slot yet, but Node A sent me, so please serve this ONE request."*

#### Node A's Decision Logic

```go
// Pseudo-code for Node A during migration
func handleGet(key string) {
    slot := KeyHashSlot(key)
    
    if slot.State == MIGRATING {
        // Check if key still exists locally
        if exists(key) {
            // Key not migrated yet, serve it
            return getValue(key)  ✅
        } else {
            // Key already migrated, redirect
            return ASKError(slot, targetNode)  ➡️
        }
    }
    
    // Normal case
    return getValue(key)
}
```

**Two cases during migration:**

**Case A: Key NOT yet migrated**
```redis
Client → Node A> GET user:5000
Node A: (key exists locally) → Returns "value" ✅
# No redirect, normal operation
```

**Case B: Key already migrated**
```redis
Client → Node A> GET user:1000
Node A: (key missing) → -ASK 14100 192.168.1.2:6379
# Key was migrated, redirect to Node B
```

#### Node B's Decision Logic

```go
// Pseudo-code for Node B during migration
func handleGet(key string) {
    slot := KeyHashSlot(key)
    
    if slot.State == IMPORTING {
        if askingFlagSet {
            // ASKING was sent, allow this request
            clearAskingFlag()
            return getValue(key)  ✅
        } else {
            // No ASKING, reject and redirect back
            return MOVEDError(slot, sourceNode)  ➡️
        }
    }
    
    // Normal case
    return getValue(key)
}
```

#### Complete Migration Timeline

```
Time 0: Normal operation
  Node A owns slot 14100 (1000 keys)
  Node B owns nothing for slot 14100
  
Time 1: Start migration
  CLUSTER SETSLOT 14100 MIGRATING node-b  (on A)
  CLUSTER SETSLOT 14100 IMPORTING node-a  (on B)
  
Time 2-100: Migrate keys incrementally
  MIGRATE user:1000 → Node B has 1 key, Node A has 999
  MIGRATE user:1001 → Node B has 2 keys, Node A has 998
  ...
  
  During this time:
    - GET user:1000 on Node A → ASK redirect to Node B
    - GET user:5000 on Node A → Served by Node A (not migrated yet)
    - Clients use ASKING when redirected
  
Time 100: Migration complete
  CLUSTER SETSLOT 14100 NODE node-b  (on both nodes)
  
Time 101+: Normal operation
  - All slot 14100 keys on Node B
  - Node A returns MOVED for any slot 14100 requests
  - Clients update their slot cache
  - No more ASK redirects
```

#### ASK vs MOVED - Critical Differences

| Aspect | MOVED | ASK |
|--------|-------|-----|
| **Trigger** | Slot ownership changed (permanent) | Slot migrating (temporary) |
| **Client cache** | ✅ Update slot map | ❌ Do NOT update |
| **ASKING needed** | ❌ No | ✅ Yes, before every retry |
| **Persistence** | Permanent redirect | One-time redirect |
| **Node state** | Target owns slot (STABLE) | Target importing slot (IMPORTING) |
| **Frequency** | Once per slot per client | Multiple times during migration |
| **Intent** | "Slot moved, update your map" | "Try the other node this one time" |

#### Client Implementation

```go
// Proper client handling of ASK
func get(key string) (string, error) {
    slot := KeyHashSlot(key)
    node := slotMap[slot]  // Get node from cache
    
    resp, err := node.Execute("GET", key)
    if err != nil {
        return "", err
    }
    
    if resp.IsASK() {
        // ASK redirect - don't update cache!
        targetNode := resp.GetTargetNode()
        
        // Send ASKING first
        targetNode.Execute("ASKING")
        
        // Retry the command
        resp, err = targetNode.Execute("GET", key)
        
        // Still use original node for next request
        // (slotMap unchanged)
        return resp.Value, err
    }
    
    if resp.IsMOVED() {
        // MOVED redirect - update cache
        targetNode := resp.GetTargetNode()
        slotMap[resp.Slot] = targetNode  // Update cache ✅
        
        // Retry on correct node
        resp, err = targetNode.Execute("GET", key)
        return resp.Value, err
    }
    
    return resp.Value, nil
}
```

#### Migration Example with Timeline

```redis
# T=0: Initial state
Node A (127.0.0.1:7000) owns slot 14100
Node B (127.0.0.1:7001) empty

# T=1: Prepare migration
NodeA> CLUSTER SETSLOT 14100 MIGRATING <node-b-id>
OK

NodeB> CLUSTER SETSLOT 14100 IMPORTING <node-a-id>
OK

# T=2: Migrate first key
NodeA> MIGRATE 127.0.0.1 7001 user:1000 0 5000
OK

# T=3: Client tries to get migrated key from Node A
Client→NodeA> GET user:1000
-ASK 14100 127.0.0.1:7001  # Not found, redirect

# T=4: Client follows ASK redirect
Client→NodeB> ASKING
OK

Client→NodeB> GET user:1000
"John Doe"  ✅

# T=5: Client tries another key (not migrated yet)
Client→NodeA> GET user:2000
"Jane Doe"  ✅  # Still on Node A

# T=6: Migrate second key
NodeA> MIGRATE 127.0.0.1 7001 user:2000 0 5000
OK

# T=7: Client tries user:2000 again
Client→NodeA> GET user:2000
-ASK 14100 127.0.0.1:7001  # Now redirected

# T=100: All keys migrated, finalize
NodeA> CLUSTER SETSLOT 14100 NODE <node-b-id>
OK

NodeB> CLUSTER SETSLOT 14100 NODE <node-b-id>
OK

# T=101: Migration complete, now MOVED
Client→NodeA> GET user:1000
-MOVED 14100 127.0.0.1:7001  # Permanent redirect now

# T=102: Client updates cache
slotMap[14100] = NodeB  ✅

# T=103: Future requests go directly to Node B
Client→NodeB> GET user:1000
"John Doe"  ✅
```

#### Why Temporary Redirects Matter

**Problem without ASK:**
```
Migration fails halfway
  → Some keys on Node A
  → Some keys on Node B
  → If clients cached Node B, they'd miss keys still on Node A
  → Data loss!
```

**Solution with ASK:**
```
Clients don't cache during migration
  → Always check Node A first
  → Node A knows which keys moved
  → Redirects to Node B only if needed
  → If migration fails, rollback is safe
```

#### Best Practices

1. **Always send ASKING before retry**
   ```
   ✅ ASKING → GET key
   ❌ GET key (will fail)
   ```

2. **Don't cache ASK redirects**
   ```
   ✅ Keep using original node in slot map
   ❌ Update slot map on ASK
   ```

3. **ASKING is one-time**
   ```
   ASKING → GET key1  ✅ Works
   GET key2  ❌ Fails (need new ASKING)
   ```

4. **Handle both MOVED and ASK**
   ```go
   if ASK → ASKING + retry (don't cache)
   if MOVED → retry + cache update
   ```

#### Summary

**ASKING tells Node B:** *"I know you don't own this slot yet, but Node A sent me here. Please serve this request anyway, just this once."*

**ASK redirect means:**
- ✅ Slot is migrating (temporary state)
- ✅ Key might be on target node
- ✅ Send ASKING before retry
- ❌ Don't update slot cache
- ❌ Not a permanent redirect

**The flow:**
```
Client → Node A (source)
    ↓
Node A: "Key moved, ASK Node B"
    ↓
Client → Node B: ASKING
    ↓
Client → Node B: GET key
    ↓
Node B: Returns value
    ↓
Client remembers: Next time still try Node A first
```

---

### MOVED vs ASK Comparison

| Aspect | MOVED | ASK |
|--------|-------|-----|
| **State** | Permanent | Temporary |
| **Update cache** | ✅ Yes | ❌ No |
| **ASKING required** | ❌ No | ✅ Yes |
| **Migration state** | Complete | In progress |
| **Frequency** | Once after migration | Multiple during migration |

---

### Redirect Examples

#### Example 1: Simple MOVED

```redis
# Client has wrong slot mapping
127.0.0.1:7000> GET user:1000
-MOVED 14100 127.0.0.1:7001

# Client updates cache and retries
127.0.0.1:7001> GET user:1000
"John Doe"
```

#### Example 2: Slot Migration (ASK)

```redis
# Migration in progress: slot 14100 moving from Node A to Node B

# Client asks Node A
127.0.0.1:7000> GET user:1000
-ASK 14100 127.0.0.1:7001

# Client must send ASKING first
127.0.0.1:7001> ASKING
OK
127.0.0.1:7001> GET user:1000
"John Doe"

# Next request still goes to Node A (no cache update)
# Until migration completes and MOVED is returned
```

---

## Hash Tags

Hash tags allow **multi-key operations** on the same node by controlling which part of the key is hashed.

### Syntax

```
{tag}rest_of_key
```

Only the content inside `{}` is hashed to determine the slot.

### Examples

```redis
# Without hash tags (keys on different nodes)
SET user:1000:profile "..."   # Slot: CRC16("user:1000:profile") % 16384
SET user:1000:sessions "..."  # Slot: CRC16("user:1000:sessions") % 16384
# These might be on different nodes!  ❌

# With hash tags (keys on same node)
SET {user:1000}:profile "..."   # Slot: CRC16("user:1000") % 16384
SET {user:1000}:sessions "..."  # Slot: CRC16("user:1000") % 16384
# Both on same node!  ✅
```

### Use Cases

#### 1. **Multi-Key Operations**

```redis
# Atomic operations on related keys
MGET {user:1000}:name {user:1000}:email {user:1000}:age
# All keys on same node → Works!  ✅

# Without tags - fails in cluster
MGET user:1000:name user:1000:email user:1000:age
-CROSSSLOT Keys in request don't hash to the same slot
```

#### 2. **Transactions**

```redis
MULTI
SET {order:42}:status "paid"
INCRBY {order:42}:total 100
SADD {order:42}:items "item-5"
EXEC
# All operations on same node → Atomic!  ✅
```

#### 3. **Pipelines**

```redis
# Pipeline multiple commands for same entity
PIPELINE
GET {session:abc123}:user_id
GET {session:abc123}:cart
GET {session:abc123}:preferences
# All on same node → Single round trip!  ✅
```

### Hash Tag Rules

```redis
# Valid hash tags
"{user}:1000"         → hash("user")
"{user:1000}"         → hash("user:1000")
"prefix{tag}suffix"   → hash("tag")

# Invalid/Ignored hash tags
"user:1000"           → hash("user:1000")  (no braces)
"{}"                  → hash("{}")  (empty tag)
"{user}{name}"        → hash("user")  (first tag only)
"user{1000"           → hash("user{1000")  (no closing brace)
```

---

## Commands

### CLUSTER SLOTS

Returns slot-to-node mapping.

**Syntax:**
```
CLUSTER SLOTS
```

**Response:**
```
1) 1) (integer) 0
   2) (integer) 5460
   3) 1) "127.0.0.1"
      2) (integer) 7000
      3) "node-id-abc..."

2) 1) (integer) 5461
   2) (integer) 10922
   3) 1) "127.0.0.1"
      2) (integer) 7001
      3) "node-id-def..."
```

**Format:** `[start_slot, end_slot, [ip, port, id]]`

---

### CLUSTER NODES

Returns cluster topology.

**Syntax:**
```
CLUSTER NODES
```

**Response:**
```
abc123... 127.0.0.1:7000@17000 myself,master - 0 0 1 connected 0-5460
def456... 127.0.0.1:7001@17001 master - 0 1234567890 2 connected 5461-10922
ghi789... 127.0.0.1:7002@17002 master - 0 1234567891 3 connected 10923-16383
```

**Format:** `id host:port@bus flags master ping pong epoch state slots`

---

### CLUSTER KEYSLOT

Returns the hash slot for a key.

**Syntax:**
```
CLUSTER KEYSLOT key
```

**Examples:**
```redis
CLUSTER KEYSLOT "user:1000"
(integer) 14100

CLUSTER KEYSLOT "{user}:1000"
(integer) 9588

CLUSTER KEYSLOT "{user}:2000"
(integer) 9588  # Same slot!
```

---

### CLUSTER INFO

Returns cluster state information.

**Syntax:**
```
CLUSTER INFO
```

**Response:**
```
cluster_state:ok
cluster_slots_assigned:16384
cluster_slots_ok:16384
cluster_slots_pfail:0
cluster_slots_fail:0
cluster_known_nodes:3
cluster_size:3
cluster_current_epoch:1
cluster_my_epoch:1
```

---

### CLUSTER ADDSLOTS

Assigns slots to the current node.

**Syntax:**
```
CLUSTER ADDSLOTS slot [slot ...]
```

**Example:**
```redis
# Assign slots 0-5460 to current node
CLUSTER ADDSLOTS 0 1 2 3 ... 5460
OK
```

---

### CLUSTER MYID

Returns the node ID of the current node.

**Syntax:**
```
CLUSTER MYID
```

**Response:**
```
"abc1234567890def..."
```

---

## Configuration

### Enabling Cluster Mode

```go
// In server initialization
cluster := cluster.NewCluster(nodeID, address, port)
cluster.Enable()
store.Cluster = cluster

// Assign slots
cluster.AssignSlotRange(0, 5460)
```

### Cluster Setup Example

```bash
# Node 1 (127.0.0.1:7000)
CLUSTER ADDSLOTS 0 1 2 ... 5460

# Node 2 (127.0.0.1:7001)
CLUSTER ADDSLOTS 5461 5462 ... 10922

# Node 3 (127.0.0.1:7002)
CLUSTER ADDSLOTS 10923 10924 ... 16383
```

---

## Best Practices

### 1. **Use Hash Tags Wisely** 🏷️

```redis
# ✅ Good: Related data together
SET {user:1000}:profile "..."
SET {user:1000}:settings "..."

# ❌ Bad: Everything on one node
SET {global}:user:1000 "..."
SET {global}:product:500 "..."
# All keys with {global} on same node → Hotspot!
```

---

### 2. **Plan Slot Distribution** 📊

```redis
# ✅ Even distribution
Node 1: 5461 slots (33.3%)
Node 2: 5461 slots (33.3%)
Node 3: 5462 slots (33.4%)

# ❌ Uneven distribution
Node 1: 10000 slots (61%)  # Overloaded!
Node 2: 3000 slots (18%)
Node 3: 3384 slots (21%)
```

---

### 3. **Monitor Redirects** 📈

```
High redirect rate = poor client caching
  → Update client slot map
  → Check if migrations happening
```

---

### 4. **Avoid Cross-Slot Operations** ⚠️

```redis
# ❌ Fails in cluster
MGET user:1 user:2 user:3
-CROSSSLOT Keys in request don't hash to the same slot

# ✅ Use hash tags
MGET {users}:1 {users}:2 {users}:3
```

---

### 5. **Plan for Growth** 📈

```
Start: 3 nodes (5461 slots each)
       ↓
Grow:  6 nodes (2731 slots each)
       → Move ~2730 slots from each old node to new nodes
       ↓
Grow:  12 nodes (1365 slots each)
       → Move ~1366 slots again
```

---

## Examples

### Example 1: Basic Cluster Setup

```redis
# Node 1
CLUSTER ADDSLOTS 0 1 2 ... 5460
CLUSTER INFO
# cluster_state:fail (not all slots assigned)

# Node 2
CLUSTER ADDSLOTS 5461 5462 ... 10922

# Node 3
CLUSTER ADDSLOTS 10923 10924 ... 16383

# Now all nodes show:
CLUSTER INFO
# cluster_state:ok
```

---

### Example 2: Finding Key's Node

```redis
# Which node owns "user:1000"?
CLUSTER KEYSLOT "user:1000"
(integer) 14100

# Check slot map
CLUSTER SLOTS
# ... slot 14100 is on 127.0.0.1:7002
```

---

### Example 3: Multi-Key with Hash Tags

```redis
# Set related data with hash tags
SET {user:1000}:name "Alice"
SET {user:1000}:email "alice@example.com"
SET {user:1000}:age "30"

# Fetch atomically
MGET {user:1000}:name {user:1000}:email {user:1000}:age
1) "Alice"
2) "alice@example.com"
3) "30"
```

---

### Example 4: Handling Redirects

```go
// Client code (pseudo)
func get(key string) {
    node := slotMap[KeyHashSlot(key)]
    
    response := node.Execute("GET", key)
    
    if response.IsRedirect() {
        if response.Type == "MOVED" {
            // Update cache permanently
            slotMap[response.Slot] = response.Node
        }
        // Retry on correct node
        return response.Node.Execute("GET", key)
    }
    
    return response.Value
}
```

---

## Performance Considerations

### Slot Lookup

```
Slot calculation: O(n) where n = key length
  - Typically 10-50 bytes → ~100 CPU cycles
  - Negligible overhead
```

### Redirect Overhead

```
Normal request: 1 RTT
MOVED redirect: 2 RTT (initial + retry)
ASK redirect:   2 RTT (initial + ASKING + retry)

With good client caching:
  - 99.9%+ requests hit correct node first
  - < 0.1% require redirects
```

### Memory Overhead

```
Per-node cluster state:
  Slot map: 2 KB (16384 bits)
  Node info: ~1 KB per node
  
3-node cluster:
  Total overhead: ~5 KB per node
  
100-node cluster:
  Total overhead: ~102 KB per node
```

---

## Limitations

### 1. **Multi-Database Not Supported**

```redis
# Redis standalone has 16 databases (SELECT 0-15)
# Cluster: ONLY database 0
SELECT 1
-ERR SELECT is not allowed in cluster mode
```

---

### 2. **Multi-Key Operations Limited**

```redis
# Only works if all keys in same slot
MGET key1 key2 key3  # ❌ Fails unless same slot
MGET {tag}:1 {tag}:2 {tag}:3  # ✅ Works
```

---

### 3. **No Cross-Slot Transactions**

```redis
MULTI
SET user:1 "Alice"
SET user:2 "Bob"
EXEC
-ERR MULTI/EXEC only allowed for keys in same slot
```

---

## Conclusion

Redis Cluster provides:
✅ **Horizontal scalability** through data sharding
✅ **High availability** via automatic failover
✅ **Predictable performance** with hash slots
✅ **Operational flexibility** with manual slot migration

**When to use:**
- Data exceeds single server memory
- Need write scalability
- Require high availability
- Geographic distribution needed

**When NOT to use:**
- Small datasets (<10 GB)
- Simple applications
- Heavy multi-key operations without hash tags
- Need multiple databases

---

## Further Reading

- [Redis Cluster Tutorial](https://redis.io/docs/management/scaling/)
- [Redis Cluster Specification](https://redis.io/docs/reference/cluster-spec/)
- [CRC16 Implementation](https://en.wikipedia.org/wiki/Cyclic_redundancy_check)
- [Consistent Hashing vs Hash Slots](https://redis.io/docs/management/scaling/#redis-cluster-data-sharding)

---
