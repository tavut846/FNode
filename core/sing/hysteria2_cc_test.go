package sing

import (
	"testing"

	"github.com/sagernet/sing-box/option"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

func intp(v int) *int { return &v }

// Brutal sends at a fixed rate and ignores loss; with the panel's up_mbps at
// 0 it was capped by nothing but the client's own declared rate. The cap and
// the BBR opt-in decide what reaches the inbound.
func TestResolveHysteria2Bandwidth(t *testing.T) {
	cases := []struct {
		name         string
		panel        panel.Hysteria2Node
		cfg          *conf.Hysteria2Config
		up, down     int
		ignore       bool
		reasonWanted bool
	}{
		{"panel unlimited gets the default cap", panel.Hysteria2Node{}, nil, 100, 0, false, true},
		{"panel limits win", panel.Hysteria2Node{UpMbps: 50, DownMbps: 20}, nil, 50, 20, false, false},
		{"a large panel limit is kept without MaxBrutalMbps", panel.Hysteria2Node{UpMbps: 500}, nil, 500, 0, false, false},
		{"configured cap for an unlimited panel", panel.Hysteria2Node{}, &conf.Hysteria2Config{MaxBrutalMbps: intp(30)}, 30, 0, false, true},
		{"configured cap lowers a larger panel limit", panel.Hysteria2Node{UpMbps: 500}, &conf.Hysteria2Config{MaxBrutalMbps: intp(200)}, 200, 0, false, true},
		{"configured cap keeps a smaller panel limit", panel.Hysteria2Node{UpMbps: 20}, &conf.Hysteria2Config{MaxBrutalMbps: intp(200)}, 20, 0, false, false},
		{"0 means unlimited", panel.Hysteria2Node{}, &conf.Hysteria2Config{MaxBrutalMbps: intp(0)}, 0, 0, false, false},
		{"-1 means unlimited", panel.Hysteria2Node{}, &conf.Hysteria2Config{MaxBrutalMbps: intp(-1)}, 0, 0, false, false},
		{"unlimited keeps a panel limit", panel.Hysteria2Node{UpMbps: 70}, &conf.Hysteria2Config{MaxBrutalMbps: intp(0)}, 70, 0, false, false},
		{"BBR for everyone forces down 0", panel.Hysteria2Node{DownMbps: 200}, &conf.Hysteria2Config{IgnoreClientBandwidth: true}, 0, 0, true, true},
		{"BBR opt-in beats the cap", panel.Hysteria2Node{UpMbps: 40, DownMbps: 200}, &conf.Hysteria2Config{IgnoreClientBandwidth: true, MaxBrutalMbps: intp(10)}, 40, 0, true, true},
		{"the panel's own ignore flag passes through", panel.Hysteria2Node{UpMbps: 40, DownMbps: 200, Ignore_Client_Bandwidth: true}, nil, 40, 200, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveHysteria2Bandwidth(&c.panel, c.cfg)
			if got.UpMbps != c.up || got.DownMbps != c.down || got.Ignore != c.ignore || (got.Reason != "") != c.reasonWanted {
				t.Errorf("got up=%d down=%d ignore=%v reason=%q, want up=%d down=%d ignore=%v reason=%v",
					got.UpMbps, got.DownMbps, got.Ignore, got.Reason, c.up, c.down, c.ignore, c.reasonWanted)
			}
		})
	}
}

// The resolved values are what the inbound is created with.
func TestHysteria2Inbound_BandwidthReachesOptions(t *testing.T) {
	b := newTestSing(t, nil)
	build := func(h panel.Hysteria2Node, cfg *conf.Hysteria2Config) *option.Hysteria2InboundOptions {
		t.Helper()
		common := panel.CommonNode{ServerPort: 8443}
		h.CommonNode = common
		node := &panel.NodeInfo{Id: 2, Type: "hysteria2", Security: panel.Tls, Common: &common, Hysteria2: &h}
		in, err := b.getInboundOptions("hy2-bw", node, &conf.Options{ListenIP: "0.0.0.0", CertConfig: conf.NewCertConfig(), Hysteria2Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		o, ok := in.Options.(*option.Hysteria2InboundOptions)
		if !ok {
			t.Fatalf("options are %T", in.Options)
		}
		return o
	}
	// the panel sends up/down 0 (empty dashboard fields): capped at 100
	if o := build(panel.Hysteria2Node{}, nil); o.UpMbps != 100 || o.DownMbps != 0 || o.IgnoreClientBandwidth {
		t.Errorf("panel 0/0: up=%d down=%d ignore=%v", o.UpMbps, o.DownMbps, o.IgnoreClientBandwidth)
	}
	// the panel's values are kept
	if o := build(panel.Hysteria2Node{UpMbps: 30, DownMbps: 60}, nil); o.UpMbps != 30 || o.DownMbps != 60 {
		t.Errorf("panel 30/60: up=%d down=%d", o.UpMbps, o.DownMbps)
	}
	// BBR for every client: ignore with receive 0
	if o := build(panel.Hysteria2Node{UpMbps: 30, DownMbps: 60}, &conf.Hysteria2Config{IgnoreClientBandwidth: true}); !o.IgnoreClientBandwidth || o.DownMbps != 0 {
		t.Errorf("BBR opt-in: ignore=%v down=%d", o.IgnoreClientBandwidth, o.DownMbps)
	}
}
