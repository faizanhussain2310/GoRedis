package storage

import (
	"strconv"
	"time"
)

// ==================== HASH OPERATIONS ====================

// getOrCreateHash returns existing hash or creates new one
func (s *Store) getOrCreateHash(key string) (*Hash, bool) {
	val, exists := s.data[key]
	if !exists {
		return NewHash(), true // New hash
	}

	// Check expiry
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return NewHash(), true // Expired, treat as new
	}

	// Check type
	if val.Type != HashType {
		return nil, false // Wrong type
	}

	// Check if existing value is a hash
	if hash, ok := val.Data.(*Hash); ok {
		return hash, true
	}
	return NewHash(), true
}

// getExistingHash returns existing hash or nil
func (s *Store) getExistingHash(key string) (*Hash, error) {
	val, exists := s.data[key]
	if !exists {
		return nil, nil // Key doesn't exist
	}

	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return nil, nil
	}

	if val.Type != HashType {
		return nil, ErrWrongType
	}

	if hash, ok := val.Data.(*Hash); ok {
		return hash, nil
	}
	return nil, nil
}

// saveHash saves the hash to storage
func (s *Store) saveHash(key string, hash *Hash) {
	if hash.Len() == 0 {
		s.deleteKey(key)
		return
	}

	s.data[key] = &Value{
		Data:      hash,
		ExpiresAt: nil,
		Type:      HashType,
	}
}

// HSet sets field(s) in hash, returns number of new fields added
func (s *Store) HSet(key string, fieldValues ...string) (int, error) {
	if len(fieldValues)%2 != 0 {
		return 0, ErrWrongNumArgs
	}

	hash, ok := s.getOrCreateHash(key)
	if !ok {
		return 0, ErrWrongType
	}

	// Copy-on-write: clone hash if snapshot is active
	if s.isSnapshotActive() && s.data[key] != nil {
		hash = hash.Clone()
	}

	newFields := 0
	for i := 0; i < len(fieldValues); i += 2 {
		if hash.Set(fieldValues[i], fieldValues[i+1]) {
			newFields++
		}
	}

	s.saveHash(key, hash)
	return newFields, nil
}

// HGet returns the value of a field
func (s *Store) HGet(key, field string) (string, bool, error) {
	hash, err := s.getExistingHash(key)
	if err != nil {
		return "", false, err
	}
	if hash == nil {
		return "", false, nil
	}

	val, exists := hash.Get(field)
	return val, exists, nil
}

// HMGet returns values of multiple fields
func (s *Store) HMGet(key string, fields ...string) ([]interface{}, error) {
	hash, err := s.getExistingHash(key)
	if err != nil {
		return nil, err
	}

	result := make([]interface{}, len(fields))
	for i, field := range fields {
		if hash != nil {
			if val, exists := hash.Get(field); exists {
				result[i] = val
			} else {
				result[i] = nil
			}
		} else {
			result[i] = nil
		}
	}
	return result, nil
}

// HDel deletes field(s) from hash, returns number of deleted fields
func (s *Store) HDel(key string, fields ...string) (int, error) {
	hash, err := s.getExistingHash(key)
	if err != nil {
		return 0, err
	}
	if hash == nil {
		return 0, nil
	}

	// Copy-on-write: clone hash if snapshot is active
	if s.isSnapshotActive() {
		hash = hash.Clone()
	}

	deleted := 0
	for _, field := range fields {
		if hash.Delete(field) {
			deleted++
		}
	}

	s.saveHash(key, hash)
	return deleted, nil
}

// HExists checks if field exists in hash
func (s *Store) HExists(key, field string) (bool, error) {
	hash, err := s.getExistingHash(key)
	if err != nil {
		return false, err
	}
	if hash == nil {
		return false, nil
	}
	return hash.Exists(field), nil
}

// HLen returns number of fields in hash
func (s *Store) HLen(key string) (int, error) {
	hash, err := s.getExistingHash(key)
	if err != nil {
		return 0, err
	}
	if hash == nil {
		return 0, nil
	}
	return hash.Len(), nil
}

// HKeys returns all field names in hash
func (s *Store) HKeys(key string) ([]string, error) {
	hash, err := s.getExistingHash(key)
	if err != nil {
		return nil, err
	}
	if hash == nil {
		return []string{}, nil
	}
	return hash.Keys(), nil
}

// HVals returns all values in hash
func (s *Store) HVals(key string) ([]string, error) {
	hash, err := s.getExistingHash(key)
	if err != nil {
		return nil, err
	}
	if hash == nil {
		return []string{}, nil
	}
	return hash.Values(), nil
}

// HGetAll returns all fields and values
func (s *Store) HGetAll(key string) ([]string, error) {
	hash, err := s.getExistingHash(key)
	if err != nil {
		return nil, err
	}
	if hash == nil {
		return []string{}, nil
	}
	return hash.GetAll(), nil
}

// HSetNX sets field only if it doesn't exist
func (s *Store) HSetNX(key, field, value string) (bool, error) {
	hash, ok := s.getOrCreateHash(key)
	if !ok {
		return false, ErrWrongType
	}

	result := hash.SetNX(field, value)
	if result {
		s.saveHash(key, hash)
	}
	return result, nil
}

// HIncrBy increments integer value of field
func (s *Store) HIncrBy(key, field string, increment int64) (int64, error) {
	hash, ok := s.getOrCreateHash(key)
	if !ok {
		return 0, ErrWrongType
	}

	var current int64 = 0
	if val, exists := hash.Get(field); exists {
		parsed, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return 0, ErrHashValueNotInteger
		}
		current = parsed
	}

	newVal := current + increment
	hash.Set(field, strconv.FormatInt(newVal, 10))
	s.saveHash(key, hash)
	return newVal, nil
}

// HIncrByFloat increments float value of field
func (s *Store) HIncrByFloat(key, field string, increment float64) (float64, error) {
	hash, ok := s.getOrCreateHash(key)
	if !ok {
		return 0, ErrWrongType
	}

	var current float64 = 0
	if val, exists := hash.Get(field); exists {
		parsed, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, ErrHashValueNotFloat
		}
		current = parsed
	}

	newVal := current + increment
	hash.Set(field, strconv.FormatFloat(newVal, 'f', -1, 64))
	s.saveHash(key, hash)
	return newVal, nil
}
