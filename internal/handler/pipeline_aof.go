package handler

import (
	"redis/internal/replication"
)

// logBlockingToAOF logs a blocking command to AOF using the non-blocking equivalent
// Works for both immediate returns and blocked operations
func (h *CommandHandler) logBlockingToAOF(command string, actualKey string, config *BlockingConfig) {
	if h.aofWriter == nil || config == nil || actualKey == "" {
		return
	}

	// Log the equivalent non-blocking operation that actually happened
	switch command {
	case "BLPOP":
		// BLPOP key1 key2 timeout → LPOP actualKey
		h.LogToAOF("LPOP", []string{actualKey})

		// Propagate write commands to replicas
		if h.replicationMgr != nil {
			if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
				replMgr.PropagateCommand([]string{"LPOP", actualKey})
			}
		}

	case "BRPOP":
		// BRPOP key1 key2 timeout → RPOP actualKey
		h.LogToAOF("RPOP", []string{actualKey})

		// Propagate write commands to replicas
		if h.replicationMgr != nil {
			if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
				replMgr.PropagateCommand([]string{"RPOP", actualKey})
			}
		}

	case "BLMOVE":
		// BLMOVE src dst LEFT|RIGHT LEFT|RIGHT timeout → LMOVE actualKey dst LEFT|RIGHT LEFT|RIGHT
		if config.DestKey != "" {
			srcDir := "LEFT"
			if config.Direction == BlockRight {
				srcDir = "RIGHT"
			}
			dstDir := "LEFT"
			if config.DestDir == BlockRight {
				dstDir = "RIGHT"
			}
			h.LogToAOF("LMOVE", []string{actualKey, config.DestKey, srcDir, dstDir})

			// Propagate write commands to replicas
			if h.replicationMgr != nil {
				if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
					replMgr.PropagateCommand([]string{"LMOVE", actualKey, config.DestKey, srcDir, dstDir})
				}
			}
		}

	case "BRPOPLPUSH":
		// BRPOPLPUSH src dst timeout → RPOPLPUSH actualKey dst
		if config.DestKey != "" {
			h.LogToAOF("RPOPLPUSH", []string{actualKey, config.DestKey})

			// Propagate write commands to replicas
			if h.replicationMgr != nil {
				if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
					replMgr.PropagateCommand([]string{"RPOPLPUSH", actualKey, config.DestKey})
				}
			}
		}
	}
}
