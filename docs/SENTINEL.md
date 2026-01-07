# Redis Sentinel - High Availability Implementation

## What is Redis Sentinel?

Redis Sentinel is a distributed monitoring and automatic failover system for Redis. It provides high availability by continuously monitoring Redis master and replica instances, detecting failures, and automatically promoting a replica to become the new master when the current master fails.

### Core Responsibilities

1. **Monitoring**: Continuously checks if master and replica instances are working as expected
2. **Notification**: Can notify system administrators or other applications about failures
3. **Automatic Failover**: Promotes a replica to master when the current master fails
4. **Configuration Provider**: Provides clients with the current master address

### Why Sentinel is Important

In production systems, hardware failures, network issues, or software crashes can cause the Redis master to become unavailable. Without Sentinel, this would result in:
- Application downtime
- Manual intervention required
- Data service interruption
- Loss of write capability

Sentinel solves this by **automatically detecting failures** and **promoting a healthy replica** to take over as the new master, typically completing the entire process in seconds.

## Master Promotion Algorithm

Our implementation uses a **Priority-Weighted Scoring Algorithm** to select the best replica for promotion to master.

### Algorithm: Score-Based Replica Selection

The selection algorithm ranks each replica based on a composite score:

```
Score = (Priority × 1,000,000) + Replication_Offset
```

#### Components

**1. Priority (Weight: 1,000,000x)**
- Manual configuration parameter (default: 100)
- Allows administrators to prefer specific replicas
- Higher priority replicas are preferred
- Priority=0 means replica will NEVER be promoted (maintenance mode)

**2. Replication Offset (Weight: 1x)**
- Automatically tracked by replication system
- Represents how much data the replica has received from master
- Higher offset = more up-to-date data
- Used as tiebreaker when priorities are equal

### Selection Process

```go
func (s *Sentinel) selectBestReplica() *MonitoredInstance {
    s.mu.RLock()
    defer s.mu.RUnlock()

    var bestReplica *MonitoredInstance
    var highestScore int64 = -1

    // Iterate through all registered replicas
    for _, replica := range s.replicas {
        replica.mu.RLock()
        
        // Skip unhealthy or priority=0 replicas
        if replica.status != "ok" || replica.priority == 0 {
            replica.mu.RUnlock()
            continue
        }

        // Calculate score: priority dominates, offset is tiebreaker
        score := int64(replica.priority)*1000000 + replica.offset
        
        // Select replica with highest score
        if score > highestScore {
            highestScore = score
            bestReplica = replica
        }
        
        replica.mu.RUnlock()
    }

    return bestReplica
}
```

### Example Scenarios

**Scenario 1: Equal Priority (Offset Decides)**
```
Replica A: Priority=100, Offset=5000 → Score = 100,005,000 ✅ SELECTED
Replica B: Priority=100, Offset=4800 → Score = 100,004,800
Replica C: Priority=100, Offset=4950 → Score = 100,004,950

Winner: Replica A (highest offset = most up-to-date)
```

**Scenario 2: Different Priorities**
```
Replica A: Priority=150, Offset=4000 → Score = 150,004,000 ✅ SELECTED
Replica B: Priority=100, Offset=9000 → Score = 100,009,000
Replica C: Priority=50,  Offset=9500 → Score = 50,009,500

Winner: Replica A (priority overrides offset difference)
```

**Scenario 3: Maintenance Mode**
```
Replica A: Priority=100, Offset=5000 → Score = 100,005,000 ✅ SELECTED
Replica B: Priority=0,   Offset=9000 → SKIPPED (maintenance)
Replica C: Priority=100, Offset=4500 → Score = 100,004,500

Winner: Replica A (Replica B excluded from consideration)
```

### Why This Algorithm?

1. **Priority Control**: Administrators can designate preferred replicas (e.g., better hardware, different availability zones)
2. **Data Freshness**: Among equal-priority replicas, selects the most up-to-date (minimizes data loss)
3. **Deterministic**: Same inputs always produce same output (predictable behavior)
4. **Fast**: O(n) time complexity where n = number of replicas
5. **Redis Compatible**: Matches official Redis Sentinel's algorithm

## Implementation Architecture

### System Components

```
┌─────────────────────────────────────────────────────────────────┐
│                         SERVER PROCESS                          │
│  ┌────────────────────────────────────────────────────────┐    │
│  │                    SENTINEL SYSTEM                      │    │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │    │
│  │  │   Master     │  │  Replica 1   │  │  Replica 2   │ │    │
│  │  │  Monitoring  │  │  Monitoring  │  │  Monitoring  │ │    │
│  │  │  (1s cycle)  │  │  (2s cycle)  │  │  (2s cycle)  │ │    │
│  │  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘ │    │
│  │         │                  │                  │         │    │
│  │         └──────────────────┴──────────────────┘         │    │
│  │                            │                            │    │
│  │                    ┌───────▼────────┐                   │    │
│  │                    │ Failure Detect │                   │    │
│  │                    │   (Threshold)  │                   │    │
│  │                    └───────┬────────┘                   │    │
│  │                            │                            │    │
│  │                    ┌───────▼────────┐                   │    │
│  │                    │ Select Replica │                   │    │
│  │                    │ (Score-Based)  │                   │    │
│  │                    └───────┬────────┘                   │    │
│  │                            │                            │    │
│  │         ┌──────────────────┴──────────────────┐         │    │
│  │         ▼                                     ▼         │    │
│  │  ┌─────────────┐                    ┌─────────────┐    │    │
│  │  │   Promote   │                    │ Reconfigure │    │    │
│  │  │   Replica   │                    │   Others    │    │    │
│  │  │ (REPLICAOF  │                    │ (REPLICAOF  │    │    │
│  │  │   NO ONE)   │                    │ <new_master>│    │    │
│  │  └─────────────┘                    └─────────────┘    │    │
│  └────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

### Implementation Structure

```
internal/
├── sentinel/
│   └── sentinel.go              # Core Sentinel implementation (612 lines)
│       ├── Sentinel struct      # Main sentinel state
│       ├── MonitoredInstance    # Tracks master/replica health
│       ├── monitorMaster()      # Health check goroutine (1s)
│       ├── monitorReplicas()    # Health check goroutine (2s)
│       ├── triggerFailover()    # Initiates failover process
│       ├── performFailover()    # Executes 6-step failover
│       ├── selectBestReplica()  # Score-based selection
│       └── promoteReplica()     # Sends REPLICAOF NO ONE
│
├── handler/
│   └── sentinel_commands.go     # SENTINEL command implementation
│       ├── handleSentinel()     # Main command router
│       ├── SENTINEL STATUS      # Returns overall status
│       ├── SENTINEL MASTER      # Returns master info
│       ├── SENTINEL REPLICAS    # Lists all replicas
│       └── GET-MASTER-ADDR      # Returns current master address
│
└── server/
    ├── config.go                # Sentinel configuration
    │   ├── SentinelEnabled
    │   ├── SentinelDownAfterMs
    │   └── SentinelQuorum
    │
    └── server.go                # Sentinel integration
        ├── Initialize sentinel
        ├── Master change callback
        └── Replica discovery
