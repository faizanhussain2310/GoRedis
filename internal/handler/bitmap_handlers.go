package handler

import (
	"fmt"
	"strconv"
	"strings"

	"redis/internal/processor"
	"redis/internal/protocol"
)

// handleSetBit sets or clears the bit at offset in the string value
// SETBIT key offset value
// Returns the original bit value at offset
func (h *CommandHandler) handleSetBit(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'setbit' command")
	}

	key := cmd.Args[1]

	// Parse offset
	offset, err := strconv.ParseInt(cmd.Args[2], 10, 64)
	if err != nil || offset < 0 {
		return protocol.EncodeError("ERR bit offset is not an integer or out of range")
	}

	// Parse value (must be 0 or 1)
	value, err := strconv.Atoi(cmd.Args[3])
	if err != nil || (value != 0 && value != 1) {
		return protocol.EncodeError("ERR bit is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdSetBit,
		Key:      key,
		Args:     []interface{}{offset, value},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(res.Result)
}

// handleGetBit returns the bit value at offset in the string value
// GETBIT key offset
// Returns 0 or 1
func (h *CommandHandler) handleGetBit(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'getbit' command")
	}

	key := cmd.Args[1]

	// Parse offset
	offset, err := strconv.ParseInt(cmd.Args[2], 10, 64)
	if err != nil || offset < 0 {
		return protocol.EncodeError("ERR bit offset is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdGetBit,
		Key:      key,
		Args:     []interface{}{offset},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(res.Result)
}

// handleBitCount returns the count of bits set to 1
// BITCOUNT key [start end]
// Start and end are byte indices (not bit indices)
func (h *CommandHandler) handleBitCount(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bitcount' command")
	}

	key := cmd.Args[1]

	var start, end *int64

	// Parse optional start and end
	if len(cmd.Args) >= 4 {
		s, err := strconv.ParseInt(cmd.Args[2], 10, 64)
		if err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
		start = &s

		e, err := strconv.ParseInt(cmd.Args[3], 10, 64)
		if err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
		end = &e
	} else if len(cmd.Args) == 3 {
		return protocol.EncodeError("ERR syntax error")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdBitCount,
		Key:      key,
		Args:     []interface{}{start, end},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(res.Result)
}

// handleBitPos finds the position of the first bit set to 0 or 1
// BITPOS key bit [start] [end]
// Start and end are byte indices
func (h *CommandHandler) handleBitPos(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bitpos' command")
	}

	key := cmd.Args[1]

	// Parse bit (must be 0 or 1)
	bit, err := strconv.Atoi(cmd.Args[2])
	if err != nil || (bit != 0 && bit != 1) {
		return protocol.EncodeError("ERR The bit argument must be 1 or 0")
	}

	var start, end *int64

	// Parse optional start
	if len(cmd.Args) >= 4 {
		s, err := strconv.ParseInt(cmd.Args[3], 10, 64)
		if err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
		start = &s
	}

	// Parse optional end
	if len(cmd.Args) >= 5 {
		e, err := strconv.ParseInt(cmd.Args[4], 10, 64)
		if err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
		end = &e
	}

	procCmd := &processor.Command{
		Type:     processor.CmdBitPos,
		Key:      key,
		Args:     []interface{}{bit, start, end},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(res.Result)
}

// handleBitOp performs bitwise operations between strings
// BITOP operation destkey srckey [srckey ...]
// Operations: AND, OR, XOR, NOT
func (h *CommandHandler) handleBitOp(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bitop' command")
	}

	operation := strings.ToUpper(cmd.Args[1])
	destKey := cmd.Args[2]

	var args []interface{}

	// NOT operation requires exactly one source key
	if operation == "NOT" {
		if len(cmd.Args) != 4 {
			return protocol.EncodeError("ERR BITOP NOT must be called with a single source key")
		}
		srcKey := cmd.Args[3]
		args = []interface{}{operation, destKey, srcKey}
	} else {
		// AND, OR, XOR require at least one source key
		if len(cmd.Args) < 4 {
			return protocol.EncodeError("ERR wrong number of arguments for 'bitop' command")
		}
		srcKeys := cmd.Args[3:]
		args = []interface{}{operation, destKey, srcKeys}
	}

	procCmd := &processor.Command{
		Type:     processor.CmdBitOp,
		Key:      destKey,
		Args:     args,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(res.Result)
}
