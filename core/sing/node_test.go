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
