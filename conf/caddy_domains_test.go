package conf

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// domainsScript returns the https_port helpers (caddy_current_port) plus the
// read-only "Caddy 域名与证书" view of FNode.sh, with /etc/caddy moved into dir.
func domainsScript(t *testing.T, dir string) string {
	t.Helper()
	raw, err := readScript(t, "FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	cut := func(start, end string) string {
		a := strings.Index(src, start)
		b := strings.Index(src, end)
		if a < 0 || b < a {
			t.Fatalf("%q ... %q not found in FNode.sh", start, end)
		}
		return src[a : b+len(end)]
	}
	helpers := cut("# ---- caddy https_port helpers", "# ---- end caddy https_port helpers ----")
	view := cut("# ---- caddy domains and certificates view", "# ---- end caddy domains and certificates view ----")
	out := "green=''; red=''; yellow=''; plain=''\nbefore_show_menu() { echo MENU_RETURN; }\n" + helpers + "\n" + view + "\n"
	return strings.ReplaceAll(out, "/etc/caddy", filepath.ToSlash(dir)+"/caddy")
}

// writeCert writes a self-signed certificate for host to <root>/certificates/<issuerDir>/<name>/<name>.crt.
// It expires `left` from now (negative = already expired).
func writeCert(t *testing.T, root, issuerDir, host, org string, left time.Duration) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "E5", Organization: []string{org}},
		NotBefore:    now.Add(-100 * 24 * time.Hour),
		NotAfter:     now.Add(left),
		DNSNames:     []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	name := host
	if strings.HasPrefix(host, "*.") {
		name = "wildcard_." + host[2:]
	}
	dir := filepath.Join(root, "certificates", issuerDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, name+".crt"), pemBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".key"), []byte("PRIVATE KEY MATERIAL"), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	leDir     = "acme-v02.api.letsencrypt.org-directory"
	zeroSSL   = "acme.zerossl.com-v2-dv90"
	domainsCF = `{
    https_port 8443
}

(snippet) {
    encode gzip
}

a.example.com, www.a.example.com {
    import snippet
    tls {
        dns cloudflare {env.CLOUDFLARE_API_TOKEN}
    }
    reverse_proxy https://simulate-hub.316293.xyz {
        header_up Host simulate-hub.316293.xyz
    }
}

https://b.example.com:8443 {
    @ws path /ws
    reverse_proxy https://simulate-news.316293.xyz
}
c.example.com {
    respond "hello"
}
x.wild.org {
    reverse_proxy 127.0.0.1:9000
}
`
)

type domainsEnv struct {
	dir, data string
}

func newDomainsEnv(t *testing.T, caddyfile string) domainsEnv {
	t.Helper()
	e := domainsEnv{dir: t.TempDir(), data: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(e.dir, "caddy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if caddyfile != "" {
		if err := os.WriteFile(filepath.Join(e.dir, "caddy", "Caddyfile"), []byte(caddyfile), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e domainsEnv) run(t *testing.T, extraEnv ...string) string {
	t.Helper()
	env := append([]string{"CADDY_DATA_DIR=" + e.data}, extraEnv...)
	out, err := bashRun(t, domainsScript(t, e.dir)+"\ncaddy_domains\necho \"rc=$?\"\n", env, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return out
}

func needOpenssl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not available")
	}
}

func rowFor(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			return l
		}
	}
	t.Fatalf("no line starting with %q in:\n%s", prefix, out)
	return ""
}

