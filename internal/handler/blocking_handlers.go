package handler

import (
	"strconv"
	"strings"
	"time"

	"redis/internal/protocol"
)

// BlockingCommandFunc is a function type for blocking command handlers
// Returns (response, shouldBlock, blockingConfig)
type BlockingCommandFunc func(cmd *protocol.Command, clientID int64) ([]byte, bool, *BlockingConfig)

// BlockingConfig holds configuration for a blocking operation
type BlockingConfig struct {
	Keys      []string
	Direction BlockingDirection
	Timeout   time.Duration
	DestKey   string            // For BLMOVE
	DestDir   BlockingDirection // For BLMOVE
	ActualKey string            // Which key actually provided data (set on immediate returns)
}

// handleBLPop handles the BLPOP command
// BLPOP key [key ...] timeout
func (h *CommandHandler) handleBLPop(cmd *protocol.Command, clientID int64) ([]byte, bool, *BlockingConfig) {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'blpop' command"), false, nil
	}

	// Parse timeout (last argument)
	timeoutSecs, err := strconv.ParseFloat(cmd.Args[len(cmd.Args)-1], 64)
	if err != nil {
		return protocol.EncodeError("ERR timeout is not a float or out of range"), false, nil
	}

	keys := cmd.Args[1 : len(cmd.Args)-1]
	timeout := time.Duration(timeoutSecs * float64(time.Second))

	// Try to pop from each key in order (non-blocking first attempt)
	for _, key := range keys {
		value, ok := h.processor.LPop(key)
		if ok {
			// Found data - return immediately
			// Touch watched keys
			h.txManager.TouchKeys([]string{key})
			return protocol.EncodeArray([]string{key, value}), false, &BlockingConfig{
				Keys:      keys,
				Direction: BlockLeft,
				Timeout:   timeout,
				ActualKey: key,
			}
		}
	}

	// No data available - need to block
	// If timeout is 0, block forever (we'll use a very long timeout internally)
	if timeout == 0 {
		timeout = 365 * 24 * time.Hour // Effectively forever
	}

	return nil, true, &BlockingConfig{
		Keys:      keys,
		Direction: BlockLeft,
		Timeout:   timeout,
	}
}

// handleBRPop handles the BRPOP command
// BRPOP key [key ...] timeout
func (h *CommandHandler) handleBRPop(cmd *protocol.Command, clientID int64) ([]byte, bool, *BlockingConfig) {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'brpop' command"), false, nil
	}

	// Parse timeout (last argument)
	timeoutSecs, err := strconv.ParseFloat(cmd.Args[len(cmd.Args)-1], 64)
	if err != nil {
		return protocol.EncodeError("ERR timeout is not a float or out of range"), false, nil
	}

	keys := cmd.Args[1 : len(cmd.Args)-1]
	timeout := time.Duration(timeoutSecs * float64(time.Second))

	// Try to pop from each key in order (non-blocking first attempt)
	for _, key := range keys {
		value, ok := h.processor.RPop(key)
		if ok {
			// Found data - return immediately
			h.txManager.TouchKeys([]string{key})
			return protocol.EncodeArray([]string{key, value}), false, &BlockingConfig{
				Keys:      keys,
				Direction: BlockRight,
				Timeout:   timeout,
				ActualKey: key,
			}
		}
	}

	// No data available - need to block
	if timeout == 0 {
		timeout = 365 * 24 * time.Hour
	}

	return nil, true, &BlockingConfig{
		Keys:      keys,
		Direction: BlockRight,
		Timeout:   timeout,
	}
}

