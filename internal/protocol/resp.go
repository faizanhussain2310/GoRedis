package protocol

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Command struct {
	Args []string
}

func ParseCommand(reader *bufio.Reader) (*Command, error) {
	line, err := readLine(reader)
	if err != nil {
		return nil, err
	}

	if len(line) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	switch line[0] {
	case '*':
		return parseArray(reader, line)
	default:
		return parseInline(line)
	}
}

func parseArray(reader *bufio.Reader, firstLine string) (*Command, error) {
	count, err := strconv.Atoi(firstLine[1:])
	if err != nil {
		return nil, fmt.Errorf("invalid array length: %v", err)
	}

	if count <= 0 {
		return nil, fmt.Errorf("invalid array length: %d", count)
	}

	args := make([]string, 0, count)

	for i := 0; i < count; i++ {
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}

		if len(line) == 0 || line[0] != '$' {
			return nil, fmt.Errorf("expected bulk string, got: %s", line)
		}

		length, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, fmt.Errorf("invalid bulk string length: %v", err)
		}

		if length < 0 {
			args = append(args, "")
			continue
		}

		data := make([]byte, length)
		_, err = io.ReadFull(reader, data)
		if err != nil {
			return nil, err
		}

		_, err = readLine(reader)
		if err != nil {
			return nil, err
		}

		args = append(args, string(data))
	}

	return &Command{Args: args}, nil
}

func parseInline(line string) (*Command, error) {
	args := strings.Fields(line)
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return &Command{Args: args}, nil
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// HasCompleteCommand checks if the buffer contains at least one complete RESP command
// without consuming any data. Returns true if a complete command is available.
func HasCompleteCommand(reader *bufio.Reader) bool {
	buffered := reader.Buffered()
	if buffered == 0 {
		return false
	}

	// Peek at all buffered data
	buf, err := reader.Peek(buffered)
	if err != nil || len(buf) == 0 {
		return false
	}

	return hasCompleteRESP(buf)
}

// hasCompleteRESP checks if buf contains a complete RESP message
func hasCompleteRESP(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}

	switch buf[0] {
	case '*':
		// Array (most common for commands)
		return hasCompleteArray(buf)
	case '$':
		// Bulk string
		return hasCompleteBulkString(buf)
	case '+', '-', ':':
		// Simple string, error, or integer - just needs CRLF
		return bytes.Contains(buf, []byte("\r\n"))
	default:
		// Inline command - just needs newline
		return bytes.Contains(buf, []byte("\n"))
	}
}

// hasCompleteArray checks if buf contains a complete RESP array
func hasCompleteArray(buf []byte) bool {
	// Find first CRLF to get array count
	crlfIdx := bytes.Index(buf, []byte("\r\n"))
	if crlfIdx == -1 {
		return false
	}

	// Parse array count
	countStr := string(buf[1:crlfIdx])
	count, err := strconv.Atoi(countStr)
	if err != nil || count <= 0 {
		return count == 0 // Empty array is complete
	}

	// Move past the array header
	idx := crlfIdx + 2

	// Check each element
	for i := 0; i < count; i++ {
		if idx >= len(buf) {
			return false
		}

		switch buf[idx] {
		case '$':
			// Bulk string
			endIdx := hasCompleteBulkStringAt(buf, idx)
			if endIdx == -1 {
				return false
			}
			idx = endIdx
		case ':':
			// Integer
			nextCRLF := bytes.Index(buf[idx:], []byte("\r\n"))
			if nextCRLF == -1 {
				return false
			}
			idx += nextCRLF + 2
		case '+', '-':
			// Simple string or error
			nextCRLF := bytes.Index(buf[idx:], []byte("\r\n"))
			if nextCRLF == -1 {
				return false
			}
			idx += nextCRLF + 2
		default:
			return false
		}
	}

	return true
}

// hasCompleteBulkString checks if buf starts with a complete bulk string
func hasCompleteBulkString(buf []byte) bool {
	return hasCompleteBulkStringAt(buf, 0) != -1
}

// hasCompleteBulkStringAt checks if a complete bulk string starts at idx
// Returns the index after the bulk string, or -1 if incomplete
func hasCompleteBulkStringAt(buf []byte, idx int) int {
	if idx >= len(buf) || buf[idx] != '$' {
		return -1
	}

	// Find CRLF after length
	remaining := buf[idx:]
	crlfIdx := bytes.Index(remaining, []byte("\r\n"))
	if crlfIdx == -1 {
		return -1
	}

	// Parse length
	lengthStr := string(remaining[1:crlfIdx])
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return -1
	}

	// Null bulk string
	if length < 0 {
		return idx + crlfIdx + 2
	}

	// Check if we have enough bytes: $N\r\n + data + \r\n
	totalNeeded := crlfIdx + 2 + length + 2
	if len(remaining) < totalNeeded {
		return -1
	}

	return idx + totalNeeded
}

func EncodeSimpleString(s string) []byte {
	return []byte(fmt.Sprintf("+%s\r\n", s))
}

func EncodeError(s string) []byte {
	return []byte(fmt.Sprintf("-%s\r\n", s))
}

func EncodeInteger(i int) []byte {
	return []byte(fmt.Sprintf(":%d\r\n", i))
}

func EncodeInteger64(i int64) []byte {
	return []byte(fmt.Sprintf(":%d\r\n", i))
}

func EncodeBulkString(s string) []byte {
	return []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(s), s))
}

func EncodeNullBulkString() []byte {
	return []byte("$-1\r\n")
}

// EncodeNilArray encodes a nil array (used for blocking command timeouts)
func EncodeNilArray() []byte {
	return []byte("*-1\r\n")
}

func EncodeArray(items []string) []byte {
	result := fmt.Sprintf("*%d\r\n", len(items))
	for _, item := range items {
		result += fmt.Sprintf("$%d\r\n%s\r\n", len(item), item)
	}
	return []byte(result)
}

// EncodeRawArray encodes an array of already-encoded RESP responses
// Used for EXEC to return an array of command results
func EncodeRawArray(items [][]byte) []byte {
	// Calculate total size for efficient allocation
	totalSize := len(fmt.Sprintf("*%d\r\n", len(items)))
	for _, item := range items {
		totalSize += len(item)
	}

	result := make([]byte, 0, totalSize)
	result = append(result, []byte(fmt.Sprintf("*%d\r\n", len(items)))...)
	for _, item := range items {
		result = append(result, item...)
	}
	return result
}

// EncodeInterfaceArray encodes an array that may contain nil values
func EncodeInterfaceArray(items []interface{}) []byte {
	result := fmt.Sprintf("*%d\r\n", len(items))
	for _, item := range items {
		if item == nil {
			result += "$-1\r\n"
		} else if s, ok := item.(string); ok {
			result += fmt.Sprintf("$%d\r\n%s\r\n", len(s), s)
		} else {
			str := fmt.Sprintf("%v", item)
			result += fmt.Sprintf("$%d\r\n%s\r\n", len(str), str)
		}
	}
	return []byte(result)
}

// EncodeIntegerArray encodes an array of integers
// Used for commands like SCRIPT EXISTS that return multiple integer values
func EncodeIntegerArray(items []int) []byte {
	result := fmt.Sprintf("*%d\r\n", len(items))
	for _, item := range items {
		result += fmt.Sprintf(":%d\r\n", item)
	}
	return []byte(result)
}
