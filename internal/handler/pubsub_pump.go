package handler

import (
	"context"
	"fmt"
	"log"
	"net"
)

// StartMessagePump starts the message pump for a pub/sub subscriber
// This goroutine reads from the subscriber's message channel and sends directly to the client connection
// Writes directly to avoid synchronization issues with the buffered writer
func (h *CommandHandler) StartMessagePump(ctx context.Context, client *Client, conn net.Conn) {
	if client.Subscriber == nil {
		return
	}

	go func() {
		defer func() {
			// Cleanup on exit
			if client.Subscriber != nil {
				// Remove subscriber from all channels/patterns
				subscriberID := fmt.Sprintf("client:%d", client.ID)
				h.processor.GetStore().PubSub.RemoveSubscriber(subscriberID)

				// Close the message channel
				close(client.Subscriber.Channels)

				// Clear client state
				client.Subscriber = nil
				client.InPubSub = false
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-client.Subscriber.Channels:
				if !ok {
					// Channel closed, exit
					return
				}

				// Encode and send the message directly to connection
				encoded := encodePubSubMessage(msg)

				// Write directly to connection (thread-safe)
				if _, err := conn.Write(encoded); err != nil {
					log.Printf("Error writing pub/sub message to client %d: %v", client.ID, err)
					return
				}
			}
		}
	}()
}
