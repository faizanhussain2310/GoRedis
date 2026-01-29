package cluster

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// NodeFlag represents a node flag type for type safety
type NodeFlag string

// Node flag constants
const (
	FlagMyself    NodeFlag = "myself"    // This is the current node
	FlagMaster    NodeFlag = "master"    // Node is a master
	FlagSlave     NodeFlag = "slave"     // Node is a replica/slave
	FlagFail      NodeFlag = "fail"      // Node is in FAIL state
	FlagPFail     NodeFlag = "pfail"     // Node is in PFAIL (possible fail) state
	FlagHandshake NodeFlag = "handshake" // Node is in handshake state
	FlagNoAddr    NodeFlag = "noaddr"    // Address is unknown
	FlagNoFlags   NodeFlag = "noflags"   // No flags set
)

// Node represents a single Redis server instance in the cluster.
// Each node is an individual server running Redis at a specific address:port.
// A node owns a subset of hash slots and can be a master (handles writes) or slave (replicates a master).
type Node struct {
	ID      string     // Unique node identifier (40-char hex string)
	Address string     // IP address
	Port    int        // Port number
	Slots   []int      // Slots owned by this node
	Flags   []NodeFlag // Node flags: master, slave, myself, fail, etc.
}

// NodeInfo returns formatted node information
func (n *Node) NodeInfo() string {
	return fmt.Sprintf("%s %s:%d", n.ID, n.Address, n.Port)
}

// HasFlag checks if node has a specific flag
func (n *Node) HasFlag(flag NodeFlag) bool {
	for _, f := range n.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// AddFlag adds a flag to the node if not already present
func (n *Node) AddFlag(flag NodeFlag) {
	if !n.HasFlag(flag) {
		n.Flags = append(n.Flags, flag)
	}
}

// RemoveFlag removes a flag from the node
func (n *Node) RemoveFlag(flag NodeFlag) {
	for i, f := range n.Flags {
		if f == flag {
			n.Flags = append(n.Flags[:i], n.Flags[i+1:]...)
			return
		}
	}
}

// FlagsString returns flags as comma-separated string
func (n *Node) FlagsString() string {
	if len(n.Flags) == 0 {
		return string(FlagNoFlags)
	}

	flags := make([]string, len(n.Flags))
	for i, f := range n.Flags {
		flags[i] = string(f)
	}
	return strings.Join(flags, ",")
}

// IsMaster checks if node is a master
func (n *Node) IsMaster() bool {
	return n.HasFlag(FlagMaster)
}

// IsSlave checks if node is a slave/replica
func (n *Node) IsSlave() bool {
	return n.HasFlag(FlagSlave)
}

// IsMyself checks if this is the current node
func (n *Node) IsMyself() bool {
	return n.HasFlag(FlagMyself)
}

// IsFailed checks if node is in FAIL state
func (n *Node) IsFailed() bool {
	return n.HasFlag(FlagFail)
}

// ClusterState represents the state of the cluster
type ClusterState string

const (
	ClusterStateOK   ClusterState = "ok"   // All slots are covered
	ClusterStateFail ClusterState = "fail" // Some slots are not covered
)

// Cluster manages the entire distributed Redis cluster topology and slot assignments.
// It maintains a view of all nodes in the cluster, tracks which node owns each of the 16384 hash slots,
// and provides routing logic to determine which node should handle a given key.
type Cluster struct {
	mu sync.RWMutex

	// Current node information
	MySelf *Node

	// All nodes in the cluster (including self)
	Nodes map[string]*Node // nodeID -> Node

	// Slot assignments: slot -> nodeID
	SlotMap [NumSlots]string

	// State of the cluster
	State ClusterState

	// Cluster enabled flag
	Enabled bool

	// Cached count of assigned slots (optimization to avoid O(16384) loop)
	AssignedSlots int
}

// NewCluster creates a new cluster instance
func NewCluster(nodeID, address string, port int) *Cluster {
	myself := &Node{
		ID:      nodeID,
		Address: address,
		Port:    port,
		Slots:   []int{},
		Flags:   []NodeFlag{FlagMyself, FlagMaster},
	}

	c := &Cluster{
		MySelf:        myself,
		Nodes:         make(map[string]*Node),
		State:         ClusterStateFail, // Start in fail state until slots are assigned
		Enabled:       false,
		AssignedSlots: 0, // No slots assigned initially
	}

	c.Nodes[nodeID] = myself

	return c
}

// Enable enables cluster mode
func (c *Cluster) Enable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Enabled = true
}

// Disable disables cluster mode
func (c *Cluster) Disable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Enabled = false
}

// IsEnabled returns whether cluster mode is enabled
func (c *Cluster) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Enabled
}

// AssignSlots assigns a range of slots to the current node
func (c *Cluster) AssignSlots(slots []int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, slot := range slots {
		if slot >= 0 && slot < NumSlots {
			// Only increment if slot was previously unassigned
			if c.SlotMap[slot] == "" {
				c.AssignedSlots++
			}
			c.SlotMap[slot] = c.MySelf.ID
			c.MySelf.Slots = append(c.MySelf.Slots, slot)
		}
	}

	c.updateState()
}

