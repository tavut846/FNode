package conf

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRelease is one release in the fake GitHub.
type fakeRelease struct {
	Tag        string
	Prerelease bool
	Draft      bool
	HasZip     bool // has FNode-linux-64.zip
}

// fakeGitHub serves what install.sh reads: the releases API, the
// releases/latest redirect, the Atom feed, the zips and the raw scripts.
type fakeGitHub struct {
	releases    []fakeRelease // newest first
	apiStatus   int           // 0 = 200
	compact     bool
	atomMissing bool
	redirectOff bool
	zip         []byte
	srv         *httptest.Server
}

func newFakeGitHub(t *testing.T, releases []fakeRelease) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{releases: releases, zip: releaseZip(t)}
	g.srv = httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGitHub) hasZip(tag string) bool {
	for _, r := range g.releases {
		if r.Tag == tag {
			return r.HasZip
		}
	}
	return false
}

func (g *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/releases":
		if g.apiStatus != 0 {
			w.WriteHeader(g.apiStatus)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 1.2.3.4.","documentation_url":"https://docs.github.com"}`))
			return
		}
		var list []map[string]any
		for _, rel := range g.releases {
			assets := []map[string]any{{"name": "FNode-linux-arm64-v8a.zip"}, {"name": "FNode-linux-64.zip.dgst"}}
			if rel.HasZip {
				assets = append(assets, map[string]any{"name": "FNode-linux-64.zip"})
			}
			list = append(list, map[string]any{
				"tag_name": rel.Tag, "name": "FNode " + rel.Tag, "draft": rel.Draft, "prerelease": rel.Prerelease,
				"assets": assets,
			})
		}
		var body []byte
		if g.compact {
			body, _ = json.Marshal(list)
		} else {
			body, _ = json.MarshalIndent(list, "", "  ")
		}
		_, _ = w.Write(body)
	case r.URL.Path == "/web/releases/latest":
		if g.redirectOff {
			http.NotFound(w, r)
			return
		}
		for _, rel := range g.releases {
			if !rel.Prerelease && !rel.Draft {
				http.Redirect(w, r, "/web/releases/tag/"+rel.Tag, http.StatusFound)
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasPrefix(r.URL.Path, "/web/releases/tag/"):
		_, _ = w.Write([]byte("release page"))
	case r.URL.Path == "/web/releases.atom":
		if g.atomMissing {
			http.NotFound(w, r)
			return
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><feed>`)
		for _, rel := range g.releases {
			fmt.Fprintf(&b, `<entry><link rel="alternate" type="text/html" href="%s/web/releases/tag/%s"/></entry>`, g.srv.URL, rel.Tag)
		}
		b.WriteString(`</feed>`)
		_, _ = w.Write([]byte(b.String()))
	case strings.HasPrefix(r.URL.Path, "/web/releases/download/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/web/releases/download/"), "/")
		if len(parts) == 2 && parts[1] == "FNode-linux-64.zip" && g.hasZip(parts[0]) {
			_, _ = w.Write(g.zip)
			return
		}
		http.NotFound(w, r)
	case strings.HasPrefix(r.URL.Path, "/raw/"):
		_, _ = w.Write([]byte("#!/bin/sh\necho management script\n"))
	default:
		http.NotFound(w, r)
	}
}

// releaseZip is a release zip whose "binary" is a script that prints its
// version. It is stored uncompressed so it passes the 1 MB sanity check.
func releaseZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	add("FNode", "#!/bin/sh\n# "+strings.Repeat("x", 1_200_000)+"\necho 9.9.9\n")
	for _, f := range []string{"dns.json", "route.json", "custom_outbound.json", "custom_inbound.json", "config.json"} {
		add(f, `{"from":"zip"}`)
	}
	add("geoip.dat", "geo")
	add("geosite.dat", "site")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var lookupReleases = []fakeRelease{
	{Tag: "draft-0876d69-73", HasZip: true},              // workflow artifact, must be skipped
	{Tag: "0.0.23-pre", Prerelease: true, HasZip: false}, // no zip for this arch, must be skipped
	{Tag: "0.0.22-pre", Prerelease: true, HasZip: true},  // newest usable pre-release
	{Tag: "0.0.21", HasZip: true},                        // newest stable
	{Tag: "0.0.21-pre-1", Prerelease: true, HasZip: true},
	{Tag: "0.0.20", HasZip: true},
}

func lookup(t *testing.T, g *fakeGitHub, noJQ bool, call string) (string, string, error) {
	t.Helper()
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not available")
	}
	_ = curl
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	src, err := os.ReadFile("../FNode-script/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	from := strings.Index(text, "# --- release lookup ---")
	to := strings.Index(text, "# --- end release lookup ---")
	if from < 0 || to < from {
		t.Fatal("release lookup helpers not found in install.sh")
	}
	cmd := exec.Command(bash, "-c", text[from:to]+"\n"+call)
	cmd.Env = append(os.Environ(),
		"FNODE_GH_API="+g.srv.URL+"/api",
		"FNODE_GH_WEB="+g.srv.URL+"/web",
	)
	if noJQ {
		cmd.Env = append(cmd.Env, "FNODE_NO_JQ=1")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	return strings.TrimSpace(stdout.String()), stderr.String(), err
}

// "Update pre-release version not found": the newest usable pre-release and
// stable release are found with or without jq, on pretty and compact JSON.
func TestInstallScript_ResolveVersion(t *testing.T) {
	for _, noJQ := range []bool{true, false} {
		for _, compact := range []bool{false, true} {
			name := fmt.Sprintf("nojq=%v/compact=%v", noJQ, compact)
			t.Run(name, func(t *testing.T) {
				if !noJQ {
					if _, err := exec.LookPath("jq"); err != nil {
						t.Skip("jq not available")
					}
				}
				g := newFakeGitHub(t, lookupReleases)
				g.compact = compact
				for kind, want := range map[string]string{"pre": "0.0.22-pre", "latest": "0.0.21"} {
					got, errOut, err := lookup(t, g, noJQ, "resolve_version "+kind+" 64")
					if err != nil || got != want {
						t.Errorf("%s: got %q err %v (want %q)\n%s", kind, got, err, want, errOut)
					}
				}
			})
		}
	}
}

// A pre-release channel must not go back behind a newer stable release.
func TestInstallScript_ResolveVersion_NewerStableWins(t *testing.T) {
	g := newFakeGitHub(t, []fakeRelease{
		{Tag: "0.0.23", HasZip: true},
		{Tag: "0.0.22-pre", Prerelease: true, HasZip: true},
	})
	got, errOut, err := lookup(t, g, true, "resolve_version pre 64")
	if err != nil || got != "0.0.23" {
		t.Errorf("got %q err %v\n%s", got, err, errOut)
	}
}

// The unauthenticated API allows 60 requests/hour per IP. When it says no,
// the lookup still works through the releases page and the Atom feed, and
// the user is told why.
func TestInstallScript_ResolveVersion_RateLimited(t *testing.T) {
	g := newFakeGitHub(t, lookupReleases)
	g.apiStatus = http.StatusForbidden
	for kind, want := range map[string]string{"pre": "0.0.22-pre", "latest": "0.0.21"} {
		got, errOut, err := lookup(t, g, true, "resolve_version "+kind+" 64")
		if err != nil || got != want {
			t.Errorf("%s: got %q err %v (want %q)\n%s", kind, got, err, want, errOut)
		}
		if !strings.Contains(errOut, "rate limit") || !strings.Contains(errOut, "403") {
			t.Errorf("%s: the rate limit is not explained:\n%s", kind, errOut)
		}
	}
}

// Nothing reachable: a non-zero status and no tag, so the installer stops
// before changing anything.
func TestInstallScript_ResolveVersion_AllSourcesFail(t *testing.T) {
	g := newFakeGitHub(t, lookupReleases)
	g.apiStatus = http.StatusInternalServerError
	g.atomMissing = true
	g.redirectOff = true
	// no git remote either: an unreachable URL
	got, errOut, err := lookup(t, g, true, `git() { return 1; }; resolve_version pre 64`)
	if err == nil || got != "" {
		t.Errorf("got %q err %v\n%s", got, err, errOut)
	}
	if !strings.Contains(errOut, "GitHub API failed") {
		t.Errorf("the API failure is not reported:\n%s", errOut)
	}
}

// A typed version resolves with or without a leading v; a missing one fails.
func TestInstallScript_ResolveExplicit(t *testing.T) {
	g := newFakeGitHub(t, lookupReleases)
	for in, want := range map[string]string{"0.0.22-pre": "0.0.22-pre", "v0.0.22-pre": "0.0.22-pre", "0.0.21": "0.0.21", "v0.0.20": "0.0.20"} {
		got, errOut, err := lookup(t, g, true, "resolve_explicit "+in+" 64")
		if err != nil || got != want {
			t.Errorf("%s: got %q err %v (want %q)\n%s", in, got, err, want, errOut)
		}
	}
	for _, in := range []string{"9.9.9", "0.0.23-pre"} { // unknown, and no zip for the arch
		if got, _, err := lookup(t, g, true, "resolve_explicit "+in+" 64"); err == nil || got != "" {
			t.Errorf("%s must not resolve, got %q", in, got)
		}
	}
}

// updateSandbox runs install.sh's update path (a version argument, no panel
// flags) against a temporary filesystem: every system path is redirected into
// the sandbox, and systemctl and the package manager are stubs.
type updateSandbox struct {
	t    *testing.T
	root string
	g    *fakeGitHub
}

func newUpdateSandbox(t *testing.T) *updateSandbox {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"etc/FNode", "usr/local", "usr/bin", "etc/systemd/system", "stubs"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		"systemctl":              "#!/bin/sh\necho \"$@\" >> \"" + filepath.ToSlash(root) + "/calls.log\"\n[ \"$1\" = status ] && echo '   Active: active (running) since now'\nexit 0\n",
		"journalctl":             "#!/bin/sh\nexit 0\n",
		"apt-get":                "#!/bin/sh\nexit 0\n",
		"crontab":                "#!/bin/sh\nexit 0\n",
		"update-ca-certificates": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "stubs", name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &updateSandbox{t: t, root: root, g: newFakeGitHub(t, lookupReleases)}
}

func (s *updateSandbox) path(p string) string { return filepath.Join(s.root, filepath.FromSlash(p)) }

// run executes the redirected install.sh with args. inject is shell code
// run just before the config guard (to simulate something rewriting it).
func (s *updateSandbox) run(inject string, args ...string) (string, error) {
	s.t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		s.t.Skip("bash not available")
	}
	if _, err = exec.LookPath("curl"); err != nil {
		s.t.Skip("curl not available")
	}
	if _, err = exec.LookPath("unzip"); err != nil {
		s.t.Skip("unzip not available")
	}
	src, err := os.ReadFile("../FNode-script/install.sh")
	if err != nil {
		s.t.Fatal(err)
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	r := filepath.ToSlash(s.root)
	text = strings.NewReplacer(
		"[[ $EUID -ne 0 ]] && echo", "false && echo",
		"if [[ -f /etc/redhat-release ]]; then\n    release=\"centos\"", "if true; then\n    release=\"debian\"",
		"df -Pm /usr/local", "df -Pm "+r+"/usr/local",
		"/etc/systemd/system", r+"/etc/systemd/system",
		"/etc/cron.d", r+"/etc/cron.d",
		"/etc/FNode", r+"/etc/FNode",
		"/usr/local/FNode", r+"/usr/local/FNode",
		"/usr/bin/FNode", r+"/usr/bin/FNode",
		"/usr/bin/fnode", r+"/usr/bin/fnode",
	).Replace(text)
	if inject != "" {
		marker := `    if [[ -n "$cfg_sum" &&`
		if !strings.Contains(text, marker) {
			s.t.Fatal("config guard not found in install.sh")
		}
		text = strings.Replace(text, marker, inject+"\n"+marker, 1)
	}
	script := filepath.Join(s.root, "install.sh")
	if err = os.WriteFile(script, []byte(text), 0o755); err != nil {
		s.t.Fatal(err)
	}
	cmd := exec.Command(bash, append([]string{script}, args...)...)
	cmd.Dir = s.root
	cmd.Stdin = strings.NewReader("")
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(s.root, "stubs")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FNODE_GH_API="+s.g.srv.URL+"/api",
		"FNODE_GH_WEB="+s.g.srv.URL+"/web",
		"FNODE_RAW="+s.g.srv.URL+"/raw",
		"FNODE_NO_JQ=1",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *updateSandbox) seedUserFiles() map[string]string {
	files := map[string]string{
		"etc/FNode/config.json":                `{"Log":{"Level":"info"},"Nodes":[{"Core":"sing","ApiHost":"https://panel.example","MachineID":7,"MachineToken":"keep-me-secret"}]}` + "\n",
		"etc/FNode/sing_origin.json":           `{"outbounds":[{"type":"direct","tag":"direct"}]}`,
		"etc/FNode/fullchain.cer":              "CERT",
		"etc/FNode/cert.key":                   "KEY",
		"etc/FNode/dns.json":                   `{"user":"dns"}`,
		"etc/FNode/generated/n.effective.json": `{"_fnode":{"node":"n"}}`,
	}
	for name, body := range files {
		p := s.path(name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			s.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			s.t.Fatal(err)
		}
	}
	// the previously installed FNode
	if err := os.MkdirAll(s.path("usr/local/FNode"), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(s.path("usr/local/FNode/FNode"), []byte("#!/bin/sh\necho 0.0.1-old\n"), 0o755); err != nil {
		s.t.Fatal(err)
	}
	return files
}

func (s *updateSandbox) assertUserFilesUntouched(files map[string]string) {
	s.t.Helper()
	for name, want := range files {
		got, err := os.ReadFile(s.path(name))
		if err != nil || string(got) != want {
			s.t.Errorf("%s changed by the update: %q (err %v), want %q", name, got, err, want)
		}
	}
}

// "After fnode update the node is disconnected": an update (a version only,
// as FNode.sh update runs it) must replace the binary and leave the user's
// config, origin, certs and generated files byte-identical.
func TestInstallScript_UpdateKeepsUserFiles(t *testing.T) {
	for _, arg := range []string{"0.0.22-pre", "pre", "latest"} {
		t.Run(arg, func(t *testing.T) {
			s := newUpdateSandbox(t)
			files := s.seedUserFiles()
			out, err := s.run("", arg)
			if err != nil {
				t.Fatalf("update %s: %v\n%s", arg, err, out)
			}
			s.assertUserFilesUntouched(files)
			bin, err := os.ReadFile(s.path("usr/local/FNode/FNode"))
			if err != nil || !strings.Contains(string(bin), "9.9.9") {
				t.Errorf("the binary was not replaced (err %v)", err)
			}
			for _, gone := range []string{"usr/local/FNode.new", "usr/local/FNode.old"} {
				if _, err := os.Stat(s.path(gone)); err == nil {
					t.Errorf("%s left behind", gone)
				}
			}
			for _, skipped := range []string{"etc/FNode/geoip.dat", "etc/FNode/geosite.dat"} {
				if _, err := os.Stat(s.path(skipped)); err == nil {
					t.Errorf("%s must not be installed", skipped)
				}
			}
			calls, _ := os.ReadFile(s.path("calls.log"))
			if !strings.Contains(string(calls), "start FNode") {
				t.Errorf("the service was not started again:\n%s", calls)
			}
			if !strings.Contains(out, "重启成功") {
				t.Errorf("the restart is not reported:\n%s", out)
			}
		})
	}
}

// If anything rewrote config.json during an update, the original is put back.
func TestInstallScript_UpdateRestoresChangedConfig(t *testing.T) {
	s := newUpdateSandbox(t)
	files := s.seedUserFiles()
	out, err := s.run(`echo '{"Nodes":[]}' > "`+filepath.ToSlash(s.path("etc/FNode/config.json"))+`"`, "0.0.22-pre")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s.assertUserFilesUntouched(files)
	if !strings.Contains(out, "restored") {
		t.Errorf("the restore is not reported:\n%s", out)
	}
}

// A failed update (unknown version) changes nothing: the installed FNode and
// the config stay as they were.
func TestInstallScript_FailedUpdateChangesNothing(t *testing.T) {
	s := newUpdateSandbox(t)
	files := s.seedUserFiles()
	out, err := s.run("", "9.9.9-missing")
	if err == nil {
		t.Fatalf("an unknown version must fail:\n%s", out)
	}
	s.assertUserFilesUntouched(files)
	bin, _ := os.ReadFile(s.path("usr/local/FNode/FNode"))
	if !strings.Contains(string(bin), "0.0.1-old") {
		t.Errorf("the installed binary was replaced by a failed update: %q", bin)
	}
	if !strings.Contains(out, "Nothing was changed") {
		t.Errorf("missing explanation:\n%s", out)
	}
}

// FNode.sh update/install run the downloaded install.sh. An empty download
// must fail (bash <(curl ...) on an empty file "succeeds" and the user is
// told the update is done).
func TestFNodeScript_RemoteInstallerDownloadIsChecked(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	if _, err = exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	src, err := os.ReadFile("../FNode-script/FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	from := strings.Index(text, "run_remote_installer() {")
	if from < 0 {
		t.Fatal("run_remote_installer not found in FNode.sh")
	}
	to := from + strings.Index(text[from:], "\n}\n") + 3
	fn := text[from:to]
	if strings.Contains(text, "bash <(curl -Ls \"https://raw.githubusercontent.com/tavut846/FNode/") {
		t.Error("FNode.sh must not run bash <(curl ...) on the installer directly")
	}

	out := filepath.ToSlash(filepath.Join(t.TempDir(), "args.txt"))
	body := "#!/bin/bash\necho \"$@\" > '" + out + "'\n"
	mode := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "empty":
			w.WriteHeader(http.StatusOK)
		case "404":
			http.NotFound(w, r)
		case "html":
			_, _ = w.Write([]byte("<html>blocked</html>"))
		default:
			_, _ = w.Write([]byte(body))
		}
	}))
	defer srv.Close()

	run := func() (string, error) {
		script := "red=''; plain=''\n" + fn + "\nrun_remote_installer 0.0.22-pre\n"
		cmd := exec.Command(bash, "-c", script)
		cmd.Env = append(os.Environ(), "FNODE_RAW="+srv.URL)
		b, err := cmd.CombinedOutput()
		return string(b), err
	}
	if o, err := run(); err != nil {
		t.Fatalf("a good installer must run: %v\n%s", err, o)
	}
	if b, _ := os.ReadFile(filepath.FromSlash(out)); strings.TrimSpace(string(b)) != "0.0.22-pre" {
		t.Errorf("the installer did not get the version argument: %q", b)
	}
	for _, m := range []string{"empty", "404", "html"} {
		mode = m
		if o, err := run(); err == nil || !strings.Contains(o, "nothing was changed") {
			t.Errorf("%s download must fail with an explanation, got err=%v\n%s", m, err, o)
		}
	}
}
