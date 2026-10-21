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
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sirupsen/logrus"
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

// Response is the panel's answer: {"data":{"log_mode","accepted","flush_interval","node_modes"}}.
type Response struct {
	LogMode       string
	Accepted      int
	FlushInterval int
	// NodeModes are per-node log modes inside a machine (node id -> mode).
	// When the panel sends the field it replaces every per-node override.
	NodeModes map[int]string
	// HasNodeModes tells an empty NodeModes ("no overrides") from an old
	// panel that does not send the field.
	HasNodeModes bool
}

// Meta is sent with every log push besides the entries.
type Meta struct {
	// Missing lists, per node id, outbound tags the node's routes use but
	// FNode does not have; the CustomNode plugin adds them to the node.
	Missing map[int][]string
	// Dropped counts entries lost to a full buffer since the last push.
	Dropped int
}

// MetaSender is a Sender that can also carry Meta (the panel client).
type MetaSender interface {
	SendLogsMeta(ctx context.Context, entries []Entry, meta Meta) (Response, error)
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
	// missing is the outbound request per node id; missingDirty forces a
	// push so the panel learns about it without waiting for the next poll.
	missing      map[int][]string
	missingDirty bool
	// sentDropped is the dropped count already reported to the panel.
	sentDropped int
	// errLogged avoids repeating the same push failure in the local log.
	errLogged bool

	wake     chan struct{}
	onChange func()
}

// DefaultMode is the log mode a shipper uses until the panel sends its own.
const DefaultMode = Info

// NewShipper creates a shipper in the default mode (info) with a flush
// interval of 10 seconds until the panel says otherwise.
func NewShipper(sender Sender) *Shipper {
	return &Shipper{
		sender:    sender,
		now:       time.Now,
		capacity:  DefaultBuffer,
		mode:      DefaultMode,
		missing:   make(map[int][]string),
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

// SetMissing records the outbound tags node nodeID needs; an empty list
// clears it. A change triggers a push.
func (s *Shipper) SetMissing(nodeID int, tags []string) {
	tags = append([]string(nil), tags...)
	sort.Strings(tags)
	s.mu.Lock()
	old := s.missing[nodeID]
	same := len(old) == len(tags)
	if same {
		for i := range old {
			if old[i] != tags[i] {
				same = false
				break
			}
		}
	}
	if same {
		s.mu.Unlock()
		return
	}
	if len(tags) == 0 {
		delete(s.missing, nodeID)
	} else {
		s.missing[nodeID] = tags
	}
	s.missingDirty = true
	s.mu.Unlock()
	s.signal()
}

// Missing returns a copy of the outbound requests per node id.
func (s *Shipper) Missing() map[int][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.missingCopyLocked()
}

func (s *Shipper) missingCopyLocked() map[int][]string {
	out := make(map[int][]string, len(s.missing))
	for id, tags := range s.missing {
		out[id] = append([]string(nil), tags...)
	}
	return out
}

// send posts a batch, with Meta when the sender supports it.
func (s *Shipper) send(ctx context.Context, batch []Entry, meta Meta) (Response, error) {
	if ms, ok := s.sender.(MetaSender); ok {
		return ms.SendLogsMeta(ctx, batch, meta)
	}
	return s.sender.SendLogs(ctx, batch)
}

// logPushError reports a failed push once in the local log (never shipped).
func (s *Shipper) logPushError(err error) {
	s.mu.Lock()
	logged := s.errLogged
	s.errLogged = true
	s.mu.Unlock()
	if logged {
		return
	}
	entry := logrus.WithField(SkipField, true).WithField("err", err)
	if errors.Is(err, ErrNotFound) {
		entry.Warn("Panel log endpoint not found (HTTP 404): install or update the CustomNode plugin to see FNode logs in the dashboard")
		return
	}
	entry.Warn("Sending logs to the panel failed, retrying with backoff")
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
			due := s.lastPoll.IsZero() || now.Sub(s.lastPoll) >= OffPollInterval || s.missingDirty
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
		s.mu.Lock()
		meta := Meta{Missing: s.missingCopyLocked(), Dropped: s.dropped - s.sentDropped}
		dropped := s.dropped
		s.missingDirty = false
		s.mu.Unlock()
		resp, err := s.send(ctx, batch, meta)
		if err != nil {
			s.logPushError(err)
		} else {
			s.mu.Lock()
			recovered := s.errLogged
			s.errLogged = false
			s.mu.Unlock()
			if recovered {
				logrus.WithField(SkipField, true).Info("Sending logs to the panel works again")
			}
		}
		s.mu.Lock()
		s.lastPoll = now
		if err != nil {
			if len(meta.Missing) > 0 {
				s.missingDirty = true
			}
			if errors.Is(err, ErrNotFound) {
				s.notBefore = now.Add(NotFoundBackoff)
			} else {
				s.failures++
				backoff := time.Duration(1<<min(s.failures, 6)) * 5 * time.Second
				if backoff > 2*time.Minute {
					backoff = 2 * time.Minute
				}
				s.notBefore = now.Add(backoff)
			}
			wait := s.notBefore.Sub(now)
			s.mu.Unlock()
			return wait
		}
		s.failures = 0
		s.notBefore = time.Time{}
		s.sentDropped = dropped
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
	}
	if r.HasNodeModes {
		// The panel's per-node modes are authoritative
		s.overrides = make(map[int]Level, len(r.NodeModes))
		for id, mode := range r.NodeModes {
			if l, ok := ParseMode(mode); ok && id > 0 {
				s.overrides[id] = l
			}
		}
	}
	s.afterModeChangeLocked()
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
