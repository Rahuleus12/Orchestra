package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewMailbox(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	if mailbox.AgentID() != "agent-1" {
		t.Errorf("expected AgentID 'agent-1', got '%s'", mailbox.AgentID())
	}

	stats := mailbox.Stats()
	if stats.Size != 0 {
		t.Errorf("expected initial size 0, got %d", stats.Size)
	}
	if stats.Capacity != 100 {
		t.Errorf("expected default capacity 100, got %d", stats.Capacity)
	}
}

func TestNewMailbox_WithOptions(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(50),
		WithMailboxBehavior(MailboxDropNewest),
		WithMailboxFilter(func(msg BusMessage) bool {
			return msg.FromAgent != "blocked"
		}),
	)
	defer mailbox.Close()

	if mailbox.Capacity() != 50 {
		t.Errorf("expected capacity 50, got %d", mailbox.Capacity())
	}
}

func TestNewMailbox_MinimumCapacity(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(0),
		WithMailboxCapacity(-10),
	)
	defer mailbox.Close()

	// Should be at least 1
	if mailbox.Capacity() < 1 {
		t.Errorf("expected capacity >= 1, got %d", mailbox.Capacity())
	}
}

func TestMailbox_SendAndTryReceive(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	msg := NewBusMessage("test", "sender", "agent-1", "hello")
	err := mailbox.Send(msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if mailbox.Size() != 1 {
		t.Errorf("expected size 1, got %d", mailbox.Size())
	}

	received, err := mailbox.TryReceive()
	if err != nil {
		t.Fatalf("TryReceive failed: %v", err)
	}

	if received.ID != msg.ID {
		t.Errorf("expected message ID %s, got %s", msg.ID, received.ID)
	}
	if received.Payload != "hello" {
		t.Errorf("expected payload 'hello', got %v", received.Payload)
	}

	if mailbox.Size() != 0 {
		t.Errorf("expected size 0 after receive, got %d", mailbox.Size())
	}
}

func TestMailbox_TryReceive_Empty(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	_, err := mailbox.TryReceive()

	if err == nil {
		t.Error("expected error for empty mailbox")
	}

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorEmpty) {
		t.Errorf("expected MailboxErrorEmpty, got %v", err)
	}
}

func TestMailbox_TryReceive_Closed(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	mailbox.Close()

	_, err := mailbox.TryReceive()

	if err == nil {
		t.Error("expected error for closed mailbox")
	}

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorClosed) {
		t.Errorf("expected MailboxErrorClosed, got %v", err)
	}
}

func TestMailbox_Receive_Blocking(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	received := make(chan BusMessage, 1)
	errCh := make(chan error, 1)

	go func() {
		msg, err := mailbox.Receive(context.Background())
		if err != nil {
			errCh <- err
			return
		}
		received <- msg
	}()

	// Give the goroutine time to block
	time.Sleep(20 * time.Millisecond)

	// Send a message
	msg := NewBusMessage("test", "sender", "agent-1", "hello")
	mailbox.Send(msg)

	select {
	case got := <-received:
		if got.Payload != "hello" {
			t.Errorf("expected payload 'hello', got %v", got.Payload)
		}
	case err := <-errCh:
		t.Fatalf("Receive returned error: %v", err)
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for Receive")
	}
}

func TestMailbox_Receive_ContextCancelled(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := mailbox.Receive(ctx)

	if err == nil {
		t.Error("expected error when context is cancelled")
	}

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorTimeout) {
		t.Errorf("expected MailboxErrorTimeout, got %v", err)
	}
}

func TestMailbox_ReceiveWithTimeout(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	// Test timeout
	start := time.Now()
	_, err := mailbox.ReceiveWithTimeout(50 * time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error")
	}

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorTimeout) {
		t.Errorf("expected MailboxErrorTimeout, got %v", err)
	}

	if elapsed < 40*time.Millisecond {
		t.Errorf("timeout happened too fast: %v", elapsed)
	}
}

