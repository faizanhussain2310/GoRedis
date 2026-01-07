package storage

// Hash represents a Redis hash (field-value map)
type Hash struct {
	Fields map[string]string
}

// NewHash creates a new empty hash
func NewHash() *Hash {
	return &Hash{
		Fields: make(map[string]string),
	}
}

// Clone creates a deep copy of the hash (for copy-on-write)
func (h *Hash) Clone() *Hash {
	if h == nil || len(h.Fields) == 0 {
		return NewHash()
	}

	newHash := &Hash{
		Fields: make(map[string]string, len(h.Fields)),
	}
	for k, v := range h.Fields {
		newHash.Fields[k] = v
	}
	return newHash
}

// Set sets a field to a value, returns true if field is new
func (h *Hash) Set(field, value string) bool {
	_, exists := h.Fields[field]
	h.Fields[field] = value
	return !exists
}

// Get returns the value of a field
func (h *Hash) Get(field string) (string, bool) {
	val, exists := h.Fields[field]
	return val, exists
}

// Delete removes a field, returns true if field existed
func (h *Hash) Delete(field string) bool {
	_, exists := h.Fields[field]
	if exists {
		delete(h.Fields, field)
	}
	return exists
}

// Exists checks if a field exists
func (h *Hash) Exists(field string) bool {
	_, exists := h.Fields[field]
	return exists
}

// Len returns the number of fields
func (h *Hash) Len() int {
	return len(h.Fields)
}

// Keys returns all field names
func (h *Hash) Keys() []string {
	keys := make([]string, 0, len(h.Fields))
	for k := range h.Fields {
		keys = append(keys, k)
	}
	return keys
}

// Values returns all values
func (h *Hash) Values() []string {
	vals := make([]string, 0, len(h.Fields))
	for _, v := range h.Fields {
		vals = append(vals, v)
	}
	return vals
}

// GetAll returns all fields and values as alternating slice [field1, val1, field2, val2, ...]
func (h *Hash) GetAll() []string {
	result := make([]string, 0, len(h.Fields)*2)
	for k, v := range h.Fields {
		result = append(result, k, v)
	}
	return result
}

// SetNX sets field only if it doesn't exist, returns true if set
func (h *Hash) SetNX(field, value string) bool {
	if _, exists := h.Fields[field]; exists {
		return false
	}
	h.Fields[field] = value
	return true
}
