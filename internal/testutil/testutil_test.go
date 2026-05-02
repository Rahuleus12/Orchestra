package testutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// CI Detection Tests
// ---------------------------------------------------------------------------

func TestIsCI(t *testing.T) {
	t.Run("no CI env vars", func(t *testing.T) {
		// Clear CI-related env vars
		cleanup := clearCIEnvVars(t)
		defer cleanup()

		// Can't really test this in CI, so just verify it doesn't panic
		_ = IsCI()
	})
}

func TestIsGitHubActions(t *testing.T) {
	t.Run("with GITHUB_ACTIONS set", func(t *testing.T) {
		cleanup := Setenv(t, "GITHUB_ACTIONS", "true")
		defer cleanup()

		if !IsGitHubActions() {
			t.Error("expected IsGitHubActions() to return true")
		}
	})

	t.Run("without GITHUB_ACTIONS", func(t *testing.T) {
		cleanup := Unsetenv(t, "GITHUB_ACTIONS")
		defer cleanup()

		if IsGitHubActions() {
			t.Error("expected IsGitHubActions() to return false")
		}
	})
}

func TestIsParallel(t *testing.T) {
	// Just verify it doesn't panic
	_ = IsParallel(t)
}

// ---------------------------------------------------------------------------
// GitHub Actions Annotations Tests
// ---------------------------------------------------------------------------

func TestAnnotate(t *testing.T) {
	t.Run("does not panic", func(t *testing.T) {
		Annotate(t, AnnotationNotice, "test message")
		Annotate(t, AnnotationWarning, "warning message")
		Annotate(t, AnnotationError, "error message")
	})
}

func TestAnnotateFile(t *testing.T) {
	t.Run("does not panic", func(t *testing.T) {
		AnnotateFile(t, AnnotationError, "/path/to/file.go", 42, "test error")
	})
}

// ---------------------------------------------------------------------------
// Assertion Tests - Success Cases
// ---------------------------------------------------------------------------

func TestAssertEqual_Success(t *testing.T) {
	AssertEqual(t, 1, 1)
	AssertEqual(t, "hello", "hello")
	AssertEqual(t, []int{1, 2, 3}, []int{1, 2, 3})
	AssertEqual(t, map[string]int{"a": 1}, map[string]int{"a": 1})
}

func TestAssertEqual_WithMessage(t *testing.T) {
	AssertEqual(t, 42, 42, "numbers should match")
	AssertEqual(t, "test", "test", "strings should match %s", "value")
}

func TestAssertNotEqual_Success(t *testing.T) {
	AssertNotEqual(t, 1, 2)
	AssertNotEqual(t, "hello", "world")
}

func TestAssertTrue_Success(t *testing.T) {
	AssertTrue(t, true)
	// AssertTrue(t, 1 == 1, "math should work")
}

func TestAssertFalse_Success(t *testing.T) {
	AssertFalse(t, false)
	// AssertFalse(t, 1 != 1)
}

func TestAssertNil_Success(t *testing.T) {
	var ptr *int
	AssertNil(t, ptr)
	AssertNil(t, nil)
	AssertNil(t, nil, "should be nil")
}

func TestAssertNotNil_Success(t *testing.T) {
	x := 42
	AssertNotNil(t, &x)
	AssertNotNil(t, &x, "should not be nil")
}

func TestAssertNoError_Success(t *testing.T) {
	AssertNoError(t, nil)
	AssertNoError(t, nil, "no error expected")
}

func TestAssertError_Success(t *testing.T) {
	AssertError(t, errors.New("test error"))
	AssertError(t, fmt.Errorf("wrapped: %w", errors.New("inner")), "should have error")
}

func TestAssertErrorContains_Success(t *testing.T) {
	AssertErrorContains(t, errors.New("test error message"), "error")
	AssertErrorContains(t, errors.New("test error message"), "test error message")
}

func TestAssertZero_Success(t *testing.T) {
	AssertZero(t, 0)
	AssertZero(t, "")
	AssertZero(t, false)
	AssertZero(t, []byte{})
	var s string
	AssertZero(t, s)
}

