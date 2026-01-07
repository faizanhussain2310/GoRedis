# Redis Publish/Subscribe (Pub/Sub)

## Table of Contents
1. [What is Pub/Sub?](#what-is-pubsub)
2. [How It Works](#how-it-works)
3. [Architecture](#architecture)
4. [Message Types](#message-types)
5. [Commands](#commands)
6. [Pattern Matching](#pattern-matching)
7. [Use Cases](#use-cases)
8. [Performance Characteristics](#performance-characteristics)
9. [Best Practices](#best-practices)
10. [Comparison with Other Patterns](#comparison-with-other-patterns)

---

## What is Pub/Sub?

**Publish/Subscribe (Pub/Sub)** is a messaging pattern where senders (publishers) send messages to channels without knowledge of who will receive them, and receivers (subscribers) listen to channels of interest without knowledge of who sent the messages.

### Key Characteristics

✅ **Decoupled** - Publishers and subscribers don't know about each other  
✅ **Real-time** - Messages are delivered immediately to active subscribers  
✅ **Fire-and-forget** - Messages are not stored; only active subscribers receive them  
✅ **Many-to-many** - Multiple publishers can send to same channel, multiple subscribers can listen  
✅ **Scalable** - Supports high-throughput message distribution

---

## How It Works

### Basic Flow

```
Publisher ──PUBLISH news "Breaking News!"──► Channel "news" ──► Subscriber A
                                                              ├──► Subscriber B  
                                                              └──► Subscriber C
```

### Pattern Subscription

```
Publisher ──PUBLISH sports:football "Goal!"──► Pattern "sports:*" ──► Subscriber
Publisher ──PUBLISH sports:basketball "Score!"─┘
```

---

## Architecture

### Data Structures

#### 1. **Channel Subscriptions**

```
channels: map[string]map[string]*Subscriber
    ├── "news" → { "client1": *Subscriber, "client2": *Subscriber }
    ├── "sports" → { "client1": *Subscriber }
    └── "weather" → { "client3": *Subscriber }
```

#### 2. **Pattern Subscriptions**

```
patterns: map[string]map[string]*Subscriber
    ├── "news:*" → { "client1": *Subscriber }
    ├── "sports:*" → { "client2": *Subscriber, "client3": *Subscriber }
    └── "*.alerts" → { "client4": *Subscriber }
```

#### 3. **Subscriber Tracking**

```
subscriberChannels: map[string]map[string]bool
    ├── "client1" → { "news": true, "sports": true }
    └── "client2" → { "sports": true }

subscriberPatterns: map[string]map[string]bool
    ├── "client1" → { "news:*": true }
    └── "client2" → { "sports:*": true, "*.alerts": true }
```

### Message Delivery

```go
// When PUBLISH news "Hello" is executed:
1. Find direct channel subscribers for "news"
2. Find pattern subscribers matching "news"
3. Send message to all subscribers' channels (non-blocking)
4. Return total count of recipients
```

---

## Message Types

### 1. Subscribe Confirmation

```redis
SUBSCRIBE news sports

Response:
*3
$9
subscribe
$4
news
:1

*3
$9
subscribe
$6
sports
:2
```

**Format**: `[type, channel, subscription_count]`

### 2. Unsubscribe Confirmation

```redis
UNSUBSCRIBE news

Response:
*3
$11
unsubscribe
$4
news
:1
```

**Format**: `[type, channel, remaining_subscriptions]`

### 3. Channel Message

```redis
(Received when someone publishes to subscribed channel)

*3
$7
message
$4
news
$13
Breaking News!
```

**Format**: `[type, channel, payload]`

### 4. Pattern Message

```redis
(Received when channel matches subscribed pattern)

*4
$8
pmessage
$8
sports:*
$15
sports:football
$5
Goal!
```

**Format**: `[type, pattern, channel, payload]`

---

## Commands

### SUBSCRIBE

Subscribes the client to one or more channels. Enters pub/sub mode.

**Syntax**: `SUBSCRIBE channel [channel ...]`

**Returns**: Array of subscription confirmations (one per channel)

**Time Complexity**: O(N) where N = number of channels

**Example**:
```redis
SUBSCRIBE news sports

*3
$9
subscribe
$4
news
:1

*3
$9
subscribe
$6
sports
:2
```

**Use Case**: Listening to real-time messages from specific channels

**Important**: Once a client enters pub/sub mode, only pub/sub commands (SUBSCRIBE, PSUBSCRIBE, UNSUBSCRIBE, PUNSUBSCRIBE) and PING/QUIT are allowed.

---

### UNSUBSCRIBE

Unsubscribes the client from one or more channels. If no channels specified, unsubscribes from all.

**Syntax**: `UNSUBSCRIBE [channel [channel ...]]`

**Returns**: Array of unsubscription confirmations

**Time Complexity**: O(N) where N = number of channels

**Example**:
```redis
UNSUBSCRIBE news

*3
$11
unsubscribe
$4
news
:1

# Unsubscribe from all channels
UNSUBSCRIBE

*3
$11
unsubscribe
$6
sports
:0
```

**Use Case**: Stop receiving messages from specific channels

---

### PSUBSCRIBE

Subscribes the client to one or more patterns. Enters pub/sub mode.

**Syntax**: `PSUBSCRIBE pattern [pattern ...]`

**Returns**: Array of subscription confirmations (one per pattern)

**Time Complexity**: O(N) where N = number of patterns

**Example**:
```redis
PSUBSCRIBE news:* sports:*

*3
$10
psubscribe
$6
news:*
:1

*3
$10
psubscribe
$8
sports:*
:2
```

**Use Case**: Listening to messages from channels matching a pattern

---

### PUNSUBSCRIBE

Unsubscribes the client from one or more patterns. If no patterns specified, unsubscribes from all.

**Syntax**: `PUNSUBSCRIBE [pattern [pattern ...]]`

**Returns**: Array of unsubscription confirmations

**Time Complexity**: O(N) where N = number of patterns

**Example**:
```redis
PUNSUBSCRIBE news:*

*3
$12
punsubscribe
$6
news:*
:1
```

**Use Case**: Stop receiving messages from channels matching patterns

---

### PUBLISH

Publishes a message to a channel.

**Syntax**: `PUBLISH channel message`

**Returns**: Number of subscribers that received the message

**Time Complexity**: O(N+M) where N = subscribers to channel, M = subscribers to matching patterns

**Example**:
```redis
PUBLISH news "Breaking: Redis 7.0 released!"
(integer) 5  # 5 subscribers received the message
```

**Use Case**: Broadcasting real-time updates to interested clients

---

### PUBSUB CHANNELS

Lists currently active channels (channels with at least one subscriber).

**Syntax**: `PUBSUB CHANNELS [pattern]`

**Returns**: Array of channel names

**Time Complexity**: O(N) where N = number of active channels

**Examples**:
```redis
# List all active channels
PUBSUB CHANNELS
1) "news"
2) "sports"
3) "weather"

# List channels matching pattern
PUBSUB CHANNELS news:*
1) "news:breaking"
2) "news:tech"
```

**Use Case**: Service discovery, monitoring active communication channels

---

### PUBSUB NUMSUB

Returns the number of subscribers for specified channels.

**Syntax**: `PUBSUB NUMSUB [channel ...]`

**Returns**: Flat array of channel names and subscriber counts

**Time Complexity**: O(N) where N = number of requested channels

**Examples**:
```redis
PUBSUB NUMSUB news sports weather
1) "news"
2) (integer) 10
3) "sports"
4) (integer) 5
5) "weather"
6) (integer) 0
```

**Use Case**: Monitoring channel popularity, load balancing

---

### PUBSUB NUMPAT

Returns the number of unique patterns currently subscribed to.

**Syntax**: `PUBSUB NUMPAT`

**Returns**: Integer count of active pattern subscriptions

**Time Complexity**: O(1)

**Example**:
```redis
PUBSUB NUMPAT
(integer) 3
```

**Use Case**: Monitoring pattern subscription overhead

---

## Quick Start / Usage Examples

### Example 1: Basic Pub/Sub

**Terminal 1 (Subscriber)**:
```redis
SUBSCRIBE news
# Client enters pub/sub mode and waits for messages

# Response:
*3
$9
subscribe
$4
news
:1
```

**Terminal 2 (Publisher)**:
```redis
PUBLISH news "Hello, World!"
# Returns: (integer) 1

PUBLISH news "Breaking news!"
# Returns: (integer) 1
```

**Terminal 1 (Subscriber receives)**:
```redis
*3
$7
message
$4
news
$13
Hello, World!

*3
$7
message
$4
news
$14
Breaking news!
```

---

### Example 2: Pattern Subscriptions

**Subscriber**:
```redis
PSUBSCRIBE news:*
# Subscribes to all channels starting with "news:"

*3
$10
psubscribe
$6
news:*
:1
```

**Publisher**:
```redis
PUBLISH news:tech "New iPhone released"
# Returns: (integer) 1

PUBLISH news:sports "Team wins championship"
# Returns: (integer) 1

PUBLISH weather "Sunny"
# Returns: (integer) 0  (no subscribers)
```

**Subscriber receives**:
```redis
*4
$8
pmessage
$6
news:*
$9
news:tech
$20
New iPhone released

*4
$8
pmessage
$6
news:*
$11
news:sports
$23
Team wins championship
```

---

### Example 3: Multiple Subscriptions

```redis
SUBSCRIBE channel1 channel2 channel3
PSUBSCRIBE pattern:*

# Client is now subscribed to 3 channels + 1 pattern
# Total subscription count: 4

UNSUBSCRIBE channel1
# Remaining: 3 subscriptions

PUNSUBSCRIBE pattern:*
# Remaining: 2 subscriptions

UNSUBSCRIBE
# Unsubscribes from all remaining channels
# Exits pub/sub mode
```

---

### Example 4: Pub/Sub Mode Restrictions

```redis
SUBSCRIBE news
# Client enters pub/sub mode

GET mykey
# Error: ERR only (P)SUBSCRIBE / (P)UNSUBSCRIBE / PING / QUIT allowed in this context

PING
# Returns: PONG (allowed)

UNSUBSCRIBE news
# Exits pub/sub mode

GET mykey
# Now works normally
```

---

## Pattern Matching

Redis Pub/Sub supports glob-style pattern matching for channel names.

### Supported Wildcards

#### 1. **Asterisk (*)** - Matches any sequence of characters

```redis
PSUBSCRIBE news:*

Matches:
✅ news:breaking
✅ news:tech
✅ news:sports
❌ news (no colon)
❌ weather:news (doesn't start with news:)
```

#### 2. **Question Mark (?)** - Matches exactly one character

```redis
PSUBSCRIBE user:?

Matches:
✅ user:1
✅ user:a
❌ user:10 (two characters)
❌ user: (no character)
```

### Pattern Examples

| Pattern | Matches | Doesn't Match |
|---------|---------|---------------|
| `news:*` | `news:tech`, `news:sports:football` | `news`, `sports:news` |
| `*.alerts` | `system.alerts`, `security.alerts` | `alerts`, `system.alerts.critical` |
| `user:?:*` | `user:1:profile`, `user:a:settings` | `user::`, `user:10:profile` |
| `log:*:error` | `log:app:error`, `log:database:error` | `log:error`, `log:app:warning` |

### Pattern Implementation

```go
// Convert glob pattern to regex
func matchPattern(pattern, channel string) bool {
    regexPattern := regexp.QuoteMeta(pattern)
    regexPattern = strings.ReplaceAll(regexPattern, `\*`, `.*`)  // * → .*
    regexPattern = strings.ReplaceAll(regexPattern, `\?`, `.`)   // ? → .
    regexPattern = "^" + regexPattern + "$"
    
    re := regexp.Compile(regexPattern)
    return re.MatchString(channel)
}
```

---

## Use Cases

### 1. **Real-Time Notifications**

```redis
# Mobile app subscribes to user-specific notifications
SUBSCRIBE user:123:notifications

# Backend publishes notification
PUBLISH user:123:notifications "New message from Alice"
```

**Scenario**: Chat applications, social media notifications, email alerts

---

### 2. **Live Dashboards**

```redis
# Dashboard subscribes to metrics
PSUBSCRIBE metrics:*

# Services publish metrics
PUBLISH metrics:cpu "85%"
PUBLISH metrics:memory "4.2GB"
PUBLISH metrics:requests "1520/s"
```

**Scenario**: Real-time monitoring, analytics dashboards, DevOps tools

---

### 3. **Event Broadcasting**

```redis
# Multiple services subscribe to events
SUBSCRIBE order:created
SUBSCRIBE order:completed

# Order service publishes events
PUBLISH order:created '{"id": 123, "user": "alice", "total": 99.99}'
PUBLISH order:completed '{"id": 123, "status": "shipped"}'
```

**Scenario**: Microservices event bus, workflow automation, audit logging

---

### 4. **Cache Invalidation**

```redis
# Web servers subscribe to cache invalidation
SUBSCRIBE cache:invalidate

# Admin publishes invalidation
PUBLISH cache:invalidate "user:profile:*"
```

**Scenario**: Distributed cache consistency, CDN purging

---

### 5. **Chat Rooms**

```redis
# Users subscribe to chat rooms
SUBSCRIBE chat:general
SUBSCRIBE chat:tech

# Users send messages
PUBLISH chat:general "Alice: Hello everyone!"
PUBLISH chat:tech "Bob: Check out this Redis feature!"
```

**Scenario**: Chat applications, collaboration tools, gaming lobbies

---

### 6. **IoT Device Updates**

```redis
# Devices subscribe to firmware updates
PSUBSCRIBE device:thermostat:*:update

# Control system publishes updates
PUBLISH device:thermostat:living-room:update "firmware-v2.1.0"
PUBLISH device:thermostat:bedroom:update "firmware-v2.1.0"
```

**Scenario**: IoT platforms, smart home systems, device management

---

## Performance Characteristics

### Time Complexity

| Operation | Complexity | Explanation |
|-----------|------------|-------------|
| PUBLISH | O(N+M) | N = channel subscribers, M = pattern subscribers |
| PUBSUB CHANNELS | O(N) | N = total active channels |
| PUBSUB NUMSUB | O(K) | K = number of requested channels |
| PUBSUB NUMPAT | O(1) | Constant time lookup |

### Throughput

**Benchmark Results** (approx):
```
PUBLISH:          ~100K-200K messages/sec
Pattern matching: ~10K-50K pattern checks/sec
```

### Memory Usage

```
Per Subscriber:  ~1 KB (channel buffer + metadata)
Per Channel:     ~100 bytes (map overhead)
Per Pattern:     ~150 bytes (map + regex)
```

**Example**: 1000 subscribers on 100 channels ≈ 1 MB

### Scalability Considerations

✅ **Horizontal**: Can scale subscribers infinitely  
✅ **Channel count**: Handles millions of channels efficiently  
⚠️ **Pattern overhead**: Each publish checks all patterns  
⚠️ **Message persistence**: None - only for active subscribers

---

## Best Practices

### 1. **Channel Naming Conventions**

```redis
✅ Good:
  user:123:notifications
  order:created
  metrics:cpu:server1

❌ Avoid:
  notifications (too generic)
  u123n (cryptic)
  user_123_notifications (inconsistent separator)
```

**Use hierarchical naming**: `category:subcategory:identifier:event`

---

### 2. **Pattern Usage**

```redis
✅ Efficient:
  PSUBSCRIBE user:*:notifications  # Specific enough

❌ Inefficient:
  PSUBSCRIBE *  # Matches everything, high overhead
```

**Tip**: Be as specific as possible with patterns to reduce matching overhead.

---

### 3. **Message Size**

```redis
✅ Optimal:
  PUBLISH alerts "CPU: 95%"  # Small, focused message

❌ Problematic:
  PUBLISH data "{... 10MB JSON ...}"  # Large payloads
```

**Recommendation**: Keep messages < 1 KB. For large data, publish IDs and fetch from storage.

---

### 4. **Error Handling**

```redis
# Subscribers should handle missed messages
if subscriber disconnects:
    - Resubscribe on reconnect
    - Pull latest state from persistent storage
    - Don't rely on receiving every message
```

---

### 5. **Monitoring**

```redis
# Regularly check channel health
PUBSUB CHANNELS
PUBSUB NUMSUB critical-channel
PUBSUB NUMPAT

# Track:
- Number of active channels
- Subscriber counts per channel
- Pattern subscription overhead
```

---

### 6. **When NOT to Use Pub/Sub**

❌ **Message Persistence** - Use streams or lists if you need message history  
❌ **Guaranteed Delivery** - Use message queues (RabbitMQ, Kafka)  
❌ **Complex Routing** - Use dedicated message brokers  
❌ **Request-Response** - Use direct commands or RPC  
❌ **Transaction Coordination** - Use distributed transactions (Saga pattern)

---

## Comparison with Other Patterns

### Pub/Sub vs Streams

| Feature | Pub/Sub | Streams |
|---------|---------|---------|
| **Persistence** | ❌ No | ✅ Yes (on-disk) |
| **Message History** | ❌ No | ✅ Yes (replay) |
| **Consumer Groups** | ❌ No | ✅ Yes |
| **Latency** | 🚀 Microseconds | ⚡ Milliseconds |
| **Guaranteed Delivery** | ❌ Only to active subscribers | ✅ Yes (until acknowledged) |
| **Use Case** | Real-time broadcasting | Event sourcing, audit logs |

---

### Pub/Sub vs Lists (Message Queues)

| Feature | Pub/Sub | Lists (LPUSH/RPOP) |
|---------|---------|-------------------|
| **Subscribers** | ✅ Many (fan-out) | ❌ One at a time |
| **Persistence** | ❌ No | ✅ Yes |
| **Blocking** | ❌ No | ✅ Yes (BLPOP) |
| **Load Balancing** | ❌ All receive same | ✅ Competing consumers |
| **Use Case** | Notifications, broadcasts | Task queues, job processing |

---

### Pub/Sub vs Keyspace Notifications

| Feature | Pub/Sub | Keyspace Notifications |
|---------|---------|----------------------|
| **Trigger** | Explicit PUBLISH | Redis key operations |
| **Control** | ✅ Full | ⚠️ Limited (config) |
| **Overhead** | 🟢 Low | 🟡 Medium (every operation) |
| **Use Case** | Application messaging | Cache invalidation, monitoring |

---

## Implementation Details

### Message Delivery Guarantees

**At-Most-Once Delivery**:
```
Publisher ──PUBLISH──► Redis ──► Subscriber
                         ↓
                      (subscriber offline)
                    Message discarded ❌
```

**No Ordering Guarantees** across channels:
```
PUBLISH ch1 "msg1"
PUBLISH ch2 "msg2"
PUBLISH ch1 "msg3"

# Subscriber to both might receive:
msg2, msg1, msg3  # Order not guaranteed across channels
```

### Subscription Limits

```
Channels per subscriber:  Unlimited (practical limit ~100K)
Subscribers per channel:   Unlimited (practical limit ~10K)
Pattern subscriptions:     Unlimited (but adds overhead)
```

### Thread Safety

```go
// All PubSub operations are thread-safe
type PubSub struct {
    channels map[string]map[string]*Subscriber
    patterns map[string]map[string]*Subscriber
    mu       sync.RWMutex  // Protects all maps
}
```

---

## Summary

### When to Use Redis Pub/Sub

✅ **Real-time notifications** to multiple clients  
✅ **Event broadcasting** in microservices  
✅ **Live dashboards** with instant updates  
✅ **Chat systems** and collaboration tools  
✅ **Cache invalidation** across distributed servers

### Key Advantages

🚀 **Ultra-low latency** (microseconds)  
🎯 **Simple API** (just PUBLISH + introspection)  
📡 **Decoupled architecture** (publishers don't know subscribers)  
⚡ **High throughput** (100K+ messages/sec)  
🔌 **Pattern matching** (flexible channel subscriptions)

### Key Limitations

⚠️ **No persistence** - Messages lost if no active subscribers  
⚠️ **No guaranteed delivery** - Fire-and-forget only  
⚠️ **No message history** - Can't replay past messages  
⚠️ **Single-threaded** processor (but still very fast)

---

## Further Reading

- **Redis Streams**: For persistent messaging with consumer groups
- **Keyspace Notifications**: For automatic notifications on key changes
- **Message Brokers**: For guaranteed delivery (RabbitMQ, Kafka, NATS)
- **Event Sourcing**: For building applications around event streams

---

**Implementation Status**: ✅ Full Pub/Sub (PUBLISH, SUBSCRIBE, PSUBSCRIBE, UNSUBSCRIBE, PUNSUBSCRIBE + introspection commands)  
**Note**: Connection enters pub/sub mode when SUBSCRIBE or PSUBSCRIBE is called. In pub/sub mode, only pub/sub commands and PING/QUIT are allowed.
