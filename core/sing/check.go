package sing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"

	"github.com/tavut846/FNode/conf"
)

// CheckConfig is sing-box's own validation of a FNode config: every sing core's
// sing_origin.json is parsed strictly and a box is built from it (outbounds,
// DNS servers, rules and rule-sets resolved) without starting or binding anything.
// A missing origin file is fine: FNode then uses its built-in defaults.
func CheckConfig(configPath string) error {
	c := conf.New()
	if err := c.LoadFromPath(configPath); err != nil {
		return err
	}
	for _, core := range c.CoresConfig {
		if core.SingConfig == nil || core.SingConfig.OriginalPath == "" {
			continue
		}
		p := core.SingConfig.OriginalPath
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(configPath), p)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if err = CheckOrigin(data); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

// CheckOrigin validates sing_origin.json content with sing-box itself.
func CheckOrigin(data []byte) error {
	ctx := box.Context(context.Background(), include.InboundRegistry(), include.OutboundRegistry(), include.EndpointRegistry(),
		include.DNSTransportRegistry(), include.ServiceRegistry(), include.CertificateProviderRegistry())
	options, err := json.UnmarshalExtendedContext[option.Options](ctx, data)
	if err != nil {
		return err
	}
	options.Log = &option.LogOptions{Disabled: true}
	options.NTP = nil
	options.Inbounds = nil
	options.Services = nil
	b, err := box.New(box.Options{Context: ctx, Options: options})
	if err != nil {
		return err
	}
	_ = b.Close()
	return nil
}

