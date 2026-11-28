package conf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	proxyNews = "https://simulate-news.316293.xyz"
	proxyHub  = "https://simulate-hub.316293.xyz"
)

// proxyHelpers returns the camouflage-site menu helpers of FNode.sh, with the
// colour variables emptied, ready to be followed by a test snippet.
func proxyHelpers(t *testing.T) string {
	t.Helper()
	raw, err := readScript(t, "FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	const endMarker = "# ---- end caddy proxy target helpers ----"
	a := strings.Index(src, "# ---- caddy proxy target helpers")
	b := strings.Index(src, endMarker)
	if a < 0 || b < a {
		t.Fatal("caddy proxy target helpers not found in FNode.sh")
	}
	return "green=''; red=''; yellow=''; plain=''\n" + src[a:b+len(endMarker)] + "\n"
}

// runChoose feeds stdin to caddy_choose_proxy_dest and returns proxy_dest, the
// return code and the output.
func runChoose(t *testing.T, stdin string, env []string, pre string) (dest string, rc int, out string) {
	t.Helper()
	script := proxyHelpers(t) + pre + "\ncaddy_choose_proxy_dest; rc=$?\necho \"DEST=[$proxy_dest] RC=$rc\"\n"
	out, _ = bashRun(t, script, env, stdin)
	i := strings.LastIndex(out, "DEST=[")
	if i < 0 {
		t.Fatalf("no result line in output:\n%s", out)
	}
	line := out[i:]
	dest = line[len("DEST=["):strings.Index(line, "] RC=")]
	rc = 1
	if strings.Contains(line, "RC=0") {
		rc = 0
	}
	return dest, rc, out
}

func TestProxyMenu_Choices(t *testing.T) {
	for _, c := range []struct {
		name, stdin, want string
	}{
		{"1 = news (old site)", "1\n", proxyNews},
		{"2 = hub (new site)", "2\n", proxyHub},
		{"3 = custom https URL", "3\nhttps://example.com\n", "https://example.com"},
		{"3 = custom without scheme gets https://", "3\nexample.com\n", "https://example.com"},
		{"3 = custom keeps http and port, drops trailing slash", "3\nhttp://my.example.com:8080/\n", "http://my.example.com:8080"},
		{"3 = custom is trimmed", "3\n   cdn.example.org  \n", "https://cdn.example.org"},
		{"invalid choice asks again", "9\nabc\n2\n", proxyHub},
		{"empty custom asks again", "3\n\n\nexample.net\n", "https://example.net"},
		{"invalid custom asks again", "3\nnot a url\nhttps://a b.com\nftp://x.com\nhttps://x.com/path\nhttps://x.com{\nhttps://ok.example.com\n", "https://ok.example.com"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dest, rc, out := runChoose(t, c.stdin, nil, "")
			if rc != 0 || dest != c.want {
				t.Fatalf("dest=%q rc=%d, want %q\n%s", dest, rc, c.want, out)
			}
		})
	}
}

func TestProxyMenu_ShowsAllChoices(t *testing.T) {
	_, _, out := runChoose(t, "1\n", nil, "")
	for _, want := range []string{"1) simulate-news", proxyNews, "2) simulate-hub", proxyHub, "3) 自定义", "直接回车 = 在 simulate-news / simulate-hub 中随机选一个"} {
		if !strings.Contains(out, want) {
			t.Errorf("menu is missing %q:\n%s", want, out)
		}
	}
}

func TestProxyMenu_InvalidCustomIsRejectedWithMessage(t *testing.T) {
	_, _, out := runChoose(t, "3\nhttps://x.com/path\nhttps://ok.example.com\n", nil, "")
	if !strings.Contains(out, "网址无效") {
		t.Errorf("an invalid URL must be reported:\n%s", out)
	}
	_, _, out = runChoose(t, "7\n1\n", nil, "")
	if !strings.Contains(out, "无效选择") {
		t.Errorf("an invalid choice must be reported:\n%s", out)
	}
}

func TestProxyMenu_EmptyPicksRandomlyBetweenTheTwoSites(t *testing.T) {
	// stubbed random: 0 = news, 1 = hub, and the choice is printed
	for rnd, want := range map[string]string{"0": proxyNews, "1": proxyHub} {
		dest, rc, out := runChoose(t, "\n", nil, "caddy_rand2() { echo "+rnd+"; }")
		if rc != 0 || dest != want {
			t.Errorf("rand=%s: dest=%q rc=%d, want %q\n%s", rnd, dest, rc, want, out)
		}
		name := map[string]string{proxyNews: "simulate-news", proxyHub: "simulate-hub"}[want]
		if !strings.Contains(out, "已随机选择伪装目标: "+name+" ("+want+")") {
			t.Errorf("the random pick must be printed (%s):\n%s", name, out)
		}
	}

	// the real RANDOM: only the two sites ever come out, and both of them do
	script := proxyHelpers(t) + `
for i in $(seq 1 60); do
  caddy_pick_random_proxy
  echo "PICK=$proxy_dest"
done
`
	out, err := bashRun(t, script, nil, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	seen := map[string]int{}
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, "PICK="); ok {
			seen[v]++
		}
	}
	if len(seen) != 2 || seen[proxyNews] == 0 || seen[proxyHub] == 0 {
		t.Errorf("random pick must produce exactly news and hub over 60 draws, got %v", seen)
	}
}

