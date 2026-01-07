package server

import (
	"fmt"
	"log"
	"time"

	"redis/internal/protocol"
	"redis/internal/rdb"
)

// loadRDB loads and restores data from the RDB file
func (s *RedisServer) loadRDB() error {
	startTime := time.Now()

	reader, err := rdb.NewReader(s.config.RDBFilepath)
	if err != nil {
		return fmt.Errorf("failed to create RDB reader: %w", err)
	}
	if reader == nil {
		// File doesn't exist - first startup
		log.Println("No RDB file found")
		return nil
	}
	defer reader.Close()

	log.Printf("Loading RDB file: %s", s.config.RDBFilepath)

	// Load all data from RDB file
	commands, err := reader.Load()
	if err != nil {
		return fmt.Errorf("failed to load RDB data: %w", err)
	}

	// Restore data by executing appropriate commands
	errorCount := 0
	for _, cmd := range commands {
		if err := s.restoreFromRDB(cmd); err != nil {
			log.Printf("RDB restore error for key %s: %v", cmd.Key, err)
			errorCount++
			// Continue loading despite errors
		}
	}

	duration := time.Since(startTime)
	log.Printf("RDB loaded: %d keys restored in %v", len(commands), duration)
	if errorCount > 0 {
		log.Printf("Warning: %d errors during RDB restore", errorCount)
	}

	return nil
}

// restoreFromRDB restores a single key from RDB data
func (s *RedisServer) restoreFromRDB(cmd rdb.LoadCommand) error {
	var args []string

	// Build command based on data type
	switch cmd.Type {
	case rdb.TypeString:
		value, ok := cmd.Value.(string)
		if !ok {
			return fmt.Errorf("invalid string value type")
		}

		if cmd.Expiration != nil {
			// SET key value PXAT timestamp
			expireMs := cmd.Expiration.UnixMilli()
			args = []string{"SET", cmd.Key, value, "PXAT", fmt.Sprintf("%d", expireMs)}
		} else {
			// SET key value
			args = []string{"SET", cmd.Key, value}
		}

	case rdb.TypeList:
		list, ok := cmd.Value.([]string)
		if !ok {
			return fmt.Errorf("invalid list value type")
		}

		// RPUSH key element1 element2 ...
		args = append([]string{"RPUSH", cmd.Key}, list...)

		// Set expiration separately if needed
		if cmd.Expiration != nil {
			if err := s.executeCommand(args); err != nil {
				return err
			}
			expireMs := cmd.Expiration.UnixMilli()
			args = []string{"PEXPIREAT", cmd.Key, fmt.Sprintf("%d", expireMs)}
		}

	case rdb.TypeHash:
		hash, ok := cmd.Value.(map[string]string)
		if !ok {
			return fmt.Errorf("invalid hash value type")
		}

		// HSET key field1 value1 field2 value2 ...
		args = []string{"HSET", cmd.Key}
		for field, value := range hash {
			args = append(args, field, value)
		}

		// Set expiration separately if needed
		if cmd.Expiration != nil {
			if err := s.executeCommand(args); err != nil {
				return err
			}
			expireMs := cmd.Expiration.UnixMilli()
			args = []string{"PEXPIREAT", cmd.Key, fmt.Sprintf("%d", expireMs)}
		}

	case rdb.TypeSet:
		set, ok := cmd.Value.(map[string]struct{})
		if !ok {
			return fmt.Errorf("invalid set value type")
		}

		// SADD key member1 member2 ...
		args = []string{"SADD", cmd.Key}
		for member := range set {
			args = append(args, member)
		}

		// Set expiration separately if needed
		if cmd.Expiration != nil {
			if err := s.executeCommand(args); err != nil {
				return err
			}
			expireMs := cmd.Expiration.UnixMilli()
			args = []string{"PEXPIREAT", cmd.Key, fmt.Sprintf("%d", expireMs)}
		}

	default:
		return fmt.Errorf("unknown data type: %d", cmd.Type)
	}

	// Execute the command
	return s.executeCommand(args)
}

// startBackgroundRDBSave starts a background goroutine that periodically checks
// if RDB save conditions are met (Redis-style: save after N seconds if M keys changed)
func (s *RedisServer) startBackgroundRDBSave() {
	checkInterval := time.Duration(s.config.RDBSavePoint.Seconds) * time.Second
	s.rdbTicker = time.NewTicker(checkInterval)

	log.Printf("RDB auto-save enabled: save after %d seconds if %d keys changed",
		s.config.RDBSavePoint.Seconds, s.config.RDBSavePoint.Changes)

	go func() {
		for {
			select {
			case <-s.rdbTicker.C:
				// Check if save conditions are met
				changes := s.changesSinceLastSave.Load()
				elapsed := time.Since(s.lastSaveTime)

				if changes >= int64(s.config.RDBSavePoint.Changes) &&
					elapsed >= time.Duration(s.config.RDBSavePoint.Seconds)*time.Second {

					log.Printf("RDB auto-save triggered: %d changes in %v", changes, elapsed)

					// Trigger BGSAVE
					if err := s.performBackgroundSave(); err != nil {
						log.Printf("RDB auto-save failed: %v", err)
					} else {
						// Reset counters after successful save
						s.saveMu.Lock()
						s.changesSinceLastSave.Store(0)
						s.lastSaveTime = time.Now()
						s.saveMu.Unlock()
					}
				}

			case <-s.rdbStopChan:
				return
			}
		}
	}()
}

// performBackgroundSave executes BGSAVE command
func (s *RedisServer) performBackgroundSave() error {
	// Execute BGSAVE through the handler
	cmd := &protocol.Command{Args: []string{"BGSAVE"}}
	response := s.handler.ExecuteCommand(cmd)

	// Check if result indicates an error
	if len(response) > 0 && response[0] == '-' {
		return fmt.Errorf("BGSAVE failed: %s", string(response))
	}

	return nil
}

// IncrementChanges increments the change counter (called after each write operation)
func (s *RedisServer) IncrementChanges() {
	s.changesSinceLastSave.Add(1)
}
