package handler

import (
	"fmt"
	"strings"

	"redis/internal/processor"
	"redis/internal/protocol"
	"redis/internal/storage"
)

// ==================== PUB/SUB HANDLERS ====================

// handlePublish handles PUBLISH command
// PUBLISH channel message
func (h *CommandHandler) handlePublish(cmd *protocol.Command) []byte {
	if len(cmd.Args) != 3 {
		return protocol.EncodeError("ERR wrong number of arguments for 'publish' command")
	}

	channel := cmd.Args[1]
	message := cmd.Args[2]

	args := make([]interface{}, 2)
	args[0] = channel
	args[1] = message

	procCmd := &processor.Command{
		Type:     processor.CmdPublish,
		Args:     args,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	switch r := result.(type) {
	case processor.PublishResult:
		if r.Err != nil {
			return protocol.EncodeError(r.Err.Error())
		}
		return protocol.EncodeInteger(r.Count)
	default:
		return protocol.EncodeError("ERR unexpected result type")
	}
}

// handlePubSub handles PUBSUB command with subcommands
// PUBSUB CHANNELS [pattern]
// PUBSUB NUMSUB [channel ...]
// PUBSUB NUMPAT
func (h *CommandHandler) handlePubSub(cmd *protocol.Command) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'pubsub' command")
	}

	subcommand := strings.ToUpper(cmd.Args[1])

	switch subcommand {
	case "CHANNELS":
		return h.handlePubSubChannels(cmd.Args[2:])
	case "NUMSUB":
		return h.handlePubSubNumSub(cmd.Args[2:])
	case "NUMPAT":
		return h.handlePubSubNumPat(cmd.Args[2:])
	default:
		return protocol.EncodeError(fmt.Sprintf("ERR unknown PUBSUB subcommand '%s'", subcommand))
	}
}

