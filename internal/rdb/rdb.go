package rdb

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"time"

	"redis/internal/storage"
)

// RDB file format constants
const (
	RDBVersion     = 9
	RDBMagicString = "REDIS"

	// Opcodes
	OpCodeEOF          = 0xFF
	OpCodeSelectDB     = 0xFE
	OpCodeExpireTime   = 0xFD
	OpCodeExpireTimeMS = 0xFC
	OpCodeResizeDB     = 0xFB
	OpCodeAux          = 0xFA

	// Type codes
	TypeString      = 0
	TypeList        = 1
	TypeSet         = 2
	TypeZSet        = 3
	TypeHash        = 4
	TypeBloomFilter = 5
	TypeListQuick   = 14
)

// Writer handles RDB snapshot writes
type Writer struct {
	filepath string
}

// NewWriter creates a new RDB writer
func NewWriter(filepath string) *Writer {
	return &Writer{
		filepath: filepath,
	}
}

// Save creates an RDB snapshot file from the given data
// This is called in a background goroutine by BGSAVE
func (w *Writer) Save(snapshot map[string]*storage.Value) error {
	// Create temporary file
	tempPath := w.filepath + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create RDB temp file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	// Create CRC64 checksum hasher
	checksumTable := crc64.MakeTable(crc64.ECMA)
	hasher := crc64.New(checksumTable)

	// Use MultiWriter to compute checksum while writing
	multiWriter := io.MultiWriter(writer, hasher)

	// Write RDB header
	if err := w.writeHeader(multiWriter); err != nil {
		os.Remove(tempPath)
		return err
	}

	// Write database selector (DB 0)
	multiWriter.Write([]byte{OpCodeSelectDB, 0})

	// Write resize DB hint
	multiWriter.Write([]byte{OpCodeResizeDB})
	w.writeLengthToWriter(multiWriter, len(snapshot))
	w.writeLengthToWriter(multiWriter, 0) // Number of keys with expiry

	// Write all keys
	for key, value := range snapshot {
		if err := w.writeKeyToWriter(multiWriter, key, value); err != nil {
			os.Remove(tempPath)
			return err
		}
	}

	// Write EOF
	multiWriter.Write([]byte{OpCodeEOF})

	// Compute checksum and write it (not included in checksum itself)
	checksum := hasher.Sum64()
	binary.Write(writer, binary.LittleEndian, checksum)

	// Flush and sync
	if err := writer.Flush(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to flush RDB: %w", err)
	}

	if err := file.Sync(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to sync RDB: %w", err)
	}

	file.Close()

	// Atomically replace old RDB with new one
	if err := os.Rename(tempPath, w.filepath); err != nil {
		return fmt.Errorf("failed to replace RDB file: %w", err)
	}

	return nil
}

// writeHeader writes the RDB file header
func (w *Writer) writeHeader(writer io.Writer) error {
	// Magic string "REDIS"
	writer.Write([]byte(RDBMagicString))

	// Version (4 digits)
	writer.Write([]byte(fmt.Sprintf("%04d", RDBVersion)))

	// Auxiliary fields (metadata)
	writer.Write([]byte{OpCodeAux})
	w.writeStringToWriter(writer, "redis-ver")
	w.writeStringToWriter(writer, "7.0.0")

	writer.Write([]byte{OpCodeAux})
	w.writeStringToWriter(writer, "ctime")
	w.writeStringToWriter(writer, fmt.Sprintf("%d", time.Now().Unix()))

	return nil
}

// writeKeyToWriter writes a single key-value pair to io.Writer (for checksum)
func (w *Writer) writeKeyToWriter(writer io.Writer, key string, value *storage.Value) error {
	// Write expiry if exists
	if value.ExpiresAt != nil && time.Now().Before(*value.ExpiresAt) {
		writer.Write([]byte{OpCodeExpireTimeMS})
		expiryMS := value.ExpiresAt.UnixMilli()
		binary.Write(writer, binary.LittleEndian, expiryMS)
	}

	// Write value type and data
	switch value.Type {
	case storage.StringType:
		if str, ok := value.Data.(string); ok {
			writer.Write([]byte{TypeString})
			w.writeStringToWriter(writer, key)
			w.writeStringToWriter(writer, str)
		}

	case storage.ListType:
		if list, ok := value.Data.([]string); ok {
			writer.Write([]byte{TypeList})
			w.writeStringToWriter(writer, key)
			w.writeLengthToWriter(writer, len(list))
			for _, item := range list {
				w.writeStringToWriter(writer, item)
			}
		}

	case storage.HashType:
		if hash, ok := value.Data.(map[string]string); ok {
			writer.Write([]byte{TypeHash})
			w.writeStringToWriter(writer, key)
			w.writeLengthToWriter(writer, len(hash))
			for field, val := range hash {
				w.writeStringToWriter(writer, field)
				w.writeStringToWriter(writer, val)
			}
		}

	case storage.SetType:
		if set, ok := value.Data.(map[string]struct{}); ok {
			writer.Write([]byte{TypeSet})
			w.writeStringToWriter(writer, key)
			w.writeLengthToWriter(writer, len(set))
			for member := range set {
				w.writeStringToWriter(writer, member)
			}
		}
	}

	return nil
}

// writeStringToWriter writes a length-prefixed string to io.Writer
func (w *Writer) writeStringToWriter(writer io.Writer, s string) {
	w.writeLengthToWriter(writer, len(s))
	writer.Write([]byte(s))
}

// writeLengthToWriter writes length to io.Writer (for checksum)
func (w *Writer) writeLengthToWriter(writer io.Writer, length int) {
	if length < 64 {
		// 6-bit length
		writer.Write([]byte{byte(length)})
	} else if length < 16384 {
		// 14-bit length
		writer.Write([]byte{
			byte(0x40 | (length >> 8)),
			byte(length & 0xFF),
		})
	} else {
		// 32-bit length
		writer.Write([]byte{0x80})
		binary.Write(writer, binary.BigEndian, uint32(length))
	}
}
