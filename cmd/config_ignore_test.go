package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigIgnore(t *testing.T) {
	cases := map[string]string{
		`{"Cores": [{"Type": "sing"}]}`:                                                        "off",
		`{"IgnorePanelOutbounds": true, "Cores": [{"Type": "sing"}]}`:                          "on",
		`{"Cores": [{"Type": "sing", "IgnorePanelOutbounds": true}]}`:                          "on",
		`{"IgnorePanelOutboundTags": ["a","b"], "Cores": [{"Type": "sing"}]}`:                  "tags a,b",
		"{\n // c\n \"IgnorePanelOutbounds\": true,\n}":                                        "on",
		`{"IgnorePanelOutbounds": false, "Cores": [{"Type": "sing"}]} `:                        "off",
		`{"IgnorePanelOutbounds": true, "IgnorePanelOutboundTags": ["x"], "Cores": []}`:        "on",
	}
	defer func() { configIgnorePath = "/etc/FNode/config.json" }()
	for content, want := range cases {
		f := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		configIgnorePath = f
		var out bytes.Buffer
		if err := configIgnore(&out); err != nil {
			t.Fatalf("%s: %v", content, err)
		}
		if got := strings.TrimSpace(out.String()); got != want {
			t.Errorf("%s: got %q, want %q", content, got, want)
		}
	}
	configIgnorePath = filepath.Join(t.TempDir(), "missing.json")
	if err := configIgnore(&bytes.Buffer{}); err == nil {
		t.Error("a missing config must be an error")
	}
}
