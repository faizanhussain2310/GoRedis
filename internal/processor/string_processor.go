package processor

// executeStringCommand handles string/basic commands
func (p *Processor) executeStringCommand(cmd *Command) {
	switch cmd.Type {
	case CmdSet:
		p.executeSet(cmd)
	case CmdGet:
		p.executeGet(cmd)
	case CmdDelete:
		p.executeDelete(cmd)
	case CmdExists:
		p.executeExists(cmd)
	case CmdKeys:
		p.executeKeys(cmd)
	case CmdFlush:
		p.executeFlush(cmd)
	case CmdCleanup:
		p.executeCleanup(cmd)
	case CmdExpire:
		p.executeExpire(cmd)
	case CmdTTL:
		p.executeTTL(cmd)
	case CmdIncr:
		p.executeIncr(cmd)
	case CmdIncrBy:
		p.executeIncrBy(cmd)
	case CmdDecr:
		p.executeDecr(cmd)
	case CmdDecrBy:
		p.executeDecrBy(cmd)
	}
}

// executeSet sets a key-value pair
func (p *Processor) executeSet(cmd *Command) {
	p.store.Set(cmd.Key, cmd.Value, cmd.Expiry)
	cmd.Response <- true
}

// executeGet retrieves a value by key
func (p *Processor) executeGet(cmd *Command) {
	val, exists := p.store.Get(cmd.Key)
	cmd.Response <- GetResult{Value: val, Exists: exists}
}

// executeDelete deletes one or more keys
func (p *Processor) executeDelete(cmd *Command) {
	result := p.store.Delete(cmd.Key)
	cmd.Response <- result
}

// executeExists checks if a key exists
func (p *Processor) executeExists(cmd *Command) {
	result := p.store.Exists(cmd.Key)
	cmd.Response <- result
}

// executeKeys returns all keys matching pattern
func (p *Processor) executeKeys(cmd *Command) {
	keys := p.store.Keys()
	cmd.Response <- keys
}

// executeFlush clears all keys
func (p *Processor) executeFlush(cmd *Command) {
	p.store.Flush()
	cmd.Response <- true
}

// executeCleanup removes expired keys
func (p *Processor) executeCleanup(cmd *Command) {
	p.store.CleanupExpiredKeys()
	cmd.Response <- true
}

// executeExpire sets expiry on a key
func (p *Processor) executeExpire(cmd *Command) {
	result := p.store.Expire(cmd.Key, cmd.Expiry)
	cmd.Response <- result
}

// executeTTL returns time-to-live for a key
func (p *Processor) executeTTL(cmd *Command) {
	ttl := p.store.TTL(cmd.Key)
	cmd.Response <- ttl
}

// executeIncr increments the integer value by 1
func (p *Processor) executeIncr(cmd *Command) {
	result, err := p.store.Incr(cmd.Key)
	cmd.Response <- Int64Result{Result: result, Err: err}
}

// executeIncrBy increments the integer value by given amount
func (p *Processor) executeIncrBy(cmd *Command) {
	increment := cmd.Value.(int64)
	result, err := p.store.IncrBy(cmd.Key, increment)
	cmd.Response <- Int64Result{Result: result, Err: err}
}

// executeDecr decrements the integer value by 1
func (p *Processor) executeDecr(cmd *Command) {
	result, err := p.store.Decr(cmd.Key)
	cmd.Response <- Int64Result{Result: result, Err: err}
}

// executeDecrBy decrements the integer value by given amount
func (p *Processor) executeDecrBy(cmd *Command) {
	decrement := cmd.Value.(int64)
	result, err := p.store.DecrBy(cmd.Key, decrement)
	cmd.Response <- Int64Result{Result: result, Err: err}
}
