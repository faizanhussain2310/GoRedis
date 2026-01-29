# Lua Scripting in Redis

## Table of Contents
1. [What is Lua Scripting?](#what-is-lua-scripting)
2. [Why Lua over Pipelining?](#why-lua-over-pipelining)
3. [Implementation Architecture](#implementation-architecture)
4. [Command Reference](#command-reference)
5. [Usage Examples](#usage-examples)
6. [Best Practices](#best-practices)
7. [Limitations and Considerations](#limitations-and-considerations)
8. [Frequently Asked Questions (FAQ)](#frequently-asked-questions-faq)

---

## What is Lua Scripting?

Lua scripting in Redis allows you to execute custom Lua scripts directly on the server. This feature provides a powerful way to perform complex operations atomically without multiple round-trips between the client and server.

### Key Characteristics:
- **Atomic Execution**: Scripts run as a single atomic operation
- **Server-Side Logic**: Complex logic executes on the Redis server
- **Reduced Network Overhead**: Multiple commands in one network request
- **Script Caching**: Scripts are cached by SHA1 hash for efficient reuse (unlimited cache size, memory-limited only)
- **Rich API**: Access to Redis commands via `redis.call()` and `redis.pcall()`
- **Deterministic**: No random or time functions to ensure replication consistency

### Lua Version
This implementation uses **Lua 5.1** via the [gopher-lua](https://github.com/yuin/gopher-lua) library, which is a pure Go implementation of Lua.

---

## Why Lua over Pipelining?

While both Lua scripting and pipelining can batch Redis operations, they serve different purposes and have distinct advantages.

### Pipelining

**What it does:**
- Batches multiple commands into a single network request
- Reduces round-trip latency
- Commands execute independently

**Limitations:**
- No conditional logic (if/else)
- Cannot use results from one command in another
- No atomicity guarantee across all commands
- Cannot loop or iterate dynamically

### Lua Scripting

**Advantages over Pipelining:**

1. **Atomicity**: The entire script executes as a single atomic operation
   ```lua
   -- This is atomic - no other commands can execute in between
   local current = redis.call('GET', KEYS[1])
   if tonumber(current) > 100 then
       redis.call('SET', KEYS[1], '0')
   end
   ```

2. **Conditional Logic**: Execute commands based on data
   ```lua
   if redis.call('EXISTS', KEYS[1]) == 1 then
       return redis.call('GET', KEYS[1])
   else
       return nil
   end
   ```

3. **Loops and Iterations**: Process multiple keys dynamically
   ```lua
   for i, key in ipairs(KEYS) do
       redis.call('INCR', key)
   end
   ```

4. **Result Chaining**: Use output from one command as input to another
   ```lua
   local value = redis.call('GET', KEYS[1])
   redis.call('SET', KEYS[2], value)
   ```

5. **Complex Calculations**: Perform server-side computations
   ```lua
   local sum = 0
   for i, key in ipairs(KEYS) do
       sum = sum + tonumber(redis.call('GET', key))
   end
   return sum
   ```

6. **Reduced Network Overhead**: Send script once, execute many times with different arguments

### When to Use What?

| Use Case | Pipelining | Lua Scripting |
|----------|-----------|---------------|
| Simple batch operations | ✅ | ❌ |
| Conditional logic needed | ❌ | ✅ |
| Atomicity required | ❌ | ✅ |
| Result chaining needed | ❌ | ✅ |
| Server-side computation | ❌ | ✅ |
| Maximum simplicity | ✅ | ❌ |

---

## Implementation Architecture

Our Lua scripting implementation consists of three main components:

### 1. Script Engine (`internal/lua/engine.go`)

The core Lua execution engine that manages script lifecycle and provides the Redis API.

**Key Components:**
```go
type ScriptEngine struct {
    scriptCache   map[string]string // SHA1 -> script source
    redisExecutor *RedisExecutor    // Bridge to Redis commands
}
```

**Responsibilities:**
- Lua VM lifecycle management
- Script caching with SHA1 hashing
- Redis API registration (`redis.call`, `redis.pcall`, etc.)
- Type conversion between Lua and Go
- Global variables setup (KEYS, ARGV)

**Main Methods:**
- `Eval(script, keys, args)` - Execute a Lua script
- `EvalSHA(sha1Hash, keys, args)` - Execute cached script by hash
- `LoadScript(script)` - Cache script and return SHA1
- `ScriptExists(sha1Hashes)` - Check if scripts are cached
- `ScriptFlush()` - Clear all cached scripts

### 2. Redis Executor (`internal/lua/redis_executor.go`)

Bridges Lua scripts to actual Redis commands executing on the store.

**Key Components:**
```go
type RedisExecutor struct {
    store *storage.Store
}
```

**Responsibilities:**
- Execute Redis commands from Lua scripts
- Convert Lua types to Go types
- Direct interaction with Redis data store
- Error handling and validation

**Supported Commands:**
- String operations: GET, SET, DEL, EXISTS, INCR, DECR, INCRBY, DECRBY
- List operations: LPUSH, RPUSH, LPOP, RPOP, LLEN, LRANGE
- Hash operations: HSET, HGET, HDEL, HGETALL
- Key expiration: EXPIRE, TTL

### 3. Command Handlers (`internal/handler/lua_commands.go`)

Handles EVAL, EVALSHA, and SCRIPT commands from clients.

**Key Handlers:**
- `handleEval()` - Parse and execute inline scripts
- `handleEvalSHA()` - Execute cached scripts by SHA1
- `handleScript()` - Dispatch SCRIPT subcommands
- `handleScriptLoad()` - Load and cache scripts
- `handleScriptExists()` - Check script cache
- `handleScriptFlush()` - Clear script cache

**Integration:**
The handlers are registered in the CommandHandler and use the ScriptEngine to execute Lua code:
```go
h.commands["EVAL"] = h.handleEval
h.commands["EVALSHA"] = h.handleEvalSHA
h.commands["SCRIPT"] = h.handleScript
```

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                         Client                              │
└────────────────────────────┬────────────────────────────────┘
                             │
                    EVAL / EVALSHA / SCRIPT
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│              Command Handler (lua_commands.go)              │
│  ┌────────────┬─────────────┬───────────────────────────┐  │
│  │ handleEval │ handleEvalSHA│  handleScript (LOAD/...)  │  │
│  └────────────┴─────────────┴───────────────────────────┘  │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│              Script Engine (engine.go)                      │
│  ┌───────────────────────────────────────────────────────┐ │
│  │  • Lua VM Management (gopher-lua)                     │ │
│  │  • Script Caching (SHA1 → source)                     │ │
│  │  • Redis API (redis.call, redis.pcall, redis.log)    │ │
│  │  • Globals Setup (KEYS[], ARGV[])                     │ │
│  │  • Type Conversion (Lua ↔ Go)                         │ │
│  └───────────────────────────────────────────────────────┘ │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│          Redis Executor (redis_executor.go)                 │
│  ┌───────────────────────────────────────────────────────┐ │
│  │  • Command Execution                                  │ │
│  │  • Type Conversion                                    │ │
│  │  • Error Handling                                     │ │
│  └───────────────────────────────────────────────────────┘ │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                   Storage Layer (store)                     │
│  ┌───────────────────────────────────────────────────────┐ │
│  │  • In-Memory Data Store                               │ │
│  │  • String/List/Hash Operations                        │ │
│  │  • Key Expiration                                     │ │
│  └───────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Client Request**: Client sends EVAL or EVALSHA command
2. **Command Parsing**: Handler parses command, extracts script/SHA, keys, and args
3. **Script Execution**: ScriptEngine creates Lua VM and executes script
4. **Redis Calls**: Script calls `redis.call()` or `redis.pcall()`
5. **Command Execution**: RedisExecutor executes commands on storage
6. **Type Conversion**: Results converted from Go to Lua types
7. **Return Value**: Final result converted to RESP format and returned to client

---

## Command Reference

### EVAL

Execute a Lua script server-side.

**Syntax:**
```
EVAL script numkeys key [key ...] arg [arg ...]
```

**Parameters:**
- `script` - The Lua script to execute
- `numkeys` - Number of keys that follow
- `key` - Redis keys (accessible as `KEYS[1]`, `KEYS[2]`, etc.)
- `arg` - Arguments (accessible as `ARGV[1]`, `ARGV[2]`, etc.)

**Return Value:**
The return value of the Lua script

**Example:**
```bash
EVAL "return redis.call('SET', KEYS[1], ARGV[1])" 1 mykey myvalue
```

### EVALSHA

Execute a cached Lua script by its SHA1 hash.

**Syntax:**
```
EVALSHA sha1 numkeys key [key ...] arg [arg ...]
```

**Parameters:**
- `sha1` - SHA1 hash of the script
- `numkeys` - Number of keys that follow
- `key` - Redis keys
- `arg` - Arguments

**Return Value:**
The return value of the cached script

**Error:**
Returns `NOSCRIPT` error if the script is not cached

**Example:**
```bash
EVALSHA "a42059b356c875f0717db19a51f6aaca9ae659ea" 1 mykey myvalue
```

### SCRIPT LOAD

Load a script into the cache and return its SHA1 hash.

**Syntax:**
```
SCRIPT LOAD script
```

**Parameters:**
- `script` - The Lua script to load

**Return Value:**
SHA1 hash of the script (40 character hex string)

**Example:**
```bash
SCRIPT LOAD "return redis.call('GET', KEYS[1])"
```

### SCRIPT EXISTS

Check if scripts exist in the cache.

**Syntax:**
```
SCRIPT EXISTS sha1 [sha1 ...]
```

**Parameters:**
- `sha1` - One or more SHA1 hashes to check

**Return Value:**
Array of integers (1 = exists, 0 = not exists)

**Example:**
```bash
SCRIPT EXISTS "a42059b356c875f0717db19a51f6aaca9ae659ea" "invalidsha1"
# Returns: [1, 0]
```

### SCRIPT FLUSH

Remove all scripts from the cache.

**Syntax:**
```
SCRIPT FLUSH
```

**Return Value:**
`OK`

**Example:**
```bash
SCRIPT FLUSH
```

---

## Usage Examples

### Example 1: Atomic Counter with Limit

Increment a counter only if it's below a limit.

```lua
-- Script
local count = tonumber(redis.call('GET', KEYS[1]) or 0)
local limit = tonumber(ARGV[1])

if count < limit then
    redis.call('INCR', KEYS[1])
    return count + 1
else
    return -1  -- Limit reached
end
```

**Usage:**
```bash
# Set initial value
SET counter 5

# Try to increment with limit of 10
EVAL "local count = tonumber(redis.call('GET', KEYS[1]) or 0); local limit = tonumber(ARGV[1]); if count < limit then redis.call('INCR', KEYS[1]); return count + 1 else return -1 end" 1 counter 10
# Returns: 6

# Try again when at limit
SET counter 10
EVAL "local count = tonumber(redis.call('GET', KEYS[1]) or 0); local limit = tonumber(ARGV[1]); if count < limit then redis.call('INCR', KEYS[1]); return count + 1 else return -1 end" 1 counter 10
# Returns: -1 (limit reached)
```

### Example 2: Atomic Transfer Between Keys

Transfer value from one key to another atomically.

```lua
-- Script
local value = redis.call('GET', KEYS[1])
if value then
    redis.call('SET', KEYS[2], value)
    redis.call('DEL', KEYS[1])
    return 1
else
    return 0
end
```

**Usage:**
```bash
# Setup
SET source "data to transfer"

# Execute transfer
EVAL "local value = redis.call('GET', KEYS[1]); if value then redis.call('SET', KEYS[2], value); redis.call('DEL', KEYS[1]); return 1 else return 0 end" 2 source destination
# Returns: 1

# Verify
GET source      # Returns: nil
GET destination # Returns: "data to transfer"
```

### Example 3: Batch Processing with Filtering

Process multiple keys and return only values above a threshold.

```lua
-- Script
local results = {}
local threshold = tonumber(ARGV[1])

for i, key in ipairs(KEYS) do
    local value = tonumber(redis.call('GET', key) or 0)
    if value > threshold then
        table.insert(results, key)
        table.insert(results, value)
    end
end

return results
```

**Usage:**
```bash
# Setup
SET key1 100
SET key2 50
SET key3 200
SET key4 25

# Get keys with values > 75
EVAL "local results = {}; local threshold = tonumber(ARGV[1]); for i, key in ipairs(KEYS) do local value = tonumber(redis.call('GET', key) or 0); if value > threshold then table.insert(results, key); table.insert(results, value) end end; return results" 4 key1 key2 key3 key4 75
# Returns: ["key1", "100", "key3", "200"]
```

### Example 4: Rate Limiting with Sliding Window

Implement rate limiting using a sliding time window.

```lua
-- Script
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local current_time = tonumber(ARGV[3])

-- Remove old entries
redis.call('ZREMRANGEBYSCORE', key, 0, current_time - window)

-- Count requests in window
local count = redis.call('ZCARD', key)

if count < limit then
    redis.call('ZADD', key, current_time, current_time)
    redis.call('EXPIRE', key, window)
    return 1
else
    return 0
end
```

**Note**: This example uses ZADD and ZREMRANGEBYSCORE which would need to be implemented in the RedisExecutor.

### Example 5: Using Script Caching

For frequently executed scripts, cache them using SCRIPT LOAD.

```bash
# Load the script
SCRIPT LOAD "return redis.call('INCR', KEYS[1])"
# Returns: "e0e1f9fabfc9d4800c877a703b823ac0578ff8db"

# Execute using SHA (more efficient)
EVALSHA "e0e1f9fabfc9d4800c877a703b823ac0578ff8db" 1 mycounter
# Returns: 1

EVALSHA "e0e1f9fabfc9d4800c877a703b823ac0578ff8db" 1 mycounter
# Returns: 2

# Check if script exists
SCRIPT EXISTS "e0e1f9fabfc9d4800c877a703b823ac0578ff8db"
# Returns: [1]
```

### Example 6: Error Handling with redis.pcall

Use `redis.pcall()` for graceful error handling.

```lua
-- Script with error handling
local result = redis.pcall('INCR', KEYS[1])

if type(result) == 'table' and result['err'] then
    -- Error occurred, return error message
    return redis.error_reply(result['err'])
else
    -- Success, return result
    return result
end
```

### Example 7: Creating Status Replies

Return status replies from Lua scripts.

```lua
-- Script
redis.call('SET', KEYS[1], ARGV[1])
return redis.status_reply('OK')
```

---

## Best Practices

### 1. Minimize Script Complexity
- Keep scripts focused on a single operation
- Break complex logic into smaller, reusable scripts
- Avoid deep nesting and complex control flow

### 2. Use Script Caching

**How SHA1 Caching Works:**
- SHA1 hash is computed from script content (40 hex characters)
- Same script always produces same hash (deterministic)
- Hash serves as key in cache map: `cache[hash] = script`
- No cache size limit (only RAM constrains how many scripts you can cache)

```bash
# Good: Load once, execute many times
SCRIPT LOAD "return redis.call('INCR', KEYS[1])"
# Returns SHA1: "e0e1f9fabfc9d4800c877a703b823ac0578ff8db"

# Execute with SHA (send 40 bytes instead of full script)
EVALSHA e0e1f9fabfc9d4800c877a703b823ac0578ff8db 1 counter

# Bad: Sending full script every time (wasteful)
EVAL "return redis.call('INCR', KEYS[1])" 1 counter
```

**Benefits of Caching:**
- Network savings: 40 characters vs potentially kilobytes
- No re-parsing: Script already compiled
- Fast lookup: O(1) hash map access

**Multiple Users, Same Script:**
If two users load the same script, both get the same SHA1 hash:
```bash
# User A loads script
SCRIPT LOAD "return redis.call('GET', KEYS[1])"
# Returns: "abc123..."

# User B loads identical script
SCRIPT LOAD "return redis.call('GET', KEYS[1])"
# Returns: "abc123..."  (same hash!)
# Second write overwrites with identical content (no-op)
# No data loss - caching is idempotent
```


**Understanding redis.call() vs redis.pcall():**

| Feature | redis.call() | redis.pcall() |
|---------|-------------|---------------|
| On Error | Script terminates immediately | Returns error table: `{err = "message"}` |
| Error Handling | Cannot catch errors | Can catch and handle errors |
| Use When | Failure is fatal | Want graceful degradation |

```lua
-- redis.call() - Stops script on error
local value = redis.call('INCR', 'stringkey')  -- If error, script STOPS here
redis.call('SET', 'other', 'value')            -- Never reached if error

-- redis.pcall() - Returns error table, script continues
local result = redis.pcall('INCR', 'stringkey')
if type(result) == 'table' and result['err'] then
    -- Error path: Log and return 0
    redis.call('SET', 'error_log', result['err'])
    return 0  -- Script EXITS here (function returns)
end
-- Success path: Only reached if NO error above
redis.call('SET', 'other', 'value')  -- Only executes if no error
return result  -- Returns the actual INCR result
```

**Important Control Flow:**
- `return` **exits the function immediately**
- Code after `return` in the same block never executes
- The `if` block and code after it are mutually exclusive paths

**Note:** In Lua, `table` is the universal data structure used for arrays, dictionaries, and objects. `redis.pcall()` instead of `redis.call()` when you want to handle errors:

⚠️ **CRITICAL: Scripts block ALL operations until completion!**

- Scripts are atomic - **no other commands can execute** during script execution
- Keep scripts short to avoid blocking other operations
- **Avoid long loops** - they will freeze your entire Redis server

```lua
-- DANGEROUS: This blocks Redis for minutes!
for i = 1, 1000000000 do
    count = count + 1
end

-- SAFE: Short, bounded operations
for i = 1, 100 do
    redis.call('INCR', KEYS[i])
end
```

**What happens with long-running scripts:**
1. Script starts executing
2. ALL client operations block (GET, SET, everything waits)
3. Script continues until completion (no automatic timeout)
4. Server effectively frozen during execution
5. Other commands resume only after script finishes

**Best Practice:** If processing large datasets, split into smaller batches and call script multiple times.
local value = redis.call('GET', KEYS[1])

-- redis.pcall() - Returns error table
local result = redis.pcall('GET', KEYS[1])
if type(result) == 'table' and result['err'] then
    -- Handle error
    return redis.error_reply(result['err'])
end
```

### 5. Type Conversion
Always convert values when doing arithmetic:

```lua

**Why:** Scripts must be **deterministic** for master-slave replication and AOF replay.

```lua
-- If math.random() were allowed (it's NOT):
local random_value = math.random(1, 100)
redis.call('SET', 'mykey', random_value)

-- Problem in replication:
-- Master executes: SET mykey 42
-- Slave executes:  SET mykey 87  <- Different value!
-- Result: Data inconsistency across cluster
```

**Determinism ensures:**
- Master and slaves have identical data
- AOF replay produces same state
- Cluster consistency
- Predictable behavior

### 2. No Time Functions

**Why:** Same determinism requirement - time changes between executions.

```lua
-- If os.time() were allowed (it's NOT):
local now = os.time()
redis.call('SET', 'timestamp', now)

-- Problem:
-- Master executes at 10:00:00 → SET timestamp 1642248000
-- Slave executes at 10:00:01 → SET timestamp 1642248001
-- Result: Different values!

-- SOLUTION: Pass time as argument
local current_time = tonumber(ARGV[1])  -- Good! Deterministic
redis.call('SET', 'timestamp', current_time)

-- Usage:
EVAL "..." 1 mykey 1642248000  -- Same timestamp everywheremputations

### 7. Return Values
Return appropriate types and Cache Management

**Understanding SHA1 and Script Identity:**

SHA1 hash is computed from script content - **changing even one character produces a different hash**.

```bash
# Version 1
SCRIPT LOAD "return redis.call('GET', KEYS[1])"
# Returns: "abc123..."

# Version 2 (added one space)
SCRIPT LOAD "return redis.call('GET',  KEYS[1])"
# Returns: "def456..."  <- Completely different hash!
```

**You cannot "overwrite" a script:**
- Hash is derived from content (cryptographic function)
- Different content = different hash
- Both versions coexist in cache

**"Updating" a script:**
```bash
# Option 1: Clear and reload
SCRIPT FLUSH                    # Clear all scripts
SCRIPT LOAD "new version"       # Load new version

# Option 2: Load new version alongside old
SCRIPT LOAD "new version"       # Returns new hash
# Both old and new versions now cached
# Gradually migrate clients to new hash
```

**Versioning Strategy:**
```go
// In your application code
const (
    GetUserScriptV1 = "abc123..."  // Old version
    GetUserScriptV2 = "def456..."  // New version
)

// Gradual migration:
// 1. Deploy code with both hashes
// 2. Update to use V2
// 3. After all clients migrated, optionally SCRIPT FLUSH old version
```

**Cache Size:**
- No limit on number of scripts (only RAM)
- Average script: ~500-1000 bytes
- 1,000 scripts ≈ 640 KB, 10,000 scripts ≈ 6.4 MB
- Use SCRIPT FLUSH if memory becomes concer
return 42

-- Strings
return "result"

-- Arrays
return {1, 2, 3}

-- Status replies
return redis.status_reply('OK')

-- Error replies
return redis.error_reply('Something went wrong')
```

---

## Limitations and Considerations

### 1. No Random Functions
Lua's random functions are not available to ensure script determinism.

### 2. No Time Functions
Time-based functions are restricted. Use passed timestamps as ARGV:

```lua
-- Good
local current_time = tonumber(ARGV[1])

-- Bad (not available)
local current_time = os.time()
```

### 3. Single-Threaded Execution
Scripts block other operations. Keep execution time minimal.

### 4. Memory Usage
- Scripts are cached in memory
- Use SCRIPT FLUSH to clear cache if needed
- Monitor memory usage with many cached scripts

### 5. Debugging
Lua scripts can be challenging to debug. Strategies:
- Use `redis.log()` for server-side logging
- Test scripts incrementally
- Return intermediate values for debugging
- Use `redis.pcall()` to catch errors

### 6. Supported Commands
Currently supported commands in `redis.call()` and `redis.pcall()`:
- **String**: GET, SET, DEL, EXISTS, INCR, DECR, INCRBY, DECRBY
- **List**: LPUSH, RPUSH, LPOP, RPOP, LLEN, LRANGE
- **Hash**: HSET, HGET, HDEL, HGETALL
- **Key**: EXPIRE, TTL

Additional commands can be added to `redis_executor.go`.

### 7. Global Variables
Only `KEYS` and `ARGV` are available as globals:
- `KEYS[1]`, `KEYS[2]`, ... - Redis keys (1-indexed)
- `ARGV[1]`, `ARGV[2]`, ... - Script arguments (1-indexed)

### 8. Script Versioning
- SHA1 changes if script changes by even one character
- Consider versioning strategy for production scripts
- Document script versions in your application

### 9. Performance Tips
- Pre-load frequently used scripts with SCRIPT LOAD
- Use EVALSHA instead of EVAL when possible
- Minimize the number of Redis calls in scripts
- Batch operations when possible

---

## API Reference

### Redis Functions Available in Lua

#### redis.call(command, ...)
Executes a Redis command. Raises an error if the command fails.

```lua
local value = redis.call('GET', KEYS[1])
redis.call('SET', KEYS[1], ARGV[1])
```

#### redis.pcall(command, ...)
Executes a Redis command. Returns an error table on failure instead of raising an error.

```lua
local result = redis.pcall('INCR', KEYS[1])
if type(result) == 'table' and result['err'] then
    -- Handle error
end
```

#### redis.log(loglevel, message)
Logs a message (currently no-op in this implementation).

```lua
redis.log(redis.LOG_WARNING, 'Something happened')
```

#### redis.status_reply(status)
Returns a status reply.

```lua
return redis.status_reply('OK')
-- Returns: {ok = "OK"}
```

#### redis.error_reply(error)
Returns an error reply.

```lua
return redis.error_reply('Something went wrong')
-- Returns: {err = "Something went wrong"}
```

### Type Conversions

#### Lua → Redis (Return Values)
- `nil` → Null Bulk String
- `boolean` → Integer (1 or 0)
- `number` → Integer
- `string` → Bulk String
- `table` (array) → Multi Bulk Reply
- `table` (with `ok` field) → Status Reply
- `table` (with `err` field) → Error Reply

#### Redis → Lua (Command Results)
- Status Reply → `{ok = "status"}`
- Error Reply → `{err = "error message"}`
- Integer Reply → number
- Bulk String → string
- Multi Bulk Reply → table (array)
- Null Bulk String → `false`

---

## Frequently Asked Questions (FAQ)

### Q1: What is gopher-lua and why use it?

**gopher-lua** is a pure Go implementation of the Lua 5.1 virtual machine.

**Key Benefits:**
- **No C Dependencies**: Unlike standard Lua (written in C), gopher-lua is 100% Go
  - No CGO required
  - Simpler build process
  - Better cross-compilation
- **Go Integration**: Natural integration with Go types and error handling
- **Cross-Platform**: Works on any platform Go supports (Windows, Linux, macOS, ARM, etc.)
- **Thread-Safe**: When used correctly (each goroutine has own Lua state)
- **Active**: Well-maintained with strong community support

**Trade-off:**
- Slower than C-based Lua (~10-20x) but fast enough for scripting workloads

**GitHub**: https://github.com/yuin/gopher-lua

### Q2: How does the KEYS array work in EVAL?

The `numkeys` parameter defines how many arguments are keys vs regular arguments.

```bash
EVAL "script" <numkeys> <key1> <key2> ... <arg1> <arg2> ...
              └────────┘ └───────────────┘ └─────────────────┘
              Count      These are KEYS    These are ARGV
```

**Example:**
```bash
EVAL "return {KEYS[1], KEYS[2], ARGV[1]}" 2 source destination myvalue
#                                         │  └─────────────┘  └──────┘
#                                    numkeys=2   KEYS[1-2]      ARGV[1]
```

**Inside script:**
```lua
KEYS[1] = "source"       -- First key
KEYS[2] = "destination"  -- Second key  
ARGV[1] = "myvalue"      -- First argument (not a key)
```

**Why separate KEYS from ARGV?**
- Cluster mode needs to know which keys are accessed
- Hash slots computed from keys only
- Better clarity: keys vs parameters

### Q3: What is a Lua 'table' and why check for it?

In Lua, **table is the only data structure** - used for everything:

```lua
-- Array (numeric indices)
local arr = {1, 2, 3}
arr[1] -- Returns: 1

-- Dictionary (string keys) 
local dict = {name = "Alice", age = 30}
dict["name"] -- Returns: "Alice"

-- Error from redis.pcall
local result = redis.pcall('INCR', 'stringvalue')
-- result = {err = "ERR value is not an integer"}

-- Check if it's an error table
if type(result) == 'table' and result['err'] then
    -- It's an error!
    print(result['err'])
end
```

**Type checking pattern:**
```lua
-- Check if result is an error
if type(result) == 'table' and result['err'] then
    -- Error occurred
elseif type(result) == 'table' and result['ok'] then
    -- Status reply
elseif type(result) == 'table' then
    -- Array or map
else
    -- Number, string, boolean, or nil
end
```

### Q4: How many scripts can be cached total?

**Answer: Unlimited (only constrained by available RAM)**

```go
// No size limit in code
scriptCache map[string]string  // Can grow indefinitely
```

**Memory calculation:**
- SHA1 hash: ~40 bytes
- Average script: ~500 bytes  
- Map overhead: ~100 bytes
- **Per script: ~640 bytes**

**Examples:**
- 100 scripts ≈ 64 KB
- 1,000 scripts ≈ 640 KB
- 10,000 scripts ≈ 6.4 MB
- 100,000 scripts ≈ 64 MB

**In practice:** Most applications use < 100 unique scripts. Use `SCRIPT FLUSH` if memory becomes an issue.

### Q5: What happens if my script has a long loop?

⚠️ **Redis will keep executing and BLOCK all other operations!**

```lua
-- DANGEROUS: Blocks Redis for minutes!
for i = 1, 1000000000 do
    count = count + 1
end
```

**What happens:**
1. Script starts running
2. **ALL client commands block** (GET, SET, everything waits in queue)
3. Loop continues until completion (no automatic timeout in our implementation)
4. Redis is effectively frozen
5. Other commands can execute only after script finishes

**Solution: Process in small batches**
```lua
-- Instead of processing 1 million keys in one script:
-- Good: Process 1000 keys per script call
for i = 1, 1000 do
    redis.call('INCR', KEYS[i])
end
-- Call script 1000 times from client

-- Better: Use pipeline for simple operations like this
```

**Real Redis:** Has `lua-time-limit` config (default 5 seconds) after which you can kill script. Our implementation doesn't have this yet.

### Q6: Can I change/update a cached script?

**No direct update, but you can flush and reload:**

SHA1 is computed from script content - changing the script produces a **different hash**.

```bash
# Original script
SCRIPT LOAD "return 1"
# Returns: "hash_abc123"

# Modified script (changed "1" to "2")
SCRIPT LOAD "return 2"  
# Returns: "hash_def456"  <- DIFFERENT hash!

# Both are now cached:
# hash_abc123 → "return 1"
# hash_def456 → "return 2"
```

**To "update":**

**Option 1: Flush all and reload**
```bash
SCRIPT FLUSH
SCRIPT LOAD "return 2"  # New version
```

**Option 2: Gradual migration**
```bash
# Load new version
SCRIPT LOAD "return 2"  # Returns hash_v2

# Update application to use hash_v2
# Both versions coexist during transition
# After migration complete, optionally SCRIPT FLUSH
```

**Why no direct update?**
- Hash is cryptographic - can't reverse engineer original
- Different content = different hash (by design)
- Ensures integrity - hash guarantees you get exact script you expect

### Q7: Why are random and time functions disabled?

**For determinism in replication and recovery:**

**Problem with random:**
```lua
-- If this were allowed:
local rand = math.random(1, 100)
redis.call('SET', 'key', rand)

-- In master-slave setup:
-- Master executes → SET key 42
-- Slave executes  → SET key 87  (different!)
-- Result: Data inconsistency!
```

**Problem with time:**
```lua
-- If this were allowed:
local now = os.time()
redis.call('SET', 'timestamp', now)

-- AOF replay (restore from disk):
-- Original execution: 2024-01-15 10:00:00
-- Replay execution:   2024-01-16 09:30:00  (different!)
-- Result: Wrong data!
```

**Solution: Pass values as arguments**
```lua
-- Deterministic approach
local random_value = tonumber(ARGV[1])  -- Client generates
local timestamp = tonumber(ARGV[2])     -- Client provides

redis.call('SET', 'key', random_value)
redis.call('SET', 'timestamp', timestamp)

-- Client call:
EVAL "..." 0 42 1642248000  -- Same values everywhere
```

### Q8: What if multiple users cache the same script?

**Answer: Both get the same SHA1 hash - second operation is effectively a no-op.**

Since SHA1 is computed from script content, identical scripts produce identical hashes:

```bash
# User A caches script
SCRIPT LOAD "return 1"
# Cache: {"hash_abc": "return 1"}

# User B caches same script
SCRIPT LOAD "return 1"
# Cache: {"hash_abc": "return 1"}  (overwrites with same content)
```

**Key points:**
- Same content = Same hash (deterministic SHA1)
- Second write overwrites with identical data (no-op)
- No data loss or conflicts
- Caching is idempotent by design
- Different scripts = Different hashes (coexist in cache)

### Q9: Does Redis verify Lua syntax?

**Yes! gopher-lua verifies syntax when executing the script.**

**Syntax errors are caught and returned:**
```bash
# Missing 'end' keyword
EVAL "if true then return 1" 0
# Error: ERR Error running script: 'end' expected

# Invalid syntax
EVAL "return )" 0  
# Error: ERR Error running script: unexpected symbol near ')'

# Valid script
EVAL "return 1 + 1" 0
# Returns: 2
```

**Two error types:**
1. **Syntax errors** (parse-time): Missing `end`, invalid operators, etc.
2. **Runtime errors** (execution-time): Undefined variables, type errors, redis.call() failures

Both are caught and returned to the client as error messages.

### Q10: Is SET command supported in redis.call()?

**Yes! SET is fully supported.** 

See [internal/lua/redis_executor.go](internal/lua/redis_executor.go):

```go
case "SET":
    if len(stringArgs) < 2 {
        return nil, fmt.Errorf("ERR wrong number of arguments for 'set' command")
    }
    r.store.Set(stringArgs[0], stringArgs[1], nil)
    return "OK", nil
```

**Supported String commands:**
- GET ✅
- SET ✅  
- DEL ✅
- EXISTS ✅
- INCR ✅
- DECR ✅
- INCRBY ✅
- DECRBY ✅

**Usage:**
```lua
redis.call('SET', KEYS[1], ARGV[1])
redis.call('SET', 'mykey', 'myvalue')
```

### Q11: Is Lua a separate programming language? What's the syntax?

**Yes! Lua is a complete, independent programming language.**

**About Lua:**
- Created in 1993 at PUC-Rio, Brazil
- Name means "moon" in Portuguese  
- Lightweight scripting language designed for embedding
- Used in: Redis, World of Warcraft, Roblox, Nginx, game engines
- Current version: 5.4 (Redis uses 5.1 for compatibility)

**Basic Syntax:**

```lua
-- Comments start with --
--[[ Multi-line comments ]]

-- Variables (dynamically typed)
local x = 10
local name = "Alice"
local flag = true

-- Tables (the ONLY data structure)
local array = {1, 2, 3}          -- Arrays are 1-indexed!
local dict = {name = "Bob", age = 30}
local mixed = {10, key = "value"}

-- Conditionals
if x > 5 then
    print("large")
elseif x > 0 then
    print("positive")
else
    print("zero or negative")
end

-- Loops
for i = 1, 10 do              -- 1 to 10 inclusive
    print(i)
end

for i, val in ipairs(array) do  -- Iterate array
    print(i, val)
end

-- Functions
local function add(a, b)
    return a + b
end

-- String operations
local str = "hello"
local upper = string.upper(str)   -- "HELLO"
local concat = str .. " world"    -- "hello world" (.. is concat)

-- Type checking
if type(result) == 'table' then
    -- It's a table
end

-- Nil coalescing
local value = redis.call('GET', key) or "default"
```

**Key Differences from Other Languages:**

| Feature | Lua | JavaScript | Python |
|---------|-----|------------|--------|
| Arrays | 1-indexed | 0-indexed | 0-indexed |
| Null value | `nil` | `null` | `None` |
| String concat | `..` | `+` | `+` |
| Not equal | `~=` | `!=` | `!=` |
| Comments | `--` | `//` | `#` |
| Data structure | Tables only | Objects, Arrays | Dicts, Lists |

**Redis-Specific Lua:**

```lua
-- Globals provided by Redis
KEYS[1], KEYS[2]   -- Keys from EVAL command (1-indexed!)
ARGV[1], ARGV[2]   -- Arguments from EVAL command (1-indexed!)

-- Redis API functions
redis.call('SET', KEYS[1], ARGV[1])
redis.pcall('GET', KEYS[1])
redis.log(redis.LOG_WARNING, "message")
redis.status_reply('OK')
redis.error_reply('Error')

-- Common patterns
local count = tonumber(redis.call('GET', 'counter') or 0)
local value = redis.call('GET', KEYS[1]) or "default"

-- Important: redis.call() returns false (not nil) for null
local result = redis.call('GET', 'nonexistent')
-- result is boolean false, not nil!
```

**Learning Resources:**
- Official Lua docs: https://www.lua.org/manual/5.1/
- Learn Lua in 15 minutes: https://learnxinyminutes.com/docs/lua/
- Redis Lua scripting: https://redis.io/docs/manual/programmability/eval-intro/

---

## Conclusion

Lua scripting in Redis provides a powerful way to execute complex, atomic operations on the server. While pipelining is great for simple batching, Lua scripts excel when you need:

- Conditional logic
- Atomicity across multiple operations
- Server-side computations
- Result chaining
- Complex data transformations

This implementation provides a solid foundation for Lua scripting with support for common Redis commands and proper error handling. As your needs grow, additional commands can be easily added to the RedisExecutor.

For production use, remember to:
- Cache scripts using SCRIPT LOAD
- Keep scripts short and focused
- Handle errors appropriately
- Test scripts thoroughly before deployment
- Monitor performance and memory usage

---

**Implementation Details:**
- **Lua Engine**: gopher-lua (Pure Go Lua 5.1 implementation)
- **Script Caching**: SHA1-based in-memory cache
- **Atomicity**: Full atomic execution guarantee
- **Type Safety**: Comprehensive type conversion between Lua and Go
- **Error Handling**: Support for both `redis.call()` and `redis.pcall()`
