# Redis Server - Commands Reference

Complete list of implemented Redis commands organized by data structure.

---

## 🔹 STRING COMMANDS (10)

| Command | Syntax | Description |
|---------|--------|-------------|
| SET | `SET key value` | Set string value |
| SETEX | `SETEX key seconds value` | Set with expiration |
| GET | `GET key` | Get string value |
| DEL | `DEL key [key ...]` | Delete one or more keys |
| EXISTS | `EXISTS key [key ...]` | Check if keys exist |
| INCR | `INCR key` | Increment integer value by 1 |
| DECR | `DECR key` | Decrement integer value by 1 |
| INCRBY | `INCRBY key increment` | Increment by integer |
| DECRBY | `DECRBY key decrement` | Decrement by integer |
| KEYS | `KEYS` | Get all keys |

---

## 🔹 LIST COMMANDS (10)

| Command | Syntax | Description |
|---------|--------|-------------|
| LPUSH | `LPUSH key element [element ...]` | Push to list head |
| RPUSH | `RPUSH key element [element ...]` | Push to list tail |
| LPOP | `LPOP key [count]` | Pop from list head |
| RPOP | `RPOP key [count]` | Pop from list tail |
| LLEN | `LLEN key` | Get list length |
| LRANGE | `LRANGE key start stop` | Get range of elements |
| LINDEX | `LINDEX key index` | Get element by index |
| LSET | `LSET key index element` | Set element by index |
| LTRIM | `LTRIM key start stop` | Trim list to range |
| LINSERT | `LINSERT key BEFORE\|AFTER pivot element` | Insert element |

---

## 🔹 HASH COMMANDS (12)

| Command | Syntax | Description |
|---------|--------|-------------|
| HSET | `HSET key field value [field value ...]` | Set hash fields |
| HGET | `HGET key field` | Get hash field value |
| HMGET | `HMGET key field [field ...]` | Get multiple field values |
| HDEL | `HDEL key field [field ...]` | Delete hash fields |
| HEXISTS | `HEXISTS key field` | Check if field exists |
| HLEN | `HLEN key` | Get number of fields |
| HKEYS | `HKEYS key` | Get all field names |
| HVALS | `HVALS key` | Get all field values |
| HGETALL | `HGETALL key` | Get all fields and values |
| HSETNX | `HSETNX key field value` | Set field if not exists |
| HINCRBY | `HINCRBY key field increment` | Increment field by integer |
| HINCRBYFLOAT | `HINCRBYFLOAT key field increment` | Increment field by float |

---

## 🔹 SET COMMANDS (13)

| Command | Syntax | Description |
|---------|--------|-------------|
| SADD | `SADD key member [member ...]` | Add members to set |
| SREM | `SREM key member [member ...]` | Remove members from set |
| SISMEMBER | `SISMEMBER key member` | Check if member exists |
| SMEMBERS | `SMEMBERS key` | Get all members |
| SCARD | `SCARD key` | Get set cardinality |
| SRANDMEMBER | `SRANDMEMBER key [count]` | Get random member(s) |
| SPOP | `SPOP key [count]` | Remove and return random member(s) |
| SUNION | `SUNION key [key ...]` | Union of sets |
| SINTER | `SINTER key [key ...]` | Intersection of sets |
| SDIFF | `SDIFF key [key ...]` | Difference of sets |
| SMOVE | `SMOVE source dest member` | Move member between sets |
| SUNIONSTORE | `SUNIONSTORE dest key [key ...]` | Store union result |
| SINTERSTORE | `SINTERSTORE dest key [key ...]` | Store intersection result |
| SDIFFSTORE | `SDIFFSTORE dest key [key ...]` | Store difference result |

---

## 🔹 SORTED SET COMMANDS (16)

| Command | Syntax | Description |
|---------|--------|-------------|
| ZADD | `ZADD key score member [score member ...]` | Add members with scores |
| ZREM | `ZREM key member [member ...]` | Remove members |
| ZSCORE | `ZSCORE key member` | Get member score |
| ZRANK | `ZRANK key member` | Get rank (ascending) |
| ZREVRANK | `ZREVRANK key member` | Get rank (descending) |
| ZCARD | `ZCARD key` | Get number of members |
| ZCOUNT | `ZCOUNT key min max` | Count members in score range |
| ZINCRBY | `ZINCRBY key increment member` | Increment member score |
| ZRANGE | `ZRANGE key start stop [WITHSCORES]` | Get range by rank (asc) |
| ZREVRANGE | `ZREVRANGE key start stop [WITHSCORES]` | Get range by rank (desc) |
| ZRANGEBYSCORE | `ZRANGEBYSCORE key min max` | Get range by score |
| ZREVRANGEBYSCORE | `ZREVRANGEBYSCORE key min max` | Get range by score (desc) |
| ZPOPMIN | `ZPOPMIN key` | Remove and return min score member |
| ZPOPMAX | `ZPOPMAX key` | Remove and return max score member |
| ZREMRANGEBYRANK | `ZREMRANGEBYRANK key start stop` | Remove range by rank |
| ZREMRANGEBYSCORE | `ZREMRANGEBYSCORE key min max` | Remove range by score |

