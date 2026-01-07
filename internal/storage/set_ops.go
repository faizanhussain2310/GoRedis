package storage

import (
	"time"
)

// ==================== SET OPERATIONS ====================

// getOrCreateSet returns existing set or creates new one
func (s *Store) getOrCreateSet(key string) (*Set, bool) {
	val, exists := s.data[key]
	if !exists {
		return NewSet(), true // New set
	}

	// Check expiry
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return NewSet(), true // Expired, treat as new
	}

	// Check type first
	if val.Type != SetType {
		return nil, false // Type mismatch
	}

	// Check if existing value is a set
	if set, ok := val.Data.(*Set); ok {
		return set, true
	}
	return NewSet(), true
}

// getExistingSet returns existing set or nil if not found/not a set
func (s *Store) getExistingSet(key string) *Set {
	val, exists := s.data[key]
	if !exists {
		return nil
	}

	// Check expiry
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return nil
	}

	// Check type first
	if val.Type != SetType {
		return nil
	}

	set, ok := val.Data.(*Set)
	if !ok {
		return nil
	}
	return set
}

// saveSet stores a set in the data store
func (s *Store) saveSet(key string, set *Set) {
	if set.Len() == 0 {
		s.deleteKey(key)
		return
	}

	s.data[key] = &Value{
		Data:      set,
		ExpiresAt: nil,
		Type:      SetType,
	}
}

// SAdd adds members to a set
// Returns the number of elements added (not including existing members)
func (s *Store) SAdd(key string, members ...string) int {
	set, ok := s.getOrCreateSet(key)
	if !ok {
		return 0 // WRONGTYPE error would be returned by handler
	}

	// Copy-on-write: clone set if snapshot is active
	if s.isSnapshotActive() && s.data[key] != nil {
		set = set.Clone()
	}

	added := 0
	for _, member := range members {
		if set.Add(member) {
			added++
		}
	}

	s.saveSet(key, set)
	return added
}

// SRem removes members from a set
// Returns the number of elements removed
func (s *Store) SRem(key string, members ...string) int {
	set := s.getExistingSet(key)
	if set == nil {
		return 0
	}

	// Copy-on-write: clone set if snapshot is active
	if s.isSnapshotActive() {
		set = set.Clone()
	}

	removed := 0
	for _, member := range members {
		if set.Remove(member) {
			removed++
		}
	}

	// Save the set (will delete key if empty)
	s.saveSet(key, set)

	return removed
}

// SIsMember checks if member exists in the set
func (s *Store) SIsMember(key, member string) bool {
	set := s.getExistingSet(key)
	if set == nil {
		return false
	}
	return set.IsMember(member)
}

// SMembers returns all members of a set
func (s *Store) SMembers(key string) []string {
	set := s.getExistingSet(key)
	if set == nil {
		return []string{}
	}
	return set.GetMembers()
}

// SCard returns the cardinality (number of members) of a set
func (s *Store) SCard(key string) int {
	set := s.getExistingSet(key)
	if set == nil {
		return 0
	}
	return set.Len()
}

// SRandMember returns random members from a set without removing
// If count > 0, returns up to count distinct members
// If count < 0, returns abs(count) members (may include duplicates)
func (s *Store) SRandMember(key string, count int) []string {
	set := s.getExistingSet(key)
	if set == nil {
		return []string{}
	}

	if count == 0 {
		return []string{}
	}

	return set.RandomMembers(count)
}

// SPop removes and returns random members from a set
func (s *Store) SPop(key string, count int) []string {
	set := s.getExistingSet(key)
	if set == nil {
		return []string{}
	}

	result := make([]string, 0, count)
	for i := 0; i < count; i++ {
		member, ok := set.Pop()
		if !ok {
			break
		}
		result = append(result, member)
	}

	// Remove key if set is empty
	if set.Len() == 0 {
		s.deleteKey(key)
	}

	return result
}

