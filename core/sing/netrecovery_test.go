package sing

import (
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus/hooks/test"

	"github.com/tavut846/FNode/conf"
)

// fakeRecovery is a netRecovery with a fake clock and counting hooks that run
// synchronously.
type fakeRecovery struct {
	*netRecovery
	t        time.Time
	resets   int
	restarts int
}

func newFakeRecovery(mode string) *fakeRecovery {
	f := &fakeRecovery{t: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)}
	f.netRecovery = newNetRecovery(mode, func() { f.resets++ })
	f.now = func() time.Time { return f.t }
	f.run = func(fn func()) { fn() }
	f.SetRestartHook(func() { f.restarts++ })
	return f
}

func (f *fakeRecovery) advance(d time.Duration) { f.t = f.t.Add(d) }

// storm records n failures spread over about a second.
func (f *fakeRecovery) storm(n int) {
	for i := 0; i < n; i++ {
		f.ObserveDNSFailure("lookup x.example: read udp 10.0.0.2:1->1.1.1.1:53: use of closed network connection")
		f.advance(50 * time.Millisecond)
	}
}

// The lines from the journal that started this: a dead DNS socket makes every
// lookup fail with a closed-connection error.
func TestIsDNSFailureLine(t *testing.T) {
	for raw, want := range map[string]bool{
		"+0800 2026-10-24 09:02:48 ERROR [4231 0.6s] connection: open connection to chatgpt.com:443 using outbound/direct[direct]: lookup chatgpt.com: read udp 10.106.99.18:33547->1.1.1.1:53: use of closed network connection": true,
		"+0800 2026-10-24 09:02:49 ERROR [4232 33s] connection: open connection to api.openai.com:443 using outbound/direct[direct]: lookup api.openai.com: file already closed":                                     true,
		"+0800 2026-10-24 09:02:50 ERROR [4233 5s] connection: open connection to x.example:443 using outbound/direct[direct]: lookup x.example on 1.1.1.1:53: i/o timeout":                                          true,
		// the DNS server answered: not a transport failure
		"+0800 2026-10-24 09:02:51 ERROR [4234 10ms] connection: open connection to nope.invalid:443 using outbound/direct[direct]: lookup nope.invalid: no such host": false,
		// not a lookup
		"+0800 2026-10-24 09:02:52 ERROR [4235 5s] connection: open connection to 1.2.3.4:443 using outbound/direct[direct]: dial tcp 1.2.3.4:443: i/o timeout": false,
		// below warning
		"+0800 2026-10-24 09:02:53 INFO [4236 0ms] router: lookup x.example: use of closed network connection": false,
	} {
		l, ok := parseLogLine(raw)
		if !ok {
			t.Fatalf("cannot parse %q", raw)
		}
		if got := isDNSFailureLine(l); got != want {
			t.Errorf("isDNSFailureLine(%q) = %v, want %v", raw, got, want)
		}
	}
}

// A burst of failed lookups resets the network once; a few do not.
func TestNetRecovery_FailureThresholdAndWindow(t *testing.T) {
	f := newFakeRecovery(recoveryReset)
	f.storm(dnsFailureThreshold - 1)
	if f.resets != 0 {
		t.Fatalf("%d failures must not reset (threshold %d)", dnsFailureThreshold-1, dnsFailureThreshold)
	}
	f.storm(1)
	if f.resets != 1 {
		t.Fatalf("the %dth failure within the window must reset once, got %d", dnsFailureThreshold, f.resets)
	}

	// failures spread over more than the window never add up
	g := newFakeRecovery(recoveryReset)
	for i := 0; i < 200; i++ {
		g.ObserveDNSFailure("lookup x: i/o timeout")
		g.advance(dnsFailureWindow / 5) // 5 per window
	}
	if g.resets != 0 {
		t.Fatalf("a trickle of failures must not reset, got %d", g.resets)
	}
}

// After a reset the next one waits for the cooldown.
func TestNetRecovery_Cooldown(t *testing.T) {
	f := newFakeRecovery(recoveryReset)
	f.storm(dnsFailureThreshold)
	f.storm(dnsFailureThreshold) // right after: inside the cooldown
	if f.resets != 1 {
		t.Fatalf("a second storm inside the cooldown must not reset again: %d", f.resets)
	}
	f.advance(resetCooldown)
	f.storm(dnsFailureThreshold)
	if f.resets != 2 {
		t.Fatalf("after the cooldown a new storm resets again: %d", f.resets)
	}
}

// chain runs the storms of one incident: two resets, then the escalation.
func (f *fakeRecovery) chain() {
	for i := 0; i < maxUnhelpfulResets+1; i++ {
		f.storm(dnsFailureThreshold)
		f.advance(resetCooldown)
	}
}

