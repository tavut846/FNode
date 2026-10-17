package sing

import (
	"testing"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	"github.com/sagernet/sing-box/option"
)

func TestGetInboundOptions_VLESS_Reality(t *testing.T) {
	s := &Sing{}
	opt := &conf.Options{
		ListenIP: "0.0.0.0",
		SingOptions: &conf.SingOptions{
			TCPFastOpen: false,
		},
	}
	node := &panel.NodeInfo{
		Id:       1,
		Type:     "vless",
		Security: panel.Reality,
		Common: &panel.CommonNode{
			ServerPort: 443,
		},
		VAllss: &panel.VAllssNode{
			CommonNode: panel.CommonNode{
				ServerPort: 443,
			},
			Tls: panel.Reality,
			TlsSettings: panel.TlsSettings{
				ServerName: "gateway.icloud.com",
				ServerPort: 443,
				PrivateKey: "u123456789012345678901234567890123456789012",
				ShortIds:   []string{"0123456789abcdef"},
			},
			Flow: "xtls-rprx-vision",
		},
	}

	in, err := s.getInboundOptions("vless-test", node, opt)
	if err != nil {
		t.Fatalf("getInboundOptions error: %s", err)
	}

	if in.Type != "vless" {
		t.Fatalf("expected inbound type vless, got %s", in.Type)
	}

	vlessOpt, ok := in.Options.(*option.VLESSInboundOptions)
	if !ok {
		t.Fatalf("expected *option.VLESSInboundOptions, got %T", in.Options)
	}

	if vlessOpt.TLS == nil || !vlessOpt.TLS.Enabled {
		t.Fatalf("expected TLS to be enabled")
	}

	// CRITICAL: ServerName must match the camouflage domain (SNI)
	if vlessOpt.TLS.ServerName != "gateway.icloud.com" {
		t.Errorf("expected TLS.ServerName gateway.icloud.com, got %s", vlessOpt.TLS.ServerName)
	}

	reality := vlessOpt.TLS.Reality
	if reality == nil || !reality.Enabled {
		t.Fatalf("expected Reality to be enabled")
	}

	if reality.PrivateKey != "u123456789012345678901234567890123456789012" {
		t.Errorf("unexpected private key: %s", reality.PrivateKey)
	}

	if reality.Handshake.ServerOptions.Server != "gateway.icloud.com" {
		t.Errorf("expected Handshake Server gateway.icloud.com, got %s", reality.Handshake.ServerOptions.Server)
	}

	if reality.Handshake.ServerOptions.ServerPort != 443 {
		t.Errorf("expected Handshake ServerPort 443, got %d", reality.Handshake.ServerOptions.ServerPort)
	}

	if len(reality.ShortID) != 1 || reality.ShortID[0] != "0123456789abcdef" {
		t.Errorf("expected short_id [0123456789abcdef], got %v", reality.ShortID)
	}
}

func TestGetInboundOptions_VLESS_Reality_DestWithPort(t *testing.T) {
	s := &Sing{}
	opt := &conf.Options{
		ListenIP: "0.0.0.0",
		SingOptions: &conf.SingOptions{
			TCPFastOpen: false,
		},
	}
	node := &panel.NodeInfo{
		Id:       1,
		Type:     "vless",
		Security: panel.Reality,
		Common: &panel.CommonNode{
			ServerPort: 443,
		},
		VAllss: &panel.VAllssNode{
			CommonNode: panel.CommonNode{
				ServerPort: 443,
			},
			Tls: panel.Reality,
			TlsSettings: panel.TlsSettings{
				ServerName: "itunes.apple.com",
				Dest:       "itunes.apple.com:8443",
				PrivateKey: "u123456789012345678901234567890123456789012",
			},
		},
	}

	in, err := s.getInboundOptions("vless-dest-test", node, opt)
	if err != nil {
		t.Fatalf("getInboundOptions error: %s", err)
	}

	vlessOpt := in.Options.(*option.VLESSInboundOptions)
	if vlessOpt.TLS.ServerName != "itunes.apple.com" {
		t.Errorf("expected TLS.ServerName itunes.apple.com, got %s", vlessOpt.TLS.ServerName)
	}

	reality := vlessOpt.TLS.Reality
	if reality.Handshake.ServerOptions.Server != "itunes.apple.com" {
		t.Errorf("expected Server itunes.apple.com, got %s", reality.Handshake.ServerOptions.Server)
	}
	if reality.Handshake.ServerOptions.ServerPort != 8443 {
		t.Errorf("expected ServerPort 8443, got %d", reality.Handshake.ServerOptions.ServerPort)
	}
}

