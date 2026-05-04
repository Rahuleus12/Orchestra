package memory

import (
	"context"
	"fmt"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/tool"
)

// LookupMessageInput is the input for the lookup_message tool.
type LookupMessageInput struct {
	// SHA is the SHA-256 hash of the message to look up.
	SHA string `json:"sha" description:"The SHA-256 hash of the message to look up"`

	// IncludeChain indicates whether to include the full message chain.
	IncludeChain bool `json:"include_chain,omitempty" description:"If true, include the full chain of messages leading to this one"`

	// MaxChainDepth limits how far back to walk the chain (default: 10).
	MaxChainDepth int `json:"max_chain_depth,omitempty" description:"Maximum depth to walk the message chain"`
}

// LookupMessageOutput is the output of the lookup_message tool.
type LookupMessageOutput struct {
	// Found indicates whether the message was found.
	Found bool `json:"found"`

	// Message is the looked up message (if found).
	Message *MessageView `json:"message,omitempty"`

	// Chain is the full message chain (if requested).
	Chain []MessageView `json:"chain,omitempty"`

	// Error is any error that occurred.
	Error string `json:"error,omitempty"`
}

// MessageView is a simplified view of a message for tool output.
type MessageView struct {
	// SHA is the message's SHA hash.
	SHA string `json:"sha"`

	// Role is the message role.
	Role string `json:"role"`

	// Content is the message content.
	Content string `json:"content"`

	// ParentSHA is the parent message's hash.
	ParentSHA string `json:"parent_sha,omitempty"`

	// IsCompactionCheckpoint indicates if this is a compaction checkpoint.
	IsCompactionCheckpoint bool `json:"is_compaction_checkpoint,omitempty"`
}

// NewLookupMessageTool creates a tool that allows agents to look up messages by SHA hash.
// The journal is extracted from context using JournalFromContext.
func NewLookupMessageTool() (tool.Tool, error) {
	return tool.New("lookup_message",
		tool.WithDescription("Look up a previous message by its SHA-256 hash. This allows referencing specific messages from the conversation history, even after compaction."),
		tool.WithInputSchema[LookupMessageInput](),
		tool.WithHandler[LookupMessageInput, LookupMessageOutput](func(ctx context.Context, input LookupMessageInput) (LookupMessageOutput, error) {
			// Extract journal from context
			journal := JournalFromContext(ctx)
			if journal == nil {
				return LookupMessageOutput{
					Found: false,
					Error: "No journal available in context",
				}, nil
			}

			// Look up the message
			msg, ok := journal.Get(input.SHA)
			if !ok {
				// Check if it was compacted
				for _, info := range journal.compacted {
					for _, compactedHash := range info.CompactedHashes {
						if compactedHash == input.SHA {
							return LookupMessageOutput{
								Found: false,
								Error: fmt.Sprintf("Message %s was compacted into a summary. The original content is no longer available.", input.SHA),
							}, nil
						}
					}
				}
				return LookupMessageOutput{
					Found: false,
					Error: fmt.Sprintf("Message %s not found", input.SHA),
				}, nil
			}

			// Convert message to view
			view := messageToView(msg)

			output := LookupMessageOutput{
				Found:   true,
				Message: &view,
			}

			// Include chain if requested
			if input.IncludeChain {
				maxDepth := input.MaxChainDepth
				if maxDepth <= 0 {
					maxDepth = 10
				}

				chain, err := journal.ResolveChain(input.SHA, maxDepth)
				if err == nil && len(chain) > 0 {
					views := make([]MessageView, len(chain))
					for i, chainMsg := range chain {
						views[i] = messageToView(chainMsg)
					}
					output.Chain = views
				}
			}

			return output, nil
		}),
	)
}

// messageToView converts a message to a MessageView.
func messageToView(msg message.Message) MessageView {
	sha, _ := msg.GetHash()
	return MessageView{
		SHA:                   sha,
		Role:                  string(msg.Role),
		Content:               msg.Text(),
		ParentSHA:             msg.ParentHash(),
		IsCompactionCheckpoint: msg.IsCompactionCheckpoint(),
	}
}