// Two resets that did not help: stop resetting and say why. Only "restart"
// mode restarts the core, and it waits 5 minutes, then 10, between restarts.
func TestNetRecovery_Escalation(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	f := newFakeRecovery(recoveryReset)
	f.chain()
	if f.resets != maxUnhelpfulResets {
		t.Fatalf("resets: %d, want %d before giving up", f.resets, maxUnhelpfulResets)
	}
	var errLogged bool
	for _, e := range hook.AllEntries() {
		if e.Level.String() == "error" && strings.Contains(e.Message, "keep failing after network resets") && strings.Contains(e.Message, "sing_origin.json") {
			errLogged = true
		}
	}
	if !errLogged {
		t.Errorf("no error explaining the DNS server: %+v", hook.AllEntries())
	}
	// still failing: no more resets while the failures continue, never a restart
	f.chain()
	if f.resets != maxUnhelpfulResets || f.restarts != 0 {
		t.Errorf("reset mode while failing: resets=%d restarts=%d", f.resets, f.restarts)
	}
}

func TestNetRecovery_RestartBackoff(t *testing.T) {
	f := newFakeRecovery(recoveryRestart)
	f.chain()
	if f.resets != maxUnhelpfulResets || f.restarts != 1 {
		t.Fatalf("first incident: resets=%d restarts=%d, want %d and 1", f.resets, f.restarts, maxUnhelpfulResets)
	}
	// the restarted core fails the same way: resets again, but no second restart within 5 minutes
	f.chain()
	if f.restarts != 1 || f.resets != 2*maxUnhelpfulResets {
		t.Fatalf("inside 5 minutes: resets=%d restarts=%d", f.resets, f.restarts)
	}
	f.advance(restartCooldown)
	f.chain()
	if f.restarts != 2 {
		t.Fatalf("after 5 minutes a second restart is allowed: %d", f.restarts)
	}
	// the wait doubles
	f.advance(restartCooldown)
	f.chain()
	if f.restarts != 2 {
		t.Fatalf("the third restart needs 10 minutes: %d", f.restarts)
	}
	f.advance(2 * restartCooldown)
	f.chain()
	if f.restarts != 3 {
		t.Fatalf("after 10 minutes the third restart is allowed: %d", f.restarts)
	}
	// a quiet spell makes the next incident a new one: back to 5 minutes
	f.advance(newIncidentAfter + time.Minute)
	f.chain()
	f.advance(restartCooldown)
	f.chain()
	if f.restarts != 5 {
		t.Fatalf("a new incident restarts on the short wait again: %d", f.restarts)
	}
}

// A storm long after the last one is a new problem, not a failed reset.
func TestNetRecovery_RecoveredThenFailsAgain(t *testing.T) {
	f := newFakeRecovery(recoveryRestart)
	f.storm(dnsFailureThreshold)
	f.advance(resetHelpedWithin + time.Second)
	f.storm(dnsFailureThreshold)
	f.advance(resetHelpedWithin + time.Second)
	f.storm(dnsFailureThreshold)
	if f.resets != 3 || f.restarts != 0 {
		t.Fatalf("separate incidents each get a reset: resets=%d restarts=%d", f.resets, f.restarts)
	}
}

// Whatever happens, an hour of nonstop failures is a bounded number of
// actions: it cannot loop.
func TestNetRecovery_CannotLoop(t *testing.T) {
	for _, mode := range []string{recoveryReset, recoveryRestart} {
		f := newFakeRecovery(mode)
		for i := 0; i < 3600*20; i++ { // 20 failures a second for an hour
			f.ObserveDNSFailure("lookup x: i/o timeout")
			f.advance(50 * time.Millisecond)
		}
		if mode == recoveryReset && (f.resets != maxUnhelpfulResets || f.restarts != 0) {
			t.Errorf("reset mode: resets=%d restarts=%d, want %d and 0", f.resets, f.restarts, maxUnhelpfulResets)
		}
		if mode == recoveryRestart && (f.restarts > 5 || f.resets > maxUnhelpfulResets*(f.restarts+1)) {
			t.Errorf("restart mode in an hour: resets=%d restarts=%d", f.resets, f.restarts)
		}
	}
}

func TestNetRecovery_AddressChange(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	f := newFakeRecovery(recoveryReset)

	if d := f.CheckAddresses("eth0 10.0.0.5"); d != netPollInterval || f.resets != 0 {
		t.Fatalf("first reading: next=%v resets=%d", d, f.resets)
	}
	f.CheckAddresses("eth0 10.0.0.5")
	f.CheckAddresses("") // no route for a moment: not a change
	if f.resets != 0 {
		t.Fatalf("unchanged or missing readings reset: %d", f.resets)
	}

	// a change is confirmed once, debounce later, before the reset
	if d := f.CheckAddresses("eth0 10.0.0.9"); d != netDebounce || f.resets != 0 {
		t.Fatalf("first sight of a new address: next=%v resets=%d", d, f.resets)
	}
	if d := f.CheckAddresses("eth0 10.0.0.9"); d != netPollInterval || f.resets != 1 {
		t.Fatalf("confirmed change: next=%v resets=%d", d, f.resets)
	}
	var warned bool
	for _, e := range hook.AllEntries() {
		if e.Level.String() == "warning" && strings.Contains(e.Message, "Network address changed") && e.Data["old"] == "eth0 10.0.0.5" && e.Data["new"] == "eth0 10.0.0.9" {
			warned = true
		}
	}
	if !warned {
		t.Errorf("no warning with the old and new address: %+v", hook.AllEntries())
	}

	// a blip that goes back is not a change
	f.CheckAddresses("eth0 10.0.0.77")
	f.CheckAddresses("eth0 10.0.0.9")
	f.CheckAddresses("eth0 10.0.0.9")
	if f.resets != 1 {
		t.Fatalf("a flap must not reset: %d", f.resets)
	}
}

