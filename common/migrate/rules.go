// Package migrate upgrades FNode's own config and the sing-box JSON it carries
// (sing_origin.json, Cores[].CustomOutbounds) to what the bundled sing-box core
// accepts. The rules work on plain JSON maps, only touch fields that sing-box
// 1.12-1.14 renamed or removed, and are idempotent: converted input comes out
// unchanged and reports no change.
package migrate

import (
	"fmt"
	"net/url"
	"strings"
)

// SchemaVersion is the config schema this FNode writes ("SchemaVersion" in
// config.json). 1 is every config without the key (sing-box <= 1.11 style).
const SchemaVersion = 2

type doc = map[string]any

// SingOptions applies every rule to one sing-box options document (a whole
// sing_origin.json) and returns one line per change.
func SingOptions(d doc) []string {
	var log []string
	log = append(log, legacyDNS(d)...)
	log = append(log, geoFields(d)...)
	log = append(log, ruleSetDetours(d)...)
	log = append(log, droppedCacheOptions(d)...)
	log = append(log, defaultDomainResolver(d)...)
	return log
}

func asMap(v any) doc {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// legacyDNS converts the pre-1.12 dns.servers form ({"address": "udp://1.1.1.1"})
// into typed servers ({"type": "udp", "server": "1.1.1.1"}), which sing-box 1.14
// requires.
func legacyDNS(d doc) []string {
	dns := asMap(d["dns"])
	if dns == nil {
		return nil
	}
	var log []string
	for i, s := range asList(dns["servers"]) {
		m := asMap(s)
		if m == nil || m["type"] != nil {
			continue
		}
		addr, ok := m["address"].(string)
		if !ok {
			continue
		}
		tag := str(m["tag"])
		typ, server, port, path, tls, ok := splitDNSAddress(addr)
		if !ok {
			log = append(log, fmt.Sprintf("dns.servers[%d] %q: address %q has no typed equivalent, convert it by hand", i, tag, addr))
			continue
		}
		delete(m, "address")
		m["type"] = typ
		if server != "" {
			m["server"] = server
		}
		if port != 0 {
			m["server_port"] = port
		}
		if path != "" {
			m["path"] = path
		}
		if tls && m["tls"] == nil {
			m["tls"] = map[string]any{}
		}
		if v, ok := m["address_resolver"]; ok {
			delete(m, "address_resolver")
			dr := map[string]any{"server": v}
			if st, ok := m["address_strategy"]; ok {
				dr["strategy"] = st
			}
			m["domain_resolver"] = dr
		}
		delete(m, "address_strategy")
		delete(m, "address_fallback_delay")
		log = append(log, fmt.Sprintf("dns.servers[%d] %q: legacy address %q -> type %q", i, tag, addr, typ))
	}
	return log
}

// splitDNSAddress maps a legacy DNS address to the typed server fields.
func splitDNSAddress(addr string) (typ, server string, port int, path string, tls, ok bool) {
	switch addr {
	case "local":
		return "local", "", 0, "", false, true
	case "fakeip", "":
		return "", "", 0, "", false, false
	}
	if !strings.Contains(addr, "://") {
		addr = "udp://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return "", "", 0, "", false, false
	}
	switch u.Scheme {
	case "udp", "tcp", "tls", "quic":
		typ = u.Scheme
	case "https":
		typ, tls = "https", true
		path = u.Path
	case "h3":
		typ, tls = "h3", true
		path = u.Path
	case "dhcp":
		return "dhcp", "", 0, "", false, true
	default: // rcode, tcp-less schemes, anything else
		return "", "", 0, "", false, false
	}
	host := u.Hostname()
	if host == "" {
		return "", "", 0, "", false, false
	}
	if p := u.Port(); p != "" {
		if _, err := fmt.Sscanf(p, "%d", &port); err != nil {
			return "", "", 0, "", false, false
		}
	}
	if path == "/dns-query" {
		path = ""
	}
	return typ, host, port, path, tls, true
}

// geoFields drops the geoip/geosite databases that sing-box removed in 1.12:
// route.geoip / route.geosite, and rule fields geoip, geosite, source_geoip.
// geoip "private" becomes ip_is_private. A rule that used any other geo field
// is removed whole (dropping just the field would widen it to match more).
func geoFields(d doc) []string {
	var log []string
	route := asMap(d["route"])
	if route == nil {
		return nil
	}
	for _, k := range []string{"geoip", "geosite"} {
		if _, ok := route[k]; ok {
			delete(route, k)
			log = append(log, "route."+k+": database section removed (sing-box 1.12 dropped it)")
		}
	}
	rules := asList(route["rules"])
	if rules == nil {
		return log
	}
	kept := make([]any, 0, len(rules))
	for i, r := range rules {
		m := asMap(r)
		if m == nil {
			kept = append(kept, r)
			continue
		}
		drop := false
		for _, k := range []string{"geoip", "geosite", "source_geoip"} {
			v, ok := m[k]
			if !ok {
				continue
			}
			delete(m, k)
			if k == "geoip" && onlyPrivate(v) {
				m["ip_is_private"] = true
				log = append(log, fmt.Sprintf("route.rules[%d]: geoip private -> ip_is_private", i))
				continue
			}
			drop = true
			log = append(log, fmt.Sprintf("route.rules[%d]: uses %s which sing-box no longer has, rule removed", i, k))
		}
		if !drop {
			kept = append(kept, m)
		}
	}
	if len(kept) != len(rules) {
		route["rules"] = kept
	}
	return log
}

func onlyPrivate(v any) bool {
	switch t := v.(type) {
	case string:
		return t == "private"
	case []any:
		for _, e := range t {
			if str(e) != "private" {
				return false
			}
		}
		return len(t) > 0
	}
	return false
}

// ruleSetDetours moves a remote rule-set's download_detour into http_client.
func ruleSetDetours(d doc) []string {
	route := asMap(d["route"])
	if route == nil {
		return nil
	}
	var log []string
	for i, s := range asList(route["rule_set"]) {
		m := asMap(s)
		if m == nil || str(m["type"]) != "remote" {
			continue
		}
		detour, ok := m["download_detour"]
		if !ok {
			continue
		}
		delete(m, "download_detour")
		if m["http_client"] == nil && str(detour) != "" {
			m["http_client"] = map[string]any{"detour": detour}
		}
		log = append(log, fmt.Sprintf("route.rule_set[%d] %q: download_detour -> http_client.detour", i, str(m["tag"])))
	}
	return log
}

// droppedCacheOptions removes options that only warn in 1.14 and are scheduled
// for removal: dns.independent_cache and experimental.cache_file.store_rdrc.
func droppedCacheOptions(d doc) []string {
	var log []string
	if dns := asMap(d["dns"]); dns != nil {
		if _, ok := dns["independent_cache"]; ok {
			delete(dns, "independent_cache")
			log = append(log, "dns.independent_cache: removed (deprecated in 1.14)")
		}
	}
	if exp := asMap(d["experimental"]); exp != nil {
		if cf := asMap(exp["cache_file"]); cf != nil {
			if _, ok := cf["store_rdrc"]; ok {
				delete(cf, "store_rdrc")
				log = append(log, "experimental.cache_file.store_rdrc: removed (deprecated in 1.14)")
			}
		}
	}
	return log
}

// defaultDomainResolver sets route.default_domain_resolver when the config
// defines two or more DNS servers: sing-box then cannot pick the resolver for
// outbound server names on its own (missing-domain-resolver in 1.14).
func defaultDomainResolver(d doc) []string {
	dns := asMap(d["dns"])
	servers := asList(dns["servers"])
	if len(servers) < 2 {
		return nil
	}
	route := asMap(d["route"])
	if route == nil || route["default_domain_resolver"] != nil {
		return nil
	}
	// the system resolver cannot depend on another server; else the final / first one
	tag := ""
	for _, s := range servers {
		if m := asMap(s); str(m["type"]) == "local" {
			tag = str(m["tag"])
			break
		}
	}
	if tag == "" {
		tag = str(dns["final"])
	}
	if tag == "" {
		tag = str(asMap(servers[0])["tag"])
	}
	if tag == "" {
		return nil
	}
	route["default_domain_resolver"] = tag
	return []string{fmt.Sprintf("route.default_domain_resolver: set to %q (the local DNS server, else final/first)", tag)}
}
