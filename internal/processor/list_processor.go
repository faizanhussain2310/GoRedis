package processor

// executeListCommand handles list commands
func (p *Processor) executeListCommand(cmd *Command) {
	switch cmd.Type {
	case CmdLPush:
		p.executeLPush(cmd)
	case CmdRPush:
		p.executeRPush(cmd)
	case CmdLPop:
		p.executeLPop(cmd)
	case CmdRPop:
		p.executeRPop(cmd)
	case CmdLLen:
		p.executeLLen(cmd)
	case CmdLRange:
		p.executeLRange(cmd)
	case CmdLIndex:
		p.executeLIndex(cmd)
	case CmdLSet:
		p.executeLSet(cmd)
	case CmdLRem:
		p.executeLRem(cmd)
	case CmdLTrim:
		p.executeLTrim(cmd)
	case CmdLInsert:
		p.executeLInsert(cmd)
	}
}

// executeLPush prepends values to a list
func (p *Processor) executeLPush(cmd *Command) {
	values := cmd.Args[0].([]string)
	result, err := p.store.LPush(cmd.Key, values...)
	cmd.Response <- IntResult{Result: result, Err: err}
}

// executeRPush appends values to a list
func (p *Processor) executeRPush(cmd *Command) {
	values := cmd.Args[0].([]string)
	result, err := p.store.RPush(cmd.Key, values...)
	cmd.Response <- IntResult{Result: result, Err: err}
}

// executeLPop removes and returns elements from the head of a list
func (p *Processor) executeLPop(cmd *Command) {
	count := cmd.Args[0].(int)
	result, err := p.store.LPop(cmd.Key, count)
	cmd.Response <- StringSliceResult{Result: result, Err: err}
}

// executeRPop removes and returns elements from the tail of a list
func (p *Processor) executeRPop(cmd *Command) {
	count := cmd.Args[0].(int)
	result, err := p.store.RPop(cmd.Key, count)
	cmd.Response <- StringSliceResult{Result: result, Err: err}
}

// executeLLen returns the length of a list
func (p *Processor) executeLLen(cmd *Command) {
	result, err := p.store.LLen(cmd.Key)
	cmd.Response <- IntResult{Result: result, Err: err}
}

// executeLRange returns a range of elements from a list
func (p *Processor) executeLRange(cmd *Command) {
	start := cmd.Args[0].(int)
	stop := cmd.Args[1].(int)
	result, err := p.store.LRange(cmd.Key, start, stop)
	cmd.Response <- StringSliceResult{Result: result, Err: err}
}

// executeLIndex returns an element by index from a list
func (p *Processor) executeLIndex(cmd *Command) {
	index := cmd.Args[0].(int)
	val, exists, err := p.store.LIndex(cmd.Key, index)
	cmd.Response <- IndexResult{Value: val, Exists: exists, Err: err}
}

// executeLSet sets the value at an index in a list
func (p *Processor) executeLSet(cmd *Command) {
	index := cmd.Args[0].(int)
	value := cmd.Args[1].(string)
	err := p.store.LSet(cmd.Key, index, value)
	cmd.Response <- err
}

// executeLRem removes elements from a list
func (p *Processor) executeLRem(cmd *Command) {
	count := cmd.Args[0].(int)
	value := cmd.Args[1].(string)
	result, err := p.store.LRem(cmd.Key, count, value)
	cmd.Response <- IntResult{Result: result, Err: err}
}

// executeLTrim trims a list to the specified range
func (p *Processor) executeLTrim(cmd *Command) {
	start := cmd.Args[0].(int)
	stop := cmd.Args[1].(int)
	err := p.store.LTrim(cmd.Key, start, stop)
	cmd.Response <- err
}

// executeLInsert inserts an element before or after a pivot
func (p *Processor) executeLInsert(cmd *Command) {
	before := cmd.Args[0].(bool)
	pivot := cmd.Args[1].(string)
	value := cmd.Args[2].(string)
	result, err := p.store.LInsert(cmd.Key, before, pivot, value)
	cmd.Response <- IntResult{Result: result, Err: err}
}
