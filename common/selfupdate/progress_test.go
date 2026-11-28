package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProgressWriterCountsBytes(t *testing.T) {
	var buf bytes.Buffer
	var seen [][2]int64
	pw := &progressWriter{w: &buf, total: 10, fn: func(n, total int64) { seen = append(seen, [2]int64{n, total}) }}
	pw.Write([]byte("abcd"))
	pw.Write([]byte("efghij"))
	if buf.String() != "abcdefghij" {
		t.Fatalf("data not passed through: %q", buf.String())
	}
	if len(seen) != 2 || seen[0] != [2]int64{4, 10} || seen[1] != [2]int64{10, 10} {
		t.Fatalf("reports: %v", seen)
	}
}

// recorder collects what a tracker emits.
type recorder struct {
	mu  sync.Mutex
	got []Progress
}

func (r *recorder) emit(p Progress) { r.mu.Lock(); r.got = append(r.got, p); r.mu.Unlock() }
func (r *recorder) list() []Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Progress(nil), r.got...)
}

func testTracker(rec *recorder, clock *time.Time) *tracker {
	return &tracker{emit: rec.emit, version: "0.1.2", now: func() time.Time { return *clock }, lastPct: -1}
}

func TestTrackerThrottlesDownloadReports(t *testing.T) {
	rec := &recorder{}
	now := time.Unix(1000, 0)
	tr := testTracker(rec, &now)

	tr.download(0, 1000)              // first report always goes out
	for i := int64(1); i <= 50; i++ { // 50 more writes within the same second, 1%..5%
		tr.download(i, 1000)
	}
	if got := rec.list(); len(got) != 1 || got[0].Percent == nil || *got[0].Percent != 0 {
		t.Fatalf("within a second only the first report is sent: %+v", got)
	}

	now = now.Add(1100 * time.Millisecond)
	tr.download(120, 1000) // 12%, a second later
	tr.download(121, 1000) // same second: held back
	now = now.Add(300 * time.Millisecond)
	tr.download(500, 1000) // 50% but only 300 ms later: held back
	got := rec.list()
	if len(got) != 2 || *got[1].Percent != 12 || got[1].Bytes != 120 || got[1].Total != 1000 || got[1].Stage != StageDownloading {
		t.Fatalf("second report: %+v", got)
	}

	tr.download(1000, 1000) // the 100% report is never held back
	got = rec.list()
	if len(got) != 3 || *got[2].Percent != 100 {
		t.Fatalf("final report: %+v", got)
	}
	tr.download(1000, 1000) // no change: no duplicate
	if len(rec.list()) != 3 {
		t.Fatalf("a repeated 100%% must not be sent again: %+v", rec.list())
	}
}

func TestTrackerUnknownSizeReportsBytesOncePerSecond(t *testing.T) {
	rec := &recorder{}
	now := time.Unix(1000, 0)
	tr := testTracker(rec, &now)
	tr.download(100, -1)
	tr.download(200, -1)
	now = now.Add(2 * time.Second)
	tr.download(900, -1)
	got := rec.list()
	if len(got) != 2 || got[0].Percent != nil || got[1].Bytes != 900 || got[1].Total != 0 {
		t.Fatalf("unknown size: %+v", got)
	}
}

