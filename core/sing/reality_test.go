package sing

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// realityKeys returns an X25519 private/public key pair in the base64url form
// Xboard and sing-box use.
func realityKeys(t *testing.T) (priv, pub string) {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(k.Bytes()), enc.EncodeToString(k.PublicKey().Bytes())
}

// socks5Dial connects through a local SOCKS5 proxy to target.
func socks5Dial(proxy, target string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", proxy, 5*time.Second)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		return nil, err
	}
	buf := make([]byte, 2)
	if _, err = io.ReadFull(c, buf); err != nil || buf[0] != 5 || buf[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("socks5 greeting: %v %v", buf, err)
	}
	host, portStr, _ := net.SplitHostPort(target)
	var port int
	_, _ = fmt.Sscan(portStr, &port)
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err = c.Write(req); err != nil {
		return nil, err
	}
	resp := make([]byte, 10)
	if _, err = io.ReadFull(c, resp); err != nil || resp[1] != 0 {
		c.Close()
		return nil, fmt.Errorf("socks5 connect: %v %v", resp, err)
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}

func startBox(t *testing.T, opts option.Options) *box.Box {
	t.Helper()
	ctx := box.Context(context.Background(), include.InboundRegistry(), include.OutboundRegistry(),
		include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry())
	b, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		if strings.Contains(err.Error(), "uTLS") || strings.Contains(err.Error(), "utls") {
			t.Skipf("uTLS unavailable (build with -tags with_utls): %v", err)
		}
		t.Fatalf("new box: %v", err)
	}
	if err = b.Start(); err != nil {
		t.Fatalf("start box: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func unmarshalOptions(t *testing.T, raw string) option.Options {
	t.Helper()
	ctx := box.Context(context.Background(), include.InboundRegistry(), include.OutboundRegistry(),
		include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry())
	o, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(raw))
	if err != nil {
		t.Fatalf("options: %v\n%s", err, raw)
	}
	return o
}

// TestReality_VlessLoopback builds the vless+reality inbound from a panel-style
// config (the way AddNode does) and connects with a sing-box vless+reality
// client using the matching public key and short id. It proves that a correctly
// configured real client works; "REALITY: processed invalid connection" lines
// in a production log therefore come from clients/probes that fail REALITY auth.
func TestReality_VlessLoopback(t *testing.T) {
	cases := []struct {
		name     string
		panelSID string // short_id as Xboard stores it
		clientID string
		flow     string
	}{
		{"vision with short id", "0123456789abcdef", "0123456789abcdef", "xtls-rprx-vision"},
		{"short id list", "a1b2, c3d4e5f6", "c3d4e5f6", "xtls-rprx-vision"},
		{"short id shorter than 8 bytes", "ab12", "ab12", "xtls-rprx-vision"},
		{"empty short id", "", "", "xtls-rprx-vision"},
		{"no flow", "0123456789abcdef", "0123456789abcdef", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Camouflage destination: a local TLS 1.3 server
			dest := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("camouflage"))
			}))
			dest.Config.ErrorLog = log.New(io.Discard, "", 0)
			dest.EnableHTTP2 = true
			dest.StartTLS()
			defer dest.Close()
			destAddr := dest.Listener.Addr().(*net.TCPAddr)

			// Target the client reaches through the proxy
			echo, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer echo.Close()
			go func() {
				for {
					c, err := echo.Accept()
					if err != nil {
						return
					}
					go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
				}
			}()

			priv, pub := realityKeys(t)
			serverPort := freePort(t)
			socksPort := freePort(t)

			// Panel-style config → inbound, exactly as AddNode builds it
			body := fmt.Sprintf(`{"protocol":"vless","server_port":%d,"network":"tcp","tls":2,"flow":%q,
				"tls_settings":{"server_name":"reality.test","server_port":"%d","dest":"127.0.0.1","private_key":%q,"short_id":%q}}`,
				serverPort, tc.flow, destAddr.Port, priv, tc.panelSID)
			client := &panel.Client{NodeType: "vless", NodeId: 1}
			info, err := client.ParseNodeConfig([]byte(body))
			if err != nil || info == nil {
				t.Fatalf("parse panel config: %v", err)
			}
			if info.Security != panel.Reality {
				t.Fatalf("security = %d, want reality", info.Security)
			}
			s := &Sing{}
			in, err := s.getInboundOptions("vless-reality-loop", info, &conf.Options{ListenIP: "127.0.0.1", SingOptions: &conf.SingOptions{}})
			if err != nil {
				t.Fatalf("getInboundOptions: %v", err)
			}
			vopt := in.Options.(*option.VLESSInboundOptions)
			vopt.Users = []option.VLESSUser{{Name: "u1", UUID: "11111111-1111-1111-1111-111111111111", Flow: tc.flow}}

			server := option.Options{
				Log:       &option.LogOptions{Level: "error"},
				Inbounds:  []option.Inbound{in},
				Outbounds: unmarshalOptions(t, `{"outbounds":[{"type":"direct","tag":"direct"}]}`).Outbounds,
			}
			startBox(t, server)

			clientOpts := unmarshalOptions(t, fmt.Sprintf(`{
				"log":{"level":"error"},
				"inbounds":[{"type":"socks","tag":"in","listen":"127.0.0.1","listen_port":%d}],
				"outbounds":[{"type":"vless","tag":"proxy","server":"127.0.0.1","server_port":%d,
					"uuid":"11111111-1111-1111-1111-111111111111","flow":%q,
					"tls":{"enabled":true,"server_name":"reality.test",
						"utls":{"enabled":true,"fingerprint":"chrome"},
						"reality":{"enabled":true,"public_key":%q,"short_id":%q}}}],
				"route":{"final":"proxy"}}`, socksPort, serverPort, tc.flow, pub, tc.clientID))
			ctx := box.Context(context.Background(), include.InboundRegistry(), include.OutboundRegistry(),
				include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry())
			cb, err := box.New(box.Options{Context: ctx, Options: clientOpts})
			if err != nil {
				if strings.Contains(err.Error(), "utls") {
					t.Skipf("uTLS unavailable (build with -tags with_utls): %v", err)
				}
				t.Fatalf("client box: %v", err)
			}
			if err = cb.Start(); err != nil {
				t.Fatalf("client start: %v", err)
			}
			defer cb.Close()

			conn, err := socks5Dial(fmt.Sprintf("127.0.0.1:%d", socksPort), echo.Addr().String())
			if err != nil {
				t.Fatalf("a correctly configured REALITY client must connect: %v", err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			if _, err = conn.Write([]byte("hello reality")); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len("hello reality"))
			if _, err = io.ReadFull(conn, got); err != nil || string(got) != "hello reality" {
				t.Fatalf("echo through REALITY: %q %v", got, err)
			}
		})
	}
}

