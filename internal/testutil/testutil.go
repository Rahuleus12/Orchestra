// Package testutil provides CI-friendly testing utilities for Orchestra.
//
// This package contains helpers for:
//   - CI environment detection
//   - Assertions with GitHub Actions annotations
//   - Test setup and teardown
//   - Temporary directory management
//   - Environment variable manipulation
//   - Test logging optimized for CI
//
// Usage:
//
//	func TestSomething(t *testing.T) {
//	    defer testutil.SetupTest(t)()
//
//	    testutil.AssertEqual(t, expected, actual, "values should match")
//
//	    if testutil.IsCI() {
//	        t.Log("Running in CI mode")
//	    }
//	}
package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// CI Detection
// ---------------------------------------------------------------------------

// IsCI returns true if running in a CI environment.
func IsCI() bool {
	return os.Getenv("CI") != "" ||
		os.Getenv("GITHUB_ACTIONS") != "" ||
		os.Getenv("JENKINS_URL") != "" ||
		os.Getenv("GITLAB_CI") != "" ||
		os.Getenv("CIRCLECI") != "" ||
		os.Getenv("TRAVIS") != "" ||
		os.Getenv("BUILDKITE") != "" ||
		os.Getenv("AZURE_PIPELINES") != "" ||
		os.Getenv("TF_BUILD") != ""
}

// IsGitHubActions returns true if running in GitHub Actions.
func IsGitHubActions() bool {
	return os.Getenv("GITHUB_ACTIONS") != ""
}

// IsParallel returns true if the test is running in parallel mode.
func IsParallel(t *testing.T) bool {
	// Go's testing package doesn't expose this directly,
	// but we can check if -test.parallel flag was set
	return os.Getenv("GO_TEST_PARALLEL") != ""
}

// ---------------------------------------------------------------------------
// GitHub Actions Annotations
// ---------------------------------------------------------------------------

// Annotation levels for GitHub Actions.
const (
	AnnotationNotice = "::notice::"
	AnnotationWarning = "::warning::"
	AnnotationError   = "::error::"
)

// Annotate posts a GitHub Actions annotation if running in GitHub Actions.
// The annotation is formatted as: ::LEVEL::message
func Annotate(t *testing.T, level, message string) {
	t.Helper()
	if IsGitHubActions() {
		t.Logf("%s%s", level, message)
	}
}

// AnnotateFile posts a file-specific GitHub Actions annotation.
// Format: ::LEVEL::file=path,line=1,col=1::message
func AnnotateFile(t *testing.T, level, file string, line int, message string) {
	t.Helper()
	if IsGitHubActions() {
		t.Logf("%sfile=%s,line=%d::%s", level, file, line, message)
	}
}

// ---------------------------------------------------------------------------
// Assertions
// ---------------------------------------------------------------------------

// AssertEqual asserts that expected equals actual.
// On failure, it logs both values and posts a GitHub Actions error annotation in CI.
func AssertEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if !assertEqual(expected, actual) {
		msg := formatMessage("AssertEqual", msgAndArgs...)
		diff := formatDiff(expected, actual)
		t.Errorf("%s\n%s", msg, diff)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: %s", msg, diff))
	}
}

// AssertNotEqual asserts that expected does not equal actual.
func AssertNotEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if assertEqual(expected, actual) {
		msg := formatMessage("AssertNotEqual", msgAndArgs...)
		t.Errorf("%s: values are equal when they shouldn't be: %v", msg, expected)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: unexpected equality: %v", msg, expected))
	}
}

// AssertTrue asserts that the condition is true.
func AssertTrue(t *testing.T, condition bool, msgAndArgs ...interface{}) {
	t.Helper()
	if !condition {
		msg := formatMessage("AssertTrue", msgAndArgs...)
		t.Errorf("%s: expected true, got false", msg)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: expected true", msg))
	}
}

// AssertFalse asserts that the condition is false.
func AssertFalse(t *testing.T, condition bool, msgAndArgs ...interface{}) {
	t.Helper()
	if condition {
		msg := formatMessage("AssertFalse", msgAndArgs...)
		t.Errorf("%s: expected false, got true", msg)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: expected false", msg))
	}
}

// AssertNil asserts that the value is nil.
func AssertNil(t *testing.T, value interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if value != nil {
		msg := formatMessage("AssertNil", msgAndArgs...)
		t.Errorf("%s: expected nil, got %v", msg, value)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: expected nil, got %T", msg, value))
	}
}