// handlePubSubChannels handles PUBSUB CHANNELS [pattern]
func (h *CommandHandler) handlePubSubChannels(args []string) []byte {
	pattern := ""
	if len(args) > 0 {
		pattern = args[0]
	}

	procArgs := make([]interface{}, 0, 1)
	if pattern != "" {
		procArgs = append(procArgs, pattern)
	}

	procCmd := &processor.Command{
		Type:     processor.CmdPubSubChannels,
		Args:     procArgs,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	switch r := result.(type) {
	case processor.ChannelsResult:
		if r.Err != nil {
			return protocol.EncodeError(r.Err.Error())
		}
		return protocol.EncodeArray(r.Channels)
	default:
		return protocol.EncodeError("ERR unexpected result type")
	}
}

// handlePubSubNumSub handles PUBSUB NUMSUB [channel ...]
func (h *CommandHandler) handlePubSubNumSub(args []string) []byte {
	procArgs := make([]interface{}, len(args))
	for i, arg := range args {
		procArgs[i] = arg
	}

	procCmd := &processor.Command{
		Type:     processor.CmdPubSubNumSub,
		Args:     procArgs,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	switch r := result.(type) {
	case processor.NumSubResult:
		if r.Err != nil {
			return protocol.EncodeError(r.Err.Error())
		}
		// Return flat array: [channel1, count1, channel2, count2, ...]
		items := make([]interface{}, 0, len(r.Counts)*2)
		for _, channel := range args {
			items = append(items, channel)
			items = append(items, r.Counts[channel])
		}
		return protocol.EncodeInterfaceArray(items)
	default:
		return protocol.EncodeError("ERR unexpected result type")
	}
}

// handlePubSubNumPat handles PUBSUB NUMPAT
func (h *CommandHandler) handlePubSubNumPat(args []string) []byte {
	if len(args) > 0 {
		return protocol.EncodeError("ERR wrong number of arguments for 'pubsub numpat' command")
	}

	procCmd := &processor.Command{
		Type:     processor.CmdPubSubNumPat,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	switch r := result.(type) {
	case processor.IntResult:
		if r.Err != nil {
			return protocol.EncodeError(r.Err.Error())
		}
		return protocol.EncodeInteger(r.Result)
	default:
		return protocol.EncodeError("ERR unexpected result type")
	}
}

// ==================== HELPER FUNCTIONS ====================

// encodePubSubMessage encodes a single pub/sub message
func encodePubSubMessage(msg *storage.Message) []byte {
	switch msg.Type {
	case "subscribe", "unsubscribe":
		// Array: [type, channel, count]
		return protocol.EncodeInterfaceArray([]interface{}{
			msg.Type,
			msg.Channel,
			msg.Count,
		})
	case "psubscribe", "punsubscribe":
		// Array: [type, pattern, count]
		return protocol.EncodeInterfaceArray([]interface{}{
			msg.Type,
			msg.Pattern,
			msg.Count,
		})
	case "message":
		// Array: [type, channel, payload]
		return protocol.EncodeInterfaceArray([]interface{}{
			msg.Type,
			msg.Channel,
			msg.Payload,
		})
	case "pmessage":
		// Array: [type, pattern, channel, payload]
		return protocol.EncodeInterfaceArray([]interface{}{
			msg.Type,
			msg.Pattern,
			msg.Channel,
			msg.Payload,
		})
	default:
		return protocol.EncodeError("ERR unknown message type")
	}
}

// ==================== SUBSCRIPTION COMMANDS (Pub/Sub Mode) ====================

// handleSubscribe handles SUBSCRIBE command
// SUBSCRIBE channel [channel ...]
// This command enters pub/sub mode
func (h *CommandHandler) handleSubscribe(cmd *protocol.Command, client *Client) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'subscribe' command")
	}

	channels := cmd.Args[1:]

	// Convert channels to []interface{}
	procArgs := make([]interface{}, len(channels))
	for i, ch := range channels {
		procArgs[i] = ch
	}

	procCmd := &processor.Command{
		Type:     processor.CmdSubscribe,
		Args:     procArgs,
		ClientID: client.ID,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	switch r := result.(type) {
	case processor.SubscribeResult:
		if r.Err != nil {
			return protocol.EncodeError(r.Err.Error())
		}

		// Store subscriber in client
		client.Subscriber = r.Subscriber
		client.InPubSub = true

		// Encode all subscription confirmations
		responses := make([]byte, 0)
		for _, msg := range r.Messages {
			responses = append(responses, encodePubSubMessage(msg)...)
		}
		return responses
	default:
		return protocol.EncodeError("ERR unexpected result type")
	}
}

// handleUnsubscribe handles UNSUBSCRIBE command
// UNSUBSCRIBE [channel [channel ...]]
// If no channels specified, unsubscribes from all channels
func (h *CommandHandler) handleUnsubscribe(cmd *protocol.Command, client *Client) []byte {
	channels := cmd.Args[1:]

	// Convert channels to []interface{}
	procArgs := make([]interface{}, len(channels))
	for i, ch := range channels {
		procArgs[i] = ch
	}

	procCmd := &processor.Command{
		Type:     processor.CmdUnsubscribe,
		Args:     procArgs,
		ClientID: client.ID,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	switch r := result.(type) {
	case processor.UnsubscribeResult:
		if r.Err != nil {
			return protocol.EncodeError(r.Err.Error())
		}

		// Exit pub/sub mode if no subscriptions left
		if r.TotalCount == 0 {
			client.InPubSub = false
			client.Subscriber = nil
		}

		// Encode all unsubscription confirmations
		responses := make([]byte, 0)
		for _, msg := range r.Messages {
			responses = append(responses, encodePubSubMessage(msg)...)
		}
		return responses
	default:
		return protocol.EncodeError("ERR unexpected result type")
	}
}

// handlePSubscribe handles PSUBSCRIBE command
// PSUBSCRIBE pattern [pattern ...]
// This command enters pub/sub mode
func (h *CommandHandler) handlePSubscribe(cmd *protocol.Command, client *Client) []byte {
	if len(cmd.Args) < 2 {
		return protocol.EncodeError("ERR wrong number of arguments for 'psubscribe' command")
	}

	patterns := cmd.Args[1:]

	// Convert patterns to []interface{}
	procArgs := make([]interface{}, len(patterns))
	for i, p := range patterns {
		procArgs[i] = p
	}

	procCmd := &processor.Command{
		Type:     processor.CmdPSubscribe,
		Args:     procArgs,
		ClientID: client.ID,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	switch r := result.(type) {
	case processor.SubscribeResult:
		if r.Err != nil {
			return protocol.EncodeError(r.Err.Error())
		}

		// Store subscriber in client
		client.Subscriber = r.Subscriber
		client.InPubSub = true

		// Encode all subscription confirmations
		responses := make([]byte, 0)
		for _, msg := range r.Messages {
			responses = append(responses, encodePubSubMessage(msg)...)
		}
		return responses
	default:
		return protocol.EncodeError("ERR unexpected result type")
	}
}

// handlePUnsubscribe handles PUNSUBSCRIBE command
// PUNSUBSCRIBE [pattern [pattern ...]]
// If no patterns specified, unsubscribes from all patterns
func (h *CommandHandler) handlePUnsubscribe(cmd *protocol.Command, client *Client) []byte {
	patterns := cmd.Args[1:]

	// Convert patterns to []interface{}
	procArgs := make([]interface{}, len(patterns))
	for i, p := range patterns {
		procArgs[i] = p
	}

	procCmd := &processor.Command{
		Type:     processor.CmdPUnsubscribe,
		Args:     procArgs,
		ClientID: client.ID,
		Response: make(chan interface{}, 1),
	}

	h.processor.Submit(procCmd)
	result := <-procCmd.Response

	switch r := result.(type) {
	case processor.UnsubscribeResult:
		if r.Err != nil {
			return protocol.EncodeError(r.Err.Error())
		}

		// Exit pub/sub mode if no subscriptions left
		if r.TotalCount == 0 {
			client.InPubSub = false
			client.Subscriber = nil
		}

		// Encode all unsubscription confirmations
		responses := make([]byte, 0)
		for _, msg := range r.Messages {
			responses = append(responses, encodePubSubMessage(msg)...)
		}
		return responses
	default:
		return protocol.EncodeError("ERR unexpected result type")
	}
}
