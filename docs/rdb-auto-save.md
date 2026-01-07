# RDB Auto-Save Implementation

This document explains the automatic RDB snapshotting feature, inspired by Redis's save points.

## Overview

The server automatically creates RDB snapshots (database backups) when specific conditions are met, ensuring data durability without manual intervention.

## Configuration

### Save Point Pattern (Redis-Style)

```go
type RDBSavePoint struct {
    Seconds int // Time interval in seconds
    Changes int // Minimum number of key changes
}
```

**Default Configuration:**
```go
RDBSavePoint: RDBSavePoint{
    Seconds: 60,    // 60 seconds
    Changes: 1000,  // 1000 key changes
}
```

**Meaning:** Save the database if **both** conditions are met:
1. At least 60 seconds have passed since the last save
2. At least 1000 keys have been modified

This is equivalent to Redis's `save 60 1000` configuration directive.

## How It Works

### 1. Change Tracking

Every write operation increments a counter:

```go
// In CommandHandler
func (h *CommandHandler) LogToAOF(command string, args []string) {
    if aof.IsWriteCommand(command) {
        if h.onChange != nil {
            h.onChange()  // ← Increments changesSinceLastSave
        }
    }
}
```

**Tracked operations:**
- SET, DEL, EXPIRE
- LPUSH, RPUSH, LPOP, RPOP
- HSET, HDEL
- SADD, SREM
- All other write commands

### 2. Background Checker

A background goroutine runs every N seconds (where N = save point interval):

```go
func (s *Server) startBackgroundRDBSave() {
    checkInterval := time.Duration(s.config.RDBSavePoint.Seconds) * time.Second
    s.rdbTicker = time.NewTicker(checkInterval)
    
    go func() {
        for {
            select {
            case <-s.rdbTicker.C:
                changes := s.changesSinceLastSave.Load()
                elapsed := time.Since(s.lastSaveTime)
                
                // Check if BOTH conditions are met
                if changes >= int64(s.config.RDBSavePoint.Changes) &&
                   elapsed >= time.Duration(s.config.RDBSavePoint.Seconds)*time.Second {
                    
                    // Trigger BGSAVE
                    s.performBackgroundSave()
                    
                    // Reset counters
                    s.changesSinceLastSave.Store(0)
                    s.lastSaveTime = time.Now()
                }
            }
        }
    }()
}
```

### 3. Trigger BGSAVE

When conditions are met, the background checker triggers a BGSAVE:

```go
func (s *Server) performBackgroundSave() error {
    cmd := &protocol.Command{Args: []string{"BGSAVE"}}
    response := s.handler.ExecuteCommand(cmd)
    return nil
}
```

This uses the existing BGSAVE implementation:
- ✅ **Non-blocking**: Runs in background goroutine
- ✅ **Copy-on-Write**: Uses shallow copy + COW for efficiency
- ✅ **Atomic file swap**: Uses temp file + rename for crash safety

### 4. Reset Counters

After a successful save:
```go
s.changesSinceLastSave.Store(0)  // Reset to 0
s.lastSaveTime = time.Now()      // Update timestamp
```

## Persistence Loading

The server follows this priority when loading data at startup:

### Loading Order

```
1. Try AOF first (more up-to-date)
   ↓ (if AOF missing or disabled)
2. Try RDB as fallback
   ↓ (if RDB also missing)
3. Start with empty database
```

### Implementation

```go
// Load persistence files (AOF takes priority, fallback to RDB)
if cfg.AOF.Enabled {
    if err := s.loadAOF(); err != nil {
        log.Printf("Warning: Failed to load AOF: %v", err)
        // Try RDB as fallback
        if err := s.loadRDB(); err != nil {
            log.Printf("Warning: Failed to load RDB: %v", err)
            log.Printf("Starting with empty database")
        } else {
            log.Printf("Loaded data from RDB file")
        }
    }
} else {
    // AOF disabled, try loading from RDB
    if err := s.loadRDB(); err != nil {
        log.Printf("Warning: Failed to load RDB: %v", err)
        log.Printf("Starting with empty database")
    }
}
```

**Why AOF has priority?**
- AOF is more up-to-date (logs every write)
- RDB is a point-in-time snapshot (may be 60 seconds old)
- If both exist, AOF is always more recent

## RDB Reader Implementation

The RDB reader parses the binary RDB file and restores data:

### File Format

```
┌──────────────────────────────────┐
│ Magic: "REDIS" (5 bytes)         │
├──────────────────────────────────┤
│ Version: "0009" (4 bytes)        │
├──────────────────────────────────┤
│ Key-Value Pairs:                 │
│   [Optional] Expiration          │
│   Type Byte                      │
│   Key (length-prefixed string)   │
│   Value (type-specific encoding) │
├──────────────────────────────────┤
│ EOF Marker (0xFF)                │
├──────────────────────────────────┤
│ CRC64 Checksum (8 bytes)         │
└──────────────────────────────────┘
```

### Data Restoration

The reader converts RDB data back to commands:

