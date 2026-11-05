package sing

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// localAddrs lists this machine's interface addresses (public and private IPs
// of a VPS, plus loopback).
func localAddrs() []netip.Addr {
	var out []netip.Addr
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
				out = append(out, ip.Unmap())
			}
		}
	}
	return out
}

// selfGuard recognises outbounds that would dial this machine itself.
type selfGuard struct {
	addrs []netip.Addr
	// ports are the listen ports of this FNode's own inbounds
	ports map[int]bool
}

func (g selfGuard) isSelf(host string) bool {
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false // a domain is not resolved here
	}
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return true
	}
	for _, a := range g.addrs {
		if a == ip {
			return true
		}
	}
	return false
}

func rawPort(v any) int {
	switch p := v.(type) {
	case float64:
		return int(p)
	case int:
		return p
	case int64:
		return int(p)
	case string:
		n, _ := strconv.Atoi(p)
		return n
	}
	return 0
}

// loops reports why an outbound would dial this machine, or "". Panel
// outbounds are refused for any own address (the dashboard's default outbound
// on a landing node); local and origin outbounds only when the target port is
// one of this FNode's own inbound ports, because a local helper such as a
// 127.0.0.1 WARP/socks proxy on another port is legitimate.
func (g selfGuard) loops(e outboundEntry) string {
	host, _ := e.Raw["server"].(string)
	if host == "" || !g.isSelf(host) {
		return ""
	}
	port := rawPort(e.Raw["server_port"])
	if e.Source == SourcePanel {
		return fmt.Sprintf("its server %s is this machine itself", host)
	}
	if g.ports[port] {
		return fmt.Sprintf("its server %s:%d is one of this node's own inbounds", host, port)
	}
	return ""
}

// dropUnsafeOutbounds removes outbounds that dial this machine (and, until
// stable, every outbound whose detour or group member chain leads to a removed
// one), so the node starts and keeps routing. It returns the kept entries, one
// warning per removed outbound and the removed tags.
func dropUnsafeOutbounds(entries []outboundEntry, g selfGuard) (kept []outboundEntry, warnings []string, dropped map[string]bool) {
	dropped = map[string]bool{}
	for _, e := range entries {
		if why := g.loops(e); why != "" {
			dropped[e.Tag] = true
			warnings = append(warnings, fmt.Sprintf("outbound %q (%s) skipped: %s; set IgnorePanelOutbounds or fix its address", e.Tag, e.Source, why))
		}
	}
	for grew := len(dropped) > 0; grew; {
		grew = false
		for _, e := range entries {
			if dropped[e.Tag] {
				continue
			}
			for _, d := range outboundDeps(e.Raw) {
				if dropped[d] {
					dropped[e.Tag] = true
					grew = true
					warnings = append(warnings, fmt.Sprintf("outbound %q (%s) skipped: it depends on skipped outbound %q", e.Tag, e.Source, d))
					break
				}
			}
		}
	}
	for _, e := range entries {
		if !dropped[e.Tag] {
			kept = append(kept, e)
		}
	}
	return kept, warnings, dropped
}

// ignoredPanelOutbound reports whether the local config drops this panel outbound.
func ignoredPanelOutbound(all bool, tags []string, tag string) bool {
	if all {
		return true
	}
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func logIgnored(all bool, tags []string, ignored []string) {
	if len(ignored) == 0 {
		return
	}
	mode := "IgnorePanelOutbounds is on"
	if !all {
		mode = fmt.Sprintf("IgnorePanelOutboundTags %v", tags)
	}
	log.Warnf("%s: ignoring panel outbound(s) %v (and the routes that use them); traffic falls back to the default route", mode, ignored)
}