---

## 🔹 BITMAP COMMANDS (8)

| Command | Syntax | Description |
|---------|--------|-------------|
| SETBIT | `SETBIT key offset value` | Set bit at offset |
| GETBIT | `GETBIT key offset` | Get bit at offset |
| BITCOUNT | `BITCOUNT key [start end]` | Count set bits |
| BITPOS | `BITPOS key bit [start] [end]` | Find first bit position |
| BITOP AND | `BITOP AND destkey srckey [srckey ...]` | Bitwise AND |
| BITOP OR | `BITOP OR destkey srckey [srckey ...]` | Bitwise OR |
| BITOP XOR | `BITOP XOR destkey srckey [srckey ...]` | Bitwise XOR |
| BITOP NOT | `BITOP NOT destkey srckey` | Bitwise NOT |

---

## 🔹 HYPERLOGLOG COMMANDS (3)

| Command | Syntax | Description |
|---------|--------|-------------|
| PFADD | `PFADD key element [element ...]` | Add elements to HyperLogLog |
| PFCOUNT | `PFCOUNT key [key ...]` | Get cardinality estimate |
| PFMERGE | `PFMERGE destkey sourcekey [sourcekey ...]` | Merge HyperLogLogs |

---

## 🔹 BLOOM FILTER COMMANDS (6)

| Command | Syntax | Description |
|---------|--------|-------------|
| BF.RESERVE | `BF.RESERVE key error_rate capacity` | Create Bloom filter |
| BF.ADD | `BF.ADD key item` | Add item to filter |
| BF.MADD | `BF.MADD key item [item ...]` | Add multiple items |
| BF.EXISTS | `BF.EXISTS key item` | Check if item exists |
| BF.MEXISTS | `BF.MEXISTS key item [item ...]` | Check multiple items |
| BF.INFO | `BF.INFO key` | Get filter information |

---

## 🔹 GEO COMMANDS (6)

| Command | Syntax | Description |
|---------|--------|-------------|
| GEOADD | `GEOADD key longitude latitude member [lon lat member ...]` | Add geo points |
| GEOPOS | `GEOPOS key member [member ...]` | Get positions |
| GEODIST | `GEODIST key member1 member2 [unit]` | Get distance between points |
| GEOHASH | `GEOHASH key member [member ...]` | Get geohash strings |
| GEORADIUS | `GEORADIUS key lon lat radius unit [options]` | Query by radius |
| GEORADIUSBYMEMBER | `GEORADIUSBYMEMBER key member radius unit [options]` | Query by member |

---

## 🔹 LUA SCRIPTING COMMANDS (5)

| Command | Syntax | Description |
|---------|--------|-------------|
| EVAL | `EVAL script numkeys key [key ...] arg [arg ...]` | Execute Lua script |
| EVALSHA | `EVALSHA sha1 numkeys key [key ...] arg [arg ...]` | Execute cached script |
| SCRIPT LOAD | `SCRIPT LOAD script` | Load script into cache |
| SCRIPT EXISTS | `SCRIPT EXISTS sha1 [sha1 ...]` | Check if scripts exist |
| SCRIPT FLUSH | `SCRIPT FLUSH` | Clear script cache |

### Lua API Functions

- `redis.call(command, ...)` - Execute Redis command (throws error on failure)
- `redis.pcall(command, ...)` - Execute Redis command (returns error table)
- `redis.log(level, message)` - Log message
- `redis.status_reply(status)` - Create status reply
- `redis.error_reply(error)` - Create error reply

### Lua Globals

- `KEYS` - Array of key arguments (1-indexed)
- `ARGV` - Array of additional arguments (1-indexed)

---

## 🔹 EXPIRY COMMANDS (2)

| Command | Syntax | Description |
|---------|--------|-------------|
| EXPIRE | `EXPIRE key seconds` | Set key expiration |
| TTL | `TTL key` | Get remaining time to live |

