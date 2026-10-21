package sing

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

// appliedState is the result of the last successful apply.
type appliedState struct {
	Outbounds []outboundEntry
	Rules     []map[string]any
	Counts    map[string]int
}

// nodeRouting is one node's panel routing; its rules only match that node's
// inbound, so nodes sharing one FNode (machine mode) do not see each other's routes.
type nodeRouting struct {
	tag         string
	routes      []panel.Route
	customRules []conf.CustomRouteRule
	rawRoutes   []map[string]any
}

// panelState is the routing data collected from every node's panel config.
type panelState struct {
	outbounds   []outboundEntry
	nodes       []nodeRouting
	disableIPv6 bool
}

// collectPanelState gathers panel outbounds and routes from all nodes, sorted
// by node tag so the result is deterministic.
func (b *Sing) collectPanelState() panelState {
	var st panelState
	if b.coreConfig != nil && b.coreConfig.SingConfig != nil && b.coreConfig.SingConfig.DisableIPv6 {
		st.disableIPv6 = true
	}
	b.users.mapLock.RLock()
	tags := make([]string, 0, len(b.inboundInfo))
	for tag := range b.inboundInfo {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	infos := make([]*panel.NodeInfo, len(tags))
	for i, tag := range tags {
		infos[i] = b.inboundInfo[tag]
	}
	for _, cfg := range b.inboundConfig {
		if cfg != nil && (cfg.DisableIPv6 || (cfg.SingOptions != nil && cfg.SingOptions.DisableIPv6)) {
			st.disableIPv6 = true
		}
	}
	b.users.mapLock.RUnlock()

	for i, info := range infos {
		if info == nil || info.Common == nil {
			continue
		}
		nr := nodeRouting{tag: tags[i]}
		for _, r := range info.Common.Routes {
			// dns routes configure the DNS module, not the router
			if r.Action == "dns" {
				continue
			}
			nr.routes = append(nr.routes, r)
		}
		// The route id is the dashboard order (the panel sends routes in
		// database row order), so the first route by id is matched first.
		sort.SliceStable(nr.routes, func(i, j int) bool { return nr.routes[i].Id < nr.routes[j].Id })
		nr.customRules = info.Common.CustomRouteRules
		nr.rawRoutes = info.Common.CustomRoutes
		st.nodes = append(st.nodes, nr)
		for _, co := range info.Common.CustomOutbounds {
			raw, err := ConvertPanelOutbound(co)
			if err != nil {
				log.WithFields(log.Fields{"tag": tags[i], "err": err}).Warn("Skip invalid panel outbound")
				continue
			}
			e, err := newEntryFromRaw(b.ctx, raw, SourcePanel, tags[i])
			if err != nil {
				log.WithFields(log.Fields{"tag": tags[i], "outbound": co.Tag, "err": err}).Warn("Skip invalid panel outbound")
				continue
			}
			st.outbounds = append(st.outbounds, e)
		}
	}
	return st
}

// ruleOutbound returns the outbound tag a rule map routes to, if any.
func ruleOutbound(rule map[string]any) string {
	if action, ok := rule["action"].(string); ok && action != "" && action != "route" {
		return ""
	}
	tag, _ := rule["outbound"].(string)
	return tag
}

// filterRules drops rules that route to an outbound that does not exist, so a
// typo or a failed outbound never surfaces as "router: outbound not found".
// Rules to direct/block are always kept: dropping an anti-SSRF block rule
// would open private ranges, while a missing block outbound still fails closed.
func filterRules(rules []map[string]any, known map[string]bool) ([]map[string]any, []string) {
	kept, warnings, _ := filterRulesMissing(rules, known)
	return kept, warnings
}

// filterRulesMissing is filterRules that also returns the unknown outbound
// tags per node tag (from the rule's "inbound" scope), so FNode can ask the
// panel for them.
func filterRulesMissing(rules []map[string]any, known map[string]bool) ([]map[string]any, []string, map[string][]string) {
	kept := make([]map[string]any, 0, len(rules))
	var warnings []string
	missing := make(map[string][]string)
	for _, r := range rules {
		if tag := ruleOutbound(r); tag != "" && tag != "block" && tag != "direct" && !known[tag] {
			warnings = append(warnings, fmt.Sprintf("route rule %v references unknown outbound %q, skipped", r, tag))
			for _, node := range ruleInbounds(r) {
				if !containsString(missing[node], tag) {
					missing[node] = append(missing[node], tag)
				}
			}
			continue
		}
		kept = append(kept, r)
	}
	return kept, warnings, missing
}

func ruleInbounds(rule map[string]any) []string {
	switch v := rule["inbound"].(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		var out []string
		for _, it := range v {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// scopeRule limits a panel rule to one node's inbound. Rules that already
// name an inbound, and logical rules (no inbound field at the top level),
// are kept as they are.
func scopeRule(rule map[string]any, tag string) map[string]any {
	if _, ok := rule["inbound"]; ok || tag == "" {
		return rule
	}
	if t, _ := rule["type"].(string); t == "logical" {
		return rule
	}
	out := make(map[string]any, len(rule)+1)
	for k, v := range rule {
		out[k] = v
	}
	out["inbound"] = []string{tag}
	return out
}

// compileScopedRules builds the router rules in xboard-node order: local
// custom rules, each node's panel custom rules and raw routes, anti-SSRF,
// IPv6 block, each node's panel routes. Panel rules are scoped to the node.
func compileScopedRules(disableIPv6 bool, local []conf.CustomRouteRule, nodes []nodeRouting) []map[string]any {
	rules := compileAllRouteRules(false, nil, local, nil, nil)
	// compileAllRouteRules ends with the anti-SSRF pair; the node rules go before it
	base := rules[:len(rules)-2]
	guard := rules[len(rules)-2:]
	out := append([]map[string]any{}, base...)
	for _, n := range nodes {
		for _, cr := range n.customRules {
			if cr.Disabled {
				continue
			}
			for _, r := range compileCustomRouteRule(cr) {
				out = append(out, scopeRule(r, n.tag))
			}
		}
		for _, r := range n.rawRoutes {
			if r = sanitizeRawRule(r); len(r) > 0 {
				out = append(out, scopeRule(r, n.tag))
			}
		}
	}
	out = append(out, guard...)
	if disableIPv6 {
		out = append(out, map[string]any{"outbound": "block", "ip_version": 6})
	}
	for _, n := range nodes {
		for _, pr := range n.routes {
			for _, r := range compilePanelRoute(pr) {
				out = append(out, scopeRule(r, n.tag))
			}
		}
	}
	return out
}

var parseRuleIndex = regexp.MustCompile(`parse rule\[(\d+)\]`)

// failedRuleIndex returns the index of the rule a router UpdateRules error
// names ("parse rule[3]: ..."), or -1.
func failedRuleIndex(err error) int {
	m := parseRuleIndex.FindStringSubmatch(err.Error())
	if m == nil {
		return -1
	}
	i, convErr := strconv.Atoi(m[1])
	if convErr != nil {
		return -1
	}
	return i
}

// applyPanelState merges the static outbounds with every node's panel
// outbounds (panel wins on a duplicate tag), applies them through the
// OutboundManager, and recompiles the route rules. With prune, outbounds that
// are no longer wanted are removed; without it they stay until the next
// pruning apply (DelNode is usually followed by AddNode for the same node).
func (b *Sing) applyPanelState(prune bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	st := b.collectPanelState()
	desired, warnings := mergeOutbounds(b.baseOutbounds, st.outbounds)
	for _, w := range warnings {
		log.Warn(w)
	}
	b.reconcileOutbounds(desired, prune)

	// The effective set is what is actually running: desired entries that
	// were applied, plus stale ones kept because prune was false.
	known := make(map[string]bool, len(b.appliedOutbounds))
	var effective []outboundEntry
	newRaw := make(map[string]map[string]any, len(desired))
	for _, e := range desired {
		if b.appliedOutbounds[e.Tag] == e.Hash {
			effective = append(effective, e)
			known[e.Tag] = true
			newRaw[e.Tag] = e.Raw
		}
	}
	for _, o := range b.box.Outbound().Outbounds() {
		known[o.Tag()] = true
	}
	for _, tag := range b.endpointTags {
		known[tag] = true
	}
	for tag := range b.appliedOutbounds {
		if !known[tag] {
			known[tag] = true
			if raw, ok := b.lastRaw[tag]; ok {
				newRaw[tag] = raw
			}
		}
	}
	b.lastRaw = newRaw

	var customRules []conf.CustomRouteRule
	if b.coreConfig != nil && b.coreConfig.SingConfig != nil {
		customRules = b.coreConfig.SingConfig.CustomRouteRules
	}
	rawRules := compileScopedRules(st.disableIPv6, customRules, st.nodes)
	rawRules, ruleWarnings, missing := filterRulesMissing(rawRules, known)
	for _, w := range ruleWarnings {
		log.Warn(w)
	}
	reportMissingOutbounds(missing, st.nodes)

	// Decode rule by rule: one malformed panel rule is skipped instead of
	// failing the whole update (which would leave the old routing running).
	rules := make([]option.Rule, 0, len(rawRules))
	keptRaw := make([]map[string]any, 0, len(rawRules))
	for _, r := range rawRules {
		data, err := json.Marshal(r)
		if err != nil {
			log.WithFields(log.Fields{"rule": r, "err": err}).Warn("Skip route rule that cannot be encoded")
			continue
		}
		rule, err := json.UnmarshalExtendedContext[option.Rule](b.ctx, data)
		if err != nil {
			log.WithFields(log.Fields{"rule": r, "err": err}).Warn("Skip invalid route rule")
			continue
		}
		rules = append(rules, rule)
		keptRaw = append(keptRaw, r)
	}
	rawRules = keptRaw

	type updatableRouter interface {
		UpdateRules(rules []option.Rule, ruleSets []option.RuleSet) error
	}
	allRules := make([]option.Rule, 0, len(rules)+len(b.originRules))
	if ur, ok := b.router.(updatableRouter); ok {
		// The router builds every rule; drop the one it rejects (named by
		// index in the error) and try again, so the valid rules still apply.
		for attempt := 0; ; attempt++ {
			allRules = append(allRules[:0], rules...)
			allRules = append(allRules, b.originRules...)
			err := ur.UpdateRules(allRules, b.originRuleSets)
			if err == nil {
				break
			}
			i := failedRuleIndex(err)
			if i < 0 || i >= len(rules) || attempt >= 50 {
				return fmt.Errorf("update route rules error: %w", err)
			}
			log.WithFields(log.Fields{"rule": rawRules[i], "err": err}).Warn("Router rejected route rule, skipped")
			rules = append(rules[:i], rules[i+1:]...)
			rawRules = append(rawRules[:i], rawRules[i+1:]...)
		}
	} else {
		allRules = append(append(allRules, rules...), b.originRules...)
	}

	counts := countSources(effective)
	b.last = &appliedState{
		Outbounds: effective,
		Rules:     rawRules,
		Counts:    counts,
	}
	log.Infof("applied %d outbounds (origin %d, local %d, panel %d, builtin %d), %d rules",
		len(effective), counts[SourceOrigin], counts[SourceLocal], counts[SourcePanel], counts[SourceBuiltin],
		len(allRules))
	b.writeEffectiveFiles(prune)
	return nil
}
