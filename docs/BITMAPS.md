# Bitmaps in Redis

## Table of Contents
- [Overview](#overview)
- [What are Bitmaps?](#what-are-bitmaps)
- [How Bitmaps Work](#how-bitmaps-work)
- [Implementation Details](#implementation-details)
- [Supported Commands](#supported-commands)
- [Use Cases](#use-cases)
- [Performance Characteristics](#performance-characteristics)
- [Common Patterns](#common-patterns)
- [Comparison with Other Data Structures](#comparison-with-other-data-structures)
- [Best Practices](#best-practices)
- [Examples](#examples)

---

## Overview

**Bitmaps** are not actually a separate data structure in Redis—they're **strings treated as bit arrays**. This allows you to perform bit-level operations on binary data with extremely high efficiency and minimal memory usage.

**Key Benefits**:
- ⚡ **Memory Efficient**: 1 bit per flag (vs 1 byte minimum for other structures)
- 🚀 **Fast**: Bitwise operations are CPU-level instructions
- 📊 **Analytics Friendly**: Perfect for tracking binary states (online/offline, active/inactive)
- 🎯 **Scalable**: Handle millions of IDs with minimal memory

---

## What are Bitmaps?

A bitmap is essentially a string where each bit can be individually accessed and modified. Think of it as an array of 0s and 1s:

```
Position:  0  1  2  3  4  5  6  7  8  9  10 11 12 13 14 15
Bit:       1  0  1  1  0  0  1  0  1  0  1  0  0  1  1  0
           │                       │
           └─ User 0 logged in     └─ User 8 logged in
```

### Real-World Analogy

Imagine a giant attendance sheet where each person has a checkbox (bit):
- ✅ Checked (1) = Present/Active/Yes
- ☐ Unchecked (0) = Absent/Inactive/No

Unlike traditional arrays, bitmaps use **just 1 bit per entry**, making them incredibly space-efficient.

---

## How Bitmaps Work

### Bit Storage

Redis stores bitmaps as **strings** where each byte contains 8 bits:

```
Byte 0:  [b7 b6 b5 b4 b3 b2 b1 b0]  ← Bits 0-7
Byte 1:  [b7 b6 b5 b4 b3 b2 b1 b0]  ← Bits 8-15
Byte 2:  [b7 b6 b5 b4 b3 b2 b1 b0]  ← Bits 16-23
...
```

### Bit Numbering

Bits are numbered **left to right** within each byte:

```
Byte:     0x2D (decimal 45)
Binary:   0  0  1  0  1  1  0  1
Bit pos:  7  6  5  4  3  2  1  0  (within byte)
```

To access bit N:
- **Byte index** = N / 8
- **Bit offset** = 7 - (N % 8)  (left to right)

### Example: Setting Bit 10

```
Bit 10:
  Byte index = 10 / 8 = 1  (second byte)
  Bit offset = 7 - (10 % 8) = 7 - 2 = 5  (6th bit from right)
  
Before:  Byte 1 = 00000000
After:   Byte 1 = 00100000  (bit 5 set to 1)
```

---

## Implementation Details

### Internal Representation

```go
// Bitmaps are stored as regular strings
s.data[key] = &Value{
    Data: string([]byte{0x2D, 0x1A, 0xFF, ...}),  // Bit array as string
    Type: StringType,                              // Still a string type!
}
```

### Key Operations

#### 1. **SETBIT** - Set or Clear a Bit

```go
func (s *Store) SetBit(key string, offset int64, value int) (int, error) {
    byteIndex := offset / 8
    bitOffset := uint(7 - (offset % 8))
    
    // Expand string if needed
    if len(str) < byteIndex+1 {
        str = str + string(make([]byte, byteIndex+1-len(str)))
    }
    
    oldBit := (str[byteIndex] >> bitOffset) & 1
    
    if value == 1 {
        str[byteIndex] |= (1 << bitOffset)   // Set bit
    } else {
        str[byteIndex] &^= (1 << bitOffset)  // Clear bit
    }
    
    return int(oldBit), nil
}
```

#### 2. **GETBIT** - Read a Bit

```go
func (s *Store) GetBit(key string, offset int64) (int, error) {
    byteIndex := offset / 8
    
    if byteIndex >= len(str) {
        return 0, nil  // Beyond string = 0
    }
    
    bitOffset := uint(7 - (offset % 8))
    bit := (str[byteIndex] >> bitOffset) & 1
    
    return int(bit), nil
}
```

#### 3. **BITCOUNT** - Count Set Bits

```go
func (s *Store) BitCount(key string, start, end *int64) (int64, error) {
    count := int64(0)
    
    for i := startByte; i <= endByte; i++ {
        count += int64(bits.OnesCount8(uint8(str[i])))
    }
    
    return count, nil
}
```

Uses Go's `bits.OnesCount8` which uses CPU instructions for fast bit counting (POPCNT).

#### 4. **BITPOS** - Find First Bit

```go
func (s *Store) BitPos(key string, bit int, start, end *int64) (int64, error) {
    for i := startByte; i <= endByte; i++ {
        currentByte := str[i]
        
        for bitOffset := 0; bitOffset < 8; bitOffset++ {
            bitValue := (currentByte >> (7 - bitOffset)) & 1
            if bitValue == bit {
                return i*8 + int64(bitOffset), nil
            }
        }
    }
    
    return -1, nil  // Not found
}
```

#### 5. **BITOP** - Bitwise Operations

```go
// AND, OR, XOR, NOT operations on entire bitmaps
func (s *Store) BitOpAnd(destKey string, srcKeys []string) (int64, error) {
    // result[i] = src1[i] & src2[i] & src3[i] & ...
}

func (s *Store) BitOpNot(destKey, srcKey string) (int64, error) {
    // result[i] = ~src[i]
}
```

---

## Supported Commands

### SETBIT

Sets or clears the bit at the specified offset.

**Syntax:**
```
SETBIT key offset value
```

**Parameters:**
- `offset`: Bit position (0-based)
- `value`: 0 or 1

**Returns:** Original bit value before modification

**Example:**
```redis
SETBIT user:1000:login 0 1
# Returns: 0 (was not set)

SETBIT user:1000:login 0 1
# Returns: 1 (was already set)
```

**Time Complexity:** O(1)

---

### GETBIT

Returns the bit value at offset.

**Syntax:**
```
GETBIT key offset
```

**Returns:** 0 or 1 (returns 0 if offset is beyond string length)

**Example:**
```redis
SETBIT mykey 7 1
GETBIT mykey 7
# Returns: 1

GETBIT mykey 100
# Returns: 0 (beyond string, defaults to 0)
```

**Time Complexity:** O(1)

---

### BITCOUNT

Counts the number of bits set to 1.

**Syntax:**
```
BITCOUNT key [start end]
```

**Parameters:**
- `start`, `end`: Optional byte range (NOT bit range)

**Returns:** Number of bits set to 1

**Example:**
```redis
SETBIT mykey 0 1
SETBIT mykey 3 1
SETBIT mykey 7 1

BITCOUNT mykey
# Returns: 3

# Count bits in bytes 0-1 (first 16 bits)
BITCOUNT mykey 0 1
```

**Time Complexity:** O(N) where N is the number of bytes in the range

---

### BITPOS

Finds the position of the first bit set to 0 or 1.

**Syntax:**
```
BITPOS key bit [start] [end]
```

**Parameters:**
- `bit`: 0 or 1
- `start`, `end`: Optional byte range

**Returns:** Bit position, or -1 if not found

**Example:**
```redis
SETBIT mykey 0 1
SETBIT mykey 1 0
SETBIT mykey 2 1

# Find first bit set to 0
BITPOS mykey 0
# Returns: 1

# Find first bit set to 1
BITPOS mykey 1
# Returns: 0

# Find in specific byte range
BITPOS mykey 1 0 0
# Returns: Position in first byte only
```

**Time Complexity:** O(N) where N is the number of bytes in the range

---

### BITOP

Performs bitwise operations between strings.

**Syntax:**
```
BITOP operation destkey srckey [srckey ...]
```

**Operations:**
- `AND`: Bitwise AND
- `OR`: Bitwise OR
- `XOR`: Bitwise XOR
- `NOT`: Bitwise NOT (single source)

**Returns:** Size of the destination string in bytes

**Examples:**

```redis
# AND operation
SET key1 "foo"  # 01100110 01101111 01101111
SET key2 "bar"  # 01100010 01100001 01110010
BITOP AND dest key1 key2
# dest = 01100010 01100001 01100010 = "bab"

# OR operation
BITOP OR dest key1 key2
# dest = 01100110 01101111 01111111 = "fov"

# XOR operation
BITOP XOR dest key1 key2
# dest = 00000100 00001110 00011101

# NOT operation (single source)
BITOP NOT dest key1
# dest = ~key1
```

**Time Complexity:** O(N) where N is the length of the longest string

---

## Use Cases

### 1. **User Activity Tracking** ⭐ Most Common

Track daily active users:

```redis
# January 1st - User 123 was active
SETBIT active:2026-01-01 123 1

# January 2nd - User 123 and 456 were active
SETBIT active:2026-01-02 123 1
SETBIT active:2026-01-02 456 1

# Count active users on Jan 1
BITCOUNT active:2026-01-01
# Returns: 1

# Find users active on BOTH days (intersection)
BITOP AND active:both active:2026-01-01 active:2026-01-02
BITCOUNT active:both
# Returns: 1 (only user 123)

# Find users active on ANY day (union)
BITOP OR active:any active:2026-01-01 active:2026-01-02
BITCOUNT active:any
# Returns: 2 (users 123 and 456)
```

**Memory savings:**
- Traditional set: 100,000 users × 8 bytes (int64) = **800 KB**
- Bitmap: 100,000 bits / 8 = **12.5 KB**
- **Savings: 98.4%!** 💰

---

### 2. **Real-Time Analytics**

Track which features users have enabled:

```redis
# User 100 features: [email:on, sms:off, push:on, dark_mode:on]
SETBIT user:100:features 0 1  # Email notifications
SETBIT user:100:features 1 0  # SMS notifications
SETBIT user:100:features 2 1  # Push notifications
SETBIT user:100:features 3 1  # Dark mode

# Check if dark mode is enabled
GETBIT user:100:features 3
# Returns: 1

# Count enabled features
BITCOUNT user:100:features
# Returns: 3
```

---

### 3. **A/B Testing**

Track which users are in which experiment group:

```redis
# Put users in experiment A
SETBIT experiment:checkout-redesign:groupA 1000 1
SETBIT experiment:checkout-redesign:groupA 1001 1

# Put users in experiment B
SETBIT experiment:checkout-redesign:groupB 2000 1
SETBIT experiment:checkout-redesign:groupB 2001 1

# Count users in each group
BITCOUNT experiment:checkout-redesign:groupA
# Returns: 2
```

---

### 4. **Session Tracking**

Track online users:

```redis
# User 42 comes online
SETBIT online:users 42 1

# Check if user 42 is online
GETBIT online:users 42
# Returns: 1

# User 42 goes offline
SETBIT online:users 42 0

# Count total online users
BITCOUNT online:users
```

---

### 5. **IP Blacklisting**

Track blocked IPs efficiently:

```redis
# IP 192.168.1.42 → Convert to integer offset
# Let's say offset = 3232235562

SETBIT ip:blacklist 3232235562 1

# Check if IP is blacklisted
GETBIT ip:blacklist 3232235562
# Returns: 1 (blocked)
```

**Memory for 1M IPs:**
- Bitmap: 1,000,000 bits / 8 = **125 KB**
- Set: 1M × 15 bytes (avg IP string) = **15 MB**
- **Savings: 99.2%!**

---

### 6. **Permissions System**

Track user permissions:

```redis
# Permissions: [read:0, write:1, delete:2, admin:3]
# User 500 has read, write permissions
SETBIT user:500:perms 0 1  # read
SETBIT user:500:perms 1 1  # write
SETBIT user:500:perms 2 0  # delete
SETBIT user:500:perms 3 0  # admin

# Check if user has delete permission
GETBIT user:500:perms 2
# Returns: 0 (no permission)

# Check if user has write permission
GETBIT user:500:perms 1
# Returns: 1 (has permission)
```

---

### 7. **Coupon Redemption**

Track which coupons have been used:

```redis
# Coupon codes 0-999999
# User redeems coupon 12345
SETBIT coupons:used 12345 1

# Check if coupon 12345 is used
GETBIT coupons:used 12345
# Returns: 1 (already used)

# Try coupon 67890
GETBIT coupons:used 67890
# Returns: 0 (available)
```

---

## Performance Characteristics

### Time Complexity

| Operation | Complexity | Notes |
|-----------|-----------|-------|
| **SETBIT** | O(1) | Constant time |
| **GETBIT** | O(1) | Constant time |
| **BITCOUNT** | O(N) | N = bytes in range, uses POPCNT |
| **BITPOS** | O(N) | N = bytes in range, linear scan |
| **BITOP** | O(N) | N = longest string length |

### Space Complexity

```
Memory = ⌈max_bit_offset / 8⌉ bytes

Examples:
  - 1 million bits = 125 KB
  - 10 million bits = 1.25 MB
  - 100 million bits = 12.5 MB
  - 1 billion bits = 125 MB
```

### Sparse vs Dense Bitmaps

**Sparse bitmap** (few bits set):
```
SETBIT users:active 1000000 1  # Creates 125KB bitmap with 1 bit set!
```
⚠️ **Not space-efficient for sparse data**

**Dense bitmap** (many bits set):
```
# 10,000 consecutive users
for i in range(10000):
    SETBIT users:active i 1
# Only 1.25 KB - very efficient!
```
✅ **Excellent for dense data**

### Optimization: Use Sets for Sparse Data

```redis
# Sparse: Only 100 users out of 1 million active
# Bad approach (bitmap):
Memory = 1,000,000 / 8 = 125 KB

# Good approach (set):
SADD active:users 42 123 456 ...
Memory = 100 users × 8 bytes = 800 bytes

Savings: 99.4%!
```

**Rule of thumb:** Use bitmaps when >1% of bits will be set.

---

## Common Patterns

### Pattern 1: Daily Active Users (DAU)

```redis
# Track daily active users
SETBIT active:2026-01-09 user_id 1

# Count DAU
BITCOUNT active:2026-01-09

# Find users active for 7 consecutive days
BITOP AND streak:7days \
    active:2026-01-03 \
    active:2026-01-04 \
    active:2026-01-05 \
    active:2026-01-06 \
    active:2026-01-07 \
    active:2026-01-08 \
    active:2026-01-09

BITCOUNT streak:7days
# Returns: Number of users with 7-day streak
```

---

### Pattern 2: Weekly Active Users (WAU)

```redis
# Combine all days of the week
BITOP OR active:week-1 \
    active:2026-01-03 \
    active:2026-01-04 \
    active:2026-01-05 \
    active:2026-01-06 \
    active:2026-01-07 \
    active:2026-01-08 \
    active:2026-01-09

BITCOUNT active:week-1
# Returns: WAU
```

---

### Pattern 3: Retention Analysis

```redis
# Day 0: New users who signed up
SETBIT cohort:2026-01-01:day0 user_id 1

# Day 1: Track who came back
SETBIT cohort:2026-01-01:day1 user_id 1

# Day 7: Track who came back after a week
SETBIT cohort:2026-01-01:day7 user_id 1

# Calculate Day 1 retention
BITOP AND cohort:2026-01-01:retention1 \
    cohort:2026-01-01:day0 \
    cohort:2026-01-01:day1

# Retention rate = returned users / total users
# retention% = BITCOUNT(retention1) / BITCOUNT(day0) × 100
```

---

### Pattern 4: Funnel Analysis

```redis
# E-commerce funnel: viewed → added to cart → purchased
SETBIT funnel:viewed product_id 1      # User viewed product
SETBIT funnel:cart product_id 1        # User added to cart
SETBIT funnel:purchased product_id 1   # User purchased

# Users who viewed but didn't purchase
BITOP AND funnel:viewed-not-purchased funnel:viewed funnel:purchased
BITOP NOT funnel:viewed-not-purchased funnel:viewed-not-purchased
BITCOUNT funnel:viewed-not-purchased
```

---

### Pattern 5: Feature Flags

```redis
# Feature: Dark mode (bit 0), Premium (bit 1), Beta (bit 2)
SETBIT user:features 0 1  # Enable dark mode
SETBIT user:features 1 0  # Disable premium
SETBIT user:features 2 1  # Enable beta

# Check multiple features at once
GETBIT user:features 0  # Dark mode?
GETBIT user:features 1  # Premium?
GETBIT user:features 2  # Beta?

# Or encode as single byte and parse client-side
GET user:features
# Returns binary: 00000101 → Dark mode ON, Premium OFF, Beta ON
```

---

## Comparison with Other Data Structures

### Bitmaps vs Sets

| Feature | Bitmap | Set |
|---------|--------|-----|
| **Memory (1M items)** | 125 KB | 8-16 MB |
| **Add element** | O(1) | O(1) |
| **Check membership** | O(1) | O(1) |
| **Count elements** | O(N) | O(1) |
| **Union/Intersection** | O(N) - Fast | O(N×M) - Slower |
| **Sparse data** | ❌ Wasteful | ✅ Efficient |
| **Dense data** | ✅ Efficient | ❌ Wasteful |
| **Range queries** | ✅ Easy (BITCOUNT) | ❌ Difficult |

**Use Bitmap when:**
- IDs are sequential/dense (0, 1, 2, 3, ...)
- Tracking binary states (yes/no, on/off)
- Need fast bitwise operations (AND, OR, XOR)
- Memory is critical

**Use Set when:**
- IDs are sparse/random (1, 10000, 9999999, ...)
- Need set operations with high cardinality
- Need to iterate over members
- Need O(1) cardinality count

---

### Bitmaps vs Hash

```redis
# Hash approach (tracking features)
HSET user:100 email_notif 1
HSET user:100 sms_notif 0
HSET user:100 push_notif 1
Memory: ~60 bytes per user

# Bitmap approach
SETBIT user:100:features 0 1  # email
SETBIT user:100:features 1 0  # sms
SETBIT user:100:features 2 1  # push
Memory: 1 byte per user

Savings: 98.3%!
```

---

## Best Practices

### 1. **Know Your Data Density** 📊

```
Bitmap wins when:
  - Bit density > 1% (1 in 100 bits set)
  - Sequential IDs (user IDs: 1, 2, 3, ...)
  
Set wins when:
  - Bit density < 1%
  - Sparse/random IDs (timestamps, UUIDs)
```

**Example calculation:**
```
If you have 100,000 users and 500 are active:
  Density = 500 / 100,000 = 0.5%
  
  Bitmap: 100,000 bits / 8 = 12.5 KB
  Set:    500 items × 8 bytes = 4 KB
  
  → Use Set! (3x smaller)
```

---

### 2. **Use Expiration for Time-Series Data** ⏰

```redis
# Daily active users - expire after 30 days
SETBIT active:2026-01-09 user_id 1
EXPIRE active:2026-01-09 2592000  # 30 days

# Auto-cleanup old data
```

---

### 3. **Batch Operations** 🚀

```redis
# Bad: Multiple round trips
SETBIT users 1 1
SETBIT users 2 1
SETBIT users 3 1

# Better: Use BITOP with pre-computed bitmaps
# Or use Lua script for atomic batch operations
```

---

### 4. **Use BITCOUNT with Ranges** 🎯

```redis
# Don't count entire bitmap if you only need part of it
BITCOUNT active:users 0 1000  # Count only first 8000 bits (1000 bytes)

# Count specific byte range
BITCOUNT active:users 0 124   # First 1000 bits (125 bytes)
```

---

### 5. **Optimize for Common Access Patterns** 💡

```redis
# If you frequently need "users active in last 7 days"
# Pre-compute and cache result:
BITOP OR active:last7days active:day1 active:day2 ... active:day7
EXPIRE active:last7days 86400  # Refresh daily

# Now queries are instant:
BITCOUNT active:last7days
```

---

### 6. **Monitor Memory Usage** 📈

```redis
# Check bitmap size
STRLEN key
# Returns: number of bytes

# For 1 million users:
STRLEN active:users
# Returns: 125000 (125 KB)
```

---

### 7. **Consider Compression** 🗜️

For very large bitmaps with low density, consider:
- **Roaring Bitmaps** (external library)
- **Run-length encoding** (external solution)
- Or just use **Redis Sets** for sparse data

---

## Examples

### Example 1: Login Streak Tracker

```redis
# User 42 logs in on Jan 1
SETBIT login:user:42 1 1  # Day 1 of year

# User 42 logs in on Jan 2
SETBIT login:user:42 2 1  # Day 2 of year

# User 42 logs in on Jan 3
SETBIT login:user:42 3 1  # Day 3 of year

# Find first login
BITPOS login:user:42 1
# Returns: 1

# Count total login days in January (days 1-31)
BITCOUNT login:user:42 0 3  # Bytes 0-3 cover bits 0-31
# Returns: 3
```

---

### Example 2: Online Status

```redis
# User comes online
SETBIT online 42 1

# User goes offline
SETBIT online 42 0

# Check if user is online
GETBIT online 42

# Count online users
BITCOUNT online

# Find first online user
BITPOS online 1
```

---

### Example 3: Voting System

```redis
# Poll with 3 options (bits 0, 1, 2)
# User 100 votes for option 0
SETBIT poll:question1:option0 100 1

# User 101 votes for option 1
SETBIT poll:question1:option1 101 1

# Count votes for each option
BITCOUNT poll:question1:option0
BITCOUNT poll:question1:option1
BITCOUNT poll:question1:option2

# Check if user 100 already voted
GETBIT poll:question1:option0 100
# Returns: 1 (already voted)
```

---

### Example 4: Product Availability

```redis
# Track which products are in stock (1) or out of stock (0)
SETBIT inventory:stock 12345 1  # Product 12345 in stock
SETBIT inventory:stock 12346 0  # Product 12346 out of stock

# Quick stock check
GETBIT inventory:stock 12345
# Returns: 1 (in stock)

# Count total products in stock
BITCOUNT inventory:stock
```

---

### Example 5: Multi-Condition Queries

```redis
# Users who:
# - Are active today
# - Have premium subscription
# - Enabled email notifications

SETBIT active:today 100 1
SETBIT premium:users 100 1
SETBIT email:enabled 100 1

# Find users matching ALL conditions (AND)
BITOP AND qualified:users active:today premium:users email:enabled

# Count qualified users
BITCOUNT qualified:users

# Check if user 100 is qualified
GETBIT qualified:users 100
# Returns: 1
```

---

## Advanced Topics

### Handling Large Offsets

```redis
# Setting bit at position 4 billion
SETBIT huge 4000000000 1
# Creates a 500 MB string!

# Memory = 4,000,000,000 / 8 = 500,000,000 bytes = 500 MB
```

⚠️ **Warning:** Large offsets can consume huge amounts of memory. Consider alternative approaches for sparse high-offset data.

---

### Bitfield Operations (Advanced)

While not implemented in this version, Redis supports `BITFIELD` for atomic multi-bit operations:

```redis
# Get/Set multiple bits atomically
BITFIELD mykey \
  GET u4 0 \      # Get 4-bit unsigned integer at offset 0
  SET u4 0 15 \   # Set 4-bit unsigned integer at offset 0 to 15
  INCRBY u4 0 1   # Increment 4-bit integer at offset 0
```

This allows treating bitmaps as arrays of integers of arbitrary bit width.

---

## Conclusion

Bitmaps are a powerful tool for:
✅ **Space-efficient binary state tracking**
✅ **Fast bitwise operations on large datasets**
✅ **Real-time analytics and user tracking**
✅ **High-performance boolean queries**

**When to use:**
- Sequential/dense IDs
- Binary states (yes/no, on/off)
- Set operations on large datasets
- Memory is critical

**When NOT to use:**
- Sparse data (<1% density)
- Need to iterate over members
- Need O(1) cardinality
- Random/UUID identifiers

---

## Further Reading

- [Redis Bitmaps Documentation](https://redis.io/docs/data-types/bitmaps/)
- [Fast Bitmap Operations (Roaring Bitmaps)](https://roaringbitmap.org/)
- [Population Count Algorithm (POPCNT)](https://en.wikipedia.org/wiki/Hamming_weight)
- [Use Cases: Real-time Analytics with Bitmaps](https://blog.getspool.com/2011/11/29/fast-easy-realtime-metrics-using-redis-bitmaps/)
