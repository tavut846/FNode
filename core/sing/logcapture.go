package sing

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/conf"
)

// Verbosity of a log line: lower is more severe.
const (
	verbError = 0
	verbWarn  = 1
	verbInfo  = 2
	verbDebug = 3
	verbTrace = 4
)

// logLine is one parsed sing-box log line.
type logLine struct {
	Verbosity int
	Tag       string // e.g. inbound/vless[<node tag>]
	Message   string
	Raw       string // the line as sing-box wrote it, colours kept, no newline
}

var (
	ansiRe  = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	levelRe = regexp.MustCompile(`(?:^|\s)(TRACE|DEBUG|INFO|WARN|ERROR|FATAL|PANIC)(?:\[\d+\])?\s`)
	idRe    = regexp.MustCompile(`^\[\d+ [^\]]*\] `)
	fromRe  = regexp.MustCompile(`from (\[[0-9a-fA-F:.]+\]|[0-9a-fA-F:.]+):\d+`)
)

var tagPrefixes = []string{"inbound/", "outbound/", "endpoint/", "dns/", "service/"}

func verbosityOf(level string) int {
	switch strings.ToUpper(level) {
	case "PANIC", "FATAL", "ERROR":
		return verbError
	case "WARN":
		return verbWarn
	case "INFO":
		return verbInfo
	case "DEBUG":
		return verbDebug
	default:
		return verbTrace
	}
}

// parseLogLine parses a line written by sing-box's log formatter, e.g.
//
//	-0700 2026-10-19 10:00:00 ERROR [123 5ms] inbound/vless[tag]: message
//	ERROR[0042] router: message
func parseLogLine(raw string) (logLine, bool) {
	plain := ansiRe.ReplaceAllString(raw, "")
	loc := levelRe.FindStringSubmatchIndex(plain)
	if loc == nil {
		return logLine{Raw: raw}, false
	}
	line := logLine{Raw: raw, Verbosity: verbosityOf(plain[loc[2]:loc[3]])}
	rest := plain[loc[1]:]
	rest = idRe.ReplaceAllString(rest, "")
	line.Message = rest
	if i := strings.Index(rest, ": "); i > 0 {
		head := rest[:i]
		known := false
		for _, p := range tagPrefixes {
			if strings.HasPrefix(head, p) {
				known = true
				break
			}
		}
		if known || !strings.ContainsAny(head, " \t") {
			line.Tag = head
			line.Message = rest[i+2:]
		}
	}
	return line, true
}

// nodeTagOfLogTag extracts the node tag from an inbound logger tag:
// "inbound/vless[<node tag>]" -> "<node tag>". It returns "" for other tags.
func nodeTagOfLogTag(tag string) string {
	if !strings.HasPrefix(tag, "inbound/") {
		return ""
	}
	i := strings.IndexByte(tag, '[')
	if i < 0 || !strings.HasSuffix(tag, "]") {
		return ""
	}
	return tag[i+1 : len(tag)-1]
}

// isRealityProbe reports the line sing-box logs when a ClientHello fails
// REALITY authentication and is forwarded to the camouflage destination.
func isRealityProbe(l logLine) bool {
	return strings.Contains(l.Message, "REALITY: processed invalid connection")
}

func topN(m map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	list := make([]kv, 0, len(m))
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].k < list[j].k
	})
	if len(list) > n {
		list = list[:n]
	}
	parts := make([]string, len(list))
	for i, e := range list {
		parts[i] = fmt.Sprintf("%s x%d", e.k, e.v)
	}
	return strings.Join(parts, ", ")
}

// logCapture sits between sing-box's logger and its destination. sing-box
// writes its log to os.Stderr when the log output is "stderr"; the capture
// swaps os.Stderr for a pipe while the box is created, reads the lines, and
// forwards them to the configured destination (stderr, stdout or a file)
// filtered by the configured level. Rejected connections (REALITY probes,
// flow mismatches, an unreachable dest) are counted per inbound and reason and
// logged as one WARN per minute (rejectlog.go); the raw probe line is debug.
type logCapture struct {
	pr, pw      *os.File
	console     io.Writer
	consoleFile *os.File
	stripColor  bool
	consoleVerb int // lines above this verbosity are not printed; -1 prints nothing
	rejects     *rejectLimiter
	probeEvery  time.Duration // how often suppressed rejects are summed up
	// onLine receives every parsed line (after probe classification), set
	// before the box starts.
	onLine func(l logLine, probe bool)

	done    chan struct{}
	stop    chan struct{}
	once    sync.Once
	started bool
}