func TestNetRecovery_OffDoesNothing(t *testing.T) {
	f := newFakeRecovery(recoveryOff)
	f.storm(dnsFailureThreshold * 3)
	f.CheckAddresses("a 1.1.1.1")
	f.CheckAddresses("a 2.2.2.2")
	f.CheckAddresses("a 2.2.2.2")
	if f.resets != 0 || f.restarts != 0 {
		t.Fatalf("off: resets=%d restarts=%d", f.resets, f.restarts)
	}
}

func TestNormalizeRecoveryMode(t *testing.T) {
	for in, want := range map[string]struct {
		mode  string
		known bool
	}{
		"": {recoveryReset, true}, "reset": {recoveryReset, true}, " RESTART ": {recoveryRestart, true},
		"off": {recoveryOff, true}, "none": {recoveryOff, true}, "bogus": {recoveryReset, false},
	} {
		if m, k := normalizeRecoveryMode(in); m != want.mode || k != want.known {
			t.Errorf("%q -> %q,%v want %q,%v", in, m, k, want.mode, want.known)
		}
	}
}

// The default-route reading never panics and has the documented shape.
func TestDefaultRouteFingerprint(t *testing.T) {
	fp := defaultRouteFingerprint()
	if fp == "" {
		t.Skip("no default route on this machine")
	}
	fields := strings.Fields(fp)
	if len(fields) == 0 || len(fields) > 2 || !strings.Contains(fields[len(fields)-1], ".") {
		t.Errorf("unexpected fingerprint %q", fp)
	}
}

// End to end through the real log path: failing lookup lines that sing-box
// writes reach the watchdog and reset the network.
func TestSing_LogLinesReachRecovery(t *testing.T) {
	b := newTestSing(t, nil)
	var resets int
	b.recovery.reset = func() { resets++ }
	b.recovery.run = func(fn func()) { fn() }
	line := "+0800 2026-10-24 09:02:48 ERROR [4231 0.6s] connection: open connection to chatgpt.com:443 using outbound/direct[direct]: lookup chatgpt.com: read udp 10.106.99.18:33547->1.1.1.1:53: use of closed network connection"
	for i := 0; i < dnsFailureThreshold; i++ {
		b.capture.handle(line)
	}
	if resets != 1 {
		t.Fatalf("resets: %d, want 1", resets)
	}
	// ordinary traffic errors do not count
	for i := 0; i < dnsFailureThreshold*3; i++ {
		b.capture.handle("+0800 2026-10-24 09:05:00 ERROR [1 0.1s] connection: open connection to nope.invalid:443 using outbound/direct[direct]: lookup nope.invalid: no such host")
	}
	if resets != 1 {
		t.Fatalf("'no such host' must not reset: %d", resets)
	}
}

func TestSing_NetworkRecoveryConfig(t *testing.T) {
	for in, want := range map[string]string{"": recoveryReset, "restart": recoveryRestart, "off": recoveryOff, "nonsense": recoveryReset} {
		b := newTestSingWith(t, func(c *conf.SingConfig) { c.NetworkRecovery = in })
		if b.recovery.mode != want {
			t.Errorf("NetworkRecovery %q -> mode %q, want %q", in, b.recovery.mode, want)
		}
	}
	// the restart hook is stored for "restart" mode
	b := newTestSingWith(t, func(c *conf.SingConfig) { c.NetworkRecovery = "restart" })
	called := false
	b.SetRestartHook(func() { called = true })
	b.recovery.mu.Lock()
	h := b.recovery.restartHook
	b.recovery.mu.Unlock()
	if h == nil {
		t.Fatal("SetRestartHook did not store the hook")
	}
	h()
	if !called {
		t.Error("stored hook is not the one set")
	}
}

// The real thing: a started core accepts ResetNetwork, the watcher runs, and
// Close stops it.
func TestSing_RecoveryLifecycleWithRealRouter(t *testing.T) {
	b := newTestSing(t, nil)
	if err := b.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if b.stopRecovery == nil {
		t.Fatal("the watcher did not start")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.recovery.reset() // the real Router.ResetNetwork
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ResetNetwork hangs on a started core")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// resetting after Close must not panic either
	b.recovery.reset()
}
