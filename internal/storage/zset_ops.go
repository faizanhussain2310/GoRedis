package storage

import (
	"time"
)

// ==================== SORTED SET HELPER FUNCTIONS ====================

// getOrCreateZSet returns existing sorted set or creates new one
func (s *Store) getOrCreateZSet(key string) (*ZSet, bool) {
	val, exists := s.data[key]
	if !exists {
		return NewZSet(), true // New sorted set
	}

	// Check expiry
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return NewZSet(), true // Expired, treat as new
	}

	// Check type
	if val.Type != ZSetType {
		return nil, false // Wrong type
	}

	// Check if existing value is a sorted set
	if zset, ok := val.Data.(*ZSet); ok {
		return zset, true
	}
	return NewZSet(), true
}

// getExistingZSet returns existing sorted set or nil
func (s *Store) getExistingZSet(key string) (*ZSet, error) {
	val, exists := s.data[key]
	if !exists {
		return nil, nil // Key doesn't exist
	}

	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return nil, nil
	}

	if val.Type != ZSetType {
		return nil, ErrWrongType
	}

	if zset, ok := val.Data.(*ZSet); ok {
		return zset, nil
	}
	return nil, nil
}

// saveZSet saves the sorted set to storage
func (s *Store) saveZSet(key string, zset *ZSet) {
	if zset.Len() == 0 {
		s.deleteKey(key)
		return
	}

	s.data[key] = &Value{
		Data:      zset,
		ExpiresAt: nil,
		Type:      ZSetType,
	}
}

// ==================== SORTED SET OPERATIONS ====================

// ZAdd adds one or more members with scores to a sorted set
// Updates score if member already exists
// Returns the number of elements added (not updated)
func (s *Store) ZAdd(key string, members []ZSetMember) int {
	zset, ok := s.getOrCreateZSet(key)
	if !ok {
		return -1 // Type error
	}

	// Copy-on-write: clone zset if snapshot is active
	if s.isSnapshotActive() && s.data[key] != nil {
		zset = zset.Clone()
	}

	// Add all members
	added := 0
	for _, member := range members {
		if zset.Add(member.Member, member.Score) {
			added++
		}
	}

	s.saveZSet(key, zset)
	return added
}

// ZRem removes one or more members from a sorted set
// Returns the number of members removed
func (s *Store) ZRem(key string, members []string) int {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return -1 // Type error
	}
	if zset == nil {
		return 0
	}

	// Copy-on-write: clone zset if snapshot is active
	if s.isSnapshotActive() {
		zset = zset.Clone()
	}

	// Remove all members
	removed := 0
	for _, member := range members {
		if zset.Remove(member) {
			removed++
		}
	}

	s.saveZSet(key, zset)
	return removed
}

// ZScore returns the score of a member in a sorted set
func (s *Store) ZScore(key string, member string) *float64 {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return nil
	}
	if zset == nil {
		return nil
	}
	return zset.Score(member)
}

// ZRank returns the rank of a member in a sorted set (ascending order, 0-based)
func (s *Store) ZRank(key string, member string) int {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return -1
	}
	if zset == nil {
		return -1
	}
	return zset.Rank(member)
}

// ZRevRank returns the rank of a member in a sorted set (descending order, 0-based)
func (s *Store) ZRevRank(key string, member string) int {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return -1
	}
	if zset == nil {
		return -1
	}
	return zset.RevRank(member)
}

// ZCard returns the number of members in a sorted set
func (s *Store) ZCard(key string) int {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return 0
	}
	if zset == nil {
		return 0
	}
	return zset.Len()
}

// ZRange returns members in a sorted set by rank range [start, stop]
// withScores determines if scores should be included
func (s *Store) ZRange(key string, start, stop int, withScores bool) []ZSetMember {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return nil
	}
	if zset == nil {
		return nil
	}

	// Handle negative indices (from end)
	length := zset.Len()
	if start < 0 {
		start = length + start
	}
	if stop < 0 {
		stop = length + stop
	}

	// Clamp to valid range
	if start < 0 {
		start = 0
	}
	if stop >= length {
		stop = length - 1
	}

	if start > stop {
		return nil
	}

	return zset.RangeByRank(start, stop)
}