// AssignSlotRange assigns a contiguous range of slots to the current node
func (c *Cluster) AssignSlotRange(start, end int) {
	slots := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		slots = append(slots, i)
	}
	c.AssignSlots(slots)
}

// GetSlotNode returns the node ID responsible for a given slot
func (c *Cluster) GetSlotNode(slot int) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if slot < 0 || slot >= NumSlots {
		return ""
	}

	return c.SlotMap[slot]
}

// IsSlotOwner checks if the current node owns the given slot
func (c *Cluster) IsSlotOwner(slot int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if slot < 0 || slot >= NumSlots {
		return false
	}

	return c.SlotMap[slot] == c.MySelf.ID
}

// IsKeyOwner checks if the current node owns the key
func (c *Cluster) IsKeyOwner(key string) bool {
	slot := KeyHashSlot(key)
	return c.IsSlotOwner(slot)
}

// GetKeyNode returns the node responsible for a key
func (c *Cluster) GetKeyNode(key string) *Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	slot := KeyHashSlot(key)
	nodeID := c.SlotMap[slot]

	return c.Nodes[nodeID]
}

// AddNode adds a node to the cluster
func (c *Cluster) AddNode(node *Node) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Nodes[node.ID] = node

	// Update slot map with this node's slots
	for _, slot := range node.Slots {
		if slot >= 0 && slot < NumSlots {
			// Only increment if slot was previously unassigned
			if c.SlotMap[slot] == "" {
				c.AssignedSlots++
			}
			c.SlotMap[slot] = node.ID
		}
	}

	c.updateState()
}

// RemoveNode removes a node from the cluster
func (c *Cluster) RemoveNode(nodeID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, exists := c.Nodes[nodeID]
	if !exists {
		return
	}

	// Clear slot assignments for this node
	for _, slot := range node.Slots {
		if c.SlotMap[slot] == nodeID {
			c.SlotMap[slot] = ""
			c.AssignedSlots-- // Decrement counter when slot becomes unassigned
		}
	}

	delete(c.Nodes, nodeID)
	c.updateState()
}

// GetSlots returns all slots owned by the current node
func (c *Cluster) GetSlots() []int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]int{}, c.MySelf.Slots...)
}

// GetSlotRanges returns contiguous slot ranges owned by the current node.
// This converts individual slot numbers (e.g., [0, 1, 2, 5, 6, 7]) into compact ranges
// (e.g., [{0-2}, {5-7}]) for efficient representation in CLUSTER SLOTS responses.
func (c *Cluster) GetSlotRanges() []SlotRange {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return BuildSlotRanges(c.MySelf.Slots)
}

// BuildSlotRanges converts a slice of slot numbers into contiguous ranges.
// This is a utility function that can be used for any set of slots, not just the current node.
// Example: [0, 1, 2, 5, 6, 7] → [{Start: 0, End: 2}, {Start: 5, End: 7}]
func BuildSlotRanges(slots []int) []SlotRange {
	if len(slots) == 0 {
		return []SlotRange{}
	}

	// Create a copy to avoid modifying the original slice
	sortedSlots := append([]int{}, slots...)

	// Sort using Go's optimized sort (O(n log n) instead of O(n²) bubble sort)
	// This is necessary to identify consecutive slot ranges
	// Example: [100, 0, 2, 1] → [0, 1, 2, 100] → ranges: {0-2}, {100}
	sort.Ints(sortedSlots)

	// Build contiguous ranges by grouping consecutive slots
	ranges := []SlotRange{}
	start := sortedSlots[0]
	end := sortedSlots[0]

	for i := 1; i < len(sortedSlots); i++ {
		if sortedSlots[i] == end+1 {
			// Consecutive slot found, extend current range
			end = sortedSlots[i]
		} else {
			// Gap found, save current range and start a new one
			ranges = append(ranges, SlotRange{Start: start, End: end})
			start = sortedSlots[i]
			end = sortedSlots[i]
		}
	}

	// Add the final range
	ranges = append(ranges, SlotRange{Start: start, End: end})

	return ranges
}

// GetAllNodes returns all nodes in the cluster
func (c *Cluster) GetAllNodes() []*Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodes := make([]*Node, 0, len(c.Nodes))
	for _, node := range c.Nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// GetState returns the current cluster state
func (c *Cluster) GetState() ClusterState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.State
}

// updateState updates cluster state based on slot coverage.
// Uses cached AssignedSlots counter for O(1) performance instead of O(16384) loop.
// Must be called with lock held.
func (c *Cluster) updateState() {
	// Check if all slots are assigned using cached counter
	if c.AssignedSlots == NumSlots {
		c.State = ClusterStateOK
	} else {
		c.State = ClusterStateFail
	}
}

// GetClusterInfo returns cluster statistics.
// Uses cached AssignedSlots counter for O(1) performance.
func (c *Cluster) GetClusterInfo() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"cluster_state":          string(c.State),
		"cluster_slots_assigned": c.AssignedSlots,
		"cluster_slots_ok":       c.AssignedSlots,
		"cluster_slots_pfail":    0,
		"cluster_slots_fail":     NumSlots - c.AssignedSlots,
		"cluster_known_nodes":    len(c.Nodes),
		"cluster_size":           len(c.Nodes),
		"cluster_my_epoch":       1,
		"cluster_current_epoch":  1,
	}
}
