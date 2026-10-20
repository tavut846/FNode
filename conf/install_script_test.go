package conf

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runInstaller runs FNode-script/install.sh (CRLF stripped, as on a Windows
// checkout) with the given arguments.
func runInstaller(t *testing.T, args ...string) (string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	src, err := os.ReadFile("../FNode-script/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "install.sh")
	if err = os.WriteFile(script, bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, append([]string{script}, args...)...)
	cmd.Stdin = strings.NewReader("") // no TTY, like curl | bash
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func loadRendered(t *testing.T, out string) *Conf {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New()
	if err := c.LoadFromPath(path); err != nil {
		t.Fatalf("FNode cannot load the installer config: %v\n%s", err, out)
	}
	return c
}

func TestInstallScript_PrintConfigBothModes(t *testing.T) {
	// Windows argv quoting cannot carry a raw quote into msys bash
	token := `tok"en`
	if runtime.GOOS == "windows" {
		token = "tok$en"
	}
	out, err := runInstaller(t, "--print-config", "--mode", "machine", "--panel", "https://panel.example.com/", "--token", token, "--machine-id", "3")
	if err != nil {
		t.Fatalf("machine mode: %v\n%s", err, out)
	}
	c := loadRendered(t, out)
	if len(c.NodeConfig) != 1 {
		t.Fatalf("nodes: %d", len(c.NodeConfig))
	}
	api := c.NodeConfig[0].ApiConfig
	if !api.IsMachine() || api.MachineID != 3 || api.Token() != token || api.APIHost != "https://panel.example.com" {
		t.Errorf("machine entry: %+v", api)
	}
	if c.NodeConfig[0].Options.Core != "sing" || len(c.CoresConfig) != 1 || c.CoresConfig[0].SingConfig == nil {
		t.Errorf("options/cores: %+v %+v", c.NodeConfig[0].Options, c.CoresConfig)
	}

	out, err = runInstaller(t, "--print-config", "--mode", "node", "--panel", "https://panel.example.com", "--token", "server-token", "--node-id", "12", "--version", "pre", "--branch", "dev")
	if err != nil {
		t.Fatalf("node mode: %v\n%s", err, out)
	}
	api = loadRendered(t, out).NodeConfig[0].ApiConfig
	if api.IsMachine() || api.NodeID != 12 || api.Key != "server-token" || api.NodeType != "" {
		t.Errorf("node entry: %+v", api)
	}

	out, err = runInstaller(t, "--print-config", "--mode=node", "--panel=https://p", "--token=t", "--node-id=5", "--type=vless", "--cert-mode", "http", "--cert-domain", "n.example.com")
	if err != nil {
		t.Fatalf("node mode with type: %v\n%s", err, out)
	}
	c = loadRendered(t, out)
	if c.NodeConfig[0].ApiConfig.NodeType != "vless" || c.NodeConfig[0].Options.CertConfig.CertMode != "http" ||
		c.NodeConfig[0].Options.CertConfig.CertDomain != "n.example.com" {
		t.Errorf("node entry with type/cert: %+v %+v", c.NodeConfig[0].ApiConfig, c.NodeConfig[0].Options.CertConfig)
	}
}

func TestInstallScript_RejectsBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--print-config", "--mode", "machine", "--panel", "https://p", "--token", "t"},                        // no machine id
		{"--print-config", "--mode", "node", "--panel", "https://p", "--token", "t", "--node-id", "x"},         // bad id
		{"--print-config", "--mode", "node", "--panel", "panel.example.com", "--token", "t", "--node-id", "1"}, // no scheme
		{"--print-config", "--mode", "vps", "--panel", "https://p", "--token", "t"},                            // bad mode
		{"--print-config", "--mode", "node", "--panel", "https://p", "--token", "t", "--node-id", "1", "--type", "naive"},
		{"--mode"},         // missing value
		{"--unknown-flag"}, // unknown option
	} {
		if out, err := runInstaller(t, args...); err == nil {
			t.Errorf("%v must fail, got:\n%s", args, out)
		}
	}
	if out, err := runInstaller(t, "--help"); err != nil || !strings.Contains(out, "--machine-id") {
		t.Errorf("--help: %v\n%s", err, out)
	}
}

// A dashboard install over an existing config must apply the panel flags:
// the old node entries go, Log/Cores stay, and an error log level becomes
// info so the panel connection shows in the journal.
func TestInstallScript_MergeReplacesExistingNodes(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available")
	}
	existing := filepath.Join(t.TempDir(), "config.json")
	old := `{
  "Log": {"Level": "error", "Output": ""},
  "Cores": [{"Type": "sing", "Log": {"Level": "info", "Timestamp": true}, "OriginalPath": "/etc/FNode/sing_origin.json"}],
  "Nodes": [
    {"Core": "sing", "ApiHost": "https://old.example", "ApiKey": "old", "NodeID": 468, "NodeType": "anytls", "ListenIP": "::"},
    {"Core": "sing", "ApiHost": "https://old.example", "ApiKey": "old", "NodeID": 469, "NodeType": "anytls", "ListenIP": "::"}
  ]
}`
	if err := os.WriteFile(existing, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runInstaller(t, "--print-config", "--existing", existing, "--mode", "machine", "--panel", "https://panel.example.com", "--token", "machine-token", "--machine-id", "7")
	if err != nil {
		t.Fatalf("merge: %v\n%s", err, out)
	}
	c := loadRendered(t, out)
	if len(c.NodeConfig) != 1 {
		t.Fatalf("old node entries kept: %d nodes\n%s", len(c.NodeConfig), out)
	}
	api := c.NodeConfig[0].ApiConfig
	if !api.IsMachine() || api.MachineID != 7 || api.Token() != "machine-token" || api.APIHost != "https://panel.example.com" {
		t.Errorf("machine entry: %+v", api)
	}
	if c.LogConfig.Level != "info" {
		t.Errorf("log level %q, want info", c.LogConfig.Level)
	}
	if len(c.CoresConfig) != 1 || c.CoresConfig[0].SingConfig.LogConfig.Level != "info" {
		t.Errorf("cores not kept: %+v", c.CoresConfig)
	}

	// A deliberate warn level is left alone.
	if err := os.WriteFile(existing, []byte(strings.Replace(old, `"Level": "error"`, `"Level": "warn"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = runInstaller(t, "--print-config", "--existing", existing, "--mode", "node", "--panel", "https://p.example", "--token", "t", "--node-id", "5")
	if err != nil {
		t.Fatalf("merge: %v\n%s", err, out)
	}
	if lvl := loadRendered(t, out).LogConfig.Level; lvl != "warn" {
		t.Errorf("warn level changed to %q", lvl)
	}
}

// The installer sizes Go's memory limit and a cgroup cap to the machine's RAM
// so a small VPS does not run into the kernel OOM killer.
func TestInstallScript_MemoryLimits(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	src, err := os.ReadFile("../FNode-script/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	from := strings.Index(text, "fnode_gomemlimit() {")
	to := strings.Index(text, "# Advice for a small machine")
	if from < 0 || to < from {
		t.Fatal("memory helpers not found in install.sh")
	}
	block := text[from:to]
	for _, c := range []struct{ mem, limit, unit string }{
		{"715", "500MiB", "Environment=GOMEMLIMIT=500MiB|MemoryMax=607M|"},
		{"1024", "716MiB", "Environment=GOMEMLIMIT=716MiB|MemoryMax=870M|"},
		{"1800", "1260MiB", "Environment=GOMEMLIMIT=1260MiB|"},
		{"4096", "", ""},
		{"0", "", ""},
	} {
		script := "total_mem_mb() { echo " + c.mem + "; }\n" + block +
			"\nprintf 'limit=%s\n' \"$(fnode_gomemlimit)\"; fnode_unit_memory_lines | tr '\n' '|'\n"
		out, err := exec.Command(bash, "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s MB: %v\n%s", c.mem, err, out)
		}
		if want := "limit=" + c.limit + "\n" + c.unit; string(out) != want {
			t.Errorf("%s MB: got %q, want %q", c.mem, out, want)
		}
	}
}