// SUnion returns the union of all given sets
func (s *Store) SUnion(keys ...string) []string {
	if len(keys) == 0 {
		return []string{}
	}

	// Start with first set (or empty set if it doesn't exist)
	result := s.getExistingSet(keys[0])
	if result == nil {
		result = NewSet()
	}

	// Union with remaining sets
	for i := 1; i < len(keys); i++ {
		set := s.getExistingSet(keys[i])
		if set != nil {
			result = result.Union(set)
		}
	}

	return result.GetMembers()
}

// SInter returns the intersection of all given sets
func (s *Store) SInter(keys ...string) []string {
	if len(keys) == 0 {
		return []string{}
	}

	// Start with first set
	result := s.getExistingSet(keys[0])
	if result == nil {
		return []string{} // If first set doesn't exist, intersection is empty
	}

	// Intersect with remaining sets
	for i := 1; i < len(keys); i++ {
		set := s.getExistingSet(keys[i])
		if set == nil {
			return []string{} // Empty intersection
		}
		result = result.Intersect(set)
		if result.Len() == 0 {
			return []string{}
		}
	}

	return result.GetMembers()
}

// SDiff returns the difference between the first set and all subsequent sets
func (s *Store) SDiff(keys ...string) []string {
	if len(keys) == 0 {
		return []string{}
	}

	// Start with first set
	result := s.getExistingSet(keys[0])
	if result == nil {
		return []string{}
	}

	// Subtract members from remaining sets
	for i := 1; i < len(keys); i++ {
		set := s.getExistingSet(keys[i])
		if set != nil {
			result = result.Diff(set)
		}
	}

	return result.GetMembers()
}

// SMove moves a member from source set to destination set
// Returns true if the element was moved, false if it didn't exist in source
func (s *Store) SMove(srcKey, destKey, member string) bool {
	srcSet := s.getExistingSet(srcKey)
	if srcSet == nil {
		return false
	}

	if !srcSet.IsMember(member) {
		return false
	}

	// Get or create destination set
	destSet, ok := s.getOrCreateSet(destKey)
	if !ok {
		return false // Type mismatch on destination
	}

	// Move the member
	srcSet.Remove(member)
	destSet.Add(member)

	// Clean up empty source set
	if srcSet.Len() == 0 {
		s.deleteKey(srcKey)
	}

	// Save destination set
	s.saveSet(destKey, destSet)

	return true
}

// SUnionStore stores the union of sets in destination, returns cardinality
func (s *Store) SUnionStore(destKey string, keys ...string) int {
	result := s.SUnion(keys...)

	if len(result) == 0 {
		s.deleteKey(destKey)
		return 0
	}

	newSet := NewSet()
	for _, member := range result {
		newSet.Add(member)
	}
	s.saveSet(destKey, newSet)

	return len(result)
}

// SInterStore stores the intersection of sets in destination, returns cardinality
func (s *Store) SInterStore(destKey string, keys ...string) int {
	result := s.SInter(keys...)

	if len(result) == 0 {
		s.deleteKey(destKey)
		return 0
	}

	newSet := NewSet()
	for _, member := range result {
		newSet.Add(member)
	}
	s.saveSet(destKey, newSet)

	return len(result)
}

// SDiffStore stores the difference of sets in destination, returns cardinality
func (s *Store) SDiffStore(destKey string, keys ...string) int {
	result := s.SDiff(keys...)

	if len(result) == 0 {
		s.deleteKey(destKey)
		return 0
	}

	newSet := NewSet()
	for _, member := range result {
		newSet.Add(member)
	}
	s.saveSet(destKey, newSet)

	return len(result)
}

// Type check for sets
func (s *Store) isSet(key string) (bool, error) {
	val, exists := s.data[key]
	if !exists {
		return false, nil // Key doesn't exist, not an error
	}

	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return false, nil
	}

	_, ok := val.Data.(*Set)
	if !ok {
		return false, ErrWrongType
	}
	return true, nil
}
