package sing

import (
	"fmt"
	"sort"

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

// panelState is the routing data collected from every node's panel config.
type panelState struct {
	outbounds   []outboundEntry
	routes      []panel.Route
	customRules []conf.CustomRouteRule
	rawRoutes   []map[string]any
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
		for _, r := range info.Common.Routes {
			// dns routes configure the DNS module, not the router
			if r.Action == "dns" {
				continue
			}
			st.routes = append(st.routes, r)
		}
		st.customRules = append(st.customRules, info.Common.CustomRouteRules...)
		st.rawRoutes = append(st.rawRoutes, info.Common.CustomRoutes...)
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
	kept := make([]map[string]any, 0, len(rules))
	var warnings []string
	for _, r := range rules {
		if tag := ruleOutbound(r); tag != "" && tag != "block" && tag != "direct" && !known[tag] {
			warnings = append(warnings, fmt.Sprintf("route rule %v references unknown outbound %q, skipped", r, tag))
			continue
		}
		kept = append(kept, r)
	}
	return kept, warnings
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
	rawRules := compileAllRouteRules(st.disableIPv6, st.routes, customRules, st.customRules, st.rawRoutes)
	rawRules, ruleWarnings := filterRules(rawRules, known)
	for _, w := range ruleWarnings {
		log.Warn(w)
	}
	rulesData, err := json.Marshal(rawRules)
	if err != nil {
		return fmt.Errorf("marshal route rules error: %w", err)
	}
	rules, err := json.UnmarshalExtendedContext[[]option.Rule](b.ctx, rulesData)
	if err != nil {
		return fmt.Errorf("unmarshal route rules error: %w", err)
	}

	allRules := make([]option.Rule, 0, len(rules)+len(b.originRules))
	allRules = append(allRules, rules...)
	allRules = append(allRules, b.originRules...)

	type updatableRouter interface {
		UpdateRules(rules []option.Rule, ruleSets []option.RuleSet) error
	}
	if ur, ok := b.router.(updatableRouter); ok {
		if err := ur.UpdateRules(allRules, b.originRuleSets); err != nil {
			return fmt.Errorf("update route rules error: %w", err)
		}
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
	return nil
}
