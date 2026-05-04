package message

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Hash computes the SHA-256 hash of the canonical JSON representation of the message.
// The hash is deterministic for the same message content.
// It excludes the Metadata["sha"] field to avoid circular hashing.
func (m *Message) Hash() (string, error) {
	// Create a canonical representation for hashing
	canonical, err := m.canonicalForHash()
	if err != nil {
		return "", fmt.Errorf("failed to create canonical message: %w", err)
	}

	hash := sha256.Sum256(canonical)
	return fmt.Sprintf("sha256:%x", hash), nil
}

// canonicalForHash creates a canonical byte representation for hashing.
// It excludes the "sha" field from metadata to avoid circular hashing.
func (m *Message) canonicalForHash() ([]byte, error) {
	// Create a copy of the message for hashing
	type hashMessage struct {
		Role      Role           `json:"role"`
		Content   []ContentBlock `json:"content"`
		Name      string         `json:"name,omitempty"`
		ToolCalls []ToolCall     `json:"tool_calls,omitempty"`
		ToolResult *ToolResult   `json:"tool_result,omitempty"`
		Metadata  map[string]any `json:"metadata,omitempty"`
	}

	hm := hashMessage{
		Role:      m.Role,
		Content:   m.Content,
		Name:      m.Name,
		ToolCalls: m.ToolCalls,
		ToolResult: m.ToolResult,
		Metadata:  make(map[string]any),
	}

	// Copy metadata, excluding "sha"
	if m.Metadata != nil {
		for k, v := range m.Metadata {
			if k != "sha" && k != "parent_hash" {
				hm.Metadata[k] = v
			}
		}
	}

	return json.Marshal(hm)
}

// ParentHash returns the SHA of the preceding message in the session,
// or empty string if this is the first message.
func (m *Message) ParentHash() string {
	if m.Metadata == nil {
		return ""
	}
	parent, ok := m.Metadata["parent_hash"].(string)
	if !ok {
		return ""
	}
	return parent
}

// SetParentHash sets the parent hash reference on the message.
func (m *Message) SetParentHash(sha string) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]any)
	}
	m.Metadata["parent_hash"] = sha
}

// SetHash computes and stores the hash in the message metadata.
func (m *Message) SetHash() error {
	hash, err := m.Hash()
	if err != nil {
		return err
	}
	if m.Metadata == nil {
		m.Metadata = make(map[string]any)
	}
	m.Metadata["sha"] = hash
	return nil
}

// GetHash returns the stored SHA hash, computing it if not present.
func (m *Message) GetHash() (string, error) {
	if m.Metadata != nil {
		if sha, ok := m.Metadata["sha"].(string); ok && sha != "" {
			return sha, nil
		}
	}
	// Compute and store hash
	return m.Hash()
}

// CompactionInfo is stored in a compaction checkpoint message's Metadata["compaction"].
type CompactionInfo struct {
	// CompactedHashes are the SHA hashes of the original messages that were compacted.
	CompactedHashes []string `json:"compacted_hashes"`

	// SummarySHA is the SHA of the summary message itself.
	SummarySHA string `json:"summary_sha"`

	// CompactedAt is the Unix timestamp when compaction occurred.
	CompactedAt int64 `json:"compacted_at"`

	// MessageCount is the number of messages that were compacted.
	MessageCount int `json:"message_count"`
}

// IsCompactionCheckpoint returns true if this message is a compaction checkpoint.
func (m *Message) IsCompactionCheckpoint() bool {
	if m.Metadata == nil {
		return false
	}
	_, ok := m.Metadata["compaction"]
	return ok && m.Role == RoleSystem
}

// GetCompactionInfo extracts CompactionInfo from a compaction checkpoint message.
func (m *Message) GetCompactionInfo() (*CompactionInfo, bool) {
	if !m.IsCompactionCheckpoint() {
		return nil, false
	}

	raw, ok := m.Metadata["compaction"]
	if !ok {
		return nil, false
	}

	// Try to handle both CompactionInfo struct and map
	switch v := raw.(type) {
	case *CompactionInfo:
		return v, true
	case CompactionInfo:
		return &v, true
	case map[string]any:
		// Convert from map
		info := &CompactionInfo{}
		if hashes, ok := v["compacted_hashes"].([]any); ok {
			for _, h := range hashes {
				if hs, ok := h.(string); ok {
					info.CompactedHashes = append(info.CompactedHashes, hs)
				}
			}
		}
		if sha, ok := v["summary_sha"].(string); ok {
			info.SummarySHA = sha
		}
		if ts, ok := v["compacted_at"].(float64); ok {
			info.CompactedAt = int64(ts)
		}
		if count, ok := v["message_count"].(float64); ok {
			info.MessageCount = int(count)
		}
		return info, true
	default:
		return nil, false
	}
}

// SetCompactionInfo stores CompactionInfo in the message metadata.
func (m *Message) SetCompactionInfo(info *CompactionInfo) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]any)
	}
	m.Metadata["compaction"] = info
}
