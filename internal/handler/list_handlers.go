package handler

import (
	"fmt"
	"strings"

	"redis/internal/processor"
	"redis/internal/protocol"
)

func (h *CommandHandler) handleLPush(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'lpush' command")
	}

	key := cmd.Args[1]
	values := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdLPush,
		Key:      key,
		Args:     []interface{}{values},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)

	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}

	// Notify any blocked clients waiting on this key
	h.NotifyListPush(key)

	return protocol.EncodeInteger(res.Result)
}

func (h *CommandHandler) handleRPush(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'rpush' command")
	}

	key := cmd.Args[1]
	values := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdRPush,
		Key:      key,
		Args:     []interface{}{values},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.IntResult)

	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}

	// Notify any blocked clients waiting on this key
	h.NotifyListPush(key)

	return protocol.EncodeInteger(res.Result)
}

func (h *CommandHandler) handleLPop(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'lpop' command")
	}

	key := cmd.Args[1]
	count := 1

	if len(cmd.Args) >= 3 {
		if _, err := fmt.Sscanf(cmd.Args[2], "%d", &count); err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
	}

	procCmd := &processor.Command{
		Type:     processor.CmdLPop,
		Key:      key,
		Args:     []interface{}{count},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(struct {
		Result []string
		Err    error
	})

	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}

	if len(res.Result) == 0 {
		return protocol.EncodeNullBulkString()
	}

	// If count was 1 (default), return single element
	if count == 1 && len(cmd.Args) < 3 {
		return protocol.EncodeBulkString(res.Result[0])
	}

	// Otherwise return array
	return protocol.EncodeArray(res.Result)
}

func (h *CommandHandler) handleRPop(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'rpop' command")
	}

	key := cmd.Args[1]
	count := 1

	if len(cmd.Args) >= 3 {
		if _, err := fmt.Sscanf(cmd.Args[2], "%d", &count); err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
	}

	procCmd := &processor.Command{
		Type:     processor.CmdRPop,
		Key:      key,
		Args:     []interface{}{count},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.StringSliceResult)

	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}

	if len(res.Result) == 0 {
		return protocol.EncodeNullBulkString()
	}

	// If count was 1 (default), return single element
	if count == 1 && len(cmd.Args) < 3 {
		return protocol.EncodeBulkString(res.Result[0])
	}

	// Otherwise return array
	return protocol.EncodeArray(res.Result)
}

func (h *CommandHandler) handleLLen(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'llen' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdLLen,
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

func (h *CommandHandler) handleLRange(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'lrange' command")
	}

	key := cmd.Args[1]
	var start, stop int

	if _, err := fmt.Sscanf(cmd.Args[2], "%d", &start); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}
	if _, err := fmt.Sscanf(cmd.Args[3], "%d", &stop); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdLRange,
		Key:      key,
		Args:     []interface{}{start, stop},
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

func (h *CommandHandler) handleLIndex(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'lindex' command")
	}

	key := cmd.Args[1]
	var index int

	if _, err := fmt.Sscanf(cmd.Args[2], "%d", &index); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdLIndex,
		Key:      key,
		Args:     []interface{}{index},
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

func (h *CommandHandler) handleLSet(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'lset' command")
	}

	key := cmd.Args[1]
	var index int

	if _, err := fmt.Sscanf(cmd.Args[2], "%d", &index); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}
	value := cmd.Args[3]

	procCmd := &processor.Command{
		Type:     processor.CmdLSet,
		Key:      key,
		Args:     []interface{}{index, value},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	if err, ok := result.(error); ok && err != nil {
		return protocol.EncodeError(err.Error())
	}
	return protocol.EncodeSimpleString("OK")
}

func (h *CommandHandler) handleLRem(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'lrem' command")
	}

	key := cmd.Args[1]
	var count int

	if _, err := fmt.Sscanf(cmd.Args[2], "%d", &count); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}
	value := cmd.Args[3]

	procCmd := &processor.Command{
		Type:     processor.CmdLRem,
		Key:      key,
		Args:     []interface{}{count, value},
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

func (h *CommandHandler) handleLTrim(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'ltrim' command")
	}

	key := cmd.Args[1]
	var start, stop int

	if _, err := fmt.Sscanf(cmd.Args[2], "%d", &start); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}
	if _, err := fmt.Sscanf(cmd.Args[3], "%d", &stop); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdLTrim,
		Key:      key,
		Args:     []interface{}{start, stop},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	if err, ok := result.(error); ok && err != nil {
		return protocol.EncodeError(err.Error())
	}
	return protocol.EncodeSimpleString("OK")
}

func (h *CommandHandler) handleLInsert(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 5 {
		return protocol.EncodeError("ERR wrong number of arguments for 'linsert' command")
	}

	key := cmd.Args[1]
	position := strings.ToUpper(cmd.Args[2])
	pivot := cmd.Args[3]
	value := cmd.Args[4]

	var before bool
	if position == "BEFORE" {
		before = true
	} else if position == "AFTER" {
		before = false
	} else {
		return protocol.EncodeError("ERR syntax error")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdLInsert,
		Key:      key,
		Args:     []interface{}{before, pivot, value},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(struct {
		Result int
		Err    error
	})

	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeInteger(res.Result)
}
