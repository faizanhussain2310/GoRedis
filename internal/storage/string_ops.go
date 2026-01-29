package storage

import (
	"fmt"
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

// Incr increments the integer value of a key by 1
// Returns the value after increment or error if value is not an integer
func (s *Store) Incr(key string) (int64, error) {
	return s.IncrBy(key, 1)
}

// IncrBy increments the integer value of a key by the given amount
// Returns the value after increment or error if value is not an integer
func (s *Store) IncrBy(key string, increment int64) (int64, error) {
	val, exists := s.data[key]

	// Check expiration if key exists
	if exists && val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		exists = false
	}

	var current int64
	if exists {
		// Try to parse existing value as integer
		switch v := val.Data.(type) {
		case string:
			parsed, err := parseInt64(v)
			if err != nil {
				return 0, err
			}
			current = parsed
		case int64:
			current = v
		case int:
			current = int64(v)
		default:
			return 0, fmt.Errorf("value is not an integer or out of range")
		}
	}

	// Perform increment
	newValue := current + increment

	// Store as string to match Redis behavior
	s.data[key] = &Value{
		Data:      fmt.Sprintf("%d", newValue),
		ExpiresAt: nil,
		Type:      StringType,
	}

	return newValue, nil
}

// Decr decrements the integer value of a key by 1
// Returns the value after decrement or error if value is not an integer
func (s *Store) Decr(key string) (int64, error) {
	return s.IncrBy(key, -1)
}

// DecrBy decrements the integer value of a key by the given amount
// Returns the value after decrement or error if value is not an integer
func (s *Store) DecrBy(key string, decrement int64) (int64, error) {
	return s.IncrBy(key, -decrement)
}

// parseInt64 parses a string to int64, matching Redis behavior
func parseInt64(s string) (int64, error) {
	var result int64
	_, err := fmt.Sscanf(s, "%d", &result)
	if err != nil {
		return 0, fmt.Errorf("value is not an integer or out of range")
	}
	return result, nil
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
