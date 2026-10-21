package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	"github.com/tavut846/FNode/limiter"
)

// Xboard marks a node as running only when it calls GET /user (a 304
// counts). With the panel WebSocket connected the node must still call it
// every pull interval, apply a changed list, and leave /alivelist to the WS.
func TestController_UserHeartbeatWithWebSocket(t *testing.T) {
	var userCalls, aliveCalls, configCalls atomic.Int32
	var userBody atomic.Value
	userBody.Store(`{"users":[{"id":1,"uuid":"u1"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"event":"auth.success","data":{"node_id":7}}`))
			for {
				if _, _, err := conn.Read(r.Context()); err != nil {
					return
				}
			}
		case strings.HasSuffix(r.URL.Path, "/config"):
			configCalls.Add(1)
			w.WriteHeader(http.StatusNotModified)
		case strings.HasSuffix(r.URL.Path, "/user"):
			userCalls.Add(1)
			body := userBody.Load().(string)
			etag := `"` + body + `"`
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/alivelist"):
			aliveCalls.Add(1)
			_, _ = w.Write([]byte(`{"alive":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := panel.New(&conf.ApiConfig{APIHost: srv.URL, NodeID: 7, NodeType: "vless", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := panel.NewWSClient("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", "k", 7, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ws.Run(ctx)
	for deadline := time.Now().Add(5 * time.Second); !ws.IsConnected(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("WebSocket did not connect")
		}
	}

	tag := "heartbeat-test"
	users := []panel.UserInfo{{Id: 1, Uuid: "u1"}}
	lim := limiter.AddLimiter(tag, &conf.LimitConfig{}, users, map[int]int{})
	defer limiter.DeleteLimiter(tag)
	core := &fakeCore{}
	c := NewController(core, client, &conf.Options{Name: tag})
	c.tag, c.limiter, c.userList, c.ws = tag, lim, users, ws
	c.info = &panel.NodeInfo{Id: 7, Type: "vless", InboundHash: "x"}

	for i := 0; i < 3; i++ {
		if err := c.nodeInfoMonitor(); err != nil {
			t.Fatal(err)
		}
	}
	if got := userCalls.Load(); got != 3 {
		t.Errorf("GET /user called %d times in 3 pull intervals with the WebSocket up, want 3", got)
	}
	if got := configCalls.Load(); got != 3 {
		t.Errorf("GET /config called %d times, want 3 (ETag check)", got)
	}
	if got := aliveCalls.Load(); got != 0 {
		t.Errorf("GET /alivelist called %d times with the WebSocket up, want 0", got)
	}
	if len(core.added) != 0 || len(core.deleted) != 0 {
		t.Errorf("unchanged user list changed the core: +%v -%v", core.added, core.deleted)
	}

	// A user the WebSocket push missed is picked up by the next check.
	userBody.Store(`{"users":[{"id":1,"uuid":"u1"},{"id":2,"uuid":"u2"}]}`)
	if err := c.nodeInfoMonitor(); err != nil {
		t.Fatal(err)
	}
	if len(core.added) != 1 || core.added[0].Uuid != "u2" || len(c.userList) != 2 {
		t.Errorf("missed user not applied: added %v list %v", core.added, c.userList)
	}

	// Without the WebSocket the alive list is fetched again.
	cancel()
	for deadline := time.Now().Add(5 * time.Second); ws.IsConnected(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("WebSocket did not disconnect")
		}
	}
	if err := c.nodeInfoMonitor(); err != nil {
		t.Fatal(err)
	}
	if aliveCalls.Load() != 1 {
		t.Errorf("GET /alivelist not called after the WebSocket dropped")
	}
}