// AssertNotNil asserts that the value is not nil.
func AssertNotNil(t *testing.T, value interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if value == nil {
		msg := formatMessage("AssertNotNil", msgAndArgs...)
		t.Errorf("%s: expected non-nil", msg)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: expected non-nil", msg))
	}
}

// AssertNoError asserts that err is nil.
func AssertNoError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	if err != nil {
		msg := formatMessage("AssertNoError", msgAndArgs...)
		t.Errorf("%s: unexpected error: %v", msg, err)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: %v", msg, err))
	}
}

// AssertError asserts that err is not nil.
func AssertError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	if err == nil {
		msg := formatMessage("AssertError", msgAndArgs...)
		t.Errorf("%s: expected error, got nil", msg)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: expected error", msg))
	}
}

// AssertErrorContains asserts that err contains the expected substring.
func AssertErrorContains(t *testing.T, err error, substring string, msgAndArgs ...interface{}) {
	t.Helper()
	if err == nil {
		msg := formatMessage("AssertErrorContains", msgAndArgs...)
		t.Errorf("%s: expected error containing %q, got nil", msg, substring)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: expected error containing %q", msg, substring))
		return
	}
	if !strings.Contains(err.Error(), substring) {
		msg := formatMessage("AssertErrorContains", msgAndArgs...)
		t.Errorf("%s: error %q does not contain %q", msg, err.Error(), substring)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: error %q does not contain %q", msg, err.Error(), substring))
	}
}

// AssertZero asserts that the value is the zero value for its type.
func AssertZero(t *testing.T, value interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if !isZero(value) {
		msg := formatMessage("AssertZero", msgAndArgs...)
		t.Errorf("%s: expected zero value, got %v", msg, value)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: expected zero, got %v", msg, value))
	}
}

// AssertNotZero asserts that the value is not the zero value for its type.
func AssertNotZero(t *testing.T, value interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if isZero(value) {
		msg := formatMessage("AssertNotZero", msgAndArgs...)
		t.Errorf("%s: expected non-zero value", msg)
		Annotate(t, AnnotationError, fmt.Sprintf("%s: expected non-zero", msg))
	}
}

// AssertPanics asserts that the function panics.
func AssertPanics(t *testing.T, fn func(), msgAndArgs ...interface{}) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			msg := formatMessage("AssertPanics", msgAndArgs...)
			t.Errorf("%s: expected panic", msg)
			Annotate(t, AnnotationError, fmt.Sprintf("%s: expected panic", msg))
		}
	}()
	fn()
}

// AssertNotPanics asserts that the function does not panic.
func AssertNotPanics(t *testing.T, fn func(), msgAndArgs ...interface{}) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			msg := formatMessage("AssertNotPanics", msgAndArgs...)
			t.Errorf("%s: unexpected panic: %v", msg, r)
			Annotate(t, AnnotationError, fmt.Sprintf("%s: unexpected panic: %v", msg, r))
		}
	}()
	fn()
}

// ---------------------------------------------------------------------------
// Test Setup and Teardown
// ---------------------------------------------------------------------------

// SetupTest returns a cleanup function that should be deferred.
// It logs test start/end times and handles panic recovery.
//
// Usage:
//
//	func TestSomething(t *testing.T) {
//	    defer SetupTest(t)()
//	    // test code
//	}
func SetupTest(t *testing.T) func() {
	t.Helper()
	start := time.Now()
	testName := t.Name()

	if IsCI() {
		Annotate(t, AnnotationNotice, fmt.Sprintf("Starting test: %s", testName))
	}

	return func() {
		duration := time.Since(start)
		if IsCI() && duration > 5*time.Second {
			Annotate(t, AnnotationNotice, fmt.Sprintf("Test %s completed in %v", testName, duration))
		}
		if t.Failed() {
			if IsCI() {
				Annotate(t, AnnotationError, fmt.Sprintf("Test %s failed after %v", testName, duration))
			}
		}
	}
}

// SetupSubtest returns a cleanup function for a subtest.
func SetupSubtest(t *testing.T) func() {
	t.Helper()
	return SetupTest(t)
}

// ---------------------------------------------------------------------------
// Temporary Directory Management
// ---------------------------------------------------------------------------