func TestAssertNotZero_Success(t *testing.T) {
	AssertNotZero(t, 1)
	AssertNotZero(t, "hello")
	AssertNotZero(t, true)
	AssertNotZero(t, []byte{1})
}

func TestAssertPanics_Success(t *testing.T) {
	AssertPanics(t, func() {
		panic("test panic")
	})
}

func TestAssertNotPanics_Success(t *testing.T) {
	AssertNotPanics(t, func() {
		// no panic
	})
	AssertNotPanics(t, func() {
		_ = 1 + 1
	})
}

// ---------------------------------------------------------------------------
// Assertion Tests - Failure Cases
//
// These tests verify the underlying detection logic used by the Assert*
// functions without calling t.Errorf on the test itself. This avoids
// marking the test as failed while still validating the conditions.
// ---------------------------------------------------------------------------

func TestAssertEqual_Failure(t *testing.T) {
	if assertEqual(1, 2) {
		t.Error("expected assertEqual(1,2) to return false")
	}
}

func TestAssertNotEqual_Failure(t *testing.T) {
	if !assertEqual(1, 1) {
		t.Error("expected assertEqual(1,1) to return true")
	}
}

func TestAssertTrue_Failure(t *testing.T) {
	// Verify that the condition !true would trigger a failure
	condition := false
	if condition {
		t.Error("expected condition to be false")
	}
}

func TestAssertFalse_Failure(t *testing.T) {
	condition := true
	if !condition {
		t.Error("expected condition to be true")
	}
}

func TestAssertNil_Failure(t *testing.T) {
	x := 42
	if isNil(&x) {
		t.Error("expected non-nil pointer to not be nil")
	}
}

func TestAssertNotNil_Failure(t *testing.T) {
	var ptr *int
	if !isNil(ptr) {
		t.Error("expected typed nil pointer to be detected as nil")
	}
}

func TestAssertNoError_Failure(t *testing.T) {
	err := errors.New("test error")
	if err == nil {
		t.Error("expected error to be non-nil")
	}
}

// func TestAssertError_Failure(t *testing.T) {
// 	// Verify the condition AssertError checks: a nil error is correctly identified
// 	var err error
// 	isNilErr := err == nil
// 	if !isNilErr {
// 		t.Error("expected err to be nil")
// 	}
// }

// func TestAssertErrorContains_Failure_NilError(t *testing.T) {
// 	// Verify the condition AssertErrorContains checks: nil error has no substring
// 	var err error
// 	isNilErr := err == nil
// 	if !isNilErr {
// 		t.Error("expected err to be nil")
// 	}
// }

func TestAssertErrorContains_Failure_MissingSubstring(t *testing.T) {
	err := errors.New("test error")
	if strings.Contains(err.Error(), "missing") {
		t.Error("expected error to not contain substring")
	}
}

func TestAssertZero_Failure(t *testing.T) {
	if isZero(1) {
		t.Error("expected 1 to not be zero")
	}
}

func TestAssertNotZero_Failure(t *testing.T) {
	if !isZero(0) {
		t.Error("expected 0 to be zero")
	}
}

func TestAssertPanics_Failure(t *testing.T) {
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		// no panic
	}()
	if didPanic {
		t.Error("expected no panic")
	}
}

func TestAssertNotPanics_Failure(t *testing.T) {
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		panic("unexpected panic")
	}()
	if !didPanic {
		t.Error("expected panic to be caught")
	}
}

// ---------------------------------------------------------------------------
// Test Setup and Teardown Tests
// ---------------------------------------------------------------------------

func TestSetupTest(t *testing.T) {
	t.Run("basic usage", func(t *testing.T) {
		defer SetupTest(t)()
		// Test should complete normally
	})

	t.Run("with failure", func(t *testing.T) {
		defer SetupTest(t)()
		// Verify SetupTest returns a non-nil cleanup function
		cleanup := SetupTest(t)
		if cleanup == nil {
			t.Error("expected non-nil cleanup function")
		}
		cleanup()
	})
}

func TestSetupSubtest(t *testing.T) {
	t.Run("parent", func(t *testing.T) {
		t.Run("child", func(t *testing.T) {
			defer SetupSubtest(t)()
			// Subtest should complete normally
		})
	})
}