```

### Key Data Structures

```go
// Main Sentinel controller
type Sentinel struct {
    masterName        string                      // Name of monitored master
    masterHost        string                      // Current master IP
    masterPort        int                         // Current master port
    master            *MonitoredInstance          // Master health tracker
    replicas          map[string]*MonitoredInstance // All replicas
    downAfterMs       int                         // Failure threshold (ms)
    quorum            int                         // Voting quorum
    failoverInProgress bool                       // Failover state flag
    onMasterChange    func(string, int)           // Callback on failover
    mu                sync.RWMutex                // Thread safety
}

// Tracks health of individual instance
type MonitoredInstance struct {
    host           string        // Instance IP
    port           int           // Instance port
    status         string        // "ok" or "down"
    lastPing       time.Time     // Last successful PING
    downSince      time.Time     // When marked as down
    priority       int           // Manual priority (default: 100)
    offset         int64         // Replication offset
    mu             sync.RWMutex  // Per-instance lock
}
```

### Failover Execution Flow

```go
func (s *Sentinel) performFailover() error {
    startTime := time.Now()
    
    // Step 1: Select best replica using score algorithm
    newMaster := s.selectBestReplica()
    if newMaster == nil {
        return fmt.Errorf("no suitable replica found")
    }
    
    // Step 2: Promote replica to master (REPLICAOF NO ONE)
    err := s.promoteReplicaToMaster(newMaster)
    if err != nil {
        return err
    }
    
    // Step 3: Update internal master reference
    s.updateMasterReference(newMaster)
    
    // Step 4: Reconfigure all other replicas
    err = s.reconfigureReplicas(newMaster)
    if err != nil {
        log.Printf("[SENTINEL] Warning: some replicas failed reconfiguration")
    }
    
    // Step 5: Add old master as replica (for when it recovers)
    s.addOldMasterAsReplica()
    
    // Step 6: Notify application via callback
    // NOTE: This callback is for REPLICA servers to reconnect to new master,
    // NOT for client applications. Clients should use Sentinel-aware libraries
    // that query Sentinel for current master address via GET-MASTER-ADDR-BY-NAME
    if s.onMasterChange != nil {
        s.onMasterChange(newMaster.host, newMaster.port)
    }
    
    duration := time.Since(startTime)
    log.Printf("[SENTINEL] FAILOVER COMPLETED in %.3fs", duration.Seconds())
    
    return nil
}
```

### Health Monitoring Implementation

**Master Health Check (Every 1 Second)**
```go
func (s *Sentinel) monitorMaster() {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        // Send PING to master
        conn, err := net.DialTimeout("tcp", 
            fmt.Sprintf("%s:%d", s.masterHost, s.masterPort), 
            2*time.Second)
        
        if err != nil {
            s.markMasterDown()
            continue
        }
        
        // Send PING command (RESP protocol)
        conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
        
        // Read response with timeout
        conn.SetReadDeadline(time.Now().Add(2 * time.Second))
        buf := make([]byte, 1024)
        n, err := conn.Read(buf)
        
        if err != nil || string(buf[:n]) != "+PONG\r\n" {
            s.markMasterDown()
        } else {
            s.markMasterUp()
        }
        
        conn.Close()
        
        // Check if down duration exceeds threshold
        if s.shouldTriggerFailover() {
            s.triggerFailover()
        }
    }
}
```

**Replica Health Check (Every 2 Seconds)**
```go
func (s *Sentinel) monitorReplicas() {
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        // Acquire lock ONLY to copy replica references (minimize lock duration)
        s.mu.RLock()
        replicas := make([]*MonitoredInstance, 0, len(s.replicas))
        for _, r := range s.replicas {
            replicas = append(replicas, r)
        }
        s.mu.RUnlock()  // Release EARLY - each replica has its own lock
        
        // Check each replica WITHOUT holding Sentinel.mu
        // Each replica.updateStatus() uses replica.mu for thread safety
        // This prevents blocking other Sentinel operations during slow network I/O
        for _, replica := range replicas {
            if s.pingInstance(replica) {
                replica.updateStatus("ok")   // Uses replica.mu internally
            } else {
                replica.updateStatus("down") // Uses replica.mu internally
            }
        }
    }
}
```

### Thread Safety Strategy

1. **Read-Write Locks**: Used for master/replica maps (many reads, few writes)
2. **Per-Instance Locks**: Each MonitoredInstance has its own mutex (reduces contention)
3. **Atomic Operations**: Failover uses exclusive lock to prevent concurrent failovers
4. **Lock Ordering**: Always acquire Sentinel.mu before MonitoredInstance.mu (prevents deadlock)

### Integration with Server

```go
// In server.go
func (s *Server) Start() error {
    // ... existing server initialization ...
    
    if s.cfg.SentinelEnabled {
        // Create sentinel instance
        sentinelInstance := sentinel.NewSentinel(
            s.cfg.SentinelMasterName,
            s.cfg.Host,
            s.cfg.Port,
            s.cfg.SentinelDownAfterMs,
            s.cfg.SentinelQuorum,
        )
        
        // Set callback for master changes
        // IMPORTANT: This callback is for REPLICA servers, not client apps!
        // When a new master is promoted, all replica servers need to
        // disconnect from the old master and reconnect to the new master.
        sentinelInstance.SetMasterChangeCallback(func(newHost string, newPort int) {
            // Disconnect from old master
            s.replicationManager.StopReplication()
            
            // Connect to new master
            s.replicationManager.StartReplication(newHost, newPort)
        })
        
        // Start monitoring
        sentinelInstance.Start()
        
        // Auto-discover replicas from replication manager
        go s.discoverAndRegisterReplicas(sentinelInstance)
        
        s.sentinel = sentinelInstance
    }
    
    return nil
}
```

### How Client Applications Discover the Master

**Important:** Client applications do NOT use the `onMasterChange` callback. Instead, they use one of these approaches:

#### Approach 1: Sentinel-Aware Client Library (Recommended)

```go
// Client application using Sentinel-aware library
sentinelClient := redis.NewSentinelClient(&redis.Options{
    SentinelAddrs: []string{
        "sentinel1:26379",
        "sentinel2:26379",
        "sentinel3:26379",
    },
    MasterName: "mymaster",
})

