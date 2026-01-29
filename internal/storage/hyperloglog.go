package storage

import (
	"hash/fnv"
	"math"
	"math/bits"
	"time"
)

// HyperLogLog implements the HyperLogLog probabilistic cardinality estimator
// It can estimate the number of unique elements in a set with ~0.81% standard error
// using only 12KB of memory (for 16384 registers with precision 14)
//
// Algorithm: HyperLogLog uses the observation that the maximum number of leading zeros
// in the binary representation of hash values is a good indicator of cardinality.
// With m registers (m = 2^precision), it hashes each element and uses the first 'precision'
// bits to select a register, then stores the position of the first 1-bit in the remaining bits.
type HyperLogLog struct {
	registers []uint8 // Array of registers (each stores max leading zeros count)
	precision uint8   // Number of bits for register indexing (typically 14)
	m         uint32  // Number of registers (2^precision, typically 16384)
	alpha     float64 // Bias correction constant
}

const (
	// DefaultPrecision provides ~0.81% standard error with 16384 registers (12KB memory)
	DefaultPrecision = 14

	// MinPrecision is 4 (16 registers, higher error but minimal memory)
	MinPrecision = 4

	// MaxPrecision is 16 (65536 registers, 64KB memory, best accuracy)
	MaxPrecision = 16
)

// NewHyperLogLog creates a new HyperLogLog with the specified precision
// Precision p means 2^p registers will be used
// Redis uses p=14 (16384 registers = 12KB)
func NewHyperLogLog(precision uint8) *HyperLogLog {
	if precision < MinPrecision {
		precision = MinPrecision
	}
	if precision > MaxPrecision {
		precision = MaxPrecision
	}

	m := uint32(1 << precision) // m = 2^precision

	// Calculate bias correction constant alpha
	// For large m (m >= 128), alpha ≈ 0.7213 / (1 + 1.079/m)
	var alpha float64
	switch m {
	case 16:
		alpha = 0.673
	case 32:
		alpha = 0.697
	case 64:
		alpha = 0.709
	default:
		alpha = 0.7213 / (1 + 1.079/float64(m))
	}

	return &HyperLogLog{
		registers: make([]uint8, m),
		precision: precision,
		m:         m,
		alpha:     alpha,
	}
}

// Add adds an element to the HyperLogLog
// Returns true if the register was updated (element potentially new)
func (hll *HyperLogLog) Add(element string) bool {
	// Hash the element
	hash := hashString(element)

	// Use first 'precision' bits to determine register index
	registerIndex := hash >> (64 - hll.precision)

	// Use remaining bits to find position of first 1-bit (leading zeros + 1)
	// We need to mask out the bits used for register index
	w := hash << hll.precision

	// Count leading zeros, but cap at the valid bit range (64 - precision)
	leadingZeros := uint8(bits.LeadingZeros64(w))
	maxLeadingZeros := uint8(64 - hll.precision)

	if leadingZeros > maxLeadingZeros {
		leadingZeros = maxLeadingZeros
	}

	leadingZeros++ // Position is 1-indexed

	// Update register if new value is larger
	if leadingZeros > hll.registers[registerIndex] {
		hll.registers[registerIndex] = leadingZeros
		return true
	}

	return false
}

// Count estimates the cardinality (number of unique elements)
func (hll *HyperLogLog) Count() int64 {
	// Calculate raw estimate using harmonic mean
	sum := 0.0
	zeros := 0

	for _, val := range hll.registers {
		sum += 1.0 / math.Pow(2.0, float64(val))
		if val == 0 {
			zeros++
		}
	}

	// Raw estimate: E = alpha * m^2 / sum
	rawEstimate := hll.alpha * float64(hll.m) * float64(hll.m) / sum

	// Apply bias correction for different ranges
	var estimate float64

	// Small range correction (if many registers are zero)
	if rawEstimate <= 2.5*float64(hll.m) {
		if zeros > 0 {
			// Linear counting for small cardinalities
			estimate = float64(hll.m) * math.Log(float64(hll.m)/float64(zeros))
		} else {
			estimate = rawEstimate
		}
	} else if rawEstimate <= (1.0/30.0)*math.Pow(2.0, 32) {
		// No correction for intermediate range
		estimate = rawEstimate
	} else {
		// Large range correction (near 2^32)
		estimate = -math.Pow(2.0, 32) * math.Log(1.0-rawEstimate/math.Pow(2.0, 32))
	}

	return int64(estimate + 0.5) // Round to nearest integer
}

// Merge merges multiple HyperLogLogs into this one
// All HyperLogLogs must have the same precision
// Returns error if precision mismatch
func (hll *HyperLogLog) Merge(others ...*HyperLogLog) error {
	for _, other := range others {
		if other.precision != hll.precision {
			return ErrPrecisionMismatch
		}

		// Take maximum value for each register
		for i := range hll.registers {
			if other.registers[i] > hll.registers[i] {
				hll.registers[i] = other.registers[i]
			}
		}
	}

	return nil
}

// Clone creates a deep copy of the HyperLogLog
func (hll *HyperLogLog) Clone() *HyperLogLog {
	clone := &HyperLogLog{
		registers: make([]uint8, len(hll.registers)),
		precision: hll.precision,
		m:         hll.m,
		alpha:     hll.alpha,
	}
	copy(clone.registers, hll.registers)
	return clone
}

