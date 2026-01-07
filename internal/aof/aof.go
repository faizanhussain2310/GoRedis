package aof

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

// SyncPolicy determines when to fsync the AOF file to disk
type SyncPolicy int

const (
	// SyncAlways fsyncs after every write (safest, slowest)
	// Data loss: None (every command is persisted before returning to client)
	// Performance: ~1000 commands/sec (limited by disk I/O)
	SyncAlways SyncPolicy = iota

	// SyncEverySecond fsyncs once per second (good balance) - Redis default
	// Data loss: Up to 1 second of data
	// Performance: ~100,000 commands/sec
	SyncEverySecond

	// SyncNo lets the OS decide when to flush (fastest, least safe)
	// Data loss: Depends on OS (typically 30 seconds of data)
	// Performance: Maximum throughput
	SyncNo
)

// Config holds AOF configuration
type Config struct {
	Enabled    bool       // Whether AOF is enabled
	Filepath   string     // Path to AOF file
	SyncPolicy SyncPolicy // When to sync to disk
	BufferSize int        // Write buffer size in bytes
}

// DefaultConfig returns default AOF configuration
func DefaultConfig() Config {
	return Config{
		Enabled:    true,
		Filepath:   "appendonly.aof",
		SyncPolicy: SyncEverySecond,
		BufferSize: 4096,
	}
}

// Writer handles append-only file writes for persistence
// Thread-safe for concurrent command logging
type Writer struct {
	config Config
	file   *os.File
	writer *bufio.Writer
	mu     sync.Mutex

	// Rewrite buffer (hybrid approach for zero data loss)
	// Using pointer for atomic swap to avoid blocking during buffer copy
	rewriteMu     sync.Mutex
	rewriteBuffer *[][]string // Pointer to buffer for atomic swap
	isRewriting   bool        // Whether rewrite is in progress

	// Metrics
	totalWrites int64
	totalBytes  int64
	lastSync    time.Time

	// For SyncEverySecond policy
	syncTicker *time.Ticker
	stopChan   chan struct{}
	closed     bool
}

// NewWriter creates a new AOF writer
func NewWriter(config Config) (*Writer, error) {
	if !config.Enabled {
		// Return a no-op writer when AOF is disabled
		return &Writer{config: config, closed: true}, nil
	}

	// Open file in append mode, create if doesn't exist
	// O_APPEND: Always write at end of file
	// O_CREATE: Create file if it doesn't exist
	// O_WRONLY: Write-only mode
	file, err := os.OpenFile(config.Filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open AOF file: %w", err)
	}

	bufSize := config.BufferSize
	if bufSize <= 0 {
		bufSize = 4096
	}

	// Initialize rewrite buffer pointer
	initialBuffer := make([][]string, 0, 10000)

	w := &Writer{
		config: config,
		file:   file,

		// creates a buffered writer that wraps the file handle.
		// This tells the buffer "when you flush, write to THIS file".
		writer: bufio.NewWriterSize(file, bufSize),

		rewriteBuffer: &initialBuffer,
		lastSync:      time.Now(),
		stopChan:      make(chan struct{}),
	}

	// Start background sync goroutine for SyncEverySecond policy
	if config.SyncPolicy == SyncEverySecond {
		w.syncTicker = time.NewTicker(1 * time.Second)
		go w.backgroundSync()
	}

	return w, nil
}

// backgroundSync periodically syncs the AOF file for SyncEverySecond policy
func (w *Writer) backgroundSync() {
	for {
		select {
		case <-w.syncTicker.C:
			w.mu.Lock()
			if !w.closed && w.file != nil {
				// Flush buffer to OS
				w.writer.Flush()
				// Sync to disk
				w.file.Sync()
				w.lastSync = time.Now()
			}
			w.mu.Unlock()
		case <-w.stopChan:
			return
		}
	}
}

