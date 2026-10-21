package conf

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestGeneratedDirDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	c := New()
	c.CoresConfig = []CoreConfig{{Type: "sing", SingConfig: NewSingConfig()}, {Type: "sing", SingConfig: &SingConfig{GeneratedDir: "/custom"}}}
	c.SetGeneratedDirs(configPath)
	if got, want := c.CoresConfig[0].SingConfig.GeneratedDir, filepath.Join(dir, "generated"); got != want {
		t.Errorf("default GeneratedDir = %q, want %q", got, want)
	}
	if c.CoresConfig[1].SingConfig.GeneratedDir != "/custom" {
		t.Error("an explicit GeneratedDir must be kept")
	}
	if got := c.GeneratedDir(configPath); got != filepath.Join(dir, "generated") {
		t.Errorf("GeneratedDir() = %q", got)
	}
}

// Writing the generated files must never trigger a config reload.
func TestConf_Watch_IgnoresGeneratedFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	originPath := filepath.Join(dir, "sing_origin.json")
	if err := os.WriteFile(configPath, []byte(`{"Cores":[{"Type":"sing","OriginalPath":`+quote(originPath)+`}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New()
	if err := c.LoadFromPath(configPath); err != nil {
		t.Fatal(err)
	}
	var reloads atomic.Int32
	w, err := c.WatchWithWatcher(configPath, func() { reloads.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetDebounce(50 * time.Millisecond)

	gen := DefaultGeneratedDir(configPath)
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(gen, "node.effective.json"),
		filepath.Join(gen, "node.panel.json"),
		// even next to the config, as with GeneratedDir set to the config dir
		filepath.Join(dir, "node.effective.json"),
		filepath.Join(dir, ".node.effective.json.123.tmp"),
	} {
		if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(400 * time.Millisecond)
	if n := reloads.Load(); n != 0 {
		t.Fatalf("generated files triggered %d reloads", n)
	}
}

func quote(s string) string {
	return `"` + filepath.ToSlash(s) + `"`
}

func TestNodeConfig_MachineAndNodeModes(t *testing.T) {
	c := New()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"Nodes":[
	  {"ApiConfig":{"ApiHost":"https://panel","ApiKey":"server-token","NodeID":12,"NodeType":"vless"},"Options":{"Core":"sing"}},
	  {"ApiConfig":{"ApiHost":"https://panel","MachineID":3,"MachineToken":"machine-token"},"Options":{"Core":"sing","ListenIP":"::"}},
	  {"ApiHost":"https://panel","ApiKey":"machine-token-2","MachineID":4}
	]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if len(c.NodeConfig) != 3 {
		t.Fatalf("nodes: %d", len(c.NodeConfig))
	}
	n := c.NodeConfig[0].ApiConfig
	if n.IsMachine() || n.Token() != "server-token" || n.NodeID != 12 {
		t.Errorf("node mode: %+v", n)
	}
	m := c.NodeConfig[1].ApiConfig
	if !m.IsMachine() || m.MachineID != 3 || m.Token() != "machine-token" || c.NodeConfig[1].Options.ListenIP != "::" {
		t.Errorf("machine mode: %+v", m)
	}
	flat := c.NodeConfig[2].ApiConfig
	if !flat.IsMachine() || flat.Token() != "machine-token-2" {
		t.Errorf("flat machine entry with ApiKey: %+v", flat)
	}
}
