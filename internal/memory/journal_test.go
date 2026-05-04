package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/user/orchestra/internal/message"
)

func TestSessionJournal(t *testing.T) {
	ctx := context.Background()
	journal := NewSessionJournal(WithSessionID("test-session"))

	t.Run("Append", func(t *testing.T) {
		msg := message.UserMessage("Hello")
		hash, err := journal.Append(ctx, msg)
		if err != nil {
			t.Fatalf("Append() failed: %v", err)
		}
		if hash == "" {
			t.Error("Append() returned empty hash")
		}
		if journal.Size() != 1 {
			t.Errorf("Size() = %d, want 1", journal.Size())
		}
	})

	t.Run("Get", func(t *testing.T) {
		msg := message.UserMessage("World")
		hash, err := journal.Append(ctx, msg)
		if err != nil {
			t.Fatalf("Append() failed: %v", err)
		}

		got, ok := journal.Get(hash)
		if !ok {
			t.Fatal("Get() returned ok=false")
		}
		if got.Text() != "World" {
			t.Errorf("Get() text = %q, want 'World'", got.Text())
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		_, ok := journal.Get("nonexistent")
		if ok {
			t.Error("Get() returned ok=true for nonexistent hash")
		}
	})

	t.Run("Head", func(t *testing.T) {
		msg := message.AssistantMessage("Response")
		hash, _ := journal.Append(ctx, msg)

		head := journal.Head()
		if head != hash {
			t.Errorf("Head() = %q, want %q", head, hash)
		}
	})

	t.Run("SessionID", func(t *testing.T) {
		if id := journal.SessionID(); id != "test-session" {
			t.Errorf("SessionID() = %q, want 'test-session'", id)
		}
	})
}

func TestSessionJournalParentHash(t *testing.T) {
	ctx := context.Background()
	journal := NewSessionJournal()

	msg1 := message.UserMessage("First")
	hash1, _ := journal.Append(ctx, msg1)

	msg2 := message.UserMessage("Second")
	hash2, _ := journal.Append(ctx, msg2)

	// msg2 should have msg1 as parent
	got2, _ := journal.Get(hash2)
	parentHash := got2.ParentHash()
	if parentHash != hash1 {
		t.Errorf("ParentHash() = %q, want %q", parentHash, hash1)
	}
}

func TestSessionJournalResolveChain(t *testing.T) {
	ctx := context.Background()
	journal := NewSessionJournal()

	// Add messages in order
	msg1 := message.UserMessage("Message 1")
	_, _ = journal.Append(ctx, msg1)

	msg2 := message.AssistantMessage("Message 2")
	hash2, _ := journal.Append(ctx, msg2)

	msg3 := message.UserMessage("Message 3")
	_, _ = journal.Append(ctx, msg3)

	t.Run("FullChain", func(t *testing.T) {
		chain, err := journal.ResolveChain(hash2, 0)
		if err != nil {
			t.Fatalf("ResolveChain() failed: %v", err)
		}
		if len(chain) < 2 {
			t.Errorf("ResolveChain() returned %d messages, want at least 2", len(chain))
		}
	})

	t.Run("LimitedDepth", func(t *testing.T) {
		chain, err := journal.ResolveChain(hash2, 1)
		if err != nil {
			t.Fatalf("ResolveChain() failed: %v", err)
		}
		if len(chain) > 1 {
			t.Errorf("ResolveChain(depth=1) returned %d messages, want at most 1", len(chain))
		}
	})
}

func TestSessionJournalRecent(t *testing.T) {
	ctx := context.Background()
	journal := NewSessionJournal()

	for i := 0; i < 5; i++ {
		journal.Append(ctx, message.UserMessage("Message"))
	}

	t.Run("RecentN", func(t *testing.T) {
		msgs, err := journal.Recent(ctx, 2)
		if err != nil {
			t.Fatalf("Recent() failed: %v", err)
		}
		if len(msgs) != 2 {
			t.Errorf("Recent(2) returned %d messages, want 2", len(msgs))
		}
	})

	t.Run("RecentAll", func(t *testing.T) {
		msgs, err := journal.Recent(ctx, 0)
		if err != nil {
			t.Fatalf("Recent(0) failed: %v", err)
		}
		if len(msgs) != 5 {
			t.Errorf("Recent(0) returned %d messages, want 5", len(msgs))
		}
	})
}

func TestSessionJournalAll(t *testing.T) {
	ctx := context.Background()
	journal := NewSessionJournal()

	journal.Append(ctx, message.UserMessage("Message 1"))
	journal.Append(ctx, message.AssistantMessage("Message 2"))

	msgs := journal.All()
	if len(msgs) != 2 {
		t.Errorf("All() returned %d messages, want 2", len(msgs))
	}
}

func TestJournalMemory(t *testing.T) {
	ctx := context.Background()
	mem := NewJournalMemory()

	t.Run("ImplementsMemory", func(t *testing.T) {
		var _ Memory = mem
	})

	t.Run("AddAndGetRelevant", func(t *testing.T) {
		err := mem.Add(ctx, message.UserMessage("Hello"))
		if err != nil {
			t.Fatalf("Add() failed: %v", err)
		}

		msgs, err := mem.GetRelevant(ctx, "Hello", DefaultGetOptions())
		if err != nil {
			t.Fatalf("GetRelevant() failed: %v", err)
		}
		if len(msgs) != 1 {
			t.Errorf("GetRelevant() returned %d messages, want 1", len(msgs))
		}
	})

	t.Run("GetAll", func(t *testing.T) {
		msgs, err := mem.GetAll(ctx, DefaultGetOptions())
		if err != nil {
			t.Fatalf("GetAll() failed: %v", err)
		}
		if len(msgs) != 1 {
			t.Errorf("GetAll() returned %d messages, want 1", len(msgs))
		}
	})

	t.Run("Size", func(t *testing.T) {
		if size := mem.Size(ctx); size != 1 {
			t.Errorf("Size() = %d, want 1", size)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		err := mem.Clear(ctx)
		if err != nil {
			t.Fatalf("Clear() failed: %v", err)
		}
		if size := mem.Size(ctx); size != 0 {
			t.Errorf("Size() after Clear() = %d, want 0", size)
		}
	})
}

func TestJournalFromContext(t *testing.T) {
	ctx := context.Background()
	journal := NewSessionJournal()

	t.Run("NotInContext", func(t *testing.T) {
		got := JournalFromContext(ctx)
		if got != nil {
			t.Error("JournalFromContext() returned non-nil for empty context")
		}
	})

	t.Run("InContext", func(t *testing.T) {
		ctxWithJournal := ContextWithJournal(ctx, journal)
		got := JournalFromContext(ctxWithJournal)
		if got == nil {
			t.Error("JournalFromContext() returned nil")
		}
		if got.SessionID() != journal.SessionID() {
			t.Error("JournalFromContext() returned wrong journal")
		}
	})
}

func TestSessionJournalReplaceForCompaction(t *testing.T) {
	ctx := context.Background()
	journal := NewSessionJournal()

	// Add messages
	hashes := make([]string, 5)
	for i := 0; i < 5; i++ {
		msg := message.UserMessage(fmt.Sprintf("Message %d", i))
		hash, _ := journal.Append(ctx, msg)
		hashes[i] = hash
	}

	// Create a checkpoint
	checkpoint := message.SystemMessage("[Summary of messages 1-3]")
	info := &message.CompactionInfo{
		CompactedHashes: hashes[:3],
		MessageCount:    3,
		CompactedAt:     1234567890,
	}
	checkpoint.SetCompactionInfo(info)

	// Replace first 3 messages with checkpoint
	err := journal.ReplaceForCompaction(ctx, hashes[:3], checkpoint)
	if err != nil {
		t.Fatalf("ReplaceForCompaction() failed: %v", err)
	}

	// Journal should now have 3 entries: checkpoint + messages 4, 5
	if size := journal.Size(); size != 3 {
		t.Errorf("Size() after compaction = %d, want 3", size)
	}

	// Store should have 6 entries (3 original + checkpoint + messages 4, 5)
	if storeSize := journal.StoreSize(); storeSize != 6 {
		t.Errorf("StoreSize() after compaction = %d, want 6", storeSize)
	}
}
