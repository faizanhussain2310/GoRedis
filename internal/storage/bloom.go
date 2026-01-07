package storage

import (
	"hash/fnv"
	"math"
	"time"
)

// BloomFilter represents a probabilistic data structure for set membership testing
// Note: No locking needed - all operations execute sequentially in the single processor goroutine
type BloomFilter struct {
	bits      []uint64 // Bit array stored as uint64 slices for efficiency
	size      uint64   // Size of bit array (m)
	numHashes uint32   // Number of hash functions (k)
	capacity  uint64   // Expected number of elements (n)
	errorRate float64  // False positive probability (p)
	count     uint64   // Approximate count of elements added
}

// BloomFilterInfo contains information about a Bloom filter
type BloomFilterInfo struct {
	Capacity    uint64
	Size        uint64
	NumHashes   uint32
	Count       uint64
	ErrorRate   float64
	BitsPerItem float64
}

// ==================== BLOOM FILTER CREATION ====================

// calculateOptimalParams calculates optimal bit array size (m) and number of hash functions (k)
// based on expected capacity (n) and desired error rate (p)
//
// Formulas:
//
//	m = -n * ln(p) / (ln(2))^2
//	k = (m/n) * ln(2)
func calculateOptimalParams(capacity uint64, errorRate float64) (size uint64, numHashes uint32) {
	n := float64(capacity)
	p := errorRate

	// Calculate optimal bit array size
	// m = -n * ln(p) / (ln(2))^2
	m := -n * math.Log(p) / (math.Ln2 * math.Ln2)

	// Round up to nearest multiple of 64 for efficient storage
	size = uint64(math.Ceil(m/64.0)) * 64

	// Calculate optimal number of hash functions
	// k = (m/n) * ln(2)
	k := (float64(size) / n) * math.Ln2
	numHashes = uint32(math.Round(k))

	// Ensure at least 1 hash function
	if numHashes < 1 {
		numHashes = 1
	}

	return size, numHashes
}

// newBloomFilter creates a new Bloom filter with specified parameters
func newBloomFilter(capacity uint64, errorRate float64) *BloomFilter {
	if capacity == 0 {
		capacity = 100 // Default capacity
	}
	if errorRate <= 0 || errorRate >= 1 {
		errorRate = 0.01 // Default 1% error rate
	}

	size, numHashes := calculateOptimalParams(capacity, errorRate)

	// Calculate number of uint64 elements needed
	// size is already a multiple of 64 from calculateOptimalParams
	numElements := size / 64

	return &BloomFilter{
		bits:      make([]uint64, numElements),
		size:      size,
		numHashes: numHashes,
		capacity:  capacity,
		errorRate: errorRate,
		count:     0,
	}
}

// ==================== HASH FUNCTIONS ====================

// hash generates k different hash values for the given key
// Uses FNV-1a hash with different seeds for each hash function
func (bf *BloomFilter) hash(key string) []uint64 {
	hashes := make([]uint64, bf.numHashes)

	// Primary hash
	h := fnv.New64a()
	h.Write([]byte(key))
	hash1 := h.Sum64()

	// Secondary hash (hash of hash for variety)
	h.Reset()
	h.Write([]byte(key + "salt"))
	hash2 := h.Sum64()

	// Generate k hash values using double hashing technique
	// h_i(x) = (hash1(x) + i * hash2(x)) mod m
	for i := uint32(0); i < bf.numHashes; i++ {
		combinedHash := hash1 + uint64(i)*hash2
		hashes[i] = combinedHash % bf.size
	}

	return hashes
}

// ==================== BIT OPERATIONS ====================

// setBit sets the bit at the specified position to 1
func (bf *BloomFilter) setBit(position uint64) {
	index := position / 64
	offset := position % 64
	bf.bits[index] |= (1 << offset)
}

// getBit returns the value of the bit at the specified position
func (bf *BloomFilter) getBit(position uint64) bool {
	index := position / 64
	offset := position % 64
	return (bf.bits[index] & (1 << offset)) != 0
}

// ==================== BLOOM FILTER OPERATIONS ====================

// BFReserve creates a new Bloom filter with specified error rate and capacity
func (s *Store) BFReserve(key string, errorRate float64, capacity uint64) error {
	// Check if key already exists
	if _, exists := s.data[key]; exists {
		return ErrInvalidOperation
	}

	bf := newBloomFilter(capacity, errorRate)

	s.data[key] = &Value{
		Data: bf,
		Type: BloomFilterType,
	}

	return nil
}

