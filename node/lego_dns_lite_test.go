//go:build !with_all_dns

package node

import (
	"strings"
	"testing"
)

// The default build links only a curated set of lego DNS providers (the
// full set doubles the binary). A listed provider is built from its env
// vars; an unlisted one fails with a message that says how to get it.
func TestNewDNSProvider_LiteBuild(t *testing.T) {
	t.Setenv("CF_DNS_API_TOKEN", "token")
	p, err := newDNSProvider(" Cloudflare ")
	if err != nil || p == nil {
		t.Fatalf("cloudflare: %v", err)
	}
	for name := range dnsProviders {
		if name != strings.ToLower(name) {
			t.Errorf("provider key %q must be lower case", name)
		}
	}

	_, err = newDNSProvider("route53")
	if err == nil {
		t.Fatal("route53 must not be in the lite build")
	}
	for _, want := range []string{"route53", "with_all_dns", "cloudflare", "alidns"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q misses %q", err, want)
		}
	}

	// a provider without its credentials reports the missing variable
	if _, err = newDNSProvider("alidns"); err == nil {
		t.Error("alidns without credentials must fail")
	}
}
