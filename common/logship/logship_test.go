package logship

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

type fakeSender struct {
	mu    sync.Mutex
	calls [][]Entry
	resp  Response
	err   error
}

func (f *fakeSender) SendLogs(_ context.Context, entries []Entry) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]Entry{}, entries...))
	if f.err != nil {
		return Response{}, f.err
	}
	return f.resp, nil
}

func (f *fakeSender) sent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += len(c)
	}
	return n
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestShipper(f *fakeSender) (*Shipper, *clock) {
	s := NewShipper(f)
	c := &clock{t: time.Unix(1_760_000_000, 0)}
	s.now = c.now
	return s, c
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Level{"off": Off, "error": Error, "warn": Warn, "info": Info, "debug": Debug, " INFO ": Info} {
		if got, ok := ParseMode(in); !ok || got != want {
			t.Errorf("ParseMode(%q) = %v %v", in, got, ok)
		}
	}
	if _, ok := ParseMode("verbose"); ok {
		t.Error("unknown modes are rejected")
	}
}

func TestShipper_LevelFiltering(t *testing.T) {
	s, _ := newTestShipper(&fakeSender{})
	// default mode is warn: error and warn ship, info and debug do not
	for _, l := range []Level{Error, Warn, Info, Debug} {
		s.Add(l, Entry{Message: l.String()})
	}
	if q, _ := s.Buffered(); q != 2 {
		t.Fatalf("warn mode must queue 2 entries, got %d", q)
	}
	s.SetMode(0, Debug)
	s.Add(Debug, Entry{Message: "d"})
	if q, _ := s.Buffered(); q != 3 {
		t.Fatalf("debug mode queues debug entries, got %d", q)
	}
	s.SetMode(0, Off)
	if q, _ := s.Buffered(); q != 0 {
		t.Fatalf("mode off clears the buffer, got %d", q)
	}
	s.Add(Error, Entry{Message: "x"})
	if q, _ := s.Buffered(); q != 0 {
		t.Fatalf("mode off ships nothing, got %d", q)
	}
}

func TestShipper_PerNodeOverride(t *testing.T) {
	s, _ := newTestShipper(&fakeSender{})
	s.SetMode(12, Debug) // only node 12
	s.Add(Debug, Entry{NodeID: 12, Message: "node 12 debug"})
	s.Add(Debug, Entry{NodeID: 13, Message: "node 13 debug"})
	s.Add(Warn, Entry{NodeID: 13, Message: "node 13 warn"})
	if q, _ := s.Buffered(); q != 2 {
		t.Fatalf("expected 2 entries, got %d", q)
	}
	if s.MaxLevel() != Debug || s.Mode() != Warn {
		t.Errorf("max %v base %v", s.MaxLevel(), s.Mode())
	}
	s.SetMode(0, Info) // machine-wide clears overrides
	s.Add(Debug, Entry{NodeID: 12, Message: "again"})
	if q, _ := s.Buffered(); q != 2 {
		t.Errorf("override must be cleared by a machine-wide mode, queue %d", q)
	}
}

func TestShipper_RingDropsOldestAndTruncates(t *testing.T) {
	s, _ := newTestShipper(&fakeSender{})
	s.capacity = 5
	for i := 0; i < 8; i++ {
		s.Add(Error, Entry{Message: string(rune('a' + i))})
	}
	q, dropped := s.Buffered()
	if q != 5 || dropped != 3 {
		t.Fatalf("queued %d dropped %d", q, dropped)
	}
	if s.buf[0].Message != "d" || s.buf[4].Message != "h" {
		t.Errorf("the oldest entries are dropped: %v", s.buf)
	}

	s2, _ := newTestShipper(&fakeSender{})
	s2.Add(Error, Entry{Message: strings.Repeat("é", 3000)}) // 6000 bytes
	if got := s2.buf[0].Message; len(got) > MaxMessageSize || len(got) < MaxMessageSize-1 || strings.ContainsRune(got, '�') {
		t.Errorf("message must be cut at %d bytes on a rune boundary, got %d", MaxMessageSize, len(got))
	}
}

func TestShipper_FlushBatchesResponseAndInterval(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "info", Accepted: 500, FlushInterval: 25}}
	s, _ := newTestShipper(f)
	s.SetMode(0, Debug)
	for i := 0; i < 1200; i++ {
		s.Add(Info, Entry{Time: float64(i), NodeID: 12, Message: "m"})
	}
	wait := s.Flush(context.Background())
	if len(f.calls) != 3 || len(f.calls[0]) != MaxBatch || len(f.calls[2]) != 200 {
		t.Fatalf("expected batches of 500/500/200, got %d calls", len(f.calls))
	}
	if q, _ := s.Buffered(); q != 0 {
		t.Errorf("sent entries are removed, %d left", q)
	}
	if s.Mode() != Info {
		t.Errorf("mode comes from the response: %v", s.Mode())
	}
	if wait != 25*time.Second {
		t.Errorf("next flush after flush_interval, got %v", wait)
	}
	if f.calls[0][0].Level != "info" || f.calls[0][0].NodeID != 12 {
		t.Errorf("entry: %+v", f.calls[0][0])
	}
}

