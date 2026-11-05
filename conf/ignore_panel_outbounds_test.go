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
