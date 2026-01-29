package storage

import (
	"time"
	"log"
)

// ==================== LIST OPERATIONS ====================

// getOrCreateList returns existing list or creates new one
func (s *Store) getOrCreateList(key string) (*List, bool) {
	val, exists := s.data[key]
	if !exists {
		return NewList(), true // New list
	}

	// Check expiry
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return NewList(), true // Expired, treat as new
	}

	// Check type
	if val.Type != ListType {
		return nil, false // Wrong type
	}

	// Check if existing value is a list
	if list, ok := val.Data.(*List); ok {
		return list, true
	}
	return NewList(), true
}

// getExistingList returns existing list or nil
func (s *Store) getExistingList(key string) (*List, error) {
	val, exists := s.data[key]
	if !exists {
		return nil, nil // Key doesn't exist
	}

	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return nil, nil
	}

	if val.Type != ListType {
		return nil, ErrWrongType
	}

	if list, ok := val.Data.(*List); ok {
		return list, nil
	}
	return nil, nil
}

// saveList saves the list to storage
func (s *Store) saveList(key string, list *List) {
	if list.Length == 0 {
		s.deleteKey(key)
		return
	}

	s.data[key] = &Value{
		Data:      list,
		ExpiresAt: nil,
		Type:      ListType,
	}
}

// LPush adds elements to the head of the list - O(1) per element
func (s *Store) LPush(key string, values ...string) (int, error) {
	list, ok := s.getOrCreateList(key)
	if !ok {
		return 0, ErrWrongType
	}

	// Copy-on-write: clone list if snapshot is active
	if s.isSnapshotActive() && s.data[key] != nil {
		list = list.Clone()
	}

	// Add values to head (in order, so last value ends up first)
	for _, v := range values {
		list.PushFront(v)
	}

	s.saveList(key, list)
	return list.Length, nil
}

// RPush adds elements to the tail of the list - O(1) per element
func (s *Store) RPush(key string, values ...string) (int, error) {
	list, ok := s.getOrCreateList(key)
	if !ok {
		return 0, ErrWrongType
	}
	// Copy-on-write: clone list if snapshot is active
	if s.isSnapshotActive() && s.data[key] != nil {
		list = list.Clone()
	}
	// Add values to tail
	for _, v := range values {
		list.PushBack(v)
	}

	s.saveList(key, list)
	return list.Length, nil
}

// LPop removes and returns the first element(s) - O(1) per element
func (s *Store) LPop(key string, count int) ([]string, error) {
	list, err := s.getExistingList(key)
	if err != nil {
		return nil, err
	}
	if list == nil || list.Length == 0 {
		return nil, nil
	}

	// Copy-on-write: clone list if snapshot is active
	if s.isSnapshotActive() {
		list = list.Clone()
	}

	if count <= 0 {
		count = 1
	}
	if count > list.Length {
		count = list.Length
	}

	result := make([]string, 0, count)
	log.Println("count = ", count)
	for i := 0; i < count; i++ {
		if val, ok := list.PopFront(); ok {
			result = append(result, val)
			log.Println("val = ", val)
		}
	}
	log.Println("result = ", result)

	s.saveList(key, list)
	return result, nil
}

// RPop removes and returns the last element(s) - O(1) per element
func (s *Store) RPop(key string, count int) ([]string, error) {
	list, err := s.getExistingList(key)
	if err != nil {
		return nil, err
	}
	if list == nil || list.Length == 0 {
		return nil, nil
	}

	// Copy-on-write: clone list if snapshot is active
	if s.isSnapshotActive() {
		list = list.Clone()
	}

	if count <= 0 {
		count = 1
	}
	if count > list.Length {
		count = list.Length
	}

	result := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if val, ok := list.PopBack(); ok {
			result = append(result, val)
		}
	}

	s.saveList(key, list)
	return result, nil
}

// LLen returns the length of the list - O(1)
func (s *Store) LLen(key string) (int, error) {
	list, err := s.getExistingList(key)
	if err != nil {
		return 0, err
	}
	if list == nil {
		return 0, nil
	}
	return list.Length, nil
}

// LRange returns elements from start to stop (inclusive) - O(n)
func (s *Store) LRange(key string, start, stop int) ([]string, error) {
	list, err := s.getExistingList(key)
	if err != nil {
		return nil, err
	}
	if list == nil {
		return []string{}, nil
	}
	return list.Range(start, stop), nil
}

// LIndex returns the element at index - O(n)
func (s *Store) LIndex(key string, index int) (string, bool, error) {
	list, err := s.getExistingList(key)
	if err != nil {
		return "", false, err
	}
	if list == nil {
		return "", false, nil
	}

	val, exists := list.GetAt(index)
	return val, exists, nil
}

// LSet sets the element at index - O(n)
func (s *Store) LSet(key string, index int, value string) error {
	list, err := s.getExistingList(key)
	if err != nil {
		return err
	}
	if list == nil {
		return ErrNoSuchKey
	}

	// Copy-on-write: clone list if snapshot is active
	if s.isSnapshotActive() {
		list = list.Clone()
	}

	if !list.SetAt(index, value) {
		return ErrIndexOutOfRange
	}

	// Save the potentially cloned list
	s.saveList(key, list)
	return nil
}

// LRem removes count occurrences of value - O(n)
// count > 0: remove from head to tail
// count < 0: remove from tail to head
// count = 0: remove all occurrences
func (s *Store) LRem(key string, count int, value string) (int, error) {
	list, err := s.getExistingList(key)
	if err != nil {
		return 0, err
	}
	if list == nil || list.Length == 0 {
		return 0, nil
	}

	// Copy-on-write: clone list if snapshot is active
	if s.isSnapshotActive() {
		list = list.Clone()
	}

	removed := 0
	toRemove := count
	if count == 0 {
		toRemove = list.Length // Remove all
	} else if count < 0 {
		toRemove = -count
	}

	if count >= 0 {
		// Remove from head to tail
		node := list.Head
		for node != nil && removed < toRemove {
			next := node.Next
			if node.Value == value {
				list.RemoveNode(node)
				removed++
			}
			node = next
		}
	} else {
		// Remove from tail to head
		node := list.Tail
		for node != nil && removed < toRemove {
			prev := node.Prev
			if node.Value == value {
				list.RemoveNode(node)
				removed++
			}
			node = prev
		}
	}

	s.saveList(key, list)
	return removed, nil
}

// LTrim trims the list to the specified range - O(n)
func (s *Store) LTrim(key string, start, stop int) error {
	list, err := s.getExistingList(key)
	if err != nil {
		return err
	}
	if list == nil {
		return nil
	}

	// Copy-on-write: clone list if snapshot is active
	if s.isSnapshotActive() {
		list = list.Clone()
	}

	list.Trim(start, stop)
	s.saveList(key, list)
	return nil
}

// LInsert inserts value before or after pivot - O(n)
func (s *Store) LInsert(key string, before bool, pivot, value string) (int, error) {
	list, err := s.getExistingList(key)
	if err != nil {
		return 0, err
	}
	if list == nil || list.Length == 0 {
		return 0, nil
	}

	// Copy-on-write: clone list if snapshot is active
	if s.isSnapshotActive() {
		list = list.Clone()
	}

	// Find pivot node
	node := list.FindNode(pivot, true)
	if node == nil {
		return -1, nil // Pivot not found
	}

	if before {
		list.InsertBefore(node, value)
	} else {
		list.InsertAfter(node, value)
	}

	s.saveList(key, list)
	return list.Length, nil
}
