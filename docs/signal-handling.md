# Signal Handling in Go - Why Buffered Channels Matter

## Overview

When handling OS signals (like Ctrl+C) in Go, using a **buffered channel** is critical to prevent signal loss. This document explains the internal mechanics and why unbuffered channels can miss signals.

---

## The Problem: Signal Loss with Unbuffered Channels

### What Happens Internally

```go
// ❌ DANGEROUS: Unbuffered channel
sigChan := make(chan os.Signal)  // Buffer size = 0
signal.Notify(sigChan, os.Interrupt)

go func() {
    <-sigChan  // Wait for signal
    shutdown()
}()
```

---

## Complete Signal Flow

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     OPERATING SYSTEM                         │
│                                                               │
│  User Action: Ctrl+C pressed                                 │
│         │                                                     │
│         ▼                                                     │
│  ┌──────────────┐                                            │
│  │ Kernel       │                                            │
│  │ Signal Table │  SIGINT (signal #2)                        │
│  └──────┬───────┘                                            │
│         │                                                     │
└─────────┼─────────────────────────────────────────────────────┘
          │
          │ System Call Boundary
          │
┌─────────▼─────────────────────────────────────────────────────┐
│                     GO RUNTIME                                 │
│                                                                 │
│  ┌────────────────────────────────────────────────────────┐   │
│  │  Signal Handler (C code in runtime)                    │   │
│  │                                                         │   │
│  │  1. OS delivers signal via sigaction()                 │   │
│  │  2. sig_handler() catches it                           │   │
│  │  3. Writes to internal pipe (non-blocking)             │   │
│  │  4. signal_recv goroutine reads from pipe              │   │
│  │  5. Looks up registered channels                       │   │
│  └────────────────────────┬───────────────────────────────┘   │
│                           │                                    │
│                           ▼                                    │
│                  ┌─────────────────┐                          │
│                  │  Try send to    │                          │
│                  │  sigChan        │                          │
│                  └─────────┬───────┘                          │
│                            │                                   │
└────────────────────────────┼───────────────────────────────────┘
                             │
                             ▼
                    User's goroutine receives
```

---

## Why Unbuffered Channels Fail

### The Critical Code in Go Runtime

```go
// runtime/signal_unix.go (simplified)

func signal_dispatch(sig os.Signal) {
    // Look up channels registered for this signal
    channels := handlers[sig]
    
    // Try to send to each registered channel
    for _, ch := range channels {
        // NON-BLOCKING SEND!
        select {
        case ch <- sig:
            // Success: channel accepted signal
        default:
            // FAILURE: nobody receiving, signal DROPPED!
            // Runtime can't block here (in signal handler context)
        }
    }
}
```

**Key Point:** The runtime uses **non-blocking send** with a `default` clause. If the channel can't accept the signal immediately, it's discarded.

---

## Timeline: Unbuffered Channel (Signal Lost)

```
Time: 0ms
Program: sigChan := make(chan os.Signal)  ◄─── Unbuffered!
         signal.Notify(sigChan, os.Interrupt)
         
         [Server starting... takes 50ms]

Time: 10ms (user is impatient!)
User: [Presses Ctrl+C]

Time: 10.001ms
Kernel: [Delivers SIGINT to process]

Time: 10.002ms
Go Runtime (signal handler):
    select {
    case sigChan <- os.Interrupt:  ◄─── Try to send
        // SUCCESS if receiver ready
    default:
        // FAILS! No goroutine receiving yet
        // Signal is DROPPED! ❌
    }

Time: 50ms (later)
Program: go func() {
             <-sigChan  ◄─── Goroutine starts
         }()
         
         But signal was already lost at 10ms!
         Program never shuts down!
```

---

## Timeline: Buffered Channel (Signal Saved)

```
Time: 0ms
Program: sigChan := make(chan os.Signal, 1)  ◄─── Buffer: 1
         signal.Notify(sigChan, os.Interrupt)
         
         [Server starting... takes 50ms]

Time: 10ms
User: [Presses Ctrl+C]

Time: 10.001ms
Kernel: [Delivers SIGINT to process]

Time: 10.002ms
Go Runtime (signal handler):
    select {
    case sigChan <- os.Interrupt:  ◄─── Sends to buffer
        // SUCCESS! Goes into buffer slot
    default:
        // Not reached
    }

Buffer state: [os.Interrupt] ✅

Time: 50ms (later)
Program: go func() {
             sig := <-sigChan  ◄─── Retrieves from buffer
             shutdown()         ◄─── Executes shutdown
         }()
         
✅ Signal successfully received and processed!
```

---

## Why Runtime Can't Block

### The Problem with Signal Handlers

Signal handlers run in a **restricted context** where most operations are unsafe:

```c
// In signal handler context (C code in Go runtime)

void sig_handler(int sig) {
    // ❌ Can't call malloc() - might deadlock
    // ❌ Can't acquire locks - might deadlock  
    // ❌ Can't block - would freeze process
    // ✅ Can only: write to pipe, set flags
    
    // This is safe:
    write(sig_pipe, &sig, sizeof(sig));
}
```

**Why these restrictions exist:**
- Signal can interrupt ANY code, including malloc/lock code
- If signal handler tries same operation = **deadlock**
- Example:
  ```
  Thread is in malloc() → holding malloc lock
  Signal arrives → calls sig_handler()
  sig_handler() calls malloc() → tries to acquire same lock
  DEADLOCK! ☠️
  ```

### Go's Solution: Two-Stage Processing

```go
// Stage 1: Signal handler (C code, restricted context)
void sig_handler(int sig) {
    write(sig_pipe, &sig, sizeof(sig));  // Just write to pipe
}

// Stage 2: Go goroutine (safe context)
func signal_recv() {
    for {
        sig := read_from_pipe()  // Read signal from pipe
        signal_dispatch(sig)      // Now safe to do more work
    }
}

func signal_dispatch(sig os.Signal) {
    // In safe Go context now
    // Try to send to registered channels (non-blocking)
    select {
    case ch <- sig:
        // Sent
    default:
        // Drop (can't block here either - would stall all signals)
    }
}
```

---

## Buffer Size: Why 1 is Enough

### Multiple Signals

```go
sigChan := make(chan os.Signal, 1)

// User frantically presses Ctrl+C multiple times:
User: [Ctrl+C] [Ctrl+C] [Ctrl+C] [Ctrl+C]

// Runtime tries to send each:
Runtime: sigChan <- SIGINT  ✅ Goes in buffer
         sigChan <- SIGINT  ❌ Buffer full, dropped
         sigChan <- SIGINT  ❌ Buffer full, dropped
         sigChan <- SIGINT  ❌ Buffer full, dropped

Buffer: [SIGINT] ◄─── Only one stored
```

**This is perfectly fine!**
- We only need **one** signal to trigger shutdown
- Additional signals are redundant
- Buffer size 1 is sufficient

### What if We Need to Handle All Signals?

If you truly need every signal (rare):

```go
// Larger buffer
sigChan := make(chan os.Signal, 100)

// Or process immediately
go func() {
    for sig := range sigChan {
        // Process each signal
        handleSignal(sig)
    }
}()
```

But for shutdown handlers, **buffer size 1 is standard practice**.

---

## Real-World Race Condition Example

### Vulnerable Code

```go
func main() {
    // ❌ Unbuffered channel
    sigChan := make(chan os.Signal)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    
    // Start server (slow initialization)
    srv := NewServer()
    go srv.Start()  // Takes 100ms to fully start
    
    // Register signal handler
    go func() {
        <-sigChan
        srv.Shutdown()
    }()
    
    // Problem: If user presses Ctrl+C in first 100ms,
    // signal might arrive before handler goroutine is scheduled!
    
    select {}
}
```

### Race Window

```
0ms:    Program starts
        signal.Notify() called
        Server starting...
        Handler goroutine created (not scheduled yet)

10ms:   User presses Ctrl+C ◄─── Too early!
        Runtime tries to send signal
        Nobody receiving (handler goroutine not scheduled)
        Signal DROPPED!

50ms:   Handler goroutine finally scheduled
        Blocks on <-sigChan
        But signal already lost
        Program never exits!
```

### Fixed Code

```go
func main() {
    // ✅ Buffered channel
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    
    srv := NewServer()
    go srv.Start()
    
    go func() {
        <-sigChan
        srv.Shutdown()
    }()
    
    select {}
}

// Now signal is buffered even if handler not ready yet!
```

---

## Best Practices

### 1. Always Use Buffered Channel for Signals

```go
// ✅ CORRECT
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

// ❌ WRONG
sigChan := make(chan os.Signal)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
```

### 2. Handle Signals Early in main()

```go
func main() {
    // Set up signal handling FIRST
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        <-sigChan
        cleanup()
    }()
    
    // Then start your application
    runApp()
}
```

### 3. Use Context for Propagation

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        <-sigChan
        log.Println("Shutdown signal received")
        cancel()  // Propagate to all goroutines
    }()
    
    // Pass context to all components
    srv.Start(ctx)
}
```

