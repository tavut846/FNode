package sing

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/conf"
)

func rulesFor(rules []map[string]any, outbound string) []map[string]any {
	var out []map[string]any
	for _, r := range rules {
		if r["outbound"] == outbound {
			out = append(out, r)
		}
	}
	return out
}

func inboundsOf(r map[string]any) []string { return ruleInbounds(r) }

// Panel routes only match their own node's inbound: two nodes on one FNode
// (machine mode) must not route each other's traffic.
func TestCompileScopedRules_PerNodeInbound(t *testing.T) {
	rules := compileScopedRules(true, []conf.CustomRouteRule{
		{Match: conf.CustomRouteMatch{Domains: []string{"local.example"}}, Action: conf.CustomRouteAction{Type: "block"}},
	}, []nodeRouting{
		{tag: "node-a", routes: []panel.Route{{Id: 1, Match: "netflix.com", Action: "proxy", ActionValue: "us"}}},
		{tag: "node-b", routes: []panel.Route{{Id: 2, Match: "netflix.com", Action: "block"}},
			rawRoutes: []map[string]any{{"domain_suffix": []any{"openai.com"}, "outbound": "relay"}}},
	})
	us := rulesFor(rules, "us")
	if len(us) != 1 || len(inboundsOf(us[0])) != 1 || inboundsOf(us[0])[0] != "node-a" {
		t.Fatalf("node-a's route must be scoped to node-a: %v", us)
	}
	relay := rulesFor(rules, "relay")
	if len(relay) != 1 || inboundsOf(relay[0])[0] != "node-b" {
		t.Fatalf("node-b's raw route must be scoped to node-b: %v", relay)
	}
	// local custom rules, anti-SSRF and the IPv6 block stay global
	for _, r := range rulesFor(rules, "block") {
		if _, ok := r["domain"]; ok && r["inbound"] != nil {
			t.Errorf("local custom rule must stay global: %v", r)
		}
		if _, ok := r["ip_cidr"]; ok && r["inbound"] != nil {
			t.Errorf("anti-SSRF rule must stay global: %v", r)
		}
		if _, ok := r["ip_version"]; ok && r["inbound"] != nil {
			t.Errorf("IPv6 block must stay global: %v", r)
		}
	}
	// order: local custom, node raw routes, anti-SSRF, ipv6, node routes
	if _, ok := rules[0]["domain"]; !ok {
		t.Errorf("local custom rules come first: %v", rules[0])
	}
	if rules[1]["outbound"] != "relay" {
		t.Errorf("node raw routes come before anti-SSRF: %v", rules[1])
	}
}

func TestScopeRule_KeepsExplicitInboundAndLogical(t *testing.T) {
	explicit := map[string]any{"inbound": []string{"x"}, "outbound": "direct"}
	if got := scopeRule(explicit, "node"); inboundsOf(got)[0] != "x" {
		t.Errorf("an explicit inbound is kept: %v", got)
	}
	logical := map[string]any{"type": "logical", "mode": "and", "rules": []any{}, "outbound": "direct"}
	if got := scopeRule(logical, "node"); got["inbound"] != nil {
		t.Errorf("logical rules have no top-level inbound: %v", got)
	}
}

func TestCompilePanelRoute_GeoItems(t *testing.T) {
	rules := compilePanelRoute(panel.Route{Id: 9, Match: []any{"geoip:private", "geosite:netflix", "geoip:cn", "example.com"}, Action: "block"})
	var private, suffix bool
	for _, r := range rules {
		if _, ok := r["geosite"]; ok {
			t.Errorf("geosite is not emitted (sing-box removed it): %v", r)
		}
		if _, ok := r["geoip"]; ok {
			t.Errorf("geoip is not emitted (sing-box removed it): %v", r)
		}
		if r["ip_is_private"] == true {
			private = true
		}
		if _, ok := r["domain_suffix"]; ok {
			suffix = true
		}
	}
	if !private || !suffix {
		t.Errorf("geoip:private becomes ip_is_private and plain domains stay: %v", rules)
	}
}

func TestSanitizeRawRule(t *testing.T) {
	if got := sanitizeRawRule(map[string]any{"geosite": []any{"cn"}, "outbound": "block"}); got != nil {
		t.Errorf("a rule left with no match is dropped: %v", got)
	}
	got := sanitizeRawRule(map[string]any{"geoip": []any{"private"}, "outbound": "block"})
	if got["ip_is_private"] != true || got["geoip"] != nil {
		t.Errorf("geoip private is translated: %v", got)
	}
	keep := map[string]any{"domain": []any{"a.com"}, "outbound": "direct"}
	if got := sanitizeRawRule(keep); len(got) != 2 {
		t.Errorf("clean rules are unchanged: %v", got)
	}
}

func TestFailedRuleIndex(t *testing.T) {
	if i := failedRuleIndex(errors.New("parse rule[12]: geosite database is deprecated")); i != 12 {
		t.Errorf("index 12, got %d", i)
	}
	if i := failedRuleIndex(errors.New("something else")); i != -1 {
		t.Errorf("no index, got %d", i)
	}
}

