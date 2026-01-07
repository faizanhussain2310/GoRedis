package handler

import (
	"context"
	"time"

	"redis/internal/protocol"
)

// executeBlockingCommand handles blocking list operations
func (h *CommandHandler) executeBlockingCommand(ctx context.Context, client *Client, cmd *protocol.Command, command string, start time.Time) PipelineResult {
	var response []byte
	var shouldBlock bool
	var blockConfig *BlockingConfig

	switch command {
	case "BLPOP":
		response, shouldBlock, blockConfig = h.handleBLPop(cmd, client.ID)
	case "BRPOP":
		response, shouldBlock, blockConfig = h.handleBRPop(cmd, client.ID)
	case "BLMOVE":
		response, shouldBlock, blockConfig = h.handleBLMove(cmd, client.ID)
	case "BRPOPLPUSH":
		response, shouldBlock, blockConfig = h.handleBRPopLPush(cmd, client.ID)
	default:
		response = protocol.EncodeError("ERR unknown blocking command")
		shouldBlock = false
	}

	// If we got data immediately, return it and log to AOF
	if !shouldBlock {
		// Log successful blocking operation to AOF
		if len(response) > 0 && response[0] != '-' && blockConfig != nil && blockConfig.ActualKey != "" {
			h.logBlockingToAOF(command, blockConfig.ActualKey, blockConfig)
		}

		return PipelineResult{
			Response: response,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}

	// Need to block - register with blocking manager and wait
	resultCh := h.blockingManager.BlockClient(
		client.ID,
		blockConfig.Keys,
		blockConfig.Direction,
		blockConfig.Timeout,
		blockConfig.DestKey,
		blockConfig.DestDir,
	)

	// Wait for result or context cancellation
	select {
	case <-ctx.Done():
		// Context cancelled - unblock client
		h.blockingManager.RemoveClient(client.ID)
		return PipelineResult{
			Response: protocol.EncodeNilArray(),
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}

	case result := <-resultCh:
		if result.Err != nil {
			// Timeout
			return PipelineResult{
				Response: protocol.EncodeNilArray(),
				Duration: time.Since(start),
				Command:  command,
				Args:     cmd.Args[1:],
			}
		}

		// Got data - format response based on command type
		var resp []byte
		if blockConfig.DestKey != "" {
			// BLMOVE/BRPOPLPUSH - return just the value
			resp = protocol.EncodeBulkString(result.Value)
		} else {
			// BLPOP/BRPOP - return [key, value]
			resp = protocol.EncodeArray([]string{result.Key, result.Value})
		}

		// Log the actual operation to AOF
		// We log what actually happened (the pop from result.Key)
		h.logBlockingToAOF(command, result.Key, blockConfig)

		// Touch watched keys
		keys := []string{result.Key}
		if blockConfig.DestKey != "" {
			keys = append(keys, blockConfig.DestKey)
		}
		h.txManager.TouchKeys(keys)

		return PipelineResult{
			Response: resp,
			Duration: time.Since(start),
			Command:  command,
			Args:     cmd.Args[1:],
		}
	}
}
