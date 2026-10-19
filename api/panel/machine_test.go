package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tavut846/FNode/conf"
)

func TestNew_ModesAndPaths(t *testing.T) {
	// Existing ApiKey + NodeID + NodeType mode: v1 UniProxy, unchanged params
	v1, err := New(&conf.ApiConfig{APIHost: "https://p", Key: "server-token", NodeID: 12, NodeType: "V2ray"})
	if err != nil {
		t.Fatal(err)
	}
	q := v1.client.QueryParam
	if v1.IsV2() || v1.apiPath("config") != "/api/v1/server/UniProxy/config" ||
		q.Get("node_type") != "vmess" || q.Get("node_id") != "12" || q.Get("token") != "server-token" || q.Has("machine_id") {
		t.Errorf("v1 mode: v2=%v path=%s params=%v", v1.IsV2(), v1.apiPath("config"), q)
	}

	// Machine mode: v2 paths, machine_id + machine token, no node_type
	m, err := New(&conf.ApiConfig{APIHost: "https://p", Key: "ignored", MachineToken: "machine-token", MachineID: 3, NodeID: 7})
	if err != nil {
		t.Fatal(err)
	}
	q = m.client.QueryParam
	if !m.IsV2() || m.apiPath("user") != "/api/v2/server/user" || q.Get("machine_id") != "3" ||
		q.Get("token") != "machine-token" || q.Get("node_id") != "7" || q.Has("node_type") || m.Token != "machine-token" {
		t.Errorf("machine mode: v2=%v path=%s params=%v", m.IsV2(), m.apiPath("user"), q)
	}
	// MachineToken falls back to ApiKey; the machine-level client has no node_id
	m, _ = New(&conf.ApiConfig{APIHost: "https://p", Key: "machine-token", MachineID: 3})
	if q = m.client.QueryParam; q.Get("token") != "machine-token" || q.Has("node_id") {
		t.Errorf("machine client params: %v", q)
	}

	// Server token without NodeType: v2 API, type detected from the config
	s, err := New(&conf.ApiConfig{APIHost: "https://p", Key: "server-token", NodeID: 12})
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsV2() || s.client.QueryParam.Has("node_type") || s.NodeType != "" {
		t.Errorf("server-token v2 mode: %+v", s.client.QueryParam)
	}

	if _, err = New(&conf.ApiConfig{APIHost: "https://p", Key: "k", NodeID: 1, NodeType: "mieru"}); err == nil {
		t.Error("unsupported NodeType must fail")
	}
}

func TestDetectNodeType(t *testing.T) {
	cases := map[string]string{
		`{"protocol":"vless"}`:                  "vless",
		`{"protocol":"v2ray"}`:                  "vmess",
		`{"protocol":"hysteria","version":2}`:   "hysteria2",
		`{"protocol":"hysteria","version":"2"}`: "hysteria2",
		`{"protocol":"hysteria","version":1}`:   "hysteria",
	}
	for body, want := range cases {
		got, err := DetectNodeType([]byte(body))
		if err != nil || got != want {
			t.Errorf("%s: got %q %v, want %q", body, got, err, want)
		}
	}
	if _, err := DetectNodeType([]byte(`{"protocol":"naive"}`)); err == nil {
		t.Error("unsupported protocol must fail")
	}

	// A v2 client parses the config with the detected type
	c := &Client{NodeId: 5}
	node, err := c.ParseNodeConfig([]byte(`{"protocol":"hysteria","version":2,"server_port":443,"up_mbps":100,"down_mbps":100,"obfs":"salamander","obfs-password":"x"}`))
	if err != nil || node == nil || node.Type != "hysteria2" || node.Hysteria2 == nil || node.Hysteria2.ObfsPassword != "x" {
		t.Fatalf("v2 parse: %+v %v", node, err)
	}
}

func TestParseMachineNodes(t *testing.T) {
	resp, err := ParseMachineNodes([]byte(`{"nodes":[{"id":12,"type":"vless","name":"HK"},{"id":13,"type":"hysteria","name":"JP"}],"base_config":{"push_interval":60,"pull_interval":120}}`))
	if err != nil || len(resp.Nodes) != 2 || resp.Nodes[1].Type != "hysteria" || resp.BaseConfig.PullInterval != 120 {
		t.Fatalf("%+v %v", resp, err)
	}
}

