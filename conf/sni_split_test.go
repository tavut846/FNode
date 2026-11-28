package conf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sniScript returns the side-effect-free sni-split helpers of FNode.sh.
func sniScript(t *testing.T) string {
	t.Helper()
	raw, err := readScript(t, "FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	a := strings.Index(src, "# ---- sni-split helpers")
	endMarker := "# ---- end sni-split helpers ----"
	b := strings.Index(src, endMarker)
	if a < 0 || b < 0 {
		t.Fatal("sni-split helpers not found in FNode.sh")
	}
	return src[a : b+len(endMarker)]
}

func TestSniSplit_ConfIsGeneratedAndParsedBack(t *testing.T) {
	f := filepath.ToSlash(filepath.Join(t.TempDir(), "fnode-stream.conf"))
	out, err := bashRun(t, sniScript(t)+"\nsni_split_conf 'JP-J35C-N.888833.best, b.example.com jp-j35c-n.888833.best' 8444 8443 0 > '"+f+"'\ncat '"+f+"'\nsni_split_parse '"+f+"'", nil, "")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, want := range []string{
		"stream {",
		"map $ssl_preread_server_name $fnode_sni_backend {",
		"hostnames;",
		"        jp-j35c-n.888833.best 127.0.0.1:8444;",
		"        b.example.com 127.0.0.1:8444;",
		"        default 127.0.0.1:8443;",
		"listen 443;",
		"ssl_preread on;",
		"proxy_pass $fnode_sni_backend;",
		"snis=jp-j35c-n.888833.best,b.example.com",
		"anytls_port=8444",
		"default_port=8443",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "jp-j35c-n.888833.best 127.0.0.1") != 1 {
		t.Errorf("duplicate or non-lowercased SNI:\n%s", out)
	}
	if strings.Contains(out, "[::]:443") {
		t.Errorf("no IPv6 listener expected with ipv6=0:\n%s", out)
	}
}

func TestSniSplit_RealityCombinedAndIPv6(t *testing.T) {
	out, err := bashRun(t, sniScript(t)+"\nsni_split_conf a.example.com 8444 8445 1", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "default 127.0.0.1:8445;") || !strings.Contains(out, "listen [::]:443;") {
		t.Errorf("combined mode / ipv6:\n%s", out)
	}
}

func TestSniSplit_DomainValidation(t *testing.T) {
	script := sniScript(t) + `
for d in jp-j35c-n.888833.best a.b.example.com '*.example.com' localhost 'a b.com' 'x;y.com' '-a.com' 'a.com}' ; do
    if sni_valid_domain "$d"; then echo "ok:$d"; else echo "bad:$d"; fi
done`
	out, err := bashRun(t, script, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ok:jp-j35c-n.888833.best", "ok:a.b.example.com", "ok:*.example.com", "bad:localhost", "bad:a b.com", "bad:x;y.com", "bad:-a.com", "bad:a.com}"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func TestSniSplit_IncludeIsAddedOnceAndRemoved(t *testing.T) {
	dir := t.TempDir()
	nginx := filepath.ToSlash(filepath.Join(dir, "nginx.conf"))
	const orig = "user www-data;\nhttp {\n    server { listen 80; }\n}"
	os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte(orig), 0o644) // no trailing newline on purpose
	script := sniScript(t) + `
inc=/etc/nginx/fnode-stream.conf
sni_split_with_include '` + nginx + `' $inc > '` + nginx + `.1'
cat '` + nginx + `.1' > '` + nginx + `'
sni_split_with_include '` + nginx + `' $inc > '` + nginx + `.2'
cmp '` + nginx + `.1' '` + nginx + `.2' && echo IDEMPOTENT
grep -c "include $inc;" '` + nginx + `.2'
sni_split_has_include '` + nginx + `' $inc && echo HAS
sni_split_without_include '` + nginx + `.2' $inc > '` + nginx + `.3'
sni_split_has_include '` + nginx + `.3' $inc || echo GONE`
	out, err := bashRun(t, script, nil, "")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, want := range []string{"IDEMPOTENT", "\n1\n", "HAS", "GONE"} {
		if !strings.Contains("\n"+out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	got, _ := os.ReadFile(filepath.Join(dir, "nginx.conf.3"))
	if strings.TrimSpace(string(got)) != orig {
		t.Errorf("removing the include must give back the original:\n%q", got)
	}
	// the include sits at the top level, after http {}
	with, _ := os.ReadFile(filepath.Join(dir, "nginx.conf.2"))
	if i, j := strings.LastIndex(string(with), "}"), strings.Index(string(with), "include /etc/nginx/fnode-stream.conf;"); j < i {
		t.Errorf("include must come after the http block:\n%s", with)
	}
}

func TestSniSplit_ScriptWiring(t *testing.T) {
	raw, err := readScript(t, "FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	for _, want := range []string{
		`"sni-split"|"sni_split") sni_split 0 "${@:2}" || exit 1 ;;`,
		`21) sni_split_menu ;;`,
		"FNode sni-split [on|off|status]",
		"sni_remove_redirect",
		"nginx -t",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("FNode.sh is missing %q", want)
		}
	}
	// coreutils install must not be called bare in this script (it defines install())
	if strings.Contains(src, "\n    install -m") {
		t.Error("bare install call")
	}
}

// sniRun runs the real sni_split_* functions against a fake /etc/nginx and
// /etc/caddy in a temp dir, with stubbed nginx, ss, systemctl and iptables.
func sniRun(t *testing.T, dir, nginxBody, ssOut, extra string) (string, error) {
	t.Helper()
	raw, err := readScript(t, "FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	a := strings.Index(src, "# ---- sni-split helpers")
	b := strings.Index(src, "\nshow_usage() {")
	if a < 0 || b < 0 {
		t.Fatal("sni-split code not found")
	}
	code := src[a:b]
	// the Caddy https_port helpers are needed too
	h0 := strings.Index(src, "# ---- caddy https_port helpers")
	h1 := strings.Index(src, "# ---- end caddy https_port helpers ----")
	code = src[h0:h1] + "\n" + code
	d := filepath.ToSlash(dir)
	code = strings.ReplaceAll(code, "/etc/nginx", d+"/nginx")
	code = strings.ReplaceAll(code, "/etc/caddy", d+"/caddy")
	fakeBin(t, dir, "dpkg", `exit 0`)
	fakeBin(t, dir, "nginx", nginxBody)
	fakeBin(t, dir, "ss", ssOut)
	fakeBin(t, dir, "systemctl", `[ "$1" = is-active ] && exit 3; exit 0`)
	fakeBin(t, dir, "iptables", `[ "$2" = nat ] && [ "$3" = -S ] && echo "-A PREROUTING -p tcp -m tcp --dport 443 -j REDIRECT --to-ports 8444"; echo "iptables $*" >> `+d+`/ipt.log; exit 0`)
	pre := "green=''; red=''; yellow=''; plain=''; release=debian\ncheck_ipv6_support() { echo 0; }\nbefore_show_menu() { :; }\n" +
		"PATH=\"$(cygpath -u '" + d + "/bin' 2>/dev/null || echo '" + d + "/bin'):$PATH\"\n"
	return bashRun(t, pre+code+"\n"+extra, nil, "")
}

func setupSniDir(t *testing.T, caddyfile string) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "nginx"), 0o755)
	os.MkdirAll(filepath.Join(dir, "caddy"), 0o755)
	os.WriteFile(filepath.Join(dir, "nginx", "nginx.conf"), []byte("events {}\nhttp {}\n"), 0o644)
	if caddyfile != "" {
		os.WriteFile(filepath.Join(dir, "caddy", "Caddyfile"), []byte(caddyfile), 0o644)
	}
	return dir
}

