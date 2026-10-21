package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tavut846/FNode/conf"
)

func TestUserAgent_HTTPAndWS(t *testing.T) {
	old := Version
	Version = "1.2.3"
	defer func() { Version = old }()

	seen := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path + " " + r.Header.Get("User-Agent")
		if r.URL.Path == "/ws" {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"event":"error","data":{"message":"bye"}}`))
			conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"websocket":{"enabled":false}}`))
	}))
	defer srv.Close()

	c, err := New(&conf.ApiConfig{APIHost: srv.URL, Key: "k", NodeID: 1, NodeType: "vless"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Handshake(); err != nil {
		t.Fatal(err)
	}
	ws, _ := NewWSClient("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", "k", 1, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = ws.connect(ctx)

	for _, want := range []string{"/api/v2/server/handshake FNode/1.2.3", "/ws FNode/1.2.3"} {
		select {
		case got := <-seen:
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no request for %q", want)
		}
	}
}
