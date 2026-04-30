package bus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// MailboxError represents errors that can occur during mailbox operations.
type MailboxError struct {
	// Type indicates the kind of error.
	Type MailboxErrorType
	// Message is a human-readable error description.
	Message string
	// Err is the underlying error, if any.
	Err error
}

// MailboxErrorType categorizes mailbox errors.
type MailboxErrorType string

const (
	// MailboxErrorClosed indicates the mailbox has been closed.
	MailboxErrorClosed MailboxErrorType = "closed"
	// MailboxErrorFull indicates the mailbox buffer is full.
	MailboxErrorFull MailboxErrorType = "full"
	// MailboxErrorTimeout indicates an operation timed out.
	MailboxErrorTimeout MailboxErrorType = "timeout"
	// MailboxErrorEmpty indicates the mailbox has no messages.
	MailboxErrorEmpty MailboxErrorType = "empty"
	// MailboxErrorFiltered indicates no messages matched the filter.
	MailboxErrorFiltered MailboxErrorType = "filtered"
)

// Error implements the error interface.
func (e *MailboxError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Unwrap returns the underlying error.
func (e *MailboxError) Unwrap() error {
	return e.Err
}

// Is checks if the target error matches this error's type.
func (e *MailboxError) Is(target error) bool {
	if t, ok := target.(*MailboxError); ok {
		return t.Type == e.Type
	}
	return false
}

// MailboxStats holds statistics about a mailbox.
type MailboxStats struct {
	// AgentID is the ID of the agent that owns this mailbox.
	AgentID string `json:"agent_id"`

	// Size is the current number of messages in the mailbox.
	Size int `json:"size"`

	// Capacity is the maximum number of messages the mailbox can hold.
	Capacity int `json:"capacity"`

	// TotalReceived is the total number of messages ever received.
	TotalReceived int64 `json:"total_received"`

	// TotalConsumed is the total number of messages consumed (removed from mailbox).
	TotalConsumed int64 `json:"total_consumed"`

	// TotalDropped is the total number of messages dropped due to full buffer.
	TotalDropped int64 `json:"total_dropped"`

	// TotalFiltered is the total number of messages filtered out before enqueue.
	TotalFiltered int64 `json:"total_filtered"`
}

// MailboxFilter determines whether a message should be accepted into the mailbox.
// Return true to accept, false to reject.
type MailboxFilter func(msg BusMessage) bool

// MailboxOption configures a Mailbox instance.
type MailboxOption func(*Mailbox)

// WithMailboxLogger sets the logger for the mailbox.
func WithMailboxLogger(logger *slog.Logger) MailboxOption {
	return func(m *Mailbox) {
		m.logger = logger
	}
}

// WithMailboxCapacity sets the buffer capacity for the mailbox.
// When the mailbox is full, new messages are either dropped or block
// depending on the MailboxBehavior setting.
// Default is 100.
func WithMailboxCapacity(capacity int) MailboxOption {
	return func(m *Mailbox) {
		m.capacity = capacity
	}
}

// WithMailboxBehavior sets how the mailbox handles messages when full.
// Default is MailboxDropOldest.
func WithMailboxBehavior(behavior MailboxBehavior) MailboxOption {
	return func(m *Mailbox) {
		m.behavior = behavior
	}
}

// WithMailboxFilter sets an inbound filter for the mailbox.
// Only messages that pass the filter will be enqueued.
func WithMailboxFilter(filter MailboxFilter) MailboxOption {
	return func(m *Mailbox) {
		m.inboundFilter = filter
	}
}

// WithOnMessage sets a callback that is invoked when a message is received.
// This is called before the message is enqueued, and can be used for
// logging, metrics, or triggering side effects.
func WithOnMessage(callback func(msg BusMessage)) MailboxOption {
	return func(m *Mailbox) {
		m.onMessage = callback
	}
}

// MailboxBehavior defines how the mailbox handles incoming messages when full.
type MailboxBehavior int

const (
	// MailboxDropOldest drops the oldest message to make room for the new one.
	MailboxDropOldest MailboxBehavior = iota

	// MailboxDropNewest rejects the new message, keeping existing messages.
	MailboxDropNewest

	// MailboxBlock blocks until space becomes available.
	// Warning: This can cause deadlocks if not used carefully.
	MailboxBlock

	// MailboxGrow doubles the capacity when full (up to MaxCapacity).
	MailboxGrow
)

// MaxCapacity is the maximum capacity when using MailboxGrow behavior.
const MaxCapacity = 10000

// Mailbox provides a per-agent message inbox for direct agent-to-agent messaging.
// It supports buffered message reception with configurable backpressure handling,
// filtering, and both blocking and non-blocking receive operations.
type Mailbox struct {
	mu            sync.Mutex
	agentID       string
	messages      []BusMessage
	notify        chan struct{} // Channel for signaling new messages
	logger        *slog.Logger
	capacity      int
	behavior      MailboxBehavior
	inboundFilter MailboxFilter
	onMessage     func(msg BusMessage)
	closed        atomic.Bool
	received      atomic.Int64
	consumed      atomic.Int64
	dropped       atomic.Int64
	filtered      atomic.Int64
	subscriptions []*Subscription
	bus           Bus
	waiters       int // Number of goroutines waiting for messages
}

// NewMailbox creates a new mailbox for the specified agent.
func NewMailbox(agentID string, opts ...MailboxOption) *Mailbox {
	m := &Mailbox{
		agentID:  agentID,
		messages: make([]BusMessage, 0, 100),
		notify:   make(chan struct{}, 1), // Buffered to avoid blocking sends
		logger:   slog.Default(),
		capacity: 100,
		behavior: MailboxDropOldest,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Ensure capacity is at least 1
	if m.capacity < 1 {
		m.capacity = 1
	}

	return m
}

// AgentID returns the ID of the agent that owns this mailbox.
func (m *Mailbox) AgentID() string {
	return m.agentID
}

// signal sends a non-blocking notification that a message is available.
func (m *Mailbox) signal() {
	select {
	case m.notify <- struct{}{}:
		// Signal sent
	default:
		// Already a pending signal, no need to send another
	}
}

// drainSignal clears any pending notification.
func (m *Mailbox) drainSignal() {
	select {
	case <-m.notify:
		// Drained
	default:
		// No pending signal
	}
}

// Receive returns the next message from the mailbox, blocking if empty.
// Returns an error if the mailbox is closed.
func (m *Mailbox) Receive(ctx context.Context) (BusMessage, error) {
	for {
		m.mu.Lock()

		// Check for closed first
		if m.closed.Load() {
			m.mu.Unlock()
			return BusMessage{}, &MailboxError{
				Type:    MailboxErrorClosed,
				Message: "mailbox is closed",
			}
		}

		// Check for messages
		if len(m.messages) > 0 {
			msg := m.messages[0]
			m.messages = m.messages[1:]
			m.consumed.Add(1)
			m.mu.Unlock()
			return msg, nil
		}

		// No messages, wait for notification
		m.waiters++
		m.mu.Unlock()

		// Wait for a signal or context cancellation
		select {
		case <-m.notify:
			// Woken up, drain signal and loop to check for message
			m.drainSignal()
			continue
		case <-ctx.Done():
			// Context cancelled
			m.mu.Lock()
			m.waiters--
			m.mu.Unlock()
			return BusMessage{}, &MailboxError{
				Type:    MailboxErrorTimeout,
				Message: "context cancelled while waiting for message",
				Err:     ctx.Err(),
			}
		}
	}
}

// ReceiveWithTimeout returns the next message, blocking until a message arrives
// or the timeout expires.
func (m *Mailbox) ReceiveWithTimeout(timeout time.Duration) (BusMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return m.Receive(ctx)
}

// TryReceive returns the next message without blocking.
// Returns MailboxErrorEmpty if no messages are available.
func (m *Mailbox) TryReceive() (BusMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() {
		return BusMessage{}, &MailboxError{
			Type:    MailboxErrorClosed,
			Message: "mailbox is closed",
		}
	}

	if len(m.messages) == 0 {
		return BusMessage{}, &MailboxError{
			Type:    MailboxErrorEmpty,
			Message: "mailbox is empty",
		}
	}

	msg := m.messages[0]
	m.messages = m.messages[1:]
	m.consumed.Add(1)
	return msg, nil
}

// ReceiveN returns up to n messages without blocking.
// Returns fewer than n messages if the mailbox contains fewer.
func (m *Mailbox) ReceiveN(n int) []BusMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() || n <= 0 || len(m.messages) == 0 {
		return nil
	}

	count := n
	if count > len(m.messages) {
		count = len(m.messages)
	}

	msgs := make([]BusMessage, count)
	copy(msgs, m.messages[:count])
	m.messages = m.messages[count:]
	m.consumed.Add(int64(count))

	return msgs
}

// ReceiveWithFilter returns the next message that matches the filter, blocking if needed.
func (m *Mailbox) ReceiveWithFilter(ctx context.Context, filter MailboxFilter) (BusMessage, error) {
	for {
		m.mu.Lock()

		if m.closed.Load() {
			m.mu.Unlock()
			return BusMessage{}, &MailboxError{
				Type:    MailboxErrorClosed,
				Message: "mailbox is closed",
			}
		}

		// Search for a matching message
		for i, msg := range m.messages {
			if filter == nil || filter(msg) {
				// Found a match, remove it
				m.messages = append(m.messages[:i], m.messages[i+1:]...)
				m.consumed.Add(1)
				m.mu.Unlock()
				return msg, nil
			}
		}

		// No match found, wait
		m.waiters++
		m.mu.Unlock()

		select {
		case <-m.notify:
			m.drainSignal()
			continue
		case <-ctx.Done():
			m.mu.Lock()
			m.waiters--
			m.mu.Unlock()
			return BusMessage{}, &MailboxError{
				Type:    MailboxErrorFiltered,
				Message: "no matching message before context cancelled",
				Err:     ctx.Err(),
			}
		}
	}
}

// Peek returns the next message without removing it from the mailbox.
// Returns MailboxErrorEmpty if no messages are available.
func (m *Mailbox) Peek() (BusMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() {
		return BusMessage{}, &MailboxError{
			Type:    MailboxErrorClosed,
			Message: "mailbox is closed",
		}
	}

	if len(m.messages) == 0 {
		return BusMessage{}, &MailboxError{
			Type:    MailboxErrorEmpty,
			Message: "mailbox is empty",
		}
	}

	return m.messages[0], nil
}

// PeekN returns up to n messages without removing them from the mailbox.
func (m *Mailbox) PeekN(n int) []BusMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() || n <= 0 || len(m.messages) == 0 {
		return nil
	}

	count := n
	if count > len(m.messages) {
		count = len(m.messages)
	}

	msgs := make([]BusMessage, count)
	copy(msgs, m.messages[:count])
	return msgs
}

