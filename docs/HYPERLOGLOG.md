# HyperLogLog Data Structure

## Table of Contents
- [What is HyperLogLog?](#what-is-hyperloglog)
- [How It Works](#how-it-works)
- [Implementation Details](#implementation-details)
- [Commands](#commands)
- [Use Cases](#use-cases)
- [Performance Characteristics](#performance-characteristics)
- [Examples](#examples)

---

## What is HyperLogLog?

**HyperLogLog (HLL)** is a probabilistic data structure used for **cardinality estimation** - counting the number of unique elements in a dataset. It was invented by Philippe Flajolet and colleagues in 2007.

### Key Features

- **Memory Efficient**: Uses only ~12KB to track billions of unique elements
- **Constant Memory**: Memory usage doesn't grow with cardinality
- **Fast Operations**: O(1) time complexity for add and count
- **Probabilistic**: Provides approximate count with ~0.81% standard error
- **Mergeable**: Multiple HyperLogLogs can be merged to get union cardinality

### Trade-offs

| Aspect | HyperLogLog | Exact Set |
|--------|-------------|-----------|
| Memory | 12 KB (fixed) | O(n) - grows with elements |
| Accuracy | ~0.81% error | 100% accurate |
| Count Speed | O(1) | O(1) |
| Add Speed | O(1) | O(1) or O(log n) |
| Merge | O(m) registers | O(n + m) elements |

**When to use HyperLogLog**: When you need to count unique items at scale and can accept ~1% error margin (e.g., unique visitors, unique IP addresses, distinct events).

---

## How It Works

### Core Algorithm

HyperLogLog exploits a clever statistical observation:

1. **Hash Elements**: Each element is hashed to a uniform random bit string
2. **Leading Zeros**: Count the position of the first `1` bit (leading zeros + 1)
3. **Statistical Insight**: If you observe a value with k leading zeros, you've likely seen ~2^k unique elements
4. **Multiple Registers**: Use m = 2^p registers (p = precision) to reduce variance
5. **Harmonic Mean**: Combine register values using harmonic mean for final estimate

### Visual Example

```
Element: "user123"
Hash:    1101001011010110...  (64 bits)
         ┬───────┬──────────
         │       └─ Remaining bits (find leading zeros)
         └─ First 14 bits select register index

Register Index: 0b11010010110101 = 13525
Remaining bits:  10110...
Leading zeros:   0 (first bit is 1)
Store in register[13525]: max(current, 1)
```

With 16,384 registers (p=14), each element updates one register. To count:

```
Estimate = α * m² / Σ(2^(-register[i]))
where:
  m = number of registers (16,384 for precision 14)
  α = bias correction constant (≈ 0.7213 for large m)
  Σ = sum over all m registers
```

**Why m² and not just m?**

We're computing the harmonic mean of 2^(-register[i]) values, then scaling by m:
- Harmonic mean: m / Σ(2^(-register[i]))
- Scale by m to get cardinality: m × [m / Σ(2^(-register[i]))] = m² / Σ(2^(-register[i]))

**Intuition**: Each register observes leading zeros independently. If one register sees k leading zeros, it suggests ~2^k unique elements hit that register. With m registers, we combine these m independent observations and multiply by m to estimate total cardinality.

**Significance of α (alpha)**:
- α is a **bias correction constant** that fixes systematic errors in the harmonic mean estimator
- Without α, estimates would be consistently biased (too high or low)
- Derived mathematically to minimize mean squared error
- Value depends on m:
  - m = 16: α = 0.673
  - m = 32: α = 0.697
  - m = 64: α = 0.709
  - m ≥ 128: α = 0.7213 / (1 + 1.079/m) ≈ 0.7213

### Why It's Accurate

- **Law of Large Numbers**: Averaging m independent observations reduces variance by factor of √m
- **Harmonic Mean**: Resists outlier influence (extreme values have less impact than arithmetic mean)
- **Bias Correction (α)**: Compensates for systematic estimator bias
- **Range-Specific Corrections**: Different formulas for small, medium, and large cardinalities

---

## Implementation Details

### Data Structure

```go
type HyperLogLog struct {
    registers []uint8  // Array of m = 2^precision registers
    precision uint8    // Typically 14 (16,384 registers)
    m         uint32   // Number of registers (2^precision)
    alpha     float64  // Bias correction constant (~0.7213)
}
```

**Why use uint8 for registers?**

The `uint8` type (unsigned 8-bit integer) is optimal for storing register values:

- **Range**: 0 to 255 (2^8 - 1)
- **Size**: 1 byte per register
- **Sufficiency**: Leading zeros in a 64-bit hash can be at most 64, so uint8's range of 0-255 is more than adequate
- **Memory efficiency**: 16,384 registers × 1 byte = 16,384 bytes = exactly 12 KB

**Type comparison:**
```go
uint8:  0-255           (1 byte)  ✅ Perfect fit
uint16: 0-65,535        (2 bytes) ❌ Doubles memory (24 KB)
uint32: 0-4,294,967,295 (4 bytes) ❌ Quadruples memory (48 KB)
```

Using uint8 keeps memory usage minimal while providing sufficient capacity for all possible leading zero counts.

### Memory Layout

| Precision | Registers | Memory | Standard Error |
|-----------|-----------|--------|----------------|
| 4 | 16 | 16 bytes | 26% |
| 8 | 256 | 256 bytes | 6.5% |
| 10 | 1,024 | 1 KB | 3.25% |
| 12 | 4,096 | 4 KB | 1.625% |
| **14** | **16,384** | **12 KB** | **0.81%** ⭐ |
| 16 | 65,536 | 64 KB | 0.4% |

**Default precision**: 14 (matches Redis)

### Algorithm Steps

#### Add Operation
```go
func (hll *HyperLogLog) Add(element string) bool {
    hash := hash64(element)                    // 1. Hash to 64 bits
    idx := hash >> (64 - precision)            // 2. Extract register index
    w := hash << precision                     // 3. Get remaining bits
    leadingZeros := countLeadingZeros(w) + 1   // 4. Count leading 0s
    
    if leadingZeros > registers[idx] {         // 5. Update if larger
        registers[idx] = leadingZeros
        return true
    }
    return false
}
```

#### Count Operation
```go
func (hll *HyperLogLog) Count() int64 {
    sum := 0.0
    zeros := 0
    
    // Calculate harmonic mean
    for _, val := range registers {
        sum += 1.0 / pow(2.0, val)
        if val == 0 { zeros++ }
    }
    
    rawEstimate := alpha * m * m / sum
    
    // Apply range corrections
    if rawEstimate <= 2.5*m && zeros > 0 {
        // Small range: linear counting
        return m * log(m / zeros)
    } else if rawEstimate > (1/30)*2^32 {
        // Large range: bias correction for extreme cardinalities
        // Note: This corrects estimator mathematical bias, not hash collisions
        // (64-bit hashes support up to 2^64, but HLL estimator accuracy degrades >2^32)
        return -2^32 * log(1 - rawEstimate/2^32)
    }
    return rawEstimate
}
```

#### Merge Operation
```go
func (hll1 *HyperLogLog) Merge(hll2 *HyperLogLog) {
    // Take maximum value for each register
    for i := 0; i < m; i++ {
        registers[i] = max(hll1.registers[i], hll2.registers[i])
    }
}
```

---

## Commands

### PFADD
Add elements to a HyperLogLog.

**Syntax:**
```
PFADD key element [element ...]
```

**Returns:**
- `1` if at least one register was updated (element potentially new)
- `0` if no registers were updated (all elements already seen)

**Time Complexity:** O(N) where N is number of elements

**Example:**
```redis
PFADD visitors "user1" "user2" "user3"
# Returns: 1 (registers updated)

PFADD visitors "user1"
# Returns: 0 (already seen, no update)
```

---

### PFCOUNT
Get estimated cardinality of one or more HyperLogLogs.

**Syntax:**
```
PFCOUNT key [key ...]
```

**Returns:** Integer representing approximate unique count

**Time Complexity:**
- Single key: O(1) - just reads registers and computes
- Multiple keys: O(N) where N is number of keys (must merge temporarily)

**Examples:**
```redis
# Single key
PFCOUNT visitors
# Returns: 3 (approximate)

# Multiple keys (union)
PFCOUNT visitors:today visitors:yesterday
# Returns: 5 (approximate union of both sets)
```

**Note:** When multiple keys are provided, PFCOUNT returns the approximated cardinality of the union without modifying any keys.

---

### PFMERGE
Merge multiple HyperLogLogs into one.

**Syntax:**
```
PFMERGE destkey sourcekey [sourcekey ...]
```

**Returns:** `OK`

**Time Complexity:** O(N) where N is number of registers (typically 16,384)

**Example:**
```redis
# Merge daily visitors into weekly total
PFMERGE visitors:week visitors:monday visitors:tuesday visitors:wednesday
# Returns: OK

PFCOUNT visitors:week
# Returns: ~1500 (union of all daily visitors)
```

**Note:** PFMERGE overwrites `destkey`. If `destkey` exists, it's replaced with the merge result.

---

## Use Cases

### 1. **Website Analytics**
Track unique visitors without storing user IDs.

```redis
# Track daily unique visitors
PFADD visitors:2024-01-07 "user123"
PFADD visitors:2024-01-07 "user456"
PFADD visitors:2024-01-07 "user123"  # Duplicate

PFCOUNT visitors:2024-01-07
# Returns: ~2 (approximately 2 unique visitors)

# Weekly unique visitors (union)
PFCOUNT visitors:2024-01-01 visitors:2024-01-02 ... visitors:2024-01-07
```

**Memory savings**: 1M unique users = 12 KB (vs ~8 MB for exact set)

---

### 2. **Real-time Stream Processing**
Count distinct events in high-volume streams.

```redis
# Count unique IP addresses per hour
PFADD ips:2024-01-07:14:00 "192.168.1.1"
PFADD ips:2024-01-07:14:00 "10.0.0.5"

# Get hourly count
PFCOUNT ips:2024-01-07:14:00

# Get daily count (union of all hours)
PFCOUNT ips:2024-01-07:00:00 ips:2024-01-07:01:00 ... ips:2024-01-07:23:00
```

---

### 3. **Social Media Metrics**
Track unique interactions (likes, shares, views).

```redis
# Track unique users who viewed a post
PFADD post:12345:views "user1" "user2" "user3"

# Track unique users who liked the post
PFADD post:12345:likes "user1" "user4"

# Get engagement metrics
PFCOUNT post:12345:views  # ~3 views
PFCOUNT post:12345:likes  # ~2 likes

# Engagement rate
PFCOUNT post:12345:likes / PFCOUNT post:12345:views
# ≈ 66.7%
```

---

### 4. **Database Query Optimization**
Estimate distinct values in columns.

```redis
# During ETL, estimate unique customer IDs
PFADD customers:unique "C001" "C002" "C003"

PFCOUNT customers:unique
# Returns: ~3 (helps decide index strategy)
```

---

### 5. **IoT and Sensor Data**
Count unique device IDs or sensor events.

```redis
# Track active IoT devices per datacenter
PFADD devices:dc1 "sensor001" "sensor002"
PFADD devices:dc2 "sensor003" "sensor004"

# Total active devices globally
PFMERGE devices:global devices:dc1 devices:dc2
PFCOUNT devices:global
# Returns: ~4
```

---

### 6. **Security & Fraud Detection**
Detect anomalies in unique connection patterns.

```redis
# Track unique failed login IPs
PFADD failed:user123 "192.168.1.100"

PFCOUNT failed:user123
# If count > threshold, flag as potential attack
```

---

## Performance Characteristics

### Time Complexity

| Operation | Complexity | Description |
|-----------|------------|-------------|
| PFADD | O(N) | N = number of elements to add |
| PFCOUNT (1 key) | O(1) | Reads m registers and computes |
| PFCOUNT (k keys) | O(k * m) | Merges k HLLs with m registers |
| PFMERGE | O(N * m) | N = number of source keys |

### Memory Usage

```
Memory per HLL = m * 1 byte
               = 2^precision bytes
               = 16,384 bytes (for p=14)
               ≈ 12 KB + overhead
```

### Accuracy

Standard error: **σ ≈ 1.04 / √m**

For p=14 (m=16,384):
- Standard error: ~0.81%
- 95% confidence interval: ±1.62%
- 99.7% confidence interval: ±2.43%

**Example**: If true cardinality is 1,000,000:
- HLL estimate: ~1,000,000 ± 8,100 (68% confidence)
- HLL estimate: ~1,000,000 ± 16,200 (95% confidence)

---

## Examples

### Basic Usage

```redis
# Add elements
redis> PFADD unique:users "alice" "bob" "charlie"
(integer) 1

redis> PFADD unique:users "alice"  # Duplicate
(integer) 0

redis> PFCOUNT unique:users
(integer) 3
```

### Multi-Key Count (Union)

```redis
# Track page views per user
redis> PFADD page:1 "user1" "user2" "user3"
redis> PFADD page:2 "user2" "user3" "user4"

# Count unique visitors across both pages
redis> PFCOUNT page:1 page:2
(integer) 4  # user1, user2, user3, user4
```

### Merging HyperLogLogs

```redis
# Daily active users
redis> PFADD dau:monday "u1" "u2" "u3"
redis> PFADD dau:tuesday "u2" "u3" "u4" "u5"
redis> PFADD dau:wednesday "u3" "u5" "u6"

# Weekly active users (merge)
redis> PFMERGE wau dau:monday dau:tuesday dau:wednesday
OK

redis> PFCOUNT wau
(integer) 6  # u1, u2, u3, u4, u5, u6
```

### Time-Series Analytics

```redis
# Track hourly unique IPs
redis> PFADD ips:2024-01-07:00 "1.2.3.4" "5.6.7.8"
redis> PFADD ips:2024-01-07:01 "1.2.3.4" "9.10.11.12"
redis> PFADD ips:2024-01-07:02 "13.14.15.16"

# Hourly counts
redis> PFCOUNT ips:2024-01-07:00
(integer) 2

# Daily count (union of all hours)
redis> PFCOUNT ips:2024-01-07:00 ips:2024-01-07:01 ips:2024-01-07:02
(integer) 4  # union of all unique IPs
```

### Large-Scale Example

```go
// Track 100M unique user IDs with only 12KB memory
for i := 0; i < 100_000_000; i++ {
    userID := fmt.Sprintf("user_%d", i)
    client.Send("PFADD", "all_users", userID)
}

count := client.Send("PFCOUNT", "all_users")
// Returns: ~100,000,000 ± 810,000 (0.81% error)
// Memory used: 12 KB (vs 800 MB for exact set)
```

---

## Comparison with Other Approaches

### Exact Set (Redis Set)

```redis
# Exact counting with SET
SADD users:exact "user1" "user2" "user3"
SCARD users:exact
# Returns: 3 (100% accurate)
# Memory: ~100 bytes/user = 100 KB for 1000 users
```

**When to use:**
- Need 100% accuracy
- Small datasets (<10K elements)
- Need set operations (intersection, difference)

### HyperLogLog

```redis
# Approximate counting with HLL
PFADD users:approx "user1" "user2" "user3"
PFCOUNT users:approx
# Returns: 3 (±0.81% error)
# Memory: 12 KB (fixed, even for billions)
```

**When to use:**
- Large datasets (>100K elements)
- Can tolerate ~1% error
- Memory is constrained
- Only need cardinality (not membership tests)

### Bloom Filter

```redis
# Membership testing (not counting)
BF.ADD filter "user1"
BF.EXISTS filter "user1"  # true
BF.EXISTS filter "user2"  # false (or false positive)
```

**When to use:**
- Need membership tests ("have I seen this before?")
- Can tolerate false positives
- Don't need exact count

---

## Advanced Topics

### Error Analysis

HyperLogLog error follows a normal distribution:

```
Error ~ N(0, σ²) where σ = 1.04 / √m
```

For p=14 (m=16,384):
- 68% of estimates within ±0.81% of true value
- 95% of estimates within ±1.62% of true value
- 99.7% of estimates within ±2.43% of true value

### Choosing Precision

```go
// Lower precision = less memory, higher error
hll_p10 := NewHyperLogLog(10)  // 1 KB, ~3.25% error
hll_p14 := NewHyperLogLog(14)  // 12 KB, ~0.81% error
hll_p16 := NewHyperLogLog(16)  // 64 KB, ~0.4% error
```

**Why Redis chose Precision 14 (16,384 registers) as default:**

This is **NOT a hardware limitation**, but a carefully considered **memory-accuracy tradeoff**:

1. **Accuracy Sweet Spot**: 
   - ~0.81% error is acceptable for most analytics use cases
   - Going to p=16 (64KB) only improves to ~0.4% error (diminishing returns)
   - Most production scenarios don't need sub-1% accuracy

2. **Memory Efficiency**:
   - 12 KB per HLL is small enough to store millions of HLLs in RAM
   - Example: 1 million HLLs = 12 GB (manageable)
   - With p=16: 1 million HLLs = 64 GB (more expensive)

3. **Cache Performance**:
   - 12 KB fits well in CPU L2/L3 cache (modern CPUs have 256KB-32MB cache)
   - Better cache locality = faster COUNT operations
   - 64 KB might cause more cache misses

4. **Network Efficiency**:
   - Smaller size = faster serialization/deserialization
   - Less bandwidth when replicating to replicas
   - Faster backups and restores

5. **Historical Context**:
   - Redis designed for commodity hardware (not enterprise servers)
   - 12 KB was chosen when typical servers had 4-16 GB RAM (circa 2013)
   - Still relevant today for cost-effective deployments

**You can customize precision** based on your needs:
- **p=10-12**: Millions of HLLs, ~1-3% error acceptable, tight memory budget
- **p=14**: Default, balanced choice for most use cases
- **p=16**: Need <0.5% error, have memory to spare, fewer HLLs

**Guideline:**
- Precision 10-12: Millions of elements, ~1-3% error acceptable
- Precision 14: Default (Redis standard), good balance
- Precision 16: Need <0.5% error, have memory

### Serialization

HyperLogLog can be serialized for persistence:

```go
// Export registers
registers := hll.GetRegisters()  // []uint8 (12 KB)

// Store in database, file, or network
db.Store("hll:backup", registers)

// Restore
hll2 := NewHyperLogLog(14)
hll2.SetRegisters(registers)
```

---

## References

- [Original HyperLogLog Paper (2007)](http://algo.inria.fr/flajolet/Publications/FlFuGaMe07.pdf) - Flajolet et al.
- [Redis HyperLogLog Documentation](https://redis.io/commands/pfcount)
- [HyperLogLog in Practice](https://research.google/pubs/pub40671/) - Google Research
- [Cardinality Estimation Done Right](http://dbms-arch.wikia.com/wiki/Cardinality_Estimation)

---

## Summary

**HyperLogLog** is a powerful probabilistic data structure that enables:

✅ **Constant memory usage** (12 KB regardless of cardinality)  
✅ **Fast operations** (O(1) add and count)  
✅ **High accuracy** (~0.81% standard error)  
✅ **Mergeable** (union of multiple HLLs)  
✅ **Scalable** (handles billions of unique elements)  

**Perfect for**: Analytics, metrics, unique counting at scale where approximate results are acceptable.

**Not suitable for**: Exact counting requirements, membership testing, small datasets where memory isn't a concern.
