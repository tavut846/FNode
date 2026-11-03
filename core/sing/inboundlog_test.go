package sing

import (
	"context"
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
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

func realityNode(t *testing.T, id, port int, priv string, destPort int) *panel.NodeInfo {
	t.Helper()
	body := fmt.Sprintf(`{"protocol":"vless","server_port":%d,"network":"tcp","tls":2,"flow":"xtls-rprx-vision",
		"multiplex":{"enabled":true},
		"tls_settings":{"server_name":"reality.test","server_port":"%d","dest":"127.0.0.1","private_key":%q,"short_id":"0123456789abcdef"}}`,
		port, destPort, priv)
	info, err := (&panel.Client{NodeType: "vless", NodeId: id}).ParseNodeConfig([]byte(body))
	if err != nil || info == nil {
		t.Fatal(err)
	}
	return info
}

func findEntry(h *test.Hook, level logrus.Level, contains string) *logrus.Entry {
	for _, e := range h.AllEntries() {
		if e.Level == level && strings.Contains(e.Message, contains) {
			return e
		}
	}
	return nil
}

// The start-up line says what the inbound is, without any secret.
func TestInboundFields_Reality(t *testing.T) {
	priv, _ := realityKeys(t)
	info := realityNode(t, 7, 443, priv, 8443)
	c := &conf.Options{ListenIP: "0.0.0.0", SingOptions: &conf.SingOptions{}}
	in, err := (&Sing{}).getInboundOptions("[https://p]-vless:7", info, c)
	if err != nil {
		t.Fatal(err)
	}
	f := inboundFields("[https://p]-vless:7", info, c, in)
	want := map[string]any{
		"node_id": 7, "type": "vless", "listen": "0.0.0.0:443", "reality": true, "server_name": "reality.test",
		"dest": "127.0.0.1:8443", "short_ids": 1, "flow": "xtls-rprx-vision", "multiplex": true,
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("%s = %v, want %v", k, f[k], v)
		}
	}
	for k, v := range f {
		s := fmt.Sprint(v)
		if strings.Contains(s, priv) || strings.Contains(s, "0123456789abcdef") {
			t.Errorf("field %s leaks a secret: %s", k, s)
		}
	}
}

// Inbounds that cannot be built or started say so at ERROR with the node.
func TestAddNode_FailuresAreLoggedAtError(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	b := newTestSing(t, nil)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// a bad private key
	bad := realityNode(t, 3, freePort(t), "not-a-key", 443)
	if err := b.AddNode("[https://p]-vless:3", bad, &conf.Options{ListenIP: "127.0.0.1", SingOptions: &conf.SingOptions{}}); err == nil {
		t.Fatal("a bad private key must fail")
	}
	e := findEntry(hook, logrus.ErrorLevel, "cannot be built")
	if e == nil || e.Data["node_id"] != 3 || e.Data["tag"] != "[https://p]-vless:3" || !strings.Contains(e.Message, "private_key") {
		t.Fatalf("build failure: %+v", hook.AllEntries())
	}

	// a port that is taken
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	common := panel.CommonNode{ServerPort: port}
	taken := &panel.NodeInfo{Id: 4, Type: "vless", Security: panel.None, Common: &common,
		VAllss: &panel.VAllssNode{CommonNode: common, Network: "tcp"}}
	if err := b.AddNode("[https://p]-vless:4", taken, &conf.Options{ListenIP: "127.0.0.1", CertConfig: conf.NewCertConfig()}); err == nil {
		t.Fatal("a taken port must fail")
	}
	e = findEntry(hook, logrus.ErrorLevel, "failed to start")
	if e == nil || e.Data["node_id"] != 4 || e.Data["port"] != port || !strings.Contains(e.Message, "already used") {
		t.Fatalf("listener failure: %+v", hook.AllEntries())
	}
}

