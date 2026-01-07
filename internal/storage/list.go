package storage

// ==================== DOUBLY LINKED LIST ====================

// ListNode represents a node in the doubly linked list
type ListNode struct {
	Value string
	Prev  *ListNode
	Next  *ListNode
}

// List represents a doubly linked list
type List struct {
	Head   *ListNode
	Tail   *ListNode
	Length int
}

// NewList creates a new empty list
func NewList() *List {
	return &List{
		Head:   nil,
		Tail:   nil,
		Length: 0,
	}
}

// Clone creates a deep copy of the list (for copy-on-write)
func (l *List) Clone() *List {
	if l == nil || l.Length == 0 {
		return NewList()
	}

	newList := NewList()
	current := l.Head
	for current != nil {
		newList.PushBack(current.Value)
		current = current.Next
	}
	return newList
}

// PushFront adds element to the head - O(1)
func (l *List) PushFront(value string) {
	node := &ListNode{Value: value}

	if l.Head == nil {
		l.Head = node
		l.Tail = node
	} else {
		node.Next = l.Head
		l.Head.Prev = node
		l.Head = node
	}
	l.Length++
}

// PushBack adds element to the tail - O(1)
func (l *List) PushBack(value string) {
	node := &ListNode{Value: value}

	if l.Tail == nil {
		l.Head = node
		l.Tail = node
	} else {
		node.Prev = l.Tail
		l.Tail.Next = node
		l.Tail = node
	}
	l.Length++
}

// PopFront removes and returns the first element - O(1)
func (l *List) PopFront() (string, bool) {
	if l.Head == nil {
		return "", false
	}

	value := l.Head.Value
	l.Head = l.Head.Next

	if l.Head != nil {
		l.Head.Prev = nil
	} else {
		l.Tail = nil
	}

	l.Length--
	return value, true
}

// PopBack removes and returns the last element - O(1)
func (l *List) PopBack() (string, bool) {
	if l.Tail == nil {
		return "", false
	}

	value := l.Tail.Value
	l.Tail = l.Tail.Prev

	if l.Tail != nil {
		l.Tail.Next = nil
	} else {
		l.Head = nil
	}

	l.Length--
	return value, true
}

// GetAt returns element at index - O(n)
func (l *List) GetAt(index int) (string, bool) {
	node := l.getNodeAt(index)
	if node == nil {
		return "", false
	}
	return node.Value, true
}

// SetAt sets element at index - O(n)
func (l *List) SetAt(index int, value string) bool {
	node := l.getNodeAt(index)
	if node == nil {
		return false
	}
	node.Value = value
	return true
}

// getNodeAt returns node at index (handles negative indices)
func (l *List) getNodeAt(index int) *ListNode {
	if l.Length == 0 {
		return nil
	}

	// Handle negative index
	if index < 0 {
		index = l.Length + index
	}

	if index < 0 || index >= l.Length {
		return nil
	}

	// Optimize: traverse from closer end
	var node *ListNode
	if index < l.Length/2 {
		// Traverse from head
		node = l.Head
		for i := 0; i < index; i++ {
			node = node.Next
		}
	} else {
		// Traverse from tail
		node = l.Tail
		for i := l.Length - 1; i > index; i-- {
			node = node.Prev
		}
	}
	return node
}

// Range returns elements from start to stop (inclusive) - O(n)
func (l *List) Range(start, stop int) []string {
	if l.Length == 0 {
		return []string{}
	}

	// Handle negative indices
	if start < 0 {
		start = l.Length + start
	}
	if stop < 0 {
		stop = l.Length + stop
	}

	// Clamp to valid range
	if start < 0 {
		start = 0
	}
	if stop >= l.Length {
		stop = l.Length - 1
	}

	if start > stop || start >= l.Length {
		return []string{}
	}

	result := make([]string, 0, stop-start+1)
	node := l.getNodeAt(start)

	for i := start; i <= stop && node != nil; i++ {
		result = append(result, node.Value)
		node = node.Next
	}

	return result
}

// ToSlice converts list to slice - O(n)
func (l *List) ToSlice() []string {
	result := make([]string, 0, l.Length)
	node := l.Head
	for node != nil {
		result = append(result, node.Value)
		node = node.Next
	}
	return result
}

// RemoveNode removes a specific node - O(1)
func (l *List) RemoveNode(node *ListNode) {
	if node == nil {
		return
	}

	if node.Prev != nil {
		node.Prev.Next = node.Next
	} else {
		l.Head = node.Next
	}

	if node.Next != nil {
		node.Next.Prev = node.Prev
	} else {
		l.Tail = node.Prev
	}

	l.Length--
}

// InsertBefore inserts value before the given node - O(1)
func (l *List) InsertBefore(node *ListNode, value string) {
	if node == nil {
		return
	}

	newNode := &ListNode{Value: value}
	newNode.Next = node
	newNode.Prev = node.Prev

	if node.Prev != nil {
		node.Prev.Next = newNode
	} else {
		l.Head = newNode
	}
	node.Prev = newNode

	l.Length++
}

// InsertAfter inserts value after the given node - O(1)
func (l *List) InsertAfter(node *ListNode, value string) {
	if node == nil {
		return
	}

	newNode := &ListNode{Value: value}
	newNode.Prev = node
	newNode.Next = node.Next

	if node.Next != nil {
		node.Next.Prev = newNode
	} else {
		l.Tail = newNode
	}
	node.Next = newNode

	l.Length++
}

// FindNode finds node with value, starting from head or tail
func (l *List) FindNode(value string, fromHead bool) *ListNode {
	if fromHead {
		node := l.Head
		for node != nil {
			if node.Value == value {
				return node
			}
			node = node.Next
		}
	} else {
		node := l.Tail
		for node != nil {
			if node.Value == value {
				return node
			}
			node = node.Prev
		}
	}
	return nil
}

// Trim keeps only elements from start to stop - O(n)
func (l *List) Trim(start, stop int) {
	if l.Length == 0 {
		return
	}

	// Handle negative indices
	if start < 0 {
		start = l.Length + start
	}
	if stop < 0 {
		stop = l.Length + stop
	}

	// Clamp to valid range
	if start < 0 {
		start = 0
	}
	if stop >= l.Length {
		stop = l.Length - 1
	}

	if start > stop || start >= l.Length {
		// Empty the list
		l.Head = nil
		l.Tail = nil
		l.Length = 0
		return
	}

	// Get new head
	newHead := l.getNodeAt(start)
	newTail := l.getNodeAt(stop)

	if newHead != nil {
		newHead.Prev = nil
	}
	if newTail != nil {
		newTail.Next = nil
	}

	l.Head = newHead
	l.Tail = newTail
	l.Length = stop - start + 1
}
