package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEffective(t *testing.T, dir, tag string) {
	t.Helper()
	doc := `{"_fnode": {"node": "` + tag + `", "outbounds": []}, "outbounds": []}`
	name := strings.NewReplacer("[", "", "]", "", ":", "_", "/", "_").Replace(tag) + ".effective.json"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMaskToken(t *testing.T) {
	for in, want := range map[string]string{
		"abcdefghijklmnopqrst": "abcde...pqrst",
		"abcdefghijk":          "abcde...ghijk",
		"abcdefghij":           "a...",
		"":                     "(empty)",
	} {
		if got := maskToken(in); got != want {
			t.Errorf("maskToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigStatus(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	// A machine entry and a node entry on the same panel, plus a named node.
	if err := os.WriteFile(cfg, []byte(`{
  // JSON5 like the real config
  "Nodes": [
    {"Core": "sing", "ApiHost": "https://board.example", "MachineID": 7, "MachineToken": "MACH1-secret-token-ZZ999"},
    {"Core": "sing", "ApiHost": "https://board.example", "ApiKey": "NODE1-server-token-YY888", "NodeID": 12, "NodeType": "vless"},
    {"Core": "sing", "ApiHost": "https://other.example", "ApiKey": "short", "NodeID": 3, "Name": "my-local-tag"},
    {"Core": "sing", "ApiHost": "https://idle.example", "ApiKey": "idle-token-0123456", "NodeID": 9},
  ],
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	gen := t.TempDir()
	for _, tag := range []string{
		"[https://board.example]-anytls:468",
		"[https://board.example]-anytls:469",
		"[https://board.example]-vless:12",
		"my-local-tag",
	} {
		writeEffective(t, gen, tag)
	}
	configStatusPath, configStatusDir = cfg, gen
	defer func() { configStatusPath, configStatusDir = "/etc/FNode/config.json", "" }()

	var out bytes.Buffer
	if err := configStatus(&out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	blocks := strings.Split(s, "\n\n")
	if len(blocks) != 4 {
		t.Fatalf("want 4 entry blocks:\n%s", s)
	}
	machine := "Panel:  https://board.example (machine 7, from dashboard)\n" +
		"Token:  MACH1...ZZ999\n" +
		"Node:   [https://board.example]-anytls:468 --Panel\n" +
		"        [https://board.example]-anytls:469 --Panel"
	if blocks[0] != machine {
		t.Errorf("machine block:\n%s\nwant:\n%s", blocks[0], machine)
	}
	node := "Panel:  https://board.example (node 12)\n" +
		"Token:  NODE1...YY888\n" +
		"Node:   [https://board.example]-vless:12"
	if blocks[1] != node {
		t.Errorf("node block:\n%s\nwant:\n%s", blocks[1], node)
	}
	if !strings.Contains(blocks[2], "Token:  s...") || !strings.HasSuffix(blocks[2], "Node:   my-local-tag") || strings.Contains(blocks[2], "--Panel") {
		t.Errorf("named node block:\n%s", blocks[2])
	}
	if !strings.Contains(blocks[3], "none running") {
		t.Errorf("idle block:\n%s", blocks[3])
	}
	if strings.Contains(s, "secret") || strings.Contains(s, "server-token") {
		t.Errorf("token not masked:\n%s", s)
	}
}