// Reset clears all registers
func (hll *HyperLogLog) Reset() {
	for i := range hll.registers {
		hll.registers[i] = 0
	}
}

// GetRegisters returns a copy of the registers (for serialization)
func (hll *HyperLogLog) GetRegisters() []uint8 {
	registers := make([]uint8, len(hll.registers))
	copy(registers, hll.registers)
	return registers
}

// SetRegisters sets the registers from a byte slice (for deserialization)
func (hll *HyperLogLog) SetRegisters(registers []uint8) error {
	if len(registers) != int(hll.m) {
		return ErrInvalidRegisterCount
	}
	copy(hll.registers, registers)
	return nil
}

// GetPrecision returns the precision of the HyperLogLog
func (hll *HyperLogLog) GetPrecision() uint8 {
	return hll.precision
}

// hashString hashes a string to a 64-bit value using FNV-1a
func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// ==================== HYPERLOGLOG OPERATIONS ====================

// PFAdd adds elements to a HyperLogLog
// Creates a new HLL if key doesn't exist
// Returns true if at least one register was updated (element potentially new)
func (s *Store) PFAdd(key string, elements []string) (bool, error) {
	// Get existing HLL or create new one
	hll, err := s.getHyperLogLog(key)
	if err != nil {
		if err == ErrKeyNotFound {
			// Create new HLL with default precision
			hll = NewHyperLogLog(DefaultPrecision)
		} else {
			return false, err
		}
	}

	// Copy-on-write: clone HLL if snapshot is active
	if s.isSnapshotActive() && s.data[key] != nil {
		hll = hll.Clone()
	}

	// Add all elements, track if any register was updated
	updated := false
	for _, element := range elements {
		if hll.Add(element) {
			updated = true
		}
	}

	// Save HLL back to storage
	s.data[key] = &Value{
		Data: hll,
		Type: HyperLogLogType,
	}

	return updated, nil
}

// PFCount returns the approximated cardinality of one or more HyperLogLogs
// If multiple keys provided, merges them temporarily and returns combined count
func (s *Store) PFCount(keys []string) (int64, error) {
	if len(keys) == 0 {
		return 0, ErrInvalidOperation
	}

	// Single key case - simple count
	if len(keys) == 1 {
		hll, err := s.getHyperLogLog(keys[0])
		if err != nil {
			if err == ErrKeyNotFound {
				return 0, nil // Non-existent key returns 0
			}
			return 0, err
		}
		return hll.Count(), nil
	}

	// Multiple keys - need to merge temporarily
	var hlls []*HyperLogLog

	for _, key := range keys {
		hll, err := s.getHyperLogLog(key)
		if err != nil {
			if err == ErrKeyNotFound {
				continue // Skip non-existent keys
			}
			return 0, err
		}
		hlls = append(hlls, hll)
	}

	// If no valid HLLs found, return 0
	if len(hlls) == 0 {
		return 0, nil
	}

	// If only one valid HLL, return its count
	if len(hlls) == 1 {
		return hlls[0].Count(), nil
	}

	// Create temporary HLL for merging
	// Use precision from first HLL
	tempHLL := NewHyperLogLog(hlls[0].precision)

	// Merge all HLLs
	if err := tempHLL.Merge(hlls...); err != nil {
		return 0, err
	}

	return tempHLL.Count(), nil
}

// PFMerge merges multiple HyperLogLogs into a destination key
// Destination key is created/overwritten
// All source HLLs must have the same precision
func (s *Store) PFMerge(destKey string, sourceKeys []string) error {
	if len(sourceKeys) == 0 {
		return ErrInvalidOperation
	}

	// Collect all source HLLs
	var sourceHLLs []*HyperLogLog

	for _, key := range sourceKeys {
		hll, err := s.getHyperLogLog(key)
		if err != nil {
			if err == ErrKeyNotFound {
				continue // Skip non-existent keys
			}
			return err
		}
		sourceHLLs = append(sourceHLLs, hll)
	}

	// If no sources exist, create empty HLL at destination
	if len(sourceHLLs) == 0 {
		emptyHLL := NewHyperLogLog(DefaultPrecision)
		s.data[destKey] = &Value{
			Data: emptyHLL,
			Type: HyperLogLogType,
		}
		return nil
	}

	// Create new HLL with same precision as first source
	destHLL := NewHyperLogLog(sourceHLLs[0].precision)

	// Merge all sources
	if err := destHLL.Merge(sourceHLLs...); err != nil {
		return err
	}

	// Store at destination (overwrites if exists)
	s.data[destKey] = &Value{
		Data: destHLL,
		Type: HyperLogLogType,
	}

	return nil
}

// getHyperLogLog retrieves a HyperLogLog from storage
func (s *Store) getHyperLogLog(key string) (*HyperLogLog, error) {
	val, exists := s.data[key]

	if !exists {
		return nil, ErrKeyNotFound
	}

	// Check expiry
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return nil, ErrKeyNotFound // Expired
	}

	if val.Type != HyperLogLogType {
		return nil, ErrInvalidOperation
	}

	hll, ok := val.Data.(*HyperLogLog)
	if !ok {
		return nil, ErrInvalidOperation
	}

	return hll, nil
}
