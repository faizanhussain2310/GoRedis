package handler

import (
	"container/list"
	"sync"
	"time"
)

// BlockingDirection specifies which end of the list to pop from
type BlockingDirection int

const (
	BlockLeft BlockingDirection = iota
	BlockRight
)

// BlockedClient represents a client waiting for data on a list
type BlockedClient struct {
	ClientID   int64
	Keys       []string            // Keys being watched (in priority order)
	Direction  BlockingDirection   // LEFT or RIGHT pop
	Timeout    time.Duration       // How long to wait (0 = forever)
	StartTime  time.Time           // When blocking started
	ResponseCh chan BlockingResult // Channel to send result

	// For BLMOVE operation
	DestKey string            // Destination key (empty for BLPOP/BRPOP)
	DestDir BlockingDirection // Direction for destination (BLMOVE)

	// Redis-style: store list.Element pointers for O(1) removal
	// Maps key → position in that key's blocked client list
	listNodes map[string]*list.Element
}

// BlockingResult is sent back to the blocked client
type BlockingResult struct {
	Key   string // The key that had data
	Value string // The popped value
	Err   error  // Error if any (timeout, etc.)
}

// BlockingManager manages blocked clients waiting for list data
// Uses Redis-style architecture with doubly-linked lists for O(1) removal
type BlockingManager struct {
	mu sync.Mutex

	// Reverse index: key → doubly-linked list of blocked clients (FIFO order)
	// Using list.List for O(1) removal when we have the Element pointer
	// Similar to Redis's adlist (doubly-linked list)
	keyBlockedClients map[string]*list.List

	// Forward index: clientID → BlockedClient (for cleanup on disconnect)
	clientBlocked map[int64]*BlockedClient
}

// NewBlockingManager creates a new blocking manager
func NewBlockingManager() *BlockingManager {
	bm := &BlockingManager{
		keyBlockedClients: make(map[string]*list.List),
		clientBlocked:     make(map[int64]*BlockedClient),
	}
	return bm
}

// BlockClient registers a client as blocked on the given keys
// Returns a channel that will receive the result
func (bm *BlockingManager) BlockClient(
	clientID int64,
	keys []string,
	direction BlockingDirection,
	timeout time.Duration,
	destKey string,
	destDir BlockingDirection,
) <-chan BlockingResult {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Create blocked client with listNodes map for O(1) removal
	bc := &BlockedClient{
		ClientID:   clientID,
		Keys:       keys,
		Direction:  direction,
		Timeout:    timeout,
		StartTime:  time.Now(),
		ResponseCh: make(chan BlockingResult, 1),
		DestKey:    destKey,
		DestDir:    destDir,
		listNodes:  make(map[string]*list.Element),
	}

	// Add to forward index
	bm.clientBlocked[clientID] = bc

	// Add to reverse index for each key (using doubly-linked list)
	for _, key := range keys {
		// Create list for this key if it doesn't exist
		if bm.keyBlockedClients[key] == nil {
			bm.keyBlockedClients[key] = list.New()
		}
		// Add to back of list (FIFO) and store the Element pointer for O(1) removal
		elem := bm.keyBlockedClients[key].PushBack(bc)
		bc.listNodes[key] = elem
	}

	// Start timeout goroutine if timeout is specified
	if timeout > 0 {
		go bm.handleTimeout(bc)
	}

	return bc.ResponseCh
}

// handleTimeout handles the timeout for a blocked client
func (bm *BlockingManager) handleTimeout(bc *BlockedClient) {
	timer := time.NewTimer(bc.Timeout)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Timeout reached - send timeout result
		bm.mu.Lock()
		defer bm.mu.Unlock()

		// Check if client is still blocked (might have been served already)
		if _, exists := bm.clientBlocked[bc.ClientID]; !exists {
			return // Already served or disconnected
		}

		// Remove from all data structures
		bm.removeBlockedClientLocked(bc)

		// Send timeout result (nil response in Redis)
		select {
		case bc.ResponseCh <- BlockingResult{Err: ErrBlockingTimeout}:
		default:
		}
		close(bc.ResponseCh)

	case <-bc.ResponseCh:
		// Client was served before timeout
		return
	}
}

// UnblockClientWithData attempts to unblock clients waiting on the given key
// Called when data is pushed to a list
// Returns true if a client was unblocked (data was consumed)
func (bm *BlockingManager) UnblockClientWithData(key string, popFunc func(direction BlockingDirection) (string, bool), pushFunc func(destKey string, value string, direction BlockingDirection)) bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	blockedList, exists := bm.keyBlockedClients[key]
	if !exists || blockedList.Len() == 0 {
		return false // No one waiting
	}

	// Get the first waiting client (FIFO) - O(1)
	elem := blockedList.Front()
	bc := elem.Value.(*BlockedClient)

	// Try to pop the value
	value, ok := popFunc(bc.Direction)
	if !ok {
		return false // No data (shouldn't happen if called correctly)
	}

	// If this is a BLMOVE, push to destination
	if bc.DestKey != "" {
		pushFunc(bc.DestKey, value, bc.DestDir)
	}

	// Remove client from all data structures - O(1) per key!
	bm.removeBlockedClientLocked(bc)

	// Send result to client
	select {
	case bc.ResponseCh <- BlockingResult{Key: key, Value: value}:
	default:
	}
	close(bc.ResponseCh)

	return true
}

// removeBlockedClientLocked removes a blocked client from all data structures
// Must be called with lock held
// Uses O(1) removal via stored list.Element pointers (Redis-style)
func (bm *BlockingManager) removeBlockedClientLocked(bc *BlockedClient) {
	// Remove from forward index - O(1)
	delete(bm.clientBlocked, bc.ClientID)

	// Remove from reverse index for each key - O(1) per key!
	// This is the Redis optimization: we stored the list.Element pointer
	// when we added the client, so removal is O(1) instead of O(N)
	for key, elem := range bc.listNodes {
		keyList := bm.keyBlockedClients[key]
		if keyList != nil {
			keyList.Remove(elem) // O(1) doubly-linked list removal!

			// Clean up empty entries
			if keyList.Len() == 0 {
				delete(bm.keyBlockedClients, key)
			}
		}
	}

	// Clear the listNodes map
	bc.listNodes = nil
}

// RemoveClient removes a client from blocking (on disconnect)
func (bm *BlockingManager) RemoveClient(clientID int64) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bc, exists := bm.clientBlocked[clientID]
	if !exists {
		return
	}

	bm.removeBlockedClientLocked(bc)
	close(bc.ResponseCh)
}

// HasBlockedClients checks if any clients are blocked on the given key
func (bm *BlockingManager) HasBlockedClients(key string) bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	keyList, exists := bm.keyBlockedClients[key]
	return exists && keyList.Len() > 0
}

// GetBlockedClientCount returns the number of clients blocked on a key
func (bm *BlockingManager) GetBlockedClientCount(key string) int {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	keyList := bm.keyBlockedClients[key]
	if keyList == nil {
		return 0
	}
	return keyList.Len()
}

// Error for blocking timeout
var ErrBlockingTimeout = &BlockingTimeoutError{}

type BlockingTimeoutError struct{}

func (e *BlockingTimeoutError) Error() string {
	return "blocking operation timeout"
}