---

## 🔹 PERSISTENCE COMMANDS (2)

| Command | Syntax | Description |
|---------|--------|-------------|
| BGSAVE | `BGSAVE` | Save RDB snapshot in background |
| BGREWRITEAOF | `BGREWRITEAOF` | Rewrite AOF file in background |

---

## 🔹 SERVER COMMANDS (3)

| Command | Syntax | Description |
|---------|--------|-------------|
| PING | `PING [message]` | Test connection |
| FLUSHALL | `FLUSHALL` | Clear all keys |
| QUIT | `QUIT` | Close connection |

---

## 📊 COMMAND SUMMARY

| Category | Commands | Total |
|----------|----------|-------|
| String | SET, SETEX, GET, DEL, EXISTS, INCR, DECR, INCRBY, DECRBY, KEYS | 10 |
| List | LPUSH, RPUSH, LPOP, RPOP, LLEN, LRANGE, LINDEX, LSET, LTRIM, LINSERT | 10 |
| Hash | HSET, HGET, HMGET, HDEL, HEXISTS, HLEN, HKEYS, HVALS, HGETALL, HSETNX, HINCRBY, HINCRBYFLOAT | 12 |
| Set | SADD, SREM, SISMEMBER, SMEMBERS, SCARD, SRANDMEMBER, SPOP, SUNION, SINTER, SDIFF, SMOVE, SUNIONSTORE, SINTERSTORE, SDIFFSTORE | 14 |
| Sorted Set | ZADD, ZREM, ZSCORE, ZRANK, ZREVRANK, ZCARD, ZCOUNT, ZINCRBY, ZRANGE, ZREVRANGE, ZRANGEBYSCORE, ZREVRANGEBYSCORE, ZPOPMIN, ZPOPMAX, ZREMRANGEBYRANK, ZREMRANGEBYSCORE | 16 |
| Bitmap | SETBIT, GETBIT, BITCOUNT, BITPOS, BITOP (AND/OR/XOR/NOT) | 8 |
| HyperLogLog | PFADD, PFCOUNT, PFMERGE | 3 |
| Bloom Filter | BF.RESERVE, BF.ADD, BF.MADD, BF.EXISTS, BF.MEXISTS, BF.INFO | 6 |
| Geo | GEOADD, GEOPOS, GEODIST, GEOHASH, GEORADIUS, GEORADIUSBYMEMBER | 6 |
| Lua Scripting | EVAL, EVALSHA, SCRIPT LOAD, SCRIPT EXISTS, SCRIPT FLUSH | 5 |
| Expiry | EXPIRE, TTL | 2 |
| Persistence | BGSAVE, BGREWRITEAOF | 2 |
| Server | PING, FLUSHALL, QUIT | 3 |
| **TOTAL** | | **97** |

---

## 🚀 USAGE EXAMPLES

### String Operations
```bash
SET mykey "Hello World"
GET mykey
INCR counter
DECR counter
INCRBY counter 5
SETEX tempkey 60 "expires in 60s"
```

### List Operations
```bash
LPUSH mylist "item1" "item2" "item3"
LRANGE mylist 0 -1
LPOP mylist
LLEN mylist
```

### Hash Operations
```bash
HSET user:1 name "Alice" age "30" city "NYC"
HGET user:1 name
HGETALL user:1
HINCRBY user:1 age 1
```

### Set Operations
```bash
SADD myset "apple" "banana" "cherry"
SMEMBERS myset
SISMEMBER myset "apple"
SADD otherset "banana" "date"
SUNION myset otherset
```

### Sorted Set Operations
```bash
ZADD leaderboard 100 "Alice" 85 "Bob" 92 "Charlie"
ZRANGE leaderboard 0 -1 WITHSCORES
ZRANK leaderboard "Alice"
ZINCRBY leaderboard 5 "Bob"
```

### Lua Scripting
```lua
EVAL "return KEYS[1] .. ' ' .. ARGV[1]" 1 "Hello" "World"
EVAL "return redis.call('GET', KEYS[1])" 1 "mykey"
SCRIPT LOAD "return {1,2,3}"
```

---

## 📝 NOTES

- All commands are **atomic** (thread-safe)
- **TTL/Expiration** supported on all data types
- **Copy-on-Write (COW)** optimization for snapshots
- **RDB** and **AOF** persistence available
- **Lua 5.1** compatible scripting
- **RESP protocol** for client communication

---

**Version:** 1.0  
**Last Updated:** January 2026
