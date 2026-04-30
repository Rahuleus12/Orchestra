package bus

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Handler processes a bus message. It receives the context and the message,
// and returns an error if processing fails.
type Handler func(ctx context.Context, msg BusMessage) error

// Subscription represents an active subscription to one or more topics.
// It can be used to unsubscribe or check the subscription status.
type Subscription struct {
	id      string
	topics  []string
	handler Handler
	cancel  context.CancelFunc
	active  atomic.Bool
	bus     *InMemoryBus
	filter  MessageFilter
}

// ID returns the unique identifier for this subscription.
func (s *Subscription) ID() string {
	return s.id
}

// Topics returns the topics this subscription is listening to.
func (s *Subscription) Topics() []string {
	return s.topics
}

// Active returns true if the subscription is still active.
func (s *Subscription) Active() bool {
	return s.active.Load()
}

// Unsubscribe removes this subscription from the bus and stops receiving messages.
func (s *Subscription) Unsubscribe() error {
	if !s.active.CompareAndSwap(true, false) {
		return nil // Already unsubscribed
	}

	s.cancel()

	if s.bus != nil {
		s.bus.removeSubscription(s)
	}

	return nil
}

// BusMessage represents a message sent through the bus.
// Messages can be targeted (ToAgent set) or broadcast (ToAgent empty).
type BusMessage struct {
	// ID is a unique identifier for this message.
	ID string `json:"id" yaml:"id"`

	// Topic is the topic this message was published to.
	Topic string `json:"topic" yaml:"topic"`

	// FromAgent is the ID of the agent that sent this message.
	FromAgent string `json:"from_agent" yaml:"from_agent"`

	// ToAgent is the ID of the target agent. Empty for broadcast messages.
	ToAgent string `json:"to_agent,omitempty" yaml:"to_agent,omitempty"`

	// Payload is the message content. Can be any type.
	Payload any `json:"payload" yaml:"payload"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`

	// Metadata contains arbitrary data associated with this message.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// NewBusMessage creates a new BusMessage with a generated ID and current timestamp.
func NewBusMessage(topic, fromAgent, toAgent string, payload any) BusMessage {
	return BusMessage{
		ID:        generateMessageID(),
		Topic:     topic,
		FromAgent: fromAgent,
		ToAgent:   toAgent,
		Payload:   payload,
		Timestamp: time.Now(),
		Metadata:  make(map[string]any),
	}
}

// IsBroadcast returns true if this message is a broadcast (not targeted to a specific agent).
func (m BusMessage) IsBroadcast() bool {
	return m.ToAgent == ""
}

// IsTargeted returns true if this message is targeted to a specific agent.
func (m BusMessage) IsTargeted() bool {
	return m.ToAgent != ""
}

// SetMetadata sets a metadata key-value pair on the message.
func (m *BusMessage) SetMetadata(key string, value any) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]any)
	}
	m.Metadata[key] = value
}

// GetMetadata retrieves a metadata value from the message.
func (m BusMessage) GetMetadata(key string) (any, bool) {
	val, ok := m.Metadata[key]
	return val, ok
}

