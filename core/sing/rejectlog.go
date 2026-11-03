package sing

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Connections that sing-box rejects before they reach the router are the ones
// an operator never sees at info: a Reality client with a wrong key or short id
// is, to sing-box, "REALITY: processed invalid connection" (and the utls
// library only explains why at trace level). FNode counts those lines per
// inbound and reason and logs one WARN per reason and inbound per minute, with
// the count and the remote addresses.
const (
	rejectEvery = time.Minute

	reasonRealityRejected = "reality-rejected"
	reasonFlowMismatch    = "flow-mismatch"
	reasonDestUnreachable = "dest-unreachable"
)

// rejectInfo is a classified rejected-connection line.
type rejectInfo struct {
	reason string
	ip     string
	detail string // reason specific text from the line
}

// classifyReject recognises the lines of rejected connections in the log of an
// inbound. It returns false for everything else.
func classifyReject(l logLine) (rejectInfo, bool) {
	if !strings.HasPrefix(l.Tag, "inbound/") {
		return rejectInfo{}, false
	}
	ip := ""
	if m := fromRe.FindStringSubmatch(l.Message); m != nil {
		ip = strings.Trim(m[1], "[]")
	}
	switch {
	case isRealityProbe(l):
		return rejectInfo{reason: reasonRealityRejected, ip: ip}, true
	case strings.Contains(l.Message, "REALITY: failed to dial dest"):
		detail := l.Message[strings.Index(l.Message, "REALITY: failed to dial dest")+len("REALITY: failed to dial dest: "):]
		return rejectInfo{reason: reasonDestUnreachable, ip: ip, detail: detail}, true
	case strings.Contains(l.Message, "flow mismatch: "):
		detail := l.Message[strings.Index(l.Message, "flow mismatch: ")+len("flow mismatch: "):]
		return rejectInfo{reason: reasonFlowMismatch, ip: ip, detail: detail}, true
	}
	return rejectInfo{}, false
}

type rejectState struct {
	lastEmit time.Time
	pending  int
	ips      map[string]int
	detail   string
}

// rejectLimiter turns rejected-connection lines into at most one WARN per
// (inbound, reason) per window.
type rejectLimiter struct {
	now   func() time.Time
	every time.Duration
	emit  func(inbound, reason string, count int, ips map[string]int, detail string)

	mu sync.Mutex
	st map[string]*rejectState
}

func newRejectLimiter() *rejectLimiter {
	return &rejectLimiter{now: time.Now, every: rejectEvery, emit: logReject, st: make(map[string]*rejectState)}
}

// Observe counts one rejected connection; the first one of a window is logged
// at once, the rest are summed up by Flush.
func (r *rejectLimiter) Observe(inbound string, ri rejectInfo) {
	key := inbound + "\x00" + ri.reason
	now := r.now()
	r.mu.Lock()
	s := r.st[key]
	if s == nil {
		s = &rejectState{ips: make(map[string]int)}
		r.st[key] = s
	}
	s.pending++
	if ri.ip != "" {
		s.ips[ri.ip]++
	}
	s.detail = ri.detail
	if !s.lastEmit.IsZero() && now.Sub(s.lastEmit) < r.every {
		r.mu.Unlock()
		return
	}
	count, ips, detail := s.take(now)
	r.mu.Unlock()
	r.emit(inbound, ri.reason, count, ips, detail)
}

func (s *rejectState) take(now time.Time) (int, map[string]int, string) {
	count, ips, detail := s.pending, s.ips, s.detail
	s.pending, s.ips, s.lastEmit = 0, make(map[string]int), now
	return count, ips, detail
}

// Flush logs what the window suppressed. force logs it even when the window
// has not ended yet (shutdown).
func (r *rejectLimiter) Flush(force bool) {
	type out struct {
		inbound, reason, detail string
		count                   int
		ips                     map[string]int
	}
	var due []out
	now := r.now()
	r.mu.Lock()
	for key, s := range r.st {
		if s.pending > 0 && (force || now.Sub(s.lastEmit) >= r.every) {
			inbound, reason, _ := strings.Cut(key, "\x00")
			count, ips, detail := s.take(now)
			due = append(due, out{inbound, reason, detail, count, ips})
		} else if s.pending == 0 && now.Sub(s.lastEmit) >= 10*r.every {
			delete(r.st, key) // quiet for a long time: forget it
		}
	}
	r.mu.Unlock()
	sort.Slice(due, func(i, j int) bool { return due[i].inbound+due[i].reason < due[j].inbound+due[j].reason })
	for _, d := range due {
		r.emit(d.inbound, d.reason, d.count, d.ips, d.detail)
	}
}

// logReject writes the WARN line.
func logReject(inbound, reason string, count int, ips map[string]int, detail string) {
	node := nodeTagOfLogTag(inbound)
	from := ""
	if len(ips) > 0 {
		from = " from " + topN(ips, 3)
	}
	var msg string
	switch reason {
	case reasonRealityRejected:
		msg = fmt.Sprintf("Reality rejected %d connection(s) on %s%s: no valid Reality handshake. A client with a wrong public key, short id or server name (SNI), a client clock that is off, or a scanner looks like this; sing-box reports no finer reason below log level trace", count, inbound, from)
	case reasonFlowMismatch:
		msg = fmt.Sprintf("VLESS flow mismatch %d time(s) on %s%s: %s. Set the client's flow to what the node expects (usually xtls-rprx-vision); sing-box does not say which user it was", count, inbound, from, detail)
	case reasonDestUnreachable:
		msg = fmt.Sprintf("Reality handshake destination cannot be reached from this server (%d connection(s) on %s%s, last error: %s). Every client of this node fails until the dest is reachable on its port", count, inbound, from, detail)
	default:
		msg = fmt.Sprintf("%d connection(s) rejected on %s%s: %s", count, inbound, from, reason)
	}
	fields := log.Fields{"inbound": inbound, "reason": reason, "count": count}
	if node != "" {
		fields["tag"] = node // routes the entry to that node's panel log
	}
	log.WithFields(fields).Warn(msg)
}