func TestMailbox_ReceiveN(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	// Send 5 messages
	for i := 0; i < 5; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	// Receive 3
	msgs := mailbox.ReceiveN(3)
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}

	// Verify remaining
	if mailbox.Size() != 2 {
		t.Errorf("expected 2 remaining, got %d", mailbox.Size())
	}

	// Request more than available
	msgs = mailbox.ReceiveN(10)
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestMailbox_ReceiveWithFilter(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	// Send messages from different agents
	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "from A"))
	mailbox.Send(NewBusMessage("test", "agent-b", "agent-1", "from B"))
	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "from A again"))

	// Filter for messages from agent-a
	msg, err := mailbox.ReceiveWithFilter(context.Background(), FilterFromAgent("agent-a"))
	if err != nil {
		t.Fatalf("ReceiveWithFilter failed: %v", err)
	}

	if msg.Payload != "from A" {
		t.Errorf("expected payload 'from A', got %v", msg.Payload)
	}

	// Next filtered message should skip agent-b
	msg, err = mailbox.ReceiveWithFilter(context.Background(), FilterFromAgent("agent-a"))
	if err != nil {
		t.Fatalf("ReceiveWithFilter failed: %v", err)
	}

	if msg.Payload != "from A again" {
		t.Errorf("expected payload 'from A again', got %v", msg.Payload)
	}
}

func TestMailbox_ReceiveWithFilter_NoMatch(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "from A"))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := mailbox.ReceiveWithFilter(ctx, FilterFromAgent("agent-z"))

	if err == nil {
		t.Error("expected error when no match found")
	}

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorFiltered) {
		t.Errorf("expected MailboxErrorFiltered, got %v", err)
	}
}

func TestMailbox_Peek(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	mailbox.Send(NewBusMessage("test", "sender", "agent-1", "peek me"))

	msg, err := mailbox.Peek()
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}

	if msg.Payload != "peek me" {
		t.Errorf("expected payload 'peek me', got %v", msg.Payload)
	}

	// Message should still be in mailbox
	if mailbox.Size() != 1 {
		t.Errorf("expected size 1 after peek, got %d", mailbox.Size())
	}
}

func TestMailbox_Peek_Empty(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	_, err := mailbox.Peek()

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorEmpty) {
		t.Errorf("expected MailboxErrorEmpty, got %v", err)
	}
}

func TestMailbox_PeekN(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	for i := 0; i < 5; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	msgs := mailbox.PeekN(3)
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}

	// All should still be in mailbox
	if mailbox.Size() != 5 {
		t.Errorf("expected size 5 after peek, got %d", mailbox.Size())
	}
}

func TestMailbox_FifoOrder(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	for i := 0; i < 5; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	for i := 0; i < 5; i++ {
		msg, err := mailbox.TryReceive()
		if err != nil {
			t.Fatalf("TryReceive %d failed: %v", i, err)
		}
		if msg.Payload != i {
			t.Errorf("expected payload %d, got %v", i, msg.Payload)
		}
	}
}

func TestMailbox_IsEmpty_IsFull(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(3),
	)
	defer mailbox.Close()

	if !mailbox.IsEmpty() {
		t.Error("expected IsEmpty to be true initially")
	}
	if mailbox.IsFull() {
		t.Error("expected IsFull to be false initially")
	}

	mailbox.Send(NewBusMessage("test", "sender", "agent-1", 1))
	if mailbox.IsEmpty() {
		t.Error("expected IsEmpty to be false after send")
	}

	mailbox.Send(NewBusMessage("test", "sender", "agent-1", 2))
	mailbox.Send(NewBusMessage("test", "sender", "agent-1", 3))
	if !mailbox.IsFull() {
		t.Error("expected IsFull to be true")
	}
}

func TestMailbox_Clear(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	for i := 0; i < 5; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	cleared := mailbox.Clear()
	if cleared != 5 {
		t.Errorf("expected 5 cleared, got %d", cleared)
	}

	if mailbox.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", mailbox.Size())
	}
}

func TestMailbox_Close(t *testing.T) {
	mailbox := NewMailbox("agent-1")

	mailbox.Send(NewBusMessage("test", "sender", "agent-1", "before close"))

	err := mailbox.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Send after close should fail
	err = mailbox.Send(NewBusMessage("test", "sender", "agent-1", "after close"))
	if err == nil {
		t.Error("expected error when sending to closed mailbox")
	}

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorClosed) {
		t.Errorf("expected MailboxErrorClosed, got %v", err)
	}

	// Close should be idempotent
	err = mailbox.Close()
	if err != nil {
		t.Errorf("second Close should not error: %v", err)
	}
}