func TestGetInboundOptions_Trojan_Reality(t *testing.T) {
	s := &Sing{}
	opt := &conf.Options{
		ListenIP: "0.0.0.0",
		SingOptions: &conf.SingOptions{
			TCPFastOpen: false,
		},
	}
	node := &panel.NodeInfo{
		Id:       2,
		Type:     "trojan",
		Security: panel.Reality,
		Common: &panel.CommonNode{
			ServerPort: 443,
		},
		Trojan: &panel.TrojanNode{
			CommonNode: panel.CommonNode{
				ServerPort: 443,
			},
			Tls: panel.Reality,
			TlsSettings: panel.TlsSettings{
				ServerName: "gateway.icloud.com",
				PrivateKey: "u123456789012345678901234567890123456789012",
			},
		},
	}

	in, err := s.getInboundOptions("trojan-test", node, opt)
	if err != nil {
		t.Fatalf("getInboundOptions error: %s", err)
	}

	if in.Type != "trojan" {
		t.Fatalf("expected inbound type trojan, got %s", in.Type)
	}

	trojanOpt, ok := in.Options.(*option.TrojanInboundOptions)
	if !ok {
		t.Fatalf("expected *option.TrojanInboundOptions, got %T", in.Options)
	}

	if trojanOpt.TLS == nil || !trojanOpt.TLS.Enabled {
		t.Fatalf("expected TLS to be enabled")
	}

	if trojanOpt.TLS.ServerName != "gateway.icloud.com" {
		t.Errorf("expected TLS.ServerName gateway.icloud.com, got %s", trojanOpt.TLS.ServerName)
	}

	reality := trojanOpt.TLS.Reality
	if reality == nil || !reality.Enabled {
		t.Fatalf("expected Reality to be enabled")
	}

	if reality.Handshake.ServerOptions.Server != "gateway.icloud.com" {
		t.Errorf("expected Server gateway.icloud.com, got %s", reality.Handshake.ServerOptions.Server)
	}
	if reality.Handshake.ServerOptions.ServerPort != 443 {
		t.Errorf("expected ServerPort 443, got %d", reality.Handshake.ServerOptions.ServerPort)
	}
}

func TestGetInboundOptions_Hysteria2_DefaultPerformance(t *testing.T) {
	s := &Sing{}
	opt := &conf.Options{
		ListenIP:   "0.0.0.0",
		CertConfig: conf.NewCertConfig(),
	}
	node := &panel.NodeInfo{
		Id:       3,
		Type:     "hysteria2",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ServerPort: 443,
		},
		Hysteria2: &panel.Hysteria2Node{
			CommonNode: panel.CommonNode{
				ServerPort: 443,
			},
			UpMbps:                  100,
			DownMbps:                200,
			Ignore_Client_Bandwidth: true,
			ObfsType:                "salamander",
			ObfsPassword:            "secret123",
		},
	}

	in, err := s.getInboundOptions("hy2-default-test", node, opt)
	if err != nil {
		t.Fatalf("getInboundOptions error: %s", err)
	}

	if in.Type != "hysteria2" {
		t.Fatalf("expected inbound type hysteria2, got %s", in.Type)
	}

	hy2Opt, ok := in.Options.(*option.Hysteria2InboundOptions)
	if !ok {
		t.Fatalf("expected *option.Hysteria2InboundOptions, got %T", in.Options)
	}

	// Verify socket reuse is active for high-performance UDP listening
	if !hy2Opt.ListenOptions.ReuseAddr {
		t.Errorf("expected ReuseAddr to be true, got false")
	}

	if hy2Opt.BrutalDebug {
		t.Errorf("expected default BrutalDebug to be false, got true")
	}

	if hy2Opt.UpMbps != 100 || hy2Opt.DownMbps != 200 {
		t.Errorf("expected 100/200 mbps, got %d/%d", hy2Opt.UpMbps, hy2Opt.DownMbps)
	}

	if hy2Opt.Obfs == nil || hy2Opt.Obfs.Password != "secret123" {
		t.Fatalf("expected obfs password 'secret123'")
	}
}

