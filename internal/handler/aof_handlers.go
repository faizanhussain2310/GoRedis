package handler

import (
	"fmt"
	"log"
	"time"

	"redis/internal/protocol"
	"redis/internal/rdb"
	"redis/internal/storage"
)

// handleBGRewriteAOF triggers AOF rewrite in the background
func (h *CommandHandler) handleBGRewriteAOF(cmd *protocol.Command) []byte {
	if h.aofWriter == nil {
		return protocol.EncodeError("ERR AOF is not enabled")
	}

	// Start rewrite in background
	go func() {
		log.Println("Starting AOF rewrite...")

		// Get snapshot of current database state (shallow copy with COW)
		snapshotFunc := func() [][]string {
			// Get raw data snapshot from processor (fast - just shallow copy)
			allData := h.processor.GetSnapshot()

			// Filter and convert to commands in background (doesn't block processor!)
			commands := make([][]string, 0)
			now := time.Now()
			filtered := 0

			for key, value := range allData {
				// Skip expired keys
				if value.ExpiresAt != nil && now.After(*value.ExpiresAt) {
					filtered++
					continue
				}

				switch value.Type {
				case 0: // StringType
					if str, ok := value.Data.(string); ok {
						commands = append(commands, []string{"SET", key, str})
						if value.ExpiresAt != nil {
							ttl := int(time.Until(*value.ExpiresAt).Seconds())
							if ttl > 0 {
								commands = append(commands, []string{"EXPIRE", key, fmt.Sprintf("%d", ttl)})
							}
						}
					}

				case 1: // ListType
					if list, ok := value.Data.([]string); ok && len(list) > 0 {
						listCmd := []string{"RPUSH", key}
						listCmd = append(listCmd, list...)
						commands = append(commands, listCmd)
						if value.ExpiresAt != nil {
							ttl := int(time.Until(*value.ExpiresAt).Seconds())
							if ttl > 0 {
								commands = append(commands, []string{"EXPIRE", key, fmt.Sprintf("%d", ttl)})
							}
						}
					}

				case 2: // SetType
					// Access the Set struct and its Members map
					if setStruct, ok := value.Data.(*storage.Set); ok && setStruct != nil && len(setStruct.Members) > 0 {
						setCmd := []string{"SADD", key}
						for member := range setStruct.Members {
							setCmd = append(setCmd, member)
						}
						commands = append(commands, setCmd)
						if value.ExpiresAt != nil {
							ttl := int(time.Until(*value.ExpiresAt).Seconds())
							if ttl > 0 {
								commands = append(commands, []string{"EXPIRE", key, fmt.Sprintf("%d", ttl)})
							}
						}
					}

				case 3: // HashType
					if hash, ok := value.Data.(map[string]string); ok && len(hash) > 0 {
						hashCmd := []string{"HSET", key}
						for field, val := range hash {
							hashCmd = append(hashCmd, field, val)
						}
						commands = append(commands, hashCmd)
						if value.ExpiresAt != nil {
							ttl := int(time.Until(*value.ExpiresAt).Seconds())
							if ttl > 0 {
								commands = append(commands, []string{"EXPIRE", key, fmt.Sprintf("%d", ttl)})
							}
						}
					}

				case 4: // ZSetType
					// Access the ZSet struct and get all members with scores
					if zsetStruct, ok := value.Data.(*storage.ZSet); ok && zsetStruct != nil && zsetStruct.Len() > 0 {
						members := zsetStruct.GetAll()
						if len(members) > 0 {
							zsetCmd := []string{"ZADD", key}
							for _, member := range members {
								zsetCmd = append(zsetCmd, fmt.Sprintf("%f", member.Score), member.Member)
							}
							commands = append(commands, zsetCmd)
							if value.ExpiresAt != nil {
								ttl := int(time.Until(*value.ExpiresAt).Seconds())
								if ttl > 0 {
									commands = append(commands, []string{"EXPIRE", key, fmt.Sprintf("%d", ttl)})
								}
							}
						}
					}

				case 5: // BloomFilterType
					// Bloom filters require special handling - they can't be reconstructed from members
					// Skip in AOF as they're probabilistic structures that should be rebuilt
					// Alternative: could implement BF.RESERVE + BF.DUMP/BF.RESTORE commands
					log.Printf("Skipping BloomFilter key '%s' in AOF (not supported in AOF rewrite)", key)

				case 6: // HyperLogLogType
					// HyperLogLog also requires special handling - it's a cardinality estimator
					// Skip in AOF as it can't be reconstructed from individual elements
					// Alternative: could implement PFMERGE or raw register export
					log.Printf("Skipping HyperLogLog key '%s' in AOF (not supported in AOF rewrite)", key)
				}
			}

			if filtered > 0 {
				log.Printf("Filtered %d expired keys from AOF rewrite snapshot", filtered)
			}

			return commands
		}

		// Perform rewrite
		if err := h.aofWriter.Rewrite(snapshotFunc); err != nil {
			log.Printf("AOF rewrite failed: %v", err)
		} else {
			log.Println("AOF rewrite completed successfully")
		}

		// Release snapshot reference (COW optimization)
		h.processor.ReleaseSnapshot()
	}()

	return protocol.EncodeSimpleString("Background append only file rewriting started")
}

// handleBGSave triggers RDB snapshot in the background
func (h *CommandHandler) handleBGSave(cmd *protocol.Command) []byte {
	// Start snapshot in background
	go func() {
		log.Println("Starting RDB snapshot (BGSAVE)...")

		// Create RDB writer
		rdbWriter := rdb.NewWriter("dump.rdb")

		// Get actual data snapshot through processor (shallow copy with COW!)
		dataSnapshot := h.processor.GetDataSnapshot()

		// Filter expired keys in background (doesn't block processor!)
		now := time.Now()
		filtered := 0
		for key, value := range dataSnapshot {
			if value.ExpiresAt != nil && now.After(*value.ExpiresAt) {
				delete(dataSnapshot, key)
				filtered++
			}
		}

		if filtered > 0 {
			log.Printf("Filtered %d expired keys from RDB snapshot", filtered)
		}

		// Perform save
		if err := rdbWriter.Save(dataSnapshot); err != nil {
			log.Printf("RDB snapshot failed: %v", err)
		} else {
			log.Println("RDB snapshot completed successfully")
		}

		// Release snapshot reference (COW optimization)
		h.processor.ReleaseSnapshot()
	}()

	return protocol.EncodeSimpleString("Background saving started")
}