const caddy8443 = "{\n    https_port 8443\n}\nsite.example.com {\n    respond ok\n}\n"

func TestSniSplit_OnWritesFilesRemovesRedirectAndOffRestores(t *testing.T) {
	dir := setupSniDir(t, caddy8443)
	out, err := sniRun(t, dir, `exit 0`, `exit 0`,
		"sni_split_on --sni jp-j35c-n.888833.best --yes; echo rc=$?\nsni_split_status\nsni_split_off; echo off-rc=$?")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, want := range []string{
		"rc=0",
		"SNI jp-j35c-n.888833.best -> 127.0.0.1:8444",
		"其他/无 SNI -> 127.0.0.1:8443",
		"Server port) 8444",
		"已删除",
		"off-rc=0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	ipt, _ := os.ReadFile(filepath.Join(dir, "ipt.log"))
	if !strings.Contains(string(ipt), "-D PREROUTING") || !strings.Contains(string(ipt), "--to-ports 8444") {
		t.Errorf("the REDIRECT rule must be deleted: %q", ipt)
	}
	conf, _ := os.ReadFile(filepath.Join(dir, "nginx", "nginx.conf"))
	if strings.Contains(string(conf), "fnode-stream") {
		t.Errorf("off must remove the include:\n%s", conf)
	}
	if _, err := os.Stat(filepath.Join(dir, "nginx", "fnode-stream.conf")); err == nil {
		t.Error("off must move the stream file away")
	}
}

func TestSniSplit_RealityCombinedRoutesDefaultToReality(t *testing.T) {
	dir := setupSniDir(t, caddy8443)
	out, err := sniRun(t, dir, `exit 0`, `exit 0`, "sni_split_on --sni a.example.com --reality-port 8445 --yes; echo rc=$?")
	if err != nil || !strings.Contains(out, "rc=0") {
		t.Fatalf("%v: %s", err, out)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "nginx", "fnode-stream.conf"))
	if !strings.Contains(string(got), "default 127.0.0.1:8445;") || !strings.Contains(string(got), "a.example.com 127.0.0.1:8444;") {
		t.Errorf("combined routing:\n%s", got)
	}
	if !strings.Contains(out, "Server port) 8444") || !strings.Contains(out, "服务端口 8445") {
		t.Errorf("panel settings missing:\n%s", out)
	}
}

