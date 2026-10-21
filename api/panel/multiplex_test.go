package panel

import (
	"strings"
	"testing"
)

func parseMux(t *testing.T, typ, body string) *Multiplex {
	t.Helper()
	n, err := ParseNodeInfo(typ, 1, []byte(body))
	if err != nil {
		t.Fatalf("%s %s: %v", typ, body, err)
	}
	return n.Common.Multiplex
}

// The multiplex form of the dashboard reaches the node info for vless, vmess
// and trojan, whatever types PHP used for the values.
func TestParseNodeInfo_Multiplex(t *testing.T) {
	for _, typ := range []string{"vless", "vmess", "trojan"} {
		m := parseMux(t, typ, `{"server_port":443,"multiplex":{"enabled":true,"protocol":"smux","padding":true,"max_connections":400,"min_streams":4,"max_streams":0,"brutal":{"enabled":false,"up_mbps":0,"down_mbps":0}}}`)
		if !m.Specified() || !*m.Enabled || !m.Padding || m.Brutal.Enabled || m.Protocol != "smux" || m.MaxConnections != 400 || m.MinStreams != 4 {
			t.Errorf("%s: %+v", typ, m)
		}
	}

	// numbers and booleans as strings or 0/1
	m := parseMux(t, "vless", `{"multiplex":{"enabled":"1","padding":1,"brutal":{"enabled":"true","up_mbps":"50","down_mbps":100.0}}}`)
	if !m.Specified() || !*m.Enabled || !m.Padding || !m.Brutal.Enabled || m.Brutal.UpMbps != 50 || m.Brutal.DownMbps != 100 {
		t.Errorf("loose types: %+v", m)
	}

	// an explicit off is a value
	m = parseMux(t, "vmess", `{"multiplex":{"enabled":false,"padding":true}}`)
	if !m.Specified() || *m.Enabled {
		t.Errorf("enabled=false must be specified and off: %+v", m)
	}
	if m = parseMux(t, "vmess", `{"multiplex":{"enabled":"0"}}`); !m.Specified() || *m.Enabled {
		t.Errorf("enabled=\"0\": %+v", m)
	}

	// nothing the panel decided: absent, null, PHP [], {}, a string, enabled null
	for name, body := range map[string]string{
		"absent":       `{"server_port":443}`,
		"null":         `{"multiplex":null}`,
		"empty array":  `{"multiplex":[]}`,
		"empty object": `{"multiplex":{}}`,
		"string":       `{"multiplex":"on"}`,
		"enabled null": `{"multiplex":{"enabled":null,"padding":true}}`,
		"junk enabled": `{"multiplex":{"enabled":"maybe"}}`,
	} {
		if m := parseMux(t, "trojan", body); m.Specified() {
			t.Errorf("%s: the panel did not decide, got %+v", name, m)
		}
	}
}

// multiplex is inbound config: toggling it rebuilds the inbound, and it is
// not a routing key.
func TestConfigHashes_MultiplexIsInbound(t *testing.T) {
	off := `{"protocol":"vless","server_port":443,"routes":[],"multiplex":{"enabled":false,"padding":false}}`
	on := strings.Replace(off, `"enabled":false`, `"enabled":true`, 1)
	padded := strings.Replace(on, `"padding":false`, `"padding":true`, 1)
	brutal := strings.Replace(padded, `}}`, `,"brutal":{"enabled":true,"up_mbps":10,"down_mbps":10}}}`, 1)
	var ins, rts []string
	for _, body := range []string{off, on, padded, brutal} {
		_, in, rt, err := configHashes([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		ins, rts = append(ins, in), append(rts, rt)
	}
	for i := 0; i < len(ins); i++ {
		for j := i + 1; j < len(ins); j++ {
			if ins[i] == ins[j] {
				t.Errorf("multiplex variants %d and %d share an inbound hash", i, j)
			}
		}
		if rts[i] != rts[0] {
			t.Errorf("multiplex must not change the routing hash (variant %d)", i)
		}
	}
}