func TestMailbox_Close_UnblocksReceivers(t *testing.T) {
	mailbox := NewMailbox("agent-1")

	errCh := make(chan error, 1)
	go func() {
		_, err := mailbox.Receive(context.Background())
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	mailbox.Close()

	select {
	case err := <-errCh:
		var mboxErr *MailboxError
		if !isMailboxError(err, mboxErr, MailboxErrorClosed) {
			t.Errorf("expected MailboxErrorClosed, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for Receive to return after Close")
	}
}

func TestMailbox_DropOldest(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(3),
		WithMailboxBehavior(MailboxDropOldest),
	)
	defer mailbox.Close()

	// Fill the mailbox
	for i := 0; i < 3; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	// Send one more - should drop oldest (0)
	mailbox.Send(NewBusMessage("test", "sender", "agent-1", 3))

	if mailbox.Size() != 3 {
		t.Errorf("expected size 3, got %d", mailbox.Size())
	}

	// First message should be 1 (0 was dropped)
	msg, _ := mailbox.TryReceive()
	if msg.Payload != 1 {
		t.Errorf("expected payload 1, got %v", msg.Payload)
	}

	stats := mailbox.Stats()
	if stats.TotalDropped != 1 {
		t.Errorf("expected 1 dropped, got %d", stats.TotalDropped)
	}
}

func TestMailbox_DropNewest(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(3),
		WithMailboxBehavior(MailboxDropNewest),
	)
	defer mailbox.Close()

	// Fill the mailbox
	for i := 0; i < 3; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	// Send one more - should be rejected
	err := mailbox.Send(NewBusMessage("test", "sender", "agent-1", 3))

	if err == nil {
		t.Error("expected error when dropping newest")
	}

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorFull) {
		t.Errorf("expected MailboxErrorFull, got %v", err)
	}

	if mailbox.Size() != 3 {
		t.Errorf("expected size 3, got %d", mailbox.Size())
	}

	stats := mailbox.Stats()
	if stats.TotalDropped != 1 {
		t.Errorf("expected 1 dropped, got %d", stats.TotalDropped)
	}
}

func TestMailbox_Grow(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(4),
		WithMailboxBehavior(MailboxGrow),
	)
	defer mailbox.Close()

	// Fill the mailbox
	for i := 0; i < 4; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	initialCap := mailbox.Capacity()

	// Send one more - should grow
	mailbox.Send(NewBusMessage("test", "sender", "agent-1", 4))

	newCap := mailbox.Capacity()
	if newCap <= initialCap {
		t.Errorf("expected capacity to grow from %d, got %d", initialCap, newCap)
	}

	if mailbox.Size() != 5 {
		t.Errorf("expected size 5, got %d", mailbox.Size())
	}

	// All messages should be present
	for i := 0; i < 5; i++ {
		msg, _ := mailbox.TryReceive()
		if msg.Payload != i {
			t.Errorf("expected payload %d, got %v", i, msg.Payload)
		}
	}
}

func TestMailbox_InboundFilter(t *testing.T) {
	filterCalled := atomic.Int32{}

	mailbox := NewMailbox("agent-1",
		WithMailboxFilter(func(msg BusMessage) bool {
			filterCalled.Add(1)
			return msg.FromAgent != "blocked"
		}),
	)
	defer mailbox.Close()

	// Send from allowed agent
	mailbox.Send(NewBusMessage("test", "allowed", "agent-1", "ok"))
	// Send from blocked agent
	mailbox.Send(NewBusMessage("test", "blocked", "agent-1", "blocked"))

	if mailbox.Size() != 1 {
		t.Errorf("expected size 1 (filtered message not queued), got %d", mailbox.Size())
	}

	stats := mailbox.Stats()
	if stats.TotalFiltered != 1 {
		t.Errorf("expected 1 filtered, got %d", stats.TotalFiltered)
	}

	if filterCalled.Load() != 2 {
		t.Errorf("expected filter to be called 2 times, got %d", filterCalled.Load())
	}
}

