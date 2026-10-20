//go:build !with_all_dns

package node

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/providers/dns/bunny"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/desec"
	"github.com/go-acme/lego/v4/providers/dns/digitalocean"
	"github.com/go-acme/lego/v4/providers/dns/dnspod"
	"github.com/go-acme/lego/v4/providers/dns/duckdns"
	"github.com/go-acme/lego/v4/providers/dns/dynu"
	"github.com/go-acme/lego/v4/providers/dns/gandi"
	"github.com/go-acme/lego/v4/providers/dns/gandiv5"
	"github.com/go-acme/lego/v4/providers/dns/godaddy"
	"github.com/go-acme/lego/v4/providers/dns/hetzner"
	"github.com/go-acme/lego/v4/providers/dns/hurricane"
	"github.com/go-acme/lego/v4/providers/dns/ionos"
	"github.com/go-acme/lego/v4/providers/dns/namecheap"
	"github.com/go-acme/lego/v4/providers/dns/namesilo"
	"github.com/go-acme/lego/v4/providers/dns/netcup"
	"github.com/go-acme/lego/v4/providers/dns/ovh"
	"github.com/go-acme/lego/v4/providers/dns/porkbun"
	"github.com/go-acme/lego/v4/providers/dns/rfc2136"
	"github.com/go-acme/lego/v4/providers/dns/tencentcloud"
	"github.com/go-acme/lego/v4/providers/dns/vultr"
)

// dnsProviders are the DNS-01 providers in the default build. Linking all of
// lego's providers (with_all_dns) roughly doubles the binary, which matters
// on small VPSes. Each constructor reads its credentials from the
// environment variables set from CertConfig.DNSEnv.
var dnsProviders = map[string]func() (challenge.Provider, error){
	"alidns":       func() (challenge.Provider, error) { return alidns.NewDNSProvider() },
	"bunny":        func() (challenge.Provider, error) { return bunny.NewDNSProvider() },
	"cloudflare":   func() (challenge.Provider, error) { return cloudflare.NewDNSProvider() },
	"desec":        func() (challenge.Provider, error) { return desec.NewDNSProvider() },
	"digitalocean": func() (challenge.Provider, error) { return digitalocean.NewDNSProvider() },
	"dnspod":       func() (challenge.Provider, error) { return dnspod.NewDNSProvider() },
	"duckdns":      func() (challenge.Provider, error) { return duckdns.NewDNSProvider() },
	"dynu":         func() (challenge.Provider, error) { return dynu.NewDNSProvider() },
	"gandi":        func() (challenge.Provider, error) { return gandi.NewDNSProvider() },
	"gandiv5":      func() (challenge.Provider, error) { return gandiv5.NewDNSProvider() },
	"godaddy":      func() (challenge.Provider, error) { return godaddy.NewDNSProvider() },
	"hetzner":      func() (challenge.Provider, error) { return hetzner.NewDNSProvider() },
	"hurricane":    func() (challenge.Provider, error) { return hurricane.NewDNSProvider() },
	"ionos":        func() (challenge.Provider, error) { return ionos.NewDNSProvider() },
	"namecheap":    func() (challenge.Provider, error) { return namecheap.NewDNSProvider() },
	"namesilo":     func() (challenge.Provider, error) { return namesilo.NewDNSProvider() },
	"netcup":       func() (challenge.Provider, error) { return netcup.NewDNSProvider() },
	"ovh":          func() (challenge.Provider, error) { return ovh.NewDNSProvider() },
	"porkbun":      func() (challenge.Provider, error) { return porkbun.NewDNSProvider() },
	"rfc2136":      func() (challenge.Provider, error) { return rfc2136.NewDNSProvider() },
	"tencentcloud": func() (challenge.Provider, error) { return tencentcloud.NewDNSProvider() },
	"vultr":        func() (challenge.Provider, error) { return vultr.NewDNSProvider() },
}

func newDNSProvider(name string) (challenge.Provider, error) {
	f, ok := dnsProviders[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		names := make([]string, 0, len(dnsProviders))
		for n := range dnsProviders {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("dns provider %q is not in this FNode build (have: %s); use CertMode \"file\" with Caddy, or a build with the with_all_dns tag",
			name, strings.Join(names, ", "))
	}
	return f()
}
