package rdb

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"time"
)

// RDB opcodes and types (matching writer constants)
const (
	opEOF          = OpCodeEOF
	opExpireTime   = OpCodeExpireTime
	opExpireTimeMs = OpCodeExpireTimeMS

	typeString = TypeString
	typeList   = TypeList
	typeHash   = TypeHash
	typeSet    = TypeSet
)

// Reader handles reading RDB files
type Reader struct {
	filepath string
	file     *os.File
	reader   *bufio.Reader
}

// NewReader creates a new RDB reader
func NewReader(filepath string) (*Reader, error) {
	file, err := os.Open(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File doesn't exist - this is fine
		}
		return nil, fmt.Errorf("failed to open RDB file: %w", err)
	}

	return &Reader{
		filepath: filepath,
		file:     file,
		reader:   bufio.NewReader(file),
	}, nil
}

// Close closes the reader
func (r *Reader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// LoadCommand represents a command to restore data
type LoadCommand struct {
	Key        string
	Value      interface{}
	Expiration *time.Time
	Type       byte
}

// Load reads and parses the RDB file, returning commands to restore the database
func (r *Reader) Load() ([]LoadCommand, error) {
	if r == nil {
		return nil, nil
	}

	// Read and verify magic string "REDIS"
	magic := make([]byte, 5)
	if _, err := io.ReadFull(r.reader, magic); err != nil {
		return nil, fmt.Errorf("failed to read magic string: %w", err)
	}
	if string(magic) != "REDIS" {
		return nil, fmt.Errorf("invalid RDB file: wrong magic string")
	}

	// Read version (4 bytes)
	version := make([]byte, 4)
	if _, err := io.ReadFull(r.reader, version); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}

	commands := make([]LoadCommand, 0)
	var currentExpiration *time.Time

	// Create CRC64 hasher for checksum verification
	table := crc64.MakeTable(crc64.ECMA)
	hasher := crc64.New(table)

	// Read header into hasher
	hasher.Write(magic)
	hasher.Write(version)

	for {
		// Read type byte
		typeByte, err := r.reader.ReadByte()
		if err == io.EOF {
			return nil, fmt.Errorf("unexpected EOF")
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read type byte: %w", err)
		}
		hasher.Write([]byte{typeByte})

		switch typeByte {
		case opExpireTime:
			// Read 4-byte Unix timestamp (seconds)
			var timestamp uint32
			if err := binary.Read(r.reader, binary.LittleEndian, &timestamp); err != nil {
				return nil, fmt.Errorf("failed to read expiration: %w", err)
			}
			timestampBytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(timestampBytes, timestamp)
			hasher.Write(timestampBytes)

			t := time.Unix(int64(timestamp), 0)
			currentExpiration = &t

		case opExpireTimeMs:
			// Read 8-byte Unix timestamp (milliseconds)
			var timestamp uint64
			if err := binary.Read(r.reader, binary.LittleEndian, &timestamp); err != nil {
				return nil, fmt.Errorf("failed to read expiration ms: %w", err)
			}
			timestampBytes := make([]byte, 8)
			binary.LittleEndian.PutUint64(timestampBytes, timestamp)
			hasher.Write(timestampBytes)

			t := time.Unix(int64(timestamp/1000), int64((timestamp%1000)*1000000))
			currentExpiration = &t

		case opEOF:
			// Read CRC64 checksum (8 bytes)
			var storedChecksum uint64
			if err := binary.Read(r.reader, binary.LittleEndian, &storedChecksum); err != nil {
				return nil, fmt.Errorf("failed to read checksum: %w", err)
			}

			// Verify checksum
			calculatedChecksum := hasher.Sum64()
			if calculatedChecksum != storedChecksum {
				return nil, fmt.Errorf("checksum mismatch: expected %d, got %d", storedChecksum, calculatedChecksum)
			}

			return commands, nil

		case typeString, typeList, typeHash, typeSet:
			// Read key-value pair
			key, keyBytes, err := r.readString()
			if err != nil {
				return nil, fmt.Errorf("failed to read key: %w", err)
			}
			hasher.Write(keyBytes)

			// Read value based on type
			var value interface{}
			var valueBytes []byte

			switch typeByte {
			case typeString:
				value, valueBytes, err = r.readString()
			case typeList:
				value, valueBytes, err = r.readList()
			case typeHash:
				value, valueBytes, err = r.readHash()
			case typeSet:
				value, valueBytes, err = r.readSet()
			}

			if err != nil {
				return nil, fmt.Errorf("failed to read value for key %s: %w", key, err)
			}
			hasher.Write(valueBytes)

			commands = append(commands, LoadCommand{
				Key:        key,
				Value:      value,
				Expiration: currentExpiration,
				Type:       typeByte,
			})

			// Reset expiration for next key
			currentExpiration = nil

		default:
			return nil, fmt.Errorf("unknown type byte: %d", typeByte)
		}
	}
}

