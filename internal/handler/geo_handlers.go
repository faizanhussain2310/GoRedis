package handler

import (
	"fmt"
	"strconv"
	"strings"

	"redis/internal/processor"
	"redis/internal/protocol"
	"redis/internal/storage"
)

// handleGeoAdd adds geospatial items to a key
// GEOADD key longitude latitude member [longitude latitude member ...]
func (h *CommandHandler) handleGeoAdd(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 5 || (len(cmd.Args)-2)%3 != 0 {
		return protocol.EncodeError("ERR wrong number of arguments for 'geoadd' command")
	}

	key := cmd.Args[1]
	points := make([]storage.GeoPoint, 0)

	// Parse longitude-latitude-member triplets
	for i := 2; i < len(cmd.Args); i += 3 {
		longitude, err := strconv.ParseFloat(cmd.Args[i], 64)
		if err != nil {
			return protocol.EncodeError("ERR value is not a valid float")
		}

		latitude, err := strconv.ParseFloat(cmd.Args[i+1], 64)
		if err != nil {
			return protocol.EncodeError("ERR value is not a valid float")
		}

		member := cmd.Args[i+2]

		points = append(points, storage.GeoPoint{
			Longitude: longitude,
			Latitude:  latitude,
			Member:    member,
		})
	}

	procCmd := &processor.Command{
		Type:     processor.CmdGeoAdd,
		Key:      key,
		Args:     []interface{}{points},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	added := result.(processor.IntResult).Result
	if added < 0 {
		return protocol.EncodeError("ERR invalid longitude,latitude pair")
	}
	return protocol.EncodeInteger(added)
}

// handleGeoPos returns positions of members
// GEOPOS key member [member ...]
func (h *CommandHandler) handleGeoPos(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'geopos' command")
	}

	key := cmd.Args[1]
	members := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdGeoPos,
		Key:      key,
		Args:     []interface{}{members},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	positions := result.([]*storage.GeoPoint)

	// Build response array
	response := make([]interface{}, len(positions))
	for i, pos := range positions {
		if pos == nil {
			response[i] = nil
		} else {
			response[i] = []interface{}{
				fmt.Sprintf("%.6f", pos.Longitude),
				fmt.Sprintf("%.6f", pos.Latitude),
			}
		}
	}

	return protocol.EncodeInterfaceArray(response)
}