```go
// String: SET key value [PXAT timestamp]
SET mykey "hello" PXAT 1735000000000

// List: RPUSH key elem1 elem2 ... [+ PEXPIREAT]
RPUSH mylist "a" "b" "c"
PEXPIREAT mylist 1735000000000

// Hash: HSET key field1 val1 field2 val2 ... [+ PEXPIREAT]
HSET myhash "field1" "val1" "field2" "val2"
PEXPIREAT myhash 1735000000000

// Set: SADD key member1 member2 ... [+ PEXPIREAT]
SADD myset "a" "b" "c"
PEXPIREAT myset 1735000000000
```

These commands are executed through the normal command handler to restore data.

## Performance Characteristics

### Memory Usage

**Change Counter:**
```go
changesSinceLastSave atomic.Int64  // 8 bytes
lastSaveTime         time.Time     // 24 bytes
```

Total overhead: **32 bytes** (negligible)

### CPU Overhead

**Per write operation:**
```go
if h.onChange != nil {
    h.onChange()  // ← atomic.Add (single instruction, ~1ns)
}
```

**Background checker:**
- Runs every 60 seconds
- Does 2 atomic reads + 1 time comparison
- Total cost: ~100ns every 60 seconds (negligible)

### I/O Overhead

**When save is triggered:**
- BGSAVE runs in background (non-blocking)
- Uses COW for minimal memory overhead
- Atomic file swap for crash safety

## Example Usage

### Default Configuration (60s / 1000 changes)

```
Time    Changes    Action
---------------------------------------------
0s      0          Server starts
10s     500        (wait... not enough changes)
30s     800        (wait... not enough changes)
50s     1200       (wait... not enough time)
60s     1200       ✅ TRIGGER BGSAVE
                   (both conditions met!)
60s     0          Reset counters
120s    50         (wait... not enough changes)
```

### Custom Configuration

You can adjust the save point in `config.go`:

```go
// More aggressive (save more often)
RDBSavePoint: RDBSavePoint{
    Seconds: 30,   // Every 30 seconds
    Changes: 100,  // If 100 keys changed
}

// Less aggressive (save less often)
RDBSavePoint: RDBSavePoint{
    Seconds: 300,  // Every 5 minutes
    Changes: 10000, // If 10,000 keys changed
}

// Disabled
RDBSavePoint: RDBSavePoint{
    Seconds: 0,    // Disabled when Seconds or Changes = 0
    Changes: 0,
}
```

### Multiple Save Points (Future Enhancement)

Redis supports multiple save points:
```
save 900 1      # Save after 15 minutes if 1 key changed
save 300 10     # Save after 5 minutes if 10 keys changed
save 60 10000   # Save after 1 minute if 10000 keys changed
```

Currently, we support **one** save point. To add multiple:
```go
type Config struct {
    RDBSavePoints []RDBSavePoint  // Array of save points
}
```

## Shutdown Behavior

When the server shuts down gracefully:

```go
func (s *Server) Shutdown() {
    // 1. Stop RDB ticker
    if s.rdbTicker != nil {
        s.rdbTicker.Stop()
        close(s.rdbStopChan)
    }
    
    // 2. Close AOF writer (flushes remaining data)
    if s.aofWriter != nil {
        s.aofWriter.Close()
    }
    
    // Note: We don't trigger BGSAVE on shutdown
    // (AOF contains all data, and BGSAVE may be running already)
}
```

**Why not trigger BGSAVE on shutdown?**
- If AOF is enabled, it already has all data
- BGSAVE may already be running in background
- Synchronous save would delay shutdown

If you want to force a save on shutdown, you can manually call `SAVE` (blocking) before stopping the server.

## Comparison with Redis

| Feature | Redis | Our Implementation |
|---------|-------|-------------------|
| Save points | Multiple | Single (easy to extend) |
| Default config | `save 900 1`, `save 300 10`, `save 60 10000` | `save 60 1000` |
| Background save | ✅ BGSAVE | ✅ BGSAVE |
| Copy-on-Write | ✅ fork() | ✅ Shallow copy + COW |
| Atomic swap | ✅ rename() | ✅ rename() |
| Change tracking | ✅ dirty counter | ✅ atomic counter |
| Loading priority | AOF > RDB | ✅ Same |
| AOF rewrite | ✅ Hybrid buffer | ✅ Same |

## Logs Example

```
2025-12-23 10:00:00 Server listening on 0.0.0.0:6379
2025-12-23 10:00:00 AOF enabled: appendonly.aof (sync: everysec)
2025-12-23 10:00:00 RDB auto-save enabled: save after 60 seconds if 1000 keys changed
2025-12-23 10:00:00 No AOF file found, starting with empty database
2025-12-23 10:00:00 No RDB file found

... (client writes 1500 keys over 65 seconds) ...

2025-12-23 10:01:05 RDB auto-save triggered: 1500 changes in 1m5s
2025-12-23 10:01:05 Background save started
2025-12-23 10:01:06 Background save completed: 1500 keys saved to dump.rdb
```

## Summary

✅ **Automatic snapshots** - No manual BGSAVE needed  
✅ **Redis-compatible** - Same save point pattern  
✅ **Efficient tracking** - Atomic counter (1ns overhead)  
✅ **Non-blocking** - Uses existing BGSAVE  
✅ **Smart loading** - AOF priority, RDB fallback  
✅ **Graceful shutdown** - Stops ticker, flushes AOF  

The implementation ensures data durability with minimal overhead and maximum compatibility with Redis behavior! 🚀
