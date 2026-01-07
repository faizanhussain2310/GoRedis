package storage

import (
	"time"
)

// Set stores a string value with optional expiry
func (s *Store) Set(key string, value interface{}, expiry *time.Time) {
	s.data[key] = &Value{
		Data:      value,
		ExpiresAt: expiry,
		Type:      StringType,
	}

	if expiry != nil {
		s.dataWithExpiry[key] = *expiry
	} else {
		delete(s.dataWithExpiry, key)
	}
}

// Get retrieves a value by key
func (s *Store) Get(key string) (interface{}, bool) {
	val, exists := s.data[key]
	if !exists {
		return nil, false
	}

	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return nil, false
	}

	return val.Data, true
}

// Delete removes a key from the store
func (s *Store) Delete(key string) bool {
	_, exists := s.data[key]
	if exists {
		s.deleteKey(key)
		return true
	}
	return false
}

// Exists checks if a key exists and is not expired
func (s *Store) Exists(key string) bool {
	val, exists := s.data[key]
	if !exists {
		return false
	}

	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return false
	}

	return true
}

// Keys returns all non-expired keys
func (s *Store) Keys() []string {
	keys := make([]string, 0, len(s.data))
	now := time.Now()

	for key, val := range s.data {
		if val.ExpiresAt == nil || now.Before(*val.ExpiresAt) {
			keys = append(keys, key)
		}
	}

	return keys
}

// Flush clears all data from the store
func (s *Store) Flush() {
	s.data = make(map[string]*Value)
	s.dataWithExpiry = make(map[string]time.Time)
}

// Expire sets an expiry time on a key
func (s *Store) Expire(key string, expiry *time.Time) bool {
	val, exists := s.data[key]
	if !exists {
		return false
	}

	// Check if already expired
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return false
	}

	val.ExpiresAt = expiry
	if expiry != nil {
		s.dataWithExpiry[key] = *expiry
	} else {
		delete(s.dataWithExpiry, key)
	}
	return true
}

// TTL returns the time-to-live for a key in seconds
// Returns -2 if key doesn't exist, -1 if key has no expiry
func (s *Store) TTL(key string) int64 {
	val, exists := s.data[key]
	if !exists {
		return -2 // Key doesn't exist
	}

	// Check if already expired
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return -2 // Key doesn't exist (expired)
	}

	if val.ExpiresAt == nil {
		return -1 // Key exists but has no expiry
	}

	// Return seconds until expiry
	ttl := time.Until(*val.ExpiresAt).Seconds()
	if ttl < 0 {
		s.deleteKey(key)
		return -2 // Already expired
	}
	return int64(ttl)
}

// CleanupExpiredKeys performs active expiration using random sampling
func (s *Store) CleanupExpiredKeys() {
	const maxCleanupTime = 1 * time.Millisecond
	const keysPerSample = 20

	startTime := time.Now()

	// Loop until time budget exhausted
	for time.Since(startTime) < maxCleanupTime {
		// Sample random keys from dataWithExpiry
		sampledKeys := s.getRandomKeysWithExpiry(keysPerSample)

		if len(sampledKeys) == 0 {
			break // No keys with expiry
		}

		expiredInSample := 0
		now := time.Now()

		// Check each sampled key
		for _, key := range sampledKeys {
			val, exists := s.data[key]

			if !exists {
				// Consistency: key in expiry index but not in data
				delete(s.dataWithExpiry, key)
				continue
			}

			// Check if expired
			if val.ExpiresAt != nil && now.After(*val.ExpiresAt) {
				s.deleteKey(key)
				expiredInSample++
			}
		}

		// Exit early if sample was small
		if len(sampledKeys) < keysPerSample {
			break
		}

		// Exit if less than 25% expired (soft hint)
		if expiredInSample*4 < keysPerSample {
			break
		}
	}
}

// getRandomKeysWithExpiry samples random keys that have expiry set
func (s *Store) getRandomKeysWithExpiry(count int) []string {
	keys := make([]string, 0, count)

	// Sample random keys from dataWithExpiry
	for key := range s.dataWithExpiry {
		keys = append(keys, key)
		if len(keys) >= count {
			break
		}
	}

	return keys
}
