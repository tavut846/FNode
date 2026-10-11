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
	nodeReportMinTrafficBytes map[string]int64
	coreConfig                *conf.CoreConfig
	originRules               []option.Rule
	originRuleSets            []option.RuleSet
	mu                        sync.RWMutex
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
	options.Log = &option.LogOptions{
		Disabled:  c.SingConfig.LogConfig.Disabled,
		Level:     c.SingConfig.LogConfig.Level,
		Timestamp: c.SingConfig.LogConfig.Timestamp,
		Output:    c.SingConfig.LogConfig.Output,
	}
	options.NTP = &option.NTPOptions{
		Enabled:       c.SingConfig.NtpConfig.Enable,
		WriteToSystem: true,
		ServerOptions: option.ServerOptions{
			Server:     c.SingConfig.NtpConfig.Server,
			ServerPort: c.SingConfig.NtpConfig.ServerPort,
		},
	}

	// 1. Build default outbounds (direct with 5s timeout & prefer_ipv4 to prevent hangs on IPv4-only hosts)
	connectTimeout := 5 * time.Second
	if c.SingConfig.ConnectTimeout > 0 {
		connectTimeout = time.Duration(c.SingConfig.ConnectTimeout) * time.Second
	}
	domainStrategy := ConvertStrategy(c.SingConfig.DomainStrategy)

	if len(options.Outbounds) == 0 {
		rawOutbounds := BuildDefaultOutbounds(connectTimeout, domainStrategy, "", c.SingConfig.CustomOutbounds)
		outboundsData, err := json.Marshal(rawOutbounds)
		if err != nil {
			return nil, fmt.Errorf("marshal default outbounds error: %s", err)
		}
		options.Outbounds, err = json.UnmarshalExtendedContext[[]option.Outbound](ctx, outboundsData)
		if err != nil {
			return nil, fmt.Errorf("unmarshal default outbounds error: %s", err)
		}
	} else if len(c.SingConfig.CustomOutbounds) > 0 {
		customOutboundsData, err := json.Marshal(c.SingConfig.CustomOutbounds)
		if err == nil {
			if customOutbounds, err := json.UnmarshalExtendedContext[[]option.Outbound](ctx, customOutboundsData); err == nil {
				options.Outbounds = append(options.Outbounds, customOutbounds...)
			}
		}
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
	if len(options.Route.Rules) == 0 {
		rawRules := CompileRouteRules(c.SingConfig.DisableIPv6, nil, c.SingConfig.CustomRouteRules)
		rulesData, err := json.Marshal(rawRules)
		if err != nil {
			return nil, fmt.Errorf("marshal default rules error: %s", err)
		}
		options.Route.Rules, err = json.UnmarshalExtendedContext[[]option.Rule](ctx, rulesData)
		if err != nil {
			return nil, fmt.Errorf("unmarshal default rules error: %s", err)
		}
	} else {
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
	}

	os.Setenv("SING_DNS_PATH", "")
	b, err := box.New(box.Options{
		Context: ctx,
		Options: options,
	})
	if err != nil {
		return nil, err
	}
	hs := &HookServer{
		counter: sync.Map{},
	}
	b.Router().AppendTracker(hs)
	return &Sing{
		ctx:                       ctx,
		box:                       b,
		hookServer:                hs,
		router:                    b.Router(),
		logFactory:                b.LogFactory(),
		users: &UserMap{
			uidMap: make(map[string]int),
		},
		inboundUsers:              make(map[string][]panel.UserInfo),
		inboundInfo:               make(map[string]*panel.NodeInfo),
		inboundConfig:             make(map[string]*conf.Options),
		nodeReportMinTrafficBytes: make(map[string]int64),
		coreConfig:                c,
		originRules:               originRules,
		originRuleSets:            originRuleSets,
	}, nil
}

func (b *Sing) UpdateRouterRules() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var allPanelRoutes []panel.Route
	disableIPv6 := false
	if b.coreConfig != nil && b.coreConfig.SingConfig.DisableIPv6 {
		disableIPv6 = true
	}

	for _, info := range b.inboundInfo {
		if info != nil && info.Common != nil {
			allPanelRoutes = append(allPanelRoutes, info.Common.Routes...)
		}
	}

	for _, cfg := range b.inboundConfig {
		if cfg != nil {
			if cfg.DisableIPv6 || (cfg.SingOptions != nil && cfg.SingOptions.DisableIPv6) {
				disableIPv6 = true
			}
		}
	}

	var customRules []conf.CustomRouteRule
	if b.coreConfig != nil {
		customRules = b.coreConfig.SingConfig.CustomRouteRules
	}

	rawRules := CompileRouteRules(disableIPv6, allPanelRoutes, customRules)
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
		return ur.UpdateRules(allRules, b.originRuleSets)
	}
	return nil
}

func (b *Sing) Start() error {
	return b.box.Start()
}

func (b *Sing) Close() error {
	return b.box.Close()
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