func TestShipper_ResponseModeFiltersBufferedEntries(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "error", FlushInterval: 10}}
	s, clk := newTestShipper(f)
	s.SetMode(0, Debug)
	s.Add(Debug, Entry{Message: "debug"})
	s.Add(Error, Entry{Message: "error"})
	s.Flush(context.Background()) // sends both, then learns the panel's mode is error
	if s.Mode() != Error {
		t.Fatalf("mode: %v", s.Mode())
	}
	s.Add(Info, Entry{Message: "info"})
	if q, _ := s.Buffered(); q != 0 {
		t.Errorf("entries above the new mode are not queued, %d", q)
	}
	clk.t = clk.t.Add(time.Second)
}

func TestShipper_OffModePollsEmptyEveryMinute(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "off", FlushInterval: 10}}
	s, clk := newTestShipper(f)
	s.SetMode(0, Off)
	if wait := s.Flush(context.Background()); wait != OffPollInterval {
		t.Errorf("wait after the first poll: %v", wait)
	}
	if len(f.calls) != 1 || len(f.calls[0]) != 0 || f.calls[0] == nil {
		t.Fatalf("mode off must POST an empty entries list, got %v", f.calls)
	}
	s.Add(Error, Entry{Message: "ignored"})
	clk.t = clk.t.Add(30 * time.Second)
	if wait := s.Flush(context.Background()); wait != 30*time.Second || len(f.calls) != 1 {
		t.Errorf("no poll before 60s: wait %v calls %d", wait, len(f.calls))
	}
	clk.t = clk.t.Add(31 * time.Second)
	f.resp = Response{LogMode: "warn", FlushInterval: 10}
	s.Flush(context.Background())
	if len(f.calls) != 2 || s.Mode() != Warn {
		t.Errorf("poll after 60s learns the new mode: calls %d mode %v", len(f.calls), s.Mode())
	}
}

func TestShipper_NotFoundBacksOffTenMinutes(t *testing.T) {
	f := &fakeSender{err: ErrNotFound}
	s, clk := newTestShipper(f)
	s.Add(Error, Entry{Message: "e"})
	if wait := s.Flush(context.Background()); wait != NotFoundBackoff {
		t.Fatalf("404 backoff: %v", wait)
	}
	clk.t = clk.t.Add(5 * time.Minute)
	s.Flush(context.Background())
	if len(f.calls) != 1 {
		t.Errorf("no retry within 10 minutes, calls %d", len(f.calls))
	}
	clk.t = clk.t.Add(6 * time.Minute)
	s.Flush(context.Background())
	if len(f.calls) != 2 {
		t.Errorf("retry after 10 minutes, calls %d", len(f.calls))
	}
	if q, _ := s.Buffered(); q != 1 {
		t.Errorf("entries stay queued while the plugin is missing, %d", q)
	}
}

func TestShipper_ErrorKeepsEntriesAndBacksOff(t *testing.T) {
	f := &fakeSender{err: errors.New("connection refused")}
	s, clk := newTestShipper(f)
	s.Add(Error, Entry{Message: "e"})
	w1 := s.Flush(context.Background())
	if q, _ := s.Buffered(); q != 1 || w1 <= 0 {
		t.Fatalf("failed push keeps the entry: queue %d wait %v", q, w1)
	}
	clk.t = clk.t.Add(w1 + time.Second)
	f.mu.Lock()
	f.err = nil
	f.resp = Response{LogMode: "warn", FlushInterval: 10}
	f.mu.Unlock()
	s.Flush(context.Background())
	if q, _ := s.Buffered(); q != 0 || f.sent() != 2 {
		t.Errorf("entry is delivered after recovery: queue %d sent %d", q, f.sent())
	}
}

