# Redis Implementation in Go

A high-performance, feature-rich Redis clone written in Go, implementing the RESP protocol and supporting most Redis commands and data structures.

## 🚀 Features

### Core Data Structures
- **Strings** - Basic key-value operations with TTL support
- **Lists** - Doubly-linked lists with blocking operations (BLPOP, BRPOP, BLMOVE)
- **Hashes** - Field-value maps with atomic operations
- **Sets** - Unordered collections with set algebra (union, intersection, difference)
- **Sorted Sets (ZSets)** - Score-based sorted collections with range queries
- **Bitmaps** - Bit-level operations for compact storage
- **HyperLogLog** - Probabilistic cardinality estimation
- **Geospatial Indexes** - Location-based queries (GEOADD, GEORADIUS)
- **Bloom Filters** - Probabilistic membership testing

### Advanced Features
- **Lua Scripting** - Execute server-side scripts with EVAL/EVALSHA
- **Transactions** - ACID guarantees with MULTI/EXEC/WATCH
- **Pub/Sub** - Real-time messaging with pattern matching
- **Pipelining** - Batch command execution for maximum throughput
- **Blocking Operations** - Client blocking on list operations with timeout support

### Persistence
- **AOF (Append-Only File)** - Durability with configurable fsync policies
- **RDB Snapshots** - Point-in-time backups (BGSAVE, auto-save triggers)
- **AOF Rewriting** - Background compaction to reduce file size

### Replication & High Availability
- **Master-Replica Replication** - Asynchronous replication with PSYNC support
- **Sentinel Mode** - Automatic failover and monitoring
  - Peer-to-peer mesh topology (no single point of failure)
  - Quorum-based leader election
  - Automatic master promotion
  - Health monitoring and heartbeat detection

### Performance & Observability
- **Slow Query Log** - Track performance bottlenecks
- **Command Statistics** - Monitor operation metrics
- **Configurable Timeouts** - Fine-tune blocking and pipeline behavior
- **Concurrent Client Handling** - High-throughput connection management

### Protocol & Compatibility
- **RESP Protocol** - Full Redis Serialization Protocol implementation
- **Command Pipelining** - Process multiple commands in single network roundtrip
- **Binary Safe** - Handle arbitrary byte sequences
- **Redis-CLI Compatible** - Works with standard Redis clients

## 📋 Supported Commands

### String Commands
`GET`, `SET`, `SETEX`, `DEL`, `EXISTS`, `KEYS`, `EXPIRE`, `TTL`, `ECHO`, `PING`

### List Commands
`LPUSH`, `RPUSH`, `LPOP`, `RPOP`, `LLEN`, `LRANGE`, `LINDEX`, `LSET`, `LREM`, `LTRIM`, `LINSERT`, `BLPOP`, `BRPOP`, `BLMOVE`, `BRPOPLPUSH`

### Hash Commands
`HSET`, `HGET`, `HMGET`, `HDEL`, `HEXISTS`, `HLEN`, `HKEYS`, `HVALS`, `HGETALL`, `HSETNX`, `HINCRBY`, `HINCRBYFLOAT`

### Set Commands
`SADD`, `SREM`, `SISMEMBER`, `SMEMBERS`, `SCARD`, `SPOP`, `SRANDMEMBER`, `SUNION`, `SINTER`, `SDIFF`, `SMOVE`, `SUNIONSTORE`, `SINTERSTORE`, `SDIFFSTORE`

### Sorted Set Commands
`ZADD`, `ZREM`, `ZSCORE`, `ZRANK`, `ZREVRANK`, `ZCARD`, `ZRANGE`, `ZREVRANGE`, `ZRANGEBYSCORE`, `ZREVRANGEBYSCORE`, `ZINCRBY`, `ZCOUNT`, `ZPOPMIN`, `ZPOPMAX`, `ZREMRANGEBYSCORE`, `ZREMRANGEBYRANK`