// ZRevRange returns members in a sorted set by rank range in descending order
func (s *Store) ZRevRange(key string, start, stop int, withScores bool) []ZSetMember {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return nil
	}
	if zset == nil {
		return nil
	}

	// Handle negative indices
	length := zset.Len()
	if start < 0 {
		start = length + start
	}
	if stop < 0 {
		stop = length + stop
	}

	// Clamp to valid range
	if start < 0 {
		start = 0
	}
	if stop >= length {
		stop = length - 1
	}

	if start > stop {
		return nil
	}

	return zset.RevRangeByRank(start, stop)
}

// ZRangeByScore returns members with scores in range [min, max]
func (s *Store) ZRangeByScore(key string, min, max float64, offset, count int) []ZSetMember {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return nil
	}
	if zset == nil {
		return nil
	}
	return zset.Range(min, max, offset, count)
}

// ZRevRangeByScore returns members with scores in range [min, max] in descending order
func (s *Store) ZRevRangeByScore(key string, min, max float64, offset, count int) []ZSetMember {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return nil
	}
	if zset == nil {
		return nil
	}
	return zset.RevRange(min, max, offset, count)
}

// ZIncrBy increments the score of a member by delta
func (s *Store) ZIncrBy(key string, delta float64, member string) (float64, error) {
	zset, ok := s.getOrCreateZSet(key)
	if !ok {
		return 0, ErrWrongType
	}

	// Copy-on-write: clone zset if snapshot is active
	if s.isSnapshotActive() && s.data[key] != nil {
		zset = zset.Clone()
	}

	newScore := zset.IncrBy(member, delta)
	s.saveZSet(key, zset)
	return newScore, nil
}

// ZCount returns the number of members with scores in range [min, max]
func (s *Store) ZCount(key string, min, max float64) int {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return 0
	}
	if zset == nil {
		return 0
	}
	return zset.Count(min, max)
}

// ZPopMin removes and returns the member with the lowest score
func (s *Store) ZPopMin(key string) *ZSetMember {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return nil
	}
	if zset == nil {
		return nil
	}

	// Copy-on-write: clone zset if snapshot is active
	if s.isSnapshotActive() {
		zset = zset.Clone()
	}

	member := zset.PopMin()
	s.saveZSet(key, zset)
	return member
}

// ZPopMax removes and returns the member with the highest score
func (s *Store) ZPopMax(key string) *ZSetMember {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return nil
	}
	if zset == nil {
		return nil
	}

	// Copy-on-write: clone zset if snapshot is active
	if s.isSnapshotActive() {
		zset = zset.Clone()
	}

	member := zset.PopMax()
	s.saveZSet(key, zset)
	return member
}

// ZRemRangeByScore removes all members with scores in range [min, max]
func (s *Store) ZRemRangeByScore(key string, min, max float64) int {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return 0
	}
	if zset == nil {
		return 0
	}

	// Copy-on-write: clone zset if snapshot is active
	if s.isSnapshotActive() {
		zset = zset.Clone()
	}

	removed := zset.RemoveRangeByScore(min, max)
	s.saveZSet(key, zset)
	return removed
}

// ZRemRangeByRank removes all members in rank range [start, stop] (0-based)
func (s *Store) ZRemRangeByRank(key string, start, stop int) int {
	zset, err := s.getExistingZSet(key)
	if err != nil {
		return 0
	}
	if zset == nil {
		return 0
	}

	// Handle negative indices
	length := zset.Len()
	if start < 0 {
		start = length + start
	}
	if stop < 0 {
		stop = length + stop
	}

	// Clamp to valid range
	if start < 0 {
		start = 0
	}
	if stop >= length {
		stop = length - 1
	}

	if start > stop {
		return 0
	}

	// Copy-on-write: clone zset if snapshot is active
	if s.isSnapshotActive() {
		zset = zset.Clone()
	}

	removed := zset.RemoveRangeByRank(start, stop)
	s.saveZSet(key, zset)
	return removed
}