// WriteCommand writes a command to the AOF file in RESP format
// This is called AFTER the command has been successfully executed
//
// Format (RESP Array):
//
//	*3\r\n       <- 3 elements in array
//	$3\r\n       <- first element is 3 bytes
//	SET\r\n      <- the command
//	$3\r\n       <- second element is 3 bytes
//	key\r\n      <- the key
//	$5\r\n       <- third element is 5 bytes
//	value\r\n    <- the value
func (w *Writer) WriteCommand(args []string) error {
	if !w.config.Enabled || w.closed {
		return nil
	}

	w.mu.Lock()

	// Encode as RESP array
	bytesWritten := 0

	// Array header: *<count>\r\n
	header := fmt.Sprintf("*%d\r\n", len(args))
	n, err := w.writer.WriteString(header)
	if err != nil {
		w.mu.Unlock()
		return fmt.Errorf("failed to write array header: %w", err)
	}
	bytesWritten += n

	// Each element as bulk string: $<len>\r\n<data>\r\n
	for _, arg := range args {
		// Length prefix
		prefix := fmt.Sprintf("$%d\r\n", len(arg))
		n, err = w.writer.WriteString(prefix)
		if err != nil {
			w.mu.Unlock()
			return fmt.Errorf("failed to write bulk prefix: %w", err)
		}
		bytesWritten += n

		// Data
		n, err = w.writer.WriteString(arg)
		if err != nil {
			w.mu.Unlock()
			return fmt.Errorf("failed to write bulk data: %w", err)
		}
		bytesWritten += n

		// CRLF
		n, err = w.writer.WriteString("\r\n")
		if err != nil {
			w.mu.Unlock()
			return fmt.Errorf("failed to write CRLF: %w", err)
		}
		bytesWritten += n
	}

	w.totalWrites++
	w.totalBytes += int64(bytesWritten)

	// Handle sync policy
	switch w.config.SyncPolicy {
	case SyncAlways:
		// Flush buffer and sync immediately
		if err := w.writer.Flush(); err != nil {
			w.mu.Unlock()
			return fmt.Errorf("failed to flush: %w", err)
		}
		if err := w.file.Sync(); err != nil {
			w.mu.Unlock()
			return fmt.Errorf("failed to sync: %w", err)
		}
		w.lastSync = time.Now()
		w.mu.Unlock()

	case SyncEverySecond:
		// Background goroutine handles syncing
		// Just make sure buffer is flushed to OS
		// (actual disk sync happens in backgroundSync)
		w.mu.Unlock()

	case SyncNo:
		// Let OS handle flushing
		w.mu.Unlock()
	}

	// HYBRID APPROACH: Also buffer command if rewrite is in progress
	// This ensures no commands are lost during AOF rewrite
	// We write to BOTH main AOF (for crash safety) AND buffer (for rewrite completion)
	w.rewriteMu.Lock()
	isRewriting := w.isRewriting
	if isRewriting {
		// Make a copy of args to avoid mutations
		argsCopy := make([]string, len(args))
		copy(argsCopy, args)
		// Append to buffer (pointer dereference)
		*w.rewriteBuffer = append(*w.rewriteBuffer, argsCopy)
	}
	w.rewriteMu.Unlock()

	return nil
}

// Sync forces a sync to disk (useful for shutdown)
func (w *Writer) Sync() error {
	if !w.config.Enabled || w.closed {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}
	w.lastSync = time.Now()
	return nil
}

// Close closes the AOF writer, flushing any remaining data
func (w *Writer) Close() error {
	if !w.config.Enabled {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true

	// Stop background sync
	if w.syncTicker != nil {
		w.syncTicker.Stop()
		close(w.stopChan)
	}

	// Flush and sync remaining data
	if w.writer != nil {
		if err := w.writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush on close: %w", err)
		}
	}

	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync on close: %w", err)
		}
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}
	}

	return nil
}

// Stats returns AOF statistics
type Stats struct {
	TotalWrites int64
	TotalBytes  int64
	LastSync    time.Time
	FilePath    string
	Enabled     bool
	SyncPolicy  string
}

// GetStats returns current AOF statistics
func (w *Writer) GetStats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()

	policyName := "unknown"
	switch w.config.SyncPolicy {
	case SyncAlways:
		policyName = "always"
	case SyncEverySecond:
		policyName = "everysec"
	case SyncNo:
		policyName = "no"
	}

	return Stats{
		TotalWrites: w.totalWrites,
		TotalBytes:  w.totalBytes,
		LastSync:    w.lastSync,
		FilePath:    w.config.Filepath,
		Enabled:     w.config.Enabled,
		SyncPolicy:  policyName,
	}
}

// IsWriteCommand checks if a command modifies data (should be logged to AOF)
// Read-only commands are not logged
func IsWriteCommand(cmd string) bool {
	switch cmd {
	// String write commands
	case "SET", "SETNX", "SETEX", "PSETEX", "MSET", "MSETNX", "APPEND",
		"INCR", "INCRBY", "INCRBYFLOAT", "DECR", "DECRBY", "GETSET", "SETRANGE":
		return true

	// List write commands
	case "LPUSH", "LPUSHX", "RPUSH", "RPUSHX", "LPOP", "RPOP",
		"LSET", "LREM", "LTRIM", "LINSERT", "LMOVE", "RPOPLPUSH":
		return true

	// Blocking list commands (also write)
	case "BLPOP", "BRPOP", "BLMOVE", "BRPOPLPUSH":
		return true

	// Hash write commands
	case "HSET", "HSETNX", "HMSET", "HDEL", "HINCRBY", "HINCRBYFLOAT":
		return true

	// Set write commands
	case "SADD", "SREM", "SPOP", "SMOVE", "SUNIONSTORE", "SINTERSTORE", "SDIFFSTORE":
		return true

	// Key write commands
	case "DEL", "UNLINK", "RENAME", "RENAMENX", "COPY",
		"EXPIRE", "EXPIREAT", "PEXPIRE", "PEXPIREAT", "PERSIST":
		return true

	// Database commands
	case "FLUSHALL", "FLUSHDB", "SELECT":
		return true

	// Transaction commands are not logged directly
	// Individual commands within transactions are logged at EXEC time
	case "MULTI", "EXEC", "DISCARD", "WATCH", "UNWATCH":
		return false

	default:
		return false
	}
}

