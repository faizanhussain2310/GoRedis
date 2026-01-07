package processor

// executeSetCommand handles all set-related commands
func (p *Processor) executeSetCommand(cmd *Command) {
	switch cmd.Type {
	case CmdSAdd:
		p.executeSAdd(cmd)
	case CmdSRem:
		p.executeSRem(cmd)
	case CmdSIsMember:
		p.executeSIsMember(cmd)
	case CmdSMembers:
		p.executeSMembers(cmd)
	case CmdSCard:
		p.executeSCard(cmd)
	case CmdSPop:
		p.executeSPop(cmd)
	case CmdSRandMember:
		p.executeSRandMember(cmd)
	case CmdSUnion:
		p.executeSUnion(cmd)
	case CmdSInter:
		p.executeSInter(cmd)
	case CmdSDiff:
		p.executeSDiff(cmd)
	case CmdSMove:
		p.executeSMove(cmd)
	case CmdSUnionStore:
		p.executeSUnionStore(cmd)
	case CmdSInterStore:
		p.executeSInterStore(cmd)
	case CmdSDiffStore:
		p.executeSDiffStore(cmd)
	}
}

// executeSAdd adds members to a set
func (p *Processor) executeSAdd(cmd *Command) {
	members := cmd.Args[0].([]string)
	result := p.store.SAdd(cmd.Key, members...)
	cmd.Response <- IntResult{Result: result, Err: nil}
}

// executeSRem removes members from a set
func (p *Processor) executeSRem(cmd *Command) {
	members := cmd.Args[0].([]string)
	result := p.store.SRem(cmd.Key, members...)
	cmd.Response <- IntResult{Result: result, Err: nil}
}

// executeSIsMember checks if a member exists in a set
func (p *Processor) executeSIsMember(cmd *Command) {
	member := cmd.Value.(string)
	result := p.store.SIsMember(cmd.Key, member)
	cmd.Response <- BoolResult{Result: result, Err: nil}
}

// executeSMembers returns all members of a set
func (p *Processor) executeSMembers(cmd *Command) {
	result := p.store.SMembers(cmd.Key)
	cmd.Response <- StringSliceResult{Result: result, Err: nil}
}

// executeSCard returns the cardinality (size) of a set
func (p *Processor) executeSCard(cmd *Command) {
	result := p.store.SCard(cmd.Key)
	cmd.Response <- IntResult{Result: result, Err: nil}
}

// executeSPop removes and returns random members from a set
func (p *Processor) executeSPop(cmd *Command) {
	count := 1
	if len(cmd.Args) > 0 {
		count = cmd.Args[0].(int)
	}
	result := p.store.SPop(cmd.Key, count)
	cmd.Response <- StringSliceResult{Result: result, Err: nil}
}

// executeSRandMember returns random members from a set without removing
func (p *Processor) executeSRandMember(cmd *Command) {
	count := 1
	if len(cmd.Args) > 0 {
		count = cmd.Args[0].(int)
	}
	result := p.store.SRandMember(cmd.Key, count)
	cmd.Response <- StringSliceResult{Result: result, Err: nil}
}

// executeSUnion returns the union of multiple sets
func (p *Processor) executeSUnion(cmd *Command) {
	keys := cmd.Args[0].([]string)
	result := p.store.SUnion(keys...)
	cmd.Response <- StringSliceResult{Result: result, Err: nil}
}

// executeSInter returns the intersection of multiple sets
func (p *Processor) executeSInter(cmd *Command) {
	keys := cmd.Args[0].([]string)
	result := p.store.SInter(keys...)
	cmd.Response <- StringSliceResult{Result: result, Err: nil}
}

// executeSDiff returns the difference between the first set and subsequent sets
func (p *Processor) executeSDiff(cmd *Command) {
	keys := cmd.Args[0].([]string)
	result := p.store.SDiff(keys...)
	cmd.Response <- StringSliceResult{Result: result, Err: nil}
}

// executeSMove moves a member from one set to another
func (p *Processor) executeSMove(cmd *Command) {
	srcKey := cmd.Key
	destKey := cmd.Args[0].(string)
	member := cmd.Args[1].(string)
	result := p.store.SMove(srcKey, destKey, member)
	cmd.Response <- BoolResult{Result: result, Err: nil}
}

// executeSUnionStore stores the union of multiple sets in a destination key
func (p *Processor) executeSUnionStore(cmd *Command) {
	destKey := cmd.Key
	keys := cmd.Args[0].([]string)
	result := p.store.SUnionStore(destKey, keys...)
	cmd.Response <- IntResult{Result: result, Err: nil}
}

// executeSInterStore stores the intersection of multiple sets in a destination key
func (p *Processor) executeSInterStore(cmd *Command) {
	destKey := cmd.Key
	keys := cmd.Args[0].([]string)
	result := p.store.SInterStore(destKey, keys...)
	cmd.Response <- IntResult{Result: result, Err: nil}
}

// executeSDiffStore stores the difference of sets in a destination key
func (p *Processor) executeSDiffStore(cmd *Command) {
	destKey := cmd.Key
	keys := cmd.Args[0].([]string)
	result := p.store.SDiffStore(destKey, keys...)
	cmd.Response <- IntResult{Result: result, Err: nil}
}