func TestMailbox_OnMessage(t *testing.T) {
	var received []BusMessage
	var mu sync.Mutex

	mailbox := NewMailbox("agent-1",
		WithOnMessage(func(msg BusMessage) {
			mu.Lock()
			received = append(received, msg)
			mu.Unlock()
		}),
	)
	defer mailbox.Close()

	mailbox.Send(NewBusMessage("test", "sender", "agent-1", "msg1"))
	mailbox.Send(NewBusMessage("test", "sender", "agent-1", "msg2"))

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Errorf("expected onMessage called 2 times, got %d", len(received))
	}
}

func TestMailbox_Stats(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(10),
	)
	defer mailbox.Close()

	for i := 0; i < 5; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	// Consume 3
	mailbox.ReceiveN(3)

	stats := mailbox.Stats()
	if stats.AgentID != "agent-1" {
		t.Errorf("expected AgentID 'agent-1', got '%s'", stats.AgentID)
	}
	if stats.Size != 2 {
		t.Errorf("expected Size 2, got %d", stats.Size)
	}
	if stats.Capacity != 10 {
		t.Errorf("expected Capacity 10, got %d", stats.Capacity)
	}
	if stats.TotalReceived != 5 {
		t.Errorf("expected TotalReceived 5, got %d", stats.TotalReceived)
	}
	if stats.TotalConsumed != 3 {
		t.Errorf("expected TotalConsumed 3, got %d", stats.TotalConsumed)
	}
}

func TestMailbox_HasMessagesFrom(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	if mailbox.HasMessagesFrom("agent-a") {
		t.Error("expected HasMessagesFrom to return false for empty mailbox")
	}

	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "msg1"))
	mailbox.Send(NewBusMessage("test", "agent-b", "agent-1", "msg2"))
	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "msg3"))

	if !mailbox.HasMessagesFrom("agent-a") {
		t.Error("expected HasMessagesFrom to return true for agent-a")
	}
	if !mailbox.HasMessagesFrom("agent-b") {
		t.Error("expected HasMessagesFrom to return true for agent-b")
	}
	if mailbox.HasMessagesFrom("agent-c") {
		t.Error("expected HasMessagesFrom to return false for agent-c")
	}
}

func TestMailbox_CountMessagesFrom(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "msg1"))
	mailbox.Send(NewBusMessage("test", "agent-b", "agent-1", "msg2"))
	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "msg3"))

	if count := mailbox.CountMessagesFrom("agent-a"); count != 2 {
		t.Errorf("expected 2 messages from agent-a, got %d", count)
	}
	if count := mailbox.CountMessagesFrom("agent-b"); count != 1 {
		t.Errorf("expected 1 message from agent-b, got %d", count)
	}
	if count := mailbox.CountMessagesFrom("agent-c"); count != 0 {
		t.Errorf("expected 0 messages from agent-c, got %d", count)
	}
}

func TestMailbox_ConnectToBus(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	err := mailbox.ConnectToBus(bus)
	if err != nil {
		t.Fatalf("ConnectToBus failed: %v", err)
	}

	// Publish a targeted message
	msg := NewBusMessage("test.topic", "sender", "agent-1", "direct message")
	bus.Publish(context.Background(), "test.topic", msg)

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	received, err := mailbox.TryReceive()
	if err != nil {
		t.Fatalf("TryReceive failed: %v", err)
	}

	if received.Payload != "direct message" {
		t.Errorf("expected payload 'direct message', got %v", received.Payload)
	}
}

func TestMailbox_ConnectToBus_FiltersOtherAgents(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	mailbox.ConnectToBus(bus)

	// Publish messages to different agents
	bus.Publish(context.Background(), "test", NewBusMessage("test", "sender", "agent-1", "for agent-1"))
	bus.Publish(context.Background(), "test", NewBusMessage("test", "sender", "agent-2", "for agent-2"))
	bus.Publish(context.Background(), "test", NewBusMessage("test", "sender", "", "broadcast"))

	time.Sleep(50 * time.Millisecond)

	// Should only have the message for agent-1
	if mailbox.Size() != 1 {
		t.Errorf("expected 1 message (only for agent-1), got %d", mailbox.Size())
	}

	msg, _ := mailbox.TryReceive()
	if msg.Payload != "for agent-1" {
		t.Errorf("expected 'for agent-1', got %v", msg.Payload)
	}
}

