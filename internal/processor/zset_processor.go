package processor

import "redis/internal/storage"

// executeZSetCommand executes sorted set commands
func (p *Processor) executeZSetCommand(cmd *Command) {
	switch cmd.Type {
	case CmdZAdd:
		p.executeZAdd(cmd)
	case CmdZRem:
		p.executeZRem(cmd)
	case CmdZScore:
		p.executeZScore(cmd)
	case CmdZRank:
		p.executeZRank(cmd)
	case CmdZRevRank:
		p.executeZRevRank(cmd)
	case CmdZCard:
		p.executeZCard(cmd)
	case CmdZRange:
		p.executeZRange(cmd)
	case CmdZRevRange:
		p.executeZRevRange(cmd)
	case CmdZRangeByScore:
		p.executeZRangeByScore(cmd)
	case CmdZRevRangeByScore:
		p.executeZRevRangeByScore(cmd)
	case CmdZIncrBy:
		p.executeZIncrBy(cmd)
	case CmdZCount:
		p.executeZCount(cmd)
	case CmdZPopMin:
		p.executeZPopMin(cmd)
	case CmdZPopMax:
		p.executeZPopMax(cmd)
	case CmdZRemRangeByScore:
		p.executeZRemRangeByScore(cmd)
	case CmdZRemRangeByRank:
		p.executeZRemRangeByRank(cmd)
	default:
		cmd.Response <- IntResult{Result: 0, Err: nil}
	}
}

// executeZAdd adds one or more members with scores to a sorted set
func (p *Processor) executeZAdd(cmd *Command) {
	members := cmd.Args[0].([]storage.ZSetMember)
	count := p.store.ZAdd(cmd.Key, members)
	cmd.Response <- IntResult{Result: count}
}

// executeZRem removes one or more members from a sorted set
func (p *Processor) executeZRem(cmd *Command) {
	members := cmd.Args[0].([]string)
	count := p.store.ZRem(cmd.Key, members)
	cmd.Response <- IntResult{Result: count}
}

// executeZScore returns the score of a member in a sorted set
func (p *Processor) executeZScore(cmd *Command) {
	member := cmd.Args[0].(string)
	score := p.store.ZScore(cmd.Key, member)
	if score == nil {
		cmd.Response <- Float64Result{Result: 0, Err: nil}
	} else {
		cmd.Response <- Float64Result{Result: *score, Err: nil}
	}
}

// executeZRank returns the rank of a member in a sorted set (ascending order)
func (p *Processor) executeZRank(cmd *Command) {
	member := cmd.Args[0].(string)
	rank := p.store.ZRank(cmd.Key, member)
	cmd.Response <- IntResult{Result: rank}
}

// executeZRevRank returns the rank of a member in a sorted set (descending order)
func (p *Processor) executeZRevRank(cmd *Command) {
	member := cmd.Args[0].(string)
	rank := p.store.ZRevRank(cmd.Key, member)
	cmd.Response <- IntResult{Result: rank}
}

// executeZCard returns the number of members in a sorted set
func (p *Processor) executeZCard(cmd *Command) {
	count := p.store.ZCard(cmd.Key)
	cmd.Response <- IntResult{Result: count}
}

// executeZRange returns members in a sorted set by rank range
func (p *Processor) executeZRange(cmd *Command) {
	start := cmd.Args[0].(int)
	stop := cmd.Args[1].(int)
	withScores := false
	if len(cmd.Args) > 2 {
		withScores = cmd.Args[2].(bool)
	}
	members := p.store.ZRange(cmd.Key, start, stop, withScores)
	cmd.Response <- members
}

// executeZRevRange returns members in a sorted set by rank range in descending order
func (p *Processor) executeZRevRange(cmd *Command) {
	start := cmd.Args[0].(int)
	stop := cmd.Args[1].(int)
	withScores := false
	if len(cmd.Args) > 2 {
		withScores = cmd.Args[2].(bool)
	}
	members := p.store.ZRevRange(cmd.Key, start, stop, withScores)
	cmd.Response <- members
}

// executeZRangeByScore returns members with scores in range [min, max]
func (p *Processor) executeZRangeByScore(cmd *Command) {
	min := cmd.Args[0].(float64)
	max := cmd.Args[1].(float64)
	offset := 0
	count := -1
	if len(cmd.Args) > 2 {
		offset = cmd.Args[2].(int)
	}
	if len(cmd.Args) > 3 {
		count = cmd.Args[3].(int)
	}
	members := p.store.ZRangeByScore(cmd.Key, min, max, offset, count)
	cmd.Response <- members
}

// executeZRevRangeByScore returns members with scores in range [min, max] in descending order
func (p *Processor) executeZRevRangeByScore(cmd *Command) {
	min := cmd.Args[0].(float64)
	max := cmd.Args[1].(float64)
	offset := 0
	count := -1
	if len(cmd.Args) > 2 {
		offset = cmd.Args[2].(int)
	}
	if len(cmd.Args) > 3 {
		count = cmd.Args[3].(int)
	}
	members := p.store.ZRevRangeByScore(cmd.Key, min, max, offset, count)
	cmd.Response <- members
}

// executeZIncrBy increments the score of a member by delta
func (p *Processor) executeZIncrBy(cmd *Command) {
	delta := cmd.Args[0].(float64)
	member := cmd.Args[1].(string)
	newScore, err := p.store.ZIncrBy(cmd.Key, delta, member)
	cmd.Response <- Float64Result{Result: newScore, Err: err}
}

// executeZCount returns the number of members with scores in range [min, max]
func (p *Processor) executeZCount(cmd *Command) {
	min := cmd.Args[0].(float64)
	max := cmd.Args[1].(float64)
	count := p.store.ZCount(cmd.Key, min, max)
	cmd.Response <- IntResult{Result: count}
}

// executeZPopMin removes and returns the member with the lowest score
func (p *Processor) executeZPopMin(cmd *Command) {
	member := p.store.ZPopMin(cmd.Key)
	cmd.Response <- member
}

// executeZPopMax removes and returns the member with the highest score
func (p *Processor) executeZPopMax(cmd *Command) {
	member := p.store.ZPopMax(cmd.Key)
	cmd.Response <- member
}

// executeZRemRangeByScore removes all members with scores in range [min, max]
func (p *Processor) executeZRemRangeByScore(cmd *Command) {
	min := cmd.Args[0].(float64)
	max := cmd.Args[1].(float64)
	count := p.store.ZRemRangeByScore(cmd.Key, min, max)
	cmd.Response <- IntResult{Result: count}
}

// executeZRemRangeByRank removes all members in rank range [start, stop]
func (p *Processor) executeZRemRangeByRank(cmd *Command) {
	start := cmd.Args[0].(int)
	stop := cmd.Args[1].(int)
	count := p.store.ZRemRangeByRank(cmd.Key, start, stop)
	cmd.Response <- IntResult{Result: count}
}