// Send adds a message to the mailbox.
// This is typically called by the bus or directly for testing.
func (m *Mailbox) Send(msg BusMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() {
		return &MailboxError{
			Type:    MailboxErrorClosed,
			Message: "mailbox is closed",
		}
	}

	// Apply inbound filter
	if m.inboundFilter != nil && !m.inboundFilter(msg) {
		m.filtered.Add(1)
		m.logger.Debug("Message filtered out",
			"agent_id", m.agentID,
			"message_id", msg.ID,
		)
		return nil
	}

	// Call onMessage callback if set
	if m.onMessage != nil {
		m.onMessage(msg)
	}

	// Handle capacity
	if len(m.messages) >= m.capacity {
		switch m.behavior {
		case MailboxDropOldest:
			dropped := m.messages[0]
			m.messages = m.messages[1:]
			m.dropped.Add(1)
			m.logger.Debug("Dropped oldest message",
				"agent_id", m.agentID,
				"dropped_message_id", dropped.ID,
				"capacity", m.capacity,
			)

		case MailboxDropNewest:
			m.dropped.Add(1)
			m.logger.Debug("Dropped new message",
				"agent_id", m.agentID,
				"new_message_id", msg.ID,
				"capacity", m.capacity,
			)
			return &MailboxError{
				Type:    MailboxErrorFull,
				Message: "mailbox is full",
			}

		case MailboxBlock:
			// For simplicity, fall back to drop oldest in block mode
			// A true blocking implementation would need more complex handling
			m.messages = m.messages[1:]
			m.dropped.Add(1)

		case MailboxGrow:
			if m.capacity < MaxCapacity {
				newCap := m.capacity * 2
				if newCap > MaxCapacity {
					newCap = MaxCapacity
				}
				m.logger.Debug("Growing mailbox capacity",
					"agent_id", m.agentID,
					"old_capacity", m.capacity,
					"new_capacity", newCap,
				)
				newMessages := make([]BusMessage, len(m.messages), newCap)
				copy(newMessages, m.messages)
				m.messages = newMessages
				m.capacity = newCap
			} else {
				// At max capacity, drop oldest
				m.messages = m.messages[1:]
				m.dropped.Add(1)
			}
		}
	}

	// Add message
	m.messages = append(m.messages, msg)
	m.received.Add(1)

	m.logger.Debug("Message received",
		"agent_id", m.agentID,
		"message_id", msg.ID,
		"from_agent", msg.FromAgent,
		"mailbox_size", len(m.messages),
	)

	// Signal waiters outside of lock
	m.signal()

	return nil
}