func TestMailbox_ConnectToBus_Closed(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	mailbox := NewMailbox("agent-1")
	mailbox.Close()

	err := mailbox.ConnectToBus(bus)
	if err == nil {
		t.Error("expected error when connecting closed mailbox to bus")
	}
}

func TestMailbox_ConnectToBusTopic(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	err := mailbox.ConnectToBusTopic(bus, "specific.topic")
	if err != nil {
		t.Fatalf("ConnectToBusTopic failed: %v", err)
	}

	// Publish to the subscribed topic
	bus.Publish(context.Background(), "specific.topic", NewBusMessage("specific.topic", "sender", "", "topic msg"))
	// Publish to a different topic
	bus.Publish(context.Background(), "other.topic", NewBusMessage("other.topic", "sender", "", "other msg"))

	time.Sleep(50 * time.Millisecond)

	if mailbox.Size() != 1 {
		t.Errorf("expected 1 message (only from specific.topic), got %d", mailbox.Size())
	}

	msg, _ := mailbox.TryReceive()
	if msg.Payload != "topic msg" {
		t.Errorf("expected 'topic msg', got %v", msg.Payload)
	}
}

func TestMailbox_DisconnectFromBus(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	mailbox.ConnectToBus(bus)
	mailbox.DisconnectFromBus()

	// Publish after disconnect
	bus.Publish(context.Background(), "test", NewBusMessage("test", "sender", "agent-1", "after disconnect"))

	time.Sleep(50 * time.Millisecond)

	if mailbox.Size() != 0 {
		t.Errorf("expected 0 messages after disconnect, got %d", mailbox.Size())
	}
}

func TestMailbox_Close_CancelsBusSubscriptions(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	mailbox := NewMailbox("agent-1")
	mailbox.ConnectToBus(bus)

	mailbox.Close()

	stats := bus.Stats()
	if stats.Subscriptions != 0 {
		t.Errorf("expected 0 bus subscriptions after mailbox close, got %d", stats.Subscriptions)
	}
}

func TestMailbox_ConcurrentSend(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(1000),
	)
	defer mailbox.Close()

	var wg sync.WaitGroup
	numSenders := 10
	numMessages := 100

	for s := 0; s < numSenders; s++ {
		wg.Add(1)
		go func(sender int) {
			defer wg.Done()
			for i := 0; i < numMessages; i++ {
				mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
			}
		}(s)
	}

	wg.Wait()

	expected := numSenders * numMessages
	if mailbox.Size() != expected {
		t.Errorf("expected %d messages, got %d", expected, mailbox.Size())
	}

	stats := mailbox.Stats()
	if stats.TotalReceived != int64(expected) {
		t.Errorf("expected TotalReceived %d, got %d", expected, stats.TotalReceived)
	}
}

func TestMailbox_ConcurrentReceive(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	// Fill the mailbox
	for i := 0; i < 100; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	var totalReceived atomic.Int64
	var wg sync.WaitGroup
	numReceivers := 10

	for r := 0; r < numReceivers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, err := mailbox.TryReceive()
				if err != nil {
					return // Mailbox empty or closed
				}
				totalReceived.Add(1)
			}
		}()
	}

	wg.Wait()

	if totalReceived.Load() != 100 {
		t.Errorf("expected 100 total received, got %d", totalReceived.Load())
	}
}

func TestFilterFromAgent(t *testing.T) {
	filter := FilterFromAgent("agent-1")

	if !filter(NewBusMessage("test", "agent-1", "", "msg")) {
		t.Error("filter should accept message from agent-1")
	}
	if filter(NewBusMessage("test", "agent-2", "", "msg")) {
		t.Error("filter should reject message from agent-2")
	}
}

