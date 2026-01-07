package processor

import (
	"redis/internal/storage"
)

// ==================== PUB/SUB RESULT TYPES ====================

// PublishResult represents the result of a publish operation
type PublishResult struct {
	Count int
	Err   error
}

// NumSubResult represents the result of PUBSUB NUMSUB
type NumSubResult struct {
	Counts map[string]int
	Err    error
}

// ChannelsResult represents the result of PUBSUB CHANNELS
type ChannelsResult struct {
	Channels []string
	Err      error
}

// SubscribeResult represents the result of SUBSCRIBE/PSUBSCRIBE
type SubscribeResult struct {
	Subscriber *storage.Subscriber
	Messages   []*storage.Message // Confirmation messages for each subscription
	Err        error
}

// UnsubscribeResult represents the result of UNSUBSCRIBE/PUNSUBSCRIBE
type UnsubscribeResult struct {
	Messages   []*storage.Message // Confirmation messages for each unsubscription
	TotalCount int                // Total remaining subscriptions
	Err        error
}

// ==================== PUB/SUB COMMAND EXECUTORS ====================

// executePubSubCommand routes pub/sub commands to appropriate executors
func (p *Processor) executePubSubCommand(cmd *Command) {
	var result interface{}

	switch cmd.Type {
	case CmdPublish:
		result = p.executePublish(cmd)
	case CmdPubSubChannels:
		result = p.executePubSubChannels(cmd)
	case CmdPubSubNumSub:
		result = p.executePubSubNumSub(cmd)
	case CmdPubSubNumPat:
		result = p.executePubSubNumPat(cmd)
	case CmdSubscribe:
		result = p.executeSubscribe(cmd)
	case CmdUnsubscribe:
		result = p.executeUnsubscribe(cmd)
	case CmdPSubscribe:
		result = p.executePSubscribe(cmd)
	case CmdPUnsubscribe:
		result = p.executePUnsubscribe(cmd)
	default:
		result = IntResult{Err: storage.ErrInvalidOperation}
	}

	cmd.Response <- result
}

// executePublish handles PUBLISH command
func (p *Processor) executePublish(cmd *Command) PublishResult {
	if len(cmd.Args) != 2 {
		return PublishResult{Err: storage.ErrWrongNumArgs}
	}

	channel, ok := cmd.Args[0].(string)
	if !ok {
		return PublishResult{Err: storage.ErrInvalidOperation}
	}

	message, ok := cmd.Args[1].(string)
	if !ok {
		return PublishResult{Err: storage.ErrInvalidOperation}
	}

	count := p.store.PubSub.Publish(channel, message)

	return PublishResult{Count: count}
}

// executePubSubChannels handles PUBSUB CHANNELS command
func (p *Processor) executePubSubChannels(cmd *Command) ChannelsResult {
	pattern := ""
	if len(cmd.Args) > 0 {
		if p, ok := cmd.Args[0].(string); ok {
			pattern = p
		}
	}

	channels := p.store.PubSub.Channels(pattern)

	return ChannelsResult{Channels: channels}
}

// executePubSubNumSub handles PUBSUB NUMSUB command
func (p *Processor) executePubSubNumSub(cmd *Command) NumSubResult {
	channels := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		if ch, ok := arg.(string); ok {
			channels = append(channels, ch)
		}
	}

	counts := p.store.PubSub.NumSub(channels...)

	return NumSubResult{Counts: counts}
}

// executePubSubNumPat handles PUBSUB NUMPAT command
func (p *Processor) executePubSubNumPat(cmd *Command) IntResult {
	count := p.store.PubSub.NumPat()
	return IntResult{Result: count}
}

