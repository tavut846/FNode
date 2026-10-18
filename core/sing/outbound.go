package sing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stdjson "encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sagernet/sing-box/option"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/json"
	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/api/panel"
)

// Outbound sources, in merge order. A later source wins on a duplicate tag.
const (
	SourceBuiltin = "builtin" // direct/block added by FNode when sing_origin.json has no outbounds
	SourceOrigin  = "origin"  // sing_origin.json (OriginalPath)
	SourceLocal   = "local"   // CustomOutbounds in config.json
	SourcePanel   = "panel"   // custom_outbounds from the panel
)

// outboundEntry is one outbound in the merged set.
type outboundEntry struct {
	Tag    string
	Type   string
	Source string
	// Node is the node tag that delivered a panel outbound.
	Node   string
	Raw    map[string]any
	Option option.Outbound
	Hash   string
}

// panelProtocolAliases maps panel/xray protocol names to sing-box outbound types.
var panelProtocolAliases = map[string]string{
	"ss":        "shadowsocks",
	"socks5":    "socks",
	"hy2":       "hysteria2",
	"wg":        "wireguard",
	"freedom":   "direct",
	"blackhole": "block",
}

// ConvertPanelOutbound turns a panel custom outbound ({tag, protocol, settings,
// proxy_tag}) into a sing-box outbound map: {type: protocol, tag, ...settings},
// with proxy_tag becoming "detour".
func ConvertPanelOutbound(o panel.CustomOutbound) (map[string]any, error) {
	tag := strings.TrimSpace(o.Tag)
	if tag == "" {
		return nil, fmt.Errorf("panel outbound without tag")
	}
	protocol := strings.ToLower(strings.TrimSpace(o.Protocol))
	if alias, ok := panelProtocolAliases[protocol]; ok {
		protocol = alias
	}
	if protocol == "" {
		return nil, fmt.Errorf("panel outbound %q without protocol", tag)
	}
	m := make(map[string]any, len(o.Settings)+3)
	for k, v := range o.Settings {
		switch k {
		case "tag", "type", "protocol", "proxy_tag", "proxyTag":
			continue
		}
		m[k] = v
	}
	m["type"] = protocol
	m["tag"] = tag
	if pt := strings.TrimSpace(o.ProxyTag); pt != "" && pt != tag {
		if _, ok := m["detour"]; !ok {
			m["detour"] = pt
		}
	}
	return m, nil
}

func hashRaw(raw map[string]any) string {
	// encoding/json sorts map keys, so equal maps hash equally
	b, _ := stdjson.Marshal(raw)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// newEntryFromRaw parses a sing-box outbound map into an entry.
func newEntryFromRaw(ctx context.Context, raw map[string]any, source, node string) (outboundEntry, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return outboundEntry{}, err
	}
	opt, err := json.UnmarshalExtendedContext[option.Outbound](ctx, data)
	if err != nil {
		return outboundEntry{}, err
	}
	if opt.Tag == "" {
		return outboundEntry{}, fmt.Errorf("outbound without tag")
	}
	return outboundEntry{
		Tag:    opt.Tag,
		Type:   opt.Type,
		Source: source,
		Node:   node,
		Raw:    raw,
		Option: opt,
		Hash:   hashRaw(raw),
	}, nil
}

// newEntryFromOption builds an entry from an already parsed outbound (sing_origin.json).
func newEntryFromOption(ctx context.Context, opt option.Outbound, source string) (outboundEntry, error) {
	data, err := json.MarshalContext(ctx, opt)
	if err != nil {
		return outboundEntry{}, err
	}
	var raw map[string]any
	if err = stdjson.Unmarshal(data, &raw); err != nil {
		return outboundEntry{}, err
	}
	return outboundEntry{
		Tag:    opt.Tag,
		Type:   opt.Type,
		Source: source,
		Raw:    raw,
		Option: opt,
		Hash:   hashRaw(raw),
	}, nil
}

// mergeOutbounds merges outbound layers in order. On a duplicate tag the later
// entry replaces the earlier one in place (keeping its position, so the first
// outbound stays the default) and a warning is returned.
func mergeOutbounds(layers ...[]outboundEntry) ([]outboundEntry, []string) {
	var merged []outboundEntry
	var warnings []string
	index := make(map[string]int)
	for _, layer := range layers {
		for _, e := range layer {
			if i, ok := index[e.Tag]; ok {
				prev := merged[i]
				from := e.Source
				if e.Node != "" {
					from += " (" + e.Node + ")"
				}
				was := prev.Source
				if prev.Node != "" {
					was += " (" + prev.Node + ")"
				}
				warnings = append(warnings, fmt.Sprintf("outbound tag %q from %s overrides the one from %s", e.Tag, from, was))
				merged[i] = e
				continue
			}
			index[e.Tag] = len(merged)
			merged = append(merged, e)
		}
	}
	return merged, warnings
}