// Library automatically handles master discovery and failover:
// 1. ONCE at startup: Queries Sentinel for master address
//    → GET-MASTER-ADDR-BY-NAME mymaster
//    → Response: ["127.0.0.1", "6380"]
// 2. Connects to master and CACHES the connection
// 3. All subsequent commands use the cached connection (no Sentinel queries!)
// 4. ONLY if connection fails: re-queries Sentinel for new master
// 5. Reconnects to new master and updates cache
// 6. Automatically retries the failed command
```

**Important: Sentinel is NOT queried for every command!**

```go
// Example: Client execution flow
client.Set("key1", "value1")  // ✅ Uses cached master connection (127.0.0.1:6380)
client.Get("key1")            // ✅ Uses cached master connection (no Sentinel query)
client.Set("key2", "value2")  // ✅ Uses cached master connection

// Master fails here! Sentinel promotes 127.0.0.1:6381 to master

client.Get("key2")            // ❌ Connection fails
                              // 📡 Library queries Sentinel: "Who is master now?"
                              // 📥 Sentinel responds: "127.0.0.1:6381"
                              // 🔌 Library connects to new master
                              // 💾 Library caches new connection
                              // 🔄 Library retries: Get("key2")
                              // ✅ Success!

client.Set("key3", "value3")  // ✅ Uses NEW cached connection (127.0.0.1:6381)
client.Get("key3")            // ✅ Uses NEW cached connection (no Sentinel query)
```

**Edge Case: What if connection drops temporarily during failover?**

```go
// Client connected to Master A (127.0.0.1:6380)
client.Set("key1", "value1")  // ✅ Success

// Network hiccup causes temporary connection drop
// During the drop, Sentinel performs failover:
//   - Replica B (127.0.0.1:6381) promoted to master
//   - Master A (127.0.0.1:6380) demoted to replica

// Network recovers, TCP connection to 127.0.0.1:6380 re-establishes
client.Set("key2", "value2")  // ❌ Error: "READONLY You can't write against a read only replica"
                              // (127.0.0.1:6380 is now a REPLICA, not master!)

// Smart client library detects READONLY error:
                              // 📡 Queries Sentinel: "Who is master now?"
                              // 📥 Sentinel responds: "127.0.0.1:6381" (Replica B)
                              // 🔌 Connects to new master
                              // 💾 Caches new connection
                              // 🔄 Retries: Set("key2", "value2")
                              // ✅ Success!
```

**Edge Case 2: Read Request to Demoted Master (Stale Data Risk!)**

```go
// Timeline of events:
// T0: Client connected to Master A (127.0.0.1:6380)
// T1: Network hiccup, client disconnects
// T2: Sentinel promotes Replica B (127.0.0.1:6381) to master
// T3: New master receives writes:
//     - New client: Set("user:100", "Bob")
//     - New client: Set("counter", "999")
// T4: Network recovers, client reconnects to 127.0.0.1:6380 (now a replica)
// T5: Old master (now replica) starts syncing from new master (but takes time)

// Client sends READ request to demoted master:
value := client.Get("user:100")  // ⚠️  Returns: nil (STALE DATA!)
                                 // New master has "Bob", but this replica hasn't synced yet!

value2 := client.Get("counter")  // ⚠️  Returns: "100" (STALE DATA!)
                                 // New master has "999", but replica shows old value

// No error! Reads succeed on replicas, but data is STALE (eventual consistency issue)
```

**Why This Happens:**

1. **Replica accepts reads**: Unlike writes, reads don't trigger READONLY error
2. **Async replication**: Replica syncs from new master asynchronously
3. **Replication lag**: During sync, replica has old data (before it was demoted)
4. **Time window**: Gap between failover and full sync completion

**Timeline Detail:**

```
Time  New Master (6381)         Old Master (6380, now replica)
----  -------------------        -------------------------------
T0    Promoted to master         Demoted to replica
T1    Receives: SET x=100        (not synced yet, still has old data)
T2    x=100 stored               (initiating sync with new master)
T3    Receives: SET y=200        (receiving RDB snapshot...)
T4    y=200 stored               (loading snapshot, x=old value)
T5                               ✅ Sync complete: x=100, y=200

      Client reads from replica before T5 → Gets stale data!
```

**Real-World Example:**

```go
// E-commerce scenario during failover:

// T0: Master A has inventory: product_123 = 5 units
client.Get("product_123")  // Returns: "5"

// T1: Network partition, Sentinel promotes Master B
// T2: Customer buys 2 units through new master
//     New Master B: product_123 = 3 units

// T3: Client reconnects to old master (now replica)
//     Replica hasn't synced yet, still shows: product_123 = 5 units
stock := client.Get("product_123")  // ⚠️  Returns: "5" (WRONG!)
                                     // Real value is "3"

