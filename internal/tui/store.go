package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session represents a persisted conversation session.
type Session struct {
	// ID is the unique session identifier.
	ID string `json:"id"`

	// Title is a user-visible title for the session.
	Title string `json:"title"`

	// AgentName is the name of the agent used in this session.
	AgentName string `json:"agent_name,omitempty"`

	// Model is the model used in this session.
	Model string `json:"model,omitempty"`

	// Messages is the conversation history.
	Messages []SessionMessage `json:"messages"`

	// CreatedAt is when the session was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the session was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// Metadata contains arbitrary session metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SessionMessage is a simplified message format for storage.
type SessionMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	Timestamp  time.Time       `json:"timestamp"`
	TokenUsage *TokenUsageInfo `json:"token_usage,omitempty"`
}

// TokenUsageInfo stores token usage for a message.
type TokenUsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// SessionStore manages session persistence.
type SessionStore struct {
	// Dir is the directory where sessions are stored.
	Dir string

	// Sessions holds loaded sessions indexed by ID.
	Sessions map[string]*Session

	// ActiveSessionID is the ID of the currently active session.
	ActiveSessionID string
}

// NewSessionStore creates a new SessionStore with the given directory.
func NewSessionStore(dir string) (*SessionStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	store := &SessionStore{
		Dir:      dir,
		Sessions: make(map[string]*Session),
	}

	// Load existing sessions
	if err := store.loadAll(); err != nil {
		return nil, fmt.Errorf("failed to load sessions: %w", err)
	}

	return store, nil
}

// CreateSession creates a new session with the given title.
func (s *SessionStore) CreateSession(title, agentName, model string) *Session {
	session := &Session{
		ID:        generateSessionID(),
		Title:     title,
		AgentName: agentName,
		Model:     model,
		Messages:  []SessionMessage{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]any),
	}

	s.Sessions[session.ID] = session
	s.ActiveSessionID = session.ID

	// Persist the new session
	_ = s.save(session)

	return session
}

// GetSession returns a session by ID.
func (s *SessionStore) GetSession(id string) (*Session, bool) {
	session, ok := s.Sessions[id]
	return session, ok
}

// GetActiveSession returns the currently active session.
func (s *SessionStore) GetActiveSession() (*Session, bool) {
	if s.ActiveSessionID == "" {
		return nil, false
	}
	return s.GetSession(s.ActiveSessionID)
}

// SetActiveSession sets the active session by ID.
func (s *SessionStore) SetActiveSession(id string) bool {
	if _, ok := s.Sessions[id]; !ok {
		return false
	}
	s.ActiveSessionID = id
	return true
}

// AddMessage adds a message to the active session.
func (s *SessionStore) AddMessage(role, content string, usage *TokenUsageInfo) error {
	session, ok := s.GetActiveSession()
	if !ok {
		return errors.New("no active session")
	}

	msg := SessionMessage{
		Role:       role,
		Content:    content,
		Timestamp:  time.Now(),
		TokenUsage: usage,
	}

	session.Messages = append(session.Messages, msg)
	session.UpdatedAt = time.Now()

	// Update title from first user message
	if role == "user" && len(session.Messages) == 1 {
		if len(content) > 50 {
			session.Title = content[:47] + "..."
		} else {
			session.Title = content
		}
	}

	return s.save(session)
}

// DeleteSession deletes a session by ID.
func (s *SessionStore) DeleteSession(id string) error {
	if _, ok := s.Sessions[id]; !ok {
		return errors.New("session not found")
	}

	delete(s.Sessions, id)

	// Remove the file
	path := s.sessionPath(id)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	// If we deleted the active session, clear it
	if s.ActiveSessionID == id {
		s.ActiveSessionID = ""
		// Set to the most recent session if available
		sessions := s.ListSessions()
		if len(sessions) > 0 {
			s.ActiveSessionID = sessions[0].ID
		}
	}

	return nil
}

