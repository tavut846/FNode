// Package logship ships FNode and sing-box logs to the Xboard panel
// (CustomNode plugin: POST /api/v2/custom-node/logs).
//
// A Shipper owns a bounded ring buffer and a flush loop for one panel
// connection (a machine, or a single node). The Hub routes log entries from
// the global sources (logrus, the sing-box log capture) to the shippers.
// Nothing here blocks the logging path or lets a failed push affect a node.
package logship

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Level is a log verbosity: lower is more severe. Off ships nothing.
type Level int

const (
	Off   Level = -1
	Error Level = 0
	Warn  Level = 1
	Info  Level = 2
	Debug Level = 3
)

// ParseMode parses the panel's log_mode (off|error|warn|info|debug).
func ParseMode(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off":
		return Off, true
	case "error":
		return Error, true
	case "warn", "warning":
		return Warn, true
	case "info":
		return Info, true
	case "debug":
		return Debug, true
	}
	return Warn, false
}

func (l Level) String() string {
	switch l {
	case Off:
		return "off"
	case Error:
		return "error"
	case Warn:
		return "warn"
	case Info:
		return "info"
	default:
		return "debug"
	}
}

// Limits of the panel contract.
const (
	MaxBatch       = 500
	MaxMessageSize = 4096
	DefaultBuffer  = 2000
	FlushAtEntries = 200
	// OffPollInterval is how often a shipper in mode "off" polls the panel
	// (an empty entries list) to learn the current mode.
	OffPollInterval = 60 * time.Second
	// NotFoundBackoff is the retry interval when the plugin is missing (404).
	NotFoundBackoff = 10 * time.Minute
)

// Entry is one log entry in the panel's wire format.
type Entry struct {
	Time    float64 `json:"time"`
	Level   string  `json:"level"`
	NodeID  int     `json:"node_id,omitempty"`
	Tag     string  `json:"tag,omitempty"`
	Message string  `json:"message"`
}

// Response is the panel's answer: {"data":{"log_mode","accepted","flush_interval"}}.
type Response struct {
	LogMode       string
	Accepted      int
	FlushInterval int
}

// ErrNotFound means the panel has no log endpoint (HTTP 404: plugin missing).
var ErrNotFound = errors.New("panel log endpoint not found")

// Sender posts a batch of entries to the panel.
type Sender interface {
	SendLogs(ctx context.Context, entries []Entry) (Response, error)
}

// Shipper buffers entries and flushes them to one panel connection.
type Shipper struct {
	sender Sender
	now    func() time.Time

	mu        sync.Mutex
	buf       []Entry
	capacity  int
	dropped   int
	mode      Level
	overrides map[int]Level
	interval  time.Duration
	// notBefore holds back every request until then (404 and error backoff).
	notBefore  time.Time
	lastPoll   time.Time
	failures   int
	lastServer Level
	haveServer bool

	wake     chan struct{}
	onChange func()
}

// NewShipper creates a shipper in the default mode (warn) with a flush
// interval of 10 seconds until the panel says otherwise.
func NewShipper(sender Sender) *Shipper {
	return &Shipper{
		sender:    sender,
		now:       time.Now,
		capacity:  DefaultBuffer,
		mode:      Warn,
		overrides: make(map[int]Level),
		interval:  10 * time.Second,
		wake:      make(chan struct{}, 1),
	}
}

// OnModeChange registers a callback that runs (without locks held) whenever
// the effective maximum verbosity of the shipper changes.
func (s *Shipper) OnModeChange(fn func()) { s.onChange = fn }

// Mode returns the base mode.
func (s *Shipper) Mode() Level {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// MaxLevel returns the most verbose level any node of the shipper ships at;
// Off when nothing is shipped.
func (s *Shipper) MaxLevel() Level {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxLevelLocked()
}

func (s *Shipper) maxLevelLocked() Level {
	m := s.mode
	for _, l := range s.overrides {
		if l > m {
			m = l
		}
	}
	return m
}

// SetMode applies a log mode pushed over the WebSocket. nodeID 0 sets the base
// mode for the whole connection (and clears per-node overrides); otherwise
// only that node is affected.
func (s *Shipper) SetMode(nodeID int, mode Level) {
	s.mu.Lock()
	before := s.maxLevelLocked()
	if nodeID == 0 {
		s.mode = mode
		s.overrides = make(map[int]Level)
	} else {
		s.overrides[nodeID] = mode
	}
	s.afterModeChangeLocked()
	after := s.maxLevelLocked()
	s.mu.Unlock()
	s.notify(before != after)
	// ship what is buffered / learn the panel state without waiting
	s.signal()
}

func (s *Shipper) afterModeChangeLocked() {
	if s.maxLevelLocked() == Off {
		s.buf = nil
	}
}

func (s *Shipper) notify(changed bool) {
	if changed && s.onChange != nil {
		s.onChange()
	}
}

func (s *Shipper) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Shipper) levelFor(nodeID int) Level {
	if l, ok := s.overrides[nodeID]; ok {
		return l
	}
	return s.mode
}

// truncate cuts msg to at most max bytes without splitting a UTF-8 sequence.
func truncate(msg string, max int) string {
	if len(msg) <= max {
		return msg
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut]
}

