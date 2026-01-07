package server

// SentinelConfig holds configuration for standalone Sentinel instances
type SentinelConfig struct {
	Host            string   // Host to bind to
	Port            int      // Port for Sentinel to listen on
	MasterName      string   // Name of the master to monitor
	MasterHost      string   // Host of the master to monitor
	MasterPort      int      // Port of the master to monitor
	SentinelAddrs   []string // Addresses of other Sentinels (e.g., ["localhost:26379"])
	Quorum          int      // Number of sentinels that need to agree for failover
	DownAfterMillis int      // Milliseconds before marking instance down
	FailoverTimeout int      // Milliseconds for failover timeout
	MaxConnections  int      // Max client connections
}

// DefaultSentinelConfig returns default configuration for Sentinel
func DefaultSentinelConfig() *SentinelConfig {
	return &SentinelConfig{
		Host:            "0.0.0.0",
		Port:            26379,
		MasterName:      "mymaster",
		SentinelAddrs:   []string{},
		Quorum:          2,
		DownAfterMillis: 30000,  // 30 seconds
		FailoverTimeout: 180000, // 3 minutes
		MaxConnections:  10000,
	}
}
