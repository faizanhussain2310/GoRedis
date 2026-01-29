package lua

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// ScriptEngine manages Lua script execution and caching
type ScriptEngine struct {
	scriptCache   map[string]string // SHA1 -> script source
	redisExecutor *RedisExecutor    // Executor for Redis commands
}

// NewScriptEngine creates a new Lua script engine
func NewScriptEngine(executor *RedisExecutor) *ScriptEngine {
	return &ScriptEngine{
		scriptCache:   make(map[string]string),
		redisExecutor: executor,
	}
}

// Eval executes a Lua script with given keys and arguments
func (se *ScriptEngine) Eval(script string, keys []string, args []string) (interface{}, error) {
	L := lua.NewState()
	defer L.Close()

	// Register Redis API functions
	se.registerRedisAPI(L)

	// Set KEYS and ARGV globals
	se.setGlobals(L, keys, args)

	// Execute the script
	if err := L.DoString(script); err != nil {
		return nil, fmt.Errorf("ERR Error running script: %v", err)
	}

	// Get the result from the stack
	result := se.convertLuaToGo(L.Get(-1))
	return result, nil
}

// EvalSHA executes a cached script by its SHA1 hash
func (se *ScriptEngine) EvalSHA(sha1Hash string, keys []string, args []string) (interface{}, error) {
	script, exists := se.scriptCache[sha1Hash]
	if !exists {
		return nil, fmt.Errorf("NOSCRIPT No matching script. Please use EVAL")
	}

	return se.Eval(script, keys, args)
}

// LoadScript loads a script into cache and returns its SHA1 hash
func (se *ScriptEngine) LoadScript(script string) string {
	hash := se.calculateSHA1(script)
	se.scriptCache[hash] = script
	return hash
}

// ScriptExists checks if scripts exist in cache
func (se *ScriptEngine) ScriptExists(sha1Hashes []string) []bool {
	results := make([]bool, len(sha1Hashes))
	for i, hash := range sha1Hashes {
		_, exists := se.scriptCache[hash]
		results[i] = exists
	}
	return results
}

// ScriptFlush removes all scripts from cache
func (se *ScriptEngine) ScriptFlush() {
	se.scriptCache = make(map[string]string)
}

// registerRedisAPI registers Redis functions in Lua state
func (se *ScriptEngine) registerRedisAPI(L *lua.LState) {
	// Create redis table
	redisTable := L.NewTable()

	// redis.call - executes command, throws error on failure
	redisTable.RawSetString("call", L.NewFunction(func(L *lua.LState) int {
		n := L.GetTop()
		if n < 1 {
			L.RaiseError("redis.call requires at least one argument")
			return 0
		}

		cmdName := L.CheckString(1)
		args := make([]interface{}, n-1)
		for i := 2; i <= n; i++ {
			args[i-2] = se.convertLuaToGo(L.Get(i))
		}

		result, err := se.redisExecutor.ExecuteCommand(cmdName, args...)
		if err != nil {
			L.RaiseError(err.Error())
			return 0
		}

		L.Push(se.convertGoToLua(L, result))
		return 1
	}))

	// redis.pcall - executes command, returns error as table on failure
	redisTable.RawSetString("pcall", L.NewFunction(func(L *lua.LState) int {
		n := L.GetTop()
		if n < 1 {
			errorTable := L.NewTable()
			errorTable.RawSetString("err", lua.LString("redis.pcall requires at least one argument"))
			L.Push(errorTable)
			return 1
		}

		cmdName := L.CheckString(1)
		args := make([]interface{}, n-1)
		for i := 2; i <= n; i++ {
			args[i-2] = se.convertLuaToGo(L.Get(i))
		}

		result, err := se.redisExecutor.ExecuteCommand(cmdName, args...)
		if err != nil {
			errorTable := L.NewTable()
			errorTable.RawSetString("err", lua.LString(err.Error()))
			L.Push(errorTable)
			return 1
		}

		L.Push(se.convertGoToLua(L, result))
		return 1
	}))

	// redis.log - logs a message (for now, just ignore it)
	redisTable.RawSetString("log", L.NewFunction(func(L *lua.LState) int {
		// In a real implementation, this would log to Redis logs
		// For now, we just ignore it
		return 0
	}))

	// redis.status_reply - creates a status reply table
	redisTable.RawSetString("status_reply", L.NewFunction(func(L *lua.LState) int {
		status := L.CheckString(1)
		statusTable := L.NewTable()
		statusTable.RawSetString("ok", lua.LString(status))
		L.Push(statusTable)
		return 1
	}))

	// redis.error_reply - creates an error reply table
	redisTable.RawSetString("error_reply", L.NewFunction(func(L *lua.LState) int {
		errMsg := L.CheckString(1)
		errorTable := L.NewTable()
		errorTable.RawSetString("err", lua.LString(errMsg))
		L.Push(errorTable)
		return 1
	}))

	// Set redis table as global
	L.SetGlobal("redis", redisTable)
}