// Add queues an entry if its level passes the mode. It never blocks: when the
// buffer is full the oldest entry is dropped.
func (s *Shipper) Add(level Level, e Entry) {
	s.mu.Lock()
	if level > s.levelFor(e.NodeID) {
		s.mu.Unlock()
		return
	}
	e.Level = level.String()
	e.Message = truncate(e.Message, MaxMessageSize)
	if len(s.buf) >= s.capacity {
		copy(s.buf, s.buf[1:])
		s.buf = s.buf[:len(s.buf)-1]
		s.dropped++
	}
	s.buf = append(s.buf, e)
	full := len(s.buf) >= FlushAtEntries
	s.mu.Unlock()
	if full {
		s.signal()
	}
}

// Buffered returns the number of queued entries and the number dropped so far.
func (s *Shipper) Buffered() (queued, dropped int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf), s.dropped
}

// peekLocked copies up to MaxBatch entries from the head of the buffer. They
// stay queued until the send succeeds (see Flush).
func (s *Shipper) peekLocked() []Entry {
	n := len(s.buf)
	if n > MaxBatch {
		n = MaxBatch
	}
	out := make([]Entry, n)
	copy(out, s.buf[:n])
	return out
}

// Flush sends buffered entries (or, in mode off, an empty poll when one is
// due). It returns the delay until the next flush should run.
func (s *Shipper) Flush(ctx context.Context) time.Duration {
	for batches := 0; batches < 10; batches++ {
		now := s.now()
		s.mu.Lock()
		if wait := s.notBefore.Sub(now); wait > 0 {
			s.mu.Unlock()
			return wait
		}
		off := s.maxLevelLocked() == Off
		if off && !s.lastPoll.IsZero() && now.Sub(s.lastPoll) < OffPollInterval {
			wait := OffPollInterval - now.Sub(s.lastPoll)
			s.mu.Unlock()
			return wait
		}
		var batch []Entry
		droppedAtPeek := s.dropped
		if !off {
			// drop entries the (possibly lowered) mode no longer wants
			kept := s.buf[:0]
			for _, e := range s.buf {
				lv, _ := ParseMode(e.Level)
				if lv <= s.levelFor(e.NodeID) && s.levelFor(e.NodeID) != Off {
					kept = append(kept, e)
				}
			}
			s.buf = kept
			batch = s.peekLocked()
		}
		s.mu.Unlock()

		if !off && len(batch) == 0 {
			// Nothing to ship: still poll once at start and then every minute so
			// a mode change made in the panel reaches an idle node too.
			s.mu.Lock()
			due := s.lastPoll.IsZero() || now.Sub(s.lastPoll) >= OffPollInterval
			wait := s.intervalLocked()
			if !due {
				if untilPoll := OffPollInterval - now.Sub(s.lastPoll); untilPoll < wait {
					wait = untilPoll
				}
			}
			s.mu.Unlock()
			if !due {
				return wait
			}
		}
		if batch == nil {
			batch = []Entry{}
		}
		resp, err := s.sender.SendLogs(ctx, batch)
		s.mu.Lock()
		s.lastPoll = now
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				s.notBefore = now.Add(NotFoundBackoff)
			} else {
				s.failures++
				backoff := time.Duration(1<<min(s.failures, 6)) * 5 * time.Second
				if backoff > 5*time.Minute {
					backoff = 5 * time.Minute
				}
				s.notBefore = now.Add(backoff)
			}
			wait := s.notBefore.Sub(now)
			s.mu.Unlock()
			return wait
		}
		s.failures = 0
		s.notBefore = time.Time{}
		// The batch was the head of the buffer; entries the ring dropped while
		// the request was in flight were part of it already.
		if n := len(batch) - (s.dropped - droppedAtPeek); n > 0 {
			if n > len(s.buf) {
				n = len(s.buf)
			}
			s.buf = append(s.buf[:0], s.buf[n:]...)
		}
		before := s.maxLevelLocked()
		s.applyResponseLocked(resp)
		after := s.maxLevelLocked()
		more := len(s.buf) > 0 && s.maxLevelLocked() != Off
		interval := s.intervalLocked()
		s.mu.Unlock()
		s.notify(before != after)
		if !more {
			if s.MaxLevel() == Off {
				return OffPollInterval
			}
			return interval
		}
	}
	return 0
}

func (s *Shipper) currentInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.intervalLocked()
}

func (s *Shipper) intervalLocked() time.Duration {
	if s.maxLevelLocked() == Off {
		return OffPollInterval
	}
	return s.interval
}

func (s *Shipper) applyResponseLocked(r Response) {
	if m, ok := ParseMode(r.LogMode); ok {
		// the panel's base mode; per-node overrides from WS events survive until
		// the panel's own mode changes
		if !s.haveServer || m != s.lastServer {
			s.mode = m
			s.overrides = make(map[int]Level)
		}
		s.lastServer, s.haveServer = m, true
		s.afterModeChangeLocked()
	}
	if r.FlushInterval > 0 {
		s.interval = time.Duration(r.FlushInterval) * time.Second
	}
}

// Run flushes until ctx is cancelled: every flush_interval seconds, as soon as
// FlushAtEntries are queued, and after a mode change.
func (s *Shipper) Run(ctx context.Context) {
	wait := time.Second // first poll shortly after start learns the panel's mode
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		wait = s.Flush(ctx)
		if wait <= 0 {
			wait = s.currentInterval()
		}
		timer.Reset(wait)
	}
}
