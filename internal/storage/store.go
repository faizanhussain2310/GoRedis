package storage

import (
	"redis/internal/cluster"
	"sync/atomic"
	"time"
)

type Store struct {
	data           map[string]*Value
	dataWithExpiry map[string]time.Time
	snapshotCount  int32            // Atomic counter for active snapshots (COW optimization)
	PubSub         *PubSub          // Publish/Subscribe manager
	Cluster        *cluster.Cluster // Cluster manager (nil if cluster mode disabled)
}

type Value struct {
	Data      interface{}
	ExpiresAt *time.Time
	Type      ValueType
}

type ValueType int

const (
	StringType ValueType = iota
	ListType
	SetType
	HashType
	ZSetType
	BloomFilterType
	HyperLogLogType
)

func NewStore() *Store {
	return &Store{
		data:           make(map[string]*Value),
		dataWithExpiry: make(map[string]time.Time),
		PubSub:         NewPubSub(),
	}
}

// deleteKey is a helper to delete from both maps
func (s *Store) deleteKey(key string) {
	delete(s.data, key)
	delete(s.dataWithExpiry, key)
}

// GetAllData returns a SHALLOW COPY of all data for snapshot purposes
// Uses copy-on-write (COW) optimization: clones Value structs but copies data pointers,
// actual data is copied only when modified during an active snapshot.
// Caller MUST call ReleaseSnapshot() when done to decrement reference count.
func (s *Store) GetAllData() map[string]*Value {
	// Increment snapshot counter atomically
	atomic.AddInt32(&s.snapshotCount, 1)

	// Shallow copy - clone Value structs, copy data pointers
	snapshot := make(map[string]*Value, len(s.data))
	for key, value := range s.data {
		// Clone the Value struct (not just copy pointer)
		snapshot[key] = &Value{
			Data:      value.Data,                   // Shallow copy data pointer
			ExpiresAt: copyTimePtr(value.ExpiresAt), // Deep copy time
			Type:      value.Type,
		}
	}

	return snapshot
}

// copyTimePtr creates a deep copy of a time pointer
func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	copied := *t
	return &copied
}

// ReleaseSnapshot decrements the snapshot reference counter
// MUST be called after snapshot operations complete (AOF rewrite, BGSAVE)
func (s *Store) ReleaseSnapshot() {
	atomic.AddInt32(&s.snapshotCount, -1)
}

// isSnapshotActive checks if any snapshot is currently active
// Used by write operations to determine if copy-on-write is needed
func (s *Store) isSnapshotActive() bool {
	return atomic.LoadInt32(&s.snapshotCount) > 0
}