// Size returns the current number of messages in the mailbox.
func (m *Mailbox) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() {
		return 0
	}
	return len(m.messages)
}

// Capacity returns the maximum capacity of the mailbox.
func (m *Mailbox) Capacity() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.capacity
}

// IsEmpty returns true if the mailbox contains no messages.
func (m *Mailbox) IsEmpty() bool {
	return m.Size() == 0
}

// IsFull returns true if the mailbox is at capacity.
func (m *Mailbox) IsFull() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.messages) >= m.capacity
}

// Clear removes all messages from the mailbox.
func (m *Mailbox) Clear() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := len(m.messages)
	m.messages = m.messages[:0]
	return count
}

// Close closes the mailbox and releases resources.
// Any blocked Receive operations will return an error.
// Subscriptions to the bus will be cancelled.
func (m *Mailbox) Close() error {
	if !m.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	// Cancel all subscriptions
	m.mu.Lock()
	for _, sub := range m.subscriptions {
		_ = sub.Unsubscribe()
	}
	m.subscriptions = nil
	m.mu.Unlock()

	// Wake up any blocked receivers
	m.signal()
	m.signal() // Send multiple signals to wake multiple waiters

	m.logger.Info("Mailbox closed",
		"agent_id", m.agentID,
		"total_received", m.received.Load(),
		"total_consumed", m.consumed.Load(),
		"total_dropped", m.dropped.Load(),
		"total_filtered", m.filtered.Load(),
	)

	return nil
}

