package conf

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
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

// installHelpers returns the extract/download helper block of install.sh.
func installHelpers(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../FNode-script/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	from := strings.Index(text, "# --- extract helpers ---")
	to := strings.Index(text, "# --- end extract helpers ---")
	if from < 0 || to < from {
		t.Fatal("extract helpers not found in install.sh")
	}
	return text[from:to]
}

func runBash(t *testing.T, env []string, script string) (string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	cmd := exec.Command(bash, "-c", script)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeReleaseZip(t *testing.T, big bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "FNode-linux.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{"FNode": "#!/bin/sh\necho 1.2.3\n", "dns.json": "{}", "geoip.dat": "geo", "geosite.dat": "site"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// The release zip is unpacked with whatever tool the machine has (unzip,
// bsdtar, python3, busybox) and the unused geo files are left out.
func TestInstallScript_ExtractZip(t *testing.T) {
	zipPath := writeReleaseZip(t, false)
	dest := filepath.Join(t.TempDir(), "out")
	script := installHelpers(t) + "\nhave_extractor || { echo NO-EXTRACTOR; exit 3; }\nextract_zip '" + filepath.ToSlash(zipPath) + "' '" + filepath.ToSlash(dest) + "'\n"
	out, err := runBash(t, nil, script)
	if err != nil {
		if strings.Contains(out, "NO-EXTRACTOR") {
			t.Skip("no zip extractor on this machine")
		}
		t.Fatalf("extract_zip: %v\n%s", err, out)
	}
	for _, want := range []string{"FNode", "dns.json"} {
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Errorf("%s not extracted: %v", want, err)
		}
	}
	for _, skip := range []string{"geoip.dat", "geosite.dat"} {
		if _, err := os.Stat(filepath.Join(dest, skip)); err == nil {
			t.Errorf("%s must not be extracted", skip)
		}
	}
}

// Without any extractor the helper reports it instead of pretending.
func TestInstallScript_NoExtractor(t *testing.T) {
	zipPath := writeReleaseZip(t, false)
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	script := installHelpers(t) + "\nhave_extractor && echo HAVE; have_downloader && echo HAVE-DL\nextract_zip '" + filepath.ToSlash(zipPath) + "' /nonexistent-dest; echo rc=$?\n"
	out, err := runBash(t, []string{"PATH=/nonexistent"}, script)
	_ = bash
	if err != nil && !strings.Contains(out, "rc=") {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "HAVE") || !strings.Contains(out, "rc=1") {
		t.Errorf("with an empty PATH there is no extractor or downloader and extract_zip must fail:\n%s", out)
	}
}

// download_file fails on an HTTP error and saves a good response.
func TestInstallScript_DownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.zip" {
			_, _ = w.Write([]byte("payload"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	dir := t.TempDir()
	good := filepath.ToSlash(filepath.Join(dir, "good"))
	bad := filepath.ToSlash(filepath.Join(dir, "bad"))
	script := installHelpers(t) + "\nhave_downloader || { echo NO-DL; exit 3; }\n" +
		"download_file " + srv.URL + "/ok.zip '" + good + "' >/dev/null 2>&1 && echo GOOD-OK\n" +
		"download_file " + srv.URL + "/missing.zip '" + bad + "' >/dev/null 2>&1 || echo BAD-FAILED\n"
	out, err := runBash(t, nil, script)
	if err != nil {
		if strings.Contains(out, "NO-DL") {
			t.Skip("no wget or curl")
		}
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "GOOD-OK") || !strings.Contains(out, "BAD-FAILED") {
		t.Errorf("download_file results:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.FromSlash(good)); string(b) != "payload" {
		t.Errorf("downloaded content %q", b)
	}
}

// The unit must not restart forever when the binary is missing, and the
// install must verify the binary before it replaces the old one.
func TestInstallScript_GuardsBrokenInstall(t *testing.T) {
	src, err := os.ReadFile("../FNode-script/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	for _, want := range []string{
		"ConditionPathIsExecutable=/usr/local/FNode/FNode",
		`"$stage/FNode" version --short`,
		"Nothing was changed",
		"local started=$?",
		"start_pre()",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("install.sh misses %q", want)
		}
	}
	// the old install is removed only after the new one is verified
	if strings.Index(text, "rm -rf /usr/local/FNode/\n") >= 0 {
		t.Error("install.sh must not delete /usr/local/FNode before the download is verified")
	}
	if strings.Index(text, `version --short`) > strings.Index(text, "mv /usr/local/FNode /usr/local/FNode.old") {
		t.Error("the binary must be verified before the old install is moved away")
	}
}
