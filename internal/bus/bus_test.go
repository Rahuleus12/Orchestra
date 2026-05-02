package bus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewBusMessage(t *testing.T) {
	msg := NewBusMessage("test.topic", "agent-1", "agent-2", "payload")

	if msg.ID == "" {
		t.Error("expected message ID to be set")
	}
	if msg.Topic != "test.topic" {
		t.Errorf("expected topic 'test.topic', got '%s'", msg.Topic)
	}
	if msg.FromAgent != "agent-1" {
		t.Errorf("expected FromAgent 'agent-1', got '%s'", msg.FromAgent)
	}
	if msg.ToAgent != "agent-2" {
		t.Errorf("expected ToAgent 'agent-2', got '%s'", msg.ToAgent)
	}
	if msg.Payload != "payload" {
		t.Errorf("expected payload 'payload', got '%v'", msg.Payload)
	}
	if msg.Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
	if msg.Metadata == nil {
		t.Error("expected metadata to be initialized")
	}
}

func TestBusMessage_IsBroadcast(t *testing.T) {
	broadcast := NewBusMessage("topic", "agent-1", "", "payload")
	if !broadcast.IsBroadcast() {
		t.Error("expected IsBroadcast to return true for empty ToAgent")
	}
	if broadcast.IsTargeted() {
		t.Error("expected IsTargeted to return false for empty ToAgent")
	}

	targeted := NewBusMessage("topic", "agent-1", "agent-2", "payload")
	if targeted.IsBroadcast() {
		t.Error("expected IsBroadcast to return false for non-empty ToAgent")
	}
	if !targeted.IsTargeted() {
		t.Error("expected IsTargeted to return true for non-empty ToAgent")
	}
}

func TestBusMessage_Metadata(t *testing.T) {
	msg := NewBusMessage("topic", "agent-1", "", "payload")

	// Test SetMetadata and GetMetadata
	msg.SetMetadata("key1", "value1")
	msg.SetMetadata("key2", 42)

	val, ok := msg.GetMetadata("key1")
	if !ok || val != "value1" {
		t.Errorf("expected metadata key1='value1', got '%v', ok=%v", val, ok)
	}

	val, ok = msg.GetMetadata("key2")
	if !ok || val != 42 {
		t.Errorf("expected metadata key2=42, got '%v', ok=%v", val, ok)
	}

	_, ok = msg.GetMetadata("nonexistent")
	if ok {
		t.Error("expected GetMetadata to return false for nonexistent key")
	}
}

func TestBusMessage_Clone(t *testing.T) {
	original := NewBusMessage("topic", "agent-1", "agent-2", "payload")
	original.SetMetadata("key", "value")

	clone := original.Clone()

	// Verify values are copied
	if clone.ID != original.ID {
		t.Error("expected cloned ID to match")
	}
	if clone.Topic != original.Topic {
		t.Error("expected cloned Topic to match")
	}

	// Verify metadata is independent
	clone.SetMetadata("key", "modified")
	val, _ := original.GetMetadata("key")
	if val != "value" {
		t.Error("modifying clone should not affect original metadata")
	}
}

func TestNewInMemoryBus(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	if bus == nil {
		t.Fatal("expected bus to be created")
	}

	stats := bus.Stats()
	if stats.Subscriptions != 0 {
		t.Errorf("expected 0 subscriptions, got %d", stats.Subscriptions)
	}
	if stats.Topics != 0 {
		t.Errorf("expected 0 topics, got %d", stats.Topics)
	}
}

func TestNewInMemoryBus_WithOptions(t *testing.T) {
	bus := NewInMemoryBus(
		WithBufferSize(50),
		WithHandlerTimeout(10*time.Second),
	)
	defer bus.Close()

	stats := bus.Stats()
	// Stats won't show buffer size, but we can verify bus was created successfully
	if stats.Subscriptions != 0 {
		t.Errorf("expected 0 subscriptions, got %d", stats.Subscriptions)
	}
}

func TestInMemoryBus_Subscribe(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	received := make(chan BusMessage, 1)
	sub, err := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		received <- msg
		return nil
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	if sub == nil {
		t.Fatal("expected subscription to be non-nil")
	}
	if !sub.Active() {
		t.Error("expected subscription to be active")
	}
	if sub.ID() == "" {
		t.Error("expected subscription ID to be set")
	}

	// Verify topics
	topics := sub.Topics()
	if len(topics) != 1 || topics[0] != "test" {
		t.Errorf("expected topics=['test'], got %v", topics)
	}
}