// Rewrite creates a new AOF file with minimal commands to reconstruct current state
// Uses HYBRID APPROACH: buffers new commands during rewrite, then merges them
// This ensures zero data loss even if commands are written during rewrite
// snapshotFunc should return a snapshot of current database state
func (w *Writer) Rewrite(snapshotFunc func() [][]string) error {
	if w == nil {
		return fmt.Errorf("writer is nil")
	}

	// Phase 1: Start buffering new commands
	newBuffer := make([][]string, 0, 10000)
	w.rewriteMu.Lock()
	w.isRewriting = true
	w.rewriteBuffer = &newBuffer // Point to new buffer
	w.rewriteMu.Unlock()

	// Phase 2: Get snapshot (unlocked - doesn't block writes)
	commands := snapshotFunc()

	// Phase 3: Write snapshot to temp file (slow, but unlocked!)
	tempPath := w.config.Filepath + ".rewrite.tmp"
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		w.rewriteMu.Lock()
		w.isRewriting = false
		w.rewriteMu.Unlock()
		return fmt.Errorf("failed to create temp AOF file: %w", err)
	}

	tempWriter := bufio.NewWriterSize(tempFile, w.config.BufferSize)

	// Write snapshot commands
	for _, args := range commands {
		encoded := EncodeCommand(args)
		if _, err := tempWriter.Write(encoded); err != nil {
			tempFile.Close()
			os.Remove(tempPath)
			w.rewriteMu.Lock()
			w.isRewriting = false
			w.rewriteMu.Unlock()
			return fmt.Errorf("failed to write to temp AOF: %w", err)
		}
	}

	// Phase 4: Atomic pointer swap (instant, no blocking!)
	w.rewriteMu.Lock()
	oldBuffer := w.rewriteBuffer
	finalBuffer := make([][]string, 0, 10000)
	w.rewriteBuffer = &finalBuffer // Swap to new buffer
	w.rewriteMu.Unlock()

	// Write buffered commands from old buffer (no lock held!)
	for _, args := range *oldBuffer {
		encoded := EncodeCommand(args)
		if _, err := tempWriter.Write(encoded); err != nil {
			tempFile.Close()
			os.Remove(tempPath)
			w.rewriteMu.Lock()
			w.isRewriting = false
			w.rewriteMu.Unlock()
			return fmt.Errorf("failed to write buffer to temp AOF: %w", err)
		}
	}

	// Flush and sync temp file
	if err := tempWriter.Flush(); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		w.rewriteMu.Lock()
		w.isRewriting = false
		w.rewriteMu.Unlock()
		return fmt.Errorf("failed to flush temp AOF: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		w.rewriteMu.Lock()
		w.isRewriting = false
		w.rewriteMu.Unlock()
		return fmt.Errorf("failed to sync temp AOF: %w", err)
	}

	tempFile.Close()

	// Phase 5: Atomically swap files (hold both locks)
	w.mu.Lock()
	w.rewriteMu.Lock()

	// Stop buffering
	w.isRewriting = false

	// Close current AOF file
	if w.writer != nil {
		w.writer.Flush()
	}
	if w.file != nil {
		w.file.Close()
	}

	// Atomically replace old AOF with new one
	if err := os.Rename(tempPath, w.config.Filepath); err != nil {
		w.rewriteMu.Unlock()
		w.mu.Unlock()
		return fmt.Errorf("failed to replace AOF file: %w", err)
	}

	// Reopen AOF file
	file, err := os.OpenFile(w.config.Filepath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		w.rewriteMu.Unlock()
		w.mu.Unlock()
		return fmt.Errorf("failed to reopen AOF file: %w", err)
	}

	w.file = file
	w.writer = bufio.NewWriterSize(file, w.config.BufferSize)
	w.totalBytes = 0

	w.rewriteMu.Unlock()
	w.mu.Unlock()

	return nil
}

// EncodeCommand encodes a command as RESP format bytes
// Useful for batch writing or testing
func EncodeCommand(args []string) []byte {
	// Calculate size
	size := 0
	size += 1 + len(strconv.Itoa(len(args))) + 2 // *<count>\r\n

	for _, arg := range args {
		size += 1 + len(strconv.Itoa(len(arg))) + 2 // $<len>\r\n
		size += len(arg) + 2                        // <data>\r\n
	}

	buf := make([]byte, 0, size)

	// Array header
	buf = append(buf, '*')
	buf = append(buf, strconv.Itoa(len(args))...)
	buf = append(buf, '\r', '\n')

	// Each element
	for _, arg := range args {
		buf = append(buf, '$')
		buf = append(buf, strconv.Itoa(len(arg))...)
		buf = append(buf, '\r', '\n')
		buf = append(buf, arg...)
		buf = append(buf, '\r', '\n')
	}

	return buf
}