func TestPublisherKeepsOnlyNewestWhilePosting(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var mu sync.Mutex
	var posted []Progress
	pub := NewPublisher(func(ctx context.Context, p Progress) error {
		mu.Lock()
		first := len(posted) == 0
		posted = append(posted, p)
		mu.Unlock()
		if first {
			close(started)
			<-release // the first post hangs: later reports pile up behind it
		}
		return nil
	})
	pub.Publish(Progress{Stage: StageDownloading})
	<-started
	for _, s := range []Stage{StageVerifying, StageExtracting, StageInstalling} {
		pub.Publish(Progress{Stage: s}) // never blocks
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pub.Flush(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(posted) != 2 || posted[0].Stage != StageDownloading || posted[1].Stage != StageInstalling {
		t.Fatalf("want the first and the newest report, got %+v", posted)
	}
}

func TestPublisherSurvivesFailingPostAndNil(t *testing.T) {
	var n int
	pub := NewPublisher(func(ctx context.Context, p Progress) error { n++; return errors.New("panel down") })
	pub.Publish(Progress{Stage: StageFailed})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pub.Flush(ctx)
	if n != 1 {
		t.Fatalf("post called %d times", n)
	}
	var nilPub *Publisher
	nilPub.Publish(Progress{}) // no panic
	nilPub.Flush(ctx)
}

func TestPublisherFlushHonoursContext(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	pub := NewPublisher(func(ctx context.Context, p Progress) error { <-block; return nil })
	pub.Publish(Progress{Stage: StageRestarting})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	pub.Flush(ctx)
	if time.Since(start) > 2*time.Second {
		t.Fatal("Flush must give up when its context ends")
	}
}

func TestPublishTruncatesLongMessages(t *testing.T) {
	got := make(chan Progress, 1)
	pub := NewPublisher(func(ctx context.Context, p Progress) error { got <- p; return nil })
	pub.Publish(Progress{Stage: StageFailed, Message: strings.Repeat("é", 1000)})
	select {
	case p := <-got:
		if n := len([]rune(p.Message)); n != maxMessage {
			t.Fatalf("message has %d runes, want %d", n, maxMessage)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no report")
	}
}

// The stage order of a successful Apply, with the download percentage.
func TestApplyReportsStagesInOrder(t *testing.T) {
	z := makeZip(t, "FNode", "#!/bin/sh\necho 0.0.9\n")
	setup(t, z, "SHA2-256= "+sum(z)+"\n")
	rec := &recorder{}
	tr := &tracker{emit: rec.emit, version: "0.0.9", now: time.Now, lastPct: -1}
	if _, err := apply(context.Background(), "0.0.9", tr); err != nil {
		t.Fatal(err)
	}
	var stages []Stage
	for _, p := range rec.list() {
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	}
	want := []Stage{StageDownloading, StageVerifying, StageExtracting, StageInstalling}
	if len(stages) != len(want) {
		t.Fatalf("stages %v, want %v", stages, want)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stages %v, want %v", stages, want)
		}
	}
	var last *Progress
	for _, p := range rec.list() {
		if p.Stage == StageDownloading && p.Percent != nil {
			p := p
			last = &p
		}
		if p.Version != "0.0.9" {
			t.Errorf("version missing: %+v", p)
		}
	}
	if last == nil || *last.Percent != 100 || last.Total != int64(len(z)) || last.Bytes != int64(len(z)) {
		t.Fatalf("the download must end at 100%% of %d bytes: %+v", len(z), last)
	}
}

// Run on a bad checksum reports failed with the reason and never restarts.
func TestRunReportsFailure(t *testing.T) {
	z := makeZip(t, "FNode", "#!/bin/sh\necho 0.0.9\n")
	exe := setup(t, z, "SHA2-256= "+strings.Repeat("0", 64)+"\n")
	var mu sync.Mutex
	var posted []Progress
	SetPublisher(NewPublisher(func(ctx context.Context, p Progress) error {
		mu.Lock()
		posted = append(posted, p)
		mu.Unlock()
		return nil
	}))
	t.Cleanup(func() { SetPublisher(nil) })

	Run("0.0.9", "0.0.1")
	mu.Lock()
	defer mu.Unlock()
	if len(posted) == 0 {
		t.Fatal("nothing was reported")
	}
	last := posted[len(posted)-1]
	if last.Stage != StageFailed || !strings.Contains(last.Message, "checksum mismatch") || last.Version != "0.0.9" {
		t.Fatalf("last report: %+v", last)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("binary must stay untouched")
	}
}

func TestRunReportsDisabled(t *testing.T) {
	Disabled = true
	t.Cleanup(func() { Disabled = false })
	got := make(chan Progress, 4)
	SetPublisher(NewPublisher(func(ctx context.Context, p Progress) error { got <- p; return nil }))
	t.Cleanup(func() { SetPublisher(nil) })
	Run("0.0.9", "0.0.1")
	p := <-got
	if p.Stage != StageFailed || !strings.Contains(p.Message, "DisableRemoteUpdate") {
		t.Fatalf("report: %+v", p)
	}
}

// Reporting is optional: no publisher, or a broken one, must not change the outcome.
func TestApplyWithoutPublisherStillWorks(t *testing.T) {
	z := makeZip(t, "FNode", "#!/bin/sh\necho 0.0.9\n")
	setup(t, z, "SHA2-256= "+sum(z)+"\n")
	SetPublisher(nil)
	if ver, err := Apply(context.Background(), "0.0.9"); err != nil || ver != "0.0.9" {
		t.Fatalf("Apply: %q %v", ver, err)
	}
}

func TestApplyIgnoresFailingPublisher(t *testing.T) {
	z := makeZip(t, "FNode", "#!/bin/sh\necho 0.0.9\n")
	setup(t, z, "SHA2-256= "+sum(z)+"\n")
	SetPublisher(NewPublisher(func(ctx context.Context, p Progress) error { return errors.New("panel unreachable") }))
	t.Cleanup(func() { SetPublisher(nil) })
	if ver, err := Apply(context.Background(), "0.0.9"); err != nil || ver != "0.0.9" {
		t.Fatalf("Apply: %q %v", ver, err)
	}
}

// fetchProgress reports against Content-Length.
func TestFetchProgressUsesContentLength(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip()
	}
	body := bytes.Repeat([]byte("x"), 70000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body))) // large bodies are otherwise chunked
		w.Write(body)
	}))
	defer srv.Close()
	var lastN, lastTotal int64
	var buf bytes.Buffer
	if err := fetchProgress(context.Background(), srv.URL, &buf, 1<<20, func(n, total int64) { lastN, lastTotal = n, total }); err != nil {
		t.Fatal(err)
	}
	if lastN != int64(len(body)) || lastTotal != int64(len(body)) || buf.Len() != len(body) {
		t.Fatalf("n=%d total=%d copied=%d", lastN, lastTotal, buf.Len())
	}

	// no Content-Length (chunked): the total is unknown (-1), bytes still count
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		w.Write(body)
	}))
	defer chunked.Close()
	lastN, lastTotal = 0, 0
	buf.Reset()
	if err := fetchProgress(context.Background(), chunked.URL, &buf, 1<<20, func(n, total int64) { lastN, lastTotal = n, total }); err != nil {
		t.Fatal(err)
	}
	if lastN != int64(len(body)) || lastTotal > 0 {
		t.Fatalf("chunked: n=%d total=%d", lastN, lastTotal)
	}
}

func TestTakeRollbackReasonOnce(t *testing.T) {
	r := "config migration failed: boom"
	rolledBack.Store(&r)
	if got := TakeRollbackReason(); got != r {
		t.Fatalf("got %q", got)
	}
	if got := TakeRollbackReason(); got != "" {
		t.Fatalf("second call must be empty, got %q", got)
	}
}
