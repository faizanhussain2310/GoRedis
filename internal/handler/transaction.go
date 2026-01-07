package handler

import (
	"sync"

	"redis/internal/protocol"
)

// TransactionState represents the state of a client's transaction
type TransactionState int

const (
	TxNone    TransactionState = iota // No transaction
	TxStarted                         // MULTI called, commands being queued
)

// QueuedCommand represents a command queued during a transaction
type QueuedCommand struct {
	Name string
	Args []string
}

// Transaction holds the transaction state for a client
type Transaction struct {
	State       TransactionState
	Queue       []QueuedCommand
	WatchedKeys map[string]struct{} // keys being watched (no version needed with dirty flag)
	Dirty       bool                // True if any watched key was modified
}

// NewTransaction creates a new transaction
func NewTransaction() *Transaction {
	return &Transaction{
		State:       TxNone,
		Queue:       make([]QueuedCommand, 0),
		WatchedKeys: make(map[string]struct{}),
		Dirty:       false,
	}
}

// Reset clears the transaction state
func (t *Transaction) Reset() {
	t.State = TxNone
	t.Queue = t.Queue[:0]
	// Note: WatchedKeys and Dirty are NOT cleared on EXEC/DISCARD in Redis
	// They are cleared on successful EXEC or explicit UNWATCH
}

// ClearWatches clears all watched keys and dirty flag
func (t *Transaction) ClearWatches() {
	t.WatchedKeys = make(map[string]struct{})
	t.Dirty = false
}

// MarkDirty marks this transaction as dirty (a watched key was modified)
func (t *Transaction) MarkDirty() {
	t.Dirty = true
}

// IsWatching checks if this transaction is watching the given key
func (t *Transaction) IsWatching(key string) bool {
	_, exists := t.WatchedKeys[key]
	return exists
}

// TransactionManager manages transactions across all clients
// Uses Redis-style reverse index: key → clients watching this key
type TransactionManager struct {
	mu           sync.RWMutex
	transactions map[int64]*Transaction        // clientID -> transaction
	keyWatchers  map[string]map[int64]struct{} // key -> set of clientIDs watching this key
}

// NewTransactionManager creates a new transaction manager
func NewTransactionManager() *TransactionManager {
	return &TransactionManager{
		transactions: make(map[int64]*Transaction),
		keyWatchers:  make(map[string]map[int64]struct{}),
	}
}

// GetTransaction gets or creates a transaction for a client
func (tm *TransactionManager) GetTransaction(clientID int64) *Transaction {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tx, exists := tm.transactions[clientID]; exists {
		return tx
	}

	tx := NewTransaction()
	tm.transactions[clientID] = tx
	return tx
}

// RemoveClient removes a client's transaction (on disconnect)
func (tm *TransactionManager) RemoveClient(clientID int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tx, exists := tm.transactions[clientID]; exists {
		// Remove from all key watchers
		for key := range tx.WatchedKeys {
			if watchers, ok := tm.keyWatchers[key]; ok {
				delete(watchers, clientID)
				if len(watchers) == 0 {
					delete(tm.keyWatchers, key)
				}
			}
		}
		delete(tm.transactions, clientID)
	}
}

// WatchKey adds a key to the client's watch list
// O(1) operation
func (tm *TransactionManager) WatchKey(clientID int64, key string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tx, exists := tm.transactions[clientID]
	if !exists {
		return
	}

	// Add to transaction's watched keys
	tx.WatchedKeys[key] = struct{}{}

	// Add to reverse index (key -> clients)
	if _, ok := tm.keyWatchers[key]; !ok {
		tm.keyWatchers[key] = make(map[int64]struct{})
	}
	tm.keyWatchers[key][clientID] = struct{}{}
}

// UnwatchAllKeys removes all watches for a client
// O(K) where K = number of keys watched by this client
func (tm *TransactionManager) UnwatchAllKeys(clientID int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tx, exists := tm.transactions[clientID]
	if !exists {
		return
	}

	// Remove from reverse index for each watched key
	for key := range tx.WatchedKeys {
		if watchers, ok := tm.keyWatchers[key]; ok {
			delete(watchers, clientID)
			if len(watchers) == 0 {
				delete(tm.keyWatchers, key)
			}
		}
	}

	// Clear transaction's watches
	tx.ClearWatches()
}

// TouchKey marks all clients watching this key as dirty
// Called when a key is modified - O(M) where M = clients watching this key
// This is the key optimization: work happens at write time, not at EXEC time
func (tm *TransactionManager) TouchKey(key string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	watchers, exists := tm.keyWatchers[key]
	if !exists {
		return // No one watching this key
	}

	// Mark all watching clients as dirty
	for clientID := range watchers {
		if tx, ok := tm.transactions[clientID]; ok {
			tx.MarkDirty()
		}
	}
}

