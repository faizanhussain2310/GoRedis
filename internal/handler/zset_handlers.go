package handler

import (
	"fmt"
	"strconv"

	"redis/internal/processor"
	"redis/internal/protocol"
	"redis/internal/storage"
)

// handleZAdd adds members with scores to a sorted set
// ZADD key score1 member1 [score2 member2 ...]
func (h *CommandHandler) handleZAdd(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 || len(cmd.Args)%2 != 0 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zadd' command")
	}

	key := cmd.Args[1]
	members := make([]storage.ZSetMember, 0)

	// Parse score-member pairs
	for i := 2; i < len(cmd.Args); i += 2 {
		score, err := strconv.ParseFloat(cmd.Args[i], 64)
		if err != nil {
			return protocol.EncodeError("ERR value is not a valid float")
		}
		member := cmd.Args[i+1]
		members = append(members, storage.ZSetMember{Member: member, Score: score})
	}

	procCmd := &processor.Command{
		Type:     processor.CmdZAdd,
		Key:      key,
		Args:     []interface{}{members},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	added := result.(processor.IntResult).Result
	return protocol.EncodeInteger(added)
}

// handleZRem removes members from a sorted set
// ZREM key member1 [member2 ...]
func (h *CommandHandler) handleZRem(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zrem' command")
	}

	key := cmd.Args[1]
	members := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdZRem,
		Key:      key,
		Args:     []interface{}{members},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	removed := result.(processor.IntResult).Result
	return protocol.EncodeInteger(removed)
}

// handleZScore returns the score of a member
// ZSCORE key member
func (h *CommandHandler) handleZScore(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zscore' command")
	}

	key := cmd.Args[1]
	member := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdZScore,
		Key:      key,
		Args:     []interface{}{member},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	scoreResult := result.(processor.Float64Result)
	if scoreResult.Err != nil {
		return protocol.EncodeNullBulkString()
	}

	return protocol.EncodeBulkString(fmt.Sprintf("%.17g", scoreResult.Result))
}

// handleZRank returns the rank of a member (ascending)
// ZRANK key member
func (h *CommandHandler) handleZRank(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zrank' command")
	}

	key := cmd.Args[1]
	member := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdZRank,
		Key:      key,
		Args:     []interface{}{member},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	rank := result.(processor.IntResult).Result
	if rank == -1 {
		return protocol.EncodeNullBulkString()
	}

	return protocol.EncodeInteger(rank)
}

// handleZRevRank returns the rank of a member (descending)
// ZREVRANK key member
func (h *CommandHandler) handleZRevRank(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zrevrank' command")
	}

	key := cmd.Args[1]
	member := cmd.Args[2]

	procCmd := &processor.Command{
		Type:     processor.CmdZRevRank,
		Key:      key,
		Args:     []interface{}{member},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	rank := result.(processor.IntResult).Result
	if rank == -1 {
		return protocol.EncodeNullBulkString()
	}

	return protocol.EncodeInteger(rank)
}

// handleZCard returns the number of members in a sorted set
// ZCARD key
func (h *CommandHandler) handleZCard(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zcard' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdZCard,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	count := result.(processor.IntResult).Result
	return protocol.EncodeInteger(count)
}

// handleZRange returns members by rank range
// ZRANGE key start stop [WITHSCORES]
func (h *CommandHandler) handleZRange(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zrange' command")
	}

	key := cmd.Args[1]
	start, err1 := strconv.Atoi(cmd.Args[2])
	stop, err2 := strconv.Atoi(cmd.Args[3])

	if err1 != nil || err2 != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	withScores := false
	if len(cmd.Args) > 4 && cmd.Args[4] == "WITHSCORES" {
		withScores = true
	}

	procCmd := &processor.Command{
		Type:     processor.CmdZRange,
		Key:      key,
		Args:     []interface{}{start, stop, withScores},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	members := result.([]storage.ZSetMember)
	return encodeZSetMembers(members, withScores)
}

// handleZRevRange returns members by rank range in descending order
// ZREVRANGE key start stop [WITHSCORES]
func (h *CommandHandler) handleZRevRange(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zrevrange' command")
	}

	key := cmd.Args[1]
	start, err1 := strconv.Atoi(cmd.Args[2])
	stop, err2 := strconv.Atoi(cmd.Args[3])

	if err1 != nil || err2 != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	withScores := false
	if len(cmd.Args) > 4 && cmd.Args[4] == "WITHSCORES" {
		withScores = true
	}

	procCmd := &processor.Command{
		Type:     processor.CmdZRevRange,
		Key:      key,
		Args:     []interface{}{start, stop, withScores},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	members := result.([]storage.ZSetMember)
	return encodeZSetMembers(members, withScores)
}

// handleZRangeByScore returns members by score range
// ZRANGEBYSCORE key min max [LIMIT offset count]
func (h *CommandHandler) handleZRangeByScore(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zrangebyscore' command")
	}

	key := cmd.Args[1]
	min, err1 := strconv.ParseFloat(cmd.Args[2], 64)
	max, err2 := strconv.ParseFloat(cmd.Args[3], 64)

	if err1 != nil || err2 != nil {
		return protocol.EncodeError("ERR min or max is not a float")
	}

	offset := 0
	count := -1

	// Parse LIMIT clause
	for i := 4; i < len(cmd.Args); i++ {
		if cmd.Args[i] == "LIMIT" && i+2 < len(cmd.Args) {
			offset, _ = strconv.Atoi(cmd.Args[i+1])
			count, _ = strconv.Atoi(cmd.Args[i+2])
			break
		}
	}

	procCmd := &processor.Command{
		Type:     processor.CmdZRangeByScore,
		Key:      key,
		Args:     []interface{}{min, max, offset, count},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	members := result.([]storage.ZSetMember)
	return encodeZSetMembers(members, false)
}

