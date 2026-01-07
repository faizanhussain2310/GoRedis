# Pattern Matching Optimization using Trie Data Structure

## Table of Contents
1. [Problem Statement](#problem-statement)
2. [The Bottleneck](#the-bottleneck)
3. [Solution Overview](#solution-overview)
4. [Trie Data Structure Explained](#trie-data-structure-explained)
5. [Implementation Details](#implementation-details)
6. [Performance Analysis](#performance-analysis)
7. [Example Walkthrough](#example-walkthrough)
8. [Trade-offs](#trade-offs)

---

## Problem Statement

In Redis Pub/Sub, when a message is published to a channel, the system needs to:
1. Deliver the message to all subscribers of that exact channel
2. Deliver the message to all subscribers of **patterns** that match the channel name

The second step is the performance bottleneck.

### Original Implementation

```go
// For every PUBLISH
for pattern, subscribers := range ps.patterns {
    if matchPattern(pattern, channel) {  // Compile regex, then match
        // Send message to subscribers
    }
}
```

**Time Complexity**: **O(P × M)** where:
- `P` = number of active patterns
- `M` = average time to compile regex + match

**Problems**:
1. **Regex compilation on every publish**: Converting glob pattern (`news.*`) to regex happens for EVERY message
2. **Full pattern iteration**: Checks ALL patterns even if most won't match
3. **No prefix filtering**: Pattern `news.*` and channel `sports.football` are compared even though they clearly won't match

---

## The Bottleneck

Let's say we have 10,000 pattern subscriptions:
- `news.*` (1000 subscribers)
- `sports.*` (1000 subscribers)
- `finance.*` (1000 subscribers)
- ... and 7,000 more patterns

When someone publishes to `news.breaking`:

**Before Optimization**:
1. Check `news.*` → compile regex → match ✅
2. Check `sports.*` → compile regex → match ❌
3. Check `finance.*` → compile regex → match ❌
4. ... check 9,997 more patterns (all compile regex)

**Result**: 10,000 regex compilations + 10,000 regex matches

**After Optimization**:
1. Use trie to get candidates: `["news.*"]` (1 pattern)
2. Use pre-compiled regex from cache
3. Match once ✅

**Result**: 1 trie lookup + 1 cached regex match

---

## Solution Overview

We implement **two complementary optimizations**:

### 1. **Pre-compiled Regex Cache**
- **What**: Compile each pattern's regex ONCE when subscribing
- **When**: During `PSUBSCRIBE` command
- **Storage**: `map[string]*regexp.Regexp`
- **Benefit**: Eliminates repeated regex compilation

### 2. **Pattern Trie (Prefix Tree)**
- **What**: Index patterns by their prefix (before first wildcard)
- **When**: During `PSUBSCRIBE` and `PUNSUBSCRIBE`
- **Storage**: Tree structure
- **Benefit**: Reduces candidate pattern set from O(P) to O(log P)

---

## Trie Data Structure Explained

### What is a Trie?

A **trie** (prefix tree) is a tree data structure where:
- Each node represents a character
- Paths from root to nodes represent strings
- Common prefixes share the same path

### Visual Example

Patterns:
- `news.*`
- `news.sports.*`
- `finance.*`
- `fin.*`
- `*.breaking` (starts with wildcard)

**Trie Structure**:

```
                    root
                   /  |  \
                 n    f   [*.breaking]
                 |    |
                 e    i
                 |    |
                 w    n
                 |    |  \
                 s    .   a
                 |   [fin.*]  n
                 .         c
           [news.*]       e
                 |         .
                 s      [finance.*]
                 p
                 o
                 r
                 t
                 s
                 .
          [news.sports.*]
```

**Key Observations**:
1. `news.*` and `news.sports.*` share the path `n→e→w→s`
2. `finance.*` and `fin.*` share the path `f→i→n`
3. Patterns starting with wildcards (`*.breaking`) are stored at root

### How Trie Helps Matching

When publishing to `news.breaking`:

**Traverse**:
1. Start at root: collect `[*.breaking]`
2. Follow `n`: continue
3. Follow `e`: continue
4. Follow `w`: continue
5. Follow `s`: collect `[news.*]`
6. Follow `.`: collect `[news.sports.*]`
7. Follow `b`: no child, stop

**Result**: Candidates = `[*.breaking, news.*, news.sports.*]`

We only check **3 patterns** instead of all patterns in the system!

---

## Implementation Details

### Data Structures

```go
// PatternTrieNode represents a node in the pattern trie
type PatternTrieNode struct {
    children map[byte]*PatternTrieNode  // Child nodes indexed by character
    patterns []string                    // Patterns stored at this node
}

// PatternTrie is a prefix tree for efficient pattern lookup
type PatternTrie struct {
    root *PatternTrieNode
}

// PubSub struct additions
type PubSub struct {
    // ... existing fields ...
    
    // OPTIMIZATION: Trie for efficient pattern prefix lookup
    patternTrie *PatternTrie
    
    // OPTIMIZATION: Pre-compiled regex cache for patterns
    compiledPatterns map[string]*regexp.Regexp
}
```

### Key Operations

#### 1. Insert Pattern (During PSUBSCRIBE)

```go
func (pt *PatternTrie) Insert(pattern string) {
    node := pt.root
    
    // Extract prefix before first wildcard
    prefixLen := 0
    for i := 0; i < len(pattern); i++ {
        if pattern[i] == '*' || pattern[i] == '?' {
            break
        }
        prefixLen++
    }
    
    prefix := pattern[:prefixLen]
    
    // Traverse/create nodes for the prefix
    for i := 0; i < len(prefix); i++ {
        char := prefix[i]
        if node.children[char] == nil {
            node.children[char] = &PatternTrieNode{
                children: make(map[byte]*PatternTrieNode),
                patterns: make([]string, 0),
            }
        }
        node = node.children[char]
    }
    
    // Store the full pattern at this node
    node.patterns = append(node.patterns, pattern)
}
```

**Example**: Insert `news.sports.*`
1. Extract prefix: `news.sports` (before `*`)
2. Create path: `n→e→w→s→.→s→p→o→r→t→s`
3. At final node, store `news.sports.*`

#### 2. Get Matching Patterns (During PUBLISH)

```go
func (pt *PatternTrie) GetMatchingPatterns(channel string) []string {
    result := make([]string, 0)
    
    // Collect patterns from root (patterns starting with wildcards)
    result = append(result, pt.root.patterns...)
    
    // Traverse the trie following the channel name
    node := pt.root
    for i := 0; i < len(channel); i++ {
        char := channel[i]
        if node.children[char] == nil {
            break // No more matching prefixes
        }
        node = node.children[char]
        
        // Collect patterns at this node
        result = append(result, node.patterns...)
    }
    
    return result
}
```

**Example**: Query `news.breaking`
1. Collect from root: `[*.breaking]`
2. Follow `n`: collect patterns at `n`
3. Follow `e`: collect patterns at `ne`
4. Follow `w`: collect patterns at `new`
5. Follow `s`: collect patterns at `news`
6. Follow `.`: collect patterns at `news.` → found `[news.*]`
7. Follow `b`: no child (only `s` exists for "sports"), stop

**Result**: `[*.breaking, news.*]`

**Note**: `news.sports.*` is NOT a candidate because we can't reach its node - the trie stops when it can't find a `b` child after `news.`

#### 3. Remove Pattern (During PUNSUBSCRIBE)

```go
func (pt *PatternTrie) Remove(pattern string) {
    // Extract prefix before first wildcard
    prefixLen := 0
    for i := 0; i < len(pattern); i++ {
        if pattern[i] == '*' || pattern[i] == '?' {
            break
        }
        prefixLen++
    }
    
    prefix := pattern[:prefixLen]
    
    // Navigate to the node
    node := pt.root
    for i := 0; i < len(prefix); i++ {
        char := prefix[i]
        if node.children[char] == nil {
            return // Pattern not found
        }
        node = node.children[char]
    }
    
    // Remove pattern from the node's list
    for i, p := range node.patterns {
        if p == pattern {
            node.patterns = append(node.patterns[:i], node.patterns[i+1:]...)
            break
        }
    }
}
```

### Integration with PubSub

#### During PSUBSCRIBE

```go
func (ps *PubSub) PSubscribe(subscriberID string, sub *Subscriber, patterns ...string) []string {
    ps.mu.Lock()
    defer ps.mu.Unlock()

    for _, pattern := range patterns {
        if ps.patterns[pattern] == nil {
            ps.patterns[pattern] = make(map[string]*Subscriber)
            
            // OPTIMIZATION: Add pattern to trie
            ps.patternTrie.Insert(pattern)
            
            // OPTIMIZATION: Pre-compile regex
            ps.compiledPatterns[pattern] = compilePattern(pattern)
        }
        
        // ... rest of subscription logic ...
    }
}
```

#### During PUBLISH

```go
func (ps *PubSub) Publish(channel string, payload string) int {
    ps.mu.RLock()
    defer ps.mu.RUnlock()

    // ... send to channel subscribers ...

    // OPTIMIZATION: Use trie to get candidate patterns
    candidatePatterns := ps.patternTrie.GetMatchingPatterns(channel)
    
    // Send to pattern subscribers
    for _, pattern := range candidatePatterns {
        subs, exists := ps.patterns[pattern]
        if !exists {
            continue
        }
        
        // OPTIMIZATION: Use pre-compiled regex
        compiledRegex := ps.compiledPatterns[pattern]
        if compiledRegex != nil && compiledRegex.MatchString(channel) {
            // Send message to subscribers
        }
    }
}
```

#### During PUNSUBSCRIBE

```go
func (ps *PubSub) PUnsubscribe(subscriberID string, patterns ...string) []string {
    ps.mu.Lock()
    defer ps.mu.Unlock()

    for _, pattern := range patterns {
        if subs, exists := ps.patterns[pattern]; exists {
            delete(subs, subscriberID)

            if len(subs) == 0 {
                delete(ps.patterns, pattern)
                
                // OPTIMIZATION: Remove pattern from trie and cache
                ps.patternTrie.Remove(pattern)
                delete(ps.compiledPatterns, pattern)
            }
        }
    }
}
```

---

## Performance Analysis

### Time Complexity

| Operation | Before | After | Improvement |
|-----------|--------|-------|-------------|
| **PSUBSCRIBE** | O(1) | O(L) | Acceptable (L = pattern length) |
| **PUNSUBSCRIBE** | O(1) | O(L) | Acceptable |
| **PUBLISH** | O(P × M) | O(L + C × R) | **Significant** |

Where:
- `P` = total patterns
- `M` = regex compile + match time
- `L` = channel name length
- `C` = candidate patterns (typically << P)
- `R` = regex match time (no compilation)

### Space Complexity

| Component | Space |
|-----------|-------|
| **Trie** | O(T) where T = total characters in all pattern prefixes |
| **Compiled Regex Cache** | O(P × S) where S = average regex size |
| **Total Additional** | O(T + P × S) |

### Real-World Example

**Scenario**: 100,000 patterns, publish to `news.breaking`

**Before**:
- 100,000 regex compilations
- 100,000 regex matches
- **~500ms** (estimated)

**After**:
- 1 trie traversal: 14 characters = 14 node lookups
- ~10 candidate patterns (only those starting with "news")
- 10 cached regex matches (no compilation)
- **~0.5ms** (estimated)

**Speedup**: **1000x faster** 🚀

---

## Example Walkthrough

### Scenario Setup

1. **Patterns subscribed**:
   - `news.*` → 100 subscribers
   - `news.sports.*` → 50 subscribers
   - `sports.*` → 200 subscribers
   - `finance.*` → 150 subscribers
   - `*.breaking` → 75 subscribers
   - `*.*` → 300 subscribers

2. **Trie after subscriptions**:
```
root: [*.*, *.breaking]
├─ n
│  └─ e
│     └─ w
│        └─ s
│           └─ .: [news.*]
│              └─ s
│                 └─ p
│                    └─ o
│                       └─ r
│                          └─ t
│                             └─ s
│                                └─ .: [news.sports.*]
├─ s
│  └─ p
│     └─ o
│        └─ r
│           └─ t
│              └─ s
│                 └─ .: [sports.*]
└─ f
   └─ i
      └─ n
         └─ a
            └─ n
               └─ c
                  └─ e
                     └─ .: [finance.*]
```

### Publishing to `news.breaking`

**Step 1: Trie Traversal**
```go
channel = "news.breaking"
node = root
result = []

// Collect from root
result = [*.*, *.breaking]

// Follow 'n'
node = root.children['n']
result = [*.*, *.breaking]  // No patterns at 'n'

// Follow 'e'
node = node.children['e']
result = [*.*, *.breaking]  // No patterns at 'ne'

// Follow 'w'
node = node.children['w']
result = [*.*, *.breaking]  // No patterns at 'new'

// Follow 's'
node = node.children['s']
result = [*.*, *.breaking]  // No patterns at 'news'

// Follow '.'
node = node.children['.']
result = [*.*, *.breaking, news.*]  // Found news.*!

// Try to follow 'b' (from 'breaking')
node.children['b'] == nil  // ❌ No 'b' child! Only 's' exists (for 'sports')
break  // Stop traversal - never reach news.sports.*
```

**Candidates**: `[*.*, *.breaking, news.*]` (only 3 patterns!)

**Why `news.sports.*` is NOT included**:
- It's stored at trie node `news.sports.` (after 11 characters)
- We stop at `news.` because there's no `b` child (only `s` for "sports")
- The trie correctly filters it out based on prefix mismatch!

**Step 2: Regex Matching**

```go
for _, pattern := range candidates {
    regex := ps.compiledPatterns[pattern]  // Pre-compiled!
    
    if regex.MatchString("news.breaking") {
        // Send to subscribers
    }
}
```

| Pattern | Regex | Match? | Send to Subscribers |
|---------|-------|--------|---------------------|
| `*.*` | `^.*\..*$` | ✅ Yes | 300 subscribers |
| `*.breaking` | `^.*\.breaking$` | ✅ Yes | 75 subscribers |
| `news.*` | `^news\..*$` | ✅ Yes | 100 subscribers |

**Total subscribers notified**: 475

**Performance**:
- Trie lookup: 14 characters = 14 comparisons
- Regex matches: 3 (using pre-compiled regex, all match!)
- **Patterns filtered by trie**: `sports.*`, `finance.*`, `news.sports.*` (never checked!)

---

## Trade-offs

### Advantages ✅

1. **Massive performance gain for PUBLISH**
   - From O(P × M) to O(L + C × R)
   - 100x-1000x faster in real-world scenarios

2. **Pre-compiled regex eliminates redundant work**
   - Compile once, use many times
   - Reduces CPU usage significantly

3. **Prefix filtering is intelligent**
   - `sports.*` never checked when publishing to `news.breaking`
   - Logarithmic reduction in search space

4. **Scales well with pattern count**
   - Linear memory growth
   - Sub-linear query time

### Disadvantages ❌

1. **Increased memory usage**
   - Trie nodes: ~100 bytes per pattern prefix
   - Compiled regex: ~500 bytes per pattern
   - Total: ~600 bytes per pattern overhead

2. **Slower PSUBSCRIBE/PUNSUBSCRIBE**
   - Trie insertion: O(L) vs O(1)
   - Negligible in practice (subscriptions are rare)

3. **Code complexity**
   - More complex implementation
   - Additional data structures to maintain

4. **Diminishing returns for small pattern counts**
   - Overhead not worth it for < 100 patterns
   - Trie lookup may be slower than linear scan

### When to Use This Optimization

**Use when**:
- 1000+ active patterns
- High publish rate (100+ msgs/sec)
- Long pattern prefixes (good filtering)

**Don't use when**:
- < 100 patterns (overhead > benefit)
- Low publish rate (< 10 msgs/sec)
- Patterns mostly start with wildcards (`*.*`)

---

## Conclusion

The **Trie + Regex Cache** optimization transforms Redis Pub/Sub pattern matching from a bottleneck into a strength:

- **Before**: Every publish checks every pattern (expensive)
- **After**: Every publish checks only relevant patterns (efficient)

This optimization is inspired by production Redis implementations and database indexing techniques, demonstrating how the right data structure (trie) can turn an O(N) problem into an O(log N) solution.

**Key Takeaway**: When you have a large search space (all patterns) but only need a small subset (matching patterns), use a data structure that can filter efficiently (trie with prefix indexing).
