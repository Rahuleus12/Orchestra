package observability

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// SensitiveKeyPatterns lists log attribute keys that should have their values
// redacted. Matching is case-insensitive and matches substrings.
var SensitiveKeyPatterns = []string{
	"api_key",
	"apikey",
	"secret",
	"password",
	"passwd",
	"token",
	"authorization",
	"credential",
	"private_key",
	"access_key",
}

// RedactedValue is the replacement value for sensitive data in logs.
const RedactedValue = "[REDACTED]"

// SanitizeAttrs redacts values for any attributes whose keys match known
// sensitive patterns. The original slice is not modified; a new slice is
// returned with sensitive values replaced.
func SanitizeAttrs(attrs []any) []any {
	result := make([]any, len(attrs))
	copy(result, attrs)

	for i := 0; i < len(result)-1; i += 2 {
		key, ok := result[i].(string)
		if !ok {
			continue
		}
		if IsSensitiveKey(key) {
			result[i+1] = RedactedValue
		}
	}

	return result
}

// IsSensitiveKey returns true if the key matches any known sensitive pattern.
// Matching is case-insensitive and checks for substring matches.
func IsSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, pattern := range SensitiveKeyPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// SanitizeLogger returns a logger that automatically redacts sensitive values
// from log attributes before they are written. This wraps the given handler
// with a sanitizing middleware.
func SanitizeLogger(logger *slog.Logger) *slog.Logger {
	return slog.New(&sanitizingHandler{inner: logger.Handler()})
}

// sanitizingHandler wraps a slog.Handler to redact sensitive values.
type sanitizingHandler struct {
	inner slog.Handler
}

func (h *sanitizingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *sanitizingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Build sanitized attributes
	var sanitizedAttrs []slog.Attr
	r.Attrs(func(attr slog.Attr) bool {
		if isSensitiveAttr(attr) {
			sanitizedAttrs = append(sanitizedAttrs, slog.String(attr.Key, RedactedValue))
		} else {
			sanitizedAttrs = append(sanitizedAttrs, attr)
		}
		return true
	})

	// Add sanitized attributes to the handler and build a new record
	innerHandler := h.inner
	if len(sanitizedAttrs) > 0 {
		innerHandler = innerHandler.WithAttrs(sanitizedAttrs)
	}

	newR := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	if err := innerHandler.Handle(ctx, newR); err != nil {
		return fmt.Errorf("sanitizing handler: %w", err)
	}
	return nil
}

func (h *sanitizingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		if isSensitiveAttr(attr) {
			sanitized[i] = slog.String(attr.Key, RedactedValue)
		} else {
			sanitized[i] = attr
		}
	}
	return &sanitizingHandler{inner: h.inner.WithAttrs(sanitized)}
}

func (h *sanitizingHandler) WithGroup(name string) slog.Handler {
	return &sanitizingHandler{inner: h.inner.WithGroup(name)}
}

func isSensitiveAttr(attr slog.Attr) bool {
	return IsSensitiveKey(attr.Key)
}
