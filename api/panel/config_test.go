package panel

import (
	"strings"
	"testing"
)

const sampleVlessConfig = `{
  "server_port": 443,
  "network": "tcp",
  "tls": 1,
  "flow": "xtls-rprx-vision",
  "server_name": "node.example.com",
  "base_config": {"push_interval": 60, "pull_interval": "30"},
  "routes": [
    {"id": 1, "match": "ads.example.com,protocol:bittorrent", "action": "block", "action_value": null},
    {"id": 2, "match": ["netflix.com", "nflxvideo.net"], "action": "proxy", "action_value": "us-proxy"},
    {"id": 3, "match": ["example.org"], "action": "dns", "action_value": "1.1.1.1"}
  ],
  "custom_outbounds": [
    {"tag": "us-proxy", "protocol": "shadowsocks", "settings": {"server": "1.2.3.4", "server_port": 8388, "method": "aes-256-gcm", "password": "p"}, "proxy_tag": "relay"},
    {"type": "socks", "tag": "relay", "server": "5.6.7.8", "server_port": 1080}
  ],
  "custom_routes": [
    {"domain_suffix": ["openai.com"], "outbound": "us-proxy"}
  ],
  "custom_route_rules": [
    {"name": "lan", "match": {"ip_cidrs": ["10.9.0.0/16"]}, "action": {"type": "direct"}}
  ]
}`

func newTestClient(t *testing.T, nodeType string) *Client {
	t.Helper()
	return &Client{NodeType: nodeType, NodeId: 7}
}

func TestParseNodeConfig_PanelRouting(t *testing.T) {
	c := newTestClient(t, "vless")
	node, err := c.ParseNodeConfig([]byte(sampleVlessConfig))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if node == nil || node.Common == nil {
		t.Fatal("expected node info")
	}
	if node.PushInterval.Seconds() != 60 || node.PullInterval.Seconds() != 30 {
		t.Errorf("intervals: push %v pull %v", node.PushInterval, node.PullInterval)
	}
	if got := len(node.Common.Routes); got != 3 {
		t.Fatalf("routes are kept for the core, got %d", got)
	}
	if node.Common.Routes[1].Action != "proxy" || node.Common.Routes[1].ActionValue != "us-proxy" {
		t.Errorf("proxy route: %+v", node.Common.Routes[1])
	}
	if len(node.Rules.Protocol) != 1 || node.Rules.Protocol[0] != "bittorrent" {
		t.Errorf("block protocol rule: %v", node.Rules.Protocol)
	}

	outs := node.Common.CustomOutbounds
	if len(outs) != 2 {
		t.Fatalf("custom_outbounds: %d", len(outs))
	}
	if outs[0].Tag != "us-proxy" || outs[0].Protocol != "shadowsocks" || outs[0].ProxyTag != "relay" {
		t.Errorf("xboard-node shape: %+v", outs[0])
	}
	if outs[0].Settings["method"] != "aes-256-gcm" {
		t.Errorf("settings: %v", outs[0].Settings)
	}
	// raw sing-box shape is folded into Settings
	if outs[1].Tag != "relay" || outs[1].Protocol != "socks" || outs[1].Settings["server"] != "5.6.7.8" {
		t.Errorf("raw shape: %+v", outs[1])
	}
	if _, ok := outs[1].Settings["type"]; ok {
		t.Errorf("meta key leaked into settings: %v", outs[1].Settings)
	}
	if len(node.Common.CustomRoutes) != 1 || node.Common.CustomRoutes[0]["outbound"] != "us-proxy" {
		t.Errorf("custom_routes: %v", node.Common.CustomRoutes)
	}
	if len(node.Common.CustomRouteRules) != 1 || node.Common.CustomRouteRules[0].Action.Type != "direct" {
		t.Errorf("custom_route_rules: %+v", node.Common.CustomRouteRules)
	}

	// Same content again (re-ordered keys, as a WS payload would be) is a no-op
	reordered := strings.Replace(sampleVlessConfig, `"server_port": 443,`, ``, 1)
	reordered = strings.TrimSuffix(strings.TrimSpace(reordered), "}") + `, "server_port": 443}`
	again, err := c.ParseNodeConfig([]byte(reordered))
	if err != nil {
		t.Fatalf("parse again: %v", err)
	}
	if again != nil {
		t.Error("identical content should be reported as unchanged")
	}
}

func TestParseNodeConfig_RoutingOnlyChangeKeepsInboundHash(t *testing.T) {
	c := newTestClient(t, "vless")
	first, err := c.ParseNodeConfig([]byte(sampleVlessConfig))
	if err != nil || first == nil {
		t.Fatalf("parse: %v", err)
	}
	changed := strings.Replace(sampleVlessConfig, `"server_port": 8388`, `"server_port": 9999`, 1)
	second, err := c.ParseNodeConfig([]byte(changed))
	if err != nil || second == nil {
		t.Fatalf("parse changed: %v", err)
	}
	if first.InboundHash != second.InboundHash {
		t.Error("an outbound change must not change the inbound hash")
	}
	if first.RoutingHash == second.RoutingHash {
		t.Error("an outbound change must change the routing hash")
	}
	third, err := c.ParseNodeConfig([]byte(strings.Replace(changed, `"server_port": 443`, `"server_port": 8443`, 1)))
	if err != nil || third == nil {
		t.Fatalf("parse port change: %v", err)
	}
	if third.InboundHash == second.InboundHash {
		t.Error("a listen port change must change the inbound hash")
	}
}

func TestParseNodeConfig_NoBaseConfig(t *testing.T) {
	c := newTestClient(t, "shadowsocks")
	node, err := c.ParseNodeConfig([]byte(`{"server_port": 8388, "cipher": "aes-128-gcm"}`))
	if err != nil || node == nil {
		t.Fatalf("parse: %v", err)
	}
	if node.PullInterval != 0 {
		t.Errorf("expected zero interval, got %v", node.PullInterval)
	}
}
