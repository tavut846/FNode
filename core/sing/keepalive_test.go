package sing

import (
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

func listenOf(t *testing.T, in option.Inbound) option.ListenOptions {
	t.Helper()
	w, ok := in.Options.(option.ListenOptionsWrapper)
	if !ok {
		t.Fatalf("%s options %T do not expose their listen options", in.Type, in.Options)
	}
	return w.TakeListenOptions()
}

func nodeFor(typ string) *panel.NodeInfo {
	common := panel.CommonNode{ServerPort: 8443}
	n := &panel.NodeInfo{Id: 1, Type: typ, Security: panel.None, Common: &common}
	switch typ {
	case "vless":
		n.VAllss = &panel.VAllssNode{CommonNode: common, Network: "tcp"}
	case "vmess":
		n.VAllss = &panel.VAllssNode{CommonNode: common, Network: "tcp"}
	case "trojan":
		n.Trojan = &panel.TrojanNode{CommonNode: common, Network: "tcp"}
	case "shadowsocks":
		n.Shadowsocks = &panel.ShadowsocksNode{CommonNode: common, Cipher: "aes-128-gcm"}
	case "anytls":
		n.Security = panel.Tls
		n.AnyTls = &panel.AnyTlsNode{CommonNode: common}
	}
	return n
}

// Every TCP inbound probes idle connections from the server side every
// 30s/15s by default, instead of sing-box's 5m/75s, so a NAT or firewall
// does not forget an idle proxy connection (an AnyTLS pooled session).
func TestInbounds_TCPKeepAlive(t *testing.T) {
	b := newTestSing(t, nil)
	cases := []struct {
		name string
		opts conf.Options
		idle time.Duration
		intv time.Duration
		off  bool
	}{
		{"default", conf.Options{}, 30 * time.Second, 15 * time.Second, false},
		{"custom", conf.Options{TCPKeepAlive: 60, TCPKeepAliveInterval: 20}, 60 * time.Second, 20 * time.Second, false},
		{"idle only", conf.Options{TCPKeepAlive: 45}, 45 * time.Second, 15 * time.Second, false},
		{"disabled", conf.Options{TCPKeepAlive: -1}, 0, 0, true},
	}
	for _, typ := range []string{"vless", "vmess", "trojan", "shadowsocks", "anytls"} {
		for _, c := range cases {
			t.Run(typ+"/"+c.name, func(t *testing.T) {
				opts := c.opts
				opts.ListenIP = "0.0.0.0"
				opts.CertConfig = conf.NewCertConfig()
				in, err := b.getInboundOptions("ka-"+typ, nodeFor(typ), &opts)
				if err != nil {
					t.Fatalf("getInboundOptions: %v", err)
				}
				if in.Type != typ {
					t.Fatalf("type %s, want %s", in.Type, typ)
				}
				l := listenOf(t, in)
				if time.Duration(l.TCPKeepAlive) != c.idle || time.Duration(l.TCPKeepAliveInterval) != c.intv || l.DisableTCPKeepAlive != c.off {
					t.Errorf("keepalive idle=%v interval=%v disabled=%v, want %v %v %v",
						time.Duration(l.TCPKeepAlive), time.Duration(l.TCPKeepAliveInterval), l.DisableTCPKeepAlive, c.idle, c.intv, c.off)
				}
			})
		}
	}
}

// The Hysteria2 UDP socket is not shared (SO_REUSEADDR) unless configured.
func TestHysteria2_ReuseAddrIsOptIn(t *testing.T) {
	b := newTestSing(t, nil)
	common := panel.CommonNode{ServerPort: 8443}
	node := &panel.NodeInfo{Id: 2, Type: "hysteria2", Security: panel.Tls, Common: &common,
		Hysteria2: &panel.Hysteria2Node{CommonNode: common}}
	for name, tc := range map[string]struct {
		cfg  *conf.Hysteria2Config
		want bool
	}{
		"unset":   {nil, false},
		"off":     {&conf.Hysteria2Config{}, false},
		"enabled": {&conf.Hysteria2Config{ReuseAddr: true}, true},
	} {
		in, err := b.getInboundOptions("hy2-"+name, node, &conf.Options{ListenIP: "0.0.0.0", CertConfig: conf.NewCertConfig(), Hysteria2Config: tc.cfg})
		if err != nil {
			t.Fatal(err)
		}
		if got := listenOf(t, in).ReuseAddr; got != tc.want {
			t.Errorf("%s: ReuseAddr=%v, want %v", name, got, tc.want)
		}
	}
}

// recreateInbound runs with the users lock held; it must not take it again.
func TestRecreateInbound_DoesNotDeadlock(t *testing.T) {
	b := newTestSing(t, nil)
	tag := "[https://panel.example]-anytls:1"
	info := anytlsNode(freePort(t))
	opts := &conf.Options{ListenIP: "127.0.0.1", CertConfig: conf.NewCertConfig()}
	if err := b.AddNode(tag, info, opts); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		b.users.mapLock.Lock()
		defer b.users.mapLock.Unlock()
		done <- b.recreateInbound(tag, info)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("recreateInbound: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recreateInbound deadlocked on the users lock")
	}
	if _, found := b.box.Inbound().Get(tag); !found {
		t.Fatal("the inbound is gone after recreateInbound")
	}
}
