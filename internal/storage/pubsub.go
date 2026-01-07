package storage

import (
	"regexp"
	"strings"
	"sync"
)

// ==================== PUB/SUB DATA STRUCTURES ====================

// Subscriber represents a client subscribed to channels/patterns
type Subscriber struct {
	ID       string
	Channels chan *Message // Channel to send messages to subscriber
}

// Message represents a pub/sub message
type Message struct {
	Type    string // "message", "pmessage", "subscribe", "unsubscribe", "psubscribe", "punsubscribe"
	Channel string // Channel name
	Pattern string // Pattern (for pmessage)
	Payload string // Message payload
	Count   int    // Number of active subscriptions (for subscribe/unsubscribe responses)
}

// PatternTrieNode represents a node in the pattern trie
type PatternTrieNode struct {
	children map[byte]*PatternTrieNode // Child nodes indexed by character
	patterns []string                  // Patterns that end at or pass through this node
}

// PatternTrie is a prefix tree for efficient pattern lookup
type PatternTrie struct {
	root *PatternTrieNode
}

// NewPatternTrie creates a new pattern trie
func NewPatternTrie() *PatternTrie {
	return &PatternTrie{
		root: &PatternTrieNode{
			children: make(map[byte]*PatternTrieNode),
			patterns: make([]string, 0),
		},
	}
}

// Insert adds a pattern to the trie
// Only inserts up to the first wildcard (* or ?)
func (pt *PatternTrie) Insert(pattern string) {
	node := pt.root

	// Extract prefix before first wildcard
	prefixLen := 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '*' || pattern[i] == '?' {
			break
		}
		prefixLen++
	}

	prefix := pattern[:prefixLen]

	// Traverse/create nodes for the prefix
	for i := 0; i < len(prefix); i++ {
		char := prefix[i]
		if node.children[char] == nil {
			node.children[char] = &PatternTrieNode{
				children: make(map[byte]*PatternTrieNode),
				patterns: make([]string, 0),
			}
		}
		node = node.children[char]
	}

	// Store the full pattern at this node
	node.patterns = append(node.patterns, pattern)
}

// Remove removes a pattern from the trie
func (pt *PatternTrie) Remove(pattern string) {
	// Extract prefix before first wildcard
	prefixLen := 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '*' || pattern[i] == '?' {
			break
		}
		prefixLen++
	}

	prefix := pattern[:prefixLen]

	// Navigate to the node
	node := pt.root
	for i := 0; i < len(prefix); i++ {
		char := prefix[i]
		if node.children[char] == nil {
			return // Pattern not found
		}
		node = node.children[char]
	}

	// Remove pattern from the node's list
	for i, p := range node.patterns {
		if p == pattern {
			node.patterns = append(node.patterns[:i], node.patterns[i+1:]...)
			break
		}
	}
}

// GetMatchingPatterns returns all patterns that could potentially match the channel
// This filters patterns based on their prefix, reducing the search space
func (pt *PatternTrie) GetMatchingPatterns(channel string) []string {
	result := make([]string, 0)

	// Collect patterns from root (patterns starting with wildcards)
	result = append(result, pt.root.patterns...)

	// Traverse the trie following the channel name
	node := pt.root
	for i := 0; i < len(channel); i++ {
		char := channel[i]
		if node.children[char] == nil {
			break // No more matching prefixes
		}
		node = node.children[char]

		// Collect patterns at this node
		result = append(result, node.patterns...)
	}

	return result
}

// PubSub manages publish/subscribe functionality
type PubSub struct {
	// Map of channel name -> list of subscribers
	channels map[string]map[string]*Subscriber

	// Map of pattern -> list of subscribers
	patterns map[string]map[string]*Subscriber

	// Map of subscriber ID -> channels they're subscribed to
	subscriberChannels map[string]map[string]bool

	// Map of subscriber ID -> patterns they're subscribed to
	subscriberPatterns map[string]map[string]bool

	// Map of subscriber ID -> Subscriber object (to reuse across subscriptions)
	subscribers map[string]*Subscriber

	// OPTIMIZATION: Trie for efficient pattern prefix lookup
	patternTrie *PatternTrie

	// OPTIMIZATION: Pre-compiled regex cache for patterns
	compiledPatterns map[string]*regexp.Regexp

	mu sync.RWMutex
}

// NewPubSub creates a new PubSub instance
func NewPubSub() *PubSub {
	return &PubSub{
		channels:           make(map[string]map[string]*Subscriber),
		patterns:           make(map[string]map[string]*Subscriber),
		subscriberChannels: make(map[string]map[string]bool),
		subscriberPatterns: make(map[string]map[string]bool),
		subscribers:        make(map[string]*Subscriber),
		patternTrie:        NewPatternTrie(),
		compiledPatterns:   make(map[string]*regexp.Regexp),
	}
}