### Geospatial Commands
`GEOADD`, `GEOPOS`, `GEODIST`, `GEOHASH`, `GEORADIUS`, `GEORADIUSBYMEMBER`

### Bloom Filter Commands
`BF.RESERVE`, `BF.ADD`, `BF.MADD`, `BF.EXISTS`, `BF.MEXISTS`, `BF.INFO`

### Bitmap Commands
`SETBIT`, `GETBIT`, `BITCOUNT`, `BITPOS`, `BITOP`, `BITFIELD`

### HyperLogLog Commands
`PFADD`, `PFCOUNT`, `PFMERGE`

### Pub/Sub Commands
`PUBLISH`, `SUBSCRIBE`, `UNSUBSCRIBE`, `PSUBSCRIBE`, `PUNSUBSCRIBE`, `PUBSUB`

### Transaction Commands
`MULTI`, `EXEC`, `DISCARD`, `WATCH`, `UNWATCH`

### Scripting Commands
`EVAL`, `EVALSHA`, `SCRIPT LOAD`, `SCRIPT EXISTS`, `SCRIPT FLUSH`, `SCRIPT KILL`

### Replication Commands
`REPLICAOF`, `SLAVEOF`, `PSYNC`, `REPLCONF`, `INFO REPLICATION`

### Server Commands
`FLUSHALL`, `BGSAVE`, `BGREWRITEAOF`, `SLOWLOG`, `CONFIG GET`, `CONFIG SET`, `COMMAND`, `INFO`

### Sentinel Commands
`SENTINEL MASTERS`, `SENTINEL REPLICAS`, `SENTINEL GET-MASTER-ADDR-BY-NAME`, `SENTINEL RESET`, `SENTINEL INFO`

## 🏗️ Architecture

### Design Principles
- **Clean Architecture** - Separation of concerns with layered design
- **Concurrency** - Thread-safe operations with goroutine-per-client model
- **Protocol Abstraction** - RESP protocol isolated in dedicated package
- **Pluggable Storage** - Modular storage backend for easy extension
- **Zero Dependencies** - Pure Go implementation, no external libraries

### Project Structure
```
redis/
├── cmd/
│   ├── server/      # Redis server binary
│   └── sentinel/    # Sentinel binary
├── internal/
│   ├── handler/     # Command handlers
│   ├── processor/   # Business logic layer
│   ├── storage/     # In-memory storage engine
│   ├── protocol/    # RESP protocol parser/encoder
│   ├── replication/ # Master-replica sync
│   ├── sentinel/    # Sentinel monitoring
│   ├── aof/         # AOF persistence
│   └── server/      # TCP server & networking
└── docs/            # Documentation
```

## 🚦 Getting Started

### Build
```bash
# Build Redis server
go build -o bin/redis-server cmd/server/main.go

# Build Sentinel
go build -o bin/redis-sentinel cmd/sentinel/main.go
```

### Run Redis Server
```bash
./bin/redis-server --port 6379
```

### Run Sentinel (High Availability)
```bash
./bin/redis-sentinel --port 26379 --peers 26380,26381
```

### Connect with redis-cli
```bash
redis-cli -p 6379
```

## ⚡ Performance Characteristics
- **Concurrent Clients** - Handles thousands of simultaneous connections
- **Pipelined Throughput** - Processes up to 1000 commands per pipeline batch
- **Low Latency** - Sub-millisecond response times for simple operations
- **Memory Efficient** - Optimized data structures with minimal overhead

## 📚 Use Cases
- **Caching Layer** - High-speed application cache
- **Session Store** - Web session management
- **Real-time Analytics** - Leaderboards, counters, metrics
- **Message Queue** - Pub/Sub and blocking list operations
- **Geospatial Applications** - Location-based services
- **Rate Limiting** - Token buckets with TTL
- **Probabilistic Counting** - HyperLogLog for cardinality estimation

## 🔧 Configuration
All configuration via command-line flags or CONFIG commands at runtime.
