package processor

import "redis/internal/storage"

// executeGeoCommand executes geospatial commands
func (p *Processor) executeGeoCommand(cmd *Command) {
	switch cmd.Type {
	case CmdGeoAdd:
		p.executeGeoAdd(cmd)
	case CmdGeoPos:
		p.executeGeoPos(cmd)
	case CmdGeoDist:
		p.executeGeoDist(cmd)
	case CmdGeoHash:
		p.executeGeoHash(cmd)
	case CmdGeoRadius:
		p.executeGeoRadius(cmd)
	case CmdGeoRadiusByMember:
		p.executeGeoRadiusByMember(cmd)
	default:
		cmd.Response <- IntResult{Result: 0, Err: nil}
	}
}

// executeGeoAdd adds geospatial items
func (p *Processor) executeGeoAdd(cmd *Command) {
	points := cmd.Args[0].([]storage.GeoPoint)
	count := p.store.GeoAdd(cmd.Key, points)
	cmd.Response <- IntResult{Result: count}
}

// executeGeoPos returns positions of members
func (p *Processor) executeGeoPos(cmd *Command) {
	members := cmd.Args[0].([]string)
	positions := p.store.GeoPos(cmd.Key, members)
	cmd.Response <- positions
}

// executeGeoDist returns distance between two members
func (p *Processor) executeGeoDist(cmd *Command) {
	member1 := cmd.Args[0].(string)
	member2 := cmd.Args[1].(string)
	unit := "m"
	if len(cmd.Args) > 2 {
		unit = cmd.Args[2].(string)
	}

	distance := p.store.GeoDist(cmd.Key, member1, member2, unit)
	if distance == nil {
		cmd.Response <- Float64Result{Result: 0, Err: nil}
	} else {
		cmd.Response <- Float64Result{Result: *distance, Err: nil}
	}
}

// executeGeoHash returns geohash strings of members
func (p *Processor) executeGeoHash(cmd *Command) {
	members := cmd.Args[0].([]string)
	hashes := p.store.GeoHash(cmd.Key, members)
	cmd.Response <- hashes
}

// executeGeoRadius returns members within radius of a point
func (p *Processor) executeGeoRadius(cmd *Command) {
	longitude := cmd.Args[0].(float64)
	latitude := cmd.Args[1].(float64)
	radius := cmd.Args[2].(float64)
	unit := cmd.Args[3].(string)

	withDist := false
	withHash := false
	withCoord := false
	count := -1

	if len(cmd.Args) > 4 {
		withDist = cmd.Args[4].(bool)
	}
	if len(cmd.Args) > 5 {
		withHash = cmd.Args[5].(bool)
	}
	if len(cmd.Args) > 6 {
		withCoord = cmd.Args[6].(bool)
	}
	if len(cmd.Args) > 7 {
		count = cmd.Args[7].(int)
	}

	results := p.store.GeoRadius(cmd.Key, longitude, latitude, radius, unit, withDist, withHash, withCoord, count)
	cmd.Response <- results
}

// executeGeoRadiusByMember returns members within radius of an existing member
func (p *Processor) executeGeoRadiusByMember(cmd *Command) {
	member := cmd.Args[0].(string)
	radius := cmd.Args[1].(float64)
	unit := cmd.Args[2].(string)

	withDist := false
	withHash := false
	withCoord := false
	count := -1

	if len(cmd.Args) > 3 {
		withDist = cmd.Args[3].(bool)
	}
	if len(cmd.Args) > 4 {
		withHash = cmd.Args[4].(bool)
	}
	if len(cmd.Args) > 5 {
		withCoord = cmd.Args[5].(bool)
	}
	if len(cmd.Args) > 6 {
		count = cmd.Args[6].(int)
	}

	results := p.store.GeoRadiusByMember(cmd.Key, member, radius, unit, withDist, withHash, withCoord, count)
	cmd.Response <- results
}