// executeSubscribe handles SUBSCRIBE command
func (p *Processor) executeSubscribe(cmd *Command) SubscribeResult {
	if len(cmd.Args) == 0 {
		return SubscribeResult{Err: storage.ErrWrongNumArgs}
	}

	// Extract channel names
	channels := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		if ch, ok := arg.(string); ok {
			channels = append(channels, ch)
		} else {
			return SubscribeResult{Err: storage.ErrInvalidOperation}
		}
	}

	// Create subscriber if doesn't exist
	subscriberID := cmd.GetSubscriberID()
	subscriber := &storage.Subscriber{
		ID:       subscriberID,
		Channels: make(chan *storage.Message, 100), // Buffered channel
	}

	// Subscribe to channels (will reuse existing subscriber if exists)
	subscribed := p.store.PubSub.Subscribe(subscriberID, subscriber, channels...)

	// Get the actual subscriber (might be reused)
	actualSubscriber := p.store.PubSub.GetSubscriber(subscriberID)

	// Create confirmation messages
	messages := make([]*storage.Message, len(subscribed))
	for i, channel := range subscribed {
		count := p.store.PubSub.GetSubscriberCount(subscriberID)
		messages[i] = &storage.Message{
			Type:    "subscribe",
			Channel: channel,
			Count:   count,
		}
	}

	return SubscribeResult{
		Subscriber: actualSubscriber,
		Messages:   messages,
	}
}

// executeUnsubscribe handles UNSUBSCRIBE command
func (p *Processor) executeUnsubscribe(cmd *Command) UnsubscribeResult {
	subscriberID := cmd.GetSubscriberID()

	// Extract channel names (empty means unsubscribe from all)
	channels := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		if ch, ok := arg.(string); ok {
			channels = append(channels, ch)
		}
	}

	// Unsubscribe from channels
	unsubscribed := p.store.PubSub.Unsubscribe(subscriberID, channels...)

	// Create confirmation messages
	messages := make([]*storage.Message, len(unsubscribed))
	totalCount := p.store.PubSub.GetSubscriberCount(subscriberID)

	for i, channel := range unsubscribed {
		// Count decreases with each unsubscription
		count := totalCount - i
		messages[i] = &storage.Message{
			Type:    "unsubscribe",
			Channel: channel,
			Count:   count,
		}
	}

	return UnsubscribeResult{
		Messages:   messages,
		TotalCount: totalCount,
	}
}

// executePSubscribe handles PSUBSCRIBE command
func (p *Processor) executePSubscribe(cmd *Command) SubscribeResult {
	if len(cmd.Args) == 0 {
		return SubscribeResult{Err: storage.ErrWrongNumArgs}
	}

	// Extract pattern names
	patterns := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		if p, ok := arg.(string); ok {
			patterns = append(patterns, p)
		} else {
			return SubscribeResult{Err: storage.ErrInvalidOperation}
		}
	}

	// Create subscriber if doesn't exist
	subscriberID := cmd.GetSubscriberID()
	subscriber := &storage.Subscriber{
		ID:       subscriberID,
		Channels: make(chan *storage.Message, 100), // Buffered channel
	}

	// Subscribe to patterns (will reuse existing subscriber if exists)
	subscribed := p.store.PubSub.PSubscribe(subscriberID, subscriber, patterns...)

	// Get the actual subscriber (might be reused)
	actualSubscriber := p.store.PubSub.GetSubscriber(subscriberID)

	// Create confirmation messages
	messages := make([]*storage.Message, len(subscribed))
	for i, pattern := range subscribed {
		count := p.store.PubSub.GetSubscriberCount(subscriberID)
		messages[i] = &storage.Message{
			Type:    "psubscribe",
			Pattern: pattern,
			Count:   count,
		}
	}

	return SubscribeResult{
		Subscriber: actualSubscriber,
		Messages:   messages,
	}
}

// executePUnsubscribe handles PUNSUBSCRIBE command
func (p *Processor) executePUnsubscribe(cmd *Command) UnsubscribeResult {
	subscriberID := cmd.GetSubscriberID()

	// Extract pattern names (empty means unsubscribe from all)
	patterns := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		if p, ok := arg.(string); ok {
			patterns = append(patterns, p)
		}
	}

	// Unsubscribe from patterns
	unsubscribed := p.store.PubSub.PUnsubscribe(subscriberID, patterns...)

	// Create confirmation messages
	messages := make([]*storage.Message, len(unsubscribed))
	totalCount := p.store.PubSub.GetSubscriberCount(subscriberID)

	for i, pattern := range unsubscribed {
		// Count decreases with each unsubscription
		count := totalCount - i
		messages[i] = &storage.Message{
			Type:    "punsubscribe",
			Pattern: pattern,
			Count:   count,
		}
	}

	return UnsubscribeResult{
		Messages:   messages,
		TotalCount: totalCount,
	}
}
