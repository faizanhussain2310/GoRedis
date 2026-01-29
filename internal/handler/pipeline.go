package handler

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"redis/internal/protocol"
)

var (
	ErrPipelineLimit  = errors.New("pipeline command limit exceeded")
	ErrCommandTimeout = errors.New("command execution timeout")
	ErrSlowClient     = errors.New("client disconnected due to slow commands")
)

// PipelineConfig holds pipeline-related configuration
type PipelineConfig struct {
	MaxCommands     int           // Maximum commands per pipeline batch
	SlowThreshold   time.Duration // Threshold for slow log
	CommandTimeout  time.Duration // Timeout for individual command execution
	ReadTimeout     time.Duration // Timeout for reading client data (idle timeout)
	PipelineTimeout time.Duration // Short timeout for waiting for in-flight pipelined commands
}

// PipelineResult holds the result of a pipelined command
type PipelineResult struct {
	Response []byte
	Duration time.Duration
	Command  string
	Args     []string
	Err      error
}

// HandlePipeline processes commands with pipelining support using Redis-style streaming.
// This approach: Read one → Execute one → Queue response → Repeat → Flush all
// Benefits: O(1) memory per command, immediate execution, matches real Redis behavior
func (h *CommandHandler) HandlePipeline(ctx context.Context, client *Client, config PipelineConfig) {
	reader := bufio.NewReaderSize(client.Conn, h.readBufferSize)
	writer := bufio.NewWriterSize(client.Conn, h.writeBufferSize)

	slowLog := NewSlowLog(128, config.SlowThreshold)
	consecutiveSlowCommands := 0
	const maxConsecutiveSlow = 10 // Disconnect after 10 consecutive slow commands

	// Get transaction state for this client
	tx := h.txManager.GetTransaction(client.ID)
	defer h.txManager.RemoveClient(client.ID) // Cleanup on disconnect

	// Cleanup pub/sub on disconnect
	defer func() {
		if client.InPubSub && client.Subscriber != nil {
			subscriberID := fmt.Sprintf("client:%d", client.ID)
			h.processor.GetStore().PubSub.RemoveSubscriber(subscriberID)
		}
	}()

	// Message pump started flag
	messagePumpStarted := false

	// Default pipeline timeout to 1ms if not set (very short - just to catch in-flight data)
	pipelineTimeout := config.PipelineTimeout
	if pipelineTimeout <= 0 {
		pipelineTimeout = 1 * time.Millisecond
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Set read deadline for the first command (blocks until client sends something)
			// In pub/sub mode, no timeout - clients wait indefinitely for messages
			if client.InPubSub {
				client.Conn.SetReadDeadline(time.Time{}) // No timeout
			} else {
				readTimeout := config.ReadTimeout
				if readTimeout <= 0 {
					readTimeout = 30 * time.Second // Default idle timeout
				}
				client.Conn.SetReadDeadline(time.Now().Add(readTimeout))
			}

			// Wait for first command (this blocks - waiting for client to initiate)
			cmd, err := protocol.ParseCommand(reader)
			if err != nil {
				if err == io.EOF {
					return
				}
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					log.Printf("Client %d: idle timeout, disconnecting", client.ID)
					return
				}
				log.Printf("Error reading command: %v", err)
				response := protocol.EncodeError(fmt.Sprintf("ERR %v", err))
				writer.Write(response)
				writer.Flush()
				continue
			}

			// Clear deadline for processing
			client.Conn.SetReadDeadline(time.Time{})

			// Check for replication commands that need raw connection access
			// (PSYNC, REPLCONF - these bypass normal command processing)
			if h.handleReplicationCommand(client.Conn, reader, writer, cmd) {
				// Replication command was handled, continue to next iteration
				// Note: PSYNC may keep connection alive for replication stream
				continue
			}

			commandsInBatch := 0

			// Process first command (with transaction support)
			result := h.executeWithTransaction(ctx, client, cmd, tx, config.CommandTimeout)

			// Start message pump if client just entered pub/sub mode
			if client.InPubSub && !messagePumpStarted {
				h.StartMessagePump(ctx, client, client.Conn)
				messagePumpStarted = true
			}

			if h.handleCommandResult(result, &consecutiveSlowCommands, maxConsecutiveSlow, slowLog, client, writer) {
				return
			}
			if _, err := writer.Write(result.Response); err != nil {
				log.Printf("Client %d: write error: %v", client.ID, err)
				return
			}
			commandsInBatch++

			// In pub/sub mode, after processing the subscription command,
			// enter a special loop that only handles pub/sub commands
			if client.InPubSub {
				// Flush the subscription confirmation
				if err := writer.Flush(); err != nil {
					log.Printf("Error flushing response: %v", err)
					return
				}

				// Enter pub/sub mode - only handle SUBSCRIBE/UNSUBSCRIBE/PING/QUIT
				// This returns when client exits pub/sub or disconnects
				if !h.HandlePubSubMode(ctx, client, reader, writer, config, tx, &consecutiveSlowCommands, maxConsecutiveSlow, slowLog) {
					// Error or disconnect - exit pipeline
					return
				}
				// Client exited pub/sub mode cleanly - continue with normal commands
				continue
			}

			// Process remaining pipelined commands
			// Use short timeout to wait for more data that might be in-flight
			for commandsInBatch < config.MaxCommands {
				// First check if there's already a complete command in buffer (non-blocking)
				if protocol.HasCompleteCommand(reader) {
					cmd, err := protocol.ParseCommand(reader)
					if err != nil {
						writer.Write(protocol.EncodeError(fmt.Sprintf("ERR %v", err)))
						break
					}

					// Check for replication commands
					if h.handleReplicationCommand(client.Conn, reader, writer, cmd) {
						continue
					}

					result := h.executeWithTransaction(ctx, client, cmd, tx, config.CommandTimeout)

					// Start message pump if client just entered pub/sub mode
					if client.InPubSub && !messagePumpStarted {
						h.StartMessagePump(ctx, client, client.Conn)
						messagePumpStarted = true
					}

					if h.handleCommandResult(result, &consecutiveSlowCommands, maxConsecutiveSlow, slowLog, client, writer) {
						return
					}
					if _, err := writer.Write(result.Response); err != nil {
						log.Printf("Client %d: write error: %v", client.ID, err)
						return
					}
					commandsInBatch++
					continue
				}

				// No complete command in buffer - wait briefly for more data
				// This catches data that's in-flight on the network
				// In pub/sub mode, no timeout (wait indefinitely)
				if !client.InPubSub {
					client.Conn.SetReadDeadline(time.Now().Add(pipelineTimeout))
				}

				// Try to read more data (will timeout quickly if nothing coming)
				cmd, err := protocol.ParseCommand(reader)

				// Clear deadline
				client.Conn.SetReadDeadline(time.Time{})

				if err != nil {
					// Timeout is expected - means no more pipelined commands
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						break // No more commands, flush what we have
					}
					if err == io.EOF {
						// Client disconnected - flush remaining responses and exit
						writer.Flush()
						return
					}
					// Actual error
					writer.Write(protocol.EncodeError(fmt.Sprintf("ERR %v", err)))
					break
				}

				// Got another command!
				// Check for replication commands first
				if h.handleReplicationCommand(client.Conn, reader, writer, cmd) {
					continue
				}

				result := h.executeWithTransaction(ctx, client, cmd, tx, config.CommandTimeout)

				// Start message pump if client just entered pub/sub mode
				if client.InPubSub && !messagePumpStarted {
					h.StartMessagePump(ctx, client, client.Conn)
					messagePumpStarted = true
				}

				if h.handleCommandResult(result, &consecutiveSlowCommands, maxConsecutiveSlow, slowLog, client, writer) {
					return
				}
				if _, err := writer.Write(result.Response); err != nil {
					log.Printf("Client %d: write error: %v", client.ID, err)
					return
				}
				commandsInBatch++
			}

			// Flush all queued responses at once
			if err := writer.Flush(); err != nil {
				log.Printf("Error flushing response: %v", err)
				return
			}
		}
	}
}