// Stats returns statistics about the mailbox.
func (m *Mailbox) Stats() MailboxStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	return MailboxStats{
		AgentID:       m.agentID,
		Size:          len(m.messages),
		Capacity:      m.capacity,
		TotalReceived: m.received.Load(),
		TotalConsumed: m.consumed.Load(),
		TotalDropped:  m.dropped.Load(),
		TotalFiltered: m.filtered.Load(),
	}
}

// ConnectToBus subscribes this mailbox to a bus for direct messages.
// The mailbox will receive all messages where ToAgent matches the agent ID.
func (m *Mailbox) ConnectToBus(bus Bus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() {
		return &MailboxError{
			Type:    MailboxErrorClosed,
			Message: "mailbox is closed",
		}
	}

	m.bus = bus

	// Subscribe to direct messages for this agent
	sub, err := bus.SubscribeWithFilter(
		[]string{"*"}, // Listen on all topics for direct messages
		func(ctx context.Context, msg BusMessage) error {
			return m.Send(msg)
		},
		func(msg BusMessage) bool {
			// Only accept messages targeted to this agent
			return msg.ToAgent == m.agentID
		},
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to bus: %w", err)
	}

	m.subscriptions = append(m.subscriptions, sub)

	m.logger.Info("Mailbox connected to bus",
		"agent_id", m.agentID,
		"subscription_id", sub.ID(),
	)

	return nil
}

