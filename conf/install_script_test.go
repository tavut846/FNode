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