func TestFilterByTopic(t *testing.T) {
	filter := FilterByTopic("specific.topic")

	if !filter(NewBusMessage("specific.topic", "sender", "", "msg")) {
		t.Error("filter should accept message on specific.topic")
	}
	if filter(NewBusMessage("other.topic", "sender", "", "msg")) {
		t.Error("filter should reject message on other.topic")
	}
}

func TestFilterByTopicPrefix(t *testing.T) {
	filter := FilterByTopicPrefix("events.")

	if !filter(NewBusMessage("events.created", "sender", "", "msg")) {
		t.Error("filter should accept message with prefix")
	}
	if !filter(NewBusMessage("events.", "sender", "", "msg")) {
		t.Error("filter should accept message with exact prefix")
	}
	if filter(NewBusMessage("event.created", "sender", "", "msg")) {
		t.Error("filter should reject message without prefix")
	}
	if filter(NewBusMessage("other", "sender", "", "msg")) {
		t.Error("filter should reject message with different prefix")
	}
}

func TestFilterNotFromAgent(t *testing.T) {
	filter := FilterNotFromAgent("blocked")

	if !filter(NewBusMessage("test", "other", "", "msg")) {
		t.Error("filter should accept message from other agent")
	}
	if filter(NewBusMessage("test", "blocked", "", "msg")) {
		t.Error("filter should reject message from blocked agent")
	}
}

func TestFilterWithMetadata(t *testing.T) {
	filter := FilterWithMetadata("priority", "high")

	msg := NewBusMessage("test", "sender", "", "msg")
	msg.SetMetadata("priority", "high")
	if !filter(msg) {
		t.Error("filter should accept message with matching metadata")
	}

	msg2 := NewBusMessage("test", "sender", "", "msg")
	msg2.SetMetadata("priority", "low")
	if filter(msg2) {
		t.Error("filter should reject message with non-matching metadata value")
	}

	msg3 := NewBusMessage("test", "sender", "", "msg")
	if filter(msg3) {
		t.Error("filter should reject message without metadata key")
	}
}

func TestFilterCombine(t *testing.T) {
	filter := FilterCombine(
		FilterFromAgent("agent-1"),
		FilterByTopic("specific"),
	)

	// Both match
	msg := NewBusMessage("specific", "agent-1", "", "msg")
	if !filter(msg) {
		t.Error("filter should accept when both match")
	}

	// Only agent matches
	msg = NewBusMessage("other", "agent-1", "", "msg")
	if filter(msg) {
		t.Error("filter should reject when only agent matches")
	}

	// Only topic matches
	msg = NewBusMessage("specific", "agent-2", "", "msg")
	if filter(msg) {
		t.Error("filter should reject when only topic matches")
	}

	// Neither matches
	msg = NewBusMessage("other", "agent-2", "", "msg")
	if filter(msg) {
		t.Error("filter should reject when neither matches")
	}
}

func TestFilterCombine_WithNil(t *testing.T) {
	filter := FilterCombine(nil, FilterFromAgent("agent-1"))

	msg := NewBusMessage("test", "agent-1", "", "msg")
	if !filter(msg) {
		t.Error("filter should accept with nil filter in combine")
	}
}

func TestFilterAny(t *testing.T) {
	filter := FilterAny(
		FilterFromAgent("agent-1"),
		FilterByTopic("priority"),
	)

	// Agent matches
	msg := NewBusMessage("test", "agent-1", "", "msg")
	if !filter(msg) {
		t.Error("filter should accept when agent matches")
	}

	// Topic matches
	msg = NewBusMessage("priority", "agent-2", "", "msg")
	if !filter(msg) {
		t.Error("filter should accept when topic matches")
	}

	// Both match
	msg = NewBusMessage("priority", "agent-1", "", "msg")
	if !filter(msg) {
		t.Error("filter should accept when both match")
	}

	// Neither matches
	msg = NewBusMessage("other", "agent-2", "", "msg")
	if filter(msg) {
		t.Error("filter should reject when neither matches")
	}
}

