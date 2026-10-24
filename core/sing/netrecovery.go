package sing

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Network recovery. sing-box keeps one UDP socket per DNS server. When the
// VPS provider moves the public IP (a NAT mapping behind the instance, or the
// address itself) the old socket keeps "working": its replies never arrive,
// every lookup waits until its context ends, and nothing in sing-box resets
// the transport. Router.ResetNetwork() closes those sockets and clears the
// DNS cache; the next lookup dials a new socket. FNode calls it when
//   - the default route's local address changes (polled every 10 s), or
//   - many outbound DNS lookups fail within a few seconds (read from the
//     sing-box log), which is the only signal when only the public IP changed
//     behind a NAT and the local address did not.
//
// Cores[].NetworkRecovery: "reset" (default), "restart" (also restarts the
// core through the config-reload path when resets do not help, at most once
// per 5 minutes) or "off".
const (
	recoveryReset   = "reset"
	recoveryRestart = "restart"
	recoveryOff     = "off"

	netPollInterval     = 10 * time.Second
	netDebounce         = 3 * time.Second
	dnsFailureThreshold = 20
	dnsFailureWindow    = 10 * time.Second
	resetCooldown       = 30 * time.Second
	// failures that come back this soon after a reset mean the reset did not help
	resetHelpedWithin = 2 * time.Minute
	// after this many resets in a row that did not help
	maxUnhelpfulResets = 2
	// restarts are disruptive: the first needs 5 minutes since the last, then
	// the wait doubles up to an hour while the network stays broken, and goes
	// back to 5 minutes after 10 quiet minutes
	restartCooldown    = 5 * time.Minute
	maxRestartDelay    = time.Hour
	newIncidentAfter   = 10 * time.Minute
	errorLogCooldown   = 5 * time.Minute
)

// normalizeRecoveryMode maps the config value to a known mode.
func normalizeRecoveryMode(m string) (mode string, known bool) {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "", recoveryReset:
		return recoveryReset, true
	case recoveryRestart:
		return recoveryRestart, true
	case recoveryOff, "none", "false", "disabled":
		return recoveryOff, true
	}
	return recoveryReset, false
}

// netRecovery decides when to reset the network. All decisions use now()
// and the hooks, so tests drive it with a fake clock.
type netRecovery struct {
	mode string
	now  func() time.Time
	// reset closes the DNS sockets and clears the DNS cache (Router.ResetNetwork)
	reset func()
	// run executes a hook; production runs it in a goroutine so a hook never
	// blocks the log reader that triggered it
	run func(func())

	mu          sync.Mutex
	restartHook func()

	fails        []time.Time
	lastError    string
	fp           string // last confirmed fingerprint of the default route
	pendingFP    string // a different fingerprint seen once, waiting for confirmation
	lastReset    time.Time
	lastTrigger  time.Time
	lastStorm    time.Time
	unhelpful    int
	nextRestart  time.Time
	restartDelay time.Duration
	lastErrorLog time.Time
}

func newNetRecovery(mode string, reset func()) *netRecovery {
	return &netRecovery{
		mode:  mode,
		now:   time.Now,
		reset: reset,
		run:   func(f func()) { go f() },
	}
}

// SetRestartHook sets what "restart" mode calls to restart the core.
func (r *netRecovery) SetRestartHook(f func()) {
	r.mu.Lock()
	r.restartHook = f
	r.mu.Unlock()
}

// ObserveLine counts an outbound DNS failure line.
func (r *netRecovery) ObserveLine(l logLine) {
	if r.mode == recoveryOff || !isDNSFailureLine(l) {
		return
	}
	r.ObserveDNSFailure(l.Message)
}

// dnsFailureMarkers are the errors a dead DNS socket produces.
var dnsFailureMarkers = []string{
	"use of closed network connection",
	"file already closed",
	"i/o timeout",
	"context deadline exceeded",
	"network is unreachable",
	"connection reset",
}