// ==================== SUBSCRIPTION OPERATIONS ====================

// Subscribe subscribes a client to one or more channels
func (ps *PubSub) Subscribe(subscriberID string, sub *Subscriber, channels ...string) []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Reuse existing subscriber if it exists, otherwise store the new one
	if existing, ok := ps.subscribers[subscriberID]; ok {
		sub = existing
	} else {
		ps.subscribers[subscriberID] = sub
	}

	// Initialize subscriber's channel map if needed
	if ps.subscriberChannels[subscriberID] == nil {
		ps.subscriberChannels[subscriberID] = make(map[string]bool)
	}

	subscribed := make([]string, 0, len(channels))

	for _, channel := range channels {
		// Initialize channel's subscriber map if needed
		if ps.channels[channel] == nil {
			ps.channels[channel] = make(map[string]*Subscriber)
		}

		// Add subscriber to channel
		ps.channels[channel][subscriberID] = sub
		ps.subscriberChannels[subscriberID][channel] = true
		subscribed = append(subscribed, channel)
	}

	return subscribed
}

// Unsubscribe unsubscribes a client from one or more channels
// If no channels specified, unsubscribes from all channels
func (ps *PubSub) Unsubscribe(subscriberID string, channels ...string) []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	unsubscribed := make([]string, 0)

	// If no channels specified, unsubscribe from all
	if len(channels) == 0 {
		for channel := range ps.subscriberChannels[subscriberID] {
			channels = append(channels, channel)
		}
	}

	for _, channel := range channels {
		// Remove subscriber from channel
		if subs, exists := ps.channels[channel]; exists {
			delete(subs, subscriberID)

			// Clean up empty channel map
			if len(subs) == 0 {
				delete(ps.channels, channel)
			}
		}

		// Remove channel from subscriber's list
		if ps.subscriberChannels[subscriberID] != nil {
			delete(ps.subscriberChannels[subscriberID], channel)
		}

		unsubscribed = append(unsubscribed, channel)
	}

	return unsubscribed
}

// PSubscribe subscribes a client to one or more patterns
func (ps *PubSub) PSubscribe(subscriberID string, sub *Subscriber, patterns ...string) []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Reuse existing subscriber if it exists, otherwise store the new one
	if existing, ok := ps.subscribers[subscriberID]; ok {
		sub = existing
	} else {
		ps.subscribers[subscriberID] = sub
	}

	// Initialize subscriber's pattern map if needed
	if ps.subscriberPatterns[subscriberID] == nil {
		ps.subscriberPatterns[subscriberID] = make(map[string]bool)
	}

	subscribed := make([]string, 0, len(patterns))

	for _, pattern := range patterns {
		// Initialize pattern's subscriber map if needed
		if ps.patterns[pattern] == nil {
			ps.patterns[pattern] = make(map[string]*Subscriber)

			// OPTIMIZATION: Add pattern to trie for efficient prefix lookup
			ps.patternTrie.Insert(pattern)

			// OPTIMIZATION: Pre-compile regex for this pattern
			ps.compiledPatterns[pattern] = compilePattern(pattern)
		}

		// Add subscriber to pattern
		ps.patterns[pattern][subscriberID] = sub
		ps.subscriberPatterns[subscriberID][pattern] = true
		subscribed = append(subscribed, pattern)
	}

	return subscribed
}

// PUnsubscribe unsubscribes a client from one or more patterns
// If no patterns specified, unsubscribes from all patterns
func (ps *PubSub) PUnsubscribe(subscriberID string, patterns ...string) []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	unsubscribed := make([]string, 0)

	// If no patterns specified, unsubscribe from all
	if len(patterns) == 0 {
		for pattern := range ps.subscriberPatterns[subscriberID] {
			patterns = append(patterns, pattern)
		}
	}

	for _, pattern := range patterns {
		// Remove subscriber from pattern
		if subs, exists := ps.patterns[pattern]; exists {
			delete(subs, subscriberID)

			// Clean up empty pattern map
			if len(subs) == 0 {
				delete(ps.patterns, pattern)

				// OPTIMIZATION: Remove pattern from trie and compiled cache
				ps.patternTrie.Remove(pattern)
				delete(ps.compiledPatterns, pattern)
			}
		}

		// Remove pattern from subscriber's list
		if ps.subscriberPatterns[subscriberID] != nil {
			delete(ps.subscriberPatterns[subscriberID], pattern)
		}

		unsubscribed = append(unsubscribed, pattern)
	}

	return unsubscribed
}

// ==================== PUBLISHING OPERATIONS ====================

