package sing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"

	"github.com/tavut846/FNode/api/panel"
)

func TestNodeFileStem(t *testing.T) {
	cases := map[string]string{
		"[https://panel.example.com]-vless:1": "https_panel.example.com_-vless_1",
		"my-node":                             "my-node",
		"../../etc/passwd":                    "etc_passwd",
		"":                                    "node",
	}
	for in, want := range cases {
		if got := NodeFileStem(in); got != want {
			t.Errorf("NodeFileStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generated")
	path := filepath.Join(dir, "a.effective.json")
	for _, content := range []string{"first", "second"} {
		if err := writeFileAtomic(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != content {
			t.Errorf("want %q got %q", content, data)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestSing_WritesEffectiveAndPanelFiles(t *testing.T) {
	b := newTestSing(t, []map[string]any{{"type": "socks", "tag": "local-only", "server": "10.0.0.3", "server_port": 1080}})
	dir := t.TempDir()
	b.coreConfig.SingConfig.GeneratedDir = dir

	tag := "[https://panel.example.com]-vless:1"
	setNode(b, tag, &panel.CommonNode{
		Routes: []panel.Route{{Id: 1, Match: "netflix.com", Action: "proxy", ActionValue: "us-proxy"}},
		CustomOutbounds: []panel.CustomOutbound{
			{Tag: "us-proxy", Protocol: "socks", Settings: map[string]any{"server": "1.2.3.4", "server_port": 1080}},
		},
	})
	b.users.mapLock.Lock()
	b.inboundOptions[tag] = option.Inbound{Type: "vless", Tag: tag, Options: &option.VLESSInboundOptions{
		Users: []option.VLESSUser{{Name: "u1", UUID: "11111111-1111-1111-1111-111111111111"}},
	}}
	b.users.mapLock.Unlock()
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}

	stem := NodeFileStem(tag)
	data, err := os.ReadFile(filepath.Join(dir, stem+EffectiveSuffix))
	if err != nil {
		t.Fatalf("effective file: %v", err)
	}
	var doc struct {
		Meta      EffectiveMeta    `json:"_fnode"`
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
		Route     struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Meta.Node != tag || doc.Meta.Counts[SourcePanel] != 1 || doc.Meta.Counts[SourceLocal] != 1 {
		t.Errorf("meta: %+v", doc.Meta)
	}
	sources := map[string]string{}
	for _, o := range doc.Meta.Outbounds {
		sources[o.Tag] = o.Source
	}
	if sources["us-proxy"] != SourcePanel || sources["local-only"] != SourceLocal || sources["shared"] != SourceOrigin {
		t.Errorf("sources: %v", sources)
	}
	if len(doc.Outbounds) != len(doc.Meta.Outbounds) {
		t.Errorf("outbounds %d vs meta %d", len(doc.Outbounds), len(doc.Meta.Outbounds))
	}
	if len(doc.Inbounds) != 1 {
		t.Fatalf("inbounds: %v", doc.Inbounds)
	}
	if _, ok := doc.Inbounds[0]["users"]; ok {
		t.Error("users must be dropped from the dump")
	}
	if !strings.Contains(string(data), `"outbound": "us-proxy"`) {
		t.Error("the proxy rule must be in the dump")
	}
	if !strings.Contains(string(data), "origin.example") {
		t.Error("origin rules must be in the dump")
	}

	panelData, err := os.ReadFile(filepath.Join(dir, stem+PanelSuffix))
	if err != nil {
		t.Fatalf("panel file: %v", err)
	}
	var pd map[string]any
	_ = json.Unmarshal(panelData, &pd)
	if outs, _ := pd["custom_outbounds"].([]any); len(outs) != 1 {
		t.Errorf("panel dump: %s", panelData)
	}
	if _, ok := pd["custom_routes"].([]any); !ok {
		t.Errorf("panel dump must list custom_routes even when empty: %s", panelData)
	}

	files, err := ReadEffectiveFiles(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("read back: %v %v", files, err)
	}
	for _, filter := range []string{"", tag, stem, "vless:1"} {
		if !files[0].Matches(filter) {
			t.Errorf("filter %q must match", filter)
		}
	}
	if files[0].Matches("trojan") {
		t.Error("filter trojan must not match")
	}

	// The node goes away: a pruning apply removes its files
	b.users.mapLock.Lock()
	delete(b.inboundInfo, tag)
	b.users.mapLock.Unlock()
	if err = b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, stem+EffectiveSuffix)); !os.IsNotExist(err) {
		t.Error("stale effective file must be removed")
	}
}

func TestSing_GeneratedFilesNeverOverwriteOrigin(t *testing.T) {
	b := newTestSing(t, nil)
	origin := b.coreConfig.SingConfig.OriginalPath
	if !isProtected(origin, b.protectedPaths()) {
		t.Fatal("OriginalPath must be protected")
	}
	before, _ := os.ReadFile(origin)
	b.coreConfig.SingConfig.GeneratedDir = filepath.Dir(origin)
	setNode(b, "n", &panel.CommonNode{})
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(origin)
	if string(before) != string(after) {
		t.Error("sing_origin.json must never be modified")
	}
}
