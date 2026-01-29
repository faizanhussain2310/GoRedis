package storage

import (
	"math/bits"
	"time"
)

// Bitmaps in Redis are strings treated as bit arrays
// This provides efficient bit-level operations on binary data

// ==================== BITMAP OPERATIONS ====================

// SetBit sets or clears the bit at offset in the string value stored at key
// The bit is either set (1) or cleared (0) based on the value parameter
// Returns the original bit value at offset
func (s *Store) SetBit(key string, offset int64, value int) (int, error) {
	if offset < 0 {
		return 0, ErrInvalidOperation
	}

	if value != 0 && value != 1 {
		return 0, ErrInvalidOperation
	}

	// Get existing value or create empty string
	str, err := s.getString(key)
	if err != nil && err != ErrKeyNotFound {
		return 0, err
	}
	// If key doesn't exist or is expired, str will be empty string

	// Calculate byte index and bit offset within that byte
	byteIndex := offset / 8
	bitOffset := uint(7 - (offset % 8)) // Bits are numbered from left to right

	// Expand string if necessary
	requiredLen := byteIndex + 1
	currentLen := int64(len(str))
	if currentLen < requiredLen {
		// Pad with zero bytes
		padding := make([]byte, requiredLen-currentLen)
		str = str + string(padding)
	}

	// Get current byte and bit value
	bytes := []byte(str)
	currentByte := bytes[byteIndex]
	oldBit := int((currentByte >> bitOffset) & 1)

	// Set or clear the bit
	if value == 1 {
		bytes[byteIndex] = currentByte | (1 << bitOffset)
	} else {
		bytes[byteIndex] = currentByte &^ (1 << bitOffset)
	}

	// Save back to storage
	s.data[key] = &Value{
		Data: string(bytes),
		Type: StringType,
	}

	return oldBit, nil
}

// GetBit returns the bit value at offset in the string value stored at key
// Returns 0 if the key doesn't exist or the offset is beyond the string length
func (s *Store) GetBit(key string, offset int64) (int, error) {
	if offset < 0 {
		return 0, ErrInvalidOperation
	}

	str, err := s.getString(key)
	if err == ErrKeyNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	byteIndex := offset / 8
	bitOffset := uint(7 - (offset % 8))

	// If offset is beyond string length, return 0
	if byteIndex >= int64(len(str)) {
		return 0, nil
	}

	// Get the bit value
	currentByte := str[byteIndex]
	bit := int((currentByte >> bitOffset) & 1)

	return bit, nil
}

// BitCount returns the number of bits set to 1 in the string
// Optional start and end parameters specify byte range (not bit range)
func (s *Store) BitCount(key string, start, end *int64) (int64, error) {
	str, err := s.getString(key)
	if err == ErrKeyNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	strLen := int64(len(str))

	if strLen == 0 {
		return 0, nil
	}

	// Determine byte range
	startByte := int64(0)
	endByte := strLen - 1

	if start != nil {
		startByte = *start
		if startByte < 0 {
			startByte = strLen + startByte
		}
		if startByte < 0 {
			startByte = 0
		}
	}

	if end != nil {
		endByte = *end
		if endByte < 0 {
			endByte = strLen + endByte
		}
		if endByte >= strLen {
			endByte = strLen - 1
		}
	}

	if startByte > endByte || startByte >= strLen {
		return 0, nil
	}

	// Count bits in the specified range
	count := int64(0)
	for i := startByte; i <= endByte; i++ {
		count += int64(bits.OnesCount8(uint8(str[i])))
	}

	return count, nil
}