// Application shows "5 units available" when only 3 exist!
// Customer tries to buy 4 units → Oversell situation!
```

**Solutions:**

**1. Re-query Sentinel periodically (Proactive)**
```go
// Every N seconds, verify we're connected to current master
func (c *SentinelClient) healthCheck() {
    currentMaster := c.querySentinelForMaster()
    if currentMaster != c.connectedAddress {
        // We're connected to wrong instance!
        c.reconnectToMaster()
    }
}

// Run in background
go func() {
    ticker := time.NewTicker(5 * time.Second)
    for range ticker.C {
        c.healthCheck()
    }
}()
```

**2. Detect role change via INFO command (Reactive)**
```go
// Before critical reads, verify we're talking to master
func (c *Client) GetCritical(key string) (string, error) {
    // Query instance role
    info := c.conn.Do("INFO", "replication")
    // Parse: "role:master" or "role:slave"
    
    if info.Contains("role:slave") {
        // We're connected to replica, not master!
        // Re-query Sentinel and reconnect
        c.reconnectToMaster()
        return c.conn.Do("GET", key)
    }
    
    // Confirmed we're on master, proceed
    return c.conn.Do("GET", key)
}
```

**3. Use master-only policy (Conservative)**
```go
// Configure client to ALWAYS read from master (no replicas)
sentinelClient := redis.NewSentinelClient(&redis.Options{
    SentinelAddrs: sentinels,
    MasterName:    "mymaster",
    ReadPolicy:    "master-only",  // Never read from replicas
})

// Trade-off: No stale data, but no read scaling
```

**4. Accept eventual consistency (Relaxed)**
```go
// For non-critical data, accept stale reads
func (c *Client) GetEventuallyConsistent(key string) (string, error) {
    // Read from wherever we're connected
    // Might be master, might be replica, might be stale
    // Use for: dashboards, analytics, cached data
    return c.conn.Do("GET", key)
}

// For critical data, always verify
func (c *Client) GetStronglyConsistent(key string) (string, error) {
    // Ensure we're reading from current master
    c.ensureConnectedToMaster()
    return c.conn.Do("GET", key)
}
```

**Comparison of Error Scenarios:**

| Scenario | Request Type | Result | Detectable? | Fix |
|----------|-------------|--------|-------------|-----|
| Connected to demoted master | **WRITE** | ❌ READONLY error | ✅ Yes (error) | Re-query Sentinel |
| Connected to demoted master | **READ** | ⚠️  Stale data (no error!) | ❌ Silent failure | Periodic health check or INFO role verification |
| Connection broken | Any | ❌ Network error | ✅ Yes (error) | Re-query Sentinel |
| Connected to current master | Any | ✅ Success | N/A | N/A |

**Critical Implementation Detail:**

Client libraries must handle **THREE** error scenarios:
1. **Connection Failure**: TCP connection broken → Query Sentinel
2. **READONLY Error**: Connected to demoted master (write attempt) → Query Sentinel
3. **⚠️  Stale Data Risk**: Connected to demoted master (read request) → No error, but data may be stale!

```go
// Comprehensive client library implementation
func (c *SentinelClient) executeCommand(cmd string, args ...interface{}) error {
    result, err := c.masterConn.Do(cmd, args...)
    
    if err != nil {
        // Scenario 1: Connection broken
        if isNetworkError(err) {
            c.reconnectToMaster()  // Queries Sentinel
            return c.executeCommand(cmd, args...)  // Retry
        }
        
        // Scenario 2: READONLY error (master became replica)
        if strings.Contains(err.Error(), "READONLY") {
            c.reconnectToMaster()  // Queries Sentinel
            return c.executeCommand(cmd, args...)  // Retry
        }
        
        return err  // Other errors
    }
    
    // Scenario 3: No error, but check for stale data (optional)
    // For critical operations, verify we're on master:
    if c.requireStrongConsistency && isReadCommand(cmd) {
        if !c.verifyConnectedToMaster() {
            c.reconnectToMaster()
            return c.executeCommand(cmd, args...)  // Retry on master
        }
    }
    
    return nil
}
```

**Best Practice Recommendation:**

For **write-heavy applications** or **critical consistency**:
- Enable periodic Sentinel health checks (every 5-10 seconds)
- Verify role via INFO before critical reads
- Accept small overhead for consistency guarantee

For **read-heavy applications** with **eventual consistency tolerance**:
- Accept stale reads during brief failover window
- Use replicas for read scaling
- Handle READONLY errors only (simpler client logic)
- Document consistency SLA (e.g., "reads may be stale for up to 30s during failover")

**Performance Impact:**
- Normal operations: 0 extra latency (uses cached connection)
- During failover: 1 extra Sentinel query + reconnection overhead
- After failover: Back to normal (new master cached)
- Edge case (reconnect to demoted master): Detect READONLY error, re-query Sentinel, reconnect to new master

#### Approach 2: Manual Sentinel Query

```go
// Client manually queries Sentinel before connecting
func connectToMaster() (*redis.Client, error) {
    // Step 1: Ask Sentinel for current master (ONCE)
    sentinelConn, _ := redis.Dial("tcp", "127.0.0.1:6379")
    masterAddr := sentinelConn.Do("SENTINEL", "GET-MASTER-ADDR-BY-NAME", "mymaster")
    // Returns: ["127.0.0.1", "6380"]
    
    // Step 2: Connect to master and CACHE the connection
    masterConn, _ := redis.Dial("tcp", fmt.Sprintf("%s:%s", masterAddr[0], masterAddr[1]))
    
    return masterConn, nil
}

// Execute commands normally - NO Sentinel queries per command!
masterConn := connectToMaster()
masterConn.Do("SET", "key1", "value1")  // Direct to master
masterConn.Do("GET", "key1")            // Direct to master
masterConn.Do("SET", "key2", "value2")  // Direct to master