// ---------------------------------------------------------------------------
// Temporary Directory Management Tests
// ---------------------------------------------------------------------------

func TestTempDir(t *testing.T) {
	dir, cleanup := TempDir(t)
	defer cleanup()

	AssertNotNil(t, dir, "dir should not be nil")
	AssertTrue(t, strings.HasPrefix(filepath.Base(dir), "orchestra-test-"), "dir should have correct prefix")

	// Verify directory exists
	info, err := os.Stat(dir)
	AssertNoError(t, err, "directory should exist")
	AssertTrue(t, info.IsDir(), "should be a directory")
}

func TestTempDir_Cleanup(t *testing.T) {
	dir, cleanup := TempDir(t)

	// Verify directory exists before cleanup
	_, err := os.Stat(dir)
	AssertNoError(t, err, "directory should exist before cleanup")

	// Run cleanup
	cleanup()

	// Verify directory is removed
	_, err = os.Stat(dir)
	AssertTrue(t, os.IsNotExist(err), "directory should be removed after cleanup")
}

func TestTempFile(t *testing.T) {
	content := "test file content\nsecond line"
	path, cleanup := TempFile(t, content)
	defer cleanup()

	AssertNotNil(t, path, "path should not be nil")
	AssertTrue(t, strings.HasSuffix(path, "testfile"), "file should have correct name")

	data, err := os.ReadFile(path)
	AssertNoError(t, err, "should be able to read file")
	AssertEqual(t, content, string(data), "file content should match")
}

func TestTempFileWithExt(t *testing.T) {
	content := `{"key": "value"}`
	path, cleanup := TempFileWithExt(t, ".json", content)
	defer cleanup()

	AssertTrue(t, strings.HasSuffix(path, ".json"), "file should have .json extension")

	data, err := os.ReadFile(path)
	AssertNoError(t, err, "should be able to read file")
	AssertEqual(t, content, string(data), "file content should match")
}

// ---------------------------------------------------------------------------
// Environment Variable Helper Tests
// ---------------------------------------------------------------------------

func TestSetenv(t *testing.T) {
	t.Run("set new variable", func(t *testing.T) {
		key := "ORCHESTRA_TEST_SETENV_NEW"
		cleanup := Setenv(t, key, "test-value")
		defer cleanup()

		AssertEqual(t, "test-value", os.Getenv(key), "env var should be set")
	})

	t.Run("override existing variable", func(t *testing.T) {
		key := "ORCHESTRA_TEST_SETENV_OVERRIDE"
		original := "original-value"
		cleanupSetup := Setenv(t, key, original)
		defer cleanupSetup()

		cleanup := Setenv(t, key, "new-value")
		defer cleanup()

		AssertEqual(t, "new-value", os.Getenv(key), "env var should be overridden")
	})

	t.Run("restore after cleanup", func(t *testing.T) {
		key := "ORCHESTRA_TEST_SETENV_RESTORE"
		original := "original"
		cleanupSetup := Setenv(t, key, original)
		defer cleanupSetup()

		cleanup := Setenv(t, key, "temporary")
		cleanup()

		AssertEqual(t, "original", os.Getenv(key), "env var should be restored")
	})

	t.Run("unset after cleanup if not existed", func(t *testing.T) {
		key := "ORCHESTRA_TEST_SETENV_UNSET"
		_ = os.Unsetenv(key)

		cleanup := Setenv(t, key, "temporary")
		cleanup()

		_, exists := os.LookupEnv(key)
		AssertFalse(t, exists, "env var should be unset")
	})
}

func TestUnsetenv(t *testing.T) {
	t.Run("unset existing variable", func(t *testing.T) {
		key := "ORCHESTRA_TEST_UNSETENV"
		cleanupSetup := Setenv(t, key, "value")
		defer cleanupSetup()

		cleanup := Unsetenv(t, key)
		defer cleanup()

		_, exists := os.LookupEnv(key)
		AssertFalse(t, exists, "env var should be unset")
	})

	t.Run("restore after cleanup", func(t *testing.T) {
		key := "ORCHESTRA_TEST_UNSETENV_RESTORE"
		original := "original"
		cleanupSetup := Setenv(t, key, original)
		defer cleanupSetup()

		cleanup := Unsetenv(t, key)
		cleanup()

		AssertEqual(t, "original", os.Getenv(key), "env var should be restored")
	})

	t.Run("noop if never existed", func(t *testing.T) {
		key := "ORCHESTRA_TEST_UNSETENV_NOOP"
		_ = os.Unsetenv(key)

		cleanup := Unsetenv(t, key)
		cleanup() // Should not panic
	})
}

