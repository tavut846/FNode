package sing

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

func panelMux(enabled *bool, padding bool, brutal panel.MultiplexBrutal) *panel.Multiplex {
	return &panel.Multiplex{Enabled: enabled, Padding: padding, Brutal: brutal}
}

func bp(v bool) *bool { return &v }

func infoWithMux(m *panel.Multiplex, flow string) *panel.NodeInfo {
	return &panel.NodeInfo{Type: "vless", Common: &panel.CommonNode{ServerPort: 443, Multiplex: m},
		VAllss: &panel.VAllssNode{Flow: flow}}
}

func TestResolveMultiplex(t *testing.T) {
	localOn := &conf.MultiplexConfig{Enabled: true, Padding: true}
	cases := []struct {
		name    string
		info    *panel.NodeInfo
		local   *conf.MultiplexConfig
		enabled bool
		padding bool
		brutal  bool
		isNil   bool
	}{
		{"panel on", infoWithMux(panelMux(bp(true), false, panel.MultiplexBrutal{}), ""), nil, true, false, false, false},
		{"panel on with padding", infoWithMux(panelMux(bp(true), true, panel.MultiplexBrutal{}), ""), nil, true, true, false, false},
		{"panel on beats local off", infoWithMux(panelMux(bp(true), false, panel.MultiplexBrutal{}), ""), &conf.MultiplexConfig{}, true, false, false, false},
		{"panel padding is exact, not the local one", infoWithMux(panelMux(bp(true), false, panel.MultiplexBrutal{}), ""), localOn, true, false, false, false},
		{"panel off beats local on", infoWithMux(panelMux(bp(false), true, panel.MultiplexBrutal{}), ""), localOn, false, false, false, true},
		{"no panel value: local fallback", infoWithMux(nil, ""), localOn, true, true, false, false},
		{"panel unspecified: local fallback", infoWithMux(panelMux(nil, true, panel.MultiplexBrutal{}), ""), localOn, true, true, false, false},
		{"nothing configured", infoWithMux(nil, ""), nil, false, false, false, true},
		{"brutal with rates", infoWithMux(panelMux(bp(true), false, panel.MultiplexBrutal{Enabled: true, UpMbps: 50, DownMbps: 100}), ""), nil, true, false, true, false},
		{"brutal without rates is dropped, not fatal", infoWithMux(panelMux(bp(true), false, panel.MultiplexBrutal{Enabled: true}), ""), nil, true, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveMultiplex("n", c.info, c.local)
			if c.isNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil")
			}
			if got.Enabled != c.enabled || got.Padding != c.padding || (got.Brutal != nil && got.Brutal.Enabled) != c.brutal {
				t.Errorf("got %+v brutal=%+v, want enabled=%v padding=%v brutal=%v", got, got.Brutal, c.enabled, c.padding, c.brutal)
			}
		})
	}
	// the panel's rates reach the options
	got := resolveMultiplex("n", infoWithMux(panelMux(bp(true), false, panel.MultiplexBrutal{Enabled: true, UpMbps: 50, DownMbps: 100}), ""), nil)
	if got.Brutal.UpMbps != 50 || got.Brutal.DownMbps != 100 {
		t.Errorf("brutal rates: %+v", got.Brutal)
	}
}