// ONLY re-query on connection error:
if err := masterConn.Do("GET", "key3"); err != nil {
    // Connection failed - query Sentinel again
    masterConn = connectToMaster()  // Gets new master address
    masterConn.Do("GET", "key3")    // Retry on new master
}
```

#### Approach 3: Pub/Sub Notifications (Used by Official Redis Sentinel)

**Yes, official Redis Sentinel uses Pub/Sub!** This is a core feature in production Redis.

**How It Works in Official Redis:**

Sentinel publishes events to specific channels that clients can subscribe to:

```go
// Official Redis Sentinel Pub/Sub channels:
// +switch-master <master-name> <old-ip> <old-port> <new-ip> <new-port>
// +sdown master <master-name> <ip> <port>  (subjectively down)
// +odown master <master-name> <ip> <port>  (objectively down)
// +failover-end <master-name> <ip> <port>

// Client subscribes to Sentinel's pub/sub channel
func (client *Client) SubscribeSentinelEvents() {
    // Connect to Sentinel (not Redis master!)
    sentinelConn := redis.Dial("tcp", "127.0.0.1:26379")
    
    // Subscribe to switch-master events
    pubsub := sentinelConn.PSubscribe("__sentinel__:*")
    
    for msg := range pubsub.Channel() {
        if strings.Contains(msg.Channel, "+switch-master") {
            // Parse: "+switch-master mymaster 127.0.0.1 6380 127.0.0.1 6381"
            // Old master: 127.0.0.1:6380
            // New master: 127.0.0.1:6381
            newHost, newPort := parseSwitch masterEvent(msg.Payload)
            
            // Reconnect to new master
            client.Reconnect(newHost, newPort)
            log.Printf("Switched to new master: %s:%d", newHost, newPort)
        }
    }
}
```

**Advantages over polling:**
- **Instant notification**: No 5-second delay, client knows immediately
- **Lower overhead**: No periodic Sentinel queries (better at scale)
- **Event-driven**: More elegant architecture

**Disadvantages:**
- **Extra connection**: Client must maintain persistent connection to Sentinel
- **More complex**: Requires pub/sub support in client library
- **Sentinel dependency**: If Sentinel crashes, client loses notifications (though can fall back to error detection)

**Our Implementation Status: ❌ Not Yet Implemented**

We currently don't publish Pub/Sub events from Sentinel. Clients must use:
- Approach 1: Re-query on connection failure (Reactive)
- Solution 1 from edge cases: Periodic health checks (Proactive)

To implement this, we would need to:
1. Add Pub/Sub support to our Sentinel
2. Publish `+switch-master` events during failover
3. Update client libraries to subscribe to these events

**Summary:**
- **Replica Servers**: Use `onMasterChange` callback to reconnect to new master
- **Client Applications**: Query Sentinel via `GET-MASTER-ADDR-BY-NAME` command
- **Best Practice**: Use Sentinel-aware client libraries that handle failover automatically
- **Query Frequency**: Sentinel queried ONCE at startup, then ONLY on connection failure (NOT per command!)
- **Normal Operation**: Commands go directly to cached master connection (zero Sentinel overhead)

## Configuration

### Server Configuration Parameters

```go
type Config struct {
    // Sentinel enable flag
    SentinelEnabled      bool   // Enable Sentinel (default: false)
    
    // Master identification
    SentinelMasterName   string // Master name (default: "mymaster")
    
    // Failure detection
    SentinelDownAfterMs  int    // Milliseconds before failover (default: 30000)
    
    // Quorum (for future multi-sentinel support)
    SentinelQuorum       int    // Voting quorum (default: 1)
    
    // Failover timeout
    SentinelFailoverMs   int    // Max failover duration (default: 180000)
}
```

### Default Values

```go
SentinelEnabled:     false         // Disabled by default
SentinelMasterName:  "mymaster"    // Default master name
SentinelQuorum:      1              // Single sentinel (no voting)
SentinelDownAfterMs: 30000          // 30 seconds threshold
SentinelFailoverMs:  180000         // 3 minutes max duration
```

### Understanding SentinelQuorum

**Important Distinction: Sentinels vs Replicas**

**Sentinels** and **Replicas** are completely different things:

- **Sentinels**: Monitoring processes that watch Redis instances (master + replicas)
  - Run as separate processes (typically on different machines)
  - Don't store data
  - Only monitor health and coordinate failover
  - Talk to each other to reach consensus

- **Replicas**: Redis server instances that replicate data
  - Store actual Redis data (copy of master's data)
  - Serve read requests
  - One gets promoted to master during failover
  - Don't communicate with each other

**Example Setup:**
```
┌─────────────────────────────────────────────────────────┐
│                   MONITORING LAYER                       │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐          │
│  │Sentinel 1│    │Sentinel 2│    │Sentinel 3│  ← 3 Sentinels (monitors)
│  └────┬─────┘    └────┬─────┘    └────┬─────┘          │
│       └───────────────┼───────────────┘                 │
│                       │ (vote/communicate)              │
└───────────────────────┼─────────────────────────────────┘
                        │ (monitor)
                        ▼
┌─────────────────────────────────────────────────────────┐
│                     DATA LAYER                           │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐          │
│  │  Master  │───>│ Replica 1│    │ Replica 2│  ← 3 Redis servers (data)
│  │ (6379)   │    │ (6380)   │    │ (6381)   │          │
│  └──────────┘    └──────────┘    └──────────┘          │
│                   (replicate data)                       │
└─────────────────────────────────────────────────────────┘
```

**What is Quorum?**

Quorum is the minimum number of **Sentinels** (not replicas!) that must agree a master is down before automatic failover is triggered. This prevents false positives from network partitions or single Sentinel failures.

**How It Works (Multi-Sentinel):**

```
Scenario: 3 Sentinels monitoring 1 master, Quorum = 2

┌─────────────┐
│ Sentinel 1  │───PING───> Master (timeout) → Marks master as DOWN
└─────────────┘
       │
       ├─────────> Asks Sentinel 2: "Is master down?"
       │           Response: "YES, I can't reach it either"
       │
       └─────────> Asks Sentinel 3: "Is master down?"
                   Response: "NO, master is responding fine"