// BFAdd adds an item to the Bloom filter
// Returns true if item was newly added, false if it probably already existed
func (s *Store) BFAdd(key string, item string) (bool, error) {
	bf, err := s.getBloomFilter(key)
	if err != nil {
		return false, err
	}

	hashes := bf.hash(item)

	// Check if all bits are already set (item might exist)
	allBitsSet := true
	for _, hash := range hashes {
		if !bf.getBit(hash) {
			allBitsSet = false
			break
		}
	}

	// Set all bits
	for _, hash := range hashes {
		bf.setBit(hash)
	}

	// Increment count if item is new
	if !allBitsSet {
		bf.count++
		return true, nil // Item newly added
	}

	return false, nil // Item probably existed
}

// BFMAdd adds multiple items to the Bloom filter
// Returns a slice of booleans indicating which items were newly added
func (s *Store) BFMAdd(key string, items []string) ([]bool, error) {
	bf, err := s.getBloomFilter(key)
	if err != nil {
		return nil, err
	}

	results := make([]bool, len(items))

	for i, item := range items {
		hashes := bf.hash(item)

		// Check if all bits are already set
		allBitsSet := true
		for _, hash := range hashes {
			if !bf.getBit(hash) {
				allBitsSet = false
				break
			}
		}

		// Set all bits
		for _, hash := range hashes {
			bf.setBit(hash)
		}

		// Record result and update count
		if !allBitsSet {
			bf.count++
			results[i] = true // Item newly added
		} else {
			results[i] = false // Item probably existed
		}
	}

	return results, nil
}

// BFExists checks if an item exists in the Bloom filter
// Returns true if item might exist, false if it definitely doesn't exist
func (s *Store) BFExists(key string, item string) (bool, error) {
	bf, err := s.getBloomFilter(key)
	if err != nil {
		return false, err
	}

	hashes := bf.hash(item)

	// Check if all bits are set
	for _, hash := range hashes {
		if !bf.getBit(hash) {
			return false, nil // Definitely doesn't exist
		}
	}

	return true, nil // Might exist (or false positive)
}

// BFMExists checks if multiple items exist in the Bloom filter
// Returns a slice of booleans indicating which items might exist
func (s *Store) BFMExists(key string, items []string) ([]bool, error) {
	bf, err := s.getBloomFilter(key)
	if err != nil {
		return nil, err
	}

	results := make([]bool, len(items))

	for i, item := range items {
		hashes := bf.hash(item)

		exists := true
		for _, hash := range hashes {
			if !bf.getBit(hash) {
				exists = false
				break
			}
		}

		results[i] = exists
	}

	return results, nil
}

// BFInfo returns information about the Bloom filter
func (s *Store) BFInfo(key string) (*BloomFilterInfo, error) {
	bf, err := s.getBloomFilter(key)
	if err != nil {
		return nil, err
	}

	bitsPerItem := 0.0
	if bf.count > 0 {
		bitsPerItem = float64(bf.size) / float64(bf.count)
	}

	return &BloomFilterInfo{
		Capacity:    bf.capacity,
		Size:        bf.size,
		NumHashes:   bf.numHashes,
		Count:       bf.count,
		ErrorRate:   bf.errorRate,
		BitsPerItem: bitsPerItem,
	}, nil
}

// ==================== HELPER FUNCTIONS ====================

// getBloomFilter retrieves a Bloom filter from storage
func (s *Store) getBloomFilter(key string) (*BloomFilter, error) {
	val, exists := s.data[key]

	if !exists {
		return nil, ErrKeyNotFound
	}

	// Check expiry
	if val.ExpiresAt != nil && time.Now().After(*val.ExpiresAt) {
		s.deleteKey(key)
		return nil, ErrKeyNotFound // Expired
	}

	if val.Type != BloomFilterType {
		return nil, ErrInvalidOperation
	}

	bf, ok := val.Data.(*BloomFilter)
	if !ok {
		return nil, ErrInvalidOperation
	}

	return bf, nil
}

// calculateActualErrorRate calculates the actual false positive rate
// based on current fill rate of the bit array
func (bf *BloomFilter) calculateActualErrorRate() float64 {
	// Count set bits
	setBits := uint64(0)
	for _, word := range bf.bits {
		// Count bits using Brian Kernighan's algorithm
		for w := word; w != 0; w &= w - 1 {
			setBits++
		}
	}

	if bf.size == 0 {
		return 0.0
	}

	// Actual false positive rate: (1 - e^(-k*n/m))^k
	// where k = numHashes, n = count, m = size
	exponent := -float64(bf.numHashes) * float64(bf.count) / float64(bf.size)
	falsePositiveRate := math.Pow(1.0-math.Exp(exponent), float64(bf.numHashes))

	return falsePositiveRate
}