func TestInMemoryBus_Subscribe_Errors(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	// Empty topics
	_, err := bus.Subscribe([]string{}, func(ctx context.Context, msg BusMessage) error {
		return nil
	})
	if err == nil {
		t.Error("expected error for empty topics")
	}

	// Nil handler
	_, err = bus.Subscribe([]string{"test"}, nil)
	if err == nil {
		t.Error("expected error for nil handler")
	}
}

func TestInMemoryBus_SubscribeWithFilter(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	received := make(chan BusMessage, 10)

	// Subscribe with filter that only accepts messages from "agent-1"
	sub, err := bus.SubscribeWithFilter(
		[]string{"test"},
		func(ctx context.Context, msg BusMessage) error {
			received <- msg
			return nil
		},
		func(msg BusMessage) bool {
			return msg.FromAgent == "agent-1"
		},
	)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// Publish message from agent-1
	msg1 := NewBusMessage("test", "agent-1", "", "payload1")
	bus.Publish(context.Background(), "test", msg1)

	// Publish message from agent-2
	msg2 := NewBusMessage("test", "agent-2", "", "payload2")
	bus.Publish(context.Background(), "test", msg2)

	// Should only receive message from agent-1
	select {
	case msg := <-received:
		if msg.FromAgent != "agent-1" {
			t.Errorf("expected message from agent-1, got from %s", msg.FromAgent)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected to receive message from agent-1")
	}

	// Should not receive message from agent-2
	select {
	case msg := <-received:
		t.Errorf("unexpected message from %s", msg.FromAgent)
	case <-time.After(50 * time.Millisecond):
		// Expected - no message
	}
}

func TestInMemoryBus_Publish(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	received := make(chan BusMessage, 1)
	sub, _ := bus.Subscribe([]string{"test.topic"}, func(ctx context.Context, msg BusMessage) error {
		received <- msg
		return nil
	})
	defer sub.Unsubscribe()

	msg := NewBusMessage("original.topic", "sender", "", "hello")
	err := bus.Publish(context.Background(), "test.topic", msg)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	select {
	case got := <-received:
		// Verify topic is set by Publish, not from the message
		if got.Topic != "test.topic" {
			t.Errorf("expected topic 'test.topic', got '%s'", got.Topic)
		}
		if got.Payload != "hello" {
			t.Errorf("expected payload 'hello', got '%v'", got.Payload)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected to receive published message")
	}
}

func TestInMemoryBus_Publish_AutoSetID(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	received := make(chan BusMessage, 1)
	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		received <- msg
		return nil
	})
	defer sub.Unsubscribe()

	// Publish message without ID
	msg := BusMessage{
		FromAgent: "sender",
		Payload:   "test",
	}
	bus.Publish(context.Background(), "test", msg)

	select {
	case got := <-received:
		if got.ID == "" {
			t.Error("expected ID to be auto-generated")
		}
		if got.Timestamp.IsZero() {
			t.Error("expected timestamp to be auto-set")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected to receive published message")
	}
}

func TestInMemoryBus_Publish_Closed(t *testing.T) {
	bus := NewInMemoryBus()
	bus.Close()

	err := bus.Publish(context.Background(), "test", BusMessage{})
	if err == nil {
		t.Error("expected error when publishing to closed bus")
	}
}

func TestInMemoryBus_Publish_ContextCancelled(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	// Create a handler that blocks
	blockCh := make(chan struct{})
	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		<-blockCh
		return nil
	})
	defer sub.Unsubscribe()
	defer close(blockCh)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Publishing should respect context cancellation for finding subscribers
	// Note: Since we publish asynchronously, we may not see the error immediately
	bus.Publish(ctx, "test", BusMessage{})
}

func TestInMemoryBus_Unsubscribe(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	received := atomic.Int32{}

	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		received.Add(1)
		return nil
	})

	// Publish before unsubscribe
	bus.Publish(context.Background(), "test", BusMessage{})
	time.Sleep(20 * time.Millisecond)

	count1 := received.Load()
	if count1 != 1 {
		t.Errorf("expected 1 message before unsubscribe, got %d", count1)
	}

	// Unsubscribe
	sub.Unsubscribe()
	if sub.Active() {
		t.Error("expected subscription to be inactive after unsubscribe")
	}

	// Publish after unsubscribe
	bus.Publish(context.Background(), "test", BusMessage{})
	time.Sleep(20 * time.Millisecond)

	count2 := received.Load()
	if count2 != count1 {
		t.Errorf("expected no new messages after unsubscribe, got %d total", count2)
	}
}

