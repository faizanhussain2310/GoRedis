# RDB Serialization and Deserialization Implementation

## Overview
Implemented actual RDB (Redis Database) file generation and loading for master-replica replication. Replaces the previous empty 19-byte placeholder RDB with full serialization/deserialization of all data types.

## Architecture

### Master Side: RDB Generation
**File**: `internal/handler/replication_handlers.go`

1. **generateRDB()** - Main RDB generation function
   - Called during PSYNC handshake
   - Retrieves store snapshot via `ReplicationManager.GetStoreSnapshot()`
   - Serializes all keys with their data types and expiry times
   - Returns binary RDB file as byte array

2. **Supported Data Types**:
   - `StringType` (RDB type 0): Simple key-value pairs
   - `ListType` (RDB type 1): Ordered lists
   - `SetType` (RDB type 2): Unordered unique sets
   - `HashType` (RDB type 4): Field-value hash maps
   - `ZSetType` (RDB type 3): Sorted sets (simplified, count=0)

3. **RDB Format**:
   ```
   REDIS0009              # Magic string + version
   0xFE 0x00             # SELECT DB 0
   0xFB <size> <exp>     # RESIZEDB (hash table sizes)
   
   # For each key with expiry:
   0xFC <ms>             # EXPIRETIME_MS (8 bytes little-endian)
   <type> <key> <value>  # Type-specific encoding
   
   # For each key without expiry:
   <type> <key> <value>  # Type-specific encoding
   
   0xFF                  # EOF
   <8 byte CRC64>        # CRC64 ECMA checksum (little-endian)
   ```

4. **Helper Functions**:
   - `writeLength()`: Encodes integers using Redis length encoding (6-bit, 14-bit, or 32-bit)
   - `writeString()`: Writes length-prefixed strings

### Replica Side: RDB Loading
**File**: `internal/replication/replica.go`

1. **loadRDBIntoStore()** - Main RDB parsing function
   - Called in `receiveReplicationStream()` when RDB is received
   - Parses RDB binary format
   - Executes commands via `executeReplicatedCommand()` to populate store

2. **Parsing Flow**:
   - Validates magic string "REDIS"
   - Reads version (e.g., "0009")
   - Processes opcodes sequentially:
     - `0xFE`: SELECTDB (database selection)
     - `0xFB`: RESIZEDB (size hints)
     - `0xFC`: EXPIRETIME_MS (milliseconds)
     - `0xFD`: EXPIRETIME (seconds)
     - `0xFF`: EOF (end of file)
     - `0-14`: Value type opcodes

3. **loadRDBValue()** - Type-specific deserialization
   - **String**: Executes `SET key value [PX ttl]`
   - **List**: Executes `RPUSH key element` for each item, then `PEXPIRE key ttl`
   - **Set**: Executes `SADD key member` for each item, then `PEXPIRE key ttl`
   - **Hash**: Executes `HSET key field value` for each pair, then `PEXPIRE key ttl`
   - **ZSet**: Executes `ZADD key score member` for each item, then `PEXPIRE key ttl`

4. **Helper Functions**:
   - `readLength()`: Decodes Redis length-encoded integers
   - `readString()`: Reads length-prefixed strings

### Store Access Integration
**File**: `internal/replication/replication.go`

1. **SetStoreGetter()**: Sets callback to retrieve store
   - Called during server initialization
   - Enables RDB generation without tight coupling

2. **GetStoreSnapshot()**: Retrieves current store state
   - Returns `*storage.Store` for snapshot
   - Uses COW (Copy-On-Write) optimization via `GetAllData()`
   - Must call `ReleaseSnapshot()` after use

3. **ReplicationManager fields**:
   ```go
   storeGetter   func() interface{}  // Callback to get store
   storeGetterMu sync.RWMutex        // Protects storeGetter
   ```

**File**: `internal/server/server.go`

- Wires up store access during initialization:
  ```go
  replMgr.SetStoreGetter(func() interface{} {
      return proc.GetStore()
  })
  ```

## Data Flow

### Full Sync (PSYNC with ? or mismatched replication ID)
```
MASTER:
1. Replica connects and sends PSYNC ? -1
2. handlePSync() called
3. Check if partial sync possible (replication ID match + offset in backlog)
4. If no: generateRDB(rm) called for full sync
5. rm.GetStoreSnapshot() retrieves store
6. Iterate all keys, serialize to RDB format
7. Calculate CRC64 checksum of RDB data
8. Send FULLRESYNC response + RDB as bulk string: $<size>\r\n<bytes>

REPLICA:
1. receiveReplicationStream() detects "$" prefix
2. Reads RDB size and bytes
3. loadRDBIntoStore(rdbData) called
4. Verify CRC64 checksum (log warning if mismatch)
5. Parse RDB opcodes sequentially
6. For each key-value: loadRDBValue()
7. Execute commands to populate store
8. Sync complete, enter online state
```

### Partial Sync (PSYNC with matching replication ID)
```
MASTER:
1. Replica sends PSYNC <replid> <offset>
2. handlePSync() checks if replid matches current master
3. Check if requested offset available in backlog
4. If yes: Send +CONTINUE response
5. Stream backlog data from offset to current
6. Add replica to replication manager
7. Replica enters online state

REPLICA:
1. Send PSYNC with known replid and offset
2. Receive +CONTINUE response (partial sync)
3. receiveReplicationStream() processes incremental commands
4. Apply commands to store
5. Sync complete, continue receiving command stream
```

