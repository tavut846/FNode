package node

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

// A start that fails (panel down at boot, no users yet) is retried in the
// background until it works, instead of leaving the node offline.
func TestStartWithRetry(t *testing.T) {
	oldMin, oldMax := retryMin, retryMax
	retryMin, retryMax = 10*time.Millisecond, 20*time.Millisecond
	defer func() { retryMin, retryMax = oldMin, oldMax }()

	n := New()
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	var calls atomic.Int32
	n.startWithRetry(ctx, "test", func() error {
		if calls.Add(1) < 3 {
			return ErrNoUsers
		}
		return nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() != 3 {
		t.Fatalf("start retried until success, calls %d", calls.Load())
	}
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 3 {
		t.Errorf("no more attempts after success: %d", calls.Load())
	}
	n.Close()
}

func TestStartWithRetry_StopsOnClose(t *testing.T) {
	oldMin := retryMin
	retryMin = 10 * time.Millisecond
	defer func() { retryMin = oldMin }()
	n := New()
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	var calls atomic.Int32
	n.startWithRetry(ctx, "test", func() error { calls.Add(1); return errors.New("down") })
	n.Close()
	after := calls.Load()
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != after {
		t.Errorf("Close stops the retries")
	}
}

// A node listed both as a standalone entry and on the panel machine runs once.
func TestMachine_SkipsStandaloneNodes(t *testing.T) {
	nodes := []conf.NodeConfig{
		{ApiConfig: conf.ApiConfig{APIHost: "https://Panel.example/", NodeID: 2, Key: "k"}},
		{ApiConfig: conf.ApiConfig{APIHost: "https://panel.example", MachineID: 1, MachineToken: "m"}},
	}
	keys := StandaloneKeys(nodes)
	if !keys[panelKey("https://panel.example", 2)] {
		t.Fatalf("host is normalised: %v", keys)
	}
	m, _, _ := newTestMachine(t)
	m.skip = func(id int) bool { return keys[panelKey("https://panel.example", id)] }
	m.Sync([]panel.MachineNode{{ID: 1, Type: "vless"}, {ID: 2, Type: "vless"}})
	ids := m.RunningIDs()
	if len(ids) != 1 || ids[0] != 1 {
		t.Errorf("node 2 is left to the standalone entry: %v", ids)
	}
}
