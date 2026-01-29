package handler

import (
	"fmt"
	"redis/internal/cluster"
	"redis/internal/protocol"
	"strconv"
	"strings"
)

// handleCluster handles CLUSTER command and its subcommands
// CLUSTER SLOTS | NODES | KEYSLOT | INFO | ADDSLOTS | ...
func (h *CommandHandler) handleCluster(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'cluster' command")
	}

	subcommand := strings.ToUpper(cmd.Args[1])

	switch subcommand {
	case "SLOTS":
		return h.handleClusterSlots(cmd)
	case "NODES":
		return h.handleClusterNodes(cmd)
	case "KEYSLOT":
		return h.handleClusterKeySlot(cmd)
	case "INFO":
		return h.handleClusterInfo(cmd)
	case "ADDSLOTS":
		return h.handleClusterAddSlots(cmd)
	case "MYID":
		return h.handleClusterMyID(cmd)
	case "ENABLED":
		return h.handleClusterEnabled(cmd)
	default:
		return protocol.EncodeError(fmt.Sprintf("ERR unknown CLUSTER subcommand '%s'", subcommand))
	}
}

// handleClusterSlots returns cluster slot ranges and node mappings
// Response format: array of [start, end, [master_ip, master_port, node_id], ...]
func (h *CommandHandler) handleClusterSlots(cmd *protocol.Command) []byte {
	if h.store.Cluster == nil || !h.store.Cluster.IsEnabled() {
		return protocol.EncodeError("ERR This instance has cluster support disabled")
	}

	nodes := h.store.Cluster.GetAllNodes()

	// Build slot ranges for each node
	// For now, return simple string representation
	// Real implementation would use custom encoding for nested arrays
	result := []string{}

	for _, node := range nodes {
		if len(node.Slots) == 0 {
			continue
		}

		// Get slot ranges for this node using the reusable BuildSlotRanges function
		ranges := cluster.BuildSlotRanges(node.Slots)

		for _, slotRange := range ranges {
			entry := fmt.Sprintf("%d-%d %s:%d %s",
				slotRange.Start,
				slotRange.End,
				node.Address,
				node.Port,
				node.ID,
			)
			result = append(result, entry)
		}
	}

	return protocol.EncodeArray(result)
}

// handleClusterNodes returns cluster node information
// Format: node_id host:port@cport flags master ping_pong ping_epoch config_epoch link_state slot_range
func (h *CommandHandler) handleClusterNodes(cmd *protocol.Command) []byte {
	if h.store.Cluster == nil || !h.store.Cluster.IsEnabled() {
		return protocol.EncodeError("ERR This instance has cluster support disabled")
	}

	nodes := h.store.Cluster.GetAllNodes()
	lines := make([]string, 0, len(nodes))

	for _, node := range nodes {
		// Build slot ranges string
		slotsStr := ""
		if len(node.Slots) > 0 {
			ranges := cluster.BuildSlotRanges(node.Slots)
			rangeStrs := make([]string, 0, len(ranges))
			for _, r := range ranges {
				if r.Start == r.End {
					rangeStrs = append(rangeStrs, fmt.Sprintf("%d", r.Start))
				} else {
					rangeStrs = append(rangeStrs, fmt.Sprintf("%d-%d", r.Start, r.End))
				}
			}
			slotsStr = " " + strings.Join(rangeStrs, " ")
		}

		// Build flags string using the FlagsString method
		flags := node.FlagsString()

		// Format: id host:port@cport flags master ping pong epoch link-state slots
		line := fmt.Sprintf("%s %s:%d@%d %s - 0 0 0 connected%s",
			node.ID,
			node.Address,
			node.Port,
			node.Port+10000, // Cluster bus port
			flags,
			slotsStr,
		)

		lines = append(lines, line)
	}

	return protocol.EncodeBulkString(strings.Join(lines, "\n"))
}

// handleClusterKeySlot returns the hash slot for a given key
// CLUSTER KEYSLOT <key>
func (h *CommandHandler) handleClusterKeySlot(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'cluster|keyslot' command")
	}

	key := cmd.Args[2]
	slot := cluster.KeyHashSlot(key)

	return protocol.EncodeInteger(slot)
}

// handleClusterInfo returns cluster configuration information
func (h *CommandHandler) handleClusterInfo(cmd *protocol.Command) []byte {
	if h.store.Cluster == nil || !h.store.Cluster.IsEnabled() {
		return protocol.EncodeError("ERR This instance has cluster support disabled")
	}

	info := h.store.Cluster.GetClusterInfo()

	lines := []string{
		fmt.Sprintf("cluster_state:%s", info["cluster_state"]),
		fmt.Sprintf("cluster_slots_assigned:%d", info["cluster_slots_assigned"]),
		fmt.Sprintf("cluster_slots_ok:%d", info["cluster_slots_ok"]),
		fmt.Sprintf("cluster_slots_pfail:%d", info["cluster_slots_pfail"]),
		fmt.Sprintf("cluster_slots_fail:%d", info["cluster_slots_fail"]),
		fmt.Sprintf("cluster_known_nodes:%d", info["cluster_known_nodes"]),
		fmt.Sprintf("cluster_size:%d", info["cluster_size"]),
		fmt.Sprintf("cluster_current_epoch:%d", info["cluster_current_epoch"]),
		fmt.Sprintf("cluster_my_epoch:%d", info["cluster_my_epoch"]),
	}

	return protocol.EncodeBulkString(strings.Join(lines, "\r\n"))
}

// handleClusterAddSlots assigns slots to the current node
// CLUSTER ADDSLOTS <slot> [slot ...]
func (h *CommandHandler) handleClusterAddSlots(cmd *protocol.Command) []byte {
	if h.store.Cluster == nil {
		return protocol.EncodeError("ERR This instance has cluster support disabled")
	}

	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'cluster|addslots' command")
	}

	slots := make([]int, 0, len(cmd.Args)-2)

	for i := 2; i < len(cmd.Args); i++ {
		slot, err := strconv.Atoi(cmd.Args[i])
		if err != nil || slot < 0 || slot >= cluster.NumSlots {
			return protocol.EncodeError(fmt.Sprintf("ERR Invalid slot %s", cmd.Args[i]))
		}
		slots = append(slots, slot)
	}

	h.store.Cluster.AssignSlots(slots)

	return protocol.EncodeSimpleString("OK")
}

// handleClusterMyID returns the node ID of the current node
func (h *CommandHandler) handleClusterMyID(cmd *protocol.Command) []byte {
	if h.store.Cluster == nil || !h.store.Cluster.IsEnabled() {
		return protocol.EncodeError("ERR This instance has cluster support disabled")
	}

	return protocol.EncodeBulkString(h.store.Cluster.MySelf.ID)
}

// handleClusterEnabled returns whether cluster mode is enabled
func (h *CommandHandler) handleClusterEnabled(cmd *protocol.Command) []byte {
	enabled := h.store.Cluster != nil && h.store.Cluster.IsEnabled()
	if enabled {
		return protocol.EncodeInteger(1)
	}
	return protocol.EncodeInteger(0)
}
