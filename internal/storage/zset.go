package storage

// ZSetMember represents a member in a sorted set with its score
type ZSetMember struct {
	Member string
	Score  float64
}

// ZSet represents a sorted set (combination of skip list + hash map)
type ZSet struct {
	dict     map[string]float64 // Member -> Score mapping for O(1) lookup
	skiplist *skipList          // Skip list for sorted operations
}

// NewZSet creates a new sorted set
func NewZSet() *ZSet {
	return &ZSet{
		dict:     make(map[string]float64),
		skiplist: newSkipList(),
	}
}

// Add adds or updates a member with the given score
// Returns true if new member was added, false if score was updated
func (z *ZSet) Add(member string, score float64) bool {
	// Check if member exists
	oldScore, exists := z.dict[member]
	if exists {
		if oldScore == score {
			// Same score, no change needed
			return false
		}
		// Remove old score from skip list
		z.skiplist.delete(member, oldScore)
	}

	// Add to dict
	z.dict[member] = score

	// Add to skip list
	wasInserted := z.skiplist.insert(member, score)

	return !exists || wasInserted
}

// Remove removes a member from the sorted set
// Returns true if member existed and was removed
func (z *ZSet) Remove(member string) bool {
	score, exists := z.dict[member]
	if !exists {
		return false
	}

	delete(z.dict, member)
	z.skiplist.delete(member, score)
	return true
}

// Score returns the score for a member, or nil if not found
func (z *ZSet) Score(member string) *float64 {
	if score, exists := z.dict[member]; exists {
		return &score
	}
	return nil
}

// Rank returns the 0-based rank of a member (ascending order)
// Returns -1 if member not found
func (z *ZSet) Rank(member string) int {
	score, exists := z.dict[member]
	if !exists {
		return -1
	}
	return z.skiplist.getRank(member, score)
}

// RevRank returns the 0-based rank of a member (descending order)
// Returns -1 if member not found
func (z *ZSet) RevRank(member string) int {
	rank := z.Rank(member)
	if rank == -1 {
		return -1
	}
	return z.Len() - rank - 1
}

// Len returns the number of members in the sorted set
func (z *ZSet) Len() int {
	return len(z.dict)
}

// Range returns members with scores in range [min, max]
// count = -1 means return all
func (z *ZSet) Range(min, max float64, offset, count int) []ZSetMember {
	return z.skiplist.getRange(min, max, offset, count, false)
}

// RevRange returns members with scores in range [min, max] in descending order
func (z *ZSet) RevRange(min, max float64, offset, count int) []ZSetMember {
	return z.skiplist.getRange(min, max, offset, count, true)
}

// RangeByRank returns members by rank range [start, stop] (0-based, inclusive)
func (z *ZSet) RangeByRank(start, stop int) []ZSetMember {
	return z.skiplist.getRangeByRank(start, stop, false)
}

// RevRangeByRank returns members by rank range in descending order
func (z *ZSet) RevRangeByRank(start, stop int) []ZSetMember {
	return z.skiplist.getRangeByRank(start, stop, true)
}

// IncrBy increments the score of a member by delta
// If member doesn't exist, creates it with score = delta
// Returns the new score
func (z *ZSet) IncrBy(member string, delta float64) float64 {
	oldScore, exists := z.dict[member]
	newScore := oldScore + delta

	if exists {
		z.skiplist.delete(member, oldScore)
	}

	z.dict[member] = newScore
	z.skiplist.insert(member, newScore)

	return newScore
}

// Count returns the number of members with scores in range [min, max]
func (z *ZSet) Count(min, max float64) int {
	members := z.skiplist.getRange(min, max, 0, -1, false)
	return len(members)
}

// PopMin removes and returns the member with the lowest score
// Returns nil if set is empty
func (z *ZSet) PopMin() *ZSetMember {
	if z.Len() == 0 {
		return nil
	}

	// Get first member from skip list
	first := z.skiplist.header.level[0]
	if first == nil {
		return nil
	}

	member := &ZSetMember{
		Member: first.member,
		Score:  first.score,
	}

	z.Remove(first.member)
	return member
}

// PopMax removes and returns the member with the highest score
// Returns nil if set is empty
func (z *ZSet) PopMax() *ZSetMember {
	if z.Len() == 0 {
		return nil
	}

	// Get last member from skip list
	last := z.skiplist.tail
	if last == nil {
		return nil
	}

	member := &ZSetMember{
		Member: last.member,
		Score:  last.score,
	}

	z.Remove(last.member)
	return member
}

// RemoveRangeByScore removes all members with scores in range [min, max]
// Returns the number of members removed
func (z *ZSet) RemoveRangeByScore(min, max float64) int {
	members := z.skiplist.getRange(min, max, 0, -1, false)
	count := 0

	for _, member := range members {
		if z.Remove(member.Member) {
			count++
		}
	}

	return count
}

// RemoveRangeByRank removes all members in rank range [start, stop] (0-based)
// Returns the number of members removed
func (z *ZSet) RemoveRangeByRank(start, stop int) int {
	members := z.skiplist.getRangeByRank(start, stop, false)
	count := 0

	for _, member := range members {
		if z.Remove(member.Member) {
			count++
		}
	}

	return count
}

// Clone creates a deep copy of the sorted set (for copy-on-write)
func (z *ZSet) Clone() *ZSet {
	newZSet := NewZSet()

	// Copy dict
	for member, score := range z.dict {
		newZSet.dict[member] = score
	}

	// Clone skip list
	newZSet.skiplist = z.skiplist.Clone()

	return newZSet
}

// GetAll returns all members with their scores
func (z *ZSet) GetAll() []ZSetMember {
	if z.Len() == 0 {
		return nil
	}
	return z.skiplist.getRangeByRank(0, z.Len()-1, false)
}
