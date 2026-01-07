package storage

import (
	"math"
	"math/rand"
)

// Skip List Implementation for Sorted Sets
// Redis uses skip lists for sorted sets because they provide:
// - O(log n) insertion, deletion, and search
// - O(1) access to min/max elements
// - Efficient range queries
// - Simple implementation compared to balanced trees
// - Better cache locality than trees

const (
	maxLevel    = 32   // Maximum level for skip list nodes
	probability = 0.25 // P = 1/4 for level generation (same as Redis)
)

// skipListNode represents a node in the skip list
type skipListNode struct {
	member string          // The member (key in the sorted set)
	score  float64         // The score for sorting
	level  []*skipListNode // Forward pointers for each level
	span   []int           // number of nodes from current node to next node at each level (for rank calculation)
}
// important: span[i] = (rank of next node at level i) - (rank of current node)
// important: span[i] = number of nodes in level 0 between current node and next node at level i

// skipList is a probabilistic data structure for fast sorted access
type skipList struct {
	header *skipListNode // Sentinel node (does not contain data)
	tail   *skipListNode // Last node (for O(1) access to max)
	length int           // Number of elements
	level  int           // Current maximum level
}

// newSkipList creates a new skip list
func newSkipList() *skipList {
	// Create header node with maximum level
	header := &skipListNode{
		member: "", // Empty sentinel
		score:  math.Inf(-1),
		level:  make([]*skipListNode, maxLevel),
		span:   make([]int, maxLevel),
	}

	return &skipList{
		header: header,
		tail:   nil,
		length: 0,
		level:  1,
	}
}

// randomLevel generates a random level for a new node
// Uses geometric distribution: P(level = k) = p^(k-1) * (1-p)
// Expected level = 1/(1-p) = 1.33 for p=0.25
func (sl *skipList) randomLevel() int {
	level := 1
	for rand.Float64() < probability && level < maxLevel {
		level++
	}
	return level
}

// insert adds a new node or updates score if member exists
// Returns true if new node was inserted, false if score was updated
func (sl *skipList) insert(member string, score float64) bool {
	// Track update positions at each level
	update := make([]*skipListNode, maxLevel)
	// Track ranks for span calculation
	rank := make([]int, maxLevel)

	x := sl.header
	// Start from highest level, work down to level 0
	for i := sl.level - 1; i >= 0; i-- {
		// Initialize rank from previous level
		if i == sl.level-1 {
			rank[i] = 0
		} else {
			rank[i] = rank[i+1]
		}

		// Traverse forward at current level
		for x.level[i] != nil &&
			(x.level[i].score < score ||
				(x.level[i].score == score && x.level[i].member < member)) {
			rank[i] += x.span[i]
			x = x.level[i]
		}
		update[i] = x
	}

	// Check if member already exists with same score
	x = x.level[0]
	if x != nil && x.score == score && x.member == member {
		// Already exists with same score, no change needed
		return false
	}

	// Check if member exists with different score
	if x != nil && x.member == member {
		// Remove old node, will insert new one
		sl.deleteNode(x, update)
	}

	// Generate random level for new node
	level := sl.randomLevel()

	// If new level is higher than current max, update header
	if level > sl.level {
		for i := sl.level; i < level; i++ {
			rank[i] = 0
			update[i] = sl.header
			update[i].span[i] = sl.length
		}
		sl.level = level
	}

	// Create new node
	x = &skipListNode{
		member: member,
		score:  score,
		level:  make([]*skipListNode, level),
		span:   make([]int, level),
	}

	// Insert node by updating pointers at each level
	for i := 0; i < level; i++ {
		x.level[i] = update[i].level[i]
		update[i].level[i] = x

		// Update span for rank tracking
		x.span[i] = update[i].span[i] - (rank[0] - rank[i])
		update[i].span[i] = (rank[0] - rank[i]) + 1
	}

	// Increment span for untouched levels
	for i := level; i < sl.level; i++ {
		update[i].span[i]++
	}

	// Update tail if necessary
	if x.level[0] == nil {
		sl.tail = x
	}

	sl.length++
	return true
}

// delete removes a node with given member
// Returns true if node was found and deleted
func (sl *skipList) delete(member string, score float64) bool {
	update := make([]*skipListNode, maxLevel)

	x := sl.header
	// Find the node to delete
	for i := sl.level - 1; i >= 0; i-- {
		for x.level[i] != nil &&
			(x.level[i].score < score ||
				(x.level[i].score == score && x.level[i].member < member)) {
			x = x.level[i]
		}
		update[i] = x
	}

	x = x.level[0]
	// Check if we found the exact node
	if x != nil && x.score == score && x.member == member {
		sl.deleteNode(x, update)
		return true
	}

	return false
}

// deleteNode removes a node from the skip list (internal helper)
func (sl *skipList) deleteNode(x *skipListNode, update []*skipListNode) {
	// Update forward pointers at each level
	for i := 0; i < sl.level; i++ {
		if update[i].level[i] == x {
			update[i].span[i] += x.span[i] - 1
			update[i].level[i] = x.level[i]
		} else {
			update[i].span[i]--
		}
	}

	// Update tail if we deleted last node
	if x.level[0] == nil {
		sl.tail = update[0]
	}

	// Reduce level if we deleted highest node
	for sl.level > 1 && sl.header.level[sl.level-1] == nil {
		sl.level--
	}

	sl.length--
}