func TestProxyMenu_NoInputAtAllPicksRandomly(t *testing.T) {
	// stdin closed (script run without a terminal): no hang, a random site
	dest, rc, out := runChoose(t, "", nil, "")
	if rc != 0 || (dest != proxyNews && dest != proxyHub) {
		t.Errorf("dest=%q rc=%d\n%s", dest, rc, out)
	}
	// closed stdin while a custom URL is expected: fail, do not loop forever
	dest, rc, out = runChoose(t, "3\n", nil, "")
	if rc == 0 || dest != "" {
		t.Errorf("EOF in the custom prompt must fail: dest=%q rc=%d\n%s", dest, rc, out)
	}
}

func TestProxyTarget_NonInteractiveEnv(t *testing.T) {
	for _, c := range []struct {
		env, want string
		rc        int
	}{
		{"https://decoy.example.com", "https://decoy.example.com", 0},
		{"decoy.example.com", "https://decoy.example.com", 0},
		{"news", proxyNews, 0},
		{"hub", proxyHub, 0},
		{"not a url", "", 1},
	} {
		dest, rc, out := runChoose(t, "", []string{"CADDY_PROXY_DEST=" + c.env}, "")
		if rc != c.rc || dest != c.want {
			t.Errorf("CADDY_PROXY_DEST=%q: dest=%q rc=%d, want %q rc=%d\n%s", c.env, dest, rc, c.want, c.rc, out)
		}
	}
	// "random" (and the unset case, covered above) never asks anything
	dest, rc, _ := runChoose(t, "", []string{"CADDY_PROXY_DEST=random"}, "caddy_rand2() { echo 1; }")
	if rc != 0 || dest != proxyHub {
		t.Errorf("random: dest=%q rc=%d", dest, rc)
	}
}

// The whole wizard with stubbed caddy commands: the chosen site ends up in the
// Caddyfile, with the Host header taken from it, and nothing else changes.
func TestCaddyWizard_ProxyTargetInCaddyfile(t *testing.T) {
	raw, err := readScript(t, "FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	a := strings.Index(src, "setup_caddy_reverse_proxy() {")
	b := strings.Index(src, "# 修改 Caddy 的 HTTPS 端口")
	if a < 0 || b < a {
		t.Fatal("wizard not found")
	}
	wizard := src[a:b]

	run := func(t *testing.T, stdin string, env []string) (caddyfile, out string, rc int) {
		t.Helper()
		dir := t.TempDir()
		script := "release=debian\nbefore_show_menu() { :; }\ncaddy_ensure() { return 0; }\ncaddy_validate() { return 0; }\ncaddy_restart_checked() { return 0; }\n" +
			proxyHelpers(t) + wizard + "\nsetup_caddy_reverse_proxy 0; echo \"rc=$?\"\n"
		script = strings.ReplaceAll(script, "/etc/caddy", filepath.ToSlash(dir)+"/caddy")
		script = strings.ReplaceAll(script, "/etc/FNode", filepath.ToSlash(dir)+"/FNode")
		out, _ = bashRun(t, script, env, stdin)
		rc = 1
		if strings.Contains(out, "rc=0") {
			rc = 0
		}
		cf, _ := os.ReadFile(filepath.Join(dir, "caddy", "Caddyfile"))
		return string(cf), out, rc
	}
	hostOf := func(u string) string { return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://") }

	for _, c := range []struct {
		name, menu, want string
	}{
		{"news", "1\n", proxyNews},
		{"hub", "2\n", proxyHub},
		{"custom", "3\nhttps://decoy.example.com:8443\n", "https://decoy.example.com:8443"},
		{"custom bad then good", "3\nnope nope\ndecoy.example.com\n", "https://decoy.example.com"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cf, out, rc := run(t, "a.example.com\ntoken123\n"+c.menu, nil)
			if rc != 0 {
				t.Fatalf("rc=%d\n%s", rc, out)
			}
			for _, want := range []string{
				"a.example.com {",
				"dns cloudflare {env.CLOUDFLARE_API_TOKEN}",
				"reverse_proxy " + c.want + " {",
				"header_up Host " + hostOf(c.want),
				"header_up X-Forwarded-Proto https",
			} {
				if !strings.Contains(cf, want) {
					t.Errorf("Caddyfile is missing %q:\n%s", want, cf)
				}
			}
			if !strings.Contains(out, "伪装反代目标: "+c.want) {
				t.Errorf("summary must show the target:\n%s", out)
			}
		})
	}

	t.Run("empty = random pick, printed and used", func(t *testing.T) {
		cf, out, rc := run(t, "a.example.com\ntoken123\n\n", nil)
		if rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, out)
		}
		var got string
		for _, u := range []string{proxyNews, proxyHub} {
			if strings.Contains(cf, "reverse_proxy "+u+" {") && strings.Contains(cf, "header_up Host "+hostOf(u)) {
				got = u
			}
		}
		if got == "" || !strings.Contains(out, "已随机选择伪装目标") || !strings.Contains(out, got) {
			t.Errorf("random pick (%q) not reported or not used:\n%s\n%s", got, out, cf)
		}
	})

	t.Run("non-interactive target", func(t *testing.T) {
		cf, out, rc := run(t, "a.example.com\ntoken123\n", []string{"CADDY_PROXY_DEST=https://decoy.example.com"})
		if rc != 0 || !strings.Contains(cf, "reverse_proxy https://decoy.example.com {") || strings.Contains(out, "请选择 [1/2/3") {
			t.Errorf("rc=%d (the menu must not be shown)\n%s\n%s", rc, out, cf)
		}
	})

	t.Run("invalid non-interactive target aborts before writing anything", func(t *testing.T) {
		cf, out, rc := run(t, "a.example.com\ntoken123\n", []string{"CADDY_PROXY_DEST=bad url"})
		if rc == 0 || cf != "" || !strings.Contains(out, "配置已中止") {
			t.Errorf("rc=%d caddyfile=%q\n%s", rc, cf, out)
		}
	})
}
