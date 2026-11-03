package logship

import (
	"context"
	"testing"
	"time"
)

func bptr(b bool) *bool { return &b }

// entryAt queues an entry stamped with the fake clock's time.
func entryAt(s *Shipper, c *clock, msg string) {
	s.Add(Warn, Entry{Time: float64(c.t.UnixNano()) / 1e9, Message: msg})
}

// ship=false: entries are kept, older than retain_seconds go, nothing is sent
// but the empty poll runs every flush interval.
func TestStandby_TrimsByAgeAndDoesNotSend(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "warn", FlushInterval: 10, Ship: bptr(false), RetainSeconds: 60}}
	s, c := newTestShipper(f)

	entryAt(s, c, "old")
	s.Flush(context.Background()) // first poll: the panel says standby, retain 60 s
	if !s.Standby() {
		t.Fatal("ship=false must put the shipper in standby")
	}
	if f.sent() != 1 { // the entry queued before the answer went out with the first poll
		t.Fatalf("first push carries the backlog queued before standby: %d", f.sent())
	}

	f.calls = nil
	entryAt(s, c, "a")
	c.t = c.t.Add(40 * time.Second)
	entryAt(s, c, "b")
	c.t = c.t.Add(30 * time.Second) // "a" is 70 s old, "b" 30 s
	entryAt(s, c, "c")
	if q, _ := s.Buffered(); q != 2 {
		t.Fatalf("standby keeps only the last 60 s: queued %d, want 2 (b, c)", q)
	}
	s.Flush(context.Background())
	if f.sent() != 0 {
		t.Fatalf("standby sends no entries, sent %d", f.sent())
	}
	if q, _ := s.Buffered(); q != 2 {
		t.Fatalf("entries stay in the ring: %d", q)
	}
}

func TestStandby_CapacityKeepsNewest(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "warn", Ship: bptr(false)}}
	s, c := newTestShipper(f)
	s.Flush(context.Background())
	for i := 0; i < StandbyCapacity+100; i++ {
		entryAt(s, c, "x")
	}
	if q, _ := s.Buffered(); q != StandbyCapacity {
		t.Fatalf("standby ring holds %d, got %d", StandbyCapacity, q)
	}
}

// The standby poll is empty and runs every flush interval (not every 60 s).
func TestStandby_PollCadence(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "warn", FlushInterval: 10, Ship: bptr(false)}}
	s, c := newTestShipper(f)
	ctx := context.Background()

	s.Flush(ctx)
	if len(f.calls) != 1 {
		t.Fatalf("first poll: %d calls", len(f.calls))
	}
	entryAt(s, c, "kept")
	for _, step := range []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second} {
		c.t = c.t.Add(step)
		wait := s.Flush(ctx)
		if wait <= 0 || wait > 10*time.Second {
			t.Fatalf("wait %v must be within the flush interval", wait)
		}
	}
	// polls at t=0, 10, 20: three calls, all empty
	if len(f.calls) != 3 {
		t.Fatalf("polls over 20 s: %d, want 3", len(f.calls))
	}
	for i, call := range f.calls {
		if len(call) != 0 {
			t.Errorf("poll %d carried %d entries", i, len(call))
		}
	}
}

// ship flips to true in a response: the backlog goes at once, in batches of
// 500, oldest first, with each entry's original time.
func TestStandby_BacklogOnShipTrue(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "warn", FlushInterval: 10, Ship: bptr(false)}}
	s, c := newTestShipper(f)
	ctx := context.Background()
	s.Flush(ctx)

	first := c.t
	for i := 0; i < 1200; i++ {
		c.t = c.t.Add(10 * time.Millisecond)
		entryAt(s, c, "m")
	}
	f.calls = nil
	f.resp = Response{LogMode: "warn", FlushInterval: 10, Ship: bptr(true)}
	c.t = c.t.Add(10 * time.Second)
	// the next due poll learns ship=true and sends the backlog right away
	s.Flush(ctx)
	if s.Standby() {
		t.Fatal("ship=true must leave standby")
	}
	var sizes []int
	for _, call := range f.calls {
		sizes = append(sizes, len(call))
	}
	want := []int{0, 500, 500, 200}
	if len(sizes) != len(want) {
		t.Fatalf("batches %v, want %v", sizes, want)
	}
	for i := range want {
		if sizes[i] != want[i] {
			t.Fatalf("batches %v, want %v", sizes, want)
		}
	}
	got := f.calls[1][0].Time
	if got <= float64(first.UnixNano())/1e9 || got != f.calls[1][0].Time || got > f.calls[2][0].Time {
		t.Errorf("oldest first with original times: %v", got)
	}
	if f.calls[1][499].Time >= f.calls[2][0].Time {
		t.Error("batches are in order")
	}
	if q, _ := s.Buffered(); q != 0 {
		t.Errorf("backlog sent, %d left", q)
	}
}

