# Sentinel Peer To Peer Connection

## Questions Answered

### 1. Is there a "main" or "master" Sentinel?

**NO!** Sentinels use a **peer-to-peer mesh network**:

```
┌─────────────┐
│  Sentinel 1 │←────────────┐
└──────┬──────┘             │
       │                    │
       │     ┌──────────────┼──────┐
       │     │              │      │
       ↓     ↓              ↑      ↑
┌─────────────┐      ┌─────────────┐
│  Sentinel 2 │─────→│  Sentinel 3 │
└─────────────┘      └─────────────┘

ALL Sentinels are EQUAL peers!
```

**How Each Sentinel Connects:**
- **Sentinel 1** connects to Sentinels 2 & 3
- **Sentinel 2** connects to Sentinels 1 & 3
- **Sentinel 3** connects to Sentinels 1 & 2

**Full Mesh Network:** Every Sentinel connects to **every other** Sentinel.

**Why this architecture?**
- ✅ **No single point of failure** - any Sentinel can coordinate failover
- ✅ **Distributed consensus** - votes come from all peers
- ✅ **Redundancy** - if one Sentinel dies, others still communicate
- ✅ **Quorum** - majority voting works because all peers have equal weight

**Example:**
```bash
# Start Sentinel 1
./bin/redis-sentinel --port 26379 --sentinel-addrs "127.0.0.1:26380,127.0.0.1:26381"
# ↑ Connects to 26380 and 26381

# Start Sentinel 2  
./bin/redis-sentinel --port 26380 --sentinel-addrs "127.0.0.1:26379,127.0.0.1:26381"
# ↑ Connects to 26379 and 26381

# Start Sentinel 3
./bin/redis-sentinel --port 26381 --sentinel-addrs "127.0.0.1:26379,127.0.0.1:26380"
# ↑ Connects to 26379 and 26380
```

Result: All 3 Sentinels are connected to each other (full mesh).

## Peer-to-Peer Architecture Details

### Connection Flow

**Sentinel 1 perspective:**
```
1. Start listening on port 26379
2. Read config: SentinelAddrs = ["127.0.0.1:26380", "127.0.0.1:26381"]
3. For each address in SentinelAddrs:
   - Spawn goroutine to connect to that peer
   - Send PING every 10 seconds
   - Query master address to detect failover
   - Auto-reconnect if connection drops
```

**Result:** Sentinel 1 has 2 **outbound** connections (to peers)

**But wait!** Sentinels 2 and 3 **also** connect back to Sentinel 1:
- Sentinel 2 creates **inbound** connection to Sentinel 1
- Sentinel 3 creates **inbound** connection to Sentinel 1

**Final state for Sentinel 1:**
- 2 outbound connections (initiated by Sentinel 1)
- 2 inbound connections (initiated by peers)
- Total: 4 TCP connections for 3-Sentinel mesh

### Why Full Mesh?

1. **Resilience:** Any Sentinel can talk to any other
2. **No coordinator:** No need to elect a leader
3. **Faster consensus:** Direct peer-to-peer voting
4. **Simplified failover:** All Sentinels have same information

### Quorum Voting Example

**Scenario:** Master is down

```
Sentinel 1 detects master down → Proposes failover
  ↓
  Sends vote request to Sentinel 2 and 3
  ↓
Sentinel 2: "I agree, master is down" ✓
Sentinel 3: "I agree, master is down" ✓
  ↓
Votes = 3 (all agree)
Quorum = 2 (majority of 3)
  ↓
✅ Quorum reached → Proceed with failover
```

**Without full mesh:**
- Would need relay messages through coordinator
- Higher latency
- Single point of failure