func TestShipper_RunFlushesAtThresholdAndOnWake(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "warn", FlushInterval: 3600}}
	s := NewShipper(f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	// The first poll runs about a second after start
	deadline := time.Now().Add(5 * time.Second)
	for f.sent() == 0 && len(f.calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	f.mu.Lock()
	first := len(f.calls)
	f.mu.Unlock()
	if first == 0 {
		t.Fatal("the shipper must poll the panel shortly after start")
	}
	for i := 0; i < FlushAtEntries; i++ {
		s.Add(Error, Entry{Message: "m"})
	}
	deadline = time.Now().Add(5 * time.Second)
	for f.sent() < FlushAtEntries && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if f.sent() != FlushAtEntries {
		t.Errorf("200 queued entries flush immediately, sent %d", f.sent())
	}
}

func TestHub_RoutingAndFloor(t *testing.T) {
	h := NewHub()
	fa, fb := &fakeSender{}, &fakeSender{}
	a, _ := newTestShipper(fa)
	b, _ := newTestShipper(fb)
	h.Register(a)
	h.Register(b)
	h.MapNode("[https://p1]-vless:1", 1, a)
	h.MapNode("[https://p2]-vless:2", 2, b)

	var floors []Level
	remove := h.OnFloorChange(func(l Level) { floors = append(floors, l) })
	defer remove()
	if len(floors) != 1 || floors[0] != Warn {
		t.Fatalf("floor callback runs immediately with the current floor: %v", floors)
	}
	b.SetMode(0, Debug)
	if h.Floor() != Debug || floors[len(floors)-1] != Debug {
		t.Errorf("floor follows the most verbose shipper: %v %v", h.Floor(), floors)
	}

	at := time.Unix(1_760_000_000, 500_000_000)
	h.Emit(Warn, "[https://p1]-vless:1", "inbound/vless[x]", "to a", at)
	h.Emit(Debug, "[https://p2]-vless:2", "inbound/vless[y]", "to b", at)
	h.Emit(Error, "", "", "global", at)
	h.Emit(Debug, "[https://p1]-vless:1", "t", "a debug is below a's warn mode", at)
	qa, _ := a.Buffered()
	qb, _ := b.Buffered()
	if qa != 2 || qb != 2 {
		t.Fatalf("a: %d (node entry + global), b: %d (node entry + global)", qa, qb)
	}
	if a.buf[0].NodeID != 1 || a.buf[0].Tag != "inbound/vless[x]" || a.buf[0].Time != 1_760_000_000.5 {
		t.Errorf("entry: %+v", a.buf[0])
	}
	if a.buf[1].NodeID != 0 || b.buf[0].NodeID != 2 {
		t.Errorf("node attribution: a=%+v b=%+v", a.buf[1], b.buf[0])
	}

	h.Unregister(b)
	if h.Floor() != Warn {
		t.Errorf("floor drops when the verbose shipper goes away: %v", h.Floor())
	}
	h.Emit(Warn, "[https://p2]-vless:2", "t", "no shipper left for this node: broadcast", at)
	if q, _ := a.Buffered(); q != 3 {
		t.Errorf("unmapped tags are broadcast, a has %d", q)
	}
}

func TestLogrusBridge(t *testing.T) {
	h := NewHub()
	f := &fakeSender{}
	s, _ := newTestShipper(f)
	h.Register(s)
	h.MapNode("node-1", 7, s)

	var console bytes.Buffer
	prevOut := logrus.StandardLogger().Out
	prevFmt := logrus.StandardLogger().Formatter
	prevLevel := logrus.GetLevel()
	logrus.SetOutput(&console)
	logrus.SetLevel(logrus.ErrorLevel) // the operator wants errors on the console only
	uninstall := InstallLogrus(h)
	defer func() {
		uninstall()
		logrus.SetOutput(prevOut)
		logrus.SetFormatter(prevFmt)
		logrus.SetLevel(prevLevel)
	}()

	if logrus.GetLevel() != logrus.WarnLevel {
		t.Fatalf("the logger level rises to the panel's mode (warn): %v", logrus.GetLevel())
	}
	logrus.WithField("tag", "node-1").Warn("warn line")
	logrus.WithField("tag", "node-1").Error("error line")
	logrus.Info("info below the mode")
	logrus.WithField(SkipField, true).Error("shipper diagnostics are not shipped")

	if strings.Contains(console.String(), "warn line") || !strings.Contains(console.String(), "error line") {
		t.Errorf("the console keeps its configured level:\n%s", console.String())
	}
	if q, _ := s.Buffered(); q != 2 {
		t.Fatalf("2 entries shipped (warn + error), got %d", q)
	}
	if s.buf[0].NodeID != 7 || s.buf[0].Level != "warn" || s.buf[0].Message != "warn line" {
		t.Errorf("entry: %+v", s.buf[0])
	}

	s.SetMode(0, Debug)
	if logrus.GetLevel() != logrus.DebugLevel {
		t.Errorf("debug mode raises the logger level: %v", logrus.GetLevel())
	}
	s.SetMode(0, Off)
	if logrus.GetLevel() != logrus.ErrorLevel {
		t.Errorf("mode off restores the configured level: %v", logrus.GetLevel())
	}
}
