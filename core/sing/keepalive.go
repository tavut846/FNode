package sing

import (
	"time"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/tavut846/FNode/conf"
)

// sing-box probes an idle TCP connection after 5 minutes and then every 75
// seconds. Home routers, CGNAT and firewalls forget an idle mapping long
// before that (often after 30 to 120 seconds), so an idle proxy connection
// (an AnyTLS pooled session, a VLESS or Trojan stream) is dead on the
// client's side while looking alive on both ends. Probing from the server
// every 30 seconds keeps the mapping alive and finds a dead peer sooner.
const (
	defaultTCPKeepAlive         = 30 * time.Second
	defaultTCPKeepAliveInterval = 15 * time.Second
)

// applyTCPKeepAlive sets the TCP keepalive of an inbound's listener from the
// node options (Options.TCPKeepAlive/TCPKeepAliveInterval, seconds).
func applyTCPKeepAlive(l *option.ListenOptions, c *conf.Options) {
	if c.TCPKeepAlive < 0 {
		l.DisableTCPKeepAlive = true
		return
	}
	idle, interval := defaultTCPKeepAlive, defaultTCPKeepAliveInterval
	if c.TCPKeepAlive > 0 {
		idle = time.Duration(c.TCPKeepAlive) * time.Second
	}
	if c.TCPKeepAliveInterval > 0 {
		interval = time.Duration(c.TCPKeepAliveInterval) * time.Second
	}
	l.TCPKeepAlive = badoption.Duration(idle)
	l.TCPKeepAliveInterval = badoption.Duration(interval)
}