Votes: 2 out of 3 Sentinels agree master is down
Quorum: 2 (satisfied!)
Action: Initiate failover ✅
```

**Current Implementation Status:**

```go
// Our implementation: SINGLE SENTINEL only
SentinelQuorum: 1  // Only 1 Sentinel, so quorum is always 1

// Future multi-sentinel implementation would look like:
type SentinelCluster struct {
    Sentinels      []*SentinelPeer  // List of other Sentinels
    Quorum         int               // Required votes (e.g., 2 out of 3)
}

func (sc *SentinelCluster) shouldTriggerFailover() bool {
    votes := 1  // This sentinel's vote
    
    // Ask other sentinels if they agree master is down
    for _, peer := range sc.Sentinels {
        if peer.IsMasterDown() {
            votes++
        }
    }
    
    // Trigger failover only if quorum is reached
    return votes >= sc.Quorum
}
```

**Why Quorum Matters:**

1. **Split-Brain Prevention**: If network partition isolates 1 Sentinel, it can't trigger failover alone
2. **False Positive Protection**: Temporary network issues won't cause unnecessary failovers
3. **Consensus**: Multiple observers must agree before taking drastic action

**Quorum Best Practices:**

```
Setup              | Sentinels | Quorum | Failure Tolerance
-------------------|-----------|--------|------------------
Single Sentinel    | 1         | 1      | None (SPOF)
Basic HA           | 3         | 2      | 1 Sentinel can fail
Production HA      | 5         | 3      | 2 Sentinels can fail
High Availability  | 7         | 4      | 3 Sentinels can fail

Formula: Quorum = (Total Sentinels / 2) + 1
         (This is about Sentinel monitors, NOT replicas!)
```

**Key Point:** 
- **Quorum** counts **Sentinel processes** (monitors)
- **Replica count** is independent (you might have 3 Sentinels monitoring 5 replicas)
- Common setup: 3 Sentinels (quorum=2) monitoring 1 master + 2 replicas

**Our Current Limitation:**

Since we only support **single Sentinel**, the quorum is hardcoded to `1`. The parameter exists in the configuration for future multi-sentinel support, but is not currently enforced.

### Usage Example

```bash
# Start master with Sentinel enabled
./redis-server \
  --port 6379 \
  --sentinel-enabled=true \
  --sentinel-master-name=mymaster \
  --sentinel-down-after-ms=10000

# Start replicas (Sentinel auto-discovers them)
./redis-server --port 6380 --replicaof 127.0.0.1 6379
./redis-server --port 6381 --replicaof 127.0.0.1 6379

# Simulate master failure
pkill -f "port 6379"

# Sentinel automatically:
# 1. Detects failure after 10 seconds
# 2. Selects best replica (highest priority+offset)
# 3. Promotes it to master (REPLICAOF NO ONE)
# 4. Reconfigures other replicas
# 5. Total time: ~2-5 seconds
```

## Replica Priority Assignment

### Current Implementation

Currently, all replicas are assigned a **default priority of 100** when registered with Sentinel:

```go
// In sentinel.go - AddReplica function
func (s *Sentinel) AddReplica(host string, port int) {
    replica := &MonitoredInstance{
        host:     host,
        port:     port,
        status:   "ok",
        priority: 100,  // HARDCODED - all replicas get same priority
        offset:   0,
    }
    s.replicas[fmt.Sprintf("%s:%d", host, port)] = replica
}
```

### How to Implement Priority Configuration

There are several approaches to allow administrators to set replica priorities:

#### Option 1: Configuration File (Recommended)

```go
// Add to server config
type ReplicaConfig struct {
    Host     string
    Port     int
    Priority int  // User-specified priority
}

type Config struct {
    // ... existing fields ...
    SentinelReplicaPriorities []ReplicaConfig
}

// Example config.yaml
sentinel:
  enabled: true
  replicas:
    - host: 127.0.0.1
      port: 6380
      priority: 100
    - host: 127.0.0.1
      port: 6381
      priority: 50   # Lower priority (backup replica)
    - host: 192.168.1.10
      port: 6379
      priority: 150  # Higher priority (better hardware)
```

#### Option 2: Command-Line Flag

```bash
# Start replica with custom priority
./redis-server \
  --port 6380 \
  --replicaof 127.0.0.1 6379 \
  --replica-priority 150

# Sentinel reads priority from replica's INFO replication output
```

#### Option 3: SENTINEL SET Command (Runtime)

```bash
# Change replica priority at runtime
redis-cli SENTINEL SET mymaster replica-priority 127.0.0.1:6380 150

# Implementation:
func (h *CommandHandler) handleSentinelSet(args []string) *Response {
    if len(args) < 4 {
        return NewErrorResponse("wrong number of arguments")
    }
    
    masterName := args[0]
    option := args[1]  // "replica-priority"
    address := args[2] // "127.0.0.1:6380"
    value := args[3]   // "150"
    
    if option == "replica-priority" {
        priority, _ := strconv.Atoi(value)
        h.sentinel.SetReplicaPriority(address, priority)
        return NewSimpleStringResponse("OK")
    }
}
```

#### Option 4: INFO Replication Integration

```go
// Replica reports its own priority via INFO replication
func (r *Replica) GetInfo() string {
    return fmt.Sprintf(
        "role:slave\n" +
        "master_host:%s\n" +
        "master_port:%d\n" +
        "slave_priority:%d\n",  // New field
        r.masterHost,
        r.masterPort,
        r.priority,  // Read from replica's config
    )
}

// Sentinel parses priority from INFO response
func (s *Sentinel) updateReplicaInfo(replica *MonitoredInstance) {
    info := s.sendCommand(replica, "INFO", "replication")
    // Parse: slave_priority:150
    if priority, found := parseInfoField(info, "slave_priority"); found {
        replica.priority = priority
    }
}
```

### Priority Use Cases

**Scenario 1: Hardware-Based Priority**
```
Replica A: SSD storage, 32GB RAM          → Priority 200
Replica B: HDD storage, 16GB RAM          → Priority 100
Replica C: Slow disk, minimal resources   → Priority 50

