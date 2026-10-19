package panel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/conf"
)

// The log push carries the client, version, missing outbound tags and the
// dropped count, and the panel's per-node modes are parsed.
func TestSendLogsMeta(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"log_mode":"info","accepted":0,"flush_interval":10,"node_modes":{"12":"debug"}}}`))
	}))
	defer srv.Close()
	c, err := New(&conf.ApiConfig{APIHost: srv.URL, MachineID: 3, MachineToken: "mt"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.SendLogsMeta(context.Background(), nil, logship.Meta{Missing: map[int][]string{12: {"hk"}}, Dropped: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.HasNodeModes || resp.NodeModes[12] != "debug" {
		t.Errorf("node modes: %+v", resp)
	}
	missing, _ := body["missing_outbounds"].(map[string]any)
	if body["client"] != "fnode" || body["dropped"] != 4.0 || missing == nil || missing["12"] == nil {
		t.Errorf("request body: %v", body)
	}
}

func TestParseLogsResponse_EmptyNodeModes(t *testing.T) {
	r, err := ParseLogsResponse([]byte(`{"data":{"log_mode":"warn","node_modes":[]}}`))
	if err != nil || !r.HasNodeModes || len(r.NodeModes) != 0 {
		t.Errorf("PHP's [] is an empty map: %+v %v", r, err)
	}
	r, _ = ParseLogsResponse([]byte(`{"data":{"log_mode":"warn"}}`))
	if r.HasNodeModes {
		t.Error("an old panel without node_modes keeps the WS overrides")
	}
}

// A NodeType that does not match the panel node is resolved through the v2
// API (type read from the config).
func TestUseAutoType(t *testing.T) {
	var v1, v2 atomic.Int32
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/server/UniProxy/config" {
			v1.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"server is not exist"}`))
			return
		}
		v2.Add(1)
		_, _ = w.Write([]byte(`{"protocol":"trojan","server_port":443,"server_name":"a.com","base_config":{"push_interval":60,"pull_interval":60}}`))
	}))
	defer srv.Close()
	c, err := New(&conf.ApiConfig{APIHost: srv.URL, NodeID: 5, NodeType: "vless", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.GetNodeInfo(); err == nil {
		t.Fatal("v1 with the wrong type fails")
	}
	if !c.UseAutoType() || c.UseAutoType() {
		t.Fatal("UseAutoType switches once")
	}
	n, err := c.GetNodeInfo()
	if err != nil || n == nil || n.Type != "trojan" {
		t.Fatalf("v2 detects the panel type: %+v %v", n, err)
	}
	if v1.Load() != 1 || v2.Load() != 1 {
		t.Errorf("calls v1 %d v2 %d", v1.Load(), v2.Load())
	}
	if want := "node_type"; contains(lastQuery, want) {
		t.Errorf("node_type must be dropped from the query: %s", lastQuery)
	}
	if n, _ := c.GetNodeInfo(); n != nil {
		t.Error("an unchanged config is nil")
	}
	c.ResetConfigCache()
	if n, _ := c.GetNodeInfo(); n == nil {
		t.Error("after ResetConfigCache the full config is returned again")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
