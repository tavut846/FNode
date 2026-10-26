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
// (the https_port helpers and setup_caddy_reverse_proxy), CRLF stripped, with
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
	fStart := strings.Index(src, "setup_caddy_reverse_proxy() {")
	// ends where show_usage starts (the Caddyfile text inside has "}" lines at column 0)
	fEnd := strings.Index(src[fStart:], "\nshow_usage() {")
	if fStart < 0 || fEnd < 0 {
		t.Fatal("setup_caddy_reverse_proxy not found in FNode.sh")
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

// The whole wizard with stdin as the answers (no TTY, so the port is not asked).
func TestCaddyWizard(t *testing.T) {
	type tc struct {
		name     string
		existing string // existing Caddyfile ("" none)
		env      []string
		stdin    string
		check    func(t *testing.T, caddyfile, out string)
	}
	answers := "a.example.com\ntoken123\n\n" // domain, token, decoy (default)
	count := func(s, sub string) int { return strings.Count(s, sub) }
	cases := []tc{
		{"fresh install, default 8443", "", nil, answers, func(t *testing.T, f, out string) {
			if !strings.HasPrefix(f, "{\n    https_port 8443\n}\n") || count(f, "https_port") != 1 || !strings.Contains(f, "a.example.com {") {
				t.Errorf("caddyfile:\n%s", f)
			}
			if !strings.Contains(out, "Reality in the panel: Destination Site = a.example.com, port = 8443, Connection/Server port 443, flow xtls-rprx-vision, multiplex off") ||
				!strings.Contains(out, "8443 是 Caddy, 443 是 FNode") {
				t.Errorf("summary:\n%s", out)
			}
		}},
		{"env 443 writes no global option", "", []string{"CADDY_HTTPS_PORT=443"}, answers, func(t *testing.T, f, out string) {
			if strings.Contains(f, "https_port") || strings.HasPrefix(f, "{") {
				t.Errorf("caddyfile:\n%s", f)
			}
			if strings.Contains(out, "Reality in the panel") {
				t.Errorf("no Reality line for 443:\n%s", out)
			}
		}},
		{"env port wins", "", []string{"CADDY_HTTPS_PORT=9443"}, answers, func(t *testing.T, f, out string) {
			if !strings.Contains(f, "https_port 9443") || !strings.Contains(out, "port = 9443") {
				t.Errorf("caddyfile:\n%s\n%s", f, out)
			}
		}},
		{"existing 443 Caddyfile is kept on 443", "old.example.com {\n    respond ok\n}\n", nil, answers, func(t *testing.T, f, out string) {
			if strings.Contains(f, "https_port") || !strings.Contains(out, "Caddy HTTPS 端口: 443") {
				t.Errorf("must stay on 443:\n%s\n%s", f, out)
			}
		}},
		{"existing https_port is kept", "{\n    https_port 9443\n}\nold.example.com {\n}\n", nil, answers, func(t *testing.T, f, out string) {
			if !strings.HasPrefix(f, "{\n    https_port 9443\n}\n") || count(f, "https_port") != 1 {
				t.Errorf("caddyfile:\n%s", f)
			}
		}},
		{"append keeps one global block", "{\n    email a@b.c\n}\nold.example.com {\n    respond ok\n}\n", []string{"CADDY_HTTPS_PORT=8443"}, answers + "2\n", func(t *testing.T, f, out string) {
			if count(f, "https_port") != 1 || !strings.HasPrefix(f, "{\n    https_port 8443\n    email a@b.c\n}\n") ||
				!strings.Contains(f, "old.example.com {") || !strings.Contains(f, "a.example.com {") {
				t.Errorf("caddyfile:\n%s", f)
			}
			if len(regexp.MustCompile(`(?m)^\{\s*$`).FindAllString(f, -1)) != 1 {
				t.Errorf("exactly one global block expected:\n%s", f)
			}
		}},
		{"append with an unsafe global block keeps it and says so", "{ debug }\nold.example.com {\n}\n", []string{"CADDY_HTTPS_PORT=8443"}, answers + "2\n", func(t *testing.T, f, out string) {
			if !strings.HasPrefix(f, "{ debug }\n") || strings.Contains(f, "https_port") || !strings.Contains(out, "https_port 8443") || !strings.Contains(out, "Caddy HTTPS 端口: 443") {
				t.Errorf("caddyfile:\n%s\n%s", f, out)
			}
		}},
		{"invalid env port is refused", "", []string{"CADDY_HTTPS_PORT=99999"}, answers, func(t *testing.T, f, out string) {
			if f != "" || !strings.Contains(out, "必须是 1-65535") {
				t.Errorf("caddyfile %q\n%s", f, out)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "caddy"), 0o755); err != nil {
				t.Fatal(err)
			}
			cf := filepath.Join(dir, "caddy", "Caddyfile")
			if c.existing != "" {
				if err := os.WriteFile(cf, []byte(c.existing), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			bin := t.TempDir()
			for _, name := range []string{"caddy", "systemctl", "service", "sleep"} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			env := append([]string{"PATH=" + filepath.ToSlash(bin) + string(os.PathListSeparator) + os.Getenv("PATH")}, c.env...)
			out, _ := bashRun(t, caddyScript(t, dir)+"\nsetup_caddy_reverse_proxy 0", env, c.stdin)
			got, _ := os.ReadFile(cf)
			c.check(t, string(got), out)
		})
	}
}
