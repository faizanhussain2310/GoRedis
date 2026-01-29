package processor

import (
	"redis/internal/storage"
)

// executeBitmapCommand routes bitmap commands to their executors
func (p *Processor) executeBitmapCommand(cmd *Command) {
	var result interface{}

	switch cmd.Type {
	case CmdSetBit:
		result = executeSetBit(cmd, p.store)
	case CmdGetBit:
		result = executeGetBit(cmd, p.store)
	case CmdBitCount:
		result = executeBitCount(cmd, p.store)
	case CmdBitPos:
		result = executeBitPos(cmd, p.store)
	case CmdBitOp:
		result = executeBitOp(cmd, p.store)
	default:
		result = IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	cmd.Response <- result
}

// executeSetBit sets or clears the bit at offset
// Args: [offset int64, value int]
func executeSetBit(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 2 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	offset, ok := cmd.Args[0].(int64)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	value, ok := cmd.Args[1].(int)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	oldBit, err := store.SetBit(cmd.Key, offset, value)
	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	return IntResult{Result: oldBit, Err: nil}
}

// executeGetBit returns the bit value at offset
// Args: [offset int64]
func executeGetBit(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 1 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	offset, ok := cmd.Args[0].(int64)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	bit, err := store.GetBit(cmd.Key, offset)
	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	return IntResult{Result: bit, Err: nil}
}

// executeBitCount counts the number of bits set to 1
// Args: [start *int64, end *int64] (optional)
func executeBitCount(cmd *Command, store *storage.Store) interface{} {
	var start, end *int64

	if len(cmd.Args) >= 1 {
		if s, ok := cmd.Args[0].(*int64); ok {
			start = s
		}
	}

	if len(cmd.Args) >= 2 {
		if e, ok := cmd.Args[1].(*int64); ok {
			end = e
		}
	}

	count, err := store.BitCount(cmd.Key, start, end)
	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	return IntResult{Result: int(count), Err: nil}
}

// executeBitPos finds the position of the first bit set to 0 or 1
// Args: [bit int, start *int64, end *int64]
func executeBitPos(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 1 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	bit, ok := cmd.Args[0].(int)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	var start, end *int64

	if len(cmd.Args) >= 2 {
		if s, ok := cmd.Args[1].(*int64); ok {
			start = s
		}
	}

	if len(cmd.Args) >= 3 {
		if e, ok := cmd.Args[2].(*int64); ok {
			end = e
		}
	}

	pos, err := store.BitPos(cmd.Key, bit, start, end)
	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	return IntResult{Result: int(pos), Err: nil}
}

// executeBitOp performs bitwise operations between strings
// Args: [operation string, destKey string, srcKeys []string]
func executeBitOp(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 3 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	operation, ok := cmd.Args[0].(string)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	destKey, ok := cmd.Args[1].(string)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	var resultLen int64
	var err error

	switch operation {
	case "AND":
		srcKeys, ok := cmd.Args[2].([]string)
		if !ok {
			return IntResult{Result: 0, Err: ErrInvalidOperation}
		}
		resultLen, err = store.BitOpAnd(destKey, srcKeys)

	case "OR":
		srcKeys, ok := cmd.Args[2].([]string)
		if !ok {
			return IntResult{Result: 0, Err: ErrInvalidOperation}
		}
		resultLen, err = store.BitOpOr(destKey, srcKeys)

	case "XOR":
		srcKeys, ok := cmd.Args[2].([]string)
		if !ok {
			return IntResult{Result: 0, Err: ErrInvalidOperation}
		}
		resultLen, err = store.BitOpXor(destKey, srcKeys)

	case "NOT":
		srcKey, ok := cmd.Args[2].(string)
		if !ok {
			return IntResult{Result: 0, Err: ErrInvalidOperation}
		}
		resultLen, err = store.BitOpNot(destKey, srcKey)

	default:
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	return IntResult{Result: int(resultLen), Err: nil}
}