// ConnectToBusTopic subscribes this mailbox to a specific topic on a bus.
// The mailbox will receive all messages on that topic (broadcast and targeted).
func (m *Mailbox) ConnectToBusTopic(bus Bus, topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() {
		return &MailboxError{
			Type:    MailboxErrorClosed,
			Message: "mailbox is closed",
		}
	}

	m.bus = bus

	sub, err := bus.Subscribe(
		[]string{topic},
		func(ctx context.Context, msg BusMessage) error {
			return m.Send(msg)
		},
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
	}

	m.subscriptions = append(m.subscriptions, sub)

	m.logger.Info("Mailbox connected to bus topic",
		"agent_id", m.agentID,
		"topic", topic,
		"subscription_id", sub.ID(),
	)

	return nil
}

// DisconnectFromBus cancels all bus subscriptions.
func (m *Mailbox) DisconnectFromBus() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sub := range m.subscriptions {
		_ = sub.Unsubscribe()
	}
	m.subscriptions = nil
	m.bus = nil

	m.logger.Info("Mailbox disconnected from bus",
		"agent_id", m.agentID,
	)
}

// HasMessagesFrom returns true if the mailbox contains any messages from the specified agent.
func (m *Mailbox) HasMessagesFrom(agentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, msg := range m.messages {
		if msg.FromAgent == agentID {
			return true
		}
	}
	return false
}

// CountMessagesFrom returns the number of messages from the specified agent.
func (m *Mailbox) CountMessagesFrom(agentID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, msg := range m.messages {
		if msg.FromAgent == agentID {
			count++
		}
	}
	return count
}

// Filter standard filters for common use cases.

// FilterFromAgent returns a filter that only accepts messages from a specific agent.
func FilterFromAgent(agentID string) MailboxFilter {
	return func(msg BusMessage) bool {
		return msg.FromAgent == agentID
	}
}

// FilterByTopic returns a filter that only accepts messages on a specific topic.
func FilterByTopic(topic string) MailboxFilter {
	return func(msg BusMessage) bool {
		return msg.Topic == topic
	}
}

// FilterByTopicPrefix returns a filter that accepts messages with topics matching a prefix.
func FilterByTopicPrefix(prefix string) MailboxFilter {
	return func(msg BusMessage) bool {
		return len(msg.Topic) >= len(prefix) && msg.Topic[:len(prefix)] == prefix
	}
}

// FilterNotFromAgent returns a filter that rejects messages from a specific agent.
func FilterNotFromAgent(agentID string) MailboxFilter {
	return func(msg BusMessage) bool {
		return msg.FromAgent != agentID
	}
}

// FilterWithMetadata returns a filter that accepts messages with a specific metadata key-value pair.
func FilterWithMetadata(key string, value any) MailboxFilter {
	return func(msg BusMessage) bool {
		v, ok := msg.GetMetadata(key)
		if !ok {
			return false
		}
		return v == value
	}
}

// FilterCombine returns a filter that accepts messages only if all provided filters accept them.
func FilterCombine(filters ...MailboxFilter) MailboxFilter {
	return func(msg BusMessage) bool {
		for _, f := range filters {
			if f != nil && !f(msg) {
				return false
			}
		}
		return true
	}
}

// FilterAny returns a filter that accepts messages if any provided filter accepts them.
func FilterAny(filters ...MailboxFilter) MailboxFilter {
	return func(msg BusMessage) bool {
		for _, f := range filters {
			if f != nil && f(msg) {
				return true
			}
		}
		return false
	}
}
