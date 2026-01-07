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