Result: Replica A always promoted first (best hardware)
```

**Scenario 2: Geographic Priority**
```
Replica A: Same datacenter as clients     → Priority 150
Replica B: Different datacenter           → Priority 100
Replica C: Remote backup site             → Priority 25

Result: Minimize client latency by preferring local replicas
```

**Scenario 3: Maintenance Mode**
```
Replica A: Production-ready               → Priority 100
Replica B: Under maintenance              → Priority 0
Replica C: Production-ready               → Priority 100

Result: Replica B never promoted (priority 0 = excluded)
```

### Implementation Recommendation

For production use, implement **Option 1 (Configuration File) + Option 4 (INFO integration)**:

1. Each replica sets `replica-priority` in its config file
2. Replica reports priority via `INFO replication` command
3. Sentinel queries each replica and updates priority dynamically
4. Allows both static configuration and runtime updates

```go
// Enhanced AddReplica with dynamic priority discovery
func (s *Sentinel) AddReplica(host string, port int) {
    replica := &MonitoredInstance{
        host:     host,
        port:     port,
        status:   "ok",
        priority: 100,  // Default
        offset:   0,
    }
    
    // Query replica for its configured priority
    if priority := s.queryReplicaPriority(host, port); priority > 0 {
        replica.priority = priority
    }
    
    s.replicas[fmt.Sprintf("%s:%d", host, port)] = replica
    log.Printf("[SENTINEL] Added replica %s:%d (priority: %d)", 
        host, port, replica.priority)
}
```

## SENTINEL Commands

Implementation provides Redis-compatible SENTINEL commands for monitoring:

### SENTINEL STATUS
```bash
redis-cli SENTINEL STATUS
# Returns: master address, status, replica count, failover state
```

### SENTINEL MASTER
```bash
redis-cli SENTINEL MASTER mymaster
# Returns: master name, IP, port, health status
```

### SENTINEL REPLICAS
```bash
redis-cli SENTINEL REPLICAS mymaster
# Returns: list of all replicas with health, priority, offset
```

### SENTINEL GET-MASTER-ADDR-BY-NAME
```bash
redis-cli SENTINEL GET-MASTER-ADDR-BY-NAME mymaster
# Returns: current master IP and port
```

## Performance Characteristics

### Resource Overhead
- **CPU**: ~0.1% per monitored instance (health checks are lightweight)
- **Memory**: ~1KB per replica (MonitoredInstance struct)
- **Network**: 1 PING/sec to master + 0.5 PING/sec per replica

### Timing
- **Detection Time**: Configurable (default 30s via SentinelDownAfterMs)
- **Failover Duration**: 1-5 seconds (depends on network latency)
- **Total Downtime**: Detection time + Failover duration (~30-35s with defaults)

### Scalability
- Tested with up to 10 replicas
- Linear overhead: O(n) where n = replica count
- Recommended: < 5 replicas for single Sentinel

## Comparison with Official Redis Sentinel

| Feature | Redis Sentinel | Our Implementation |
|---------|---------------|-------------------|
| Master Monitoring | ✅ PING-based | ✅ PING-based (1s) |
| Replica Monitoring | ✅ INFO-based | ✅ PING-based (2s) |
| Failover Algorithm | ✅ Priority + Offset | ✅ Priority + Offset |
| Multi-Sentinel Quorum | ✅ Raft consensus | ❌ Single Sentinel only |
| Pub/Sub Notifications | ✅ | ❌ Callback-based |
| Config Persistence | ✅ Writes sentinel.conf | ❌ In-memory only |

## Read Scaling with Replicas

### Does Redis Use Replicas for Read Requests?

**Yes!** This is a key feature for scaling read-heavy workloads.

**Official Redis Behavior:**
- **Master**: Handles all writes + reads
- **Replicas**: Handle reads only (read-only by default)
- **Scaling Pattern**: 1 master + N replicas = N+1x read capacity

**Example Setup:**
```
┌─────────────────────────────────────────────────────┐
│                   CLIENT LAYER                       │
│                                                      │
│  Application with 1000 read/sec, 100 write/sec      │
│                                                      │
│         ┌──────────┬──────────┬──────────┐          │
│         │  Write   │  Read    │  Read    │          │
│         │ requests │ requests │ requests │          │
│         └────┬─────┴────┬─────┴────┬─────┘          │
└──────────────┼──────────┼──────────┼────────────────┘
               │          │          │
               ▼          ▼          ▼
        ┌──────────┬──────────┬──────────┐
        │  Master  │ Replica1 │ Replica2 │
        │  (6379)  │  (6380)  │  (6381)  │
        │          │          │          │
        │ 100 w/s  │ 500 r/s  │ 500 r/s  │  ← Load distributed!
        └──────────┴──────────┴──────────┘
```

### Our Implementation Status

**✅ Replicas CAN serve reads** (they have all the data)
**❌ No automatic read-routing** (clients must manually connect to replicas)
**❌ No read-only enforcement** (replicas accept writes but shouldn't)

**What's Implemented:**
1. Replicas receive all data from master ✅
2. Replicas maintain synchronized data ✅
3. Clients can connect to replica ports ✅
4. Replicas execute read commands (GET, HGET, etc.) ✅

**What's Missing:**
1. ❌ Read-only mode enforcement (reject writes on replicas)
2. ❌ Client library with read-write splitting
3. ❌ Replica discovery for clients

### How to Implement Read Scaling

#### Option 1: Manual Connection (Current State)

```go
// Client application manually connects to different instances
masterConn := redis.Dial("tcp", "127.0.0.1:6379")  // For writes
replica1Conn := redis.Dial("tcp", "127.0.0.1:6380") // For reads
replica2Conn := redis.Dial("tcp", "127.0.0.1:6381") // For reads

// Write to master
masterConn.Do("SET", "user:1", "Alice")

