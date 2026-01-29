package handler

import (
	"context"
	"log"
	"net"
)

// StartMessagePump starts the message pump for a pub/sub subscriber
// This goroutine reads from the subscriber's message channel and sends directly to the client connection
// Writes directly to connection to bypass buffered writer (pub/sub messages are sent immediately)
func (h *CommandHandler) StartMessagePump(ctx context.Context, client *Client, conn net.Conn) {
	if client.Subscriber == nil {
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-client.Subscriber.Channels:
				if !ok {
					// Channel closed, exit
					return
				}

				// Encode the message
				encoded := encodePubSubMessage(msg)

				// Write directly to connection (bypasses buffered writer)
				// This is safe because only the message pump writes messages
				// The main pipeline only writes command responses
				if _, err := conn.Write(encoded); err != nil {
					log.Printf("Error writing pub/sub message to client %d: %v", client.ID, err)
					return
				}
			}
		}
	}()
}
