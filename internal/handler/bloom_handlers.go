package handler

import (
	"fmt"
	"strconv"

	"redis/internal/processor"
	"redis/internal/protocol"
)

// handleBFReserve creates a new Bloom filter
// BF.RESERVE key error_rate capacity
func (h *CommandHandler) handleBFReserve(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.reserve' command")
	}

	key := cmd.Args[1]

	// Parse error rate
	errorRate, err := strconv.ParseFloat(cmd.Args[2], 64)
	if err != nil {
		return protocol.EncodeError("ERR error rate must be a valid float")
	}

	if errorRate <= 0 || errorRate >= 1 {
		return protocol.EncodeError("ERR error rate must be between 0 and 1")
	}

	// Parse capacity
	capacity, err := strconv.ParseUint(cmd.Args[3], 10, 64)
	if err != nil {
		return protocol.EncodeError("ERR capacity must be a positive integer")
	}

	if capacity == 0 {
		return protocol.EncodeError("ERR capacity must be greater than 0")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdBFReserve,
		Key:      key,
		Args:     []interface{}{errorRate, capacity},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.StringResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}
	return protocol.EncodeSimpleString(res.Result)
}

// handleBFAdd adds an item to the Bloom filter
// BF.ADD key item
func (h *CommandHandler) handleBFAdd(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.add' command")
	}

	key := cmd.Args[1]
	item := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdBFAdd,
		Key:      key,
		Args:     []interface{}{item},
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

// handleBFMAdd adds multiple items to the Bloom filter
// BF.MADD key item [item ...]
func (h *CommandHandler) handleBFMAdd(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.madd' command")
	}

	key := cmd.Args[1]
	items := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdBFMAdd,
		Key:      key,
		Args:     []interface{}{items},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.BoolSliceResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}

	// Encode results as array of integers (1 for true, 0 for false)
	response := make([]interface{}, len(res.Results))
	for i, added := range res.Results {
		if added {
			response[i] = "1"
		} else {
			response[i] = "0"
		}
	}
	return protocol.EncodeInterfaceArray(response)
}

// handleBFExists checks if an item exists in the Bloom filter
// BF.EXISTS key item
func (h *CommandHandler) handleBFExists(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.exists' command")
	}

	key := cmd.Args[1]
	item := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdBFExists,
		Key:      key,
		Args:     []interface{}{item},
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

// handleBFMExists checks if multiple items exist in the Bloom filter
// BF.MEXISTS key item [item ...]
func (h *CommandHandler) handleBFMExists(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.mexists' command")
	}

	key := cmd.Args[1]
	items := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdBFMExists,
		Key:      key,
		Args:     []interface{}{items},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.BoolSliceResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}

	// Encode results as array of integers (1 for true, 0 for false)
	response := make([]interface{}, len(res.Results))
	for i, exists := range res.Results {
		if exists {
			response[i] = "1"
		} else {
			response[i] = "0"
		}
	}
	return protocol.EncodeInterfaceArray(response)
}

// handleBFInfo returns information about the Bloom filter
// BF.INFO key
func (h *CommandHandler) handleBFInfo(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'bf.info' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdBFInfo,
		Key:      key,
		Args:     []interface{}{},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	res := result.(processor.BloomFilterInfoResult)
	if res.Err != nil {
		return protocol.EncodeError(res.Err.Error())
	}

	info := res.Info

	// Encode as array with field-value pairs
	response := []interface{}{
		"Capacity", fmt.Sprintf("%d", info.Capacity),
		"Size", fmt.Sprintf("%d", info.Size),
		"Number of filters", fmt.Sprintf("%d", info.NumHashes),
		"Number of items inserted", fmt.Sprintf("%d", info.Count),
		"Expansion rate", fmt.Sprintf("%.6f", info.ErrorRate),
		"Bits per item", fmt.Sprintf("%.2f", info.BitsPerItem),
	}

	return protocol.EncodeInterfaceArray(response)
}
