package conf

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The existing wizard keeps today's behaviour: no port prompt, no global block.
func TestCaddyWizardUnchanged(t *testing.T) {
	raw, err := os.ReadFile("../FNode-script/FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	a := strings.Index(src, "setup_caddy_reverse_proxy() {")
	b := strings.Index(src, "# 修改 Caddy 的 HTTPS 端口")
	if a < 0 || b < a {
		t.Fatal("functions not found")
	}
	wizard := src[a:b]
	for _, bad := range []string{"https_port", "caddy_global", "caddy_set_https_port", "CADDY_HTTPS_PORT", "HTTPS 端口"} {
		if strings.Contains(wizard, bad) {
			t.Errorf("the Caddy wizard must not change (found %q)", bad)
		}
	}
}

func runSetPort(t *testing.T, existing, args string, env []string, failValidate bool) (caddyfile, out string, backups []string, rc int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "caddy"), 0o755); err != nil {
		t.Fatal(err)
	}
	cf := filepath.Join(dir, "caddy", "Caddyfile")
	if existing != "" {
		if err := os.WriteFile(cf, []byte(existing), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "caddy", "caddy.env"), []byte("CLOUDFLARE_API_TOKEN=tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	caddy := "#!/bin/sh\nexit 0\n"
	if failValidate {
		caddy = "#!/bin/sh\n[ \"$1\" = validate ] && exit 1\nexit 0\n"
	}
	stub := "#!/bin/sh\nexit 0\n"
	for name, body := range map[string]string{"caddy": caddy, "systemctl": stub, "service": stub, "sleep": stub} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env = append([]string{"PATH=" + filepath.ToSlash(bin) + string(os.PathListSeparator) + os.Getenv("PATH")}, env...)
	out, _ = bashRun(t, caddyScript(t, dir)+"\nset_caddy_port "+args+"; echo \"rc=$?\"", env, "")
	rc = 1
	if strings.Contains(out, "rc=0") {
		rc = 0
	}
	b, _ := os.ReadFile(cf)
	backups, _ = filepath.Glob(cf + ".bak.*")
	return string(b), out, backups, rc
}

func TestSetCaddyPort(t *testing.T) {
	globals := func(s string) int { return len(regexp.MustCompile(`(?m)^\{\s*$`).FindAllString(s, -1)) }

	// no Caddyfile: send the user to the wizard
	if f, out, bk, rc := runSetPort(t, "", "0", nil, false); rc != 1 || f != "" || len(bk) != 0 || !strings.Contains(out, "Caddy 向导") {
		t.Errorf("no Caddyfile: rc=%d\n%s", rc, out)
	}

	// sites only: the default 8443, a new global block, sites untouched, backup, Reality line
	f, out, bk, rc := runSetPort(t, caddySites, "0", nil, false)
	if rc != 0 || f != "{\n    https_port 8443\n}\n\n"+caddySites || len(bk) != 1 {
		t.Fatalf("default: rc=%d backups=%v\n%s\n%s", rc, bk, f, out)
	}
	if saved, _ := os.ReadFile(bk[0]); string(saved) != caddySites {
		t.Errorf("the backup is the old file: %q", saved)
	}
	for _, want := range []string{"当前 Caddy HTTPS 端口: 443",
		"Reality in the panel: Destination Site = a.example.com, port = 8443, Connection/Server port 443, flow xtls-rprx-vision, multiplex off. Caddy is on 8443, FNode must be the one on 443.",
		"Caddy 之前在 443"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary misses %q:\n%s", want, out)
		}
	}

	// the port as an argument and as an environment variable
	if f, _, _, rc = runSetPort(t, caddySites, "0 9443", nil, false); rc != 0 || !strings.HasPrefix(f, "{\n    https_port 9443\n}") {
		t.Errorf("argument: rc=%d\n%s", rc, f)
	}
	if f, _, _, rc = runSetPort(t, caddySites, "0", []string{"CADDY_HTTPS_PORT=7443"}, false); rc != 0 || !strings.HasPrefix(f, "{\n    https_port 7443\n}") {
		t.Errorf("env: rc=%d\n%s", rc, f)
	}

	// a global block with other options: the line is inserted, no second block
	other := "{\n    email a@b.c\n}\n\n" + caddySites
	if f, _, _, rc = runSetPort(t, other, "0 8443", nil, false); rc != 0 || globals(f) != 1 || !strings.HasPrefix(f, "{\n    https_port 8443\n    email a@b.c\n}") || !strings.HasSuffix(f, caddySites) {
		t.Errorf("insert: rc=%d\n%s", rc, f)
	}
	// one that already has https_port: replaced
	has := "{\n    https_port 9443\n    email a@b.c\n}\n\n" + caddySites
	f, out, _, rc = runSetPort(t, has, "0 8443", nil, false)
	if rc != 0 || strings.Count(f, "https_port") != 1 || !strings.Contains(f, "https_port 8443") || strings.Contains(f, "9443") || !strings.Contains(out, "当前 Caddy HTTPS 端口: 9443") || strings.Contains(out, "Caddy 之前在 443") {
		t.Errorf("replace: rc=%d\n%s\n%s", rc, f, out)
	}

	// 443 restores the default: the option goes, and the block when it is empty
	if f, _, _, rc = runSetPort(t, has, "0 443", nil, false); rc != 0 || strings.Contains(f, "https_port") || globals(f) != 1 || !strings.Contains(f, "email a@b.c") {
		t.Errorf("443 keeps the other options: rc=%d\n%s", rc, f)
	}
	only := "{\n    https_port 8443\n}\n\n" + caddySites
	f, out, _, rc = runSetPort(t, only, "0 443", nil, false)
	if rc != 0 || f != caddySites || strings.Contains(out, "Reality in the panel") {
		t.Errorf("443 removes an empty block: rc=%d\n%q\n%s", rc, f, out)
	}

	// the same port again changes nothing
	if f, out, bk, rc = runSetPort(t, only, "0 8443", nil, false); rc != 0 || f != only || len(bk) != 0 || !strings.Contains(out, "无需修改") {
		t.Errorf("same port: rc=%d\n%s", rc, out)
	}

	// a bad port
	for _, bad := range []string{"0 abc", "0 0", "0 70000"} {
		if f, out, bk, rc = runSetPort(t, caddySites, bad, nil, false); rc != 1 || f != caddySites || len(bk) != 0 || !strings.Contains(out, "1-65535") {
			t.Errorf("%s: rc=%d\n%s", bad, rc, out)
		}
	}

	// caddy validate fails: the old file is back
	f, out, _, rc = runSetPort(t, other, "0 8443", nil, true)
	if rc != 1 || f != other || !strings.Contains(out, "已恢复原 Caddyfile") {
		t.Errorf("validate failure: rc=%d\n%s\n%s", rc, f, out)
	}

	// a layout it cannot edit safely: nothing changes, the lines to add are printed
	odd := "{ debug }\n" + caddySites
	f, out, bk, rc = runSetPort(t, odd, "0 8443", nil, false)
	if rc != 1 || f != odd || len(bk) != 0 || !strings.Contains(out, "https_port 8443") {
		t.Errorf("unsafe: rc=%d\n%s\n%s", rc, f, out)
	}
}

func TestCaddyFirstDomain(t *testing.T) {
	script := caddyScript(t, t.TempDir())
	for content, want := range map[string]string{
		caddySites: "a.example.com",
		"{\n    email a@b.c\n}\n\n(snip) {\n    encode gzip\n}\nhttps://b.example.com:8443, c.example.com {\n}\n": "b.example.com",
		"# c\n:80 {\n}\n":          "",
		"{\n    https_port 1\n}\n": "",
	} {
		f := writeCaddyfile(t, content)
		out, err := bashRun(t, script+"\ncaddy_first_domain \""+filepath.ToSlash(f)+"\"", nil, "")
		if err != nil {
			t.Fatal(err, out)
		}
		if got := strings.TrimSpace(out); got != want {
			t.Errorf("%q: %q, want %q", content, got, want)
		}
	}
}