func TestMailboxError(t *testing.T) {
	err := &MailboxError{
		Type:    MailboxErrorClosed,
		Message: "mailbox is closed",
	}

	if err.Error() != "closed: mailbox is closed" {
		t.Errorf("unexpected error message: %s", err.Error())
	}

	// Test with underlying error
	innerErr := context.Canceled
	err = &MailboxError{
		Type:    MailboxErrorTimeout,
		Message: "timeout",
		Err:     innerErr,
	}

	if err.Unwrap() != innerErr {
		t.Error("Unwrap should return the inner error")
	}

	// Test Is
	target := &MailboxError{Type: MailboxErrorTimeout}
	if !err.Is(target) {
		t.Error("Is should return true for matching type")
	}

	other := &MailboxError{Type: MailboxErrorClosed}
	if err.Is(other) {
		t.Error("Is should return false for different type")
	}
}

// isMailboxError is a test helper that checks if an error is a MailboxError of the expected type.
func isMailboxError(err error, target *MailboxError, expectedType MailboxErrorType) bool {
	if err == nil {
		return false
	}
	mboxErr, ok := err.(*MailboxError)
	if !ok {
		return false
	}
	return mboxErr.Type == expectedType
}

func TestMailbox_GrowToMaxCapacity(t *testing.T) {
	mailbox := NewMailbox("agent-1",
		WithMailboxCapacity(2),
		WithMailboxBehavior(MailboxGrow),
	)
	defer mailbox.Close()

	// Fill and grow multiple times
	// Starting at 2, growth pattern: 2 -> 4 -> 8 -> 16 -> 32...
	// With 20 messages, we'll grow to 32 (which can hold all 20)
	for i := 0; i < 20; i++ {
		mailbox.Send(NewBusMessage("test", "sender", "agent-1", i))
	}

	// Should have grown from initial capacity
	if mailbox.Capacity() <= 2 {
		t.Errorf("expected capacity to have grown from 2, got %d", mailbox.Capacity())
	}

	// Should not exceed MaxCapacity
	if mailbox.Capacity() > MaxCapacity {
		t.Errorf("capacity should not exceed MaxCapacity %d, got %d", MaxCapacity, mailbox.Capacity())
	}

	// Should have received all messages (no drops since we didn't hit MaxCapacity)
	stats := mailbox.Stats()
	if stats.TotalReceived != 20 {
		t.Errorf("expected 20 messages received, got %d", stats.TotalReceived)
	}
	if stats.Size != 20 {
		t.Errorf("expected 20 messages in mailbox, got %d", stats.Size)
	}

	// Verify the growth doubled correctly (2 -> 4 -> 8 -> 16 -> 32)
	// 20 messages need capacity >= 20, so we should have grown to 32
	expectedCapacity := 32
	if mailbox.Capacity() != expectedCapacity {
		t.Errorf("expected capacity %d after growth, got %d", expectedCapacity, mailbox.Capacity())
	}
}

func TestMailbox_MultipleReceivers_WithFilter(t *testing.T) {
	mailbox := NewMailbox("agent-1")
	defer mailbox.Close()

	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "a1"))
	mailbox.Send(NewBusMessage("test", "agent-b", "agent-1", "b1"))
	mailbox.Send(NewBusMessage("test", "agent-a", "agent-1", "a2"))
	mailbox.Send(NewBusMessage("test", "agent-c", "agent-1", "c1"))

	var fromA, fromB []string

	// Use a short timeout to avoid blocking forever when no more matching messages exist
	filterCtx, cancelFilter := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelFilter()

	// Drain messages from agent-a
	for {
		msg, err := mailbox.ReceiveWithFilter(filterCtx, FilterFromAgent("agent-a"))
		if err != nil {
			break
		}
		fromA = append(fromA, msg.Payload.(string))
	}

	// Drain messages from agent-b
	for {
		msg, err := mailbox.ReceiveWithFilter(filterCtx, FilterFromAgent("agent-b"))
		if err != nil {
			break
		}
		fromB = append(fromB, msg.Payload.(string))
	}

	if len(fromA) != 2 {
		t.Errorf("expected 2 messages from agent-a, got %d", len(fromA))
	}
	if len(fromB) != 1 {
		t.Errorf("expected 1 message from agent-b, got %d", len(fromB))
	}

	// agent-c message should remain
	if mailbox.Size() != 1 {
		t.Errorf("expected 1 remaining message from agent-c, got %d", mailbox.Size())
	}
}