## Expiry Handling

### Serialization (Master)
- Checks if `value.ExpiresAt != nil && value.ExpiresAt.After(time.Now())`
- Writes `0xFC` opcode + 8-byte millisecond timestamp (little-endian)
- Expired keys are skipped (not serialized)

### Deserialization (Replica)
- Reads expiry timestamp from RDB
- Calculates TTL: `ttl = expiryMs - currentMs`
- If TTL > 0: Executes `PEXPIRE key ttl` after loading value
- If TTL ≤ 0: Key is not loaded (already expired)

## Performance Considerations

1. **Snapshot Overhead**: Uses COW optimization from `storage.Store.GetAllData()`
   - Increments `snapshotCount` atomically
   - Shallow copies values (pointers shared until modified)
   - Must call `ReleaseSnapshot()` to decrement counter

2. **Encoding Efficiency**:
   - Length encoding: 6-bit (0-63), 14-bit (64-16383), 32-bit (16384+)
   - Minimizes overhead for small values

3. **Memory**: RDB fully loaded into memory before parsing
   - For large datasets, consider streaming parser (future improvement)

4. **Command Execution**: Each key executes via `executeReplicatedCommand()`
   - Uses existing command handlers (SET, RPUSH, SADD, etc.)
   - Ensures consistency with normal command processing
   - May be slower than direct store manipulation

## Limitations and Future Work

1. **~~CRC64 Checksum~~**: ✅ **IMPLEMENTED**
   - CRC64 ECMA checksum now calculated and verified
   - Master generates checksum during RDB creation
   - Replica verifies checksum on load (warns on mismatch)

2. **ZSet Encoding**: Simplified (writes count=0)
   - TODO: Properly serialize ZSet members with scores
   - Current implementation skips ZSet data

3. **~~Partial Sync~~**: ✅ **IMPLEMENTED**
   - Backlog-based partial resync now supported
   - Master checks if requested offset is in backlog
   - Sends `+CONTINUE` response and streams delta
   - Falls back to full sync if offset too old

4. **Special Encodings**: Not supported
   - Integer encoding (encType=3)
   - LZF compression
   - Quicklist, Ziplist, Intset encodings

5. **Multiple Databases**: Only DB 0 supported
   - Redis supports 16 databases (0-15)
   - TODO: Add multi-database support

6. **Error Recovery**: Limited error handling during RDB load
   - Corrupted RDB may leave partial data
   - TODO: Add transaction-like all-or-nothing loading

## Testing Recommendations

### Manual Testing
```bash
# Terminal 1: Start master
./redis-server --port 6379

# Terminal 2: Populate master
redis-cli SET key1 "value1"
redis-cli SET key2 "value2" EX 3600
redis-cli LPUSH list1 "item1" "item2" "item3"
redis-cli SADD set1 "member1" "member2"
redis-cli HSET hash1 field1 "val1" field2 "val2"

# Terminal 3: Start replica
./redis-server --port 6380 --replicaof 127.0.0.1 6379

# Verify replication
redis-cli -p 6380 GET key1      # Should return "value1"
redis-cli -p 6380 LRANGE list1 0 -1
redis-cli -p 6380 SMEMBERS set1
redis-cli -p 6380 HGETALL hash1
```

### Integration Tests Needed
1. Large dataset replication (10K+ keys)
2. Expiry preservation across sync
3. All data types (String, List, Set, Hash, ZSet)
4. Mixed expired/non-expired keys
5. Empty database RDB transfer
6. Error cases (corrupted RDB, network failure)

## Code Changes Summary

### Modified Files
1. **internal/handler/replication_handlers.go**
   - Added `generateRDB()` function with actual data serialization
   - Added `calculateCRC64()` for checksum calculation
   - Added `writeLength()`, `writeString()` helpers
   - Modified `handlePSync()` to support partial sync
   - Checks replication ID and backlog before full resync
   - Sends `+CONTINUE` for partial sync, `+FULLRESYNC` for full
   - Added imports: `bytes`, `encoding/binary`, `time`, `storage`, `hash/crc64`

2. **internal/replication/replica.go**
   - Added `loadRDBIntoStore()` function with RDB parsing
   - Added CRC64 checksum verification
   - Added `loadRDBValue()` function for type-specific deserialization
   - Added `readLength()`, `readString()` helpers
   - Modified `receiveReplicationStream()` to call `loadRDBIntoStore()`
   - Added imports: `encoding/binary`, `hash/crc64`

3. **internal/replication/replication.go**
   - Added `storeGetter` field to `ReplicationManager`
   - Added `storeGetterMu` mutex for thread-safe access
   - Added `SetStoreGetter()` method
   - Added `GetStoreSnapshot()` method
   - Added `GetBacklogData()` method for partial sync support

4. **internal/server/server.go**
   - Added store getter registration after `NewReplicationManager()`
   - Wires `proc.GetStore()` to replication manager

### No Changes Required
- `internal/storage/store.go`: Already has `GetAllData()` and `ReleaseSnapshot()`
- `internal/processor/processor.go`: Already has `GetStore()` method

## References
- [Redis RDB Format Specification](https://github.com/sripathikrishnan/redis-rdb-tools/wiki/Redis-RDB-Dump-File-Format)
- [Redis Replication Protocol](https://redis.io/docs/management/replication/)
- Original empty RDB: 19 bytes (REDIS0009 + EOF + checksum)