// Clone returns a deep copy of the message.
func (m BusMessage) Clone() BusMessage {
	cp := m
	if m.Metadata != nil {
		cp.Metadata = make(map[string]any, len(m.Metadata))
		for k, v := range m.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}

// MessageFilter is a function that determines whether a message should be
// delivered to a subscriber. Return true to deliver, false to skip.
type MessageFilter func(msg BusMessage) bool

// Bus defines the interface for a message bus that supports publish-subscribe
// messaging between agents.
type Bus interface {
	// Publish sends a message to all subscribers of the given topic.
	// If the context is cancelled, publishing may be interrupted.
	Publish(ctx context.Context, topic string, msg BusMessage) error

	// Subscribe registers a handler for messages on the given topic(s).
	// Returns a Subscription that can be used to unsubscribe.
	Subscribe(topics []string, handler Handler) (*Subscription, error)

	// SubscribeWithFilter registers a handler with an additional message filter.
	// The filter is called before the handler; if it returns false, the message is skipped.
	SubscribeWithFilter(topics []string, handler Handler, filter MessageFilter) (*Subscription, error)

	// Unsubscribe removes a subscription from the bus.
	Unsubscribe(sub *Subscription) error

	// Close shuts down the bus and all subscriptions.
	Close() error

	// Stats returns statistics about the bus.
	Stats() BusStats
}

// BusStats holds statistics about bus operations.
type BusStats struct {
	// Subscriptions is the current number of active subscriptions.
	Subscriptions int `json:"subscriptions"`

	// Topics is the number of unique topics with subscribers.
	Topics int `json:"topics"`

	// MessagesPublished is the total number of messages published.
	MessagesPublished int64 `json:"messages_published"`

	// MessagesDelivered is the total number of messages delivered to handlers.
	MessagesDelivered int64 `json:"messages_delivered"`

	// MessagesDropped is the total number of messages dropped due to backpressure.
	MessagesDropped int64 `json:"messages_dropped"`

	// Errors is the total number of handler errors encountered.
	Errors int64 `json:"errors"`
}

// BusOption configures an InMemoryBus instance.
type BusOption func(*InMemoryBus)

// WithBusLogger sets the logger for the bus.
func WithBusLogger(logger *slog.Logger) BusOption {
	return func(b *InMemoryBus) {
		b.logger = logger
	}
}

// WithBufferSize sets the buffer size for subscriber channels.
// Larger buffers reduce backpressure but increase memory usage.
// Default is 100.
func WithBufferSize(size int) BusOption {
	return func(b *InMemoryBus) {
		b.bufferSize = size
	}
}

// WithHandlerTimeout sets the default timeout for handler execution.
// Handlers that exceed this timeout will be cancelled.
// Default is 30 seconds. Set to 0 for no timeout.
func WithHandlerTimeout(timeout time.Duration) BusOption {
	return func(b *InMemoryBus) {
		b.handlerTimeout = timeout
	}
}

// InMemoryBus is an in-process implementation of Bus using Go channels.
// It supports topic-based subscriptions with wildcard matching and
// direct agent-to-agent messaging.
type InMemoryBus struct {
	mu             sync.RWMutex
	subscriptions  map[string]*Subscription
	topicIndex     map[string][]*Subscription // topic -> subscriptions (including wildcards)
	logger         *slog.Logger
	bufferSize     int
	handlerTimeout time.Duration
	closed         atomic.Bool
	published      atomic.Int64
	delivered      atomic.Int64
	dropped        atomic.Int64
	errors         atomic.Int64
	wg             sync.WaitGroup
}

// NewInMemoryBus creates a new in-memory message bus with the given options.
func NewInMemoryBus(opts ...BusOption) *InMemoryBus {
	bus := &InMemoryBus{
		subscriptions:  make(map[string]*Subscription),
		topicIndex:     make(map[string][]*Subscription),
		logger:         slog.Default(),
		bufferSize:     100,
		handlerTimeout: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(bus)
	}

	return bus
}

// Publish sends a message to all matching subscribers.
// Messages are delivered asynchronously to avoid blocking the publisher.
func (b *InMemoryBus) Publish(ctx context.Context, topic string, msg BusMessage) error {
	if b.closed.Load() {
		return fmt.Errorf("bus is closed")
	}

	// Ensure the message has an ID and timestamp
	if msg.ID == "" {
		msg.ID = generateMessageID()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	msg.Topic = topic

	b.published.Add(1)

	b.logger.Debug("Publishing message",
		"message_id", msg.ID,
		"topic", topic,
		"from_agent", msg.FromAgent,
		"to_agent", msg.ToAgent,
	)

	// Find matching subscriptions
	b.mu.RLock()
	var targets []*Subscription
	for _, sub := range b.subscriptions {
		if !sub.active.Load() {
			continue
		}
		if b.matchesTopics(sub.topics, topic) {
			// Apply filter if present
			if sub.filter == nil || sub.filter(msg) {
				targets = append(targets, sub)
			}
		}
	}
	b.mu.RUnlock()

	if len(targets) == 0 {
		b.logger.Debug("No matching subscribers for message",
			"message_id", msg.ID,
			"topic", topic,
		)
		return nil
	}

	// Deliver to each subscriber
	var deliveryErrors []error
	for _, sub := range targets {
		if !sub.active.Load() {
			continue
		}

		// Clone the message for each subscriber to avoid shared state issues
		msgCopy := msg.Clone()

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Deliver asynchronously
			b.wg.Add(1)
			go func(s *Subscription, m BusMessage) {
				defer b.wg.Done()
				b.deliverMessage(s, m)
			}(sub, msgCopy)
		}
	}

	if len(deliveryErrors) > 0 {
		return fmt.Errorf("delivery errors: %v", deliveryErrors)
	}

	return nil
}

// Subscribe registers a handler for messages on the given topic(s).
// Topics can include wildcards (*) to match multiple topics.
func (b *InMemoryBus) Subscribe(topics []string, handler Handler) (*Subscription, error) {
	return b.SubscribeWithFilter(topics, handler, nil)
}

// SubscribeWithFilter registers a handler with an additional message filter.
func (b *InMemoryBus) SubscribeWithFilter(topics []string, handler Handler, filter MessageFilter) (*Subscription, error) {
	if b.closed.Load() {
		return nil, fmt.Errorf("bus is closed")
	}

	if len(topics) == 0 {
		return nil, fmt.Errorf("at least one topic is required")
	}

	if handler == nil {
		return nil, fmt.Errorf("handler cannot be nil")
	}

	_, cancel := context.WithCancel(context.Background())

	sub := &Subscription{
		id:      generateSubscriptionID(),
		topics:  topics,
		handler: handler,
		cancel:  cancel,
		bus:     b,
		filter:  filter,
	}
	sub.active.Store(true)

	b.mu.Lock()
	b.subscriptions[sub.id] = sub
	for _, topic := range topics {
		b.topicIndex[topic] = append(b.topicIndex[topic], sub)
	}
	b.mu.Unlock()

	b.logger.Debug("New subscription",
		"subscription_id", sub.id,
		"topics", topics,
	)

	return sub, nil
}

// Unsubscribe removes a subscription from the bus.
func (b *InMemoryBus) Unsubscribe(sub *Subscription) error {
	return sub.Unsubscribe()
}

// removeSubscription is called by Subscription.Unsubscribe to clean up internal state.
func (b *InMemoryBus) removeSubscription(sub *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.subscriptions, sub.id)

	// Remove from topic index
	for _, topic := range sub.topics {
		subs := b.topicIndex[topic]
		for i, s := range subs {
			if s.id == sub.id {
				b.topicIndex[topic] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(b.topicIndex[topic]) == 0 {
			delete(b.topicIndex, topic)
		}
	}

	b.logger.Debug("Subscription removed",
		"subscription_id", sub.id,
		"topics", sub.topics,
	)
}

// Close shuts down the bus and cancels all subscriptions.
func (b *InMemoryBus) Close() error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	b.mu.Lock()
	// Cancel all subscriptions
	for _, sub := range b.subscriptions {
		sub.active.Store(false)
		sub.cancel()
	}
	b.subscriptions = make(map[string]*Subscription)
	b.topicIndex = make(map[string][]*Subscription)
	b.mu.Unlock()

	// Wait for in-flight deliveries to complete
	b.wg.Wait()

	b.logger.Info("Bus closed",
		"published", b.published.Load(),
		"delivered", b.delivered.Load(),
		"dropped", b.dropped.Load(),
		"errors", b.errors.Load(),
	)

	return nil
}

// Stats returns statistics about the bus.
func (b *InMemoryBus) Stats() BusStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return BusStats{
		Subscriptions:     len(b.subscriptions),
		Topics:            len(b.topicIndex),
		MessagesPublished: b.published.Load(),
		MessagesDelivered: b.delivered.Load(),
		MessagesDropped:   b.dropped.Load(),
		Errors:            b.errors.Load(),
	}
}

