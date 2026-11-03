package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const legacyOrigin = `{
  "dns": {
    "independent_cache": true,
    "servers": [
      {"tag": "cf", "address": "https://1.1.1.1/dns-query", "address_resolver": "local", "address_strategy": "ipv4_only", "detour": "direct"},
      {"tag": "local", "address": "local"},
      {"tag": "g", "address": "tls://8.8.8.8:853"},
      {"tag": "u", "address": "1.0.0.1"}
    ],
    "final": "cf"
  },
  "outbounds": [{"type": "direct", "tag": "direct"}, {"type": "block", "tag": "block"}],
  "route": {
    "geoip": {"path": "geoip.db"},
    "rules": [
      {"geoip": ["private"], "outbound": "block"},
      {"geosite": ["cn"], "outbound": "direct"},
      {"domain_suffix": ["a.com"], "outbound": "direct"}
    ],
    "rule_set": [
      {"type": "remote", "tag": "r", "format": "binary", "url": "https://example.com/r.srs", "download_detour": "direct"}
    ]
  },
  "experimental": {"cache_file": {"enabled": true, "store_rdrc": true}}
}`

func quote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func writeCfg(t *testing.T) (cfg, origin string) {
	t.Helper()
	dir := t.TempDir()
	origin = filepath.Join(dir, "sing_origin.json")
	cfg = filepath.Join(dir, "config.json")
	if err := os.WriteFile(origin, []byte(legacyOrigin), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "{\n  // my node\n  \"Log\": {\"Level\": \"info\"},\n  \"Cores\": [{\"Type\": \"sing\", \"OriginalPath\": " + quote(origin) + "}]\n}\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSingOptions_ConvertsLegacy(t *testing.T) {
	var d doc
	if err := decodeLoose([]byte(legacyOrigin), &d); err != nil {
		t.Fatal(err)
	}
	lines := SingOptions(d)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		`legacy address "https://1.1.1.1/dns-query" -> type "https"`,
		`geoip private -> ip_is_private`,
		`uses geosite`,
		`download_detour -> http_client.detour`,
		`independent_cache`,
		`store_rdrc`,
		`route.geoip: database section removed`,
		`default_domain_resolver: set to "local"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing change %q in:\n%s", want, joined)
		}
	}
	servers := asList(asMap(d["dns"])["servers"])
	cf := asMap(servers[0])
	dr := asMap(cf["domain_resolver"])
	if cf["type"] != "https" || cf["server"] != "1.1.1.1" || cf["address"] != nil || dr["server"] != "local" || dr["strategy"] != "ipv4_only" {
		t.Errorf("https server: %v", cf)
	}
	if asMap(servers[1])["type"] != "local" {
		t.Errorf("local server: %v", servers[1])
	}
	g := asMap(servers[2])
	if g["type"] != "tls" || g["server"] != "8.8.8.8" || g["server_port"] != 853 {
		t.Errorf("tls server: %v", g)
	}
	if u := asMap(servers[3]); u["type"] != "udp" || u["server"] != "1.0.0.1" {
		t.Errorf("udp server: %v", u)
	}
	rules := asList(asMap(d["route"])["rules"])
	if len(rules) != 2 || asMap(rules[0])["ip_is_private"] != true {
		t.Errorf("rules: %v", rules)
	}
	rs := asMap(asList(asMap(d["route"])["rule_set"])[0])
	if rs["download_detour"] != nil || asMap(rs["http_client"])["detour"] != "direct" {
		t.Errorf("rule_set: %v", rs)
	}
	if again := SingOptions(d); len(again) != 0 {
		t.Errorf("second pass must change nothing: %v", again)
	}
}

func TestSingOptions_UnconvertibleAddressIsReported(t *testing.T) {
	d := doc{"dns": map[string]any{"servers": []any{map[string]any{"tag": "f", "address": "fakeip"}}}}
	lines := SingOptions(d)
	if len(lines) != 1 || !strings.Contains(lines[0], "convert it by hand") {
		t.Errorf("lines: %v", lines)
	}
}

func TestRun_MigratesBacksUpAndIsIdempotent(t *testing.T) {
	stamp = func() string { return "20260101-000000" }
	cfg, origin := writeCfg(t)
	var validated int
	res, err := Run(cfg, func(string) error { validated++; return nil })
	if err != nil || !res.Migrated || validated != 1 {
		t.Fatalf("run: %+v %v validated=%d", res, err, validated)
	}
	if len(res.Changes) < 6 {
		t.Errorf("want one line per change, got %v", res.Changes)
	}
	for _, p := range []string{origin + ".bak-20260101-000000", cfg + ".bak-20260101-000000"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("backup missing: %v", err)
		}
	}
	if b, _ := os.ReadFile(origin + ".bak-20260101-000000"); string(b) != legacyOrigin {
		t.Error("origin backup must be the original bytes")
	}
	text, _ := os.ReadFile(cfg)
	if !strings.Contains(string(text), `"SchemaVersion": 2`) || !strings.Contains(string(text), "// my node") {
		t.Errorf("config must carry the schema version and keep its comments:\n%s", text)
	}
	if o, _ := os.ReadFile(origin); strings.Contains(string(o), `"address"`) || strings.Contains(string(o), "geosite") {
		t.Errorf("origin still legacy:\n%s", o)
	}
	before, _ := os.ReadFile(origin)
	stamp = func() string { return "20260101-000001" }
	res2, err := Run(cfg, func(string) error { validated++; return nil })
	if err != nil || res2.Migrated || len(res2.Changes) != 0 || len(res2.Backups) != 0 || validated != 1 {
		t.Fatalf("second run: %+v %v", res2, err)
	}
	after, _ := os.ReadFile(origin)
	if string(before) != string(after) {
		t.Error("second run changed the origin file")
	}
	if _, err := os.Stat(cfg + ".bak-20260101-000001"); err == nil {
		t.Error("second run must not write backups")
	}
}

func TestRun_ValidationFailureRestoresFiles(t *testing.T) {
	stamp = func() string { return "20260101-000002" }
	cfg, origin := writeCfg(t)
	cfgBefore, _ := os.ReadFile(cfg)
	_, err := Run(cfg, func(string) error { return errors.New("boom: unknown field") })
	if !errors.Is(err, ErrRolledBack) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want rolled back error with the reason, got %v", err)
	}
	if o, _ := os.ReadFile(origin); string(o) != legacyOrigin {
		t.Error("origin must be restored")
	}
	if c, _ := os.ReadFile(cfg); string(c) != string(cfgBefore) {
		t.Error("config must be restored (no SchemaVersion)")
	}
}

func TestSetSchemaVersion(t *testing.T) {
	for in, want := range map[string]string{
		`{"A":1}`:                     "{\n  \"SchemaVersion\": 2,\"A\":1}",
		`{}`:                          "{\n  \"SchemaVersion\": 2}",
		`{"SchemaVersion": 1, "A":1}`: `{"SchemaVersion": 2, "A":1}`,
	} {
		if got := string(setSchemaVersion([]byte(in), 2)); got != want {
			t.Errorf("%s -> %q, want %q", in, got, want)
		}
	}
}
