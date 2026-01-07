package handler

import (
	"fmt"
	"strconv"

	"redis/internal/processor"
	"redis/internal/protocol"
)

func (h *CommandHandler) handleHSet(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 || len(cmd.Args)%2 != 0 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hset' command")
	}

	key := cmd.Args[1]
	fieldValues := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdHSet,
		Key:      key,
		Args:     []interface{}{fieldValues},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeInteger(res.Result)
}

func (h *CommandHandler) handleHGet(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hget' command")
	}

	key := cmd.Args[1]
	field := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdHGet,
		Key:      key,
		Args:     []interface{}{field},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IndexResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	if !res.Exists {
		return protocol.EncodeNullBulkString()
	}
	return protocol.EncodeBulkString(res.Value)
}

func (h *CommandHandler) handleHMGet(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hmget' command")
	}

	key := cmd.Args[1]
	fields := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdHMGet,
		Key:      key,
		Args:     []interface{}{fields},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.InterfaceSliceResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}

	// Encode as array with nulls for missing fields
	return protocol.EncodeInterfaceArray(res.Result)
}

func (h *CommandHandler) handleHDel(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hdel' command")
	}

	key := cmd.Args[1]
	fields := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdHDel,
		Key:      key,
		Args:     []interface{}{fields},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeInteger(res.Result)
}

func (h *CommandHandler) handleHExists(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hexists' command")
	}

	key := cmd.Args[1]
	field := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdHExists,
		Key:      key,
		Args:     []interface{}{field},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.BoolResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	if res.Result {
		return protocol.EncodeInteger(1)
	}
	return protocol.EncodeInteger(0)
}

func (h *CommandHandler) handleHLen(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hlen' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdHLen,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeInteger(res.Result)
}

func (h *CommandHandler) handleHKeys(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hkeys' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdHKeys,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.StringSliceResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeArray(res.Result)
}

func (h *CommandHandler) handleHVals(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hvals' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdHVals,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.StringSliceResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeArray(res.Result)
}

func (h *CommandHandler) handleHGetAll(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hgetall' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdHGetAll,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.StringSliceResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeArray(res.Result)
}

func (h *CommandHandler) handleHSetNX(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hsetnx' command")
	}

	key := cmd.Args[1]
	field := cmd.Args[2]
	value := cmd.Args[3]

	procCmd := &processor.Command{
		Type:     processor.CmdHSetNX,
		Key:      key,
		Args:     []interface{}{field, value},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.BoolResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	if res.Result {
		return protocol.EncodeInteger(1)
	}
	return protocol.EncodeInteger(0)
}

func (h *CommandHandler) handleHIncrBy(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hincrby' command")
	}

	key := cmd.Args[1]
	field := cmd.Args[2]

	increment, err := strconv.ParseInt(cmd.Args[3], 10, 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdHIncrBy,
		Key:      key,
		Args:     []interface{}{field, increment},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.Int64Result)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeInteger64(res.Result)
}

func (h *CommandHandler) handleHIncrByFloat(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'hincrbyfloat' command")
	}

	key := cmd.Args[1]
	field := cmd.Args[2]

	increment, err := strconv.ParseFloat(cmd.Args[3], 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not a valid float")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdHIncrByFloat,
		Key:      key,
		Args:     []interface{}{field, increment},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.Float64Result)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeBulkString(fmt.Sprintf("%v", res.Result))
}
