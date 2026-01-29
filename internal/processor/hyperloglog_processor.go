package processor

import (
	"redis/internal/storage"
)

// executeHyperLogLogCommand routes HyperLogLog commands to their executors
func (p *Processor) executeHyperLogLogCommand(cmd *Command) {
	var result interface{}

	switch cmd.Type {
	case CmdPFAdd:
		result = executePFAdd(cmd, p.store)
	case CmdPFCount:
		result = executePFCount(cmd, p.store)
	case CmdPFMerge:
		result = executePFMerge(cmd, p.store)
	default:
		result = IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	cmd.Response <- result
}

// executePFAdd adds elements to a HyperLogLog
// Args: [elements []string]
func executePFAdd(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 1 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	elements, ok := cmd.Args[0].([]string)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	// Use Store method for consistent handling
	updated, err := store.PFAdd(cmd.Key, elements)
	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	// Return 1 if updated, 0 otherwise
	if updated {
		return IntResult{Result: 1, Err: nil}
	}
	return IntResult{Result: 0, Err: nil}
}

// executePFCount returns the approximated cardinality of HyperLogLog(s)
// Args: [keys []string]
func executePFCount(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 1 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	keys, ok := cmd.Args[0].([]string)
	if !ok {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	if len(keys) == 0 {
		return IntResult{Result: 0, Err: ErrInvalidOperation}
	}

	// Use Store method for consistent handling
	count, err := store.PFCount(keys)
	if err != nil {
		return IntResult{Result: 0, Err: err}
	}

	return IntResult{Result: int(count), Err: nil}
}

// executePFMerge merges multiple HyperLogLogs into one
// Args: [destKey string, sourceKeys []string]
func executePFMerge(cmd *Command, store *storage.Store) interface{} {
	if len(cmd.Args) < 2 {
		return StringResult{Result: "", Err: ErrInvalidOperation}
	}

	destKey, ok := cmd.Args[0].(string)
	if !ok {
		return StringResult{Result: "", Err: ErrInvalidOperation}
	}

	sourceKeys, ok := cmd.Args[1].([]string)
	if !ok {
		return StringResult{Result: "", Err: ErrInvalidOperation}
	}

	// Use Store method for consistent handling
	err := store.PFMerge(destKey, sourceKeys)
	if err != nil {
		return StringResult{Result: "", Err: err}
	}

	return StringResult{Result: "OK", Err: nil}
}