func TestDiffMachineNodes(t *testing.T) {
	add, remove := DiffMachineNodes([]int{1, 2, 3}, []MachineNode{{ID: 4, Type: "vless"}, {ID: 2}, {ID: 1}, {ID: 4}, {ID: 0}})
	if len(add) != 1 || add[0].ID != 4 {
		t.Errorf("add: %+v", add)
	}
	if len(remove) != 1 || remove[0] != 3 {
		t.Errorf("remove: %v", remove)
	}
	add, remove = DiffMachineNodes(nil, nil)
	if len(add) != 0 || len(remove) != 0 {
		t.Errorf("empty diff: %v %v", add, remove)
	}
	_, remove = DiffMachineNodes([]int{5, 1}, []MachineNode{})
	if len(remove) != 2 || remove[0] != 1 {
		t.Errorf("all removed, sorted: %v", remove)
	}
}

func TestMachineStatusPayload(t *testing.T) {
	p := MachineStatusPayload(MachineStatus{CPU: 140, MemTotal: 100, MemUsed: 150, DiskTotal: 10, DiskUsed: 4,
		NetInSpeed: -1, NetOutSpeed: -1}, "1.2.3")
	if p["cpu"] != 100.0 || p["client"] != "fnode" || p["version"] != "1.2.3" {
		t.Errorf("payload: %v", p)
	}
	if mem := p["mem"].(map[string]any); mem["total"] != uint64(100) || mem["used"] != uint64(100) {
		t.Errorf("mem used must be clamped to total: %v", mem)
	}
	if _, ok := p["net"]; ok {
		t.Error("unknown net speed must be omitted")
	}
	p = MachineStatusPayload(MachineStatus{CPU: 12.5, NetInSpeed: 10, NetOutSpeed: 20}, "v")
	net, ok := p["net"].(map[string]any)
	if !ok || net["in_speed"] != 10.0 || net["out_speed"] != 20.0 {
		t.Errorf("net: %v", p["net"])
	}
	// The JSON keys match MachineController::status validation
	b, _ := json.Marshal(p)
	for _, key := range []string{`"cpu":12.5`, `"mem":{`, `"swap":{`, `"disk":{`, `"in_speed":10`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("payload JSON misses %s: %s", key, b)
		}
	}
}

func TestMachineAPI_NodesAndStatus(t *testing.T) {
	var statusBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodPost || q.Get("machine_id") != "3" || q.Get("token") != "mt" {
			http.Error(w, "bad auth "+r.URL.String(), http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/server/machine/nodes":
			_, _ = w.Write([]byte(`{"nodes":[{"id":12,"type":"vless","name":"HK"}],"base_config":{"push_interval":60,"pull_interval":60}}`))
		case "/api/v2/server/machine/status":
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &statusBody)
			_, _ = w.Write([]byte(`{"data":true}`))
		case "/api/v2/server/handshake":
			_, _ = w.Write([]byte(`{"websocket":{"enabled":true,"ws_url":"ws://p/ws"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := New(&conf.ApiConfig{APIHost: srv.URL, MachineToken: "mt", MachineID: 3})
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := c.GetMachineNodes()
	if err != nil || len(nodes.Nodes) != 1 || nodes.Nodes[0].ID != 12 {
		t.Fatalf("nodes: %+v %v", nodes, err)
	}
	hs, err := c.Handshake()
	if err != nil || !hs.WebSocket.Enabled {
		t.Fatalf("machine handshake: %+v %v", hs, err)
	}
	if err = c.ReportMachineStatus(MachineStatus{CPU: 5, MemTotal: 10, MemUsed: 5, NetInSpeed: 1, NetOutSpeed: 2}, "9.9.9"); err != nil {
		t.Fatal(err)
	}
	if statusBody["cpu"] != 5.0 || statusBody["client"] != "fnode" || statusBody["version"] != "9.9.9" {
		t.Errorf("status body: %v", statusBody)
	}
}

func TestParseWSMessage_SyncNodesAndMachineURL(t *testing.T) {
	_, ev, err := ParseWSMessage([]byte(`{"event":"sync.nodes","data":{"nodes":[{"id":4,"type":"trojan","name":"x"}]}}`))
	if err != nil || ev == nil || len(ev.Nodes) != 1 || ev.Nodes[0].ID != 4 {
		t.Fatalf("sync.nodes: %+v %v", ev, err)
	}
	_, ev, err = ParseWSMessage([]byte(`{"event":"sync.nodes","data":{"nodes":[]}}`))
	if err != nil || ev.Nodes == nil || len(ev.Nodes) != 0 {
		t.Fatalf("empty sync.nodes: %+v %v", ev, err)
	}
	ws, err := NewMachineWSClient("wss://p.example.com/ws", "mt", 3, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ws.url, "machine_id=3") || strings.Contains(ws.url, "node_id") {
		t.Errorf("machine WS URL: %s", ws.url)
	}
}