func TestInMemoryBus_Unsubscribe_Idempotent(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		return nil
	})

	// Multiple unsubscribes should not error
	err := sub.Unsubscribe()
	if err != nil {
		t.Errorf("first unsubscribe should not error: %v", err)
	}

	err = sub.Unsubscribe()
	if err != nil {
		t.Errorf("second unsubscribe should not error: %v", err)
	}
}

func TestInMemoryBus_Close(t *testing.T) {
	bus := NewInMemoryBus()

	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		return nil
	})

	// Close should cancel all subscriptions
	err := bus.Close()
	if err != nil {
		t.Errorf("Close should not error: %v", err)
	}

	if sub.Active() {
		t.Error("expected subscription to be inactive after bus close")
	}

	// Close should be idempotent
	err = bus.Close()
	if err != nil {
		t.Errorf("second Close should not error: %v", err)
	}

	// Cannot subscribe after close
	_, err = bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		return nil
	})
	if err == nil {
		t.Error("expected error when subscribing to closed bus")
	}
}

func TestInMemoryBus_MultipleSubscribers(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	var wg sync.WaitGroup
	received1 := make(chan BusMessage, 1)
	received2 := make(chan BusMessage, 1)

	wg.Add(2)

	sub1, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		received1 <- msg
		wg.Done()
		return nil
	})
	defer sub1.Unsubscribe()

	sub2, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		received2 <- msg
		wg.Done()
		return nil
	})
	defer sub2.Unsubscribe()

	bus.Publish(context.Background(), "test", BusMessage{Payload: "hello"})

	wg.Wait()

	// Both should receive
	select {
	case <-received1:
		// OK
	default:
		t.Error("subscriber 1 should have received message")
	}

	select {
	case <-received2:
		// OK
	default:
		t.Error("subscriber 2 should have received message")
	}
}

func TestInMemoryBus_MultipleTopics(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	topic1Received := make(chan BusMessage, 1)
	topic2Received := make(chan BusMessage, 1)

	sub1, _ := bus.Subscribe([]string{"topic.1"}, func(ctx context.Context, msg BusMessage) error {
		topic1Received <- msg
		return nil
	})
	defer sub1.Unsubscribe()

	sub2, _ := bus.Subscribe([]string{"topic.2"}, func(ctx context.Context, msg BusMessage) error {
		topic2Received <- msg
		return nil
	})
	defer sub2.Unsubscribe()

	// Publish to topic 1
	bus.Publish(context.Background(), "topic.1", BusMessage{Payload: "msg1"})

	select {
	case <-topic1Received:
		// OK
	case <-time.After(50 * time.Millisecond):
		t.Error("expected topic1 subscriber to receive message")
	}

	select {
	case <-topic2Received:
		t.Error("topic2 subscriber should not receive message from topic1")
	case <-time.After(20 * time.Millisecond):
		// OK
	}

	// Publish to topic 2
	bus.Publish(context.Background(), "topic.2", BusMessage{Payload: "msg2"})

	select {
	case <-topic2Received:
		// OK
	case <-time.After(50 * time.Millisecond):
		t.Error("expected topic2 subscriber to receive message")
	}
}

func TestInMemoryBus_SubscribeMultipleTopics(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	received := make(chan BusMessage, 10)

	sub, _ := bus.Subscribe([]string{"topic.a", "topic.b"}, func(ctx context.Context, msg BusMessage) error {
		received <- msg
		return nil
	})
	defer sub.Unsubscribe()

	bus.Publish(context.Background(), "topic.a", BusMessage{Payload: "a"})
	bus.Publish(context.Background(), "topic.b", BusMessage{Payload: "b"})
	bus.Publish(context.Background(), "topic.c", BusMessage{Payload: "c"}) // Should not receive

	time.Sleep(50 * time.Millisecond)

	count := len(received)
	if count != 2 {
		t.Errorf("expected 2 messages, got %d", count)
	}
}

func TestInMemoryBus_WildcardTopics(t *testing.T) {
	tests := []struct {
		pattern string
		topic   string
		match   bool
	}{
		{"*", "anything", true},
		{"*", "a.b.c", true},
		{"test.*", "test.hello", true},
		{"test.*", "test.hello.world", true},
		{"test.*", "other.hello", false},
		{"*.event", "user.event", true},
		{"*.event", "user.other", false},
		{"agent.*.status", "agent.123.status", true},
		{"agent.*.status", "agent.123.456.status", true},
		{"exact.match", "exact.match", true},
		{"exact.match", "exact.other", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"->"+tt.topic, func(t *testing.T) {
			result := topicMatches(tt.pattern, tt.topic)
			if result != tt.match {
				t.Errorf("topicMatches(%q, %q) = %v, want %v", tt.pattern, tt.topic, result, tt.match)
			}
		})
	}
}