func TestGetInboundOptions_Hysteria2_CustomConfig(t *testing.T) {
	s := &Sing{}
	udpFrag := true
	opt := &conf.Options{
		ListenIP:   "0.0.0.0",
		CertConfig: conf.NewCertConfig(),
		Hysteria2Config: &conf.Hysteria2Config{
			UDPFragment: &udpFrag,
			UDPTimeout:  "45s",
			BrutalDebug: true,
		},
	}
	node := &panel.NodeInfo{
		Id:       4,
		Type:     "hysteria2",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ServerPort: 443,
		},
		Hysteria2: &panel.Hysteria2Node{
			CommonNode: panel.CommonNode{
				ServerPort: 443,
			},
			UpMbps:   50,
			DownMbps: 100,
		},
	}

	in, err := s.getInboundOptions("hy2-custom-test", node, opt)
	if err != nil {
		t.Fatalf("getInboundOptions error: %s", err)
	}

	hy2Opt := in.Options.(*option.Hysteria2InboundOptions)

	if !hy2Opt.ListenOptions.ReuseAddr {
		t.Errorf("expected ReuseAddr to be true")
	}

	if !hy2Opt.BrutalDebug {
		t.Errorf("expected BrutalDebug to be true")
	}

	if hy2Opt.ListenOptions.UDPFragment == nil || !*hy2Opt.ListenOptions.UDPFragment {
		t.Errorf("expected UDPFragment to be true")
	}
}

func TestGetInboundOptions_Hysteria2_MasqueradeString(t *testing.T) {
	s := &Sing{}
	opt := &conf.Options{
		ListenIP:   "0.0.0.0",
		CertConfig: conf.NewCertConfig(),
	}
	node := &panel.NodeInfo{
		Id:       5,
		Type:     "hysteria2",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ServerPort: 443,
		},
		Hysteria2: &panel.Hysteria2Node{
			CommonNode: panel.CommonNode{
				ServerPort: 443,
			},
			Masquerade: "Hello, World! Camouflage Site",
		},
	}

	in, err := s.getInboundOptions("hy2-masq-test", node, opt)
	if err != nil {
		t.Fatalf("getInboundOptions error: %s", err)
	}

	hy2Opt := in.Options.(*option.Hysteria2InboundOptions)
	if hy2Opt.Masquerade == nil || hy2Opt.Masquerade.Type != "string" {
		t.Fatalf("expected string masquerade, got %v", hy2Opt.Masquerade)
	}

	if hy2Opt.Masquerade.StringOptions.Content != "Hello, World! Camouflage Site" {
		t.Errorf("unexpected string content: %s", hy2Opt.Masquerade.StringOptions.Content)
	}
}

func TestGetInboundOptions_Hysteria2_PanelOverride(t *testing.T) {
	s := &Sing{}
	udpFrag := false
	opt := &conf.Options{
		ListenIP:   "0.0.0.0",
		CertConfig: conf.NewCertConfig(),
		Hysteria2Config: &conf.Hysteria2Config{
			UDPFragment: &udpFrag,
			BrutalDebug: false,
		},
	}
	node := &panel.NodeInfo{
		Id:       6,
		Type:     "hysteria2",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ServerPort: 443,
		},
		Hysteria2: &panel.Hysteria2Node{
			CommonNode: panel.CommonNode{
				ServerPort: 443,
			},
			UDPFragment: true,
			BrutalDebug: true,
		},
	}

	in, err := s.getInboundOptions("hy2-override-test", node, opt)
	if err != nil {
		t.Fatalf("getInboundOptions error: %s", err)
	}

	hy2Opt := in.Options.(*option.Hysteria2InboundOptions)

	// Panel-level setting must override local config
	if !hy2Opt.BrutalDebug {
		t.Errorf("expected BrutalDebug from panel to be true")
	}
	if hy2Opt.ListenOptions.UDPFragment == nil || !*hy2Opt.ListenOptions.UDPFragment {
		t.Errorf("expected UDPFragment from panel to be true")
	}
}