// handleZRevRangeByScore returns members by score range in descending order
func (h *CommandHandler) handleZRevRangeByScore(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zrevrangebyscore' command")
	}

	key := cmd.Args[1]
	max, err1 := strconv.ParseFloat(cmd.Args[2], 64)
	min, err2 := strconv.ParseFloat(cmd.Args[3], 64)

	if err1 != nil || err2 != nil {
		return protocol.EncodeError("ERR min or max is not a float")
	}

	offset := 0
	count := -1

	for i := 4; i < len(cmd.Args); i++ {
		if cmd.Args[i] == "LIMIT" && i+2 < len(cmd.Args) {
			offset, _ = strconv.Atoi(cmd.Args[i+1])
			count, _ = strconv.Atoi(cmd.Args[i+2])
			break
		}
	}

	procCmd := &processor.Command{
		Type:     processor.CmdZRevRangeByScore,
		Key:      key,
		Args:     []interface{}{min, max, offset, count},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	members := result.([]storage.ZSetMember)
	return encodeZSetMembers(members, false)
}

// handleZIncrBy increments the score of a member
// ZINCRBY key increment member
func (h *CommandHandler) handleZIncrBy(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zincrby' command")
	}

	key := cmd.Args[1]
	delta, err := strconv.ParseFloat(cmd.Args[2], 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not a valid float")
	}
	member := cmd.Args[3]

	procCmd := &processor.Command{
		Type:     processor.CmdZIncrBy,
		Key:      key,
		Args:     []interface{}{delta, member},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	scoreResult := result.(processor.Float64Result)
	if scoreResult.Err != nil {
		return protocol.EncodeError(scoreResult.Err.Error())
	}

	return protocol.EncodeBulkString(fmt.Sprintf("%.17g", scoreResult.Result))
}

// handleZCount returns the count of members with scores in range
// ZCOUNT key min max
func (h *CommandHandler) handleZCount(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zcount' command")
	}

	key := cmd.Args[1]
	min, err1 := strconv.ParseFloat(cmd.Args[2], 64)
	max, err2 := strconv.ParseFloat(cmd.Args[3], 64)

	if err1 != nil || err2 != nil {
		return protocol.EncodeError("ERR min or max is not a float")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdZCount,
		Key:      key,
		Args:     []interface{}{min, max},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	count := result.(processor.IntResult).Result
	return protocol.EncodeInteger(count)
}

// handleZPopMin removes and returns the member with lowest score
// ZPOPMIN key
func (h *CommandHandler) handleZPopMin(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zpopmin' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdZPopMin,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	member := result.(*storage.ZSetMember)
	if member == nil {
		return protocol.EncodeArray([]string{})
	}

	return protocol.EncodeArray([]string{member.Member, fmt.Sprintf("%.17g", member.Score)})
}

// handleZPopMax removes and returns the member with highest score
// ZPOPMAX key
func (h *CommandHandler) handleZPopMax(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zpopmax' command")
	}

	key := cmd.Args[1]

	procCmd := &processor.Command{
		Type:     processor.CmdZPopMax,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	member := result.(*storage.ZSetMember)
	if member == nil {
		return protocol.EncodeArray([]string{})
	}

	return protocol.EncodeArray([]string{member.Member, fmt.Sprintf("%.17g", member.Score)})
}

// handleZRemRangeByScore removes members with scores in range
// ZREMRANGEBYSCORE key min max
func (h *CommandHandler) handleZRemRangeByScore(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zremrangebyscore' command")
	}

	key := cmd.Args[1]
	min, err1 := strconv.ParseFloat(cmd.Args[2], 64)
	max, err2 := strconv.ParseFloat(cmd.Args[3], 64)

	if err1 != nil || err2 != nil {
		return protocol.EncodeError("ERR min or max is not a float")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdZRemRangeByScore,
		Key:      key,
		Args:     []interface{}{min, max},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	removed := result.(processor.IntResult).Result
	return protocol.EncodeInteger(removed)
}

// handleZRemRangeByRank removes members in rank range
// ZREMRANGEBYRANK key start stop
func (h *CommandHandler) handleZRemRangeByRank(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'zremrangebyrank' command")
	}

	key := cmd.Args[1]
	start, err1 := strconv.Atoi(cmd.Args[2])
	stop, err2 := strconv.Atoi(cmd.Args[3])

	if err1 != nil || err2 != nil {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdZRemRangeByRank,
		Key:      key,
		Args:     []interface{}{start, stop},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	removed := result.(processor.IntResult).Result
	return protocol.EncodeInteger(removed)
}

// encodeZSetMembers encodes sorted set members for RESP protocol
func encodeZSetMembers(members []storage.ZSetMember, withScores bool) []byte {
	if members == nil {
		return protocol.EncodeArray([]string{})
	}

	if withScores {
		result := make([]string, 0, len(members)*2)
		for _, member := range members {
			result = append(result, member.Member)
			result = append(result, fmt.Sprintf("%.17g", member.Score))
		}
		return protocol.EncodeArray(result)
	}

	result := make([]string, 0, len(members))
	for _, member := range members {
		result = append(result, member.Member)
	}
	return protocol.EncodeArray(result)
}