// The panel's multiplex reaches every TCP inbound type, including VLESS with
// Reality and Vision, which is what clients are told to use.
func TestInbounds_PanelMultiplexReachesOptions(t *testing.T) {
	b := newTestSing(t, nil)
	mux := panelMux(bp(true), true, panel.MultiplexBrutal{})
	common := panel.CommonNode{ServerPort: 443, Multiplex: mux}

	reality := &panel.NodeInfo{Id: 1, Type: "vless", Security: panel.Reality, Common: &common,
		VAllss: &panel.VAllssNode{CommonNode: common, Tls: panel.Reality, Flow: "xtls-rprx-vision",
			TlsSettings: panel.TlsSettings{ServerName: "gateway.icloud.com", ServerPort: 443,
				PrivateKey: "u123456789012345678901234567890123456789012", ShortIds: []string{"0123456789abcdef"}}}}
	in, err := b.getInboundOptions("vless-reality", reality, &conf.Options{ListenIP: "0.0.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	v, ok := in.Options.(*option.VLESSInboundOptions)
	if !ok || v.Multiplex == nil || !v.Multiplex.Enabled || !v.Multiplex.Padding || v.Multiplex.Brutal != nil {
		t.Fatalf("VLESS Reality: %T %+v", in.Options, v.Multiplex)
	}
	if v.TLS == nil || v.TLS.Reality == nil || !v.TLS.Reality.Enabled {
		t.Error("Reality must stay enabled with multiplex")
	}

	for typ, node := range map[string]*panel.NodeInfo{
		"vmess":       {Id: 2, Type: "vmess", Common: &common, VAllss: &panel.VAllssNode{CommonNode: common, Network: "tcp"}},
		"trojan":      {Id: 3, Type: "trojan", Common: &common, Trojan: &panel.TrojanNode{CommonNode: common, Network: "tcp"}},
		"shadowsocks": {Id: 4, Type: "shadowsocks", Common: &common, Shadowsocks: &panel.ShadowsocksNode{CommonNode: common, Cipher: "aes-128-gcm"}},
	} {
		in, err := b.getInboundOptions("mux-"+typ, node, &conf.Options{ListenIP: "0.0.0.0", CertConfig: conf.NewCertConfig(), SingOptions: &conf.SingOptions{}})
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		var got *option.InboundMultiplexOptions
		switch o := in.Options.(type) {
		case *option.VMessInboundOptions:
			got = o.Multiplex
		case *option.TrojanInboundOptions:
			got = o.Multiplex
		case *option.ShadowsocksInboundOptions:
			got = o.Multiplex
		default:
			t.Fatalf("%s: %T", typ, in.Options)
		}
		if got == nil || !got.Enabled || !got.Padding {
			t.Errorf("%s: multiplex %+v", typ, got)
		}
	}

	// the panel sends none: the local config.json is the fallback
	noPanel := &panel.NodeInfo{Id: 5, Type: "vless", Security: panel.None, Common: &panel.CommonNode{ServerPort: 443},
		VAllss: &panel.VAllssNode{Network: "tcp"}}
	in, err = b.getInboundOptions("mux-local", noPanel, &conf.Options{ListenIP: "0.0.0.0",
		SingOptions: &conf.SingOptions{Multiplex: &conf.MultiplexConfig{Enabled: true, Padding: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if m := in.Options.(*option.VLESSInboundOptions).Multiplex; m == nil || !m.Enabled || !m.Padding {
		t.Errorf("local fallback: %+v", m)
	}
}

// One info line says whether multiplex is on and where that came from; Vision
// is not warned about; Brutal warns about the kernel module.
func TestResolveMultiplex_Logs(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	resolveMultiplex("n", infoWithMux(panelMux(bp(true), true, panel.MultiplexBrutal{}), "xtls-rprx-vision"), nil)
	var info, warn int
	for _, e := range hook.AllEntries() {
		switch {
		case e.Message == "Multiplex is on" && e.Data["source"] == "panel" && e.Data["padding"] == true:
			info++
		case e.Level.String() == "warning":
			warn++
		}
	}
	if info != 1 || warn != 0 {
		t.Errorf("Vision + panel multiplex: %d info lines (want 1), %d warnings (want 0): %+v", info, warn, hook.AllEntries())
	}

	hook.Reset()
	resolveMultiplex("n", infoWithMux(panelMux(bp(true), false, panel.MultiplexBrutal{Enabled: true, UpMbps: 10, DownMbps: 10}), ""), nil)
	var brutalWarn bool
	for _, e := range hook.AllEntries() {
		if e.Level.String() == "warning" && strings.Contains(e.Message, "brutal kernel module") {
			brutalWarn = true
		}
	}
	if !brutalWarn {
		t.Errorf("no Brutal kernel module warning: %+v", hook.AllEntries())
	}

	hook.Reset()
	resolveMultiplex("n", infoWithMux(panelMux(bp(false), false, panel.MultiplexBrutal{}), ""), &conf.MultiplexConfig{Enabled: true})
	if es := hook.AllEntries(); len(es) != 1 || es[0].Message != "Multiplex is off" || es[0].Data["source"] != "panel" {
		t.Errorf("panel off: %+v", es)
	}
}

// sing-box builds the multiplex router when it creates the inbound, so this
// is where bad options (Brutal without rates) would stop a node from starting.
func TestAddNode_WithPanelMultiplex(t *testing.T) {
	for name, m := range map[string]*panel.Multiplex{
		"on":              panelMux(bp(true), false, panel.MultiplexBrutal{}),
		"padding":         panelMux(bp(true), true, panel.MultiplexBrutal{}),
		"brutal no rates": panelMux(bp(true), true, panel.MultiplexBrutal{Enabled: true}),
		"off":             panelMux(bp(false), true, panel.MultiplexBrutal{}),
	} {
		t.Run(name, func(t *testing.T) {
			b := newTestSing(t, nil)
			common := panel.CommonNode{ServerPort: freePort(t), Multiplex: m}
			node := &panel.NodeInfo{Id: 1, Type: "vless", Security: panel.None, Common: &common,
				VAllss: &panel.VAllssNode{CommonNode: common, Network: "tcp"}}
			if err := b.AddNode("[https://p.example]-vless:1", node, &conf.Options{ListenIP: "127.0.0.1", CertConfig: conf.NewCertConfig()}); err != nil {
				t.Fatalf("AddNode: %v", err)
			}
			if _, ok := b.box.Inbound().Get("[https://p.example]-vless:1"); !ok {
				t.Fatal("inbound not created")
			}
		})
	}
}