// BitPos returns the position of the first bit set to 1 or 0 in the string
// Optional start and end parameters specify byte range
func (s *Store) BitPos(key string, bit int, start, end *int64) (int64, error) {
	if bit != 0 && bit != 1 {
		return 0, ErrInvalidOperation
	}

	str, err := s.getString(key)
	if err == ErrKeyNotFound {
		// If looking for 0 bit in non-existent key, return 0
		if bit == 0 {
			return 0, nil
		}
		// If looking for 1 bit in non-existent key, return -1
		return -1, nil
	}
	if err != nil {
		return 0, err
	}
	strLen := int64(len(str))

	if strLen == 0 {
		if bit == 0 {
			return 0, nil
		}
		return -1, nil
	}

	// Determine byte range
	startByte := int64(0)
	endByte := strLen - 1

	if start != nil {
		startByte = *start
		if startByte < 0 {
			startByte = strLen + startByte
		}
		if startByte < 0 {
			startByte = 0
		}
	}

	if end != nil {
		endByte = *end
		if endByte < 0 {
			endByte = strLen + endByte
		}
		if endByte >= strLen {
			endByte = strLen - 1
		}
	}

	if startByte > endByte || startByte >= strLen {
		return -1, nil
	}

	// Search for the bit
	for i := startByte; i <= endByte; i++ {
		currentByte := uint8(str[i])

		// Check each bit in the byte
		for bitOffset := uint(0); bitOffset < 8; bitOffset++ {
			bitValue := int((currentByte >> (7 - bitOffset)) & 1)
			if bitValue == bit {
				return i*8 + int64(bitOffset), nil
			}
		}
	}

	// Bit not found in range
	return -1, nil
}

// BitOpAnd performs bitwise AND between multiple keys and stores result in destkey
func (s *Store) BitOpAnd(destKey string, srcKeys []string) (int64, error) {
	return s.bitOperation(destKey, srcKeys, func(a, b byte) byte { return a & b })
}

// BitOpOr performs bitwise OR between multiple keys and stores result in destkey
func (s *Store) BitOpOr(destKey string, srcKeys []string) (int64, error) {
	return s.bitOperation(destKey, srcKeys, func(a, b byte) byte { return a | b })
}

// BitOpXor performs bitwise XOR between multiple keys and stores result in destkey
func (s *Store) BitOpXor(destKey string, srcKeys []string) (int64, error) {
	return s.bitOperation(destKey, srcKeys, func(a, b byte) byte { return a ^ b })
}

// BitOpNot performs bitwise NOT on a single key and stores result in destkey
func (s *Store) BitOpNot(destKey string, srcKey string) (int64, error) {
	str, err := s.getString(srcKey)
	if err == ErrKeyNotFound {
		// NOT of empty string is empty string
		s.data[destKey] = &Value{
			Data: "",
			Type: StringType,
		}
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	result := make([]byte, len(str))

	for i := 0; i < len(str); i++ {
		result[i] = ^str[i]
	}

	s.data[destKey] = &Value{
		Data: string(result),
		Type: StringType,
	}

	return int64(len(result)), nil
}

// ==================== HELPER FUNCTIONS ====================

// getString retrieves a string value from storage with expiry and type checking
func (s *Store) getString(key string) (string, error) {
	val, exists := s.data[key]
	if !exists {
		return "", ErrKeyNotFound
	}

	// Check expiry
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return "", ErrKeyNotFound
	}

	if val.Type != StringType {
		return "", ErrWrongType
	}

	return val.Data.(string), nil
}

// bitOperation performs a bitwise operation between multiple keys
func (s *Store) bitOperation(destKey string, srcKeys []string, op func(byte, byte) byte) (int64, error) {
	if len(srcKeys) == 0 {
		return 0, ErrInvalidOperation
	}

	// Find the maximum length among all source strings
	maxLen := 0
	srcStrings := make([]string, len(srcKeys))

	for i, key := range srcKeys {
		str, err := s.getString(key)
		if err == ErrKeyNotFound {
			srcStrings[i] = ""
			continue
		}
		if err != nil {
			return 0, err
		}

		srcStrings[i] = str
		if len(srcStrings[i]) > maxLen {
			maxLen = len(srcStrings[i])
		}
	}

	// If all sources are empty, result is empty
	if maxLen == 0 {
		s.data[destKey] = &Value{
			Data: "",
			Type: StringType,
		}
		return 0, nil
	}

	// Perform the operation
	result := make([]byte, maxLen)

	// Initialize with first string (padded with zeros if needed)
	for i := 0; i < maxLen; i++ {
		if i < len(srcStrings[0]) {
			result[i] = srcStrings[0][i]
		} else {
			result[i] = 0
		}
	}

	// Apply operation with remaining strings
	for j := 1; j < len(srcStrings); j++ {
		for i := 0; i < maxLen; i++ {
			var b byte
			if i < len(srcStrings[j]) {
				b = srcStrings[j][i]
			} else {
				b = 0
			}
			result[i] = op(result[i], b)
		}
	}

	s.data[destKey] = &Value{
		Data: string(result),
		Type: StringType,
	}

	return int64(len(result)), nil
}