// Publish publishes a message to a channel
// Returns the number of subscribers that received the message
func (ps *PubSub) Publish(channel string, payload string) int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	count := 0

	// Send to channel subscribers
	if subs, exists := ps.channels[channel]; exists {
		msg := &Message{
			Type:    "message",
			Channel: channel,
			Payload: payload,
		}

		for _, sub := range subs {
			select {
			case sub.Channels <- msg:
				count++
			default:
				// Subscriber's channel is full, skip
			}
		}
	}

	// OPTIMIZATION: Use trie to get candidate patterns instead of checking all patterns
	// This reduces the search space from O(P) to O(log P) in most cases
	candidatePatterns := ps.patternTrie.GetMatchingPatterns(channel)

	// Send to pattern subscribers
	for _, pattern := range candidatePatterns {
		subs, exists := ps.patterns[pattern]
		if !exists {
			continue
		}

		// OPTIMIZATION: Use pre-compiled regex instead of compiling on every publish
		compiledRegex := ps.compiledPatterns[pattern]
		if compiledRegex != nil && compiledRegex.MatchString(channel) {
			msg := &Message{
				Type:    "pmessage",
				Pattern: pattern,
				Channel: channel,
				Payload: payload,
			}

			for _, sub := range subs {
				select {
				case sub.Channels <- msg:
					count++
				default:
					// Subscriber's channel is full, skip
				}
			}
		}
	}

	return count
}

// ==================== INTROSPECTION OPERATIONS ====================

// NumSub returns the number of subscribers for specified channels
func (ps *PubSub) NumSub(channels ...string) map[string]int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	result := make(map[string]int)

	for _, channel := range channels {
		if subs, exists := ps.channels[channel]; exists {
			result[channel] = len(subs)
		} else {
			result[channel] = 0
		}
	}

	return result
}

// NumPat returns the number of unique patterns subscribed to
func (ps *PubSub) NumPat() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	return len(ps.patterns)
}

// Channels returns all active channels matching the pattern
// If pattern is empty, returns all active channels
func (ps *PubSub) Channels(pattern string) []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	channels := make([]string, 0)

	for channel := range ps.channels {
		if pattern == "" || matchPattern(pattern, channel) {
			channels = append(channels, channel)
		}
	}

	return channels
}

// GetSubscriberCount returns total number of subscriptions for a subscriber
func (ps *PubSub) GetSubscriberCount(subscriberID string) int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	count := 0

	if channels, exists := ps.subscriberChannels[subscriberID]; exists {
		count += len(channels)
	}

	if patterns, exists := ps.subscriberPatterns[subscriberID]; exists {
		count += len(patterns)
	}

	return count
}

// RemoveSubscriber removes all subscriptions for a subscriber (cleanup on disconnect)
// RemoveSubscriber removes a subscriber from all channels and patterns
func (ps *PubSub) RemoveSubscriber(subscriberID string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Remove from all channels
	for channel := range ps.subscriberChannels[subscriberID] {
		if subs, exists := ps.channels[channel]; exists {
			delete(subs, subscriberID)
			if len(subs) == 0 {
				delete(ps.channels, channel)
			}
		}
	}
	delete(ps.subscriberChannels, subscriberID)

	// Remove from all patterns
	for pattern := range ps.subscriberPatterns[subscriberID] {
		if subs, exists := ps.patterns[pattern]; exists {
			delete(subs, subscriberID)
			if len(subs) == 0 {
				delete(ps.patterns, pattern)
			}
		}
	}
	delete(ps.subscriberPatterns, subscriberID)

	// Remove from subscribers map
	delete(ps.subscribers, subscriberID)
}

// GetSubscriber returns the subscriber object for a subscriber ID
func (ps *PubSub) GetSubscriber(subscriberID string) *Subscriber {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.subscribers[subscriberID]
}

// ==================== HELPER FUNCTIONS ====================

// compilePattern pre-compiles a glob pattern to regex for efficient reuse
func compilePattern(pattern string) *regexp.Regexp {
	// Convert glob pattern to regex
	// Escape special regex characters except * and ?
	regexPattern := regexp.QuoteMeta(pattern)

	// Replace escaped \* with .* (match any characters)
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, `.*`)

	// Replace escaped \? with . (match single character)
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, `.`)

	// Anchor pattern to match entire string
	regexPattern = "^" + regexPattern + "$"

	// Compile and return
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil
	}

	return re
}

// matchPattern matches a channel name against a glob-style pattern
// Supports * (any characters) and ? (single character)
// NOTE: This function is kept for backward compatibility (used by Channels introspection)
func matchPattern(pattern, channel string) bool {
	re := compilePattern(pattern)
	if re == nil {
		return false
	}
	return re.MatchString(channel)
}