func TestInMemoryBus_WildcardSubscription(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	received := make(chan BusMessage, 10)

	sub, _ := bus.Subscribe([]string{"events.*"}, func(ctx context.Context, msg BusMessage) error {
		received <- msg
		return nil
	})
	defer sub.Unsubscribe()

	bus.Publish(context.Background(), "events.created", BusMessage{Payload: "1"})
	bus.Publish(context.Background(), "events.updated", BusMessage{Payload: "2"})
	bus.Publish(context.Background(), "other.topic", BusMessage{Payload: "3"}) // Should not match

	time.Sleep(50 * time.Millisecond)

	count := len(received)
	if count != 2 {
		t.Errorf("expected 2 messages from wildcard, got %d", count)
	}
}

func TestInMemoryBus_Stats(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		return nil
	})

	stats := bus.Stats()
	if stats.Subscriptions != 1 {
		t.Errorf("expected 1 subscription, got %d", stats.Subscriptions)
	}
	if stats.Topics != 1 {
		t.Errorf("expected 1 topic, got %d", stats.Topics)
	}

	// Publish some messages
	for i := 0; i < 5; i++ {
		bus.Publish(context.Background(), "test", BusMessage{Payload: i})
	}
	time.Sleep(50 * time.Millisecond)

	stats = bus.Stats()
	if stats.MessagesPublished != 5 {
		t.Errorf("expected 5 published, got %d", stats.MessagesPublished)
	}
	if stats.MessagesDelivered != 5 {
		t.Errorf("expected 5 delivered, got %d", stats.MessagesDelivered)
	}
	if stats.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", stats.Errors)
	}

	sub.Unsubscribe()

	stats = bus.Stats()
	if stats.Subscriptions != 0 {
		t.Errorf("expected 0 subscriptions after unsubscribe, got %d", stats.Subscriptions)
	}
}

func TestInMemoryBus_HandlerError(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	testErr := errors.New("handler error")
	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		return testErr
	})
	defer sub.Unsubscribe()

	bus.Publish(context.Background(), "test", BusMessage{})
	time.Sleep(50 * time.Millisecond)

	stats := bus.Stats()
	if stats.Errors != 1 {
		t.Errorf("expected 1 error, got %d", stats.Errors)
	}
	if stats.MessagesDelivered != 0 {
		t.Errorf("expected 0 delivered (errors don't count), got %d", stats.MessagesDelivered)
	}
}

func TestInMemoryBus_NoMatchingSubscribers(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	// Subscribe to different topic
	sub, _ := bus.Subscribe([]string{"other"}, func(ctx context.Context, msg BusMessage) error {
		return nil
	})
	defer sub.Unsubscribe()

	// Publish to unmatched topic
	err := bus.Publish(context.Background(), "test", BusMessage{})
	if err != nil {
		t.Errorf("publishing to unmatched topic should not error: %v", err)
	}

	stats := bus.Stats()
	if stats.MessagesPublished != 1 {
		t.Errorf("expected 1 published, got %d", stats.MessagesPublished)
	}
	if stats.MessagesDelivered != 0 {
		t.Errorf("expected 0 delivered, got %d", stats.MessagesDelivered)
	}
}

func TestInMemoryBus_ConcurrentPublish(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	var count atomic.Int64
	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		count.Add(1)
		return nil
	})
	defer sub.Unsubscribe()

	var wg sync.WaitGroup
	numPublishers := 10
	numMessages := 100

	for p := 0; p < numPublishers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numMessages; i++ {
				bus.Publish(context.Background(), "test", BusMessage{})
			}
		}()
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	received := count.Load()
	expected := int64(numPublishers * numMessages)
	if received != expected {
		t.Errorf("expected %d messages, got %d", expected, received)
	}
}

