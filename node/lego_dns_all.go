//go:build with_all_dns

package node

import (
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns"
)

// newDNSProvider returns the lego DNS-01 provider called name, configured
// from the environment variables set from CertConfig.DNSEnv. This build
// (tag with_all_dns) links every lego provider; it is much larger.
func newDNSProvider(name string) (challenge.Provider, error) {
	return dns.NewDNSChallengeProviderByName(name)
}
