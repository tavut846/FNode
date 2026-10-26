package conf

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// caddyScript returns the part of FNode-script/FNode.sh the Caddy wizard needs
// (the https_port helpers and set_caddy_port), CRLF stripped, with
// /etc/caddy and /etc/FNode redirected into dir.
func caddyScript(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile("../FNode-script/FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	hStart := strings.Index(src, "# ---- caddy https_port helpers")
	hEndMarker := "# ---- end caddy https_port helpers ----"
	hEnd := strings.Index(src, hEndMarker)
	if hStart < 0 || hEnd < 0 {
		t.Fatal("https_port helpers not found in FNode.sh")
	}
	helpers := src[hStart : hEnd+len(hEndMarker)]
	fStart := strings.Index(src, "set_caddy_port() {")
	// ends where show_usage starts
	fEnd := strings.Index(src[fStart:], "\nshow_usage() {")
	if fStart < 0 || fEnd < 0 {
		t.Fatal("set_caddy_port not found in FNode.sh")
	}
	fn := src[fStart : fStart+fEnd]
	out := "green=''; red=''; yellow=''; plain=''; release=debian\nbefore_show_menu() { :; }\n" + helpers + "\n" + fn
	out = strings.ReplaceAll(out, "/etc/caddy", filepath.ToSlash(dir)+"/caddy")
	out = strings.ReplaceAll(out, "/etc/FNode", filepath.ToSlash(dir)+"/FNode")
	return out
}

func bashRun(t *testing.T, script string, env []string, stdin string) (string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	// via a file: Windows argv quoting cannot carry a script with quotes into msys bash
	file := filepath.Join(t.TempDir(), "run.sh")
	if err = os.WriteFile(file, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, file)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const caddySites = `a.example.com {
    reverse_proxy https://decoy.example.com
}
`

func writeCaddyfile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCaddyHelpers(t *testing.T) {
	script := caddyScript(t, t.TempDir())
	run := func(body string) string {
		t.Helper()
		out, err := bashRun(t, script+"\n"+body, nil, "")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return out
	}

	// port validation
	for p, want := range map[string]string{"8443": "ok", "443": "ok", "1": "ok", "65535": "ok", "0": "bad", "65536": "bad", "abc": "bad", "": "bad", "80a": "bad", "-1": "bad"} {
		got := strings.TrimSpace(run(`if caddy_valid_port "` + p + `"; then echo ok; else echo bad; fi`))
		if got != want {
			t.Errorf("caddy_valid_port(%q) = %s, want %s", p, got, want)
		}
	}

	// the global block
	if got := run(`caddy_global_block 443`); got != "" {
		t.Errorf("443 writes no global block: %q", got)
	}
	if got := run(`caddy_global_block 8443`); got != "{\n    https_port 8443\n}\n\n" {
		t.Errorf("global block: %q", got)
	}

	// state and current port
	cases := []struct {
		name, content, state, port string
	}{
		{"sites only", caddySites, "none", ""},
		{"empty", "", "none", ""},
		{"comment then site", "# my caddy\n\n" + caddySites, "none", ""},
		{"global with port", "{\n    email a@b.c\n    https_port 9443\n}\n\n" + caddySites, "block", "9443"},
		{"global without port", "{\n    email a@b.c\n}\n" + caddySites, "block", ""},
		{"nested port is not the global one", "{\n    servers {\n        https_port 1234\n    }\n}\n" + caddySites, "block", ""},
		{"comment before global", "# top\n{\n    https_port 7443\n}\n" + caddySites, "block", "7443"},
		{"one-line global", "{ debug }\n" + caddySites, "unsafe", ""},
		{"unterminated global", "{\n    email a@b.c\n" + caddySites, "unsafe", ""},
	}
	for _, c := range cases {
		f := writeCaddyfile(t, c.content)
		out := run(`caddy_global_state "` + filepath.ToSlash(f) + `"; caddy_current_port "` + filepath.ToSlash(f) + `"`)
		lines := strings.Fields(out)
		gotState, gotPort := "", ""
		if len(lines) > 0 {
			gotState = lines[0]
		}
		if len(lines) > 1 {
			gotPort = lines[1]
		}
		if c.name == "unterminated global" && gotState == "unsafe" {
			// the port scan stops at the end of the file too
			gotPort = ""
		}
		if gotState != c.state || gotPort != c.port {
			t.Errorf("%s: state=%q port=%q, want %q %q", c.name, gotState, gotPort, c.state, c.port)
		}
	}
}

func TestCaddySetHTTPSPort(t *testing.T) {
	script := caddyScript(t, t.TempDir())
	set := func(content, port string) (string, int) {
		t.Helper()
		f := writeCaddyfile(t, content)
		p := filepath.ToSlash(f)
		out, err := bashRun(t, script+"\ncaddy_set_https_port \""+p+"\" "+port+"; echo \"rc=$?\" >&2", nil, "")
		rc := 0
		if strings.Contains(out, "rc=1") {
			rc = 1
		}
		_ = err
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		return string(b), rc
	}
	globals := func(s string) int { return len(regexp.MustCompile(`(?m)^\{\s*$`).FindAllString(s, -1)) }

	// no global block: one is created at the top, once
	got, rc := set(caddySites, "8443")
	if rc != 0 || got != "{\n    https_port 8443\n}\n\n"+caddySites {
		t.Errorf("create: rc=%d\n%s", rc, got)
	}
	again, _ := set(got, "8443")
	if again != got {
		t.Errorf("not idempotent:\n%s", again)
	}
	// 443 adds nothing
	if got, rc = set(caddySites, "443"); rc != 0 || got != caddySites {
		t.Errorf("443 on a plain file: rc=%d\n%s", rc, got)
	}

	// an existing global block gets the option inserted, never a second block
	got, rc = set("{\n    email a@b.c\n}\n\n"+caddySites, "8443")
	if rc != 0 || globals(got) != 1 || strings.Count(got, "https_port") != 1 || !strings.HasPrefix(got, "{\n    https_port 8443\n    email a@b.c\n}") {
		t.Errorf("insert: rc=%d\n%s", rc, got)
	}
	// ... and replaces its https_port line
	got, rc = set("{\n    https_port 9443\n    email a@b.c\n}\n\n"+caddySites, "8443")
	if rc != 0 || strings.Count(got, "https_port") != 1 || !strings.Contains(got, "https_port 8443") || strings.Contains(got, "9443") || !strings.Contains(got, "email a@b.c") {
		t.Errorf("replace: rc=%d\n%s", rc, got)
	}
	// 443 removes the option but keeps the rest
	got, rc = set("{\n    https_port 9443\n    email a@b.c\n}\n\n"+caddySites, "443")
	if rc != 0 || strings.Contains(got, "https_port") || !strings.Contains(got, "email a@b.c") || globals(got) != 1 {
		t.Errorf("remove: rc=%d\n%s", rc, got)
	}
	// a nested https_port is left alone, the global one is inserted
	got, rc = set("{\n    servers {\n        https_port 1234\n    }\n}\n"+caddySites, "8443")
	if rc != 0 || !strings.Contains(got, "https_port 1234") || !strings.HasPrefix(got, "{\n    https_port 8443\n    servers {") {
		t.Errorf("nested: rc=%d\n%s", rc, got)
	}
	// a comment line before the block
	got, rc = set("# top\n{\n    email a@b.c\n}\n"+caddySites, "8443")
	if rc != 0 || globals(got) != 1 || strings.Count(got, "https_port") != 1 || !strings.HasPrefix(got, "# top\n{\n    https_port 8443\n") {
		t.Errorf("comment first: rc=%d\n%s", rc, got)
	}

	// what cannot be edited safely stays as it is
	for name, content := range map[string]string{
		"one-line global": "{ debug }\n" + caddySites,
		"unterminated":    "{\n    email a@b.c\n" + caddySites,
	} {
		got, rc = set(content, "8443")
		if rc != 1 || got != content {
			t.Errorf("%s: rc=%d, file must stay as is:\n%s", name, rc, got)
		}
	}
}

// ss is faked: a port held by nginx is a conflict, one held by caddy is not.
func TestCaddyPortConflict(t *testing.T) {
	bin := t.TempDir()
	ss := "#!/bin/sh\ncat <<'EOF'\n" +
		"LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:* users:((\"nginx\",pid=11,fd=6))\n" +
		"LISTEN 0 4096 [::]:9443 [::]:* users:((\"caddy\",pid=12,fd=7))\n" +
		"LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:((\"FNode\",pid=13,fd=8))\n" +
		"LISTEN 0 4096 0.0.0.0:18443 0.0.0.0:*\nEOF\n"
	if err := os.WriteFile(filepath.Join(bin, "ss"), []byte(ss), 0o755); err != nil {
		t.Fatal(err)
	}
	script := caddyScript(t, t.TempDir())
	env := []string{"PATH=" + filepath.ToSlash(bin) + string(os.PathListSeparator) + os.Getenv("PATH")}
	for port, want := range map[string]string{"8443": "nginx", "9443": "", "443": "FNode", "7443": "", "18443": "unknown"} {
		out, err := bashRun(t, script+"\ncaddy_port_conflict "+port, env, "")
		if err != nil {
			t.Fatal(err, out)
		}
		if got := strings.TrimSpace(out); got != want {
			t.Errorf("port %s: %q, want %q", port, got, want)
		}
	}
}
