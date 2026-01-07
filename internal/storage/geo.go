package storage

import (
	"math"
)

// Geospatial constants
const (
	earthRadiusKm    = 6371.0  // Earth's radius in kilometers
	earthRadiusMiles = 3959.0  // Earth's radius in miles
	earthRadiusM     = 6371000 // Earth's radius in meters
	geoHashStep      = 26      // Number of bits for geohash (52 bits total, 26 for lat, 26 for lon)
)

// GeoPoint represents a geographic location
type GeoPoint struct {
	Longitude float64
	Latitude  float64
	Member    string
}

// GeoRadiusResult represents a result from GEORADIUS query
type GeoRadiusResult struct {
	Member   string
	Distance float64
	GeoHash  int64
	Point    GeoPoint
}

// ==================== GEOHASH ENCODING/DECODING ====================

// geohashEncode converts latitude and longitude to a 52-bit geohash
// The geohash interleaves bits of longitude and latitude
// This ensures that nearby locations have similar geohash values
func geohashEncode(latitude, longitude float64) int64 {
	// Normalize to range [0, 1]
	lat := (latitude + 90.0) / 180.0
	lon := (longitude + 180.0) / 360.0

	// Convert to integer coordinates (26 bits each)
	latInt := int64(lat * float64(uint64(1)<<geoHashStep))
	lonInt := int64(lon * float64(uint64(1)<<geoHashStep))

	// Interleave bits (longitude in even positions, latitude in odd positions)
	var hash int64
	for i := 0; i < geoHashStep; i++ {
		hash |= ((lonInt >> i) & 1) << (2 * i)
		hash |= ((latInt >> i) & 1) << (2*i + 1)
	}

	return hash
}

// geohashDecode converts a 52-bit geohash back to latitude and longitude
func geohashDecode(hash int64) (latitude, longitude float64) {
	var latInt, lonInt int64

	// De-interleave bits
	for i := 0; i < geoHashStep; i++ {
		lonInt |= ((hash >> (2 * i)) & 1) << i
		latInt |= ((hash >> (2*i + 1)) & 1) << i
	}

	// Convert back to float coordinates
	longitude = float64(lonInt) / float64(uint64(1)<<geoHashStep)
	lat := float64(latInt) / float64(uint64(1)<<geoHashStep)

	// Denormalize from [0, 1] to actual coordinates
	latitude = lat*180.0 - 90.0
	longitude = longitude*360.0 - 180.0

	return latitude, longitude
}

// geohashEncodeString converts geohash to base32 string (for GEOHASH command)
func geohashEncodeString(hash int64) string {
	const base32 = "0123456789bcdefghjkmnpqrstuvwxyz"
	chars := make([]byte, 11) // 11 characters for 52 bits (52/5 ≈ 11)

	for i := 10; i >= 0; i-- {
		chars[i] = base32[hash&0x1f]
		hash >>= 5
	}

	return string(chars)
}

// ==================== DISTANCE CALCULATION ====================

// haversineDistance calculates the great-circle distance between two points
// using the Haversine formula
// Returns distance in meters
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	// Convert degrees to radians
	lat1Rad := lat1 * math.Pi / 180.0
	lat2Rad := lat2 * math.Pi / 180.0
	lon1Rad := lon1 * math.Pi / 180.0
	lon2Rad := lon2 * math.Pi / 180.0

	// Haversine formula
	dLat := lat2Rad - lat1Rad
	dLon := lon2Rad - lon1Rad

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	// Distance in meters
	return earthRadiusM * c
}

// ==================== GEOSPATIAL OPERATIONS ====================

// GeoAdd adds one or more geospatial items to a key
// Returns the number of elements added (not updated)
func (s *Store) GeoAdd(key string, points []GeoPoint) int {
	// Validate coordinates
	for _, point := range points {
		if !isValidCoordinate(point.Latitude, point.Longitude) {
			return -1 // Invalid coordinates
		}
	}

	// Convert to ZSET members with geohash as score
	members := make([]ZSetMember, len(points))
	for i, point := range points {
		hash := geohashEncode(point.Latitude, point.Longitude)
		members[i] = ZSetMember{
			Member: point.Member,
			Score:  float64(hash),
		}
	}

	// Use ZADD to store
	return s.ZAdd(key, members)
}

// GeoPos returns the positions (latitude, longitude) of members
func (s *Store) GeoPos(key string, members []string) []*GeoPoint {
	results := make([]*GeoPoint, len(members))

	for i, member := range members {
		score := s.ZScore(key, member)
		if score == nil {
			results[i] = nil
			continue
		}

		hash := int64(*score)
		lat, lon := geohashDecode(hash)
		results[i] = &GeoPoint{
			Longitude: lon,
			Latitude:  lat,
			Member:    member,
		}
	}

	return results
}

