package sing

import (
	"testing"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

func TestBuildDefaultOutbounds(t *testing.T) {
	outbounds := BuildDefaultOutbounds(5*time.Second, ConvertStrategy("prefer_ipv4"), "1.2.3.4", nil)
	if len(outbounds) != 2 {
		t.Fatalf("expected 2 outbounds, got %d", len(outbounds))
	}

	direct := outbounds[0]
	if direct["type"] != "direct" || direct["tag"] != "direct" {
		t.Errorf("expected direct outbound, got %v", direct)
	}
	if direct["connect_timeout"] != "5s" {
		t.Errorf("expected connect_timeout 5s, got %v", direct["connect_timeout"])
	}
	if direct["domain_strategy"] != "prefer_ipv4" {
		t.Errorf("expected domain_strategy prefer_ipv4, got %v", direct["domain_strategy"])
	}
	if direct["inet4_bind_address"] != "1.2.3.4" {
		t.Errorf("expected inet4_bind_address 1.2.3.4, got %v", direct["inet4_bind_address"])
	}

	block := outbounds[1]
	if block["type"] != "block" || block["tag"] != "block" {
		t.Errorf("expected block outbound, got %v", block)
	}
}

func TestCompileRouteRules_AntiSSRFAndIPv6(t *testing.T) {
	rules := CompileRouteRules(true, nil, nil)
	// Should contain: 2 anti-SSRF rules (IPv4 & IPv6) + 1 IPv6 block rule = 3 rules
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	// First two must be SSRF blocks
	if rules[0]["outbound"] != "block" || rules[0]["ip_cidr"] == nil {
		t.Errorf("expected SSRF IPv4 block rule, got %v", rules[0])
	}
	if rules[1]["outbound"] != "block" || rules[1]["ip_cidr"] == nil {
		t.Errorf("expected SSRF IPv6 block rule, got %v", rules[1])
	}

	// Third must be IPv6 block
	if rules[2]["outbound"] != "block" || rules[2]["ip_version"] != 6 {
		t.Errorf("expected IPv6 disable rule, got %v", rules[2])
	}
}

func TestCompileRouteRules_PanelAndCustomRules(t *testing.T) {
	panelRoutes := []panel.Route{
		{
			Id:          1,
			Match:       "ads.example.com,*.doubleclick.net,regexp:^ad[0-9]\\.,protocol:bittorrent,192.168.100.0/24",
			Action:      "block",
			ActionValue: "",
		},
		{
			Id:          2,
			Match:       []string{"warp.cloudflare.com"},
			Action:      "route",
			ActionValue: "warp-out",
		},
	}

	customRules := []conf.CustomRouteRule{
		{
			Name: "my-direct",
			Match: conf.CustomRouteMatch{
				DomainSuffixes: []string{"local.net"},
			},
			Action: conf.CustomRouteAction{
				Type: "direct",
			},
		},
	}

	rules := CompileRouteRules(false, panelRoutes, customRules)
	if len(rules) < 3 {
		t.Fatalf("expected multiple rules compiled, got %d", len(rules))
	}

	// First rule should be custom rule
	if rules[0]["outbound"] != "direct" {
		t.Errorf("expected custom rule first, got %v", rules[0])
	}
}