// isDNSFailureLine reports an outbound lookup that failed because the DNS
// transport is broken: "lookup <name>: read udp ...: use of closed network
// connection". "no such host" and similar answers are not failures of the
// transport and are not counted.
func isDNSFailureLine(l logLine) bool {
	if l.Verbosity > verbWarn {
		return false
	}
	m := l.Message
	if !strings.Contains(m, "lookup ") && !strings.Contains(m, "dns: ") {
		return false
	}
	for _, marker := range dnsFailureMarkers {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

// ObserveDNSFailure records one failed lookup and acts when too many failed
// within the window.
func (r *netRecovery) ObserveDNSFailure(msg string) {
	if r.mode == recoveryOff {
		return
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := now.Add(-dnsFailureWindow)
	kept := r.fails[:0]
	for _, t := range r.fails {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	r.fails = append(kept, now)
	r.lastError = msg
	if len(r.fails) < dnsFailureThreshold {
		return
	}
	if !r.lastReset.IsZero() && now.Sub(r.lastReset) < resetCooldown {
		// a reset just ran: let it work before judging again
		r.fails = r.fails[:0]
		return
	}
	r.fails = r.fails[:0]
	r.onFailureStorm(now)
}

// onFailureStorm runs with r.mu held.
func (r *netRecovery) onFailureStorm(now time.Time) {
	if !r.lastStorm.IsZero() && now.Sub(r.lastStorm) > newIncidentAfter {
		r.restartDelay = 0
	}
	r.lastStorm = now
	if !r.lastTrigger.IsZero() && now.Sub(r.lastTrigger) < resetHelpedWithin {
		r.unhelpful++
	} else {
		r.unhelpful = 0
	}
	r.lastTrigger = now
	if r.unhelpful >= maxUnhelpfulResets {
		r.escalate(now)
		return
	}
	log.WithFields(log.Fields{"failures": dnsFailureThreshold, "window": dnsFailureWindow.String(), "last_error": r.lastError}).
		Warn("Outbound DNS lookups are failing, resetting the network (DNS sockets and cache)")
	r.doReset(now)
}

// escalate runs with r.mu held: resets did not help.
func (r *netRecovery) escalate(now time.Time) {
	if r.lastErrorLog.IsZero() || now.Sub(r.lastErrorLog) >= errorLogCooldown {
		r.lastErrorLog = now
		log.WithField("last_error", r.lastError).
			Error("Outbound DNS lookups keep failing after network resets. Check that this server can reach its DNS server (the one in sing_origin.json dns.servers, or the system resolver), that the provider's firewall/NAT allows UDP 53 out, and the VPS's network settings; set Cores[].NetworkRecovery to \"restart\" to let FNode restart the core")
	}
	if r.mode != recoveryRestart || r.restartHook == nil {
		return
	}
	if now.Before(r.nextRestart) {
		return
	}
	delay := r.restartDelay
	if delay == 0 {
		delay = restartCooldown
	}
	r.nextRestart = now.Add(delay)
	r.restartDelay = delay * 2
	if r.restartDelay > maxRestartDelay {
		r.restartDelay = maxRestartDelay
	}
	// the restarted core starts a fresh chain of resets
	r.unhelpful, r.lastTrigger = 0, time.Time{}
	hook := r.restartHook
	log.Warn("Restarting the core to recover the network (NetworkRecovery: restart)")
	r.run(hook)
}

// doReset runs with r.mu held.
func (r *netRecovery) doReset(now time.Time) {
	r.lastReset = now
	r.run(r.reset)
}

// CheckAddresses takes the current default-route fingerprint ("" when the
// machine has no route) and returns when to look again. A changed address is
// confirmed once, netDebounce later, before the network is reset.
func (r *netRecovery) CheckAddresses(fp string) time.Duration {
	if r.mode == recoveryOff || fp == "" {
		return netPollInterval
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.fp == "":
		r.fp = fp
	case fp == r.fp:
		r.pendingFP = ""
	case r.pendingFP != fp:
		r.pendingFP = fp
		return netDebounce
	default:
		old := r.fp
		r.fp, r.pendingFP = fp, ""
		log.WithFields(log.Fields{"old": old, "new": fp}).Warn("Network address changed, resetting the network (DNS sockets and cache)")
		r.doReset(r.now())
	}
	return netPollInterval
}

// defaultRouteFingerprint describes the interface and local IPv4 address the
// default route uses, e.g. "eth0 10.0.0.5". Dialing UDP sends no packet; it
// only asks the kernel which source address it would use. IPv6 is left out on
// purpose: privacy addresses rotate daily and would reset the network daily.
// A public IP that changes behind a NAT does not show here; the DNS failure
// count covers that case.
func defaultRouteFingerprint() string {
	conn, err := net.Dial("udp4", "1.1.1.1:53")
	if err != nil {
		return ""
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil {
		return ""
	}
	name := ""
	if ifaces, err := net.Interfaces(); err == nil {
		for _, ifc := range ifaces {
			addrs, err := ifc.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(local.IP) {
					name = ifc.Name
				}
			}
		}
	}
	return strings.TrimSpace(name + " " + local.IP.String())
}

// watch polls the default route until ctx ends.
func (r *netRecovery) watch(ctx context.Context, fingerprint func() string) {
	delay := r.CheckAddresses(fingerprint()) // the starting point
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = r.CheckAddresses(fingerprint())
	}
}