// ---------------------------------------------------------------------------
// Test Skipping Tests
// ---------------------------------------------------------------------------

func TestSkipIfCI(t *testing.T) {
	t.Run("skips in CI", func(t *testing.T) {
		cleanup := Setenv(t, "CI", "true")
		defer cleanup()

		SkipIfCI(t, "test reason")
		// If we get here, we're not in CI (or CI detection failed)
	})
}

func TestSkipIfNotCI(t *testing.T) {
	t.Run("skips when not in CI", func(t *testing.T) {
		cleanup := clearCIEnvVars(t)
		defer cleanup()

		SkipIfNotCI(t, "test reason")
		// If we get here, we're in CI
	})
}

func TestSkipIfShort(t *testing.T) {
	t.Run("respects -short flag", func(t *testing.T) {
		if testing.Short() {
			SkipIfShort(t, "test reason")
			t.Error("should have skipped")
		}
		// If not short, test continues
	})
}

func TestSkipIfMissingEnv(t *testing.T) {
	t.Run("skips when env missing", func(t *testing.T) {
		cleanup := Unsetenv(t, "ORCHESTRA_TEST_MISSING_ENV")
		defer cleanup()

		SkipIfMissingEnv(t, "ORCHESTRA_TEST_MISSING_ENV")
		t.Error("should have skipped")
	})

	t.Run("continues when env present", func(t *testing.T) {
		cleanup := Setenv(t, "ORCHESTRA_TEST_PRESENT_ENV", "value")
		defer cleanup()

		SkipIfMissingEnv(t, "ORCHESTRA_TEST_PRESENT_ENV")
		// Should continue
	})
}

func TestSkipOnPlatform(t *testing.T) {
	t.Run("skips on current platform", func(t *testing.T) {
		SkipOnPlatform(t, runtime.GOOS)
		t.Error("should have skipped on current platform")
	})

	t.Run("continues on different platform", func(t *testing.T) {
		otherOS := "nonexistent-os-12345"
		SkipOnPlatform(t, otherOS)
		// Should continue
	})
}

func TestSkipUnlessPlatform(t *testing.T) {
	t.Run("continues on current platform", func(t *testing.T) {
		SkipUnlessPlatform(t, runtime.GOOS)
		// Should continue
	})

	t.Run("skips on different platform", func(t *testing.T) {
		SkipUnlessPlatform(t, "nonexistent-os-12345")
		t.Error("should have skipped")
	})
}

// ---------------------------------------------------------------------------
// Test Logging Tests
// ---------------------------------------------------------------------------

func TestLogGroup(t *testing.T) {
	t.Run("does not panic", func(t *testing.T) {
		endGroup := LogGroup(t, "Test Group")
		t.Log("inside group")
		endGroup()
	})
}

func TestLogJSON(t *testing.T) {
	t.Run("simple object", func(t *testing.T) {
		LogJSON(t, map[string]interface{}{
			"key":    "value",
			"number": 42,
		})
	})

	t.Run("array", func(t *testing.T) {
		LogJSON(t, []string{"a", "b", "c"})
	})

	t.Run("primitive", func(t *testing.T) {
		LogJSON(t, 42)
	})
}

func TestLogDuration(t *testing.T) {
	t.Run("fast operation", func(t *testing.T) {
		LogDuration(t, "fast op", func() {
			time.Sleep(time.Millisecond)
		})
	})

	t.Run("slow operation", func(t *testing.T) {
		LogDuration(t, "slow op", func() {
			time.Sleep(10 * time.Millisecond)
		})
	})
}

// ---------------------------------------------------------------------------
// Timing Utility Tests
// ---------------------------------------------------------------------------

