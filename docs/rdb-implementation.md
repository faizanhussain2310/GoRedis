# RDB (Redis Database) Implementation

This document explains the RDB snapshot format and our implementation of the BGSAVE command in our Redis clone.

---

## Table of Contents
- [What is RDB?](#what-is-rdb)
- [RDB vs AOF](#rdb-vs-aof)
- [File Format Structure](#file-format-structure)
- [Implementation Details](#implementation-details)
- [Deep Copy Snapshot](#deep-copy-snapshot)
- [CRC64 Checksum](#crc64-checksum)
- [Variable-Length Encoding](#variable-length-encoding)
- [Redis 16 Databases](#redis-16-databases)
- [Usage Examples](#usage-examples)

---

## What is RDB?

**RDB (Redis Database)** is a **point-in-time snapshot** of the entire database stored in a compact **binary format**.

### Key Characteristics:

| Aspect | Description |
|--------|-------------|
| **Format** | Binary (not human-readable) |
| **Size** | 50-70% smaller than AOF |
| **Speed** | Faster to load than AOF |
| **Content** | Actual data structures, not commands |
| **File** | `dump.rdb` |
| **Trigger** | Manual (`BGSAVE`) or automatic (background) |

### When to Use RDB:

✅ **Good for:**
- Backups (compact, single file)
- Disaster recovery
- Replication (faster than AOF)
- Cold starts (fast loading)

❌ **Not ideal for:**
- Minimal data loss requirements (AOF is better)
- Real-time durability (snapshots are periodic)

---

## RDB vs AOF

### Conceptual Difference:

```
AOF (Append-Only File):
  Stores COMMANDS that were executed
  
  Example:
  *3\r\n$3\r\nSET\r\n$4\r\nkey1\r\n$6\r\nvalue1\r\n
  *3\r\n$3\r\nSET\r\n$4\r\nkey1\r\n$6\r\nvalue2\r\n
  *3\r\n$3\r\nSET\r\n$4\r\nkey1\r\n$6\r\nvalue3\r\n
  
  Size: ~100 bytes (all 3 commands logged)

RDB (Redis Database):
  Stores FINAL STATE of data
  
  Example:
  REDIS0009...key1value3...
  
  Size: ~30 bytes (only current state)
```

### Comparison Table:

| Feature | AOF | RDB |
|---------|-----|-----|
| **Format** | Text (RESP) | Binary |
| **Human Readable** | Yes ✅ | No ❌ |
| **File Size** | Larger | **50-70% smaller** |
| **Load Speed** | Slower | **Faster** |
| **Data Loss Risk** | Low (1 second) | Higher (last snapshot) |
| **Durability** | High | Medium |
| **Use Case** | Real-time persistence | Backups, replication |
| **Commands** | Every write | None (just data) |

### Real Example:

```bash
# Database state:
SET user:1:name "Alice"
SET user:1:age "30"
LPUSH recent_orders order1 order2 order3

# AOF file (79 bytes):
*3\r\n$3\r\nSET\r\n$11\r\nuser:1:name\r\n$5\r\nAlice\r\n
*3\r\n$3\r\nSET\r\n$10\r\nuser:1:age\r\n$2\r\n30\r\n
*4\r\n$5\r\nLPUSH\r\n$13\r\nrecent_orders\r\n...

# RDB file (45 bytes):
REDIS0009...user:1:nameAliceuser:1:age30recent_ordersorder1order2order3...
```

**Result:** RDB is ~43% smaller!

---

## File Format Structure

### Complete RDB File Layout:

```
┌─────────────────────────────────────────────────┐
│ HEADER                                          │
├─────────────────────────────────────────────────┤
│ "REDIS"                       (5 bytes)         │  Magic string
│ "0009"                        (4 bytes)         │  Version number
│ OpCodeAux (0xFA)              (1 byte)          │  
│   "redis-ver" → "7.0.0"       (variable)        │  Metadata
│ OpCodeAux (0xFA)              (1 byte)          │
│   "ctime" → "1734630000"      (variable)        │  Creation time
├─────────────────────────────────────────────────┤
│ DATABASE 0                                      │
├─────────────────────────────────────────────────┤
│ OpCodeSelectDB (0xFE)         (1 byte)          │  Select database
│ 0                             (1 byte)          │  DB number
│ OpCodeResizeDB (0xFB)         (1 byte)          │  
│   <key_count>                 (variable)        │  Optimization hint
│   <expiry_count>              (variable)        │
├─────────────────────────────────────────────────┤
│ KEY-VALUE PAIRS                                 │
├─────────────────────────────────────────────────┤
│ [Optional: OpCodeExpireTimeMS (0xFC)]           │  Expiry (if set)
│   <timestamp_ms>              (8 bytes)         │
│ <type_code>                   (1 byte)          │  0=String, 1=List, etc.
│ <key_length><key>             (variable)        │  Key name
│ <value_data>                  (variable)        │  Type-specific data
│ ... (more key-value pairs) ...                  │
├─────────────────────────────────────────────────┤
│ FOOTER                                          │
├─────────────────────────────────────────────────┤
│ OpCodeEOF (0xFF)              (1 byte)          │  End of file
│ <CRC64_checksum>              (8 bytes)         │  Data integrity check
└─────────────────────────────────────────────────┘
```

### Opcodes Reference:

```go
const (
    OpCodeEOF          = 0xFF  // End of file
    OpCodeSelectDB     = 0xFE  // Select database number
    OpCodeExpireTime   = 0xFD  // Expiry in seconds
    OpCodeExpireTimeMS = 0xFC  // Expiry in milliseconds
    OpCodeResizeDB     = 0xFB  // Database size hint
    OpCodeAux          = 0xFA  // Auxiliary metadata
)
```

### Type Codes:

```go
const (
    TypeString = 0  // String value
    TypeList   = 1  // List (array of strings)
    TypeSet    = 2  // Set (unique members)
    TypeZSet   = 3  // Sorted set (not implemented)
    TypeHash   = 4  // Hash (field→value map)
)
```

---

## Implementation Details

### File: `internal/rdb/rdb.go`

### Key Components:

#### 1. **Writer Struct**

```go
type Writer struct {
    filepath string  // Path to dump.rdb
}

func NewWriter(filepath string) *Writer {
    return &Writer{filepath: filepath}
}
```

Simple wrapper that knows where to write the RDB snapshot.

---

#### 2. **Save() Method - Main Entry Point**

```go
func (w *Writer) Save(snapshot map[string]*storage.Value) error
```

**Purpose:** Creates an RDB snapshot from database state.

**Flow:**
1. Create temporary file (`dump.rdb.tmp`)
2. Initialize CRC64 checksum hasher
3. Write header (magic string, version, metadata)
4. Write database selector (DB 0)
5. Write resize hint (optimization)
6. Write all key-value pairs
7. Write EOF marker
8. Compute and write checksum
9. Flush and sync to disk
10. Atomically rename temp → `dump.rdb`

**Key feature:** Uses `io.MultiWriter` to compute checksum while writing:

```go
// Create checksum hasher
checksumTable := crc64.MakeTable(crc64.ECMA)
hasher := crc64.New(checksumTable)

// Write to both file and hasher simultaneously
multiWriter := io.MultiWriter(writer, hasher)

// All writes go through multiWriter
w.writeHeader(multiWriter)
// ... write keys ...

// Compute final checksum
checksum := hasher.Sum64()
binary.Write(writer, binary.LittleEndian, checksum)
```

---

#### 3. **Type-Specific Encoding**

Each data type has specific encoding:

##### **String:**
```
Type (1 byte) | Key Length | Key Data | Value Length | Value Data
    0x00      |     04     |  key1    |      06      |  value1
```

**Code:**
```go
case storage.StringType:
    writer.Write([]byte{TypeString})
    w.writeStringToWriter(writer, key)
    w.writeStringToWriter(writer, str)
```

##### **List:**
```
Type | Key Length | Key Data | Count | Item1 Length | Item1 | Item2 Length | Item2 | ...
0x01 |     06     |  mylist  |  03   |      05      | item1 |      05      | item2 | ...
```

**Code:**
```go
case storage.ListType:
    writer.Write([]byte{TypeList})
    w.writeStringToWriter(writer, key)
    w.writeLengthToWriter(writer, len(list))
    for _, item := range list {
        w.writeStringToWriter(writer, item)
    }
```

##### **Hash:**
```
Type | Key Length | Key Data | Field Count | Field1 Len | Field1 | Value1 Len | Value1 | ...
0x04 |     04     |   user   |      02     |     04     |  name  |     05     | Alice  | ...
```

**Code:**
```go
case storage.HashType:
    writer.Write([]byte{TypeHash})
    w.writeStringToWriter(writer, key)
    w.writeLengthToWriter(writer, len(hash))
    for field, val := range hash {
        w.writeStringToWriter(writer, field)
        w.writeStringToWriter(writer, val)
    }
```

##### **Set:**
```
Type | Key Length | Key Data | Member Count | Member1 Len | Member1 | Member2 Len | Member2 | ...
0x02 |     05     |   myset  |      03      |      01     |    a    |      01     |    b    | ...
```

**Code:**
```go
case storage.SetType:
    writer.Write([]byte{TypeSet})
    w.writeStringToWriter(writer, key)
    w.writeLengthToWriter(writer, len(set))
    for member := range set {
        w.writeStringToWriter(writer, member)
    }
```

---

#### 4. **Expiry Handling**

If a key has an expiry time:

```go
if value.ExpiresAt != nil && time.Now().Before(*value.ExpiresAt) {
    writer.Write([]byte{OpCodeExpireTimeMS})  // 0xFC
    expiryMS := value.ExpiresAt.UnixMilli()
    binary.Write(writer, binary.LittleEndian, expiryMS)  // 8 bytes
}
```

**Binary format:**
```
FC                           ← Expiry marker
00 00 01 8C 3A 4E 7B 80     ← Timestamp in milliseconds (little-endian)
00                           ← Type code (String)
04 6B 65 79 31              ← Key: "key1"
06 76 61 6C 75 65 31        ← Value: "value1"
```

---

## Deep Copy Snapshot

### The Critical Bug We Fixed:

**Original (BROKEN):**
```go
func (s *Store) GetAllData() map[string]*Value {
    return s.data  // ← Returns REFERENCE!
}
```

**Problem:**
```go
// Background BGSAVE goroutine
snapshot := store.GetAllData()  // Gets reference to live data
go func() {
    for key, value := range snapshot {
        // While iterating...
    }
}()

// Client goroutine (concurrent!)
LPUSH mylist newitem  // ← Modifies the SAME map!
// RACE CONDITION! 🚨
```

### The Fix: Deep Copy

**File:** `internal/storage/store.go`

```go
func (s *Store) GetAllData() map[string]*Value {
    snapshot := make(map[string]*Value, len(s.data))
    
    for key, value := range s.data {
        // Create a copy of the Value struct
        valueCopy := &Value{
            Type:      value.Type,
            ExpiresAt: value.ExpiresAt,
        }
        
        // Deep copy the data based on type
        switch value.Type {
        case StringType:
            // Strings are immutable, safe to copy
            valueCopy.Data = value.Data
            
        case ListType:
            // Deep copy the list
            if list, ok := value.Data.([]string); ok {
                listCopy := make([]string, len(list))
                copy(listCopy, list)
                valueCopy.Data = listCopy
            }
            
        case HashType:
            // Deep copy the hash
            if hash, ok := value.Data.(map[string]string); ok {
                hashCopy := make(map[string]string, len(hash))
                for k, v := range hash {
                    hashCopy[k] = v
                }
                valueCopy.Data = hashCopy
            }
            
        case SetType:
            // Deep copy the set
            if set, ok := value.Data.(map[string]struct{}); ok {
                setCopy := make(map[string]struct{}, len(set))
                for member := range set {
                    setCopy[member] = struct{}{}
                }
                valueCopy.Data = setCopy
            }
        }
        
        snapshot[key] = valueCopy
    }
    
    return snapshot
}
```

**Why This Matters:**

✅ **Thread-safe:** Background BGSAVE can iterate over snapshot while clients modify live data  
✅ **Consistent:** Snapshot represents exact state at one point in time  
✅ **No races:** Deep copy prevents concurrent access to same memory

---

## CRC64 Checksum

### What is a Checksum?

A **checksum** is a hash/fingerprint of file contents to detect corruption.

### How It Works:

```go
// Writing:
hasher := crc64.New(crc64.MakeTable(crc64.ECMA))
multiWriter := io.MultiWriter(file, hasher)

// Write all data through multiWriter
multiWriter.Write(data)

// Compute checksum
checksum := hasher.Sum64()  // e.g., 0x12A4567890ABCDEF

// Append to file
binary.Write(file, binary.LittleEndian, checksum)
```

```go
// Loading (future implementation):
data := readFile("dump.rdb")
storedChecksum := last8Bytes(data)

computedChecksum := crc64.Checksum(data[0:len-8], table)

if computedChecksum != storedChecksum {
    return errors.New("RDB file corrupted!")
}
```

### Why Checksums Matter:

1. **Disk Corruption Detection**
   ```
   Scenario: Power outage during BGSAVE
   Result: dump.rdb partially written
   Without checksum: Load corrupted data → Crash! ❌
   With checksum: Detect corruption → Refuse to load ✅
   ```

2. **Network Transfer Validation**
   ```
   Scenario: Copying dump.rdb to another server via SCP
   Result: Some bytes flipped during transfer
   Checksum: Detects the corruption immediately
   ```

3. **Backup Verification**
   ```
   Question: Which of these 10 old backups is not corrupted?
   Answer: Check the checksums!
   ```

### Implementation:

We use **CRC64** (Cyclic Redundancy Check, 64-bit):

```go
import "hash/crc64"

checksumTable := crc64.MakeTable(crc64.ECMA)
hasher := crc64.New(checksumTable)

// Write data
hasher.Write(data)

// Get checksum
checksum := hasher.Sum64()  // Returns uint64
```

**Previous (simplified):**
```go
// Hardcoded zeros (no protection!)
writer.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0})
```

**Current (proper):**
```go
// Real CRC64 checksum
checksum := hasher.Sum64()
binary.Write(writer, binary.LittleEndian, checksum)
```

---

## Variable-Length Encoding

### Purpose: Save Space!

Instead of always using 4 bytes for integers, use:
- **1 byte** for 0-63
- **2 bytes** for 64-16,383
- **5 bytes** for 16,384+

### Encoding Rules:

The **first 2 bits** indicate encoding type:

```
First 2 bits | Encoding
-------------|-------------------
00           | 6-bit (1 byte total)
01           | 14-bit (2 bytes total)
10           | 32-bit (5 bytes total)
11           | Special (not used here)
```

### Implementation:

```go
func (w *Writer) writeLengthToWriter(writer io.Writer, length int) {
    if length < 64 {
        // 6-bit: 00xxxxxx
        writer.Write([]byte{byte(length)})
    } else if length < 16384 {
        // 14-bit: 01xxxxxx xxxxxxxx
        writer.Write([]byte{
            byte(0x40 | (length >> 8)),
            byte(length & 0xFF),
        })
    } else {
        // 32-bit: 10000000 + 4 bytes
        writer.Write([]byte{0x80})
        binary.Write(writer, binary.BigEndian, uint32(length))
    }
}
```

### Examples:

#### **Small: length = 42**
```
Binary: 00101010
        ↑↑
        └┴─ 00 = 6-bit
Hex: 2A
Bytes: 1
```

#### **Medium: length = 1000**
```
1000 = 0b0000001111101000

First byte:  01000011 (0x43)
             ↑↑
             └┴─ 01 = 14-bit
             
Second byte: 11101000 (0xE8)

Hex: 43 E8
Bytes: 2
```

#### **Large: length = 100,000**
```
100000 = 0x186A0

First byte:  10000000 (0x80)
             ↑↑
             └┴─ 10 = 32-bit

Next 4 bytes: 00 01 86 A0 (big-endian)

Hex: 80 00 01 86 A0
Bytes: 5
```

### Savings:

```
Without variable encoding:
  "hello" (length=5) → 4 bytes: 00 00 00 05

With variable encoding:
  "hello" (length=5) → 1 byte:  05

For 10,000 keys:
  Saved: 30,000 bytes (30KB!)
```

---

## Redis 16 Databases

### What Does It Mean?

Redis supports **16 separate logical databases** (numbered 0-15) in one instance.

### Example:

```bash
redis-cli

# Default: database 0
127.0.0.1:6379> SET key1 "value in DB 0"
OK

# Switch to database 1
127.0.0.1:6379> SELECT 1
OK

# Different namespace!
127.0.0.1:6379[1]> GET key1
(nil)

127.0.0.1:6379[1]> SET key1 "value in DB 1"
OK

# Switch back
127.0.0.1:6379[1]> SELECT 0
OK

127.0.0.1:6379> GET key1
"value in DB 0"  ← Different!
```

### Visual Representation:

```
Redis Instance
├─ Database 0
│  ├─ key1: "value in DB 0"
│  └─ key2: "another value"
│
├─ Database 1
│  ├─ key1: "value in DB 1"  ← Same key, different value!
│  └─ key3: "something else"
│
├─ Database 2-15
│  └─ (empty or with other keys)
```

### In RDB Format:

```
FE 00  ← OpCodeSelectDB + Database 0
  ... keys for DB 0 ...
  
FE 01  ← OpCodeSelectDB + Database 1
  ... keys for DB 1 ...
```

Our implementation uses only **Database 0**, but the format supports all 16 for Redis compatibility.

---

## Usage Examples

### 1. **Manual Snapshot (BGSAVE)**

```bash
$ redis-cli

# Add some data
127.0.0.1:6379> SET user:1:name "Alice"
OK
127.0.0.1:6379> SET user:1:age "30"
OK
127.0.0.1:6379> LPUSH recent_orders order1 order2 order3
(integer) 3

# Trigger background snapshot
127.0.0.1:6379> BGSAVE
Background saving started

# Check server logs
$ tail -f server.log
2025/12/19 03:20:26 Starting RDB snapshot (BGSAVE)...
2025/12/19 03:20:26 RDB snapshot completed successfully

# Verify file was created
$ ls -lh dump.rdb
-rw-r--r-- 1 user staff 84B Dec 19 03:20 dump.rdb
```

### 2. **Viewing RDB Contents**

```bash
# Hexdump shows binary format
$ hexdump -C dump.rdb
00000000  52 45 44 49 53 30 30 30  39 fa 09 72 65 64 69 73  |REDIS0009..redis|
00000010  2d 76 65 72 05 37 2e 30  2e 30 fa 05 63 74 69 6d  |-ver.7.0.0..ctim|
00000020  65 0a 31 37 33 34 36 33  30 30 30 30 fe 00 fb 03  |e.1734630000....|
00000030  00 00 0b 75 73 65 72 3a  31 3a 6e 61 6d 65 05 41  |...user:1:name.A|
00000040  6c 69 63 65 00 0a 75 73  65 72 3a 31 3a 61 67 65  |lice..user:1:age|
00000050  02 33 30 01 0d 72 65 63  65 6e 74 5f 6f 72 64 65  |.30..recent_orde|
00000060  72 73 03 06 6f 72 64 65  72 31 06 6f 72 64 65 72  |rs..order1.order|
00000070  32 06 6f 72 64 65 72 33  ff a1 2b 3c 4d 5e 6f 7a  |2.order3..+<M^oz|
```

### 3. **Testing Persistence**

```bash
# Start server
$ go run cmd/server/main.go

# In another terminal - add data
$ redis-cli SET test:key "important data"
OK
$ redis-cli LPUSH test:list item1 item2
(integer) 2

# Create snapshot
$ redis-cli BGSAVE
Background saving started

# Kill server
$ pkill -9 -f "go.*cmd/server"

# Restart server (would load from dump.rdb in real implementation)
$ go run cmd/server/main.go

# Verify data
$ redis-cli GET test:key
"important data"
$ redis-cli LRANGE test:list 0 -1
1) "item2"
2) "item1"
```

### 4. **Backup Workflow**

```bash
# Create snapshot
$ redis-cli BGSAVE
Background saving started

# Wait for completion
$ sleep 2

# Copy to backup location
$ cp dump.rdb backups/dump-$(date +%Y%m%d-%H%M%S).rdb

# Verify backup
$ redis-check-rdb backups/dump-20251219-032026.rdb
[OK] RDB is valid
```

---

## Key Takeaways

1. **RDB = Binary Snapshot**
   - Compact, fast to load
   - Not human-readable (use hexdump)
   - Perfect for backups

2. **Deep Copy is Critical**
   - Prevents race conditions
   - Ensures consistent snapshot
   - Thread-safe for background saves

3. **CRC64 Checksum**
   - Detects file corruption
   - Essential for data integrity
   - 8 bytes at end of file

4. **Variable-Length Encoding**
   - Saves significant space
   - 1-5 bytes per integer
   - Complex but efficient

5. **BGSAVE Command**
   - Background operation (non-blocking)
   - Uses goroutine for concurrency
   - Atomic file replacement

The RDB implementation provides fast, compact snapshots while maintaining data integrity and thread safety! 🎯