// An enabled inbound nobody connects to warns once; one that served a
// connection does not.
func TestIdleInboundWarning(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	b := newTestSing(t, nil)
	b.idleWarnAfter = 150 * time.Millisecond
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	add := func(id int, tag string) int {
		port := freePort(t)
		common := panel.CommonNode{ServerPort: port}
		info := &panel.NodeInfo{Id: id, Type: "vless", Security: panel.None, Common: &common,
			VAllss: &panel.VAllssNode{CommonNode: common, Network: "tcp"}}
		if err := b.AddNode(tag, info, &conf.Options{ListenIP: "127.0.0.1", CertConfig: conf.NewCertConfig()}); err != nil {
			t.Fatal(err)
		}
		return port
	}
	idlePort := add(1, "[https://p]-vless:1")
	busyPort := add(2, "[https://p]-vless:2")
	b.hookServer.markAccepted("[https://p]-vless:2")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && findEntry(hook, logrus.WarnLevel, "No client has connected") == nil {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	var idle, busy int
	for _, e := range hook.AllEntries() {
		if strings.Contains(e.Message, "No client has connected") {
			if strings.Contains(e.Message, fmt.Sprintf("vless:%d ", idlePort)) {
				idle++
				if !strings.Contains(e.Message, "firewall") || !strings.Contains(e.Message, "0 users") {
					t.Errorf("message: %s", e.Message)
				}
			}
			if strings.Contains(e.Message, fmt.Sprintf("vless:%d ", busyPort)) {
				busy++
			}
		}
	}
	if idle != 1 || busy != 0 {
		t.Fatalf("idle warnings %d (want 1), busy warnings %d (want 0): %+v", idle, busy, hook.AllEntries())
	}

	// a removed node does not warn
	hook.Reset()
	port3 := add(3, "[https://p]-vless:3")
	_ = port3
	if err := b.DelNode("[https://p]-vless:3"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if findEntry(hook, logrus.WarnLevel, "vless:"+fmt.Sprint(port3)) != nil {
		t.Error("a deleted inbound must not warn")
	}
}

func TestClassifyReject(t *testing.T) {
	cases := []struct {
		raw, reason, ip, detail string
		ok                      bool
	}{
		{"-0700 2026-10-19 10:00:00 ERROR [1 5ms] inbound/vless[n]: process connection from 52.36.5.236:40000: TLS handshake: REALITY: processed invalid connection",
			reasonRealityRejected, "52.36.5.236", "", true},
		{"-0700 2026-10-19 10:00:00 ERROR [1 5ms] inbound/vless[n]: process connection from 1.2.3.4:5: flow mismatch: expected xtls-rprx-vision, but got none",
			reasonFlowMismatch, "1.2.3.4", "expected xtls-rprx-vision, but got none", true},
		{"-0700 2026-10-19 10:00:00 ERROR [1 5ms] inbound/vless[n]: process connection from 1.2.3.4:5: TLS handshake: REALITY: failed to dial dest: dial tcp 9.9.9.9:443: i/o timeout",
			reasonDestUnreachable, "1.2.3.4", "dial tcp 9.9.9.9:443: i/o timeout", true},
		{"-0700 2026-10-19 10:00:00 INFO [1 5ms] inbound/vless[n]: [user] inbound connection to example.com:443", "", "", "", false},
		{"-0700 2026-10-19 10:00:00 ERROR [1 5ms] outbound/direct[x]: flow mismatch: a", "", "", "", false},
	}
	for _, c := range cases {
		l, ok := parseLogLine(c.raw)
		if !ok {
			t.Fatalf("parse %q", c.raw)
		}
		ri, got := classifyReject(l)
		if got != c.ok || ri.reason != c.reason || ri.ip != c.ip || ri.detail != c.detail {
			t.Errorf("%q => %v %+v", c.raw, got, ri)
		}
	}
}

// One WARN per inbound and reason per minute, with the count and addresses of
// what the window swallowed.
func TestRejectLimiter(t *testing.T) {
	type emitted struct {
		inbound, reason string
		count           int
		ips             map[string]int
	}
	var got []emitted
	now := time.Unix(1_760_000_000, 0)
	r := newRejectLimiter()
	r.now = func() time.Time { return now }
	r.emit = func(inbound, reason string, count int, ips map[string]int, detail string) {
		got = append(got, emitted{inbound, reason, count, ips})
	}
	obs := func(in, reason, ip string) { r.Observe(in, rejectInfo{reason: reason, ip: ip}) }

	obs("inbound/vless[a]", reasonRealityRejected, "1.1.1.1")
	if len(got) != 1 || got[0].count != 1 {
		t.Fatalf("the first reject warns at once: %+v", got)
	}
	for i := 0; i < 4; i++ {
		now = now.Add(5 * time.Second)
		obs("inbound/vless[a]", reasonRealityRejected, "2.2.2.2")
	}
	obs("inbound/vless[b]", reasonRealityRejected, "3.3.3.3") // another inbound has its own window
	obs("inbound/vless[a]", reasonFlowMismatch, "4.4.4.4")    // and so has another reason
	if len(got) != 3 {
		t.Fatalf("within the window nothing more is logged for a/reality: %+v", got)
	}
	r.Flush(false)
	if len(got) != 3 {
		t.Fatalf("the window has not ended: %+v", got)
	}
	now = now.Add(45 * time.Second)
	r.Flush(false)
	if len(got) != 4 || got[3].count != 4 || got[3].ips["2.2.2.2"] != 4 || got[3].inbound != "inbound/vless[a]" {
		t.Fatalf("the summary counts what was suppressed: %+v", got)
	}
	r.Flush(false)
	if len(got) != 4 {
		t.Fatalf("nothing pending, nothing logged: %+v", got)
	}
	// a late reject after the window logs at once again
	now = now.Add(2 * time.Minute)
	obs("inbound/vless[a]", reasonRealityRejected, "5.5.5.5")
	if len(got) != 5 || got[4].count != 1 {
		t.Fatalf("next window: %+v", got)
	}
	// shutdown logs what is pending
	obs("inbound/vless[a]", reasonRealityRejected, "5.5.5.5")
	r.Flush(true)
	if len(got) != 6 || got[5].count != 1 {
		t.Fatalf("force flush: %+v", got)
	}
}

// The real thing: a REALITY client with the wrong public key against a node
// started by FNode produces a WARN naming the inbound and the client address,
// at the default log level and without debug.
func TestReality_WrongKeyIsWarned(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	dest := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dest.Config.ErrorLog = log.New(io.Discard, "", 0)
	dest.EnableHTTP2 = true
	dest.StartTLS()
	defer dest.Close()
	priv, _ := realityKeys(t)
	_, wrongPub := realityKeys(t)
	serverPort, socksPort := freePort(t), freePort(t)

	b := newTestSing(t, nil)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	tag := "[https://p]-vless:9"
	info := realityNode(t, 9, serverPort, priv, dest.Listener.Addr().(*net.TCPAddr).Port)
	if err := b.AddNode(tag, info, &conf.Options{ListenIP: "127.0.0.1", SingOptions: &conf.SingOptions{}}); err != nil {
		t.Fatal(err)
	}
	if e := findEntry(hook, logrus.InfoLevel, "Inbound created"); e == nil || e.Data["reality"] != true || e.Data["dest"] == nil {
		t.Fatalf("start-up line: %+v", hook.AllEntries())
	}

	clientOpts := unmarshalOptions(t, fmt.Sprintf(`{
		"log":{"level":"fatal"},
		"inbounds":[{"type":"socks","tag":"in","listen":"127.0.0.1","listen_port":%d}],
		"outbounds":[{"type":"vless","tag":"proxy","server":"127.0.0.1","server_port":%d,
			"uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision",
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
	if conn, err := socks5Dial(fmt.Sprintf("127.0.0.1:%d", socksPort), "127.0.0.1:9"); err == nil {
		conn.Close()
		t.Fatal("a client with the wrong public key must not get through")
	}

	deadline := time.Now().Add(5 * time.Second)
	var e *logrus.Entry
	for time.Now().Before(deadline) && e == nil {
		e = findEntry(hook, logrus.WarnLevel, "Reality rejected")
		time.Sleep(20 * time.Millisecond)
	}
	if e == nil {
		t.Fatalf("no WARN for the rejected client: %+v", hook.AllEntries())
	}
	if e.Data["tag"] != tag || !strings.Contains(e.Message, "127.0.0.1 x1") || !strings.Contains(e.Message, "inbound/vless["+tag+"]") {
		t.Errorf("the WARN names the node and the client: %s %v", e.Message, e.Data)
	}
}
