package selfupdate

import (
	"context"
	"io"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Stage is where a remote update currently is. The panel (CustomNode plugin)
// shows it next to the download percentage.
type Stage string

const (
	StageDownloading Stage = "downloading" // release zip, the only stage with a percentage
	StageVerifying   Stage = "verifying"   // checksum file and SHA-256
	StageExtracting  Stage = "extracting"  // unzip and run the new binary once
	StageInstalling  Stage = "installing"  // swap the binary, keep the old one
	StageRestarting  Stage = "restarting"  // systemd restart requested, cannot be measured
	StageFailed      Stage = "failed"      // the update stopped, the current binary keeps running
	StageRolledBack  Stage = "rolled_back" // the new binary failed its start-up check
)

const (
	// maxMessage keeps a progress report small (error texts can be long).
	maxMessage = 300
	// minInterval is the least time between two download-percentage reports.
	minInterval = time.Second
	// flushTimeout bounds how long Run waits for the last report before a restart.
	flushTimeout = 4 * time.Second
	postTimeout  = 10 * time.Second
)

// Progress is one report to the panel.
type Progress struct {
	Version string `json:"version"`
	Stage   Stage  `json:"stage"`
	// Percent is set while downloading and the size is known (0-100).
	Percent *int `json:"percent,omitempty"`
	// Bytes and Total are the downloaded and expected size (Total 0 = unknown).
	Bytes   int64  `json:"bytes,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Message string `json:"message,omitempty"`
}

// PostFunc sends one report to the panel. Errors are logged at debug level
// only: reporting must never fail or slow down the update.
type PostFunc func(ctx context.Context, p Progress) error

// Publisher sends reports in the background. Publish never blocks: while a post
// is running only the newest pending report is kept (the older ones are stale
// by then), and the worker goroutine exists only while there is something to
// send.
type Publisher struct {
	post PostFunc

	mu      sync.Mutex
	pending *Progress
	done    chan struct{} // non-nil while a worker runs; closed when it exits
}

// NewPublisher returns a Publisher that sends through post.
func NewPublisher(post PostFunc) *Publisher { return &Publisher{post: post} }

// Publish queues p for sending. It is safe on a nil Publisher.
func (p *Publisher) Publish(pr Progress) {
	if p == nil || p.post == nil {
		return
	}
	pr.Message = truncate(pr.Message, maxMessage)
	p.mu.Lock()
	p.pending = &pr
	if p.done == nil {
		p.done = make(chan struct{})
		go p.work(p.done)
	}
	p.mu.Unlock()
}

func (p *Publisher) work(done chan struct{}) {
	for {
		p.mu.Lock()
		pr := p.pending
		p.pending = nil
		if pr == nil {
			p.done = nil
			p.mu.Unlock()
			close(done)
			return
		}
		p.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), postTimeout)
		if err := p.post(ctx, *pr); err != nil {
			log.Debugf("Update: progress report not delivered: %v", err)
		}
		cancel()
	}
}

// Flush waits until everything queued so far was sent (or failed), at most
// until ctx ends. It is how the "restarting" report gets out before the restart.
func (p *Publisher) Flush(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

var (
	pubMu     sync.Mutex
	publisher *Publisher
)

// SetPublisher chooses where Run reports its progress (nil = nowhere). The
// machine sets it once at start.
func SetPublisher(p *Publisher) {
	pubMu.Lock()
	publisher = p
	pubMu.Unlock()
}

func currentPublisher() *Publisher {
	pubMu.Lock()
	defer pubMu.Unlock()
	return publisher
}

// tracker turns the update steps into throttled reports for one version.
type tracker struct {
	emit    func(Progress) // Publisher.Publish; tests record instead
	version string
	now     func() time.Time

	mu       sync.Mutex
	lastAt   time.Time
	lastPct  int
	lastSent bool
}

func newTracker(pub *Publisher, version string) *tracker {
	return &tracker{emit: pub.Publish, version: version, now: time.Now, lastPct: -1}
}

// stage reports a stage change at once (stage changes are rare, so they are
// not throttled).
func (t *tracker) stage(s Stage, msg string) {
	if t == nil {
		return
	}
	t.emit(Progress{Version: t.version, Stage: s, Message: msg})
}

// download reports download progress: at most one report per second, and only
// when the percentage (or, without a known size, the byte count) moved. The
// first and the final (100%) report always go out.
func (t *tracker) download(n, total int64) {
	if t == nil {
		return
	}
	pct := -1
	if total > 0 {
		pct = int(n * 100 / total)
		if pct > 100 {
			pct = 100
		}
	}
	t.mu.Lock()
	now := t.now()
	final := total > 0 && n >= total
	moved := pct != t.lastPct || (pct < 0 && n > 0)
	if t.lastSent && (!moved || (now.Sub(t.lastAt) < minInterval && !(final && pct != t.lastPct))) {
		t.mu.Unlock()
		return
	}
	t.lastSent, t.lastAt, t.lastPct = true, now, pct
	t.mu.Unlock()

	pr := Progress{Version: t.version, Stage: StageDownloading, Bytes: n}
	if total > 0 {
		pr.Total = total
		pr.Percent = &pct
	}
	t.emit(pr)
}

// progressWriter counts the bytes copied to w and calls fn after each write.
type progressWriter struct {
	w     io.Writer
	n     int64
	total int64
	fn    func(n, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	if p.fn != nil && n > 0 {
		p.fn(p.n, p.total)
	}
	return n, err
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