// TempDir creates a temporary directory for testing and returns a cleanup function.
// The directory path is also returned for convenience.
//
// Usage:
//
//	func TestWithFiles(t *testing.T) {
//	    dir, cleanup := TempDir(t)
//	    defer cleanup()
//	    // use dir
//	}
func TempDir(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "orchestra-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	return dir, func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("failed to remove temp dir %s: %v", dir, err)
		}
	}
}

// TempFile creates a temporary file with the given content and returns
// the file path and a cleanup function.
func TempFile(t *testing.T, content string) (string, func()) {
	t.Helper()
	dir, dirCleanup := TempDir(t)
	path := filepath.Join(dir, "testfile")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		dirCleanup()
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path, func() {
		dirCleanup()
	}
}

// TempFileWithExt creates a temporary file with the specified extension.
func TempFileWithExt(t *testing.T, ext, content string) (string, func()) {
	t.Helper()
	dir, dirCleanup := TempDir(t)
	path := filepath.Join(dir, "testfile"+ext)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		dirCleanup()
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path, func() {
		dirCleanup()
	}
}

// ---------------------------------------------------------------------------
// Environment Variable Helpers
// ---------------------------------------------------------------------------

// Setenv sets an environment variable and returns a cleanup function
// that restores the original value.
//
// Usage:
//
//	func TestWithEnv(t *testing.T) {
//	    cleanup := Setenv(t, "MY_VAR", "value")
//	    defer cleanup()
//	    // test code
//	}
func Setenv(t *testing.T, key, value string) func() {
	t.Helper()
	original, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("failed to set env %s: %v", key, err)
	}
	return func() {
		if existed {
			if err := os.Setenv(key, original); err != nil {
				t.Errorf("failed to restore env %s: %v", key, err)
			}
		} else {
			if err := os.Unsetenv(key); err != nil {
				t.Errorf("failed to unset env %s: %v", key, err)
			}
		}
	}
}

