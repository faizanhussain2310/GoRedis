package aof

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Reader handles reading and replaying AOF files
type Reader struct {
	filepath string
	file     *os.File
	scanner  *bufio.Scanner
}

// NewReader creates a new AOF reader
func NewReader(filepath string) (*Reader, error) {
	file, err := os.Open(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet - this is fine for first startup
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open AOF file: %w", err)
	}

	return &Reader{
		filepath: filepath,
		file:     file,
		scanner:  bufio.NewScanner(file),
	}, nil
}

// Close closes the reader
func (r *Reader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// ReadCommand reads and parses the next command from the AOF file
// Returns the command arguments or nil if EOF
// Returns error if the file is corrupted
func (r *Reader) ReadCommand() ([]string, error) {
	if r == nil || r.scanner == nil {
		return nil, io.EOF
	}

	// Read array header: *<count>\r\n
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read array header: %w", err)
		}
		return nil, io.EOF
	}

	line := r.scanner.Text()
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("invalid AOF format: expected '*' array header, got: %s", line)
	}

	// Parse array size
	countStr := strings.TrimPrefix(line, "*")
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return nil, fmt.Errorf("invalid array count: %s", countStr)
	}

	if count <= 0 {
		return nil, fmt.Errorf("invalid array count: %d", count)
	}

	// Read each bulk string element
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		arg, err := r.readBulkString()
		if err != nil {
			return nil, fmt.Errorf("failed to read argument %d: %w", i, err)
		}
		args = append(args, arg)
	}

	return args, nil
}

// readBulkString reads a RESP bulk string: $<len>\r\n<data>\r\n
func (r *Reader) readBulkString() (string, error) {
	// Read length line: $<len>\r\n
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return "", fmt.Errorf("failed to read bulk string length: %w", err)
		}
		return "", io.ErrUnexpectedEOF
	}

	line := r.scanner.Text()
	if !strings.HasPrefix(line, "$") {
		return "", fmt.Errorf("invalid bulk string format: expected '$', got: %s", line)
	}

	// Parse length
	lengthStr := strings.TrimPrefix(line, "$")
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", fmt.Errorf("invalid bulk string length: %s", lengthStr)
	}

	if length < 0 {
		return "", fmt.Errorf("invalid bulk string length: %d", length)
	}

	// Read data line
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return "", fmt.Errorf("failed to read bulk string data: %w", err)
		}
		return "", io.ErrUnexpectedEOF
	}

	data := r.scanner.Text()
	if len(data) != length {
		return "", fmt.Errorf("bulk string length mismatch: expected %d, got %d", length, len(data))
	}

	return data, nil
}

// LoadAll reads all commands from the AOF file and returns them
func (r *Reader) LoadAll() ([][]string, error) {
	if r == nil {
		return nil, nil
	}

	commands := make([][]string, 0)
	for {
		cmd, err := r.ReadCommand()
		if err == io.EOF {
			break
		}
		if err != nil {
			return commands, fmt.Errorf("error reading command at position %d: %w", len(commands), err)
		}
		commands = append(commands, cmd)
	}

	return commands, nil
}
