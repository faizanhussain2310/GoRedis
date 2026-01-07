package storage

// Set represents a Redis set (unique members)
type Set struct {
	Members map[string]struct{}
}

// NewSet creates a new empty set
func NewSet() *Set {
	return &Set{
		Members: make(map[string]struct{}),
	}
}

// Clone creates a deep copy of the set (for copy-on-write)
func (s *Set) Clone() *Set {
	if s == nil || len(s.Members) == 0 {
		return NewSet()
	}

	newSet := &Set{
		Members: make(map[string]struct{}, len(s.Members)),
	}
	for member := range s.Members {
		newSet.Members[member] = struct{}{}
	}
	return newSet
}

// Add adds a member to the set, returns true if member is new
func (s *Set) Add(member string) bool {
	if _, exists := s.Members[member]; exists {
		return false
	}
	s.Members[member] = struct{}{}
	return true
}

// Remove removes a member from the set, returns true if member existed
func (s *Set) Remove(member string) bool {
	if _, exists := s.Members[member]; !exists {
		return false
	}
	delete(s.Members, member)
	return true
}

// IsMember checks if member exists in set
func (s *Set) IsMember(member string) bool {
	_, exists := s.Members[member]
	return exists
}

// Len returns the number of members
func (s *Set) Len() int {
	return len(s.Members)
}

// Members returns all members as a slice
func (s *Set) GetMembers() []string {
	members := make([]string, 0, len(s.Members))
	for m := range s.Members {
		members = append(members, m)
	}
	return members
}

// Pop removes and returns a random member
func (s *Set) Pop() (string, bool) {
	for m := range s.Members {
		delete(s.Members, m)
		return m, true
	}
	return "", false
}

// RandomMember returns a random member without removing
func (s *Set) RandomMember() (string, bool) {
	for m := range s.Members {
		return m, true
	}
	return "", false
}

// RandomMembers returns n random members without removing
// If count is negative, allows duplicates
func (s *Set) RandomMembers(count int) []string {
	if len(s.Members) == 0 {
		return []string{}
	}

	allowDuplicates := count < 0
	if count < 0 {
		count = -count
	}

	members := s.GetMembers()
	result := make([]string, 0, count)

	if allowDuplicates {
		// With duplicates allowed, just pick randomly
		for i := 0; i < count; i++ {
			idx := i % len(members) // Simple distribution
			result = append(result, members[idx])
		}
	} else {
		// Without duplicates, limit to set size
		if count > len(members) {
			count = len(members)
		}
		result = members[:count]
	}

	return result
}

// Union returns a new set with all members from both sets
func (s *Set) Union(other *Set) *Set {
	result := NewSet()
	for m := range s.Members {
		result.Add(m)
	}
	if other != nil {
		for m := range other.Members {
			result.Add(m)
		}
	}
	return result
}

// Intersect returns a new set with members common to both sets
func (s *Set) Intersect(other *Set) *Set {
	result := NewSet()
	if other == nil {
		return result
	}

	// Iterate over smaller set for efficiency
	smaller, larger := s, other
	if len(s.Members) > len(other.Members) {
		smaller, larger = other, s
	}

	for m := range smaller.Members {
		if larger.IsMember(m) {
			result.Add(m)
		}
	}
	return result
}

// Diff returns a new set with members in s but not in other
func (s *Set) Diff(other *Set) *Set {
	result := NewSet()
	for m := range s.Members {
		if other == nil || !other.IsMember(m) {
			result.Add(m)
		}
	}
	return result
}
