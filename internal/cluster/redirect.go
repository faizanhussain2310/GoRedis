package cluster

import "fmt"

// RedirectType represents the type of cluster redirect
type RedirectType string

const (
	// MOVED indicates the slot has been permanently moved to another node
	// Client should update its slot cache and retry
	RedirectMoved RedirectType = "MOVED"

	// ASK indicates the slot is being migrated to another node
	// Client should send ASKING command before retry (one-time redirect)
	RedirectASK RedirectType = "ASK"
)

// RedirectError represents a cluster redirect response
type RedirectError struct {
	Type    RedirectType
	Slot    int
	NodeID  string
	Address string
	Port    int
}

// Error implements the error interface
func (r *RedirectError) Error() string {
	return fmt.Sprintf("%s %d %s:%d", r.Type, r.Slot, r.Address, r.Port)
}

// NewMovedError creates a MOVED redirect error
func NewMovedError(slot int, node *Node) *RedirectError {
	return &RedirectError{
		Type:    RedirectMoved,
		Slot:    slot,
		NodeID:  node.ID,
		Address: node.Address,
		Port:    node.Port,
	}
}

// NewAskError creates an ASK redirect error
func NewAskError(slot int, node *Node) *RedirectError {
	return &RedirectError{
		Type:    RedirectASK,
		Slot:    slot,
		NodeID:  node.ID,
		Address: node.Address,
		Port:    node.Port,
	}
}

// CheckKeyOwnership checks if the current node owns the key
// Returns nil if owned, RedirectError if not
func (c *Cluster) CheckKeyOwnership(key string) error {
	if !c.IsEnabled() {
		return nil // Cluster mode disabled, allow all operations
	}

	slot := KeyHashSlot(key)

	if c.IsSlotOwner(slot) {
		return nil // This node owns the slot
	}

	// Get the node that owns this slot
	node := c.GetKeyNode(key)
	if node == nil {
		// Slot not assigned to any node
		return fmt.Errorf("CLUSTERDOWN Hash slot not served")
	}

	// Return MOVED redirect
	return NewMovedError(slot, node)
}

// CheckMultiKeyOwnership checks if all keys belong to the same slot owned by this node
// Used for multi-key commands like MGET, MSET, etc.
func (c *Cluster) CheckMultiKeyOwnership(keys []string) error {
	if !c.IsEnabled() {
		return nil // Cluster mode disabled
	}

	if len(keys) == 0 {
		return nil
	}

	// Check if all keys are in the same slot
	if !KeysInSameSlot(keys) {
		return fmt.Errorf("CROSSSLOT Keys in request don't hash to the same slot")
	}

	// Check if this node owns the slot
	return c.CheckKeyOwnership(keys[0])
}
