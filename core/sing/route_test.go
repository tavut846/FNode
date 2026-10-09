package sing

import (
	"encoding/json"
	"os"
	"regexp"
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

func TestSing_PreserveOriginRulesAndRuleSets(t *testing.T) {
	originContent := `{
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    },
    {
      "type": "block",
      "tag": "block"
    },
    {
      "type": "shadowsocks",
      "tag": "USLaxSS",
      "server": "1.2.3.4",
      "server_port": 10001,
      "method": "aes-256-gcm",
      "password": "password"
    }
  ],
  "route": {
    "rules": [
      {
        "domain_suffix": ["ip.sb"],
        "outbound": "USLaxSS"
      }
    ],
    "rule_set": [
      {
        "type": "inline",
        "tag": "test-ruleset",
        "rules": [
          {
            "domain_suffix": ["reddit.com"]
          }
        ]
      }
    ]
  }
}`
	tmpFile, err := os.CreateTemp("", "sing_origin_test_*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(originContent); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	coreCfg := &conf.CoreConfig{
		SingConfig: &conf.SingConfig{
			OriginalPath: tmpFile.Name(),
			DisableIPv6:  true,
		},
	}

	coreInst, err := New(coreCfg)
	if err != nil {
		t.Fatalf("failed to create sing core: %v", err)
	}
	singInst, ok := coreInst.(*Sing)
	if !ok {
		t.Fatalf("expected *Sing, got %T", coreInst)
	}

	if len(singInst.originRules) != 1 {
		t.Fatalf("expected 1 origin rule, got %d", len(singInst.originRules))
	}
	if len(singInst.originRuleSets) != 1 {
		t.Fatalf("expected 1 origin rule set, got %d", len(singInst.originRuleSets))
	}

	// Simulate dynamic router rule update (as triggered by AddNode/DelNode)
	err = singInst.UpdateRouterRules()
	if err != nil {
		t.Fatalf("UpdateRouterRules failed: %v", err)
	}
}

func TestSing_ExampleSingOriginJsonLoads(t *testing.T) {
	coreCfg := &conf.CoreConfig{
		SingConfig: &conf.SingConfig{
			OriginalPath: "../../example/sing_origin.json",
			DisableIPv6:  true,
		},
	}

	coreInst, err := New(coreCfg)
	if err != nil {
		t.Fatalf("failed to create sing core from example/sing_origin.json: %v", err)
	}
	singInst, ok := coreInst.(*Sing)
	if !ok {
		t.Fatalf("expected *Sing, got %T", coreInst)
	}

	if len(singInst.originRules) == 0 {
		t.Errorf("expected origin rules to be loaded from example/sing_origin.json")
	}

	err = singInst.UpdateRouterRules()
	if err != nil {
		t.Fatalf("UpdateRouterRules failed on example/sing_origin.json: %v", err)
	}
}

func TestSing_ExampleCustomOutboundJsonLoads(t *testing.T) {
	coreCfg := &conf.CoreConfig{
		SingConfig: &conf.SingConfig{
			OriginalPath: "../../example/custom_outbound.json",
			DisableIPv6:  true,
		},
	}

	coreInst, err := New(coreCfg)
	if err != nil {
		t.Fatalf("failed to create sing core from example/custom_outbound.json: %v", err)
	}
	singInst, ok := coreInst.(*Sing)
	if !ok {
		t.Fatalf("expected *Sing, got %T", coreInst)
	}

	if len(singInst.originRules) == 0 {
		t.Errorf("expected origin rules to be loaded from example/custom_outbound.json")
	}
	if len(singInst.originRuleSets) == 0 {
		t.Errorf("expected origin rule sets to be loaded from example/custom_outbound.json")
	}

	err = singInst.UpdateRouterRules()
	if err != nil {
		t.Fatalf("UpdateRouterRules failed on example/custom_outbound.json: %v", err)
	}
}

func TestSing_ProtectionRulesAndDomainRegex(t *testing.T) {
	content, err := os.ReadFile("../../example/sing_origin.json")
	if err != nil {
		t.Fatalf("failed to read example/sing_origin.json: %v", err)
	}

	var parsed struct {
		Route struct {
			Rules []struct {
				Protocol    []string `json:"protocol"`
				Port        []uint16 `json:"port"`
				DomainRegex []string `json:"domain_regex"`
				Outbound    string   `json:"outbound"`
			} `json:"rules"`
		} `json:"route"`
	}

	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse example/sing_origin.json: %v", err)
	}

	var regexList []string
	hasBittorrentBlock := false
	hasSMTPPortBlock := false

	for _, rule := range parsed.Route.Rules {
		if rule.Outbound == "block" {
			for _, proto := range rule.Protocol {
				if proto == "bittorrent" {
					hasBittorrentBlock = true
				}
			}
			for _, port := range rule.Port {
				if port == 25 {
					hasSMTPPortBlock = true
				}
			}
			if len(rule.DomainRegex) > 0 {
				regexList = append(regexList, rule.DomainRegex...)
			}
		}
	}

	if !hasBittorrentBlock {
		t.Errorf("expected bittorrent protocol block rule in sing_origin.json")
	}
	if !hasSMTPPortBlock {
		t.Errorf("expected port 25 SMTP block rule in sing_origin.json")
	}

	// Verify all regex compile
	compiled := make([]*regexp.Regexp, 0, len(regexList))
	for _, pattern := range regexList {
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("failed to compile domain regex %q: %v", pattern, err)
		}
		compiled = append(compiled, re)
	}

	isBlocked := func(domain string) bool {
		for _, re := range compiled {
			if re.MatchString(domain) {
				return true
			}
		}
		return false
	}

	// Must block abusive / DMCA / malicious domains
	blockedSamples := []string{
		"thepiratebay.org",
		"nyaa.si",
		"tracker.opentrackr.org",
		"supportxmr.com",
		"ethermine.org",
		"guerrillamail.com",
		"10minutemail.com",
		"bincheck.io",
		"ipstresser.com",
		"360.cn",
		"guanjia.qq.com",
		"12377.cn",
		"pincong.rocks",
		"miaoko.pages.dev",
	}
	for _, domain := range blockedSamples {
		if !isBlocked(domain) {
			t.Errorf("expected %s to be blocked by domain regex, but was not", domain)
		}
	}

	// Must NOT block normal legitimate internet domains
	allowedSamples := []string{
		"google.com",
		"youtube.com",
		"github.com",
		"microsoft.com",
		"apple.com",
		"wikipedia.org",
		"cloudflare.com",
		"netflix.com",
		"amazon.com",
	}
	for _, domain := range allowedSamples {
		if isBlocked(domain) {
			t.Errorf("expected %s to be allowed, but was blocked by domain regex", domain)
		}
	}
}