// Unsetenv unsets an environment variable and returns a cleanup function.
func Unsetenv(t *testing.T, key string) func() {
	t.Helper()
	original, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("failed to unset env %s: %v", key, err)
	}
	return func() {
		if existed {
			if err := os.Setenv(key, original); err != nil {
				t.Errorf("failed to restore env %s: %v", key, err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test Skipping
// ---------------------------------------------------------------------------

// SkipIfCI skips the test if running in CI.
func SkipIfCI(t *testing.T, reason string) {
	t.Helper()
	if IsCI() {
		Annotate(t, AnnotationNotice, fmt.Sprintf("Skipping %s (CI): %s", t.Name(), reason))
		t.Skipf("skipping in CI: %s", reason)
	}
}

// SkipIfNotCI skips the test if not running in CI.
func SkipIfNotCI(t *testing.T, reason string) {
	t.Helper()
	if !IsCI() {
		t.Skipf("skipping outside CI: %s", reason)
	}
}

// SkipIfShort skips the test if -short flag is set.
func SkipIfShort(t *testing.T, reason string) {
	t.Helper()
	if testing.Short() {
		t.Skipf("skipping with -short: %s", reason)
	}
}

// SkipIfMissingEnv skips the test if the environment variable is not set.
func SkipIfMissingEnv(t *testing.T, key string) {
	t.Helper()
	if os.Getenv(key) == "" {
		Annotate(t, AnnotationNotice, fmt.Sprintf("Skipping %s: %s not set", t.Name(), key))
		t.Skipf("skipping: %s not set", key)
	}
}

// SkipOnPlatform skips the test on specific platforms.
func SkipOnPlatform(t *testing.T, goos ...string) {
	t.Helper()
	current := runtime.GOOS
	for _, os := range goos {
		if current == os {
			Annotate(t, AnnotationNotice, fmt.Sprintf("Skipping %s on %s", t.Name(), current))
			t.Skipf("skipping on %s", current)
			return
		}
	}
}

// SkipUnlessPlatform skips the test unless running on one of the specified platforms.
func SkipUnlessPlatform(t *testing.T, goos ...string) {
	t.Helper()
	current := runtime.GOOS
	for _, os := range goos {
		if current == os {
			return
		}
	}
	Annotate(t, AnnotationNotice, fmt.Sprintf("Skipping %s: not on %v", t.Name(), goos))
	t.Skipf("skipping: not on %v", goos)
}

// SkipIfRaceDetector skips the test if the race detector is enabled.
func SkipIfRaceDetector(t *testing.T, reason string) {
	t.Helper()
	// The race detector is enabled when -race flag is passed
	// Unfortunately there's no direct way to detect this in Go,
	// but we can check for the runtime.raceenabled internal variable indirectly
	if os.Getenv("GO_TEST_RACE") != "" {
		t.Skipf("skipping with race detector: %s", reason)
	}
}

// ---------------------------------------------------------------------------
// Test Logging
// ---------------------------------------------------------------------------

// LogGroup starts a GitHub Actions log group.
// The returned function ends the group.
//
// Usage:
//
//	endGroup := LogGroup(t, "Setup Phase")
//	// setup code
//	endGroup()
func LogGroup(t *testing.T, title string) func() {
	t.Helper()
	if IsGitHubActions() {
		t.Logf("::group::%s", title)
		return func() {
			t.Log("::endgroup::")
		}
	}
	t.Logf("=== %s ===", title)
	return func() {
		t.Logf("=== End %s ===", title)
	}
}

// LogJSON logs a value as formatted JSON.
func LogJSON(t *testing.T, v interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Errorf("failed to marshal to JSON: %v", err)
		return
	}
	t.Log(string(data))
}

// LogDuration logs the duration of a function execution.
func LogDuration(t *testing.T, label string, fn func()) {
	t.Helper()
	start := time.Now()
	fn()
	duration := time.Since(start)
	t.Logf("%s: %v", label, duration)
	if IsCI() && duration > time.Second {
		Annotate(t, AnnotationNotice, fmt.Sprintf("%s took %v", label, duration))
	}
}

// ---------------------------------------------------------------------------
// Timing Utilities
// ---------------------------------------------------------------------------

// Measure returns a function that measures elapsed time when called.
//
// Usage:
//
//	measure := Measure(t)
//	// do work
//	elapsed := measure()
//	t.Logf("Took %v", elapsed)
func Measure(t *testing.T) func() time.Duration {
	t.Helper()
	start := time.Now()
	return func() time.Duration {
		return time.Since(start)
	}
}

// Timeout returns a channel that fires after the specified duration.
// Useful for testing timeouts.
func Timeout(d time.Duration) <-chan time.Duration {
	ch := make(chan time.Duration, 1)
	go func() {
		time.Sleep(d)
		ch <- d
	}()
	return ch
}

// ---------------------------------------------------------------------------
// Retry Helper
// ---------------------------------------------------------------------------

// Retry executes fn up to n times, returning the last error.
// It waits between retries using exponential backoff.
func Retry(t *testing.T, n int, fn func() error) error {
	t.Helper()
	var err error
	for i := 0; i < n; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if i < n-1 {
			backoff := time.Duration(1<<uint(i)) * time.Millisecond
			if backoff > time.Second {
				backoff = time.Second
			}
			t.Logf("Retry %d/%d after %v: %v", i+1, n, backoff, err)
			time.Sleep(backoff)
		}
	}
	return err
}

// ---------------------------------------------------------------------------
// Internal Helpers
// ---------------------------------------------------------------------------

func assertEqual(expected, actual interface{}) bool {
	return fmt.Sprintf("%v", expected) == fmt.Sprintf("%v", actual)
}

func formatMessage(prefix string, msgAndArgs ...interface{}) string {
	if len(msgAndArgs) == 0 {
		return prefix
	}
	if len(msgAndArgs) == 1 {
		if msg, ok := msgAndArgs[0].(string); ok {
			return fmt.Sprintf("%s: %s", prefix, msg)
		}
		return fmt.Sprintf("%s: %v", prefix, msgAndArgs[0])
	}
	return fmt.Sprintf("%s: %s", prefix, fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...))
}

func formatDiff(expected, actual interface{}) string {
	return fmt.Sprintf("expected: %v\nactual:   %v", expected, actual)
}

func isZero(value interface{}) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case int:
		return v == 0
	case int8:
		return v == 0
	case int16:
		return v == 0
	case int32:
		return v == 0
	case int64:
		return v == 0
	case uint:
		return v == 0
	case uint8:
		return v == 0
	case uint16:
		return v == 0
	case uint32:
		return v == 0
	case uint64:
		return v == 0
	case float32:
		return v == 0
	case float64:
		return v == 0
	case string:
		return v == ""
	case bool:
		return !v
	case []byte:
		return len(v) == 0
	default:
		return false
	}
}
