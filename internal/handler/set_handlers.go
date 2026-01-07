package handler

import (
	"strconv"

	"redis/internal/processor"
	"redis/internal/protocol"
)

// handleSAdd handles SADD key member [member ...]
func (h *CommandHandler) handleSAdd(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sadd' command")
	}

	key := cmd.Args[1]
	members := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdSAdd,
		Key:      key,
		Args:     []interface{}{members},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.IntResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeInteger(result.Result)
}

// handleSRem handles SREM key member [member ...]
func (h *CommandHandler) handleSRem(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'srem' command")
	}

	key := cmd.Args[1]
	members := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdSRem,
		Key:      key,
		Args:     []interface{}{members},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.IntResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeInteger(result.Result)
}

// handleSIsMember handles SISMEMBER key member
func (h *CommandHandler) handleSIsMember(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sismember' command")
	}

	key := cmd.Args[1]
	member := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdSIsMember,
		Key:      key,
		Value:    member,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.BoolResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	if result.Result {
		return protocol.EncodeInteger(1)
	}
	return protocol.EncodeInteger(0)
}

// handleSMembers handles SMEMBERS key
func (h *CommandHandler) handleSMembers(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'smembers' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdSMembers,
		Key:      key,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.StringSliceResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeArray(result.Result)
}

// handleSCard handles SCARD key
func (h *CommandHandler) handleSCard(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'scard' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdSCard,
		Key:      key,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.IntResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeInteger(result.Result)
}

// handleSPop handles SPOP key [count]
func (h *CommandHandler) handleSPop(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'spop' command")
	}

	key := cmd.Args[1]
	count := 1
	returnSingle := true

	if len(cmd.Args) >= 3 {
		var err error
		count, err = strconv.Atoi(cmd.Args[2])
		if err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
		if count < 0 {
			return protocol.EncodeError("ERR value is negative")
		}
		returnSingle = false
	}

	procCmd := &processor.Command{
		Type:     processor.CmdSPop,
		Key:      key,
		Args:     []interface{}{count},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.StringSliceResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}

	// If count not specified, return single element or nil
	if returnSingle {
		if len(result.Result) == 0 {
			return protocol.EncodeNullBulkString()
		}
		return protocol.EncodeBulkString(result.Result[0])
	}

	return protocol.EncodeArray(result.Result)
}

// handleSRandMember handles SRANDMEMBER key [count]
func (h *CommandHandler) handleSRandMember(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'srandmember' command")
	}

	key := cmd.Args[1]
	count := 1
	returnSingle := true

	if len(cmd.Args) >= 3 {
		var err error
		count, err = strconv.Atoi(cmd.Args[2])
		if err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
		returnSingle = false
	}

	procCmd := &processor.Command{
		Type:     processor.CmdSRandMember,
		Key:      key,
		Args:     []interface{}{count},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.StringSliceResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}

	// If count not specified, return single element or nil
	if returnSingle {
		if len(result.Result) == 0 {
			return protocol.EncodeNullBulkString()
		}
		return protocol.EncodeBulkString(result.Result[0])
	}

	return protocol.EncodeArray(result.Result)
}

// handleSUnion handles SUNION key [key ...]
func (h *CommandHandler) handleSUnion(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sunion' command")
	}

	keys := cmd.Args[1:]

	procCmd := &processor.Command{
		Type:     processor.CmdSUnion,
		Args:     []interface{}{keys},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.StringSliceResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeArray(result.Result)
}

// handleSInter handles SINTER key [key ...]
func (h *CommandHandler) handleSInter(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sinter' command")
	}

	keys := cmd.Args[1:]

	procCmd := &processor.Command{
		Type:     processor.CmdSInter,
		Args:     []interface{}{keys},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.StringSliceResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeArray(result.Result)
}

// handleSDiff handles SDIFF key [key ...]
func (h *CommandHandler) handleSDiff(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sdiff' command")
	}

	keys := cmd.Args[1:]

	procCmd := &processor.Command{
		Type:     processor.CmdSDiff,
		Args:     []interface{}{keys},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.StringSliceResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeArray(result.Result)
}

// handleSMove handles SMOVE source destination member
func (h *CommandHandler) handleSMove(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'smove' command")
	}

	srcKey := cmd.Args[1]
	destKey := cmd.Args[2]
	member := cmd.Args[3]

	procCmd := &processor.Command{
		Type:     processor.CmdSMove,
		Key:      srcKey,
		Args:     []interface{}{destKey, member},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.BoolResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	if result.Result {
		return protocol.EncodeInteger(1)
	}
	return protocol.EncodeInteger(0)
}

// handleSUnionStore handles SUNIONSTORE destination key [key ...]
func (h *CommandHandler) handleSUnionStore(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sunionstore' command")
	}

	destKey := cmd.Args[1]
	keys := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdSUnionStore,
		Key:      destKey,
		Args:     []interface{}{keys},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.IntResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeInteger(result.Result)
}

// handleSInterStore handles SINTERSTORE destination key [key ...]
func (h *CommandHandler) handleSInterStore(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sinterstore' command")
	}

	destKey := cmd.Args[1]
	keys := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdSInterStore,
		Key:      destKey,
		Args:     []interface{}{keys},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.IntResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeInteger(result.Result)
}

// handleSDiffStore handles SDIFFSTORE destination key [key ...]
func (h *CommandHandler) handleSDiffStore(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'sdiffstore' command")
	}

	destKey := cmd.Args[1]
	keys := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdSDiffStore,
		Key:      destKey,
		Args:     []interface{}{keys},
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := (<-procCmd.Response).(processor.IntResult)

	if result.Err != nil {
		return protocol.EncodeError(result.Err.Error())
	}
	return protocol.EncodeInteger(result.Result)
}
