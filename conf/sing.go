package conf

import (
	"github.com/sagernet/sing-box/option"
)

type SingConfig struct {
	LogConfig        SingLogConfig     `json:"Log"`
	NtpConfig        SingNtpConfig     `json:"NTP"`
	OriginalPath     string            `json:"OriginalPath"`
	ConnectTimeout   int               `json:"ConnectTimeout"` // dial timeout in seconds, default 5s
	DomainStrategy   string            `json:"DomainStrategy"` // "prefer_ipv4", "ipv4_only", "prefer_ipv6", "ipv6_only"
	DisableIPv6      bool              `json:"DisableIPv6"`    // block ipv6 traffic immediately to avoid timeouts on IPv4-only hosts
	CustomOutbounds  []map[string]any  `json:"CustomOutbounds"`
	CustomRouteRules []CustomRouteRule `json:"CustomRouteRules"`
}

type CustomRouteRule struct {
	Name     string            `json:"name,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
	Match    CustomRouteMatch  `json:"match"`
	Action   CustomRouteAction `json:"action"`
}

type CustomRouteMatch struct {
	Domains        []string `json:"domains,omitempty"`
	DomainSuffixes []string `json:"domain_suffixes,omitempty"`
	IPCIDRs        []string `json:"ip_cidrs,omitempty"`
	Ports          []string `json:"ports,omitempty"`
	Networks       []string `json:"networks,omitempty"`
	SourceCIDRs    []string `json:"source_cidrs,omitempty"`
	SourcePorts    []string `json:"source_ports,omitempty"`
}

type CustomRouteAction struct {
	Type   string `json:"type"`             // "direct", "block", "route"
	Target string `json:"target,omitempty"` // outbound tag when type is "route"
}

type SingLogConfig struct {
	Disabled  bool   `json:"Disable"`
	Level     string `json:"Level"`
	Output    string `json:"Output"`
	Timestamp bool   `json:"Timestamp"`
}

func NewSingConfig() *SingConfig {
	return &SingConfig{
		LogConfig: SingLogConfig{
			Level:     "error",
			Timestamp: true,
		},
		NtpConfig: SingNtpConfig{
			Enable:     true,
			Server:     "time.apple.com",
			ServerPort: 0,
		},
		ConnectTimeout: 5,
		DomainStrategy: "prefer_ipv4",
	}
}

type SingOptions struct {
	TCPFastOpen              bool                   `json:"EnableTFO"`
	SniffEnabled             bool                   `json:"EnableSniff"`
	SniffOverrideDestination bool                   `json:"SniffOverrideDestination"`
	EnableDNS                bool                   `json:"EnableDNS"`
	DomainStrategy           option.DomainStrategy  `json:"DomainStrategy"`
	FallBackConfigs          *FallBackConfigForSing `json:"FallBackConfigs"`
	Multiplex                *MultiplexConfig       `json:"MultiplexConfig"`
	DisableIPv6              bool                   `json:"DisableIPv6"`
	ConnectTimeout           int                    `json:"ConnectTimeout"`
}

type SingNtpConfig struct {
	Enable     bool   `json:"Enable"`
	Server     string `json:"Server"`
	ServerPort uint16 `json:"ServerPort"`
}

type FallBackConfigForSing struct {
	// sing-box
	FallBack        FallBack            `json:"FallBack"`
	FallBackForALPN map[string]FallBack `json:"FallBackForALPN"`
}

type FallBack struct {
	Server     string `json:"Server"`
	ServerPort string `json:"ServerPort"`
}

type MultiplexConfig struct {
	Enabled bool          `json:"Enable"`
	Padding bool          `json:"Padding"`
	Brutal  BrutalOptions `json:"Brutal"`
}

type BrutalOptions struct {
	Enabled  bool `json:"Enable"`
	UpMbps   int  `json:"UpMbps"`
	DownMbps int  `json:"DownMbps"`
}

func NewSingOptions() *SingOptions {
	return &SingOptions{
		EnableDNS:                false,
		TCPFastOpen:              false,
		SniffEnabled:             true,
		SniffOverrideDestination: true,
		FallBackConfigs:          &FallBackConfigForSing{},
		Multiplex:                &MultiplexConfig{},
	}
}