// setGlobals sets KEYS and ARGV as global Lua arrays
func (se *ScriptEngine) setGlobals(L *lua.LState, keys []string, args []string) {
	// Create KEYS array (1-indexed in Lua)
	keysTable := L.NewTable()
	for i, key := range keys {
		keysTable.RawSetInt(i+1, lua.LString(key))
	}
	L.SetGlobal("KEYS", keysTable)

	// Create ARGV array (1-indexed in Lua)
	argvTable := L.NewTable()
	for i, arg := range args {
		argvTable.RawSetInt(i+1, lua.LString(arg))
	}
	L.SetGlobal("ARGV", argvTable)
}

// convertLuaToGo converts Lua value to Go value
func (se *ScriptEngine) convertLuaToGo(lv lua.LValue) interface{} {
	switch v := lv.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(v)
	case lua.LNumber:
		return int64(v)
	case lua.LString:
		return string(v)
	case *lua.LTable:
		// Check if it's a status reply
		if ok := v.RawGetString("ok"); ok != lua.LNil {
			return map[string]interface{}{"ok": se.convertLuaToGo(ok)}
		}
		// Check if it's an error reply
		if err := v.RawGetString("err"); err != lua.LNil {
			return map[string]interface{}{"err": se.convertLuaToGo(err)}
		}

		// Check if it's an array or a map
		isArray := true
		maxN := 0
		v.ForEach(func(k, val lua.LValue) {
			if num, ok := k.(lua.LNumber); ok {
				if int(num) > maxN {
					maxN = int(num)
				}
			} else {
				isArray = false
			}
		})

		if isArray && maxN > 0 {
			// It's an array
			arr := make([]interface{}, maxN)
			for i := 1; i <= maxN; i++ {
				arr[i-1] = se.convertLuaToGo(v.RawGetInt(i))
			}
			return arr
		} else {
			// It's a map
			m := make(map[string]interface{})
			v.ForEach(func(k, val lua.LValue) {
				if str, ok := k.(lua.LString); ok {
					m[string(str)] = se.convertLuaToGo(val)
				}
			})
			return m
		}
	default:
		return nil
	}
}

// convertGoToLua converts Go value to Lua value
func (se *ScriptEngine) convertGoToLua(L *lua.LState, v interface{}) lua.LValue {
	if v == nil {
		return lua.LNil
	}

	switch val := v.(type) {
	case bool:
		return lua.LBool(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case float64:
		return lua.LNumber(val)
	case string:
		return lua.LString(val)
	case []interface{}:
		table := L.NewTable()
		for i, item := range val {
			table.RawSetInt(i+1, se.convertGoToLua(L, item))
		}
		return table
	case map[string]interface{}:
		table := L.NewTable()
		for k, item := range val {
			table.RawSetString(k, se.convertGoToLua(L, item))
		}
		return table
	default:
		return lua.LString(fmt.Sprintf("%v", val))
	}
}

// calculateSHA1 computes SHA1 hash of script
func (se *ScriptEngine) calculateSHA1(script string) string {
	hash := sha1.Sum([]byte(script))
	return hex.EncodeToString(hash[:])
}
