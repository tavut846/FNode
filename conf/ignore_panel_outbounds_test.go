package conf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ignoreScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../FNode-script/FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	a := strings.Index(src, "# ---- ignore-panel-outbounds")
	endMarker := "# ---- end ignore-panel-outbounds ----"
	b := strings.Index(src, endMarker)
	if a < 0 || b < 0 {
		t.Fatal("ignore-panel-outbounds helper not found in FNode.sh")
	}
	return src[a : b+len(endMarker)]
}

func runIgnore(t *testing.T, content, mode string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := bashRun(t, ignoreScript(t)+"\nignore_panel_outbounds_set "+mode+" '"+filepath.ToSlash(f)+"'", nil, "")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	b, _ := os.ReadFile(f)
	if baks, _ := filepath.Glob(f + ".bak-*"); len(baks) != 1 {
		t.Errorf("want one backup, got %v", baks)
	}
	return string(b)
}

func TestIgnorePanelOutbounds_ScriptInsertsAndFlipsTheKey(t *testing.T) {
	const cfg = "{\n  // my node\n  \"Log\": {\"Level\": \"info\"},\n  \"Cores\": []\n}\n"
	on := runIgnore(t, cfg, "on")
	if !strings.Contains(on, `"IgnorePanelOutbounds": true,`) || !strings.Contains(on, "// my node") {
		t.Fatalf("on must insert the key and keep comments:\n%s", on)
	}
	c := New()
	if err := os.WriteFile(filepath.Join(t.TempDir(), "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(f, []byte(on), 0o644)
	if err := c.LoadFromPath(f); err != nil || !c.IgnorePanelOutbounds {
		t.Fatalf("the edited config must load with the switch on: %v %v", err, c.IgnorePanelOutbounds)
	}
	off := runIgnore(t, on, "off")
	if !strings.Contains(off, `"IgnorePanelOutbounds": false`) || strings.Count(off, "IgnorePanelOutbounds") != 1 {
		t.Fatalf("off must flip the existing key:\n%s", off)
	}
}

func TestIgnorePanelOutbounds_ScriptEdgeCases(t *testing.T) {
	load := func(s string) *Conf {
		t.Helper()
		f := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(f, []byte(s), 0o644)
		c := New()
		if err := c.LoadFromPath(f); err != nil {
			t.Fatalf("edited config must load: %v\n%s", err, s)
		}
		return c
	}
	// the first "{" is in a comment; the key must go after the real one
	got := runIgnore(t, "// old: {\n{\n  \"Log\": {}\n}\n", "on")
	if !strings.HasPrefix(got, "// old: {\n{") || !load(got).IgnorePanelOutbounds {
		t.Errorf("comment line with a brace:\n%s", got)
	}
	// empty object: no trailing-comma trouble
	if got = runIgnore(t, "{}\n", "on"); !load(got).IgnorePanelOutbounds {
		t.Errorf("empty object:\n%s", got)
	}
	// single-line config
	if got = runIgnore(t, `{"Log":{"Level":"info"}}`, "on"); !load(got).IgnorePanelOutbounds || !strings.Contains(got, `"Log":{"Level":"info"}`) {
		t.Errorf("single line:\n%s", got)
	}
	// values that are not a bare true/false are replaced too, so "on" cannot silently do nothing
	got = runIgnore(t, "{\n  \"IgnorePanelOutbounds\": \"false\", // keep\n  \"Cores\": []\n}\n", "on")
	if !strings.Contains(got, "\"IgnorePanelOutbounds\": true, // keep") || !load(got).IgnorePanelOutbounds {
		t.Errorf("quoted value:\n%s", got)
	}
	// reading the key back
	f := filepath.Join(t.TempDir(), "config.json")
	for content, want := range map[string]string{
		`{"IgnorePanelOutbounds": true}`:    "true",
		`{"IgnorePanelOutbounds":"false" }`: "false",
		`{"Log": {}}`:                       "",
	} {
		os.WriteFile(f, []byte(content), 0o644)
		out, err := bashRun(t, ignoreScript(t)+"\nignore_panel_outbounds_read '"+filepath.ToSlash(f)+"'", nil, "")
		if err != nil || strings.TrimSpace(out) != want {
			t.Errorf("read %s = %q (%v), want %q", content, out, err, want)
		}
	}
}

func TestIgnorePanelOutbounds_OffWithoutKeyChangesNothing(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config.json")
	const cfg = "{\n  \"Log\": {}\n}\n"
	os.WriteFile(f, []byte(cfg), 0o644)
	out, err := bashRun(t, ignoreScript(t)+"\nignore_panel_outbounds_set off '"+filepath.ToSlash(f)+"' && echo \"backup=[$IGNORE_PANEL_BACKUP]\"", nil, "")
	if err != nil || !strings.Contains(out, "backup=[]") {
		t.Fatalf("%v: %s", err, out)
	}
	if b, _ := os.ReadFile(f); string(b) != cfg {
		t.Errorf("config changed:\n%s", b)
	}
	if baks, _ := filepath.Glob(f + ".bak-*"); len(baks) != 0 {
		t.Errorf("no backup expected, got %v", baks)
	}
}

func TestIgnorePanelOutbounds_ScriptWiring(t *testing.T) {
	raw, err := os.ReadFile("../FNode-script/FNode.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	for _, want := range []string{
		`"ignore-panel-outbounds"|"ignore_panel_outbounds") check_install 0 && { ignore_panel_outbounds 0 "$2"`,
		`21) check_install && ignore_panel_outbounds_menu ;;`,
		`22) exit ;;`,
		`请输入选择 [0-22]`,
		`config ignore-panel-outbounds -c`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("FNode.sh is missing %q", want)
		}
	}
	// every menu number has a case and the exit is the last one
	for _, n := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20", "21", "22"} {
		if !strings.Contains(src, "${green}"+n+".${plain}") || !strings.Contains(src, "        "+n+") ") {
			t.Errorf("menu entry or case %s missing", n)
		}
	}
}

func TestIgnorePanelOutbounds_TopLevelReachesTheSingCore(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(f, []byte(`{"IgnorePanelOutbounds": true, "IgnorePanelOutboundTags": ["a"], "Cores": [{"Type": "sing", "IgnorePanelOutboundTags": ["b"]}]}`), 0o644)
	c := New()
	if err := c.LoadFromPath(f); err != nil {
		t.Fatal(err)
	}
	c.SetGeneratedDirs(f)
	c.SetGeneratedDirs(f) // reloads call it again: no duplicates
	sc := c.CoresConfig[0].SingConfig
	if !sc.IgnorePanelOutbounds || len(sc.IgnorePanelOutboundTags) != 2 {
		t.Fatalf("sing core: %+v", sc.IgnorePanelOutboundTags)
	}
}
