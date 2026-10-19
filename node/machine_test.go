package node

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/sysinfo"
	"github.com/tavut846/FNode/conf"
)

func newTestMachine(t *testing.T) (*Machine, *fakeCore, *[]int) {
	t.Helper()
	core := &fakeCore{}
	m, err := NewMachine(conf.NodeConfig{
		ApiConfig: conf.ApiConfig{APIHost: "http://127.0.0.1:1", MachineID: 3, MachineToken: "mt"},
		Options:   conf.Options{Name: "edge", CertConfig: &conf.CertConfig{CertMode: "none"}},
	}, core)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	started := []int{}
	m.newController = func(n panel.MachineNode) (*Controller, error) {
		if n.ID == 99 {
			return nil, fmt.Errorf("boom")
		}
		mu.Lock()
		started = append(started, n.ID)
		mu.Unlock()
		opts := m.nodeOptions(n.ID)
		api := m.cfg.ApiConfig
		api.NodeID = n.ID
		client, err := panel.New(&api)
		if err != nil {
			return nil, err
		}
		return &Controller{server: core, apiClient: client, tag: opts.Name, Options: opts}, nil
	}
	return m, core, &started
}

func sortedIDs(m *Machine) []int {
	ids := m.RunningIDs()
	sort.Ints(ids)
	return ids
}

func TestMachine_SyncAddsAndRemovesNodes(t *testing.T) {
	m, core, started := newTestMachine(t)

	m.Sync([]panel.MachineNode{{ID: 1, Type: "vless"}, {ID: 2, Type: "hysteria"}, {ID: 3, Type: "mieru"}, {ID: 99, Type: "trojan"}})
	if got := sortedIDs(m); fmt.Sprint(got) != "[1 2]" {
		t.Fatalf("running after first sync: %v (unsupported and failed nodes are skipped)", got)
	}
	if m.controllers[1].Options.Name != "edge-1" {
		t.Errorf("per-node name: %q", m.controllers[1].Options.Name)
	}

	// Node 1 removed, node 4 added, 99 retried (still failing)
	m.Sync([]panel.MachineNode{{ID: 2, Type: "hysteria"}, {ID: 4, Type: "v2ray"}, {ID: 99, Type: "trojan"}})
	if got := sortedIDs(m); fmt.Sprint(got) != "[2 4]" {
		t.Fatalf("running after second sync: %v", got)
	}
	if core.delNode != 1 {
		t.Errorf("removed node must be closed, DelNode calls: %d", core.delNode)
	}
	if fmt.Sprint(*started) != "[1 2 4]" {
		t.Errorf("started: %v (running nodes must not restart)", *started)
	}

	m.Close()
	if len(m.RunningIDs()) != 0 || core.delNode != 3 {
		t.Errorf("close: running %v delNode %d", m.RunningIDs(), core.delNode)
	}
	m.Sync([]panel.MachineNode{{ID: 7, Type: "vless"}})
	if len(m.RunningIDs()) != 0 {
		t.Error("a closed machine must not start nodes")
	}
}

func TestMachine_WSRouting(t *testing.T) {
	m, _, _ := newTestMachine(t)
	m.Sync([]panel.MachineNode{{ID: 1, Type: "vless"}})

	// sync.nodes updates the node set
	_, ev, err := panel.ParseWSMessage([]byte(`{"event":"sync.nodes","data":{"nodes":[{"id":1,"type":"vless"},{"id":5,"type":"tuic"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	m.handleWSEvent(ev)
	deadline := time.Now().Add(2 * time.Second)
	for len(m.RunningIDs()) != 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := sortedIDs(m); fmt.Sprint(got) != "[1 5]" {
		t.Fatalf("after sync.nodes: %v", got)
	}

	// Node events reach the controller named by node_id (closed controllers ignore them)
	m.controllers[5].closed = true
	_, ev, _ = panel.ParseWSMessage([]byte(`{"event":"sync.devices","data":{"node_id":5,"users":{"1":["1.1.1.1"]}}}`))
	m.handleWSEvent(ev)
	_, ev, _ = panel.ParseWSMessage([]byte(`{"event":"sync.devices","data":{"node_id":42,"users":{}}}`))
	m.handleWSEvent(ev) // unknown node: dropped without panic
}

func TestMachine_Intervals(t *testing.T) {
	m, _, _ := newTestMachine(t)
	m.applyIntervals(panel.MachineBaseConfig{PushInterval: 5, PullInterval: 10})
	pull, push := m.intervals()
	if pull != 60*time.Second || push != 60*time.Second {
		t.Errorf("too small intervals fall back to 60s: pull %v push %v", pull, push)
	}
	m.applyIntervals(panel.MachineBaseConfig{PushInterval: 30, PullInterval: 120})
	pull, push = m.intervals()
	if pull != 120*time.Second || push != 30*time.Second {
		t.Errorf("intervals: pull %v push %v", pull, push)
	}
}

func TestStatusSample(t *testing.T) {
	s := StatusSample(sysinfo.Status{CPU: 10, MemTotal: 2, NetInSpeed: -1, NetOutSpeed: -1})
	if s.CPU != 10 || s.MemTotal != 2 || s.NetInSpeed != -1 {
		t.Errorf("%+v", s)
	}
}
