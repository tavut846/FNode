package conf

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// caddyInstallScript returns the "caddy install helpers" part of FNode.sh with
// /etc/caddy and /etc/systemd/system redirected into dir, a fake bin dir first
// in PATH (caddy, systemctl) and the given extra shell appended.
func caddyInstallScript(t *testing.T, dir, extra string) string {
	t.Helper()
	raw, err := os.ReadFile("../FNode-script/FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	a := strings.Index(src, "# ---- caddy install helpers")
	endMarker := "# ---- end caddy install helpers ----"
	b := strings.Index(src, endMarker)
	if a < 0 || b < 0 {
		t.Fatal("caddy install helpers not found in FNode.sh")
	}
	helpers := src[a : b+len(endMarker)]
	d := filepath.ToSlash(dir)
	helpers = strings.ReplaceAll(helpers, "/etc/caddy", d+"/caddy")
	helpers = strings.ReplaceAll(helpers, "/etc/systemd/system", d+"/systemd")
	return "green=''; red=''; yellow=''; plain=''; release=debian\nPATH=\"$(cygpath -u '" + d + "/bin' 2>/dev/null || echo '" + d + "/bin'):$PATH\"\n" + helpers + "\n" + extra
}

func fakeBin(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, "bin")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, name), []byte("#!/bin/bash\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCaddyHasCF_ChecksTheModuleNotJustTheBinary(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "caddy", `[ "$1" = list-modules ] && printf 'http.handlers.file_server\ntls.issuance.acme\n'`)
	out, err := bashRun(t, caddyInstallScript(t, dir, `caddy_has_cf && echo HAS || echo NO`), nil, "")
	if err != nil || !strings.Contains(out, "NO") {
		t.Fatalf("a standard caddy must not count as ready: %q %v", out, err)
	}
	fakeBin(t, dir, "caddy", `[ "$1" = list-modules ] && printf 'dns.providers.cloudflare\nhttp.handlers.file_server\n'`)
	out, err = bashRun(t, caddyInstallScript(t, dir, `caddy_has_cf && echo HAS || echo NO`), nil, "")
	if err != nil || !strings.Contains(out, "HAS") {
		t.Fatalf("a caddy with the module must count as ready: %q %v", out, err)
	}
}

func TestCaddyEnsure_ReinstallsStandardBuildAndStopsWhenNoModuleBuild(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "caddy", `[ "$1" = list-modules ] && echo http.handlers.file_server`)
	out, _ := bashRun(t, caddyInstallScript(t, dir, `install_caddy() { echo INSTALL_CALLED; return 1; }
caddy_ensure && echo READY || echo "STOPPED rc=$?"`), nil, "")
	if !strings.Contains(out, "INSTALL_CALLED") || !strings.Contains(out, "STOPPED") || strings.Contains(out, "READY") {
		t.Fatalf("a standard caddy must trigger the install and a failed install must stop: %q", out)
	}
}

func TestCaddyValidate_FailureIsReportedAndTokenMasked(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "caddy"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "caddy", "caddy.env"), []byte("CLOUDFLARE_API_TOKEN=sEcReT-tok.123\n"), 0o600)
	fakeBin(t, dir, "caddy", `echo "adapting config using caddyfile: parsing Caddyfile:5 token sEcReT-tok.123" >&2; exit 1`)
	out, err := bashRun(t, caddyInstallScript(t, dir, `caddy_validate; echo "rc=$?"`), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rc=1") || !strings.Contains(out, "Caddyfile:5") {
		t.Errorf("validation failure must be returned with Caddy's error: %q", out)
	}
	if strings.Contains(out, "sEcReT-tok.123") {
		t.Errorf("token leaked: %q", out)
	}
}

func TestCaddyRestartChecked_InactiveServiceFailsWithJournal(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, "systemctl", `[ "$1" = is-active ] && exit 3; exit 0`)
	fakeBin(t, dir, "journalctl", `echo "caddy[1]: boom"`)
	fakeBin(t, dir, "sleep", `exit 0`)
	out, err := bashRun(t, caddyInstallScript(t, dir, `caddy_restart_checked && echo OK || echo "FAILED"`), nil, "")
	if err != nil || !strings.Contains(out, "FAILED") || !strings.Contains(out, "caddy[1]: boom") || strings.Contains(out, "OK\n") {
		t.Fatalf("an inactive caddy must fail and show the journal: %q %v", out, err)
	}
	fakeBin(t, dir, "systemctl", `exit 0`)
	out, err = bashRun(t, caddyInstallScript(t, dir, `caddy_restart_checked && echo OK || echo "FAILED"`), nil, "")
	if err != nil || !strings.Contains(out, "OK") {
		t.Fatalf("an active caddy must pass: %q %v", out, err)
	}
}

func TestCaddyFixUnit_DropsEnviron(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "systemd"), 0o755)
	unit := filepath.Join(dir, "systemd", "caddy.service")
	os.WriteFile(unit, []byte("[Service]\nExecStart=/usr/bin/caddy run --environ --config /etc/caddy/Caddyfile\n"), 0o644)
	fakeBin(t, dir, "systemctl", `exit 0`)
	if out, err := bashRun(t, caddyInstallScript(t, dir, `caddy_fix_unit`), nil, ""); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	b, _ := os.ReadFile(unit)
	if strings.Contains(string(b), "--environ") || !strings.Contains(string(b), "caddy run --config") {
		t.Errorf("unit still dumps the environment: %s", b)
	}
}

func TestInstallCaddy_NeverFallsBackToStandardAndUnitHasNoEnviron(t *testing.T) {
	raw, err := os.ReadFile("../FNode-script/FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	if regexp.MustCompile(`(?m)^ExecStart=.*--environ`).MatchString(src) {
		t.Error("the systemd unit must not use --environ (it logs the Cloudflare token)")
	}
	if strings.Contains(src, "apt install -y caddy") || strings.Contains(src, "copr enable") {
		t.Error("install_caddy must not fall back to the standard Caddy package")
	}
}

// {env.CLOUDFLARE_API_TOKEN} only exists in the service environment: validation
// must see the token from caddy.env, and never print it.
func TestCaddyValidate_PassesTokenFromEnvFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "caddy"), 0o755)
	os.WriteFile(filepath.Join(dir, "caddy", "caddy.env"), []byte("CLOUDFLARE_API_TOKEN=tok-abc_123\n"), 0o600)
	fakeBin(t, dir, "caddy", `[ -n "$CLOUDFLARE_API_TOKEN" ] || { echo "API token '' appears invalid" >&2; exit 1; }; echo "Valid configuration, token $CLOUDFLARE_API_TOKEN"`)
	out, err := bashRun(t, caddyInstallScript(t, dir, `caddy_validate; echo "rc=$?"`), nil, "")
	if err != nil || !strings.Contains(out, "rc=0") || strings.Contains(out, "tok-abc_123") {
		t.Fatalf("validate must succeed with the env-file token and print nothing: %q %v", out, err)
	}
}