### 4. Stop Receiving Signals When Done

```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt)

// When done
signal.Stop(sigChan)  // Unregister
close(sigChan)        // Close channel
```

---

## Common Mistakes

### Mistake 1: Unbuffered Channel

```go
// ❌ Can lose signal
sigChan := make(chan os.Signal)
```

### Mistake 2: Buffer Too Large

```go
// ⚠️ Unnecessary - wastes memory
sigChan := make(chan os.Signal, 1000)
```

### Mistake 3: Blocking in Signal Handler

```go
// ❌ DON'T do heavy work directly
go func() {
    <-sigChan
    
    // This blocks signal handling for other signals!
    time.Sleep(10 * time.Second)  // Bad!
    doSlowWork()                  // Bad!
}()

// ✅ DO use separate goroutine
go func() {
    <-sigChan
    go doSlowWork()  // Spawn separate goroutine
    quickCleanup()   // Only quick work here
}()
```

### Mistake 4: Not Handling Multiple Signals

```go
// ❌ Only handles first signal
<-sigChan
shutdown()

// ✅ Can handle repeated signals (e.g., force shutdown)
count := 0
for sig := range sigChan {
    count++
    if count == 1 {
        log.Println("Graceful shutdown...")
        go gracefulShutdown()
    } else {
        log.Println("Force shutdown!")
        os.Exit(1)
    }
}
```

