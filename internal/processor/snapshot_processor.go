package processor

// executeSnapshot creates a snapshot of all data for AOF rewrite
// Returns raw data snapshot - filtering and command conversion happens in background
func (p *Processor) executeSnapshot(cmd *Command) {
	// Get all data from storage (shallow copy with COW)
	// No filtering here - keeps processor thread fast!
	allData := p.store.GetAllData()
	cmd.Response <- allData
}

// executeDataSnapshot returns raw storage data for RDB snapshots
// Returns raw data snapshot - filtering happens in background
func (p *Processor) executeDataSnapshot(cmd *Command) {
	// GetAllData returns shallow copy with COW, safe for background processing
	// No filtering here - keeps processor thread fast!
	snapshot := p.store.GetAllData()
	cmd.Response <- snapshot
}