// deliverMessage delivers a message to a subscriber's handler.
func (b *InMemoryBus) deliverMessage(sub *Subscription, msg BusMessage) {
	if !sub.active.Load() {
		return
	}

	// Create context with optional timeout
	ctx := context.Background()
	var cancel context.CancelFunc
	if b.handlerTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, b.handlerTimeout)
		defer cancel()
	}

	// Execute handler
	err := sub.handler(ctx, msg)

	if err != nil {
		b.errors.Add(1)
		b.logger.Warn("Handler error",
			"subscription_id", sub.id,
			"message_id", msg.ID,
			"topic", msg.Topic,
			"error", err,
		)
	} else {
		b.delivered.Add(1)
	}
}

// matchesTopics checks if any of the subscription's topics match the message topic.
// Supports wildcard patterns:
//   - "foo.*" matches "foo.bar", "foo.baz", etc.
//   - "*" matches any single topic segment
//   - "**" matches any number of topic segments (not yet implemented)
func (b *InMemoryBus) matchesTopics(subTopics []string, msgTopic string) bool {
	for _, pattern := range subTopics {
		if topicMatches(pattern, msgTopic) {
			return true
		}
	}
	return false
}

// topicMatches checks if a pattern matches a topic.
func topicMatches(pattern, topic string) bool {
	if pattern == topic {
		return true
	}

	// Handle wildcard patterns
	if strings.Contains(pattern, "*") {
		patternParts := strings.Split(pattern, ".")
		topicParts := strings.Split(topic, ".")

		return wildcardMatch(patternParts, topicParts, 0, 0)
	}

	return false
}

// wildcardMatch performs recursive wildcard matching.
func wildcardMatch(pattern, topic []string, pi, ti int) bool {
	// Base case: both exhausted
	if pi == len(pattern) && ti == len(topic) {
		return true
	}

	// Pattern exhausted but topic remains
	if pi == len(pattern) {
		return false
	}

	// Current pattern is wildcard
	if pattern[pi] == "*" {
		// Wildcard can match zero or more segments
		// Try matching zero segments (skip wildcard)
		if wildcardMatch(pattern, topic, pi+1, ti) {
			return true
		}
		// Try matching one segment and keeping wildcard
		if ti < len(topic) {
			return wildcardMatch(pattern, topic, pi, ti+1)
		}
		return false
	}

	// Current pattern is literal
	if ti < len(topic) && pattern[pi] == topic[ti] {
		return wildcardMatch(pattern, topic, pi+1, ti+1)
	}

	return false
}

// Helper functions

func generateMessageID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("msg-%x-%d", b[:8], time.Now().UnixNano()%1000000)
}

func generateSubscriptionID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("sub-%x-%d", b[:6], time.Now().UnixNano()%1000000)
}
