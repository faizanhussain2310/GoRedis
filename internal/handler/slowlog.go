package handler

import (
	"log"
	"sync"
	"time"
)

// SlowLogEntry represents a slow command entry
type SlowLogEntry struct {
	ID        int64
	Timestamp time.Time
	Duration  time.Duration
	ClientID  int64
	Command   string
	Args      []string
}

// SlowLog tracks slow commands like Redis SLOWLOG
type SlowLog struct {
	mu        sync.RWMutex
	entries   []SlowLogEntry
	maxLen    int
	threshold time.Duration
	idCounter int64
}

// NewSlowLog creates a new slow log with given max entries and threshold
func NewSlowLog(maxLen int, threshold time.Duration) *SlowLog {
	return &SlowLog{
		entries:   make([]SlowLogEntry, 0, maxLen),
		maxLen:    maxLen,
		threshold: threshold,
	}
}

// LogIfSlow logs a command if it exceeds the threshold
// Returns true if the command was slow
func (s *SlowLog) LogIfSlow(clientID int64, command string, args []string, duration time.Duration) bool {
	if duration < s.threshold {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.idCounter++
	entry := SlowLogEntry{
		ID:        s.idCounter,
		Timestamp: time.Now(),
		Duration:  duration,
		ClientID:  clientID,
		Command:   command,
		Args:      args,
	}

	// Add to front (newest first)
	s.entries = append([]SlowLogEntry{entry}, s.entries...)

	// Trim to max length
	if len(s.entries) > s.maxLen {
		s.entries = s.entries[:s.maxLen]
	}

	log.Printf("[SLOWLOG] Client %d: %s took %v", clientID, command, duration)
	return true
}

// Get returns the last n slow log entries
func (s *SlowLog) Get(count int) []SlowLogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if count <= 0 || count > len(s.entries) {
		count = len(s.entries)
	}

	result := make([]SlowLogEntry, count)
	copy(result, s.entries[:count])
	return result
}

// Len returns the current number of entries
func (s *SlowLog) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Reset clears all slow log entries
func (s *SlowLog) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = s.entries[:0]
}

// SetThreshold updates the slow log threshold
func (s *SlowLog) SetThreshold(threshold time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threshold = threshold
}

// GetThreshold returns the current threshold
func (s *SlowLog) GetThreshold() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.threshold
}