// outboundDeps returns the tags an outbound map refers to (detour, group members).
func outboundDeps(raw map[string]any) []string {
	var deps []string
	if d, ok := raw["detour"].(string); ok && d != "" {
		deps = append(deps, d)
	}
	if d, ok := raw["default"].(string); ok && d != "" {
		deps = append(deps, d)
	}
	switch list := raw["outbounds"].(type) {
	case []any:
		for _, it := range list {
			if s, ok := it.(string); ok && s != "" {
				deps = append(deps, s)
			}
		}
	case []string:
		deps = append(deps, list...)
	}
	return deps
}

// orderByDeps sorts entries so that an outbound comes after the outbounds it
// depends on (within the given set). Cycles keep their input order.
func orderByDeps(entries []outboundEntry) []outboundEntry {
	inSet := make(map[string]bool, len(entries))
	for _, e := range entries {
		inSet[e.Tag] = true
	}
	done := make(map[string]bool, len(entries))
	visiting := make(map[string]bool)
	byTag := make(map[string]outboundEntry, len(entries))
	for _, e := range entries {
		byTag[e.Tag] = e
	}
	var out []outboundEntry
	var visit func(e outboundEntry)
	visit = func(e outboundEntry) {
		if done[e.Tag] || visiting[e.Tag] {
			return
		}
		visiting[e.Tag] = true
		for _, d := range outboundDeps(e.Raw) {
			if inSet[d] {
				visit(byTag[d])
			}
		}
		visiting[e.Tag] = false
		done[e.Tag] = true
		out = append(out, e)
	}
	for _, e := range entries {
		visit(e)
	}
	return out
}

// planOutbounds compares the desired set with the applied hashes. It returns
// the entries to (re)create, in dependency order, and the tags to remove.
// An outbound whose dependency is recreated is recreated too, because sing-box
// dialers keep a reference to the detour outbound they resolved first.
func planOutbounds(desired []outboundEntry, applied map[string]string, prune bool) (create []outboundEntry, remove []string) {
	changed := make(map[string]bool)
	for _, e := range desired {
		if applied[e.Tag] != e.Hash {
			changed[e.Tag] = true
		}
	}
	// propagate to dependents until stable
	for {
		grew := false
		for _, e := range desired {
			if changed[e.Tag] {
				continue
			}
			for _, d := range outboundDeps(e.Raw) {
				if changed[d] {
					changed[e.Tag] = true
					grew = true
					break
				}
			}
		}
		if !grew {
			break
		}
	}
	for _, e := range desired {
		if changed[e.Tag] {
			create = append(create, e)
		}
	}
	create = orderByDeps(create)
	if prune {
		want := make(map[string]bool, len(desired))
		for _, e := range desired {
			want[e.Tag] = true
		}
		for tag := range applied {
			if !want[tag] {
				remove = append(remove, tag)
			}
		}
		sort.Strings(remove)
	}
	return create, remove
}

// reconcileOutbounds applies the desired outbound set through sing-box's
// OutboundManager: changed and new outbounds are created (Create replaces an
// existing tag), and outbounds that are no longer wanted are removed.
func (b *Sing) reconcileOutbounds(desired []outboundEntry, prune bool) {
	create, remove := planOutbounds(desired, b.appliedOutbounds, prune)
	manager := b.box.Outbound()
	for _, e := range create {
		err := manager.Create(
			b.ctx,
			b.box.Router(),
			b.logFactory.NewLogger(F.ToString("outbound/", e.Type, "[", e.Tag, "]")),
			e.Tag,
			e.Type,
			e.Option.Options,
		)
		if err != nil {
			log.WithFields(log.Fields{"tag": e.Tag, "source": e.Source, "err": err}).Warn("Create outbound failed")
			delete(b.appliedOutbounds, e.Tag)
			continue
		}
		b.appliedOutbounds[e.Tag] = e.Hash
	}
	// Remove dependents before the outbounds they depend on: sing-box refuses
	// to remove an outbound that another one still depends on.
	removed := make(map[string]bool)
	for pass := 0; pass < len(remove)+1; pass++ {
		progress := false
		for _, tag := range remove {
			if removed[tag] {
				continue
			}
			if dependedBy := b.dependents(tag, remove, removed); dependedBy != "" {
				continue
			}
			if _, ok := manager.Outbound(tag); ok {
				if err := manager.Remove(tag); err != nil {
					log.WithFields(log.Fields{"tag": tag, "err": err}).Warn("Remove outbound failed")
				}
			}
			delete(b.appliedOutbounds, tag)
			removed[tag] = true
			progress = true
		}
		if !progress {
			break
		}
	}
	for _, tag := range remove {
		if !removed[tag] {
			log.WithField("tag", tag).Warn("Outbound is still used by another outbound, keeping it")
		}
	}
}

// dependents reports another to-be-removed outbound that still depends on tag.
func (b *Sing) dependents(tag string, remove []string, removed map[string]bool) string {
	for _, other := range remove {
		if other == tag || removed[other] {
			continue
		}
		if out, ok := b.lastRaw[other]; ok {
			for _, d := range outboundDeps(out) {
				if d == tag {
					return other
				}
			}
		}
	}
	return ""
}

// countSources counts merged outbounds per source.
func countSources(entries []outboundEntry) map[string]int {
	counts := make(map[string]int)
	for _, e := range entries {
		counts[e.Source]++
	}
	return counts
}