// A client with the wrong public key or short id is rejected; this is the
// "REALITY: processed invalid connection" case.
func TestReality_WrongCredentialsRejected(t *testing.T) {
	dest := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dest.Config.ErrorLog = log.New(io.Discard, "", 0)
	dest.EnableHTTP2 = true
	dest.StartTLS()
	defer dest.Close()
	priv, _ := realityKeys(t)
	_, wrongPub := realityKeys(t)
	serverPort := freePort(t)
	socksPort := freePort(t)

	body := fmt.Sprintf(`{"protocol":"vless","server_port":%d,"network":"tcp","tls":2,
		"tls_settings":{"server_name":"reality.test","server_port":"%d","dest":"127.0.0.1","private_key":%q,"short_id":"0123456789abcdef"}}`,
		serverPort, dest.Listener.Addr().(*net.TCPAddr).Port, priv)
	info, err := (&panel.Client{NodeType: "vless", NodeId: 1}).ParseNodeConfig([]byte(body))
	if err != nil || info == nil {
		t.Fatal(err)
	}
	in, err := (&Sing{}).getInboundOptions("vless-reality-bad", info, &conf.Options{ListenIP: "127.0.0.1", SingOptions: &conf.SingOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	in.Options.(*option.VLESSInboundOptions).Users = []option.VLESSUser{{Name: "u", UUID: "11111111-1111-1111-1111-111111111111"}}
	startBox(t, option.Options{
		Log:       &option.LogOptions{Level: "fatal"},
		Inbounds:  []option.Inbound{in},
		Outbounds: unmarshalOptions(t, `{"outbounds":[{"type":"direct","tag":"direct"}]}`).Outbounds,
	})

	clientOpts := unmarshalOptions(t, fmt.Sprintf(`{
		"log":{"level":"fatal"},
		"inbounds":[{"type":"socks","tag":"in","listen":"127.0.0.1","listen_port":%d}],
		"outbounds":[{"type":"vless","tag":"proxy","server":"127.0.0.1","server_port":%d,
			"uuid":"11111111-1111-1111-1111-111111111111",
			"tls":{"enabled":true,"server_name":"reality.test","utls":{"enabled":true,"fingerprint":"chrome"},
				"reality":{"enabled":true,"public_key":%q,"short_id":"0123456789abcdef"}}}],
		"route":{"final":"proxy"}}`, socksPort, serverPort, wrongPub))
	ctx := box.Context(context.Background(), include.InboundRegistry(), include.OutboundRegistry(),
		include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry())
	cb, err := box.New(box.Options{Context: ctx, Options: clientOpts})
	if err != nil {
		t.Skipf("client box unavailable: %v", err)
	}
	if err = cb.Start(); err != nil {
		t.Fatal(err)
	}
	defer cb.Close()
	conn, err := socks5Dial(fmt.Sprintf("127.0.0.1:%d", socksPort), "127.0.0.1:9")
	if err == nil {
		conn.Close()
		t.Fatal("a client with the wrong public key must not get through")
	}
}
