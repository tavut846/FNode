package sing

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	vCore "github.com/tavut846/FNode/core"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	logrus "github.com/sirupsen/logrus"
)

var _ vCore.Core = (*Sing)(nil)

type DNSConfig struct {
	Servers []map[string]interface{} `json:"servers"`
	Rules   []map[string]interface{} `json:"rules"`
}

type Sing struct {
	box                       *box.Box
	ctx                       context.Context
	hookServer                *HookServer
	router                    adapter.Router
	logFactory                log.Factory
	users                     *UserMap
	inboundUsers              map[string][]panel.UserInfo
	inboundInfo               map[string]*panel.NodeInfo
	inboundConfig             map[string]*conf.Options
	inboundOptions            map[string]option.Inbound
	nodeReportMinTrafficBytes map[string]int64
	coreConfig                *conf.CoreConfig
	originRules               []option.Rule
	originRuleSets            []option.RuleSet
	routeFinal                string
	endpointTags              []string
	capture                   *logCapture

	// baseOutbounds is the static outbound set (sing_origin.json, local
	// CustomOutbounds and builtin direct/block) the box was created with.
	baseOutbounds []outboundEntry
	// appliedOutbounds maps every outbound tag FNode manages to the hash of
	// the options it is currently running with.
	appliedOutbounds map[string]string
	// lastRaw holds the raw options of the outbounds currently applied.
	lastRaw map[string]map[string]any
	// last holds the result of the last apply, for the effective-config dump.
	last *appliedState

	mu sync.RWMutex
}

type UserMap struct {
	uidMap  map[string]int
	mapLock sync.RWMutex
}

func init() {
	vCore.RegisterCore("sing", New)
}