func TestSniSplit_NginxTestFailureRollsBack(t *testing.T) {
	dir := setupSniDir(t, caddy8443)
	before, _ := os.ReadFile(filepath.Join(dir, "nginx", "nginx.conf"))
	out, err := sniRun(t, dir, `[ "$1" = -t ] && { echo 'nginx: [emerg] unknown directive "stream"' >&2; exit 1; }; exit 0`, `exit 0`,
		"sni_split_on --sni a.example.com --yes; echo rc=$?")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(out, "rc=1") || !strings.Contains(out, "已恢复原配置") || !strings.Contains(out, "libnginx-mod-stream") {
		t.Errorf("failure must roll back and explain:\n%s", out)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "nginx", "nginx.conf"))
	if string(after) != string(before) {
		t.Errorf("nginx.conf must be restored:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(dir, "nginx", "fnode-stream.conf")); err == nil {
		t.Error("the new stream file must be removed on rollback")
	}
}

func TestSniSplit_RefusesWhenAnotherServiceHolds443(t *testing.T) {
	cases := map[string]string{
		`LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:(("FNode",pid=1,fd=7))`:  "443 现在由 FNode 自己监听",
		`LISTEN 0 4096 *:443 *:* users:(("caddy",pid=1,fd=7))`:              "FNode caddy-port 8443",
		`LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:(("openresty",pid=1))`: "已被其他进程占用",
		`LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:(("nginx",pid=1))`:     "不是本脚本配置的",
	}
	for ss, want := range cases {
		dir := setupSniDir(t, caddy8443)
		out, err := sniRun(t, dir, `exit 0`, "echo '"+ss+"'", "sni_split_on --sni a.example.com --yes; echo rc=$?")
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		if !strings.Contains(out, "rc=1") || !strings.Contains(out, want) {
			t.Errorf("%s: want %q in:\n%s", ss, want, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "nginx", "fnode-stream.conf")); err == nil {
			t.Errorf("%s: nothing may be written", ss)
		}
	}
}

func TestSniSplit_CaddyStillOn443IsRefused(t *testing.T) {
	dir := setupSniDir(t, "site.example.com {\n    respond ok\n}\n")
	out, _ := sniRun(t, dir, `exit 0`, `exit 0`, "sni_split_on --sni a.example.com --yes; echo rc=$?")
	if !strings.Contains(out, "rc=1") || !strings.Contains(out, "FNode caddy-port 8443") {
		t.Errorf("Caddy on 443 must be refused:\n%s", out)
	}
}