// handleBLMove handles the BLMOVE command
// BLMOVE source destination LEFT|RIGHT LEFT|RIGHT timeout
func (h *CommandHandler) handleBLMove(cmd *protocol.Command, clientID int64) ([]byte, bool, *BlockingConfig) {
	if len(cmd.Args) != 6 {
		return protocol.EncodeError("ERR wrong number of arguments for 'blmove' command"), false, nil
	}

	source := cmd.Args[1]
	dest := cmd.Args[2]
	srcDir := strings.ToUpper(cmd.Args[3])
	destDir := strings.ToUpper(cmd.Args[4])
	timeoutSecs, err := strconv.ParseFloat(cmd.Args[5], 64)
	if err != nil {
		return protocol.EncodeError("ERR timeout is not a float or out of range"), false, nil
	}

	// Parse directions
	var srcDirection, destDirection BlockingDirection
	switch srcDir {
	case "LEFT":
		srcDirection = BlockLeft
	case "RIGHT":
		srcDirection = BlockRight
	default:
		return protocol.EncodeError("ERR syntax error"), false, nil
	}

	switch destDir {
	case "LEFT":
		destDirection = BlockLeft
	case "RIGHT":
		destDirection = BlockRight
	default:
		return protocol.EncodeError("ERR syntax error"), false, nil
	}

	timeout := time.Duration(timeoutSecs * float64(time.Second))

	// Try non-blocking first
	var value string
	var ok bool

	if srcDirection == BlockLeft {
		value, ok = h.processor.LPop(source)
	} else {
		value, ok = h.processor.RPop(source)
	}

	if ok {
		// Got data - push to destination
		if destDirection == BlockLeft {
			h.processor.LPush(dest, []string{value})
		} else {
			h.processor.RPush(dest, []string{value})
		}
		// Touch both keys
		h.txManager.TouchKeys([]string{source, dest})
		return protocol.EncodeBulkString(value), false, &BlockingConfig{
			Keys:      []string{source},
			Direction: srcDirection,
			Timeout:   timeout,
			DestKey:   dest,
			DestDir:   destDirection,
			ActualKey: source,
		}
	}

	// No data - need to block
	if timeout == 0 {
		timeout = 365 * 24 * time.Hour
	}

	return nil, true, &BlockingConfig{
		Keys:      []string{source},
		Direction: srcDirection,
		Timeout:   timeout,
		DestKey:   dest,
		DestDir:   destDirection,
	}
}

// handleBRPopLPush handles the BRPOPLPUSH command (deprecated, use BLMOVE)
// BRPOPLPUSH source destination timeout
func (h *CommandHandler) handleBRPopLPush(cmd *protocol.Command, clientID int64) ([]byte, bool, *BlockingConfig) {
	if len(cmd.Args) != 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'brpoplpush' command"), false, nil
	}

	source := cmd.Args[1]
	dest := cmd.Args[2]
	timeoutSecs, err := strconv.ParseFloat(cmd.Args[3], 64)
	if err != nil {
		return protocol.EncodeError("ERR timeout is not a float or out of range"), false, nil
	}

	timeout := time.Duration(timeoutSecs * float64(time.Second))

	// Try non-blocking first
	value, ok := h.processor.RPop(source)
	if ok {
		// Got data - push to destination (left)
		h.processor.LPush(dest, []string{value})
		h.txManager.TouchKeys([]string{source, dest})
		return protocol.EncodeBulkString(value), false, &BlockingConfig{
			Keys:      []string{source},
			Direction: BlockRight,
			Timeout:   timeout,
			DestKey:   dest,
			DestDir:   BlockLeft,
			ActualKey: source,
		}
	}

	// No data - need to block
	if timeout == 0 {
		timeout = 365 * 24 * time.Hour
	}

	return nil, true, &BlockingConfig{
		Keys:      []string{source},
		Direction: BlockRight,
		Timeout:   timeout,
		DestKey:   dest,
		DestDir:   BlockLeft,
	}
}

// NotifyListPush should be called when data is pushed to a list
// This wakes up any blocked clients waiting on that key
func (h *CommandHandler) NotifyListPush(key string) {
	if !h.blockingManager.HasBlockedClients(key) {
		return
	}

	// Define pop function based on direction
	popFunc := func(direction BlockingDirection) (string, bool) {
		if direction == BlockLeft {
			return h.processor.LPop(key)
		}
		return h.processor.RPop(key)
	}

	// Define push function for BLMOVE
	pushFunc := func(destKey string, value string, direction BlockingDirection) {
		if direction == BlockLeft {
			h.processor.LPush(destKey, []string{value})
		} else {
			h.processor.RPush(destKey, []string{value})
		}
		// Touch destination key
		h.txManager.TouchKeys([]string{destKey})
	}

	// Try to unblock a client
	h.blockingManager.UnblockClientWithData(key, popFunc, pushFunc)
}

// IsBlockingCommand checks if a command is a blocking command
func IsBlockingCommand(cmd string) bool {
	switch cmd {
	case "BLPOP", "BRPOP", "BLMOVE", "BRPOPLPUSH":
		return true
	}
	return false
}
