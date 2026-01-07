package processor

// executeHashCommand handles hash commands
func (p *Processor) executeHashCommand(cmd *Command) {
	switch cmd.Type {
	case CmdHSet:
		p.executeHSet(cmd)
	case CmdHGet:
		p.executeHGet(cmd)
	case CmdHMGet:
		p.executeHMGet(cmd)
	case CmdHDel:
		p.executeHDel(cmd)
	case CmdHExists:
		p.executeHExists(cmd)
	case CmdHLen:
		p.executeHLen(cmd)
	case CmdHKeys:
		p.executeHKeys(cmd)
	case CmdHVals:
		p.executeHVals(cmd)
	case CmdHGetAll:
		p.executeHGetAll(cmd)
	case CmdHSetNX:
		p.executeHSetNX(cmd)
	case CmdHIncrBy:
		p.executeHIncrBy(cmd)
	case CmdHIncrByFloat:
		p.executeHIncrByFloat(cmd)
	}
}

// executeHSet sets field-value pairs in a hash
func (p *Processor) executeHSet(cmd *Command) {
	fieldValues := cmd.Args[0].([]string)
	result, err := p.store.HSet(cmd.Key, fieldValues...)
	cmd.Response <- IntResult{Result: result, Err: err}
}

// executeHGet retrieves a field value from a hash
func (p *Processor) executeHGet(cmd *Command) {
	field := cmd.Args[0].(string)
	val, exists, err := p.store.HGet(cmd.Key, field)
	cmd.Response <- IndexResult{Value: val, Exists: exists, Err: err}
}

// executeHMGet retrieves multiple field values from a hash
func (p *Processor) executeHMGet(cmd *Command) {
	fields := cmd.Args[0].([]string)
	result, err := p.store.HMGet(cmd.Key, fields...)
	cmd.Response <- InterfaceSliceResult{Result: result, Err: err}
}

// executeHDel deletes fields from a hash
func (p *Processor) executeHDel(cmd *Command) {
	fields := cmd.Args[0].([]string)
	result, err := p.store.HDel(cmd.Key, fields...)
	cmd.Response <- IntResult{Result: result, Err: err}
}

// executeHExists checks if a field exists in a hash
func (p *Processor) executeHExists(cmd *Command) {
	field := cmd.Args[0].(string)
	result, err := p.store.HExists(cmd.Key, field)
	cmd.Response <- BoolResult{Result: result, Err: err}
}

// executeHLen returns the number of fields in a hash
func (p *Processor) executeHLen(cmd *Command) {
	result, err := p.store.HLen(cmd.Key)
	cmd.Response <- IntResult{Result: result, Err: err}
}

// executeHKeys returns all field names in a hash
func (p *Processor) executeHKeys(cmd *Command) {
	result, err := p.store.HKeys(cmd.Key)
	cmd.Response <- StringSliceResult{Result: result, Err: err}
}

// executeHVals returns all values in a hash
func (p *Processor) executeHVals(cmd *Command) {
	result, err := p.store.HVals(cmd.Key)
	cmd.Response <- StringSliceResult{Result: result, Err: err}
}

// executeHGetAll returns all field-value pairs in a hash
func (p *Processor) executeHGetAll(cmd *Command) {
	result, err := p.store.HGetAll(cmd.Key)
	cmd.Response <- StringSliceResult{Result: result, Err: err}
}

// executeHSetNX sets a field only if it doesn't exist
func (p *Processor) executeHSetNX(cmd *Command) {
	field := cmd.Args[0].(string)
	value := cmd.Args[1].(string)
	result, err := p.store.HSetNX(cmd.Key, field, value)
	cmd.Response <- BoolResult{Result: result, Err: err}
}

// executeHIncrBy increments a hash field by an integer
func (p *Processor) executeHIncrBy(cmd *Command) {
	field := cmd.Args[0].(string)
	increment := cmd.Args[1].(int64)
	result, err := p.store.HIncrBy(cmd.Key, field, increment)
	cmd.Response <- Int64Result{Result: result, Err: err}
}

// executeHIncrByFloat increments a hash field by a float
func (p *Processor) executeHIncrByFloat(cmd *Command) {
	field := cmd.Args[0].(string)
	increment := cmd.Args[1].(float64)
	result, err := p.store.HIncrByFloat(cmd.Key, field, increment)
	cmd.Response <- Float64Result{Result: result, Err: err}
}