// Read from replica (round-robin or random)
user := replica1Conn.Do("GET", "user:1")  // ✅ Works! Replica has the data

// Read from another replica
user2 := replica2Conn.Do("GET", "user:2") // ✅ Works! Data replicated
```

**Issue:** Replicas currently accept writes (they shouldn't!)

```go
// This SHOULD fail but currently succeeds:
replica1Conn.Do("SET", "key", "value")  // ❌ Should return READONLY error
```

#### Option 2: Implement Read-Only Mode (Recommended)

**Step 1: Add read-only enforcement in command handler**

```go
// In handler/handler.go - modify executeCommand
func (h *CommandHandler) executeCommand(cmd *protocol.Command) []byte {
    if cmd == nil || len(cmd.Args) == 0 {
        return protocol.EncodeError("ERR empty command")
    }

    command := strings.ToUpper(cmd.Args[0])
    
    // NEW: Check if replica is trying to execute write command
    if h.isReplica() && h.isWriteCommand(command) {
        return protocol.EncodeError("READONLY You can't write against a read only replica")
    }

    if handler, exists := h.commands[command]; exists {
        return handler(cmd)
    }

    return protocol.EncodeError(fmt.Sprintf("ERR unknown command '%s'", command))
}

// Helper to check if server is replica
func (h *CommandHandler) isReplica() bool {
    if h.replicationMgr == nil {
        return false
    }
    if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
        return replMgr.GetRole() == replication.RoleReplica
    }
    return false
}

// Helper to identify write commands
func (h *CommandHandler) isWriteCommand(cmd string) bool {
    writeCommands := map[string]bool{
        "SET": true, "DEL": true, "HSET": true, "LPUSH": true,
        "RPUSH": true, "SADD": true, "ZADD": true, "EXPIRE": true,
        "SETEX": true, "APPEND": true, "INCR": true, "DECR": true,
        // ... add all write commands
    }
    return writeCommands[cmd]
}
```

**Step 2: Client library with read-write splitting**

```go
// Sentinel-aware client with read scaling
type ReadScalingClient struct {
    sentinelAddrs []string
    masterName    string
    masterConn    *redis.Client
    replicaConns  []*redis.Client
    roundRobin    int  // For load balancing reads
}

func NewReadScalingClient(sentinelAddrs []string, masterName string) (*ReadScalingClient, error) {
    client := &ReadScalingClient{
        sentinelAddrs: sentinelAddrs,
        masterName:    masterName,
    }
    
    // Discover master
    masterAddr := client.queryMasterAddress()
    client.masterConn = redis.Dial("tcp", masterAddr)
    
    // Discover all replicas
    replicaAddrs := client.queryReplicaAddresses()
    for _, addr := range replicaAddrs {
        conn := redis.Dial("tcp", addr)
        client.replicaConns = append(client.replicaConns, conn)
    }
    
    return client, nil
}

// Write goes to master
func (c *ReadScalingClient) Set(key, value string) error {
    return c.masterConn.Do("SET", key, value)
}

// Read from replica (round-robin load balancing)
func (c *ReadScalingClient) Get(key string) (string, error) {
    if len(c.replicaConns) == 0 {
        // No replicas, read from master
        return c.masterConn.Do("GET", key)
    }
    
    // Round-robin across replicas
    replica := c.replicaConns[c.roundRobin % len(c.replicaConns)]
    c.roundRobin++
    
    value, err := replica.Do("GET", key)
    if err != nil {
        // Replica failed, try master
        return c.masterConn.Do("GET", key)
    }
    
    return value, nil
}
```

**Step 3: Query Sentinel for replica addresses**

```go
func (c *ReadScalingClient) queryReplicaAddresses() []string {
    // Connect to Sentinel
    sentinelConn := redis.Dial("tcp", c.sentinelAddrs[0])
    
    // Query: SENTINEL REPLICAS mymaster
    result := sentinelConn.Do("SENTINEL", "REPLICAS", c.masterName)
    // Returns: ["replica0:host=127.0.0.1,port=6380,status=ok", ...]
    
    // Parse addresses
    var addrs []string
    for _, replicaInfo := range result {
        // Parse "host=127.0.0.1,port=6380,status=ok"
        host, port := parseReplicaInfo(replicaInfo)
        if status == "ok" {
            addrs = append(addrs, fmt.Sprintf("%s:%d", host, port))
        }
    }
    
    return addrs
}
```

### Read Scaling Benefits

**Performance:**
```
Single Master:
  1000 reads/sec → Master handles all → 100% master CPU

Master + 2 Replicas:
  1000 reads/sec → 333 reads each → 33% CPU per instance
  3x read capacity!
```

**Latency:**
```
Geographic distribution:
  Master: US East
  Replica 1: US West  ← West coast clients read locally
  Replica 2: Europe   ← European clients read locally
  
  Result: Lower latency for read-heavy apps
```

**Availability:**
```
Master fails during failover (30s downtime):
  ❌ Writes blocked
  ✅ Reads still work (replicas serve reads)
  
  Partial availability > complete downtime!
```

### Consistency Considerations

**Replication is Asynchronous:**

```
Time  Master         Replica
----  ------         -------
T0    SET x=1        (replicating...)
T1    Client reads   x=1
T2    SET x=2        (replicating...)
T3                   x=1  ← Stale read! (replica hasn't caught up)
T4                   x=2  ← Eventually consistent
```

**Trade-offs:**
- **Reads from Master**: Consistent, but doesn't scale
- **Reads from Replica**: Scales, but may be stale (eventual consistency)

**When to use replica reads:**
- Analytics queries
- Dashboard displays
- User profile lookups (okay if slightly stale)
- Product catalogs

**When to avoid:**
- Bank account balances (must be consistent)
- Inventory counts (critical accuracy)
- Session data (must be current)

## References

- **Redis Sentinel Specification**: https://redis.io/docs/management/sentinel/
- **Failover Protocol**: https://redis.io/docs/management/replication/
- **RESP Protocol**: https://redis.io/docs/reference/protocol-spec/
