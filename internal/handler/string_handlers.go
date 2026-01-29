package handler

import (
	"fmt"
	"time"

	"redis/internal/processor"
	"redis/internal/protocol"
)

func (h *CommandHandler) handlePing(cmd *protocol.Command) []byte {
	if len(cmd.Args) > 1 {
		return protocol.EncodeBulkString(cmd.Args[1])
	}
	return protocol.EncodeSimpleString("PONG")
}

func (h *CommandHandler) handleEcho(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'echo' command")
	}
	return protocol.EncodeBulkString(cmd.Args[1])
}

func (h *CommandHandler) handleSet(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'set' command")
	}

	key := cmd.Args[1]
	value := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdSet,
		Key:      key,
		Value:    value,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	<-procCmd.Response

	return protocol.EncodeSimpleString("OK")
}

func (h *CommandHandler) handleSetEx(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'setex' command")
	}

	key := cmd.Args[1]
	seconds := cmd.Args[2]
	value := cmd.Args[3]

	// Parse seconds
	var sec int
	if _, err := fmt.Sscanf(seconds, "%d", &sec); err != nil || sec <= 0 {
		return protocol.EncodeError("ERR invalid expire time in 'setex' command")
	}

	expiry := time.Now().Add(time.Duration(sec) * time.Second)
	procCmd := &processor.Command{
		Type:     processor.CmdSet,
		Key:      key,
		Value:    value,
		Expiry:   &expiry,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	<-procCmd.Response

	return protocol.EncodeSimpleString("OK")
}

func (h *CommandHandler) handleGet(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'get' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdGet,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.GetResult)

	if !res.Exists {
		return protocol.EncodeNullBulkString()
	}

	if str, ok := res.Value.(string); ok {
		return protocol.EncodeBulkString(str)
	}

	return protocol.EncodeNullBulkString()
}

func (h *CommandHandler) handleDel(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'del' command")
	}

	count := 0
	for i := 1; i < len(cmd.Args); i++ {
		procCmd := &processor.Command{
			Type:     processor.CmdDelete,
			Key:      cmd.Args[i],
			Response: make(chan interface{}, 1),
		}
		h.processor.Submit(procCmd)
		result := <-procCmd.Response
		if result.(bool) {
			count++
		}
	}

	return protocol.EncodeInteger(count)
}

func (h *CommandHandler) handleExists(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'exists' command")
	}

	count := 0
	for i := 1; i < len(cmd.Args); i++ {
		procCmd := &processor.Command{
			Type:     processor.CmdExists,
			Key:      cmd.Args[i],
			Response: make(chan interface{}, 1),
		}
		h.processor.Submit(procCmd)
		result := <-procCmd.Response
		if result.(bool) {
			count++
		}
	}

	return protocol.EncodeInteger(count)
}

func (h *CommandHandler) handleKeys(cmd *protocol.Command) []byte {
	procCmd := &processor.Command{
		Type:     processor.CmdKeys,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	keys := result.([]string)
	return protocol.EncodeArray(keys)
}

func (h *CommandHandler) handleFlushAll(cmd *protocol.Command) []byte {
	procCmd := &processor.Command{
		Type:     processor.CmdFlush,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	<-procCmd.Response

	return protocol.EncodeSimpleString("OK")
}

func (h *CommandHandler) handleCommand(cmd *protocol.Command) []byte {
	return protocol.EncodeArray([]string{})
}

func (h *CommandHandler) handleExpire(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'expire' command")
	}

	key := cmd.Args[1]
	seconds := cmd.Args[2]

	// Parse seconds
	var sec int
	if _, err := fmt.Sscanf(seconds, "%d", &sec); err != nil || sec <= 0 {
		return protocol.EncodeError("ERR invalid expire time in 'expire' command")
	}

	expiry := time.Now().Add(time.Duration(sec) * time.Second)
	procCmd := &processor.Command{
		Type:     processor.CmdExpire,
		Key:      key,
		Expiry:   &expiry,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	if result.(bool) {
		return protocol.EncodeInteger(1) // Success
	}
	return protocol.EncodeInteger(0) // Key doesn't exist
}

func (h *CommandHandler) handleTTL(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'ttl' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdTTL,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	ttl := result.(int64)
	return protocol.EncodeInteger(int(ttl))
}

func (h *CommandHandler) handleIncr(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'incr' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdIncr,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.Int64Result)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(int(res.Result))
}

func (h *CommandHandler) handleIncrBy(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'incrby' command")
	}

	key := cmd.Args[1]
	increment := cmd.Args[2]

	// Parse increment value
	var inc int64
	if _, err := fmt.Sscanf(increment, "%d", &inc); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdIncrBy,
		Key:      key,
		Value:    inc,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.Int64Result)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(int(res.Result))
}

func (h *CommandHandler) handleDecr(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'decr' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdDecr,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.Int64Result)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(int(res.Result))
}

func (h *CommandHandler) handleDecrBy(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'decrby' command")
	}

	key := cmd.Args[1]
	decrement := cmd.Args[2]

	// Parse decrement value
	var dec int64
	if _, err := fmt.Sscanf(decrement, "%d", &dec); err != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdDecrBy,
		Key:      key,
		Value:    dec,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.Int64Result)
	if res.Err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %v", res.Err))
	}

	return protocol.EncodeInteger(int(res.Result))
}
