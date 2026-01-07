package handler

import (
	"fmt"
	"strconv"
	"strings"

	"redis/internal/protocol"
)

// handleSlowLog handles SLOWLOG command
// SLOWLOG GET [count] - Get slow log entries
// SLOWLOG LEN - Get slow log length
// SLOWLOG RESET - Reset slow log
func (h *CommandHandler) handleSlowLog(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'slowlog' command")
	}

	subcommand := strings.ToUpper(cmd.Args[1])

	switch subcommand {
	case "GET":
		return h.handleSlowLogGet(cmd)
	case "LEN":
		return h.handleSlowLogLen()
	case "RESET":
		return h.handleSlowLogReset()
	default:
		return protocol.EncodeError(fmt.Sprintf("ERR unknown subcommand '%s'. Try SLOWLOG GET, SLOWLOG LEN, SLOWLOG RESET", subcommand))
	}
}

// handleSlowLogGet returns slow log entries
func (h *CommandHandler) handleSlowLogGet(cmd *protocol.Command) []byte {
	count := 10 // Default count
	if len(cmd.Args) >= 3 {
		var err error
		count, err = strconv.Atoi(cmd.Args[2])
		if err != nil {
			return protocol.EncodeError("ERR value is not an integer or out of range")
		}
	}

	entries := h.slowLog.Get(count)

	// Build response as array of arrays
	// Each entry: [id, timestamp, duration_microseconds, [command, args...], client_id]
	result := make([]interface{}, len(entries))
	for i, entry := range entries {
		// Build command array
		cmdArgs := make([]interface{}, len(entry.Args)+1)
		cmdArgs[0] = entry.Command
		for j, arg := range entry.Args {
			cmdArgs[j+1] = arg
		}

		// Entry as array: [id, timestamp, duration_us, command_array, client_id]
		entryArray := []interface{}{
			entry.ID,
			entry.Timestamp.Unix(),
			entry.Duration.Microseconds(),
			cmdArgs,
			fmt.Sprintf("client-%d", entry.ClientID),
		}
		result[i] = entryArray
	}

	return protocol.EncodeInterfaceArray(result)
}

// handleSlowLogLen returns slow log length
func (h *CommandHandler) handleSlowLogLen() []byte {
	return protocol.EncodeInteger(h.slowLog.Len())
}

// handleSlowLogReset resets slow log
func (h *CommandHandler) handleSlowLogReset() []byte {
	h.slowLog.Reset()
	return protocol.EncodeSimpleString("OK")
}