---

## Summary

### The Key Points

1. **Go runtime sends signals non-blocking** (can't block in signal handler)
2. **Unbuffered channels require immediate receiver** (runtime uses `default` clause)
3. **Race condition exists** between signal arrival and goroutine scheduling
4. **Buffer size 1 solves the problem** (signal stored until handler ready)
5. **Only one signal needed** for shutdown (additional signals redundant)

### The Golden Rule

**Always use buffered channel with size 1 for OS signal handling:**

```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
```

This simple change prevents signal loss and ensures reliable shutdown behavior!

---

## 🔥 Critical Understanding: The Go Runtime as Sender

> **KEY INSIGHT:** When you use `signal.Notify`, the **sender is the Go runtime itself**, processing an OS signal like Ctrl+C.

The runtime's signal handling logic is critical to the application's health. It is **programmed with the priority of never blocking** on a user's poorly set up channel.

### Behavior Matrix

| Scenario | Channel Type | Sender Action | Outcome |
|----------|--------------|---------------|---------|
| **Sender (Runtime) Arrives First** | Unbuffered | Attempts send, finds no receiver | ❌ **Signal is dropped** |
| **Sender (Runtime) Arrives First** | Buffered (Size ≥1) | Puts value in buffer, finds space | ✅ **Signal is saved in buffer** |
| **Receiver Arrives First** | Any | Waits for the sender | ⏳ Receiver blocks until signal arrives |

### The Strict Requirement

> ⚠️ **When the Go runtime is the sender, the requirement is much stricter:**
> 
> The receiver **absolutely must be waiting first**, OR the channel **must be buffered**.
> 
> Since you cannot guarantee that your signal handler goroutine will be scheduled and reach the `<-sigChan` line before the signal arrives (especially during startup), you **must rely on the buffered channel** to store the signal until the receiver is ready.

---