// GeoDist returns the distance between two members
func (s *Store) GeoDist(key, member1, member2, unit string) *float64 {
	// Get positions
	score1 := s.ZScore(key, member1)
	score2 := s.ZScore(key, member2)

	if score1 == nil || score2 == nil {
		return nil
	}

	// Decode coordinates
	lat1, lon1 := geohashDecode(int64(*score1))
	lat2, lon2 := geohashDecode(int64(*score2))

	// Calculate distance in meters
	distanceM := haversineDistance(lat1, lon1, lat2, lon2)

	// Convert to requested unit
	var distance float64
	switch unit {
	case "m":
		distance = distanceM
	case "km":
		distance = distanceM / 1000.0
	case "mi":
		distance = distanceM / 1609.34
	case "ft":
		distance = distanceM * 3.28084
	default:
		distance = distanceM // Default to meters
	}

	return &distance
}

// GeoHash returns the geohash string of members
func (s *Store) GeoHash(key string, members []string) []string {
	results := make([]string, len(members))

	for i, member := range members {
		score := s.ZScore(key, member)
		if score == nil {
			results[i] = ""
			continue
		}

		hash := int64(*score)
		results[i] = geohashEncodeString(hash)
	}

	return results
}

// GeoRadius returns members within a radius from a point
func (s *Store) GeoRadius(key string, longitude, latitude, radius float64, unit string, withDist, withHash, withCoord bool, count int) []GeoRadiusResult {
	if !isValidCoordinate(latitude, longitude) {
		return nil
	}

	// Convert radius to meters
	radiusM := convertToMeters(radius, unit)

	// Get center geohash
	centerHash := geohashEncode(latitude, longitude)

	// Estimate geohash range based on radius
	// This is an approximation - we'll filter more precisely later
	hashRange := estimateGeohashRange(radiusM)

	// Get all members in the approximate range
	minHash := float64(centerHash - hashRange)
	maxHash := float64(centerHash + hashRange)

	candidates := s.ZRangeByScore(key, minHash, maxHash, 0, -1)
	if candidates == nil {
		return nil
	}

	// Filter by actual distance
	results := make([]GeoRadiusResult, 0)
	for _, candidate := range candidates {
		candLat, candLon := geohashDecode(int64(candidate.Score))
		dist := haversineDistance(latitude, longitude, candLat, candLon)

		if dist <= radiusM {
			result := GeoRadiusResult{
				Member: candidate.Member,
			}

			if withDist {
				result.Distance = convertFromMeters(dist, unit)
			}
			if withHash {
				result.GeoHash = int64(candidate.Score)
			}
			if withCoord {
				result.Point = GeoPoint{
					Longitude: candLon,
					Latitude:  candLat,
					Member:    candidate.Member,
				}
			}

			results = append(results, result)
		}
	}

	// Limit results if count is specified
	if count > 0 && len(results) > count {
		results = results[:count]
	}

	return results
}

// GeoRadiusByMember returns members within a radius from an existing member
func (s *Store) GeoRadiusByMember(key, member string, radius float64, unit string, withDist, withHash, withCoord bool, count int) []GeoRadiusResult {
	// Get member's position
	score := s.ZScore(key, member)
	if score == nil {
		return nil
	}

	lat, lon := geohashDecode(int64(*score))
	return s.GeoRadius(key, lon, lat, radius, unit, withDist, withHash, withCoord, count)
}

// ==================== HELPER FUNCTIONS ====================

// isValidCoordinate checks if latitude and longitude are valid
func isValidCoordinate(latitude, longitude float64) bool {
	return latitude >= -90.0 && latitude <= 90.0 &&
		longitude >= -180.0 && longitude <= 180.0
}

// convertToMeters converts distance to meters based on unit
func convertToMeters(distance float64, unit string) float64 {
	switch unit {
	case "m":
		return distance
	case "km":
		return distance * 1000.0
	case "mi":
		return distance * 1609.34
	case "ft":
		return distance / 3.28084
	default:
		return distance // Default to meters
	}
}

// convertFromMeters converts distance from meters to specified unit
func convertFromMeters(distanceM float64, unit string) float64 {
	switch unit {
	case "m":
		return distanceM
	case "km":
		return distanceM / 1000.0
	case "mi":
		return distanceM / 1609.34
	case "ft":
		return distanceM * 3.28084
	default:
		return distanceM
	}
}

// estimateGeohashRange estimates the geohash range for a given radius
// This is a rough approximation - we filter by actual distance later
func estimateGeohashRange(radiusM float64) int64 {
	// At equator, 1 degree ≈ 111km
	// We need to estimate how many geohash units correspond to the radius

	// Approximate: each geohash step represents ~156 meters at finest resolution
	// (Earth circumference / 2^26 ≈ 156 meters)
	stepsPerMeter := float64(uint64(1)<<geoHashStep) / (40075000.0 / 2.0) // Half circumference

	// Add margin for safety (2x)
	return int64(radiusM * stepsPerMeter * 2.0)
}