// handleCommandResult processes a command result, checking for timeouts and slow commands.
// Returns true if the client should be disconnected.
func (h *CommandHandler) handleCommandResult(
	result PipelineResult,
	consecutiveSlowCommands *int,
	maxConsecutiveSlow int,
	slowLog *SlowLog,
	client *Client,
	writer *bufio.Writer,
) bool {
	// Check for command timeout
	if result.Err != nil {
		if errors.Is(result.Err, ErrCommandTimeout) {
			log.Printf("Client %d disconnected: command timeout", client.ID)
			response := protocol.EncodeError("ERR command execution timeout, disconnecting")
			writer.Write(response)
			writer.Flush()
			return true
		}
	}

	// Track consecutive slow commands
	if slowLog.LogIfSlow(client.ID, result.Command, result.Args, result.Duration) {
		*consecutiveSlowCommands++
		if *consecutiveSlowCommands >= maxConsecutiveSlow {
			log.Printf("Client %d disconnected: too many slow commands", client.ID)
			response := protocol.EncodeError("ERR too many slow commands, disconnecting")
			writer.Write(response)
			writer.Flush()
			return true
		}
	} else {
		*consecutiveSlowCommands = 0 // Reset counter on fast command
	}

	return false
}

// HandlePubSubMode handles client in pub/sub mode
// Only allows SUBSCRIBE, UNSUBSCRIBE, PSUBSCRIBE, PUNSUBSCRIBE, PING, QUIT
// Returns true if client exited pub/sub cleanly, false if error/disconnect
func (h *CommandHandler) HandlePubSubMode(
	ctx context.Context,
	client *Client,
	reader *bufio.Reader,
	writer *bufio.Writer,
	config PipelineConfig,
	tx *Transaction,
	consecutiveSlowCommands *int,
	maxConsecutiveSlow int,
	slowLog *SlowLog,
) bool {
	for {
		select {
		case <-ctx.Done():
			return false // Context canceled
		default:
			// No timeout in pub/sub mode - wait indefinitely for commands
			client.Conn.SetReadDeadline(time.Time{})

			// Wait for command from client
			cmd, err := protocol.ParseCommand(reader)
			if err != nil {
				if err == io.EOF {
					return false // Client disconnected
				}
				log.Printf("Error reading command in pub/sub mode: %v", err)
				return false // Error
			}

			// Execute the command
			result := h.executeWithTransaction(ctx, client, cmd, tx, config.CommandTimeout)

			// Check if client exited pub/sub mode
			if !client.InPubSub {
				// Client unsubscribed from all channels, exit pub/sub mode
				writer.Write(result.Response)
				writer.Flush()
				return true // Exited cleanly, continue in normal mode
			}

			// Write response
			if h.handleCommandResult(result, consecutiveSlowCommands, maxConsecutiveSlow, slowLog, client, writer) {
				return false // Client should be disconnected
			}
			if _, err := writer.Write(result.Response); err != nil {
				log.Printf("Client %d: write error: %v", client.ID, err)
				return false // Write error
			}

			// Flush immediately in pub/sub mode
			if err := writer.Flush(); err != nil {
				log.Printf("Error flushing response: %v", err)
				return false // Flush error
			}
		}
	}
}
