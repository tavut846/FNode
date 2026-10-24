package sing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

func TestConvertPanelOutbound(t *testing.T) {
	m, err := ConvertPanelOutbound(panel.CustomOutbound{
		Tag:      "us-proxy",
		Protocol: "SS",
		Settings: map[string]any{"server": "1.2.3.4", "server_port": 8388, "tag": "ignored", "type": "ignored"},
		ProxyTag: "relay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m["type"] != "shadowsocks" || m["tag"] != "us-proxy" || m["detour"] != "relay" || m["server"] != "1.2.3.4" {
		t.Errorf("unexpected conversion: %v", m)
	}

	// An explicit detour in settings is kept
	m, _ = ConvertPanelOutbound(panel.CustomOutbound{Tag: "a", Protocol: "socks", Settings: map[string]any{"detour": "x"}, ProxyTag: "y"})
	if m["detour"] != "x" {
		t.Errorf("detour should stay x, got %v", m["detour"])
	}
	// proxy_tag pointing at itself is ignored
	m, _ = ConvertPanelOutbound(panel.CustomOutbound{Tag: "a", Protocol: "socks", ProxyTag: "a"})
	if _, ok := m["detour"]; ok {
		t.Errorf("self detour must be dropped: %v", m)
	}
	if _, err = ConvertPanelOutbound(panel.CustomOutbound{Protocol: "socks"}); err == nil {
		t.Error("missing tag must fail")
	}
	if _, err = ConvertPanelOutbound(panel.CustomOutbound{Tag: "a"}); err == nil {
		t.Error("missing protocol must fail")
	}
}

func TestMergeOutbounds_Precedence(t *testing.T) {
	origin := []outboundEntry{{Tag: "direct", Source: SourceOrigin}, {Tag: "shared", Source: SourceOrigin, Hash: "o"}}
	local := []outboundEntry{{Tag: "shared", Source: SourceLocal, Hash: "l"}, {Tag: "local-only", Source: SourceLocal}}
	panelOut := []outboundEntry{{Tag: "shared", Source: SourcePanel, Node: "n1", Hash: "p"}, {Tag: "panel-only", Source: SourcePanel}}

	merged, warnings := mergeOutbounds(origin, local, panelOut)
	if len(merged) != 4 {
		t.Fatalf("expected 4 outbounds, got %d", len(merged))
	}
	want := []string{"direct", "shared", "local-only", "panel-only"}
	for i, tag := range want {
		if merged[i].Tag != tag {
			t.Errorf("position %d: want %s got %s", i, tag, merged[i].Tag)
		}
	}
	if merged[1].Source != SourcePanel || merged[1].Hash != "p" {
		t.Errorf("panel must win on duplicate tag, got %+v", merged[1])
	}
	if len(warnings) != 2 {
		t.Errorf("expected a warning per override, got %v", warnings)
	}
}

func TestPlanOutbounds_RecreatesDependents(t *testing.T) {
	desired := []outboundEntry{
		{Tag: "chain", Hash: "c1", Raw: map[string]any{"detour": "relay"}},
		{Tag: "relay", Hash: "r2", Raw: map[string]any{}},
		{Tag: "direct", Hash: "d1", Raw: map[string]any{}},
	}
	applied := map[string]string{"chain": "c1", "relay": "r1", "direct": "d1", "gone": "g1"}
	create, remove := planOutbounds(desired, applied, true)
	if len(create) != 2 || create[0].Tag != "relay" || create[1].Tag != "chain" {
		t.Errorf("expected relay then chain, got %+v", create)
	}
	if len(remove) != 1 || remove[0] != "gone" {
		t.Errorf("expected gone removed, got %v", remove)
	}
	_, remove = planOutbounds(desired, applied, false)
	if len(remove) != 0 {
		t.Errorf("no removal without prune, got %v", remove)
	}
}

const testOrigin = `{
  "outbounds": [
    {"type": "direct", "tag": "direct"},
    {"type": "block", "tag": "block"},
    {"type": "socks", "tag": "shared", "server": "10.0.0.1", "server_port": 1080}
  ],
  "route": {"rules": [{"domain_suffix": ["origin.example"], "outbound": "shared"}]}
}`

func newTestSing(t *testing.T, local []map[string]any) *Sing {
	t.Helper()
	return newTestSingWith(t, func(c *conf.SingConfig) { c.CustomOutbounds = local })
}

// newTestSingWith builds a core whose SingConfig the caller can adjust.
func newTestSingWith(t *testing.T, adjust func(*conf.SingConfig)) *Sing {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sing_origin.json")
	if err := os.WriteFile(path, []byte(testOrigin), 0o644); err != nil {
		t.Fatal(err)
	}
	sc := &conf.SingConfig{
		OriginalPath: path,
		LogConfig:    conf.SingLogConfig{Level: "error"},
	}
	adjust(sc)
	coreInst, err := New(&conf.CoreConfig{SingConfig: sc})
	if err != nil {
		t.Fatalf("new core: %v", err)
	}
	return coreInst.(*Sing)
}

func setNode(b *Sing, tag string, common *panel.CommonNode) {
	b.users.mapLock.Lock()
	b.inboundInfo[tag] = &panel.NodeInfo{Type: "vless", Common: common}
	b.users.mapLock.Unlock()
}

func sourceOf(b *Sing, tag string) string {
	for _, e := range b.last.Outbounds {
		if e.Tag == tag {
			return e.Source
		}
	}
	return ""
}

func TestSing_ApplyPanelOutboundsAndRules(t *testing.T) {
	b := newTestSing(t, []map[string]any{
		{"type": "socks", "tag": "shared", "server": "10.0.0.2", "server_port": 1080},
		{"type": "socks", "tag": "local-only", "server": "10.0.0.3", "server_port": 1080},
	})
	if src := b.baseOutbounds[2].Source; src != SourceLocal {
		t.Fatalf("local CustomOutbounds must override origin, got %s", src)
	}

	setNode(b, "node-a", &panel.CommonNode{
		Routes: []panel.Route{
			{Id: 1, Match: []any{"netflix.com"}, Action: "proxy", ActionValue: "us-proxy"},
			{Id: 2, Match: "typo.example", Action: "proxy", ActionValue: "missing-tag"},
			{Id: 3, Match: "example.org", Action: "dns", ActionValue: "1.1.1.1"},
		},
		CustomOutbounds: []panel.CustomOutbound{
			{Tag: "us-proxy", Protocol: "shadowsocks", Settings: map[string]any{
				"server": "1.2.3.4", "server_port": 8388, "method": "aes-256-gcm", "password": "p"}, ProxyTag: "relay"},
			{Tag: "relay", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.8", "server_port": 1080}},
			{Tag: "shared", Protocol: "socks", Settings: map[string]any{"server": "9.9.9.9", "server_port": 1080}},
		},
		CustomRoutes: []map[string]any{{"domain_suffix": []any{"openai.com"}, "outbound": "relay"}},
	})
	if err := b.applyPanelState(true); err != nil {
		t.Fatalf("apply: %v", err)
	}

	manager := b.box.Outbound()
	for _, tag := range []string{"us-proxy", "relay", "shared", "local-only", "direct", "block"} {
		if _, ok := manager.Outbound(tag); !ok {
			t.Errorf("outbound %s must exist in the OutboundManager", tag)
		}
	}
	if got := sourceOf(b, "shared"); got != SourcePanel {
		t.Errorf("panel must win on duplicate tag, got %s", got)
	}
	if b.last.Counts[SourcePanel] != 3 || b.last.Counts[SourceOrigin] != 2 || b.last.Counts[SourceLocal] != 1 {
		t.Errorf("unexpected counts %v", b.last.Counts)
	}

	var proxyRule, rawRule, typoRule bool
	for _, r := range b.last.Rules {
		switch r["outbound"] {
		case "us-proxy":
			proxyRule = true
		case "relay":
			rawRule = true
		case "missing-tag", "1.1.1.1":
			typoRule = true
		}
	}
	if !proxyRule {
		t.Error("panel proxy route must resolve to the panel outbound")
	}
	if !rawRule {
		t.Error("panel custom_routes must be applied")
	}
	if typoRule {
		t.Error("rules to unknown outbounds (and dns routes) must be dropped")
	}

	// The panel drops its outbounds: they are removed, origin "shared" is restored
	setNode(b, "node-a", &panel.CommonNode{})
	if err := b.applyPanelState(true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, tag := range []string{"us-proxy", "relay"} {
		if _, ok := manager.Outbound(tag); ok {
			t.Errorf("outbound %s must be removed", tag)
		}
	}
	if got := sourceOf(b, "shared"); got != SourceLocal {
		t.Errorf("shared must fall back to the local outbound, got %s", got)
	}
}

func TestSing_DelNodeKeepsOutboundsUntilPrune(t *testing.T) {
	b := newTestSing(t, nil)
	setNode(b, "node-a", &panel.CommonNode{CustomOutbounds: []panel.CustomOutbound{
		{Tag: "p1", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.8", "server_port": 1080}},
	}})
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	b.users.mapLock.Lock()
	delete(b.inboundInfo, "node-a")
	b.users.mapLock.Unlock()
	if err := b.applyPanelState(false); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.box.Outbound().Outbound("p1"); !ok {
		t.Error("non-pruning apply must keep the outbound")
	}
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.box.Outbound().Outbound("p1"); ok {
		t.Error("pruning apply must remove the outbound")
	}
}

func TestSing_ApplyOnStartedBox(t *testing.T) {
	b := newTestSing(t, nil)
	if err := b.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Close()
	setNode(b, "node-a", &panel.CommonNode{
		Routes: []panel.Route{{Id: 1, Match: "netflix.com", Action: "proxy", ActionValue: "chain"}},
		CustomOutbounds: []panel.CustomOutbound{
			{Tag: "chain", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.8", "server_port": 1080}, ProxyTag: "relay"},
			{Tag: "relay", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.9", "server_port": 1080}},
		},
	})
	if err := b.applyPanelState(true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, tag := range []string{"chain", "relay"} {
		if _, ok := b.box.Outbound().Outbound(tag); !ok {
			t.Fatalf("outbound %s must be live on a started box", tag)
		}
	}
	// Change the relay: chain depends on it and is recreated too
	setNode(b, "node-a", &panel.CommonNode{
		Routes: []panel.Route{{Id: 1, Match: "netflix.com", Action: "proxy", ActionValue: "chain"}},
		CustomOutbounds: []panel.CustomOutbound{
			{Tag: "chain", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.8", "server_port": 1080}, ProxyTag: "relay"},
			{Tag: "relay", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.10", "server_port": 1080}},
		},
	})
	if err := b.applyPanelState(true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if b.last.Counts[SourcePanel] != 2 {
		t.Errorf("both panel outbounds must stay applied: %v", b.last.Counts)
	}
}
