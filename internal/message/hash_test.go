package message

import (
	"testing"
)

func TestMessageHash(t *testing.T) {
	t.Run("ConsistentHash", func(t *testing.T) {
		msg := UserMessage("Hello, world!")
		hash1, err := msg.Hash()
		if err != nil {
			t.Fatalf("Hash() failed: %v", err)
		}

		hash2, err := msg.Hash()
		if err != nil {
			t.Fatalf("Hash() failed: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("Hash() = %q, want %q (not consistent)", hash1, hash2)
		}
	})

	t.Run("DifferentMessages", func(t *testing.T) {
		msg1 := UserMessage("Hello")
		msg2 := UserMessage("World")

		hash1, _ := msg1.Hash()
		hash2, _ := msg2.Hash()

		if hash1 == hash2 {
			t.Error("Different messages have same hash")
		}
	})

	t.Run("SHAFormat", func(t *testing.T) {
		msg := UserMessage("test")
		hash, err := msg.Hash()
		if err != nil {
			t.Fatalf("Hash() failed: %v", err)
		}

		// Check format: should start with "sha256:"
		if len(hash) < 7 || hash[:7] != "sha256:" {
			t.Errorf("Hash() = %q, want prefix 'sha256:'", hash)
		}
	})
}

func TestMessageSetHash(t *testing.T) {
	msg := UserMessage("test")

	err := msg.SetHash()
	if err != nil {
		t.Fatalf("SetHash() failed: %v", err)
	}

	storedHash, ok := msg.Metadata["sha"].(string)
	if !ok {
		t.Fatal("SetHash() did not store hash in metadata")
	}

	if storedHash == "" {
		t.Error("SetHash() stored empty hash")
	}
}

func TestMessageGetHash(t *testing.T) {
	t.Run("ComputeAndCache", func(t *testing.T) {
		msg := UserMessage("test")

		hash, err := msg.GetHash()
		if err != nil {
			t.Fatalf("GetHash() failed: %v", err)
		}

		// Second call should return cached value
		hash2, err := msg.GetHash()
		if err != nil {
			t.Fatalf("GetHash() failed: %v", err)
		}

		if hash != hash2 {
			t.Error("GetHash() returned different values on subsequent calls")
		}
	})

	t.Run("CachedHash", func(t *testing.T) {
		msg := UserMessage("test")
		msg.Metadata = map[string]any{"sha": "sha256:cached"}

		hash, err := msg.GetHash()
		if err != nil {
			t.Fatalf("GetHash() failed: %v", err)
		}

		if hash != "sha256:cached" {
			t.Errorf("GetHash() = %q, want 'sha256:cached' (cached value)", hash)
		}
	})
}

func TestMessageParentHash(t *testing.T) {
	msg := UserMessage("test")

	t.Run("Default", func(t *testing.T) {
		parent := msg.ParentHash()
		if parent != "" {
			t.Errorf("ParentHash() = %q, want empty string", parent)
		}
	})

	t.Run("SetAndGet", func(t *testing.T) {
		msg.SetParentHash("sha256:parent123")
		parent := msg.ParentHash()
		if parent != "sha256:parent123" {
			t.Errorf("ParentHash() = %q, want 'sha256:parent123'", parent)
		}
	})
}

func TestCompactionInfo(t *testing.T) {
	t.Run("IsCompactionCheckpoint", func(t *testing.T) {
		regularMsg := UserMessage("test")
		if regularMsg.IsCompactionCheckpoint() {
			t.Error("IsCompactionCheckpoint() = true for regular message, want false")
		}

		checkpointMsg := SystemMessage("[Summary]")
		checkpointMsg.SetCompactionInfo(&CompactionInfo{
			CompactedHashes: []string{"sha256:1", "sha256:2"},
			MessageCount:    2,
		})
		if !checkpointMsg.IsCompactionCheckpoint() {
			t.Error("IsCompactionCheckpoint() = false for checkpoint message, want true")
		}
	})

	t.Run("GetCompactionInfo", func(t *testing.T) {
		expected := &CompactionInfo{
			CompactedHashes: []string{"sha256:a", "sha256:b"},
			SummarySHA:      "sha256:summary",
			CompactedAt:     1234567890,
			MessageCount:    2,
		}

		msg := SystemMessage("[Summary]")
		msg.SetCompactionInfo(expected)

		info, ok := msg.GetCompactionInfo()
		if !ok {
			t.Fatal("GetCompactionInfo() returned ok=false, want true")
		}

		if len(info.CompactedHashes) != 2 {
			t.Errorf("GetCompactionInfo() has %d compacted hashes, want 2", len(info.CompactedHashes))
		}
		if info.MessageCount != 2 {
			t.Errorf("GetCompactionInfo() MessageCount = %d, want 2", info.MessageCount)
		}
	})
}
