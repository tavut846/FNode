package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testEffective = `{
  "_fnode": {
    "node": "[https://panel.example.com]-vless:1",
    "generated_at": "2026-10-18T12:00:00Z",
    "note": "test",
    "outbound_counts": {"origin": 1, "panel": 1},
    "rules": 4,
    "outbounds": [
      {"tag": "direct", "type": "direct", "source": "origin"},
      {"tag": "us-proxy", "type": "socks", "source": "panel", "node": "[https://panel.example.com]-vless:1"}
    ]
  },
  "outbounds": []
}
`

func TestConfigShow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "https_panel.example.com_-vless_1.effective.json"), []byte(testEffective), 0o600); err != nil {
		t.Fatal(err)
	}
	configShowDir = dir
	defer func() { configShowDir, configShowNode, configShowSummary = "", "", false }()

	var out bytes.Buffer
	configShowSummary = true
	if err := configShow(&out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"== [https://panel.example.com]-vless:1", "outbounds: 2 (origin 1, local 0, panel 1, builtin 0), rules: 4", "us-proxy", "panel"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary misses %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"outbounds": []`) {
		t.Error("--summary must not print the JSON")
	}
	// panel outbounds are listed first
	if strings.Index(s, "us-proxy") > strings.Index(s, "direct  ") {
		t.Errorf("panel outbounds should come first:\n%s", s)
	}

	out.Reset()
	configShowSummary = false
	configShowNode = "vless:1"
	if err := configShow(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"outbounds": []`) {
		t.Error("show must print the effective file")
	}

	configShowNode = "trojan"
	if err := configShow(&out); err == nil || !strings.Contains(err.Error(), "vless:1") {
		t.Errorf("unknown node must fail and list nodes, got %v", err)
	}
}
