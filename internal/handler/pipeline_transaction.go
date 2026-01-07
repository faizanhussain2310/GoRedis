package handler

import (
	"context"
	"time"

	"redis/internal/protocol"
	"redis/internal/replication"
)

// handleMultiCommand handles the MULTI command
func (h *CommandHandler) handleMultiCommand(tx *Transaction) []byte {
	if tx.State == TxStarted {
		return protocol.EncodeError("ERR MULTI calls can not be nested")
	}
	tx.State = TxStarted
	tx.Queue = tx.Queue[:0] // Clear any previous queue
	return OKResponse
}

// handleExecCommand handles the EXEC command
func (h *CommandHandler) handleExecCommand(ctx context.Context, client *Client, tx *Transaction, timeout time.Duration) []byte {
	if tx.State != TxStarted {
		return protocol.EncodeError("ERR EXEC without MULTI")
	}

	// Check if transaction is dirty (a watched key was modified)
	// This is O(1) - just check the dirty flag!
	if h.txManager.IsTransactionDirty(tx) {
		// Watched key was modified - abort transaction
		tx.Reset()
		h.txManager.UnwatchAllKeys(client.ID)
		return NilResponse // Return nil array (transaction aborted)
	}

	// Execute all queued commands
	results := make([][]byte, len(tx.Queue))
	successfulCmds := make([]QueuedCommand, 0, len(tx.Queue))

	for i, qcmd := range tx.Queue {
		// Reconstruct the command
		args := append([]string{qcmd.Name}, qcmd.Args...)
		cmd := &protocol.Command{Args: args}

		// Execute with timeout (but don't log to AOF yet - we'll batch log after)
		result := h.executeWithTimeoutNoAOF(ctx, cmd, timeout)
		results[i] = result.Response

		// Track successful commands for AOF logging
		// Only log commands that succeeded (not errors) because Redis logs after execution
		if len(result.Response) > 0 && result.Response[0] != '-' {
			successfulCmds = append(successfulCmds, qcmd)
		}

		// Touch watched keys for any clients watching these keys
		if writeKeys := GetWriteKeys(qcmd.Name, qcmd.Args); len(writeKeys) > 0 {
			h.txManager.TouchKeys(writeKeys)
		}
	}

	// Log only successful write commands to AOF after execution
	// Redis logs to AOF after execution, so we only log commands that actually succeeded
	for _, qcmd := range successfulCmds {
		h.LogToAOF(qcmd.Name, qcmd.Args)

		// Propagate write commands to replicas
		if h.replicationMgr != nil {
			if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
				// Build full command args (command + arguments)
				fullArgs := append([]string{qcmd.Name}, qcmd.Args...)
				replMgr.PropagateCommand(fullArgs)
			}
		}
	}

	// Reset transaction state and clear watches
	tx.Reset()
	h.txManager.UnwatchAllKeys(client.ID)

	// Return array of results
	return protocol.EncodeRawArray(results)
}

// handleDiscardCommand handles the DISCARD command
func (h *CommandHandler) handleDiscardCommand(tx *Transaction) []byte {
	if tx.State != TxStarted {
		return protocol.EncodeError("ERR DISCARD without MULTI")
	}
	tx.Reset()
	// Note: DISCARD does NOT clear watches in Redis
	return OKResponse
}

// handleWatchCommand handles the WATCH command
func (h *CommandHandler) handleWatchCommand(cmd *protocol.Command, client *Client, tx *Transaction) []byte {
	if tx.State == TxStarted {
		return protocol.EncodeError("ERR WATCH inside MULTI is not allowed")
	}

	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'watch' command")
	}

	// Watch each key - adds to reverse index for O(1) dirty check later
	for _, key := range cmd.Args[1:] {
		h.txManager.WatchKey(client.ID, key)
	}

	return OKResponse
}

// handleUnwatchCommand handles the UNWATCH command
func (h *CommandHandler) handleUnwatchCommand(client *Client, tx *Transaction) []byte {
	h.txManager.UnwatchAllKeys(client.ID)
	return OKResponse
}
