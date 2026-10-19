package sing

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

// realityInboundFor parses a panel-style config and builds the vless inbound.
func realityInboundFor(t *testing.T, body string) (*option.VLESSInboundOptions, error) {
	t.Helper()
	info, err := (&panel.Client{NodeType: "vless", NodeId: 1}).ParseNodeConfig([]byte(body))
	if err != nil || info == nil {
		t.Fatalf("parse panel config: %v", err)
	}
	in, err := (&Sing{}).getInboundOptions("t", info, &conf.Options{ListenIP: "0.0.0.0", SingOptions: &conf.SingOptions{}})
	if err != nil {
		return nil, err
	}
	return in.Options.(*option.VLESSInboundOptions), nil
}

// 32 bytes, base64url without padding
const goodKey = "WDY1czl4bWhpVGI3a1p0ZTZ1d3FFYkhSN3pXdU1QdnA"

func TestReality_MappingFromXboardPayload(t *testing.T) {
	// What Xboard sends for a vless node: tls 2, reality_settings as tls_settings
	// (server_port is a string, short_id a single string or null), flow at top level.
	v, err := realityInboundFor(t, `{"protocol":"vless","server_port":493,"network":"tcp","tls":2,"flow":"xtls-rprx-vision",
		"tls_settings":{"server_name":"www.example.com","server_port":"443","public_key":"x","private_key":"`+goodKey+`","short_id":"0123abcd","allow_insecure":false}}`)
	if err != nil {
		t.Fatal(err)
	}
	r := v.TLS.Reality
	if v.TLS.ServerName != "www.example.com" || r.Handshake.Server != "www.example.com" || r.Handshake.ServerPort != 443 {
		t.Errorf("server_name/dest: %q %v:%d", v.TLS.ServerName, r.Handshake.Server, r.Handshake.ServerPort)
	}
	if r.PrivateKey != goodKey || len(r.ShortID) != 1 || r.ShortID[0] != "0123abcd" {
		t.Errorf("key/short id: %q %v", r.PrivateKey, r.ShortID)
	}

	// Null / empty short_id: the empty id (clients without short_id)
	for _, sid := range []string{`null`, `""`, `"  "`} {
		v, err = realityInboundFor(t, `{"protocol":"vless","server_port":493,"network":"tcp","tls":2,
			"tls_settings":{"server_name":"a.example.com","private_key":"`+goodKey+`","short_id":`+sid+`}}`)
		if err != nil {
			t.Fatalf("short_id %s: %v", sid, err)
		}
		if len(v.TLS.Reality.ShortID) != 0 {
			t.Errorf("short_id %s must map to the empty list (sing-box then accepts the empty id), got %v", sid, v.TLS.Reality.ShortID)
		}
		if v.TLS.Reality.Handshake.ServerPort != 443 {
			t.Errorf("default dest port: %d", v.TLS.Reality.Handshake.ServerPort)
		}
	}
}

func TestReality_PrivateKeyNormalizedAndShortIDsValidated(t *testing.T) {
	raw, _ := base64.RawURLEncoding.DecodeString(goodKey)
	std := base64.StdEncoding.EncodeToString(raw) // padded, may contain + and /
	for _, k := range []string{goodKey, " " + goodKey + "\n", goodKey + "=", std} {
		got, err := NormalizeRealityPrivateKey(k)
		if err != nil {
			t.Errorf("key %q: %v", k, err)
			continue
		}
		if decoded, err := base64.RawURLEncoding.DecodeString(got); err != nil || string(decoded) != string(raw) {
			t.Errorf("key %q normalised to %q, which does not decode to the same bytes", k, got)
		}
	}
	for _, k := range []string{"short", "!!!!", strings.Repeat("A", 60)} {
		if _, err := NormalizeRealityPrivateKey(k); err == nil {
			t.Errorf("key %q must be rejected", k)
		}
	}

	ids, err := ValidateRealityShortIDs([]string{"ab12", "AB12", "", "0123456789abcdef"})
	if err != nil || len(ids) != 3 || ids[0] != "ab12" || ids[1] != "" {
		t.Errorf("ids: %v %v (case-insensitive duplicates dropped, empty id kept)", ids, err)
	}
	for _, bad := range [][]string{{"abc"}, {"zz"}, {"0123456789abcdef00"}} {
		if _, err := ValidateRealityShortIDs(bad); err == nil {
			t.Errorf("short ids %v must be rejected with a clear error", bad)
		}
	}
	// An invalid id surfaces as a node error naming the field, not a vague sing-box one
	_, err = realityInboundFor(t, `{"protocol":"vless","server_port":493,"network":"tcp","tls":2,
		"tls_settings":{"server_name":"a.example.com","private_key":"`+goodKey+`","short_id":"abc"}}`)
	if err == nil || !strings.Contains(err.Error(), "short_id") {
		t.Errorf("odd-length short id: %v", err)
	}
	// A standard-base64 key from the panel still starts the inbound
	if _, err = realityInboundFor(t, `{"protocol":"vless","server_port":493,"network":"tcp","tls":2,
		"tls_settings":{"server_name":"a.example.com","private_key":"`+std+`","short_id":"ab"}}`); err != nil {
		t.Errorf("padded key: %v", err)
	}
}
