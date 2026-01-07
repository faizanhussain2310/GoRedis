package processor

import (
	"redis/internal/storage"
)

var (
	ErrInvalidOperation = storage.ErrInvalidOperation
)

// BloomFilterInfoResult wraps a BloomFilterInfo result
type BloomFilterInfoResult struct {
	Info *storage.BloomFilterInfo
	Err  error
}

// executeBloomCommand routes Bloom filter commands to their executors
func (p *Processor) executeBloomCommand(cmd *Command) {
	var result interface{}

	switch cmd.Type {
	case CmdBFReserve:
		result = executeBFReserve(cmd, p.store)
	case CmdBFAdd:
		result = executeBFAdd(cmd, p.store)
	case CmdBFMAdd:
		result = executeBFMAdd(cmd, p.store)
	case CmdBFExists:
		result = executeBFExists(cmd, p.store)
	case CmdBFMExists:
		result = executeBFMExists(cmd, p.store)
	case CmdBFInfo:
		result = executeBFInfo(cmd, p.store)
	default:
		result = IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	cmd.Response <- result
}

// executeBFReserve creates a new Bloom filter
// Args: [errorRate, capacity]
func executeBFReserve(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 2 {
		return StringResult{Result: "", Err: ErrInvalidOperation}
	}

	errorRate, ok := cmd.Args[0].(float64)
	if !ok {
		return StringResult{Result: "", Err: ErrInvalidOperation}
	}

	capacity, ok := cmd.Args[1].(uint64)
	if !ok {
		return StringResult{Result: "", Err: ErrInvalidOperation}
	}

	err := store.BFReserve(cmd.Key, errorRate, capacity)
	if err != nil {
		return StringResult{Result: "", Err: err}
	}

	return StringResult{Result: "OK", Err: nil}
}

// executeBFAdd adds an item to the Bloom filter
// Args: [item]
func executeBFAdd(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 1 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	item, ok := cmd.Args[0].(string)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	added, err := store.BFAdd(cmd.Key, item)
	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	// Return 1 if newly added, 0 if probably existed
	if added {
		return IntResult{Result: 1, Err: nil}
	}
	return IntResult{Result: 0, Err: nil}
}

// executeBFMAdd adds multiple items to the Bloom filter
// Args: [items]
func executeBFMAdd(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 1 {
		return BoolSliceResult{Results: nil, Err: ErrInvalidOperation}
	}

	items, ok := cmd.Args[0].([]string)
	if !ok {
		return BoolSliceResult{Results: nil, Err: ErrInvalidOperation}
	}

	results, err := store.BFMAdd(cmd.Key, items)
	if err != nil {
		return BoolSliceResult{Results: nil, Err: err}
	}

	return BoolSliceResult{Results: results, Err: nil}
}

// executeBFExists checks if an item exists in the Bloom filter
// Args: [item]
func executeBFExists(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 1 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	item, ok := cmd.Args[0].(string)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	exists, err := store.BFExists(cmd.Key, item)
	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	// Return 1 if might exist, 0 if definitely doesn't exist
	if exists {
		return IntResult{Result: 1, Err: nil}
	}
	return IntResult{Result: 0, Err: nil}
}

// executeBFMExists checks if multiple items exist in the Bloom filter
// Args: [items]
func executeBFMExists(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 1 {
		return BoolSliceResult{Results: nil, Err: ErrInvalidOperation}
	}

	items, ok := cmd.Args[0].([]string)
	if !ok {
		return BoolSliceResult{Results: nil, Err: ErrInvalidOperation}
	}

	results, err := store.BFMExists(cmd.Key, items)
	if err != nil {
		return BoolSliceResult{Results: nil, Err: err}
	}

	return BoolSliceResult{Results: results, Err: nil}
}

// executeBFInfo returns information about the Bloom filter
// Args: []
func executeBFInfo(cmd *Command, store *storage.Store) interface{} {
	info, err := store.BFInfo(cmd.Key)
	if err != nil {
		return BloomFilterInfoResult{Info: nil, Err: err}
	}

	return BloomFilterInfoResult{Info: info, Err: nil}
}