var stderrSwap sync.Mutex

func levelToVerbosity(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "panic", "fatal", "error":
		return verbError
	case "warn", "warning":
		return verbWarn
	case "info":
		return verbInfo
	case "debug":
		return verbDebug
	default:
		return verbTrace
	}
}

// newLogCapture prepares a capture for the core's log settings and rewrites
// the settings so sing-box logs to "stderr" (which install() redirects).
func newLogCapture(lc *conf.SingLogConfig) (*logCapture, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c := &logCapture{
		pr:          pr,
		pw:          pw,
		consoleVerb: levelToVerbosity(lc.Level),
		rejects:     newRejectLimiter(),
		probeEvery:  15 * time.Second,
		done:        make(chan struct{}),
		stop:        make(chan struct{}),
	}
	switch lc.Output {
	case "", "stderr":
		c.console = os.Stderr
	case "stdout":
		c.console = os.Stdout
	default:
		f, err := os.OpenFile(lc.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			_ = pr.Close()
			_ = pw.Close()
			return nil, fmt.Errorf("open core log file: %w", err)
		}
		c.console = f
		c.consoleFile = f
		c.stripColor = true
	}
	if lc.Disabled {
		c.consoleVerb = -1
	}
	return c, nil
}

// install redirects os.Stderr to the capture pipe; call the returned function
// as soon as box.New has returned (sing-box keeps the pipe it was given).
func (c *logCapture) install() (restore func()) {
	stderrSwap.Lock()
	orig := os.Stderr
	os.Stderr = c.pw
	return func() {
		os.Stderr = orig
		stderrSwap.Unlock()
	}
}

func (c *logCapture) start() {
	c.started = true
	go c.readLoop()
	go c.probeLoop()
}

func (c *logCapture) print(l logLine) {
	if l.Verbosity > c.consoleVerb {
		return
	}
	text := l.Raw
	if c.stripColor {
		text = ansiRe.ReplaceAllString(text, "")
	}
	_, _ = io.WriteString(c.console, text+"\n")
}

func (c *logCapture) readLoop() {
	defer close(c.done)
	r := bufio.NewReaderSize(c.pr, 64<<10)
	for {
		raw, err := r.ReadString('\n')
		raw = strings.TrimRight(raw, "\r\n")
		if raw != "" {
			c.handle(raw)
		}
		if err != nil {
			return
		}
	}
}

func (c *logCapture) handle(raw string) {
	l, ok := parseLogLine(raw)
	if !ok {
		// continuation lines and anything unrecognised: print as is
		if c.consoleVerb >= verbError {
			_, _ = io.WriteString(c.console, raw+"\n")
		}
		return
	}
	probe := isRealityProbe(l)
	if ri, ok := classifyReject(l); ok {
		c.rejects.Observe(l.Tag, ri)
	}
	if probe {
		// the WARN comes from the rejectLimiter; the raw line stays at debug
		if log.IsLevelEnabled(log.DebugLevel) {
			log.WithField(logship.SkipField, true).Debug(l.Tag + ": " + l.Message)
		}
	} else {
		c.print(l)
	}
	if c.onLine != nil {
		c.onLine(l, probe)
	}
}

func (c *logCapture) probeLoop() {
	t := time.NewTicker(c.probeEvery)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.flushProbes()
		}
	}
}

func (c *logCapture) flushProbes() { c.rejects.Flush(false) }

// Close stops the capture after sing-box has closed its logger.
func (c *logCapture) Close() {
	c.once.Do(func() {
		close(c.stop)
		_ = c.pw.Close()
		if c.started {
			<-c.done
		}
		_ = c.pr.Close()
		c.rejects.Flush(true)
		if c.consoleFile != nil {
			_ = c.consoleFile.Close()
		}
	})
}

// shipToPanel feeds a parsed sing-box line to the panel log shippers. Probe
// rejections ship as debug entries; the rate-limited WARN ships at warn.
func shipToPanel(l logLine, probe bool) {
	level := logship.Level(min(l.Verbosity, int(logship.Debug)))
	if probe {
		level = logship.Debug
	}
	logship.Default.Emit(level, nodeTagOfLogTag(l.Tag), l.Tag, l.Message, time.Now())
}