// ClearMessages removes all messages from the active session.
func (s *SessionStore) ClearMessages() error {
	session, ok := s.GetActiveSession()
	if !ok {
		return errors.New("no active session")
	}

	session.Messages = []SessionMessage{}
	session.UpdatedAt = time.Now()

	return s.save(session)
}

// ListSessions returns all sessions sorted by most recently updated.
func (s *SessionStore) ListSessions() []*Session {
	sessions := make([]*Session, 0, len(s.Sessions))
	for _, session := range s.Sessions {
		sessions = append(sessions, session)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions
}

// SearchSessions returns sessions whose title contains the query.
func (s *SessionStore) SearchSessions(query string) []*Session {
	var results []*Session
	for _, session := range s.Sessions {
		if containsFold(session.Title, query) ||
			containsFold(session.AgentName, query) {
			results = append(results, session)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})

	return results
}

// ExportSession exports a session to JSON format.
func (s *SessionStore) ExportSession(id string) ([]byte, error) {
	session, ok := s.GetSession(id)
	if !ok {
		return nil, errors.New("session not found")
	}

	return json.MarshalIndent(session, "", "  ")
}

// ExportSessionToMarkdown exports a session to Markdown format.
func (s *SessionStore) ExportSessionToMarkdown(id string) ([]byte, error) {
	session, ok := s.GetSession(id)
	if !ok {
		return nil, errors.New("session not found")
	}

	var md strings.Builder
	md.WriteString(fmt.Sprintf("# %s\n\n", session.Title))
	md.WriteString(fmt.Sprintf("**Agent:** %s  \n", session.AgentName))
	md.WriteString(fmt.Sprintf("**Model:** %s  \n", session.Model))
	md.WriteString(fmt.Sprintf("**Created:** %s  \n", session.CreatedAt.Format(time.RFC3339)))
	md.WriteString(fmt.Sprintf("**Updated:** %s\n\n", session.UpdatedAt.Format(time.RFC3339)))
	md.WriteString("---\n\n")

	for _, msg := range session.Messages {
		var roleLabel string
		switch msg.Role {
		case "user":
			roleLabel = "**You**"
		case "assistant":
			roleLabel = "**Assistant**"
		case "system":
			roleLabel = "*System*"
		case "tool":
			roleLabel = "*Tool*"
		default:
			roleLabel = msg.Role
		}
		md.WriteString(fmt.Sprintf("%s:\n\n%s\n\n", roleLabel, msg.Content))
		if msg.TokenUsage != nil {
			md.WriteString(fmt.Sprintf("*Tokens: %d prompt, %d completion, %d total*\n\n",
				msg.TokenUsage.PromptTokens,
				msg.TokenUsage.CompletionTokens,
				msg.TokenUsage.TotalTokens))
		}
		md.WriteString("---\n\n")
	}

	return []byte(md.String()), nil
}

// loadAll loads all sessions from disk.
func (s *SessionStore) loadAll() error {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !isSessionFile(entry.Name()) {
			continue
		}

		path := filepath.Join(s.Dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // Skip corrupted files
		}

		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue // Skip corrupted files
		}

		s.Sessions[session.ID] = &session
	}

	return nil
}

// save persists a session to disk.
func (s *SessionStore) save(session *Session) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Write atomically: write to a temp file in the same directory, then rename.
	// os.Rename is atomic on POSIX and Windows (same volume), so a crash mid-write
	// leaves the previous session file intact rather than a truncated/partial one.
	path := s.sessionPath(session.ID)
	tmp, err := os.CreateTemp(s.Dir, ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("failed to write session: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		cleanup()
		return fmt.Errorf("failed to set session file perms: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to commit session file: %w", err)
	}

	return nil
}

// sessionPath returns the file path for a session ID.
func (s *SessionStore) sessionPath(id string) string {
	return filepath.Join(s.Dir, "session_"+id+".json")
}

// isSessionFile checks if a filename matches the session file pattern.
func isSessionFile(name string) bool {
	return filepath.Ext(name) == ".json"
}

// generateSessionID creates a new unique session ID.
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// containsFold is a case-insensitive string contains check.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