// A WS event with ship=true wakes the flusher and the backlog goes out
// without waiting for the next poll.
func TestStandby_WSEventShipsBacklog(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "warn", FlushInterval: 10, Ship: bptr(false)}}
	s, c := newTestShipper(f)
	ctx := context.Background()
	s.Flush(ctx)
	entryAt(s, c, "one")
	entryAt(s, c, "two")
	f.calls = nil

	c.t = c.t.Add(time.Second) // not a due poll
	s.SetShip(true, 0)
	select {
	case <-s.wake:
	default:
		t.Fatal("SetShip must wake the flusher")
	}
	f.resp = Response{LogMode: "warn", FlushInterval: 10} // old-style answer: ship
	s.Flush(ctx)
	if f.sent() != 2 || f.calls[0][0].Message != "one" {
		t.Fatalf("backlog after the event: %+v", f.calls)
	}

	// and back to standby by event; retain_seconds applies
	s.SetShip(false, 30)
	if !s.Standby() {
		t.Fatal("ship=false event")
	}
	entryAt(s, c, "a")
	c.t = c.t.Add(31 * time.Second)
	entryAt(s, c, "b")
	if q, _ := s.Buffered(); q != 1 {
		t.Fatalf("retain 30 s: queued %d, want 1", q)
	}
}

// A panel that sends neither field behaves as before.
func TestStandby_OldPanelUnchanged(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "warn", FlushInterval: 10}}
	s, c := newTestShipper(f)
	ctx := context.Background()
	entryAt(s, c, "one")
	s.Flush(ctx)
	if s.Standby() || f.sent() != 1 {
		t.Fatalf("old panel: standby=%v sent=%d", s.Standby(), f.sent())
	}
	entryAt(s, c, "two")
	s.Flush(ctx)
	if f.sent() != 2 {
		t.Fatalf("entries ship normally: %d", f.sent())
	}
	// idle polling stays at 60 s like before
	f.calls = nil
	c.t = c.t.Add(20 * time.Second)
	s.Flush(ctx)
	if len(f.calls) != 0 {
		t.Errorf("idle polls stay at 60 s, got %d calls", len(f.calls))
	}
}

// Off stays the user's explicit off: ship never turns it on or off, and ship
// state does not change the mode.
func TestStandby_ModeIsIndependentOfShip(t *testing.T) {
	f := &fakeSender{resp: Response{LogMode: "info", FlushInterval: 10, Ship: bptr(false)}}
	s, _ := newTestShipper(f)
	s.Flush(context.Background())
	if s.Mode() != Info || s.MaxLevel() != Info {
		t.Fatalf("standby keeps the real mode: %v", s.Mode())
	}
	s.Add(Info, Entry{Message: "info stays collected"})
	if q, _ := s.Buffered(); q != 1 {
		t.Fatalf("collected at the mode level: %d", q)
	}
	s.Add(Debug, Entry{Message: "debug is not"})
	if q, _ := s.Buffered(); q != 1 {
		t.Fatalf("mode filter still applies: %d", q)
	}
	// lowering the mode filters the ring as before
	s.SetMode(0, Error)
	s.Flush(context.Background())
	if q, _ := s.Buffered(); q != 0 {
		t.Fatalf("entries above the lowered mode are dropped: %d", q)
	}
}