// handleGeoDist returns distance between two members
// GEODIST key member1 member2 [unit]
func (h *CommandHandler) handleGeoDist(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 4 {
		return protocol.EncodeError("ERR wrong number of arguments for 'geodist' command")
	}

	key := cmd.Args[1]
	member1 := cmd.Args[2]
	member2 := cmd.Args[3]
	unit := "m"
	if len(cmd.Args) > 4 {
		unit = strings.ToLower(cmd.Args[4])
		if unit != "m" && unit != "km" && unit != "mi" && unit != "ft" {
			return protocol.EncodeError("ERR unsupported unit provided. please use m, km, ft, mi")
		}
	}

	procCmd := &processor.Command{
		Type:     processor.CmdGeoDist,
		Key:      key,
		Args:     []interface{}{member1, member2, unit},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	floatResult := result.(processor.Float64Result)
	if floatResult.Result == 0 && floatResult.Err == nil {
		// One or both members not found
		return protocol.EncodeNullBulkString()
	}

	return protocol.EncodeBulkString(fmt.Sprintf("%.4f", floatResult.Result))
}

// handleGeoHash returns geohash strings of members
// GEOHASH key member [member ...]
func (h *CommandHandler) handleGeoHash(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'geohash' command")
	}

	key := cmd.Args[1]
	members := cmd.Args[2:]

	procCmd := &processor.Command{
		Type:     processor.CmdGeoHash,
		Key:      key,
		Args:     []interface{}{members},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	hashes := result.([]string)

	// Build response array
	response := make([]interface{}, len(hashes))
	for i, hash := range hashes {
		if hash == "" {
			response[i] = nil
		} else {
			response[i] = hash
		}
	}

	return protocol.EncodeInterfaceArray(response)
}

// handleGeoRadius returns members within radius of a point
// GEORADIUS key longitude latitude radius m|km|ft|mi [WITHDIST] [WITHHASH] [WITHCOORD] [COUNT count]
func (h *CommandHandler) handleGeoRadius(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 6 {
		return protocol.EncodeError("ERR wrong number of arguments for 'georadius' command")
	}

	key := cmd.Args[1]

	longitude, err := strconv.ParseFloat(cmd.Args[2], 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not a valid float")
	}

	latitude, err := strconv.ParseFloat(cmd.Args[3], 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not a valid float")
	}

	radius, err := strconv.ParseFloat(cmd.Args[4], 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not a valid float")
	}

	unit := strings.ToLower(cmd.Args[5])
	if unit != "m" && unit != "km" && unit != "mi" && unit != "ft" {
		return protocol.EncodeError("ERR unsupported unit provided. please use m, km, ft, mi")
	}

	// Parse optional parameters
	withDist := false
	withHash := false
	withCoord := false
	count := -1

	for i := 6; i < len(cmd.Args); i++ {
		arg := strings.ToUpper(cmd.Args[i])
		switch arg {
		case "WITHDIST":
			withDist = true
		case "WITHHASH":
			withHash = true
		case "WITHCOORD":
			withCoord = true
		case "COUNT":
			if i+1 < len(cmd.Args) {
				c, err := strconv.Atoi(cmd.Args[i+1])
				if err != nil {
					return protocol.EncodeError("ERR value is not an integer or out of range")
				}
				count = c
				i++
			}
		}
	}

	procCmd := &processor.Command{
		Type:     processor.CmdGeoRadius,
		Key:      key,
		Args:     []interface{}{longitude, latitude, radius, unit, withDist, withHash, withCoord, count},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	results := result.([]storage.GeoRadiusResult)
	return encodeGeoRadiusResults(results, withDist, withHash, withCoord)
}

// handleGeoRadiusByMember returns members within radius of an existing member
// GEORADIUSBYMEMBER key member radius m|km|ft|mi [WITHDIST] [WITHHASH] [WITHCOORD] [COUNT count]
func (h *CommandHandler) handleGeoRadiusByMember(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 5 {
		return protocol.EncodeError("ERR wrong number of arguments for 'georadiusbymember' command")
	}

	key := cmd.Args[1]
	member := cmd.Args[2]

	radius, err := strconv.ParseFloat(cmd.Args[3], 64)
	if err != nil {
		return protocol.EncodeError("ERR value is not a valid float")
	}

	unit := strings.ToLower(cmd.Args[4])
	if unit != "m" && unit != "km" && unit != "mi" && unit != "ft" {
		return protocol.EncodeError("ERR unsupported unit provided. please use m, km, ft, mi")
	}

	// Parse optional parameters
	withDist := false
	withHash := false
	withCoord := false
	count := -1

	for i := 5; i < len(cmd.Args); i++ {
		arg := strings.ToUpper(cmd.Args[i])
		switch arg {
		case "WITHDIST":
			withDist = true
		case "WITHHASH":
			withHash = true
		case "WITHCOORD":
			withCoord = true
		case "COUNT":
			if i+1 < len(cmd.Args) {
				c, err := strconv.Atoi(cmd.Args[i+1])
				if err != nil {
					return protocol.EncodeError("ERR value is not an integer or out of range")
				}
				count = c
				i++
			}
		}
	}

	procCmd := &processor.Command{
		Type:     processor.CmdGeoRadiusByMember,
		Key:      key,
		Args:     []interface{}{member, radius, unit, withDist, withHash, withCoord, count},
		Response: make(chan interface{}, 1),
	}
	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	results := result.([]storage.GeoRadiusResult)
	if results == nil {
		return protocol.EncodeInterfaceArray([]interface{}{})
	}
	return encodeGeoRadiusResults(results, withDist, withHash, withCoord)
}

// encodeGeoRadiusResults encodes GEORADIUS results based on options
func encodeGeoRadiusResults(results []storage.GeoRadiusResult, withDist, withHash, withCoord bool) []byte {
	response := make([]interface{}, len(results))

	for i, r := range results {
		if !withDist && !withHash && !withCoord {
			// Simple member name
			response[i] = r.Member
		} else {
			// Complex response with additional info
			info := make([]interface{}, 0)
			info = append(info, r.Member)

			if withDist {
				info = append(info, fmt.Sprintf("%.4f", r.Distance))
			}
			if withHash {
				info = append(info, r.GeoHash)
			}
			if withCoord {
				info = append(info, []interface{}{
					fmt.Sprintf("%.6f", r.Point.Longitude),
					fmt.Sprintf("%.6f", r.Point.Latitude),
				})
			}

			response[i] = info
		}
	}

	return protocol.EncodeInterfaceArray(response)
}