// getScore returns the score for a member, or nil if not found
func (sl *skipList) getScore(member string) *float64 {
	x := sl.header

	// Search from highest level down
	for i := sl.level - 1; i >= 0; i-- {
		for x.level[i] != nil && x.level[i].member < member {
			x = x.level[i]
		}
	}

	x = x.level[0]
	if x != nil && x.member == member {
		return &x.score
	}

	return nil
}

// getRank returns the rank (0-based) of a member, or -1 if not found
// Rank 0 = lowest score, rank n-1 = highest score
func (sl *skipList) getRank(member string, score float64) int {
	rank := 0
	x := sl.header

	for i := sl.level - 1; i >= 0; i-- {
		for x.level[i] != nil &&
			(x.level[i].score < score ||
				(x.level[i].score == score && x.level[i].member <= member)) {
			rank += x.span[i]
			x = x.level[i]
		}

		// Found the exact member
		if x.member == member {
			return rank - 1 // Convert to 0-based rank
		}
	}

	return -1 // Not found
}

// getRange returns members in score range [min, max]
// If reverse is true, returns in descending order
func (sl *skipList) getRange(min, max float64, offset, count int, reverse bool) []ZSetMember {
	if sl.length == 0 {
		return nil
	}

	if reverse {
		return sl.getRangeReverse(min, max, offset, count)
	}

	result := make([]ZSetMember, 0)
	x := sl.header

	// Find first node >= min
	for i := sl.level - 1; i >= 0; i-- {
		for x.level[i] != nil && x.level[i].score < min {
			x = x.level[i]
		}
	}
	x = x.level[0]

	// Skip offset nodes
	for offset > 0 && x != nil {
		x = x.level[0]
		offset--
	}

	// Collect nodes in range
	for x != nil && x.score <= max && (count == -1 || len(result) < count) {
		result = append(result, ZSetMember{Member: x.member, Score: x.score})
		x = x.level[0]
	}

	return result
}

// getRangeReverse returns members in reverse order
func (sl *skipList) getRangeReverse(min, max float64, offset, count int) []ZSetMember {
	result := make([]ZSetMember, 0)
	x := sl.header

	// Find first node <= max
	for i := sl.level - 1; i >= 0; i-- {
		for x.level[i] != nil && x.level[i].score <= max {
			x = x.level[i]
		}
	}

	// x is now the last node <= max, or header if none
	if x == sl.header {
		return result
	}

	// Skip offset nodes going backwards
	for offset > 0 {
		// Go backwards by finding predecessor
		x = sl.findPredecessor(x)
		if x == sl.header {
			return result
		}
		offset--
	}

	// Collect nodes in range (going backwards)
	for x != sl.header && x.score >= min && (count == -1 || len(result) < count) {
		result = append(result, ZSetMember{Member: x.member, Score: x.score})
		x = sl.findPredecessor(x)
	}

	return result
}

// findPredecessor finds the node before the given node
func (sl *skipList) findPredecessor(node *skipListNode) *skipListNode {
	x := sl.header

	for i := sl.level - 1; i >= 0; i-- {
		for x.level[i] != nil && x.level[i] != node {
			if x.level[i].score > node.score ||
				(x.level[i].score == node.score && x.level[i].member > node.member) {
				break
			}
			x = x.level[i]
		}

		if x.level[i] == node {
			return x
		}
	}

	return sl.header
}

// getRangeByRank returns members by rank range [start, stop] (0-based, inclusive)
func (sl *skipList) getRangeByRank(start, stop int, reverse bool) []ZSetMember {
	if start < 0 || start >= sl.length || stop < start {
		return nil
	}

	if stop >= sl.length {
		stop = sl.length - 1
	}

	count := stop - start + 1
	result := make([]ZSetMember, 0, count)

	if reverse {
		// Start from end
		start = sl.length - 1 - stop
		stop = sl.length - 1 - (stop - count + 1)
	}

	// Traverse to start position
	rank := 0
	x := sl.header
	for i := sl.level - 1; i >= 0; i-- {
		for x.level[i] != nil && rank+x.span[i] <= start {
			rank += x.span[i]
			x = x.level[i]
		}
	}

	// Now at start position, collect nodes
	x = x.level[0]
	for count > 0 && x != nil {
		if reverse {
			result = append([]ZSetMember{{Member: x.member, Score: x.score}}, result...)
		} else {
			result = append(result, ZSetMember{Member: x.member, Score: x.score})
		}
		x = x.level[0]
		count--
	}

	return result
}

// Clone creates a deep copy of the skip list (for copy-on-write)
func (sl *skipList) Clone() *skipList {
	newList := newSkipList()

	// Copy all nodes
	x := sl.header.level[0]
	for x != nil {
		newList.insert(x.member, x.score)
		x = x.level[0]
	}

	return newList
}
