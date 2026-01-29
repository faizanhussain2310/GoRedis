package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"redis/internal/protocol"
	"redis/internal/replication"
)

// executeWithTransaction handles command execution with transaction support
func (h *CommandHandler) executeWithTransaction(ctx context.Context, client *Client, cmd *protocol.Command, tx *Transaction, timeout time.Duration) PipelineResult {
	if cmd == nil || len(cmd.Args) == 0 {
		return PipelineResult{
			Response: protocol.EncodeError("ERR empty command"),
			Command:  "",
			Args:     nil,
		}
	}

	command := strings.ToUpper(cmd.Args[0])
	start := time.Now()

	// Check if client is in pub/sub mode
	if client.InPubSub {
		// In pub/sub mode, only allow specific commands
		switch command {
		case "SUBSCRIBE", "PSUBSCRIBE", "UNSUBSCRIBE", "PUNSUBSCRIBE", "PING", "QUIT":
			// These are allowed
		default:
			return PipelineResult{
				Response: protocol.EncodeError("ERR only (P)SUBSCRIBE / (P)UNSUBSCRIBE / PING / QUIT allowed in this context"),
				Duration: time.Since(start),
				Command:  command,
				Args:     cmd.Args[1:],
			}
		}
	}

	// Handle pub/sub subscription commands (need client context)
	switch command {
	case "SUBSCRIBE":
		response := h.handleSubscribe(cmd, client)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	case "UNSUBSCRIBE":
		response := h.handleUnsubscribe(cmd, client)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	case "PSUBSCRIBE":
		response := h.handlePSubscribe(cmd, client)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	case "PUNSUBSCRIBE":
		response := h.handlePUnsubscribe(cmd, client)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}

	// Handle transaction control commands specially
	switch command {
	case "MULTI":
		response := h.handleMultiCommand(tx)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}

	case "EXEC":
		response := h.handleExecCommand(ctx, client, tx, timeout)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}

	case "DISCARD":
		response := h.handleDiscardCommand(tx)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}

	case "WATCH":
		response := h.handleWatchCommand(cmd, client, tx)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}

	case "UNWATCH":
		response := h.handleUnwatchCommand(client, tx)
		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}

	// If in transaction, queue the command instead of executing
	if tx.State == TxStarted {
		// Blocking commands are not allowed inside transactions
		if IsBlockingCommand(command) {
			return PipelineResult{
				Response: protocol.EncodeError("ERR " + command + " is not allowed in a transaction"),
				Duration: time.Since(start),
				Command:  command,
				Args:     cmd.Args[1:],
			}
		}

		tx.Queue = append(tx.Queue, QueuedCommand{
			Name: command,
			Args: cmd.Args[1:],
		})
		return PipelineResult{
			Response: QueuedResponse,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}

	// Handle blocking commands specially
	if IsBlockingCommand(command) {
		return h.executeBlockingCommand(ctx, client, cmd, command, start)
	}

	// Normal execution (not in transaction)
	result := h.executeWithTimeout(ctx, cmd, timeout)

	// Touch watched keys for any clients watching these keys
	// This marks those transactions as dirty (O(M) where M = watchers)
	if writeKeys := GetWriteKeys(command, cmd.Args[1:]); len(writeKeys) > 0 {
		h.txManager.TouchKeys(writeKeys)
	}

	return result
}

// executeWithTimeout executes a single command with timeout tracking
func (h *CommandHandler) executeWithTimeout(ctx context.Context, cmd *protocol.Command, timeout time.Duration) PipelineResult {
	if cmd == nil || len(cmd.Args) == 0 {
		return PipelineResult{
			Response: protocol.EncodeError("ERR empty command"),
			Command:  "",
			Args:     nil,
		}
	}

	command := strings.ToUpper(cmd.Args[0])

	// Create a context with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Check if replica is trying to execute write command (for direct client writes)
	if h.isReplica() && IsWriteCommand(command) {
		return PipelineResult{
			Response: protocol.EncodeError("READONLY You can't write against a read only replica"),
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}

	// Execute command in channel to support timeout
	resultChan := make(chan []byte, 1)
	go func() {
		if handler, exists := h.commands[command]; exists {
			resultChan <- handler(cmd)
		} else {
			resultChan <- protocol.EncodeError(fmt.Sprintf("ERR unknown command '%s'", command))
		}
	}()

	select {
	case <-cmdCtx.Done():
		duration := time.Since(start)
		return PipelineResult{
			Response: protocol.EncodeError("ERR command timeout"),
			Duration: duration,
			Command:  command,
			Args:     cmd.Args[1:],
			Err:      ErrCommandTimeout,
		}
	case response := <-resultChan:
		duration := time.Since(start)

		// Log successful write commands to AOF
		// We check if response is not an error before logging
		if len(response) > 0 && response[0] != '-' {
			h.LogToAOF(command, cmd.Args[1:])

			// Propagate write commands to replicas
			if h.replicationMgr != nil {
				if replMgr, ok := h.replicationMgr.(*replication.ReplicationManager); ok {
					replMgr.PropagateCommand(cmd.Args)
				}
			}
		}

		return PipelineResult{
			Response: response,
			Duration: duration,
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}
}

// executeWithTimeoutNoAOF executes a command without AOF logging
// Used for transaction commands where we batch log after EXEC
func (h *CommandHandler) executeWithTimeoutNoAOF(ctx context.Context, cmd *protocol.Command, timeout time.Duration) PipelineResult {
	if cmd == nil || len(cmd.Args) == 0 {
		return PipelineResult{
			Response: protocol.EncodeError("ERR empty command"),
			Command:  "",
			Args:     nil,
		}
	}

	command := strings.ToUpper(cmd.Args[0])

	// Create a context with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Check if replica is trying to execute write command (for direct client writes)
	if h.isReplica() && IsWriteCommand(command) {
		return PipelineResult{
			Response: protocol.EncodeError("READONLY You can't write against a read only replica"),
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}

	// Execute command in channel to support timeout
	resultChan := make(chan []byte, 1)
	go func() {
		if handler, exists := h.commands[command]; exists {
			resultChan <- handler(cmd)
		} else {
			resultChan <- protocol.EncodeError(fmt.Sprintf("ERR unknown command '%s'", command))
		}
	}()

	select {
	case <-cmdCtx.Done():
		duration := time.Since(start)
		return PipelineResult{
			Response: protocol.EncodeError("ERR command timeout"),
			Duration: duration,
			Command:  command,
			Args:     cmd.Args[1:],
			Err:      ErrCommandTimeout,
		}
	case response := <-resultChan:
		duration := time.Since(start)
		// No AOF logging here - caller handles it
		return PipelineResult{
			Response: response,
			Duration: duration,
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}
}
