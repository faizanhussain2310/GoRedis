package handler

import (
	"fmt"

	"redis/internal/processor"
	"redis/internal/protocol"
)

// handlePFAdd adds elements to a HyperLogLog
// PFADD key element [element ...]
// Returns 1 if at least one register was updated, 0 otherwise
func (h *CommandHandler) handlePFAdd(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'pfadd' command")
	}

	key := cmd.Args[1]
	elements := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdPFAdd,
		Key:      key,
		Args:     []interface{}{elements},
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

// handlePFCount returns the approximated cardinality of HyperLogLog(s)
// PFCOUNT key [key ...]
// Single key: returns cardinality of that HyperLogLog
// Multiple keys: returns cardinality of union of all HyperLogLogs
func (h *CommandHandler) handlePFCount(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'pfcount' command")
	}

	keys := cmd.Args[1:]

	procCmd := &processor.Command{
		Type:     processor.CmdPFCount,
		Key:      keys[0], // Use first key for command tracking
		Args:     []interface{}{keys},
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

// handlePFMerge merges multiple HyperLogLogs into one
// PFMERGE destkey sourcekey [sourcekey ...]
// Stores the result in destkey
func (h *CommandHandler) handlePFMerge(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'pfmerge' command")
	}

	destKey := cmd.Args[1]
	sourceKeys := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdPFMerge,
		Key:      destKey,
		Args:     []interface{}{destKey, sourceKeys},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.StringResult)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeSimpleString("OK")
}