// A rule the router rejects is skipped; the other panel rules still apply.
// (Before, one bad rule made UpdateRules fail and the old routing stayed.)
func TestSing_RejectedRuleDoesNotBlockRouting(t *testing.T) {
	b := newTestSing(t, nil)
	setNode(b, "node-a", &panel.CommonNode{
		Routes: []panel.Route{{Id: 1, Match: "netflix.com", Action: "proxy", ActionValue: "us"}},
		CustomOutbounds: []panel.CustomOutbound{
			{Tag: "us", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.8", "server_port": 1080}},
		},
		// geosite nested in a logical rule: decodes, but the router rejects it
		CustomRoutes: []map[string]any{{"type": "logical", "mode": "or",
			"rules": []any{map[string]any{"geosite": []any{"cn"}}}, "outbound": "direct"}},
	})
	if err := b.applyPanelState(true); err != nil {
		t.Fatalf("apply must succeed despite a bad rule: %v", err)
	}
	if len(rulesFor(b.last.Rules, "us")) != 1 {
		t.Errorf("the valid proxy rule must be applied: %v", b.last.Rules)
	}
	for _, r := range b.last.Rules {
		if r["type"] == "logical" {
			t.Errorf("the rejected rule must be dropped: %v", r)
		}
	}
}

// Routes to an outbound FNode does not have are reported for the node, so
// the panel plugin can add it; the report clears once the outbound exists.
func TestSing_ReportsMissingOutbounds(t *testing.T) {
	hub := logship.Default
	s := logship.NewShipper(nopSender{})
	hub.Register(s)
	defer hub.Unregister(s)
	hub.MapNode("node-miss", 41, s)
	defer hub.UnmapNode("node-miss")

	b := newTestSing(t, nil)
	setNode(b, "node-miss", &panel.CommonNode{
		Routes: []panel.Route{{Id: 1, Match: "netflix.com", Action: "proxy", ActionValue: "hk-proxy"}},
	})
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	if got := s.Missing()[41]; len(got) != 1 || got[0] != "hk-proxy" {
		t.Fatalf("missing outbound must be reported for node 41: %v", s.Missing())
	}
	setNode(b, "node-miss", &panel.CommonNode{
		Routes: []panel.Route{{Id: 1, Match: "netflix.com", Action: "proxy", ActionValue: "hk-proxy"}},
		CustomOutbounds: []panel.CustomOutbound{
			{Tag: "hk-proxy", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.8", "server_port": 1080}},
		},
	})
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	if got := s.Missing(); len(got) != 0 {
		t.Errorf("the request clears once the outbound exists: %v", got)
	}
}

func TestDefaultOrigin_MatchesRequestedDefaults(t *testing.T) {
	opts, err := json.UnmarshalExtendedContext[option.Options](newTestSing(t, nil).ctx, DefaultOrigin)
	if err != nil {
		t.Fatalf("default origin must parse: %v", err)
	}
	var tags []string
	for _, o := range opts.Outbounds {
		tags = append(tags, o.Tag)
	}
	if len(tags) != 3 || tags[0] != "direct" || tags[1] != "direct6" || tags[2] != "block" {
		t.Errorf("outbounds direct, direct6, block: %v", tags)
	}
	if opts.Route == nil || !opts.Route.AutoDetectInterface || len(opts.Route.Rules) != 5 {
		t.Fatalf("route: auto_detect_interface and 5 rules")
	}
}

// A missing or broken sing_origin.json falls back to the default instead of
// failing the core (which kept the whole node offline).
func TestNew_OriginFallback(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"missing": filepath.Join(dir, "nope.json"),
		"broken":  broken,
		"empty":   "",
	} {
		c, err := New(&conf.CoreConfig{SingConfig: &conf.SingConfig{OriginalPath: path, LogConfig: conf.SingLogConfig{Level: "error"}}})
		if err != nil {
			t.Fatalf("%s origin must not fail the core: %v", name, err)
		}
		b := c.(*Sing)
		if _, ok := b.box.Outbound().Outbound("direct6"); !ok {
			t.Errorf("%s origin uses the default outbounds (direct6 missing)", name)
		}
		if len(b.originRules) != 5 {
			t.Errorf("%s origin uses the default rules, got %d", name, len(b.originRules))
		}
	}
}

// Panel routes are matched in route id order (the dashboard order), whatever
// order the panel sent them in; custom_routes keep their array order.
func TestCollectPanelState_RoutesSortedById(t *testing.T) {
	b := newTestSing(t, nil)
	tag := "[https://panel.example]-vless:1"
	b.users.mapLock.Lock()
	b.inboundInfo[tag] = &panel.NodeInfo{Type: "vless", Common: &panel.CommonNode{
		Routes: []panel.Route{
			{Id: 5, Match: "five.example", Action: "proxy", ActionValue: "out5"},
			{Id: 2, Match: "two.example", Action: "proxy", ActionValue: "out2"},
			{Id: 7, Match: "main", Action: "dns", ActionValue: "1.1.1.1"},
			{Id: 9, Match: "nine.example", Action: "proxy", ActionValue: "out9"},
		},
		CustomRoutes: []map[string]any{
			{"domain_suffix": []any{"z.example"}, "outbound": "rawZ"},
			{"domain_suffix": []any{"a.example"}, "outbound": "rawA"},
		},
	}}
	b.users.mapLock.Unlock()

	st := b.collectPanelState()
	var order []string
	for _, r := range compileScopedRules(false, nil, st.nodes) {
		out, _ := r["outbound"].(string)
		switch out {
		case "out2", "out5", "out9", "rawZ", "rawA":
			if ins := inboundsOf(r); len(ins) != 1 || ins[0] != tag {
				t.Errorf("rule not scoped to its node: %v", r)
			}
			order = append(order, out)
		}
	}
	want := []string{"rawZ", "rawA", "out2", "out5", "out9"}
	if len(order) != len(want) {
		t.Fatalf("compiled order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("compiled order %v, want %v", order, want)
		}
	}
}
