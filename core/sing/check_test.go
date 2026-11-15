package sing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tavut846/FNode/common/migrate"
)

// The legacy pre-1.12 DNS form is rejected by sing-box 1.14 itself, and what
// the migration writes is accepted by the same check.
func TestCheckOrigin_LegacyRejectedMigratedAccepted(t *testing.T) {
	legacy := `{"dns":{"servers":[{"tag":"cf","address":"https://1.1.1.1/dns-query","address_resolver":"local","detour":"direct"},{"tag":"local","address":"local"}],"final":"cf"},
"outbounds":[{"type":"direct","tag":"direct"},{"type":"block","tag":"block"}],
"route":{"rules":[{"geoip":["private"],"outbound":"block"}],"rule_set":[{"type":"remote","tag":"r","format":"binary","url":"https://example.com/r.srs","download_detour":"direct"}]}}`
	if err := CheckOrigin([]byte(legacy)); err == nil {
		t.Fatal("sing-box 1.14 must reject the legacy config")
	}
	got, err := migrate.MigrateJSON([]byte(legacy))
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckOrigin(got); err != nil {
		t.Fatalf("migrated config rejected: %v\n%s", err, got)
	}
	if !strings.Contains(string(got), `"type": "https"`) {
		t.Errorf("migrated:\n%s", got)
	}
	if err = CheckOrigin(DefaultOrigin); err != nil {
		t.Fatalf("the default origin must pass: %v", err)
	}
}

// The shipped example origin files are valid for the bundled core and need no migration.
func TestCheckOrigin_ShippedExamples(t *testing.T) {
	for _, name := range []string{"sing_origin.json", "sing_origin v1.json", "custom_outbound.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "example", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = CheckOrigin(data); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		lines, err := migrate.ChangesJSON(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(lines) != 0 {
			t.Errorf("%s still needs migration: %v", name, lines)
		}
	}
}