// TouchKeys marks all clients watching any of these keys as dirty
func (tm *TransactionManager) TouchKeys(keys []string) {
	if len(keys) == 0 {
		return
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, key := range keys {
		if watchers, exists := tm.keyWatchers[key]; exists {
			for clientID := range watchers {
				if tx, ok := tm.transactions[clientID]; ok {
					tx.MarkDirty()
				}
			}
		}
	}
}

// IsTransactionDirty checks if the transaction should be aborted
// O(1) operation - just check the dirty flag!
func (tm *TransactionManager) IsTransactionDirty(tx *Transaction) bool {
	return tx.Dirty
}

// TransactionContext holds transaction-related data for command execution
type TransactionContext struct {
	InTransaction bool
	Execute       bool // True when executing EXEC
}

// Transaction command responses
var (
	QueuedResponse = []byte("+QUEUED\r\n")
	OKResponse     = []byte("+OK\r\n")
	NilResponse    = []byte("$-1\r\n")
)

// handleMulti handles the MULTI command
func (h *CommandHandler) handleMulti(cmd *protocol.Command) []byte {
	// This is handled specially in the pipeline - shouldn't reach here normally
	return OKResponse
}

// handleExec handles the EXEC command
func (h *CommandHandler) handleExec(cmd *protocol.Command) []byte {
	// This is handled specially in the pipeline - shouldn't reach here normally
	return protocol.EncodeError("ERR EXEC without MULTI")
}

// handleDiscard handles the DISCARD command
func (h *CommandHandler) handleDiscard(cmd *protocol.Command) []byte {
	// This is handled specially in the pipeline - shouldn't reach here normally
	return protocol.EncodeError("ERR DISCARD without MULTI")
}

// handleWatch handles the WATCH command
func (h *CommandHandler) handleWatch(cmd *protocol.Command) []byte {
	// This is handled specially in the pipeline - shouldn't reach here normally
	return OKResponse
}

// handleUnwatch handles the UNWATCH command
func (h *CommandHandler) handleUnwatch(cmd *protocol.Command) []byte {
	// This is handled specially in the pipeline - shouldn't reach here normally
	return OKResponse
}

// IsTransactionCommand checks if a command is a transaction control command
func IsTransactionCommand(cmd string) bool {
	switch cmd {
	case "MULTI", "EXEC", "DISCARD", "WATCH", "UNWATCH":
		return true
	}
	return false
}

// GetWriteKeys returns the keys that a command will write to (for WATCH)
// Returns nil if the command doesn't write or we can't determine the keys
func GetWriteKeys(cmd string, args []string) []string {
	if len(args) == 0 {
		return nil
	}

	switch cmd {
	// String commands
	case "SET", "SETEX", "SETNX", "GETSET", "INCR", "INCRBY", "INCRBYFLOAT", "DECR", "DECRBY", "APPEND":
		return []string{args[0]}
	case "MSET", "MSETNX":
		// MSET key1 val1 key2 val2 ...
		keys := make([]string, 0, len(args)/2)
		for i := 0; i < len(args); i += 2 {
			keys = append(keys, args[i])
		}
		return keys

	// List commands
	case "LPUSH", "RPUSH", "LPOP", "RPOP", "LSET", "LREM", "LTRIM", "LINSERT":
		return []string{args[0]}
	case "RPOPLPUSH", "LMOVE":
		if len(args) >= 2 {
			return []string{args[0], args[1]}
		}
		return []string{args[0]}

	// Hash commands
	case "HSET", "HSETNX", "HDEL", "HINCRBY", "HINCRBYFLOAT":
		return []string{args[0]}

	// Set commands
	case "SADD", "SREM", "SPOP", "SMOVE":
		return []string{args[0]}
	case "SUNIONSTORE", "SINTERSTORE", "SDIFFSTORE":
		return []string{args[0]} // destination key

	// Key commands
	case "DEL", "UNLINK":
		return args
	case "RENAME":
		if len(args) >= 2 {
			return []string{args[0], args[1]}
		}
		return []string{args[0]}
	case "EXPIRE", "EXPIREAT", "PEXPIRE", "PEXPIREAT", "PERSIST":
		return []string{args[0]}

	// FLUSHALL, FLUSHDB write to all keys - return nil to indicate special handling
	case "FLUSHALL", "FLUSHDB":
		return nil
	}

	return nil
}
