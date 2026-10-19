package panel

import (
	"testing"

	"github.com/tavut846/FNode/conf"
)

func TestParseNodeInfo_PanelCertConfig(t *testing.T) {
	body := `{"server_port":443,"server_name":"a.example","cert_config":{"cert_mode":"File","domain":"a.example",
"cert_file":"/root/a.crt","key_file":"/root/a.key","email":"me@a.example","dns_provider":"cloudflare",
"dns_env":{"CF_DNS_API_TOKEN":"x","N":1},"http_port":"8080"}}`
	n, err := ParseNodeInfo("anytls", 468, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	pc := n.PanelCert()
	if pc == nil {
		t.Fatal("cert_config not parsed")
	}
	if pc.CertMode != "file" || pc.Domain != "a.example" || pc.CertFile != "/root/a.crt" || pc.KeyFile != "/root/a.key" ||
		pc.Email != "me@a.example" || pc.DNSProvider != "cloudflare" || pc.DNSEnv["CF_DNS_API_TOKEN"] != "x" ||
		pc.DNSEnv["N"] != "1" || pc.HTTPPort != 8080 {
		t.Errorf("cert_config: %+v", pc)
	}

	for name, raw := range map[string]string{
		"mode fallback": `{"server_port":443,"cert_config":{"mode":"self","domain":"b.example"}}`,
	} {
		n, err := ParseNodeInfo("trojan", 1, []byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if pc := n.PanelCert(); pc == nil || pc.CertMode != "self" || pc.Domain != "b.example" {
			t.Errorf("%s: %+v", name, pc)
		}
	}

	// Not managed by the panel: absent, none, PHP's [] or a non-object.
	for name, raw := range map[string]string{
		"absent": `{"server_port":443}`,
		"none":   `{"server_port":443,"cert_config":{"cert_mode":"none","cert_file":"/x"}}`,
		"empty":  `{"server_port":443,"cert_config":[]}`,
		"string": `{"server_port":443,"cert_config":"file"}`,
		"null":   `{"server_port":443,"cert_config":null}`,
	} {
		n, err := ParseNodeInfo("hysteria2", 1, []byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if pc := n.PanelCert(); pc != nil {
			t.Errorf("%s: want no panel cert, got %+v", name, pc)
		}
	}
}

// A cert_config change is an inbound change (rebuild), not a routing one.
func TestConfigHashes_CertConfigIsInbound(t *testing.T) {
	a := `{"server_port":443,"routes":[],"cert_config":{"cert_mode":"file","cert_file":"/a.crt","key_file":"/a.key"}}`
	b := `{"server_port":443,"routes":[],"cert_config":{"cert_mode":"file","cert_file":"/b.crt","key_file":"/a.key"}}`
	_, inA, rtA, err := configHashes([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	_, inB, rtB, err := configHashes([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if inA == inB {
		t.Error("cert_config change must change the inbound hash")
	}
	if rtA != rtB {
		t.Error("cert_config change must not change the routing hash")
	}
}

func TestClient_ResetConfigCache(t *testing.T) {
	c, err := New(&conf.ApiConfig{APIHost: "http://127.0.0.1:1", Key: "k", NodeType: "anytls", NodeID: 468})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"server_port":443}`)
	if n, err := c.ParseNodeConfig(body); err != nil || n == nil {
		t.Fatalf("first parse: %v %v", n, err)
	}
	if n, _ := c.ParseNodeConfig(body); n != nil {
		t.Fatal("unchanged config must parse to nil")
	}
	c.ResetConfigCache()
	if n, err := c.ParseNodeConfig(body); err != nil || n == nil {
		t.Fatalf("after ResetConfigCache the same config must be returned again: %v %v", n, err)
	}
}