// readString reads a length-prefixed string and returns both the string and raw bytes for hashing
func (r *Reader) readString() (string, []byte, error) {
	// Read length
	length, lengthBytes, err := r.readLength()
	if err != nil {
		return "", nil, fmt.Errorf("failed to read string length: %w", err)
	}

	// Read string data
	data := make([]byte, length)
	if _, err := io.ReadFull(r.reader, data); err != nil {
		return "", nil, fmt.Errorf("failed to read string data: %w", err)
	}

	// Combine length bytes and data for hashing
	allBytes := append(lengthBytes, data...)
	return string(data), allBytes, nil
}

// readList reads a list value
func (r *Reader) readList() ([]string, []byte, error) {
	length, lengthBytes, err := r.readLength()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read list length: %w", err)
	}

	allBytes := lengthBytes
	list := make([]string, length)
	for i := uint32(0); i < length; i++ {
		elem, elemBytes, err := r.readString()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read list element %d: %w", i, err)
		}
		list[i] = elem
		allBytes = append(allBytes, elemBytes...)
	}

	return list, allBytes, nil
}

// readHash reads a hash value
func (r *Reader) readHash() (map[string]string, []byte, error) {
	length, lengthBytes, err := r.readLength()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read hash length: %w", err)
	}

	allBytes := lengthBytes
	hash := make(map[string]string, length)
	for i := uint32(0); i < length; i++ {
		field, fieldBytes, err := r.readString()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read hash field %d: %w", i, err)
		}
		allBytes = append(allBytes, fieldBytes...)

		value, valueBytes, err := r.readString()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read hash value %d: %w", i, err)
		}
		allBytes = append(allBytes, valueBytes...)

		hash[field] = value
	}

	return hash, allBytes, nil
}

// readSet reads a set value
func (r *Reader) readSet() (map[string]struct{}, []byte, error) {
	length, lengthBytes, err := r.readLength()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read set length: %w", err)
	}

	allBytes := lengthBytes
	set := make(map[string]struct{}, length)
	for i := uint32(0); i < length; i++ {
		member, memberBytes, err := r.readString()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read set member %d: %w", i, err)
		}
		allBytes = append(allBytes, memberBytes...)
		set[member] = struct{}{}
	}

	return set, allBytes, nil
}

// readLength reads a variable-length integer
func (r *Reader) readLength() (uint32, []byte, error) {
	firstByte, err := r.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}

	// Check encoding type (top 2 bits)
	encodingType := (firstByte & 0xC0) >> 6

	switch encodingType {
	case 0: // 6-bit length
		return uint32(firstByte & 0x3F), []byte{firstByte}, nil

	case 1: // 14-bit length
		secondByte, err := r.reader.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		length := uint32(firstByte&0x3F)<<8 | uint32(secondByte)
		return length, []byte{firstByte, secondByte}, nil

	case 2: // 32-bit length
		bytes := make([]byte, 4)
		if _, err := io.ReadFull(r.reader, bytes); err != nil {
			return 0, nil, err
		}
		length := binary.BigEndian.Uint32(bytes)
		allBytes := append([]byte{firstByte}, bytes...)
		return length, allBytes, nil

	default:
		return 0, nil, fmt.Errorf("unsupported length encoding: %d", encodingType)
	}
}
