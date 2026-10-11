package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestConf_LoadFromPath(t *testing.T) {
	c := New()
	t.Log(c.LoadFromPath("../example/config.json"), c.NodeConfig)
}

func TestConf_Watch_FileModification(t *testing.T) {
	c := New()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialContent := `{"Log": {"Level": "info"}}`
	if err := os.WriteFile(configPath, []byte(initialContent), 0644); err != nil {
		t.Fatal(err)
	}

	var reloadCount atomic.Int32
	watcher, err := c.WatchWithWatcher(configPath, func() {
		reloadCount.Add(1)
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer watcher.Close()
	watcher.SetDebounce(50 * time.Millisecond)

	// Modify file
	updatedContent := `{"Log": {"Level": "debug"}}`
	if err := os.WriteFile(configPath, []byte(updatedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for reload
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reloadCount.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if reloadCount.Load() == 0 {
		t.Fatalf("expected reload callback to be triggered upon file modification")
	}
	if c.LogConfig.Level != "debug" {
		t.Fatalf("expected updated LogConfig Level to be debug, got %s", c.LogConfig.Level)
	}
}

func TestConf_Watch_AtomicRename(t *testing.T) {
	c := New()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	tmpFile := filepath.Join(tmpDir, "config.json.tmp")

	if err := os.WriteFile(configPath, []byte(`{"Log": {"Level": "warn"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	var reloadCount atomic.Int32
	watcher, err := c.WatchWithWatcher(configPath, func() {
		reloadCount.Add(1)
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer watcher.Close()
	watcher.SetDebounce(50 * time.Millisecond)

	// Simulate vim/nano atomic save: write to tmp file then rename over target
	if err := os.WriteFile(tmpFile, []byte(`{"Log": {"Level": "error"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpFile, configPath); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reloadCount.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if reloadCount.Load() == 0 {
		t.Fatalf("expected reload callback to be triggered upon atomic rename")
	}
	if c.LogConfig.Level != "error" {
		t.Fatalf("expected updated LogConfig Level to be error, got %s", c.LogConfig.Level)
	}
}

func TestConf_Watch_InvalidSyntaxIgnored(t *testing.T) {
	c := New()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(configPath, []byte(`{"Log": {"Level": "info"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadFromPath(configPath); err != nil {
		t.Fatal(err)
	}

	var reloadCount atomic.Int32
	watcher, err := c.WatchWithWatcher(configPath, func() {
		reloadCount.Add(1)
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer watcher.Close()
	watcher.SetDebounce(50 * time.Millisecond)

	// Write invalid malformed JSON
	if err := os.WriteFile(configPath, []byte(`{INVALID JSON syntax`), 0644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	if reloadCount.Load() != 0 {
		t.Fatalf("expected reload callback NOT to be triggered on syntax error")
	}
	// Verify current running config was NOT wiped
	if c.LogConfig.Level != "info" {
		t.Fatalf("expected running config to remain info, got %s", c.LogConfig.Level)
	}
}

func TestConf_Watch_OriginalPathTracking(t *testing.T) {
	c := New()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	originPath := filepath.Join(tmpDir, "sing_origin.json")

	if err := os.WriteFile(originPath, []byte(`{"route": {"final": "direct"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Config pointing to OriginalPath
	confContent := fmt.Sprintf(`{
		"Cores": [{"Type": "sing", "OriginalPath": %q}],
		"Log": {"Level": "info"}
	}`, originPath)
	if err := os.WriteFile(configPath, []byte(confContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadFromPath(configPath); err != nil {
		t.Fatal(err)
	}

	var reloadCount atomic.Int32
	watcher, err := c.WatchWithWatcher(configPath, func() {
		reloadCount.Add(1)
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer watcher.Close()
	watcher.SetDebounce(50 * time.Millisecond)

	// Modifying the sing_origin.json file should trigger reload
	if err := os.WriteFile(originPath, []byte(`{"route": {"final": "block"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reloadCount.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if reloadCount.Load() == 0 {
		t.Fatalf("expected reload callback to be triggered when sing_origin.json is modified")
	}
}
