package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanLogs(t *testing.T) {
	tmpDir := t.TempDir()
	testLogFile := filepath.Join(tmpDir, "test_fnode.log")

	// Write some initial data
	initialData := []byte("2026-10-16 [INFO] Test log entry line 1\n2026-10-16 [INFO] Test log entry line 2\n")
	if err := os.WriteFile(testLogFile, initialData, 0644); err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	fi, err := os.Stat(testLogFile)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("Expected non-empty test log file, got size %d", fi.Size())
	}

	// Run CleanLogs with custom test path
	cleaned := CleanLogs(testLogFile)

	found := false
	for _, p := range cleaned {
		if p == testLogFile {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected %s in cleaned files list, got %v", testLogFile, cleaned)
	}

	// Verify file is truncated to 0 bytes
	fi, err = os.Stat(testLogFile)
	if err != nil {
		t.Fatalf("Failed to stat cleaned test log file: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("Expected cleaned file size to be 0, got %d", fi.Size())
	}
}

func TestStartAutoLogCleaner(t *testing.T) {
	tmpDir := t.TempDir()
	testLogFile := filepath.Join(tmpDir, "test_auto_clean.log")

	if err := os.WriteFile(testLogFile, []byte("log data to clean"), 0644); err != nil {
		t.Fatalf("Failed to write test log file: %v", err)
	}

	// Start auto cleaner with a short interval (30ms) for testing
	stop := startAutoLogCleaner(30*time.Millisecond, testLogFile)
	defer stop()

	// Poll until truncated or timeout
	deadline := time.Now().Add(2 * time.Second)
	var lastSize int64 = -1
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(testLogFile); err == nil {
			lastSize = fi.Size()
			if lastSize == 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lastSize != 0 {
		t.Fatalf("Expected file to be truncated by auto cleaner, got size %d", lastSize)
	}
}
