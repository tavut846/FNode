package sing

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sblog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/conf"
)

func TestParseLogLine(t *testing.T) {
	cases := []struct {
		raw   string
		verb  int
		tag   string
		msg   string
		probe bool
	}{
		{"-0700 2026-10-19 10:00:00 ERROR [123 5ms] inbound/vless[[https://p]-vless:493]: process connection from 52.36.5.236:40000: TLS handshake: REALITY: processed invalid connection",
			verbError, "inbound/vless[[https://p]-vless:493]", "process connection from 52.36.5.236:40000: TLS handshake: REALITY: processed invalid connection", true},
		{"\x1b[31mERROR\x1b[0m[0042] router: outbound not found: x", verbError, "router", "outbound not found: x", false},
		{"-0700 2026-10-19 10:00:00 WARN outbound/socks[us]: dial failed", verbWarn, "outbound/socks[us]", "dial failed", false},
		{"INFO[0001] started", verbInfo, "", "started", false},
	}
	for _, c := range cases {
		l, ok := parseLogLine(c.raw)
		if !ok || l.Verbosity != c.verb || l.Tag != c.tag || l.Message != c.msg || isRealityProbe(l) != c.probe {
			t.Errorf("%q => ok=%v %+v probe=%v", c.raw, ok, l, isRealityProbe(l))
		}
	}
	if _, ok := parseLogLine("goroutine 1 [running]:"); ok {
		t.Error("a stack trace line is not a log line")
	}
	if got := nodeTagOfLogTag("inbound/vless[[https://p]-vless:493]"); got != "[https://p]-vless:493" {
		t.Errorf("node tag: %q", got)
	}
	if nodeTagOfLogTag("router") != "" || nodeTagOfLogTag("outbound/socks[x]") != "" {
		t.Error("only inbound tags map to a node")
	}
}

// End to end: a real sing-box logger writing to "stderr" while the capture is
// installed. Probe lines become a rate-limited WARN, other lines reach the console.
func TestLogCapture_EndToEnd(t *testing.T) {
	var out bytes.Buffer
	c, err := newLogCapture(&conf.SingLogConfig{Level: "info", Timestamp: true})
	if err != nil {
		t.Fatal(err)
	}
	c.console = &out
	c.probeEvery = time.Hour
	var mu sync.Mutex
	var seen []string
	c.onLine = func(l logLine, probe bool) {
		mu.Lock()
		seen = append(seen, l.Message)
		mu.Unlock()
	}
	restore := c.install()
	factory, err := sblog.New(sblog.Options{Context: context.Background(),
		Options: option.LogOptions{Level: "info", Output: "stderr", Timestamp: true}})
	restore()
	if err != nil {
		t.Fatal(err)
	}
	if os.Stderr == c.pw {
		t.Fatal("os.Stderr must be restored")
	}
	c.start()

	prevLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.InfoLevel)
	defer logrus.SetLevel(prevLevel)
	var summary bytes.Buffer
	prevOut := logrus.StandardLogger().Out
	logrus.SetOutput(&summary)
	defer logrus.SetOutput(prevOut)

	lg := factory.NewLogger("inbound/vless[node-1]")
	lg.Error("process connection from 52.36.5.236:40000: TLS handshake: REALITY: processed invalid connection")
	lg.Error("process connection from 52.36.5.236:40001: TLS handshake: REALITY: processed invalid connection")
	lg.Warn("something else")
	lg.Debug("below the level")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = factory.Close()
	c.Close()

	console := out.String()
	if strings.Contains(console, "REALITY") {
		t.Errorf("probe lines must not reach the console:\n%s", console)
	}
	if !strings.Contains(console, "something else") || strings.Contains(console, "below the level") {
		t.Errorf("console:\n%s", console)
	}
	// the first reject warns at once, the second is summed up on close
	got := summary.String()
	if strings.Count(got, "Reality rejected 1 connection(s)") != 2 || !strings.Contains(got, "52.36.5.236 x1") {
		t.Errorf("the first reject is a WARN with its address:\n%s", got)
	}
	if !strings.Contains(got, "level=warning") || !strings.Contains(got, "Reality rejected 1 connection(s)") {
		t.Errorf("rejects are logged at warn:\n%s", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Errorf("sinks see every emitted line: %v", seen)
	}
}

// A box that fails to build must not leave the capture hanging.
func TestLogCapture_CloseWithoutStart(t *testing.T) {
	c, err := newLogCapture(&conf.SingLogConfig{Level: "error"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close hangs when the capture was never started")
	}
}
