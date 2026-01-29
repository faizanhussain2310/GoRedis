package lua

import (
	"fmt"
	"redis/internal/storage"
	"strconv"
	"strings"
	"time"
)

// RedisExecutor implements RedisCommandExecutor for actual Redis operations
type RedisExecutor struct {
	store *storage.Store
}

// NewRedisExecutor creates a new Redis command executor for Lua
func NewRedisExecutor(store *storage.Store) *RedisExecutor {
	return &RedisExecutor{
		store: store,
	}
}

// ExecuteCommand executes a Redis command and returns the result
func (r *RedisExecutor) ExecuteCommand(cmdName string, args ...interface{}) (interface{}, error) {
	// Convert command name to uppercase
	cmdName = strings.ToUpper(cmdName)

	// Convert interface{} args to strings
	stringArgs := make([]string, len(args))
	for i, arg := range args {
		stringArgs[i] = fmt.Sprintf("%v", arg)
	}

	// Execute the command directly on the store
	// Storage layer handles expiration checks internally for all read operations
	switch cmdName {
	// ==================== STRING COMMANDS ====================
	case "GET":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'get' command")
		}
		value, exists := r.store.Get(stringArgs[0])
		if !exists {
			return nil, nil
		}
		return value, nil

	case "SET":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'set' command")
		}
		r.store.Set(stringArgs[0], stringArgs[1], nil)
		return "OK", nil

	case "DEL":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'del' command")
		}
		count := 0
		for _, key := range stringArgs {
			if r.store.Delete(key) {
				count++
			}
		}
		return int64(count), nil

	case "EXISTS":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'exists' command")
		}
		count := int64(0)
		for _, key := range stringArgs {
			if r.store.Exists(key) {
				count++
			}
		}
		return count, nil

	case "INCR":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'incr' command")
		}
		return r.increment(stringArgs[0], 1)

	case "DECR":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'decr' command")
		}
		return r.increment(stringArgs[0], -1)

	case "INCRBY":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'incrby' command")
		}
		delta, err := strconv.ParseInt(stringArgs[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		return r.increment(stringArgs[0], delta)

	case "DECRBY":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'decrby' command")
		}
		delta, err := strconv.ParseInt(stringArgs[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		return r.increment(stringArgs[0], -delta)

	case "APPEND":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'append' command")
		}
		value, exists := r.store.Get(stringArgs[0])
		var current string
		if exists {
			current, _ = value.(string)
		}
		newValue := current + stringArgs[1]
		r.store.Set(stringArgs[0], newValue, nil)
		return int64(len(newValue)), nil

	case "STRLEN":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'strlen' command")
		}
		value, exists := r.store.Get(stringArgs[0])
		if !exists {
			return int64(0), nil
		}
		if str, ok := value.(string); ok {
			return int64(len(str)), nil
		}
		return int64(0), nil

	case "GETRANGE":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'getrange' command")
		}
		value, exists := r.store.Get(stringArgs[0])
		if !exists {
			return "", nil
		}
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		start, err := strconv.Atoi(stringArgs[1])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		end, err := strconv.Atoi(stringArgs[2])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}

		// Handle negative indices
		length := len(str)
		if start < 0 {
			start = length + start
		}
		if end < 0 {
			end = length + end
		}

		// Clamp indices
		if start < 0 {
			start = 0
		}
		if end >= length {
			end = length - 1
		}
		if start > end || start >= length {
			return "", nil
		}

		return str[start : end+1], nil

	case "SETRANGE":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'setrange' command")
		}
		offset, err := strconv.Atoi(stringArgs[1])
		if err != nil || offset < 0 {
			return nil, fmt.Errorf("ERR offset is out of range")
		}

		value, exists := r.store.Get(stringArgs[0])
		var current string
		if exists {
			current, _ = value.(string)
		}

		// Extend string with nulls if needed
		if offset > len(current) {
			current = current + string(make([]byte, offset-len(current)))
		}

		newValue := stringArgs[2]
		if offset == 0 {
			result := newValue + current[len(newValue):]
			r.store.Set(stringArgs[0], result, nil)
			return int64(len(result)), nil
		}

		result := current[:offset] + newValue
		if offset+len(newValue) < len(current) {
			result += current[offset+len(newValue):]
		}
		r.store.Set(stringArgs[0], result, nil)
		return int64(len(result)), nil

	case "MGET":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'mget' command")
		}
		result := make([]interface{}, len(stringArgs))
		for i, key := range stringArgs {
			value, exists := r.store.Get(key)
			if exists {
				result[i] = value
			} else {
				result[i] = nil
			}
		}
		return result, nil

	case "MSET":
		if len(stringArgs) < 2 || len(stringArgs)%2 != 0 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'mset' command")
		}
		for i := 0; i < len(stringArgs); i += 2 {
			r.store.Set(stringArgs[i], stringArgs[i+1], nil)
		}
		return "OK", nil

	// ==================== LIST COMMANDS ====================
	case "LPUSH":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'lpush' command")
		}
		count, _ := r.store.LPush(stringArgs[0], stringArgs[1:]...)
		return int64(count), nil

	case "RPUSH":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'rpush' command")
		}
		count, _ := r.store.RPush(stringArgs[0], stringArgs[1:]...)
		return int64(count), nil

	case "LPOP":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'lpop' command")
		}
		values, err := r.store.LPop(stringArgs[0], 1)
		if err != nil || len(values) == 0 {
			return nil, nil
		}
		return values[0], nil

	case "RPOP":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'rpop' command")
		}
		values, err := r.store.RPop(stringArgs[0], 1)
		if err != nil || len(values) == 0 {
			return nil, nil
		}
		return values[0], nil

	case "LLEN":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'llen' command")
		}
		length, _ := r.store.LLen(stringArgs[0])
		return int64(length), nil

	case "LRANGE":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'lrange' command")
		}
		start, err := strconv.Atoi(stringArgs[1])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		stop, err := strconv.Atoi(stringArgs[2])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		values, _ := r.store.LRange(stringArgs[0], start, stop)
		result := make([]interface{}, len(values))
		for i, v := range values {
			result[i] = v
		}
		return result, nil

	case "LINDEX":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'lindex' command")
		}
		index, err := strconv.Atoi(stringArgs[1])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		value, exists, err := r.store.LIndex(stringArgs[0], index)
		if err != nil || !exists {
			return nil, nil
		}
		return value, nil

	case "LSET":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'lset' command")
		}
		index, err := strconv.Atoi(stringArgs[1])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		err = r.store.LSet(stringArgs[0], index, stringArgs[2])
		if err != nil {
			return nil, err
		}
		return "OK", nil

	case "LTRIM":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'ltrim' command")
		}
		start, err := strconv.Atoi(stringArgs[1])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		stop, err := strconv.Atoi(stringArgs[2])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		err = r.store.LTrim(stringArgs[0], start, stop)
		if err != nil {
			return nil, err
		}
		return "OK", nil

	case "LINSERT":
		if len(stringArgs) < 4 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'linsert' command")
		}
		before := strings.ToUpper(stringArgs[1]) == "BEFORE"
		count, err := r.store.LInsert(stringArgs[0], before, stringArgs[2], stringArgs[3])
		if err != nil {
			return nil, err
		}
		return int64(count), nil

	// ==================== HASH COMMANDS ====================
	case "HSET":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hset' command")
		}
		count, _ := r.store.HSet(stringArgs[0], stringArgs[1], stringArgs[2])
		return int64(count), nil

	case "HGET":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hget' command")
		}
		value, exists, _ := r.store.HGet(stringArgs[0], stringArgs[1])
		if !exists {
			return nil, nil
		}
		return value, nil

	case "HDEL":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hdel' command")
		}
		count, _ := r.store.HDel(stringArgs[0], stringArgs[1:]...)
		return int64(count), nil

	case "HGETALL":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hgetall' command")
		}
		hash, _ := r.store.HGetAll(stringArgs[0])
		result := make([]interface{}, len(hash))
		for i, v := range hash {
			result[i] = v
		}
		return result, nil

	case "HEXISTS":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hexists' command")
		}
		exists, _ := r.store.HExists(stringArgs[0], stringArgs[1])
		if exists {
			return int64(1), nil
		}
		return int64(0), nil

	case "HLEN":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hlen' command")
		}
		count, _ := r.store.HLen(stringArgs[0])
		return int64(count), nil

	case "HKEYS":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hkeys' command")
		}
		keys, _ := r.store.HKeys(stringArgs[0])
		result := make([]interface{}, len(keys))
		for i, k := range keys {
			result[i] = k
		}
		return result, nil

	case "HVALS":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hvals' command")
		}
		values, _ := r.store.HVals(stringArgs[0])
		result := make([]interface{}, len(values))
		for i, v := range values {
			result[i] = v
		}
		return result, nil

	case "HINCRBY":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hincrby' command")
		}
		delta, err := strconv.ParseInt(stringArgs[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		newValue, err := r.store.HIncrBy(stringArgs[0], stringArgs[1], delta)
		if err != nil {
			return nil, err
		}
		return newValue, nil

	case "HMGET":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hmget' command")
		}
		result := make([]interface{}, len(stringArgs)-1)
		for i, field := range stringArgs[1:] {
			value, exists, _ := r.store.HGet(stringArgs[0], field)
			if exists {
				result[i] = value
			} else {
				result[i] = nil
			}
		}
		return result, nil

	case "HMSET":
		if len(stringArgs) < 3 || len(stringArgs)%2 == 0 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'hmset' command")
		}
		for i := 1; i < len(stringArgs); i += 2 {
			r.store.HSet(stringArgs[0], stringArgs[i], stringArgs[i+1])
		}
		return "OK", nil

	// ==================== SET COMMANDS ====================
	case "SADD":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'sadd' command")
		}
		count := r.store.SAdd(stringArgs[0], stringArgs[1:]...)
		return int64(count), nil

	case "SREM":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'srem' command")
		}
		count := r.store.SRem(stringArgs[0], stringArgs[1:]...)
		return int64(count), nil

	case "SMEMBERS":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'smembers' command")
		}
		members := r.store.SMembers(stringArgs[0])
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m
		}
		return result, nil

	case "SISMEMBER":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'sismember' command")
		}
		exists := r.store.SIsMember(stringArgs[0], stringArgs[1])
		if exists {
			return int64(1), nil
		}
		return int64(0), nil

	case "SCARD":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'scard' command")
		}
		count := r.store.SCard(stringArgs[0])
		return int64(count), nil

	case "SPOP":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'spop' command")
		}
		count := 1
		if len(stringArgs) > 1 {
			var err error
			count, err = strconv.Atoi(stringArgs[1])
			if err != nil {
				return nil, fmt.Errorf("ERR value is not an integer or out of range")
			}
		}
		members := r.store.SPop(stringArgs[0], count)
		if len(members) == 0 {
			return nil, nil
		}
		if len(stringArgs) == 1 {
			// Single SPOP returns string
			return members[0], nil
		}
		// Multiple SPOP returns array
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m
		}
		return result, nil

	case "SRANDMEMBER":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'srandmember' command")
		}
		count := 1
		if len(stringArgs) > 1 {
			var err error
			count, err = strconv.Atoi(stringArgs[1])
			if err != nil {
				return nil, fmt.Errorf("ERR value is not an integer or out of range")
			}
		}
		members := r.store.SRandMember(stringArgs[0], count)
		if len(members) == 0 {
			return nil, nil
		}
		if len(stringArgs) == 1 {
			// Single SRANDMEMBER returns string
			return members[0], nil
		}
		// Multiple SRANDMEMBER returns array
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m
		}
		return result, nil

	case "SUNION":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'sunion' command")
		}
		members := r.store.SUnion(stringArgs...)
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m
		}
		return result, nil

	case "SINTER":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'sinter' command")
		}
		members := r.store.SInter(stringArgs...)
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m
		}
		return result, nil

	case "SDIFF":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'sdiff' command")
		}
		members := r.store.SDiff(stringArgs...)
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m
		}
		return result, nil

	// ==================== SORTED SET COMMANDS ====================
	case "ZADD":
		if len(stringArgs) < 3 || len(stringArgs)%2 == 0 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zadd' command")
		}
		members := make([]storage.ZSetMember, 0)
		for i := 1; i < len(stringArgs); i += 2 {
			score, err := strconv.ParseFloat(stringArgs[i], 64)
			if err != nil {
				return nil, fmt.Errorf("ERR value is not a valid float")
			}
			members = append(members, storage.ZSetMember{
				Member: stringArgs[i+1],
				Score:  score,
			})
		}
		added := r.store.ZAdd(stringArgs[0], members)
		return int64(added), nil

	case "ZREM":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zrem' command")
		}
		count := r.store.ZRem(stringArgs[0], stringArgs[1:])
		return int64(count), nil

	case "ZSCORE":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zscore' command")
		}
		score := r.store.ZScore(stringArgs[0], stringArgs[1])
		if score == nil {
			return nil, nil
		}
		return *score, nil

	case "ZCARD":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zcard' command")
		}
		count := r.store.ZCard(stringArgs[0])
		return int64(count), nil

	case "ZCOUNT":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zcount' command")
		}
		min, err := strconv.ParseFloat(stringArgs[1], 64)
		if err != nil {
			return nil, fmt.Errorf("ERR min value is not a valid float")
		}
		max, err := strconv.ParseFloat(stringArgs[2], 64)
		if err != nil {
			return nil, fmt.Errorf("ERR max value is not a valid float")
		}
		count := r.store.ZCount(stringArgs[0], min, max)
		return int64(count), nil

	case "ZINCRBY":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zincrby' command")
		}
		increment, err := strconv.ParseFloat(stringArgs[1], 64)
		if err != nil {
			return nil, fmt.Errorf("ERR value is not a valid float")
		}
		newScore, err := r.store.ZIncrBy(stringArgs[0], increment, stringArgs[2])
		if err != nil {
			return nil, err
		}
		return newScore, nil

	case "ZRANGE":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zrange' command")
		}
		start, err := strconv.Atoi(stringArgs[1])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		stop, err := strconv.Atoi(stringArgs[2])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		withScores := len(stringArgs) > 3 && strings.ToUpper(stringArgs[3]) == "WITHSCORES"
		members := r.store.ZRange(stringArgs[0], start, stop, withScores)
		if withScores {
			result := make([]interface{}, len(members)*2)
			for i, m := range members {
				result[i*2] = m.Member
				result[i*2+1] = m.Score
			}
			return result, nil
		}
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m.Member
		}
		return result, nil

	case "ZREVRANGE":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zrevrange' command")
		}
		start, err := strconv.Atoi(stringArgs[1])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		stop, err := strconv.Atoi(stringArgs[2])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		withScores := len(stringArgs) > 3 && strings.ToUpper(stringArgs[3]) == "WITHSCORES"
		members := r.store.ZRevRange(stringArgs[0], start, stop, withScores)
		if withScores {
			result := make([]interface{}, len(members)*2)
			for i, m := range members {
				result[i*2] = m.Member
				result[i*2+1] = m.Score
			}
			return result, nil
		}
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m.Member
		}
		return result, nil

	case "ZRANGEBYSCORE":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zrangebyscore' command")
		}
		min, err := strconv.ParseFloat(stringArgs[1], 64)
		if err != nil {
			return nil, fmt.Errorf("ERR min value is not a valid float")
		}
		max, err := strconv.ParseFloat(stringArgs[2], 64)
		if err != nil {
			return nil, fmt.Errorf("ERR max value is not a valid float")
		}
		// ZRangeByScore expects offset and count, default to 0 and -1 (all)
		members := r.store.ZRangeByScore(stringArgs[0], min, max, 0, -1)
		// Check for WITHSCORES option
		withScores := len(stringArgs) > 3 && strings.ToUpper(stringArgs[3]) == "WITHSCORES"
		if withScores {
			result := make([]interface{}, len(members)*2)
			for i, m := range members {
				result[i*2] = m.Member
				result[i*2+1] = m.Score
			}
			return result, nil
		}
		result := make([]interface{}, len(members))
		for i, m := range members {
			result[i] = m.Member
		}
		return result, nil

	case "ZRANK":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zrank' command")
		}
		rank := r.store.ZRank(stringArgs[0], stringArgs[1])
		if rank == -1 {
			return nil, nil
		}
		return int64(rank), nil

	case "ZREVRANK":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zrevrank' command")
		}
		rank := r.store.ZRevRank(stringArgs[0], stringArgs[1])
		if rank == -1 {
			return nil, nil
		}
		return int64(rank), nil

	case "ZREMRANGEBYRANK":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zremrangebyrank' command")
		}
		start, err := strconv.Atoi(stringArgs[1])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		stop, err := strconv.Atoi(stringArgs[2])
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		count := r.store.ZRemRangeByRank(stringArgs[0], start, stop)
		return int64(count), nil

	case "ZREMRANGEBYSCORE":
		if len(stringArgs) < 3 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'zremrangebyscore' command")
		}
		min, err := strconv.ParseFloat(stringArgs[1], 64)
		if err != nil {
			return nil, fmt.Errorf("ERR min value is not a valid float")
		}
		max, err := strconv.ParseFloat(stringArgs[2], 64)
		if err != nil {
			return nil, fmt.Errorf("ERR max value is not a valid float")
		}
		count := r.store.ZRemRangeByScore(stringArgs[0], min, max)
		return int64(count), nil

	// ==================== KEY COMMANDS ====================
	case "EXPIRE":
		if len(stringArgs) < 2 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'expire' command")
		}
		seconds, err := strconv.ParseInt(stringArgs[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ERR value is not an integer or out of range")
		}
		expiryTime := time.Now().Add(time.Duration(seconds) * time.Second)
		success := r.store.Expire(stringArgs[0], &expiryTime)
		if success {
			return int64(1), nil
		}
		return int64(0), nil

	case "TTL":
		if len(stringArgs) < 1 {
			return nil, fmt.Errorf("ERR wrong number of arguments for 'ttl' command")
		}
		ttl := r.store.TTL(stringArgs[0])
		return ttl, nil

	case "KEYS":
		// Note: Keys() returns all keys, pattern matching not implemented in storage layer
		keys := r.store.Keys()
		result := make([]interface{}, len(keys))
		for i, k := range keys {
			result[i] = k
		}
		return result, nil

	default:
		return nil, fmt.Errorf("ERR unknown command '%s' called from script", cmdName)
	}
}

// increment increments a key's value
func (r *RedisExecutor) increment(key string, delta int64) (int64, error) {
	value, exists := r.store.Get(key)
	if !exists {
		// Key doesn't exist, set to 0 + delta
		r.store.Set(key, fmt.Sprintf("%d", delta), nil)
		return delta, nil
	}

	// Parse current value as integer
	valueStr, ok := value.(string)
	if !ok {
		return 0, fmt.Errorf("ERR value is not an integer or out of range")
	}

	current, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("ERR value is not an integer or out of range")
	}

	// Calculate new value
	newValue := current + delta
	r.store.Set(key, fmt.Sprintf("%d", newValue), nil)
	return newValue, nil
}
