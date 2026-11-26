package sing

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	R "github.com/sagernet/sing-box/route/rule"
	"github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/tavut846/FNode/api/panel"
)

const route9Tag = "[https://myboard.example]-vless:362"

func route9() panel.Route {
	return panel.Route{
		Id: 9, Remarks: "AI Domain - IPv6", Action: "proxy", ActionValue: "direct6",
		Match: []any{"chat.com", "*.chat.com", "chatgpt.com", "*.chatgpt.com", "anthropic.com", "*.anthropic.com", "claude.ai", "*.claude.ai"},
	}
}

// Route #9 ("AI Domain - IPv6" -> direct6) compiles to a rule scoped to the
// node's inbound that matches both the apex and any subdomain, and it is kept
// even though the (legacy) sing_origin.json has no direct6 outbound.
func TestRoute9_MatchesAndIsNotDropped(t *testing.T) {
	b := newTestSing(t, nil) // testOrigin has no direct6: a pre-direct6 / migrated origin
	if _, ok := b.box.Outbound().Outbound("direct6"); !ok {
		t.Fatal("direct6 must be built in when the origin lacks it")
	}
	setNode(b, route9Tag, &panel.CommonNode{Routes: []panel.Route{route9()}})
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	var found map[string]any
	for _, r := range b.last.Rules {
		if r["outbound"] == "direct6" {
			found = r
		}
	}
	if found == nil {
		t.Fatalf("route #9 was dropped: %v", b.last.Rules)
	}
	rule, err := json.UnmarshalExtendedContext[option.Rule](b.ctx, mustJSON(t, found))
	if err != nil {
		t.Fatal(err)
	}
	built, err := R.NewRule(b.ctx, log.NewNOPFactory().NewLogger("t"), rule, false)
	if err != nil {
		t.Fatal(err)
	}
	match := func(inbound, domain string) bool {
		return built.Match(&adapter.InboundContext{Inbound: inbound, Domain: domain})
	}
	for _, d := range []string{"ab.chatgpt.com", "chatgpt.com", "chat.com", "a.b.claude.ai", "api.anthropic.com"} {
		if !match(route9Tag, d) {
			t.Errorf("%s must match route #9", d)
		}
	}
	if match(route9Tag, "notchatgpt.com") || match(route9Tag, "example.org") {
		t.Error("unrelated domains must not match")
	}
	if match("[https://myboard.example]-vless:1", "ab.chatgpt.com") {
		t.Error("route must be scoped to node 362's inbound")
	}
	if b.hookServer.routeName(built) != "AI Domain - IPv6 (#9)" {
		t.Errorf("route name: %q", b.hookServer.routeName(built))
	}
}

// A user-defined direct6 wins over the built-in one.
func TestWithBuiltinDirect6_KeepsUserDefinition(t *testing.T) {
	local := []map[string]any{{"type": "direct", "tag": "direct6", "bind_interface": "eth0"}}
	if got := withBuiltinDirect6(local, nil, time.Second); len(got) != 1 {
		t.Errorf("user direct6 must not be duplicated: %v", got)
	}
	if got := withBuiltinDirect6(nil, nil, 0); len(got) != 1 || got[0]["domain_strategy"] != "prefer_ipv6" {
		t.Errorf("built-in direct6 must prefer IPv6: %v", got)
	}
}

// The unknown-outbound warning names the route and the node.
func TestFilterRulesMissing_WarningNamesRoute(t *testing.T) {
	rules := compileScopedRules(false, nil, []nodeRouting{{tag: "n1", routes: []panel.Route{
		{Id: 4, Remarks: "Typo", Match: "x.example", Action: "proxy", ActionValue: "nope"}}}})
	_, warns, missing := filterRulesMissing(rules, map[string]bool{})
	if len(warns) != 1 || !strings.Contains(warns[0], `"Typo (#4)"`) || !strings.Contains(warns[0], `"nope"`) || !strings.Contains(warns[0], "n1") {
		t.Errorf("warning: %v", warns)
	}
	if len(missing["n1"]) != 1 {
		t.Errorf("missing: %v", missing)
	}
}

// Every routed connection logs "route <name> -> <outbound> for <dest>" through
// the core logger at info level.
func TestLogRoute_Line(t *testing.T) {
	var buf bytes.Buffer
	f := log.NewDefaultFactory(context.Background(), log.Formatter{DisableColors: true}, &buf, "", nil, false)
	if err := f.Start(); err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.SetLevel(log.LevelInfo)
	h := &HookServer{logger: f.NewLogger("")}
	m := adapter.InboundContext{Inbound: route9Tag, Destination: M.ParseSocksaddrHostPort("ab.chatgpt.com", 443)}
	h.logRoute(context.Background(), m, nil, nil)
	time.Sleep(50 * time.Millisecond)
	if out := buf.String(); !strings.Contains(out, "route default -> unknown for ab.chatgpt.com:443") {
		t.Errorf("log line: %q", out)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	d, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