func TestCaddySiteList(t *testing.T) {
	e := newDomainsEnv(t, domainsCF)
	script := domainsScript(t, e.dir) + "\ncaddy_site_list " + filepath.ToSlash(e.dir) + "/caddy/Caddyfile\n"
	out, err := bashRun(t, script, nil, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "a.example.com\thttps://simulate-hub.316293.xyz\n" +
		"www.a.example.com\thttps://simulate-hub.316293.xyz\n" +
		"b.example.com\thttps://simulate-news.316293.xyz\n" +
		"c.example.com\t-\n" +
		"x.wild.org\t127.0.0.1:9000\n"
	if out != want {
		t.Errorf("site list:\n%q\nwant\n%q", out, want)
	}
}

func TestCaddyDomains_TableAndStatuses(t *testing.T) {
	needOpenssl(t)
	e := newDomainsEnv(t, domainsCF)
	if err := os.WriteFile(filepath.Join(e.dir, "caddy", "caddy.env"), []byte("CLOUDFLARE_API_TOKEN=SUPERSECRETTOKEN123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := time.Hour
	writeCert(t, e.data, leDir, "a.example.com", "Let's Encrypt", 60*24*h+h)    // valid
	writeCert(t, e.data, zeroSSL, "b.example.com", "ZeroSSL", 5*24*h+h)         // expiring soon
	writeCert(t, e.data, leDir, "old.example.com", "Let's Encrypt", 90*24*h+h)  // not in the Caddyfile
	writeCert(t, e.data, leDir, "*.wild.org", "Let's Encrypt", 30*24*h+h)       // wildcard for x.wild.org
	writeCert(t, e.data, leDir, "gone.example.com", "Let's Encrypt", -3*24*h-h) // expired and orphaned

	out := e.run(t)
	if strings.Contains(out, "SUPERSECRETTOKEN123") {
		t.Fatalf("the Cloudflare token must never be printed:\n%s", out)
	}
	for _, want := range []string{"Cloudflare Token: 已保存", "HTTPS 端口:   8443", "只读视图", "rc=0", "MENU_RETURN"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}

	// sites section
	sites := out[strings.Index(out, "-- Caddyfile 站点"):strings.Index(out, "-- Caddy 证书库")]
	re := func(row, pat string) {
		if ok, _ := regexp.MatchString(pat, row); !ok {
			t.Errorf("row %q does not match %q", row, pat)
		}
	}
	re(rowFor(t, sites, "a.example.com"), `https://simulate-hub\.316293\.xyz\s+有效 \d{4}-\d\d-\d\d \(剩 60 天\)`)
	re(rowFor(t, sites, "www.a.example.com"), `暂无证书`)
	re(rowFor(t, sites, "b.example.com"), `https://simulate-news\.316293\.xyz\s+即将过期 \(<14天\) \d{4}-\d\d-\d\d \(剩 5 天\)`)
	re(rowFor(t, sites, "c.example.com"), `\s-\s+暂无证书`)
	re(rowFor(t, sites, "x.wild.org"), `127\.0\.0\.1:9000\s+有效 .*\(剩 30 天\) \(通配符证书\)`)
	if !strings.Contains(sites, "有 2 个域名还没有证书") || !strings.Contains(sites, "FNode caddy") || !strings.Contains(sites, "journalctl -u caddy") {
		t.Errorf("missing no-certificate hint:\n%s", sites)
	}

	// certificate store section
	certs := out[strings.Index(out, "-- Caddy 证书库"):]
	re(rowFor(t, certs, "a.example.com"), `Let's Encrypt\s+\d{4}-\d\d-\d\d\s+60\s+有效`)
	re(rowFor(t, certs, "b.example.com"), `ZeroSSL\s+\d{4}-\d\d-\d\d\s+5\s+即将过期`)
	re(rowFor(t, certs, "old.example.com"), `90\s+有效 \[不在 Caddyfile 中\]`)
	re(rowFor(t, certs, "gone.example.com"), `-3\s+已过期 \[不在 Caddyfile 中\]`)
	if strings.Contains(rowFor(t, certs, "*.wild.org"), "不在 Caddyfile") {
		t.Errorf("a wildcard certificate that covers x.wild.org is in use:\n%s", certs)
	}
	if strings.Contains(rowFor(t, certs, "a.example.com"), "不在 Caddyfile") {
		t.Errorf("a.example.com is in the Caddyfile:\n%s", certs)
	}
}

func TestCaddyDomains_ReadOnly(t *testing.T) {
	needOpenssl(t)
	e := newDomainsEnv(t, domainsCF)
	writeCert(t, e.data, leDir, "a.example.com", "Let's Encrypt", 60*24*time.Hour)
	list := func() string {
		var b strings.Builder
		for _, root := range []string{e.dir, e.data} {
			_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					c, _ := os.ReadFile(p)
					b.WriteString(p + ":" + info.ModTime().String() + ":" + string(c) + "\n")
				}
				return nil
			})
		}
		return b.String()
	}
	before := list()
	e.run(t)
	if after := list(); after != before {
		t.Errorf("the view changed files:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCaddyDomains_NoCaddyfileNoCertificates(t *testing.T) {
	e := newDomainsEnv(t, "")
	out := e.run(t)
	for _, want := range []string{"没有找到", "还没有配置过 Caddy", "菜单 18 / FNode caddy", "没有找到任何证书", "Cloudflare Token: 未保存", "rc=0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "域名  ") && strings.Contains(out, "反代目标") {
		t.Errorf("no site table without a Caddyfile:\n%s", out)
	}
}

func TestCaddyDomains_CaddyfileWithoutCertificates(t *testing.T) {
	e := newDomainsEnv(t, "a.example.com {\n    reverse_proxy https://x.example.org\n}\n")
	out := e.run(t)
	if !strings.Contains(out, "HTTPS 端口:   443 (默认)") || !strings.Contains(rowFor(t, out, "a.example.com"), "暂无证书") ||
		!strings.Contains(out, "有 1 个域名还没有证书") || !strings.Contains(out, "没有找到任何证书") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestCaddyDomains_CaddyInstalledAndServiceState(t *testing.T) {
	e := newDomainsEnv(t, "a.example.com {\n    respond ok\n}\n")
	bin := t.TempDir()
	for name, body := range map[string]string{
		"caddy":     "#!/bin/sh\n[ \"$1\" = version ] && echo 'v2.8.4 h1:abc'\nexit 0\n",
		"systemctl": "#!/bin/sh\n[ \"$1\" = is-active ] && echo active\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out := e.run(t, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if !strings.Contains(out, "Caddy:        已安装 (v2.8.4)") || !strings.Contains(out, "服务状态:     运行中") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// Without openssl the view still lists domains and certificate files and says what is missing.
func TestCaddyDomains_NoOpenssl(t *testing.T) {
	e := newDomainsEnv(t, "a.example.com {\n    respond ok\n}\n")
	writeCert(t, e.data, leDir, "a.example.com", "Let's Encrypt", 60*24*time.Hour)

	bin := t.TempDir()
	for _, tool := range []string{"awk", "sed", "date", "cut", "head", "basename", "dirname", "grep", "tr", "cat", "getent"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	out := e.run(t, "PATH="+bin)
	if !strings.Contains(out, "没有找到 openssl") || !strings.Contains(out, "apt install openssl") {
		t.Errorf("must tell the user openssl is missing:\n%s", out)
	}
	if !strings.Contains(rowFor(t, out, "a.example.com"), "未知 (缺少 openssl)") || !strings.Contains(out, "rc=0") {
		t.Errorf("domains and certificate files are still listed:\n%s", out)
	}
}

func TestCaddyDomains_CertRootsFromXDGAndUser(t *testing.T) {
	needOpenssl(t)
	e := newDomainsEnv(t, "a.example.com {\n    respond ok\n}\n")
	xdg := t.TempDir()
	writeCert(t, filepath.Join(xdg, "caddy"), zeroSSL, "a.example.com", "ZeroSSL", 20*24*time.Hour)
	// CADDY_DATA_DIR unset: XDG_DATA_HOME/caddy is searched
	script := domainsScript(t, e.dir) + "\ncaddy_domains 0\n"
	out, err := bashRun(t, script, []string{"CADDY_DATA_DIR=", "XDG_DATA_HOME=" + xdg}, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(rowFor(t, out, "a.example.com"), "有效") || !strings.Contains(out, "ZeroSSL") {
		t.Errorf("certificate under XDG_DATA_HOME not found:\n%s", out)
	}
}
