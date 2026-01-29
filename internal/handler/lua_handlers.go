package handler

import (
	"fmt"
	"redis/internal/protocol"
	"strconv"
	"strings"
)

// handleEval executes a Lua script
// EVAL script numkeys key [key ...] arg [arg ...]
func (h *CommandHandler) handleEval(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'eval' command")
	}

	script := cmd.Args[1]
	numKeys, err := strconv.Atoi(cmd.Args[2])
	if err != nil || numKeys < 0 {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	if len(cmd.Args) < 3+numKeys {
		return protocol.EncodeError("ERR Number of keys can't be greater than number of args")
	}

	// Extract keys and args
	keys := make([]string, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = cmd.Args[3+i]
	}

	args := make([]string, len(cmd.Args)-3-numKeys)
	for i := 0; i < len(args); i++ {
		args[i] = cmd.Args[3+numKeys+i]
	}

	// Execute the script
	result, err := h.luaEngine.Eval(script, keys, args)
	if err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %s", err.Error()))
	}

	// Convert result to RESP format
	return h.convertLuaResultToRESP(result)
}

// handleEvalSHA executes a cached Lua script by SHA1 hash
// EVALSHA sha1 numkeys key [key ...] arg [arg ...]
func (h *CommandHandler) handleEvalSHA(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'evalsha' command")
	}

	sha1Hash := cmd.Args[1]
	numKeys, err := strconv.Atoi(cmd.Args[2])
	if err != nil || numKeys < 0 {
		return protocol.EncodeError("ERR value is not an integer or out of range")
	}

	if len(cmd.Args) < 3+numKeys {
		return protocol.EncodeError("ERR Number of keys can't be greater than number of args")
	}

	// Extract keys and args
	keys := make([]string, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = cmd.Args[3+i]
	}

	args := make([]string, len(cmd.Args)-3-numKeys)
	for i := 0; i < len(args); i++ {
		args[i] = cmd.Args[3+numKeys+i]
	}

	// Execute the cached script
	result, err := h.luaEngine.EvalSHA(sha1Hash, keys, args)
	if err != nil {
		return protocol.EncodeError(fmt.Sprintf("ERR %s", err.Error()))
	}

	// Convert result to RESP format
	return h.convertLuaResultToRESP(result)
}

// handleScript handles SCRIPT subcommands
// SCRIPT LOAD | EXISTS | FLUSH | DEBUG | KILL
func (h *CommandHandler) handleScript(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'script' command")
	}

	subcommand := strings.ToUpper(cmd.Args[1])

	switch subcommand {
	case "LOAD":
		return h.handleScriptLoad(cmd)
	case "EXISTS":
		return h.handleScriptExists(cmd)
	case "FLUSH":
		return h.handleScriptFlush(cmd)
	default:
		return protocol.EncodeError(fmt.Sprintf("ERR unknown SCRIPT subcommand '%s'", subcommand))
	}
}

// handleScriptLoad caches a script and returns its SHA1 hash
// SCRIPT LOAD script
func (h *CommandHandler) handleScriptLoad(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'script|load' command")
	}

	script := cmd.Args[2]
	sha1Hash := h.luaEngine.LoadScript(script)

	return protocol.EncodeBulkString(sha1Hash)
}

// handleScriptExists checks if scripts exist in cache
// SCRIPT EXISTS sha1 [sha1 ...]
func (h *CommandHandler) handleScriptExists(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'script|exists' command")
	}

	sha1Hashes := cmd.Args[2:]
	results := h.luaEngine.ScriptExists(sha1Hashes)

	// Convert bool results to integers
	response := make([]int, len(results))
	for i, exists := range results {
		if exists {
			response[i] = 1
		} else {
			response[i] = 0
		}
	}

	return protocol.EncodeIntegerArray(response)
}

// handleScriptFlush removes all cached scripts
// SCRIPT FLUSH
func (h *CommandHandler) handleScriptFlush(cmd *protocol.Command) []byte {
	h.luaEngine.ScriptFlush()
	return protocol.EncodeSimpleString("OK")
}

// convertLuaResultToRESP converts Lua result to RESP format
func (h *CommandHandler) convertLuaResultToRESP(result interface{}) []byte {
	if result == nil {
		return protocol.EncodeBulkString("") // Null bulk string
	}

	switch v := result.(type) {
	case bool:
		if v {
			return protocol.EncodeInteger(1)
		}
		return protocol.EncodeInteger(0)

	case int:
		return protocol.EncodeInteger(v)

	case int64:
		return protocol.EncodeInteger(int(v))

	case float64:
		return protocol.EncodeInteger(int(v))

	case string:
		return protocol.EncodeBulkString(v)

	case []interface{}:
		// Convert array to string array for protocol encoding
		strArray := make([]string, len(v))
		for i, item := range v {
			strArray[i] = fmt.Sprintf("%v", item)
		}
		return protocol.EncodeArray(strArray)

	case map[string]interface{}:
		// Check if it's a status reply
		if status, ok := v["ok"]; ok {
			return protocol.EncodeSimpleString(fmt.Sprintf("%v", status))
		}
		// Check if it's an error reply
		if errMsg, ok := v["err"]; ok {
			return protocol.EncodeError(fmt.Sprintf("%v", errMsg))
		}
		// Convert map to array of key-value pairs
		result := make([]string, 0, len(v)*2)
		for key, val := range v {
			result = append(result, key, fmt.Sprintf("%v", val))
		}
		return protocol.EncodeArray(result)

	default:
		return protocol.EncodeBulkString(fmt.Sprintf("%v", v))
	}
}