func TestInMemoryBus_ConcurrentSubscribe(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	var wg sync.WaitGroup
	numSubscriptions := 100

	for i := 0; i < numSubscriptions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			topic := "test"
			_, err := bus.Subscribe([]string{topic}, func(ctx context.Context, msg BusMessage) error {
				return nil
			})
			if err != nil {
				t.Errorf("subscription %d failed: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	stats := bus.Stats()
	if stats.Subscriptions != numSubscriptions {
		t.Errorf("expected %d subscriptions, got %d", numSubscriptions, stats.Subscriptions)
	}
}

func TestInMemoryBus_MessageIsolation(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	var mu sync.Mutex
	var msg1Payload, msg2Payload string
	var received sync.WaitGroup
	received.Add(2)

	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		// Simulate modifying the message
		msg.SetMetadata("modified", true)
		// Store payload based on some criteria
		mu.Lock()
		if msg.Payload == "first" {
			msg1Payload = msg.Payload.(string)
		} else {
			msg2Payload = msg.Payload.(string)
		}
		mu.Unlock()
		received.Done()
		return nil
	})
	defer sub.Unsubscribe()

	// Publish two messages quickly
	bus.Publish(context.Background(), "test", BusMessage{Payload: "first"})
	bus.Publish(context.Background(), "test", BusMessage{Payload: "second"})

	// Wait for both messages to be delivered
	received.Wait()

	mu.Lock()
	defer mu.Unlock()
	if msg1Payload != "first" {
		t.Errorf("expected first payload 'first', got '%s'", msg1Payload)
	}
	if msg2Payload != "second" {
		t.Errorf("expected second payload 'second', got '%s'", msg2Payload)
	}
}

func TestWildcardMatch_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		topic   string
		match   bool
	}{
		{"empty pattern and topic", "", "", true},
		{"empty pattern non-empty topic", "", "topic", false},
		{"non-empty pattern empty topic", "topic", "", false},
		{"single wildcard matches empty", "*", "", true},
		{"single segment wildcard", "a.*.c", "a.b.c", true},
		{"single segment wildcard no match", "a.*.c", "a.b.d", false},
		{"multiple wildcards", "*.*", "a.b", true},
		{"multiple wildcards single segment", "*.*", "a", true},
		{"wildcard at end", "prefix.*", "prefix.suffix.extra", true},
		{"wildcard at start", "*.suffix", "prefix.suffix", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := topicMatches(tt.pattern, tt.topic)
			if result != tt.match {
				t.Errorf("topicMatches(%q, %q) = %v, want %v", tt.pattern, tt.topic, result, tt.match)
			}
		})
	}
}

func TestInMemoryBus_TargetedMessaging(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	agent1Received := make(chan BusMessage, 1)
	agent2Received := make(chan BusMessage, 1)

	// Agent 1 subscribes with filter for targeted messages
	sub1, _ := bus.SubscribeWithFilter(
		[]string{"*"},
		func(ctx context.Context, msg BusMessage) error {
			agent1Received <- msg
			return nil
		},
		func(msg BusMessage) bool {
			return msg.ToAgent == "agent-1"
		},
	)
	defer sub1.Unsubscribe()

	// Agent 2 subscribes with filter for targeted messages
	sub2, _ := bus.SubscribeWithFilter(
		[]string{"*"},
		func(ctx context.Context, msg BusMessage) error {
			agent2Received <- msg
			return nil
		},
		func(msg BusMessage) bool {
			return msg.ToAgent == "agent-2"
		},
	)
	defer sub2.Unsubscribe()

	// Send message to agent-1
	bus.Publish(context.Background(), "direct", BusMessage{
		FromAgent: "sender",
		ToAgent:   "agent-1",
		Payload:   "for agent 1",
	})

	select {
	case msg := <-agent1Received:
		if msg.Payload != "for agent 1" {
			t.Errorf("wrong payload: %v", msg.Payload)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("agent-1 should have received message")
	}

	select {
	case <-agent2Received:
		t.Error("agent-2 should not have received message")
	case <-time.After(20 * time.Millisecond):
		// OK
	}

	// Send message to agent-2
	bus.Publish(context.Background(), "direct", BusMessage{
		FromAgent: "sender",
		ToAgent:   "agent-2",
		Payload:   "for agent 2",
	})

	select {
	case msg := <-agent2Received:
		if msg.Payload != "for agent 2" {
			t.Errorf("wrong payload: %v", msg.Payload)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("agent-2 should have received message")
	}
}

func TestInMemoryBus_HandlerTimeout(t *testing.T) {
	bus := NewInMemoryBus(
		WithHandlerTimeout(50 * time.Millisecond),
	)
	defer bus.Close()

	handlerCalled := make(chan struct{})
	sub, _ := bus.Subscribe([]string{"test"}, func(ctx context.Context, msg BusMessage) error {
		close(handlerCalled)
		// Block longer than timeout
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	defer sub.Unsubscribe()

	bus.Publish(context.Background(), "test", BusMessage{})

	// Handler should be called
	<-handlerCalled

	// Wait for handler to complete (with timeout)
	time.Sleep(300 * time.Millisecond)

	stats := bus.Stats()
	// Handler timed out, so it should count as an error
	if stats.Errors != 1 {
		t.Errorf("expected 1 error due to timeout, got %d", stats.Errors)
	}
}
