package sing

import (
	"net/netip"
	"testing"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

func landingNode() *panel.CommonNode {
	return &panel.CommonNode{
		ServerPort: 8443,
		Routes:     []panel.Route{{Id: 1, Match: []any{"netflix.com"}, Action: "proxy", ActionValue: "back"}},
		CustomOutbounds: []panel.CustomOutbound{
			{Tag: "back", Protocol: "socks", Settings: map[string]any{"server": "127.0.0.1", "server_port": 1080}},
			{Tag: "back-chain", Protocol: "socks", Settings: map[string]any{"server": "8.8.8.8", "server_port": 1080}, ProxyTag: "back"},
			{Tag: "ok", Protocol: "socks", Settings: map[string]any{"server": "5.6.7.8", "server_port": 1080}},
		},
	}
}

func hasOutbound(b *Sing, tag string) bool {
	_, ok := b.box.Outbound().Outbound(tag)
	return ok
}

func TestSelfGuard_PanelOutboundToThisMachineIsSkippedAndNodeStarts(t *testing.T) {
	b := newTestSing(t, nil)
	setNode(b, "node-a", landingNode())
	if err := b.applyPanelState(true); err != nil {
		t.Fatalf("apply must not fail: %v", err)
	}
	if hasOutbound(b, "back") || hasOutbound(b, "back-chain") {
		t.Error("the loopback outbound and the one that detours through it must be skipped")
	}
	if !hasOutbound(b, "ok") || !hasOutbound(b, "direct") {
		t.Error("other outbounds must stay")
	}
	for _, r := range b.last.Rules {
		if ruleOutbound(r) == "back" {
			t.Errorf("a route to the skipped outbound must be dropped: %v", r)
		}
	}
}

func TestIgnorePanelOutbounds_AllAndByTag(t *testing.T) {
	b := newTestSingWith(t, func(c *conf.SingConfig) { c.IgnorePanelOutbounds = true })
	n := landingNode()
	n.CustomOutbounds[0].Settings["server"] = "9.9.9.9"
	setNode(b, "node-a", n)
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	if b.last.Counts[SourcePanel] != 0 || hasOutbound(b, "ok") || hasOutbound(b, "back") {
		t.Errorf("IgnorePanelOutbounds must drop every panel outbound: %v", b.last.Counts)
	}
	for _, r := range b.last.Rules {
		if ruleOutbound(r) == "back" {
			t.Errorf("route to an ignored outbound must be dropped: %v", r)
		}
	}

	b = newTestSingWith(t, func(c *conf.SingConfig) { c.IgnorePanelOutboundTags = []string{"back"} })
	n = landingNode()
	n.CustomOutbounds[0].Settings["server"] = "9.9.9.9"
	setNode(b, "node-a", n)
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	if hasOutbound(b, "back") || hasOutbound(b, "back-chain") || !hasOutbound(b, "ok") {
		t.Error("only the listed tag (and what detours through it) is dropped")
	}
}

func TestIgnorePanelOutbounds_DefaultKeepsBehaviour(t *testing.T) {
	b := newTestSing(t, nil)
	n := landingNode()
	n.CustomOutbounds[0].Settings["server"] = "9.9.9.9"
	setNode(b, "node-a", n)
	if err := b.applyPanelState(true); err != nil {
		t.Fatal(err)
	}
	if b.last.Counts[SourcePanel] != 3 {
		t.Errorf("by default all panel outbounds apply: %v", b.last.Counts)
	}
}

func TestSelfGuard_LocalOutboundOnlySkippedWhenItHitsOwnInbound(t *testing.T) {
	g := selfGuard{addrs: []netip.Addr{netip.MustParseAddr("203.0.113.5")}, ports: map[int]bool{8443: true}}
	mk := func(src, server string, port int) outboundEntry {
		return outboundEntry{Tag: server, Source: src, Raw: map[string]any{"type": "socks", "tag": server, "server": server, "server_port": port}}
	}
	cases := []struct {
		e    outboundEntry
		skip bool
	}{
		{mk(SourcePanel, "203.0.113.5", 1080), true},
		{mk(SourcePanel, "::1", 1080), true},
		{mk(SourcePanel, "198.51.100.9", 1080), false},
		{mk(SourceLocal, "127.0.0.1", 40000), false}, // e.g. a local WARP/socks helper
		{mk(SourceLocal, "127.0.0.1", 8443), true},
		{mk(SourceOrigin, "203.0.113.5", 8443), true},
	}
	for _, c := range cases {
		kept, _, _ := dropUnsafeOutbounds([]outboundEntry{c.e}, g)
		if (len(kept) == 0) != c.skip {
			t.Errorf("%s %v: skipped=%v, want %v", c.e.Source, c.e.Raw["server"], len(kept) == 0, c.skip)
		}
	}
}
