package logship

import (
	"context"
	"sync"
	"testing"
	"time"
)

type metaSender struct {
	mu    sync.Mutex
	metas []Meta
	resp  Response
}

func (m *metaSender) SendLogs(ctx context.Context, e []Entry) (Response, error) {
	return m.SendLogsMeta(ctx, e, Meta{})
}

func (m *metaSender) SendLogsMeta(_ context.Context, _ []Entry, meta Meta) (Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metas = append(m.metas, meta)
	return m.resp, nil
}

// A new outbound request is pushed right away (not on the next minute poll)
// and carried in the request meta.
func TestShipper_MissingOutboundsArePushed(t *testing.T) {
	f := &metaSender{resp: Response{LogMode: "info", FlushInterval: 10}}
	s := NewShipper(f)
	c := &clock{t: time.Unix(1_760_000_000, 0)}
	s.now = c.now
	s.Flush(context.Background()) // first poll
	s.SetMissing(5, []string{"hk", "us"})
	s.Flush(context.Background())
	if len(f.metas) != 2 || len(f.metas[1].Missing[5]) != 2 {
		t.Fatalf("missing tags must be sent at once: %+v", f.metas)
	}
	s.SetMissing(5, []string{"us", "hk"}) // same set: no extra push
	s.Flush(context.Background())
	if len(f.metas) != 2 {
		t.Errorf("an unchanged request does not trigger a push: %d", len(f.metas))
	}
	s.SetMissing(5, nil)
	if len(s.Missing()) != 0 {
		t.Errorf("an empty list clears the request")
	}
}

// The hub keeps a request for a node tag that is not mapped yet and hands it
// to the shipper when the controller maps the tag.
func TestHub_PendingMissing(t *testing.T) {
	h := NewHub()
	s := NewShipper(&fakeSender{})
	h.Register(s)
	h.ReportMissing("node-x", []string{"jp"})
	if len(s.Missing()) != 0 {
		t.Fatal("unmapped tag: nothing reaches the shipper yet")
	}
	h.MapNode("node-x", 9, s)
	if got := s.Missing()[9]; len(got) != 1 || got[0] != "jp" {
		t.Fatalf("pending request delivered on MapNode: %v", s.Missing())
	}
	h.UnmapNode("node-x")
	if len(s.Missing()) != 0 {
		t.Errorf("UnmapNode clears the node's request")
	}
}

// The panel's per-node modes (machine log type selection) replace overrides.
func TestShipper_NodeModesFromPanel(t *testing.T) {
	f := &metaSender{resp: Response{LogMode: "warn", HasNodeModes: true, NodeModes: map[int]string{7: "debug"}}}
	s := NewShipper(f)
	c := &clock{t: time.Unix(1_760_000_000, 0)}
	s.now = c.now
	s.Flush(context.Background())
	if s.Mode() != Warn || s.MaxLevel() != Debug {
		t.Fatalf("base warn, node 7 debug: base %v max %v", s.Mode(), s.MaxLevel())
	}
	s.Add(Debug, Entry{NodeID: 7, Message: "kept"})
	s.Add(Debug, Entry{NodeID: 8, Message: "dropped"})
	if q, _ := s.Buffered(); q != 1 {
		t.Errorf("only node 7 ships debug: %d", q)
	}
	f.resp = Response{LogMode: "warn", HasNodeModes: true}
	c.t = c.t.Add(OffPollInterval)
	s.Flush(context.Background())
	if s.MaxLevel() != Warn {
		t.Errorf("an empty node_modes clears the overrides: %v", s.MaxLevel())
	}
}
