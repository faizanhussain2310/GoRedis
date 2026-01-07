package handler

// writeCommands is a set of all commands that perform write operations
// This is used to enforce READONLY errors on replicas
var writeCommands = map[string]bool{
	// String commands
	"SET": true, "SETEX": true, "SETNX": true, "PSETEX": true,
	"APPEND": true, "INCR": true, "DECR": true, "INCRBY": true, "DECRBY": true,
	"GETSET": true, "MSET": true, "MSETNX": true,
	
	// Key commands
	"DEL": true, "UNLINK": true, "EXPIRE": true, "EXPIREAT": true,
	"PEXPIRE": true, "PEXPIREAT": true, "PERSIST": true, "RENAME": true,
	"RENAMENX": true, "MOVE": true,
	
	// Hash commands
	"HSET": true, "HSETNX": true, "HMSET": true, "HDEL": true,
	"HINCRBY": true, "HINCRBYFLOAT": true,
	
	// List commands
	"LPUSH": true, "RPUSH": true, "LPUSHX": true, "RPUSHX": true,
	"LPOP": true, "RPOP": true, "LSET": true, "LINSERT": true,
	"LREM": true, "LTRIM": true, "RPOPLPUSH": true,
	"BLPOP": true, "BRPOP": true, "BRPOPLPUSH": true,
	
	// Set commands
	"SADD": true, "SREM": true, "SPOP": true, "SMOVE": true,
	
	// Sorted set commands
	"ZADD": true, "ZREM": true, "ZINCRBY": true, "ZREMRANGEBYRANK": true,
	"ZREMRANGEBYSCORE": true, "ZREMRANGEBYLEX": true, "ZPOPMIN": true,
	"ZPOPMAX": true, "BZPOPMIN": true, "BZPOPMAX": true,
	
	// Geo commands
	"GEOADD": true,
	
	// Bloom filter commands
	"BF.ADD": true, "BF.MADD": true,
	
	// Pub/Sub commands (writes to pub/sub state)
	"PUBLISH": true,
	
	// Admin commands
	"FLUSHDB": true, "FLUSHALL": true,
}

// IsWriteCommand checks if a command is a write operation
// This is a package-level utility that can be used by any handler
func IsWriteCommand(cmd string) bool {
	return writeCommands[cmd]
}
