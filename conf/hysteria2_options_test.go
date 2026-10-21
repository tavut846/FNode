package conf

import (
	"os"
	"path/filepath"
	"testing"
)

// MaxBrutalMbps distinguishes "not set" (default cap) from 0/-1 (unlimited),
// and the new Options keys load from config.json.
func TestConf_Hysteria2AndKeepAliveOptions(t *testing.T) {
	load := func(node string) NodeConfig {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.json")
		body := `{"Cores":[{"Type":"sing"}],"Nodes":[{"Core":"sing","ApiHost":"https://p","ApiKey":"k","NodeID":1,` + node + `}]}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		c := New()
		if err := c.LoadFromPath(path); err != nil {
			t.Fatalf("load: %v", err)
		}
		return c.NodeConfig[0]
	}
	n := load(`"TCPKeepAlive":20,"TCPKeepAliveInterval":5,"Hysteria2Config":{"IgnoreClientBandwidth":true,"MaxBrutalMbps":0}`)
	if n.Options.TCPKeepAlive != 20 || n.Options.TCPKeepAliveInterval != 5 {
		t.Errorf("keepalive: %d %d", n.Options.TCPKeepAlive, n.Options.TCPKeepAliveInterval)
	}
	h := n.Options.Hysteria2Config
	if h == nil || !h.IgnoreClientBandwidth || h.MaxBrutalMbps == nil || *h.MaxBrutalMbps != 0 {
		t.Errorf("hysteria2 options: %+v", h)
	}
	n = load(`"Hysteria2Config":{"MaxBrutalMbps":-1}`)
	if h := n.Options.Hysteria2Config; h == nil || h.MaxBrutalMbps == nil || *h.MaxBrutalMbps != -1 {
		t.Errorf("-1 not loaded: %+v", h)
	}
	n = load(`"Hysteria2Config":{"UDPTimeout":"60s"}`)
	if h := n.Options.Hysteria2Config; h == nil || h.MaxBrutalMbps != nil || h.IgnoreClientBandwidth {
		t.Errorf("absent keys must stay unset: %+v", h)
	}
}