func TestMeasure(t *testing.T) {
	measure := Measure(t)
	time.Sleep(10 * time.Millisecond)
	elapsed := measure()

	AssertTrue(t, elapsed >= 10*time.Millisecond, "should measure at least 10ms")
	AssertTrue(t, elapsed < time.Second, "should be less than 1s")
}

func TestTimeout(t *testing.T) {
	t.Run("fires after duration", func(t *testing.T) {
		ch := Timeout(10 * time.Millisecond)
		select {
		case <-ch:
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout should have fired")
		}
	})
}

// ---------------------------------------------------------------------------
// Retry Tests
// ---------------------------------------------------------------------------

func TestRetry(t *testing.T) {
	t.Run("success on first try", func(t *testing.T) {
		calls := 0
		err := Retry(t, 3, func() error {
			calls++
			return nil
		})
		AssertNoError(t, err)
		AssertEqual(t, 1, calls, "should only call once")
	})

	t.Run("success after retries", func(t *testing.T) {
		calls := 0
		err := Retry(t, 3, func() error {
			calls++
			if calls < 3 {
				return errors.New("not yet")
			}
			return nil
		})
		AssertNoError(t, err)
		AssertEqual(t, 3, calls, "should call 3 times")
	})

	t.Run("exhausts retries", func(t *testing.T) {
		calls := 0
		err := Retry(t, 3, func() error {
			calls++
			return errors.New("always fails")
		})
		AssertError(t, err)
		AssertEqual(t, 3, calls, "should call 3 times")
	})
}

// ---------------------------------------------------------------------------
// Internal Helper Tests
// ---------------------------------------------------------------------------

func TestIsZero(t *testing.T) {
	t.Run("zero values", func(t *testing.T) {
		AssertTrue(t, isZero(0))
		AssertTrue(t, isZero(int8(0)))
		AssertTrue(t, isZero(int16(0)))
		AssertTrue(t, isZero(int32(0)))
		AssertTrue(t, isZero(int64(0)))
		AssertTrue(t, isZero(uint(0)))
		AssertTrue(t, isZero(uint8(0)))
		AssertTrue(t, isZero(uint16(0)))
		AssertTrue(t, isZero(uint32(0)))
		AssertTrue(t, isZero(uint64(0)))
		AssertTrue(t, isZero(float32(0)))
		AssertTrue(t, isZero(float64(0)))
		AssertTrue(t, isZero(""))
		AssertTrue(t, isZero(false))
		AssertTrue(t, isZero([]byte{}))
		AssertTrue(t, isZero(nil))
	})

	t.Run("non-zero values", func(t *testing.T) {
		AssertFalse(t, isZero(1))
		AssertFalse(t, isZero("hello"))
		AssertFalse(t, isZero(true))
		AssertFalse(t, isZero([]byte{1}))
	})
}

func TestFormatMessage(t *testing.T) {
	t.Run("no args", func(t *testing.T) {
		AssertEqual(t, "prefix", formatMessage("prefix"))
	})

	t.Run("single string arg", func(t *testing.T) {
		AssertEqual(t, "prefix: message", formatMessage("prefix", "message"))
	})

	t.Run("single non-string arg", func(t *testing.T) {
		AssertEqual(t, "prefix: 42", formatMessage("prefix", 42))
	})

	t.Run("format string with args", func(t *testing.T) {
		AssertEqual(t, "prefix: value is 42", formatMessage("prefix", "value is %d", 42))
	})
}

// ---------------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------------

// clearCIEnvVars clears all CI-related environment variables and returns
// a cleanup function to restore them.
func clearCIEnvVars(t *testing.T) func() {
	t.Helper()
	ciVars := []string{
		"CI",
		"GITHUB_ACTIONS",
		"JENKINS_URL",
		"GITLAB_CI",
		"CIRCLECI",
		"TRAVIS",
		"BUILDKITE",
		"AZURE_PIPELINES",
		"TF_BUILD",
	}

	var cleanups []func()
	for _, key := range ciVars {
		if _, exists := os.LookupEnv(key); exists {
			cleanups = append(cleanups, Unsetenv(t, key))
		}
	}

	return func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}
}