func New(c *conf.CoreConfig) (vCore.Core, error) {
	ctx := context.Background()
	ctx = box.Context(ctx, include.InboundRegistry(), include.OutboundRegistry(), include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry())
	options := option.Options{}
	var originRules []option.Rule
	var originRuleSets []option.RuleSet
	if len(c.SingConfig.OriginalPath) != 0 {
		data, err := os.ReadFile(c.SingConfig.OriginalPath)
		if err != nil {
			return nil, fmt.Errorf("read original config error: %s", err)
		}
		options, err = json.UnmarshalExtendedContext[option.Options](ctx, data)
		if err != nil {
			return nil, fmt.Errorf("unmarshal original config error: %s", err)
		}
		if options.Route != nil {
			originRules = options.Route.Rules
			originRuleSets = options.Route.RuleSet
		}
	}
	options.NTP = &option.NTPOptions{
		Enabled:       c.SingConfig.NtpConfig.Enable,
		WriteToSystem: true,
		ServerOptions: option.ServerOptions{
			Server:     c.SingConfig.NtpConfig.Server,
			ServerPort: c.SingConfig.NtpConfig.ServerPort,
		},
	}

	// 1. Build the static outbound set. Merge order: sing_origin.json, then the
	// local CustomOutbounds (a duplicate tag replaces the origin one and logs a
	// warning). Without origin outbounds FNode adds builtin direct (5s timeout,
	// prefer_ipv4, to prevent hangs on IPv4-only hosts) and block.
	connectTimeout := 5 * time.Second
	if c.SingConfig.ConnectTimeout > 0 {
		connectTimeout = time.Duration(c.SingConfig.ConnectTimeout) * time.Second
	}
	domainStrategy := ConvertStrategy(c.SingConfig.DomainStrategy)

	var originEntries []outboundEntry
	for _, o := range options.Outbounds {
		e, err := newEntryFromOption(ctx, o, SourceOrigin)
		if err != nil {
			return nil, fmt.Errorf("marshal original outbound %q error: %s", o.Tag, err)
		}
		originEntries = append(originEntries, e)
	}
	localTags := make(map[string]bool)
	for _, co := range c.SingConfig.CustomOutbounds {
		if tag, ok := co["tag"].(string); ok {
			localTags[tag] = true
		}
	}
	rawLocal := c.SingConfig.CustomOutbounds
	if len(originEntries) == 0 {
		rawLocal = BuildDefaultOutbounds(connectTimeout, domainStrategy, "", c.SingConfig.CustomOutbounds)
	}
	var localEntries []outboundEntry
	for _, raw := range rawLocal {
		source := SourceLocal
		if tag, _ := raw["tag"].(string); !localTags[tag] {
			source = SourceBuiltin
		}
		e, err := newEntryFromRaw(ctx, raw, source, "")
		if err != nil {
			if source == SourceBuiltin {
				return nil, fmt.Errorf("unmarshal default outbounds error: %s", err)
			}
			logrus.WithFields(logrus.Fields{"outbound": raw["tag"], "err": err}).Warn("Skip invalid local CustomOutbound")
			continue
		}
		localEntries = append(localEntries, e)
	}
	baseOutbounds, warnings := mergeOutbounds(originEntries, localEntries)
	for _, w := range warnings {
		logrus.Warn(w)
	}
	options.Outbounds = make([]option.Outbound, 0, len(baseOutbounds))
	appliedOutbounds := make(map[string]string, len(baseOutbounds))
	lastRaw := make(map[string]map[string]any, len(baseOutbounds))
	for _, e := range baseOutbounds {
		options.Outbounds = append(options.Outbounds, e.Option)
		appliedOutbounds[e.Tag] = e.Hash
		lastRaw[e.Tag] = e.Raw
	}

	// 2. Build default routing rules (anti-SSRF and IPv6 disable if configured)
	autoDetect := true
	if c.SingConfig != nil && c.SingConfig.AutoDetectInterface != nil {
		autoDetect = *c.SingConfig.AutoDetectInterface
	}
	if options.Route == nil {
		options.Route = &option.RouteOptions{
			Final:               "direct",
			AutoDetectInterface: autoDetect,
		}
	} else if c.SingConfig != nil && c.SingConfig.AutoDetectInterface != nil {
		options.Route.AutoDetectInterface = *c.SingConfig.AutoDetectInterface
	} else if !options.Route.AutoDetectInterface {
		options.Route.AutoDetectInterface = autoDetect
	}
	// Ensure Anti-SSRF, IPv6 blocking and custom config rules precede origin rules at startup
	rawRules := CompileRouteRules(c.SingConfig.DisableIPv6, nil, c.SingConfig.CustomRouteRules)
	rulesData, err := json.Marshal(rawRules)
	if err != nil {
		return nil, fmt.Errorf("marshal default rules error: %s", err)
	}
	compiledRules, err := json.UnmarshalExtendedContext[[]option.Rule](ctx, rulesData)
	if err != nil {
		return nil, fmt.Errorf("unmarshal default rules error: %s", err)
	}
	options.Route.Rules = append(compiledRules, originRules...)

	var endpointTags []string
	for _, ep := range options.Endpoints {
		endpointTags = append(endpointTags, ep.Tag)
	}

	// sing-box logs through a capture (core/sing/logcapture.go) so FNode can
	// demote REALITY probe noise and ship logs to the panel. The destination
	// and level the user configured are honoured by the capture.
	capture, err := newLogCapture(&c.SingConfig.LogConfig)
	if err != nil {
		return nil, err
	}
	logLevel := c.SingConfig.LogConfig.Level
	if c.SingConfig.LogConfig.Disabled {
		logLevel = "error"
	}
	options.Log = &option.LogOptions{
		Level:     logLevel,
		Timestamp: c.SingConfig.LogConfig.Timestamp,
		Output:    "stderr",
	}
	os.Setenv("SING_DNS_PATH", "")
	restoreStderr := capture.install()
	b, err := box.New(box.Options{
		Context: ctx,
		Options: options,
	})
	restoreStderr()
	if err != nil {
		capture.Close()
		return nil, err
	}
	capture.start()
	hs := &HookServer{
		counter: sync.Map{},
	}
	b.Router().AppendTracker(hs)
	return &Sing{
		ctx:        ctx,
		box:        b,
		hookServer: hs,
		router:     b.Router(),
		logFactory: b.LogFactory(),
		users: &UserMap{
			uidMap: make(map[string]int),
		},
		inboundUsers:              make(map[string][]panel.UserInfo),
		inboundInfo:               make(map[string]*panel.NodeInfo),
		inboundConfig:             make(map[string]*conf.Options),
		inboundOptions:            make(map[string]option.Inbound),
		nodeReportMinTrafficBytes: make(map[string]int64),
		coreConfig:                c,
		originRules:               originRules,
		originRuleSets:            originRuleSets,
		routeFinal:                options.Route.Final,
		endpointTags:              endpointTags,
		capture:                   capture,
		baseOutbounds:             baseOutbounds,
		appliedOutbounds:          appliedOutbounds,
		lastRaw:                   lastRaw,
	}, nil
}

// UpdateRouterRules recompiles the route rules from every node's panel routes
// and applies the panel outbounds (without removing stale ones).
func (b *Sing) UpdateRouterRules() error {
	return b.applyPanelState(false)
}

func (b *Sing) Start() error {
	return b.box.Start()
}

func (b *Sing) Close() error {
	err := b.box.Close()
	if b.capture != nil {
		b.capture.Close()
	}
	return err
}

func (b *Sing) Protocols() []string {
	return []string{
		"vmess",
		"vless",
		"shadowsocks",
		"trojan",
		"tuic",
		"anytls",
		"hysteria",
		"hysteria2",
	}
}

func (b *Sing) Type() string {
	return "sing"
}
