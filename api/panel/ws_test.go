package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestParseWSMessage_Events(t *testing.T) {
	// sync.config carries the same shape as the HTTP config
	event, ev, err := ParseWSMessage([]byte(`{"event":"sync.config","data":{"config":` + sampleVlessConfig + `}}`))
	if err != nil || event != WSEventSyncConfig || ev == nil {
		t.Fatalf("sync.config: %s %v %v", event, ev, err)
	}
	c := newTestClient(t, "vless")
	node, err := c.ParseNodeConfig(ev.Config)
	if err != nil || node == nil || len(node.Common.CustomOutbounds) != 2 {
		t.Fatalf("pushed config must parse like the HTTP one: %v %v", node, err)
	}

	_, ev, err = ParseWSMessage([]byte(`{"event":"sync.users","data":{"users":[{"id":1,"uuid":"u1","speed_limit":null,"device_limit":2}]}}`))
	if err != nil || len(ev.Users) != 1 || ev.Users[0].Uuid != "u1" || ev.Users[0].DeviceLimit != 2 {
		t.Fatalf("sync.users: %+v %v", ev, err)
	}

	_, ev, err = ParseWSMessage([]byte(`{"event":"sync.user.delta","data":{"action":"add","users":[{"id":2,"uuid":"u2","speed_limit":10,"device_limit":0}]}}`))
	if err != nil || ev.DeltaAction != "add" || len(ev.DeltaUsers) != 1 || ev.DeltaUsers[0].SpeedLimit != 10 {
		t.Fatalf("delta add: %+v %v", ev, err)
	}
	_, ev, err = ParseWSMessage([]byte(`{"event":"sync.user.delta","data":{"action":"remove","users":[{"id":2}]}}`))
	if err != nil || ev.DeltaAction != "remove" || ev.DeltaUsers[0].Id != 2 {
		t.Fatalf("delta remove: %+v %v", ev, err)
	}
	if _, _, err = ParseWSMessage([]byte(`{"event":"sync.user.delta","data":{"action":"wipe","users":[{"id":2}]}}`)); err == nil {
		t.Error("unknown delta action must fail")
	}

	_, ev, err = ParseWSMessage([]byte(`{"event":"sync.devices","data":{"users":{"5":["1.1.1.1","2.2.2.2"]}}}`))
	if err != nil || len(ev.Devices[5]) != 2 {
		t.Fatalf("sync.devices: %+v %v", ev, err)
	}
	// PHP encodes an empty map as []
	_, ev, err = ParseWSMessage([]byte(`{"event":"sync.devices","data":{"users":[]}}`))
	if err != nil || ev.Devices == nil || len(ev.Devices) != 0 {
		t.Fatalf("empty sync.devices: %+v %v", ev, err)
	}

	event, ev, err = ParseWSMessage([]byte(`{"event":"ping"}`))
	if err != nil || event != "ping" || ev != nil {
		t.Fatalf("ping: %s %v %v", event, ev, err)
	}
}

func TestValidateWSURL(t *testing.T) {
	for _, ok := range []string{"ws://panel.example.com/ws", "wss://panel.example.com:8076"} {
		if _, err := ValidateWSURL(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "https://panel.example.com/ws", "panel.example.com/ws", "ws://", "ws://%zz"} {
		if _, err := ValidateWSURL(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestParseHandshake(t *testing.T) {
	hs, err := ParseHandshake([]byte(`{"websocket":{"enabled":true,"ws_url":"wss://p.example.com/ws"}}`))
	if err != nil || !hs.WebSocket.Enabled || hs.WebSocket.WSURL != "wss://p.example.com/ws" {
		t.Fatalf("handshake: %+v %v", hs, err)
	}
	hs, err = ParseHandshake([]byte(`{"websocket":{"enabled":false}}`))
	if err != nil || hs.WebSocket.Enabled {
		t.Fatalf("disabled handshake: %+v %v", hs, err)
	}
}

func TestWSClient_PingPongEventsAndReconnect(t *testing.T) {
	var conns atomic.Int32
	pong := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "secret" || r.URL.Query().Get("node_id") != "7" {
			http.Error(w, "bad auth", http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		n := conns.Add(1)
		ctx := r.Context()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"event":"auth.success","data":{"node_id":7}}`))
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"event":"ping"}`))
		_, data, err := conn.Read(ctx)
		if err == nil && strings.Contains(string(data), `"pong"`) {
			pong <- struct{}{}
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"event":"sync.users","data":{"users":[{"id":1,"uuid":"u1"}]}}`))
		if n == 1 {
			// drop the first connection to exercise reconnect
			conn.Close(websocket.StatusGoingAway, "restart")
			return
		}
		<-ctx.Done()
	}))
	defer srv.Close()

	events := make(chan *WSEvent, 4)
	var statuses atomic.Int32
	ws, err := NewWSClient("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", "secret", 7,
		func(ev *WSEvent) { events <- ev },
		func(connected bool) {
			if connected {
				statuses.Add(1)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ws.URL(), "secret") {
		t.Errorf("log URL must mask the token: %s", ws.URL())
	}
	ws.BackoffInitial = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { ws.Run(ctx); close(done) }()

	for i := 0; i < 2; i++ {
		select {
		case <-pong:
		case <-time.After(5 * time.Second):
			t.Fatalf("no pong on connection %d", i+1)
		}
		select {
		case ev := <-events:
			if ev.Type != WSEventSyncUsers || len(ev.Users) != 1 {
				t.Fatalf("unexpected event %+v", ev)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no event on connection %d", i+1)
		}
	}
	if conns.Load() < 2 || statuses.Load() < 2 {
		t.Errorf("expected a reconnect: conns=%d connected=%d", conns.Load(), statuses.Load())
	}
	deadline := time.Now().Add(2 * time.Second)
	for !ws.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !ws.IsConnected() {
		t.Error("client must report connected")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return after cancel")
	}
	if ws.IsConnected() {
		t.Error("client must report disconnected after cancel")
	}
}

func TestWSClient_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"event":"error","data":{"message":"invalid token"}}`))
		conn.Close(websocket.StatusPolicyViolation, "auth")
	}))
	defer srv.Close()
	ws, _ := NewWSClient("ws"+strings.TrimPrefix(srv.URL, "http"), "bad", 1, nil, nil)
	err := ws.connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("expected auth error, got %v", err)
	}
	if ws.IsConnected() {
		t.Error("must not be connected after auth error")
	}
}
