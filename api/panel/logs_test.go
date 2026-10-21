package panel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/conf"
)

func TestBuildLogsRequest_AuthPerMode(t *testing.T) {
	entries := []logship.Entry{{Time: 1760840000.123, Level: "error", NodeID: 12, Tag: "inbound/vless[x]", Message: "boom"}}

	// Machine mode: machine_id + machine token, no top-level node_id
	m, _ := New(&conf.ApiConfig{APIHost: "https://p", MachineID: 3, MachineToken: "mt"})
	b, _ := json.Marshal(m.BuildLogsRequest(entries))
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	if got["machine_id"] != 3.0 || got["token"] != "mt" || got["node_id"] != nil {
		t.Errorf("machine body: %s", b)
	}
	e := got["entries"].([]any)[0].(map[string]any)
	if e["time"] != 1760840000.123 || e["level"] != "error" || e["node_id"] != 12.0 || e["tag"] != "inbound/vless[x]" || e["message"] != "boom" {
		t.Errorf("entry: %v", e)
	}

	// Node mode: server token + top-level node_id, no machine_id
	n, _ := New(&conf.ApiConfig{APIHost: "https://p", Key: "st", NodeID: 12, NodeType: "vless"})
	b, _ = json.Marshal(n.BuildLogsRequest(nil))
	got = nil
	_ = json.Unmarshal(b, &got)
	if got["token"] != "st" || got["node_id"] != 12.0 || got["machine_id"] != nil {
		t.Errorf("node body: %s", b)
	}
	if es, ok := got["entries"].([]any); !ok || len(es) != 0 {
		t.Errorf("an empty poll sends entries: [] (not null): %s", b)
	}
}

func TestParseLogsResponse(t *testing.T) {
	r, err := ParseLogsResponse([]byte(`{"data":{"log_mode":"info","accepted":3,"flush_interval":10}}`))
	if err != nil || r.LogMode != "info" || r.Accepted != 3 || r.FlushInterval != 10 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = ParseLogsResponse([]byte(`not json`)); err == nil {
		t.Error("garbage must fail")
	}
}

func TestSendLogs_HTTP(t *testing.T) {
	old := Version
	Version = "7.7.7"
	defer func() { Version = old }()

	status := http.StatusOK
	var path, ua, ct string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ua, ct = r.URL.Path, r.Header.Get("User-Agent"), r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"data":{"log_mode":"debug","accepted":1,"flush_interval":5}}`))
	}))
	defer srv.Close()

	c, err := New(&conf.ApiConfig{APIHost: srv.URL, MachineID: 3, MachineToken: "mt"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.SendLogs(context.Background(), []logship.Entry{{Time: 1, Level: "warn", Message: "w"}})
	if err != nil || resp.LogMode != "debug" || resp.FlushInterval != 5 {
		t.Fatalf("%+v %v", resp, err)
	}
	if path != "/api/v2/custom-node/logs" || ua != "FNode/7.7.7" || ct != "application/json" || body["machine_id"] != 3.0 {
		t.Errorf("path %q ua %q ct %q body %v", path, ua, ct, body)
	}

	status = http.StatusNotFound
	if _, err = c.SendLogs(context.Background(), nil); !errors.Is(err, logship.ErrNotFound) {
		t.Errorf("404 must be ErrNotFound, got %v", err)
	}
	status = http.StatusInternalServerError
	if _, err = c.SendLogs(context.Background(), nil); err == nil || errors.Is(err, logship.ErrNotFound) {
		t.Errorf("500 is an ordinary error, got %v", err)
	}
}

func TestParseWSMessage_LogMode(t *testing.T) {
	_, ev, err := ParseWSMessage([]byte(`{"event":"custom_node.log_mode","data":{"mode":"info","node_id":12}}`))
	if err != nil || ev == nil || ev.LogMode != "info" || ev.NodeID != 12 {
		t.Fatalf("node mode: %+v %v", ev, err)
	}
	_, ev, err = ParseWSMessage([]byte(`{"event":"custom_node.log_mode","data":{"mode":"debug","node_id":null}}`))
	if err != nil || ev.LogMode != "debug" || ev.NodeID != 0 {
		t.Fatalf("null node_id means the whole machine: %+v %v", ev, err)
	}
	_, ev, err = ParseWSMessage([]byte(`{"event":"custom_node.log_mode","data":{"mode":"off"}}`))
	if err != nil || ev.LogMode != "off" {
		t.Fatalf("missing node_id: %+v %v", ev, err)
	}
	if _, _, err = ParseWSMessage([]byte(`{"event":"custom_node.log_mode","data":{"mode":"loud","node_id":1}}`)); err == nil {
		t.Error("an unknown mode must be rejected")
	}
	if _, _, err = ParseWSMessage([]byte(`{"event":"custom_node.log_mode","data":[]}`)); err == nil {
		t.Error("an empty payload must be rejected")
	}
}
