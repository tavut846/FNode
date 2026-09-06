package sing

import (
	"strconv"
	"strings"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

var PrivateIPv4CIDR = []string{
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
}

var PrivateIPv6CIDR = []string{
	"fc00::/7",
	"fe80::/10",
	"::1/128",
}

// BuildDefaultOutbounds builds native sing-box outbound configurations.
// Guarantees direct and block outbounds are properly configured with timeouts and domain strategy.
func BuildDefaultOutbounds(connectTimeout time.Duration, domainStrategy option.DomainStrategy, sendIP string, customOutbounds []map[string]any) []map[string]any {
	var outbounds []map[string]any
	tags := make(map[string]bool)

	for _, co := range customOutbounds {
		if tag, ok := co["tag"].(string); ok && tag != "" {
			tags[strings.ToLower(tag)] = true
		}
		outbounds = append(outbounds, co)
	}

	if !tags["direct"] {
		directM := map[string]any{
			"type": "direct",
			"tag":  "direct",
		}
		if connectTimeout > 0 {
			directM["connect_timeout"] = connectTimeout.String()
		} else {
			directM["connect_timeout"] = "5s"
		}
		if domainStrategy != 0 {
			directM["domain_strategy"] = domainStrategy.String()
		} else {
			directM["domain_strategy"] = "prefer_ipv4"
		}
		if sendIP != "" && sendIP != "0.0.0.0" {
			directM["inet4_bind_address"] = sendIP
		}
		outbounds = append([]map[string]any{directM}, outbounds...)
		tags["direct"] = true
	}

	if !tags["block"] {
		outbounds = append(outbounds, map[string]any{
			"type": "block",
			"tag":  "block",
		})
		tags["block"] = true
	}

	return outbounds
}

// CompileRouteRules compiles panel routes, custom route rules, anti-SSRF protections,
// and optional IPv6 blocking into sing-box native rule map representations.
func CompileRouteRules(disableIPv6 bool, panelRoutes []panel.Route, customRules []conf.CustomRouteRule) []map[string]any {
	var rules []map[string]any

	// 1. Structured custom rules (highest priority)
	for _, cr := range customRules {
		if cr.Disabled {
			continue
		}
		rules = append(rules, compileCustomRouteRule(cr)...)
	}

	// 2. Anti-SSRF Protection: Block private IPv4 and IPv6 addresses
	rules = append(rules,
		map[string]any{
			"outbound": "block",
			"ip_cidr":  PrivateIPv4CIDR,
		},
		map[string]any{
			"outbound": "block",
			"ip_cidr":  PrivateIPv6CIDR,
		},
	)

	// 3. IPv6 Blocking (if host does not have working IPv6 internet routing)
	// Immediately rejects IPv6 connections to prevent 30-40s hangs and UoT closed pipe errors
	if disableIPv6 {
		rules = append(rules, map[string]any{
			"outbound":   "block",
			"ip_version": 6,
		})
	}

	// 4. Panel-defined routes
	for _, pr := range panelRoutes {
		rules = append(rules, compilePanelRoute(pr)...)
	}

	return rules
}

func compileCustomRouteRule(rule conf.CustomRouteRule) []map[string]any {
	if rule.Disabled {
		return nil
	}

	outbound := rule.Action.Type
	if rule.Action.Type == "route" && rule.Action.Target != "" {
		outbound = rule.Action.Target
	} else if rule.Action.Type != "direct" && rule.Action.Type != "block" {
		outbound = "block"
	}

	var compiled []map[string]any

	if len(rule.Match.Domains) > 0 {
		compiled = append(compiled, map[string]any{
			"domain":   rule.Match.Domains,
			"outbound": outbound,
		})
	}
	if len(rule.Match.DomainSuffixes) > 0 {
		compiled = append(compiled, map[string]any{
			"domain_suffix": rule.Match.DomainSuffixes,
			"outbound":      outbound,
		})
	}
	if len(rule.Match.IPCIDRs) > 0 {
		compiled = append(compiled, map[string]any{
			"ip_cidr":  rule.Match.IPCIDRs,
			"outbound": outbound,
		})
	}
	if len(rule.Match.Ports) > 0 {
		ports, portRanges := splitPorts(rule.Match.Ports)
		entry := map[string]any{"outbound": outbound}
		if len(ports) > 0 {
			entry["port"] = ports
		}
		if len(portRanges) > 0 {
			entry["port_range"] = portRanges
		}
		compiled = append(compiled, entry)
	}
	if len(rule.Match.Networks) > 0 {
		compiled = append(compiled, map[string]any{
			"network":  rule.Match.Networks,
			"outbound": outbound,
		})
	}
	if len(rule.Match.SourceCIDRs) > 0 {
		compiled = append(compiled, map[string]any{
			"source_ip_cidr": rule.Match.SourceCIDRs,
			"outbound":       outbound,
		})
	}
	if len(rule.Match.SourcePorts) > 0 {
		ports, portRanges := splitPorts(rule.Match.SourcePorts)
		entry := map[string]any{"outbound": outbound}
		if len(ports) > 0 {
			entry["source_port"] = ports
		}
		if len(portRanges) > 0 {
			entry["source_port_range"] = portRanges
		}
		compiled = append(compiled, entry)
	}

	return compiled
}

func compilePanelRoute(pr panel.Route) []map[string]any {
	var matches []string
	switch m := pr.Match.(type) {
	case string:
		matches = strings.Split(m, ",")
	case []string:
		matches = m
	case []any:
		for _, item := range m {
			if str, ok := item.(string); ok {
				matches = append(matches, str)
			}
		}
	}

	if len(matches) == 0 {
		return nil
	}

	outbound := "block"
	switch pr.Action {
	case "direct":
		outbound = "direct"
	case "block":
		outbound = "block"
	case "route", "proxy":
		if pr.ActionValue != "" {
			outbound = pr.ActionValue
		}
	case "dns":
		if pr.ActionValue != "" {
			outbound = pr.ActionValue
		} else {
			outbound = "dns-out"
		}
	}

	var domains []string
	var domainSuffixes []string
	var domainRegex []string
	var geosites []string
	var geoips []string
	var ipCidrs []string
	var protocols []string

	for _, item := range matches {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		switch {
		case strings.HasPrefix(item, "protocol:"):
			protocols = append(protocols, strings.TrimPrefix(item, "protocol:"))
		case strings.HasPrefix(item, "regexp:"):
			domainRegex = append(domainRegex, strings.TrimPrefix(item, "regexp:"))
		case strings.HasPrefix(item, "geosite:"):
			geosites = append(geosites, strings.TrimPrefix(item, "geosite:"))
		case strings.HasPrefix(item, "geoip:"):
			geoips = append(geoips, strings.TrimPrefix(item, "geoip:"))
		case strings.Contains(item, "/"):
			ipCidrs = append(ipCidrs, item)
		case strings.HasPrefix(item, "*."):
			domainSuffixes = append(domainSuffixes, strings.TrimPrefix(item, "*."))
		default:
			// By default in panel rules, domain entries act as domain suffixes
			domainSuffixes = append(domainSuffixes, item)
		}
	}

	var compiled []map[string]any
	if len(domains) > 0 {
		compiled = append(compiled, map[string]any{
			"domain":   domains,
			"outbound": outbound,
		})
	}
	if len(domainSuffixes) > 0 {
		compiled = append(compiled, map[string]any{
			"domain_suffix": domainSuffixes,
			"outbound":      outbound,
		})
	}
	if len(domainRegex) > 0 {
		compiled = append(compiled, map[string]any{
			"domain_regex": domainRegex,
			"outbound":     outbound,
		})
	}
	if len(geosites) > 0 {
		compiled = append(compiled, map[string]any{
			"geosite":  geosites,
			"outbound": outbound,
		})
	}
	if len(geoips) > 0 {
		compiled = append(compiled, map[string]any{
			"geoip":    geoips,
			"outbound": outbound,
		})
	}
	if len(ipCidrs) > 0 {
		compiled = append(compiled, map[string]any{
			"ip_cidr":  ipCidrs,
			"outbound": outbound,
		})
	}
	if len(protocols) > 0 {
		compiled = append(compiled, map[string]any{
			"protocol": protocols,
			"outbound": outbound,
		})
	}

	return compiled
}

func splitPorts(values []string) ([]int, []string) {
	var ports []int
	var ranges []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, ":-") {
			ranges = append(ranges, strings.ReplaceAll(value, "-", ":"))
			continue
		}
		if port, err := strconv.Atoi(value); err == nil {
			ports = append(ports, port)
		}
	}
	return ports, ranges
}

// ConvertStrategy converts a string strategy into sing-box DomainStrategy
func ConvertStrategy(s string) option.DomainStrategy {
	switch strings.ToLower(s) {
	case "prefer_ipv4":
		return option.DomainStrategy(C.DomainStrategyPreferIPv4)
	case "prefer_ipv6":
		return option.DomainStrategy(C.DomainStrategyPreferIPv6)
	case "ipv4_only":
		return option.DomainStrategy(C.DomainStrategyIPv4Only)
	case "ipv6_only":
		return option.DomainStrategy(C.DomainStrategyIPv6Only)
	default:
		return option.DomainStrategy(C.DomainStrategyPreferIPv4)
	}
}
