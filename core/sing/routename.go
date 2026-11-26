package sing

import (
	"context"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	R "github.com/sagernet/sing-box/route/rule"

	"github.com/tavut846/FNode/api/panel"
)

// ruleNameKey carries a rule's display name through the compile pipeline; it
// is stripped (takeRuleName) before the rule is handed to sing-box.
const ruleNameKey = "_name"

// nameRules tags compiled rule maps with a display name (in place).
func nameRules(rules []map[string]any, name string) []map[string]any {
	for _, r := range rules {
		r[ruleNameKey] = name
	}
	return rules
}

// takeRuleName returns a copy of the rule without the name key, plus the name.
func takeRuleName(r map[string]any) (map[string]any, string) {
	name, ok := r[ruleNameKey].(string)
	if !ok {
		return r, ""
	}
	out := make(map[string]any, len(r))
	for k, v := range r {
		if k != ruleNameKey {
			out[k] = v
		}
	}
	return out, name
}

// ruleLabel is the name when set, else "<kind>#<index>".
func ruleLabel(name, kind string, index int) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return kind + "#" + strconv.Itoa(index)
}

// rawRuleName reads an optional "name"/"remarks" from a raw custom_routes entry.
func rawRuleName(r map[string]any) string {
	for _, k := range []string{"name", "remarks"} {
		if s, ok := r[k].(string); ok && strings.TrimSpace(s) != "" {
			delete(r, k) // not a sing-box rule field
			return s
		}
	}
	return ""
}

// panelRouteLabel names a panel route: its remarks/name when the panel sends
// one (optional), else "route#<id>".
func panelRouteLabel(pr panel.Route) string {
	name := pr.Remarks
	if strings.TrimSpace(name) == "" {
		name = pr.Name
	}
	return ruleLabel(name, "route", pr.Id)
}

func ruleKey(rule adapter.Rule) string {
	return rule.String() + " => " + rule.Action().String()
}

// SetRouteNames records the display name of every router rule so the
// connection hook can log "route=<name>". Rules are keyed by their sing-box
// description; identical rules do the same thing, so sharing a name is fine.
func (h *HookServer) SetRouteNames(ctx context.Context, logger log.ContextLogger, rules []option.Rule, names []string) {
	if h == nil {
		return
	}
	m := make(map[string]string, len(rules))
	for i, opt := range rules {
		if i >= len(names) || names[i] == "" {
			continue
		}
		built, err := R.NewRule(ctx, logger, opt, false)
		if err != nil {
			continue
		}
		if k := ruleKey(built); m[k] == "" {
			m[k] = names[i]
		}
	}
	h.routeNames.Store(&m)
}

func (h *HookServer) routeName(rule adapter.Rule) string {
	if rule == nil {
		return "default"
	}
	if p := h.routeNames.Load(); p != nil {
		if n, ok := (*p)[ruleKey(rule)]; ok {
			return n
		}
	}
	return "other"
}

// logRoute writes one stable line per routed connection:
// [<inbound>] route=<name> outbound=<tag> dest=<host:port>
func (h *HookServer) logRoute(m adapter.InboundContext, rule adapter.Rule, out adapter.Outbound) {
	tag := "unknown"
	if out != nil {
		tag = out.Tag()
	}
	name := h.routeName(rule)
	if strings.ContainsAny(name, " \t\"") {
		name = strconv.Quote(name)
	}
	log.Info("[", m.Inbound, "] route=", name, " outbound=", tag, " dest=", m.Destination.String())
}
