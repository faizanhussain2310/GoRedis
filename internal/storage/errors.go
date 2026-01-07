package storage

import "errors"

var (
	// General errors
	ErrKeyNotFound      = errors.New("key not found")
	ErrInvalidOperation = errors.New("invalid operation")
	ErrWrongType        = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")

	// List errors
	ErrNoSuchKey       = errors.New("ERR no such key")
	ErrIndexOutOfRange = errors.New("ERR index out of range")

	// Hash errors
	ErrWrongNumArgs        = errors.New("ERR wrong number of arguments for 'hset' command")
	ErrHashValueNotInteger = errors.New("ERR hash value is not an integer")
	ErrHashValueNotFloat   = errors.New("ERR hash value is not a float")
)
