package node

import (
	"os"
	"path/filepath"
	"testing"
)

func Test_generateSelfSslCertificate(t *testing.T) {
	t.Log(generateSelfSslCertificate("domain.com", "1.pem", "1.key"))
}

func Test_findCaddyCertificate(t *testing.T) {
	// Empty domain
	crt, key := findCaddyCertificate("")
	if crt != "" || key != "" {
		t.Fatalf("expected empty for empty domain, got cert=%s, key=%s", crt, key)
	}

	// Non-existent domain
	crt, key = findCaddyCertificate("nonexistent.example.org")
	if crt != "" || key != "" {
		t.Fatalf("expected empty for non-existent domain, got cert=%s, key=%s", crt, key)
	}

	// Setup fake caddy cert in user home directory
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		testDomain := "fnode-test-multi.example.com"
		certDir := filepath.Join(home, ".local", "share", "caddy", "certificates", "acme-v02.api.letsencrypt.org-directory", testDomain)
		err := os.MkdirAll(certDir, 0755)
		if err == nil {
			fakeCrt := filepath.Join(certDir, testDomain+".crt")
			fakeKey := filepath.Join(certDir, testDomain+".key")
			_ = os.WriteFile(fakeCrt, []byte("fake-cert"), 0644)
			_ = os.WriteFile(fakeKey, []byte("fake-key"), 0644)
			defer os.RemoveAll(filepath.Join(home, ".local", "share", "caddy", "certificates", "acme-v02.api.letsencrypt.org-directory", testDomain))

			// Test single domain lookup
			foundCrt, foundKey := findCaddyCertificate(testDomain)
			if foundCrt == "" || foundKey == "" {
				t.Fatalf("expected to find fake caddy cert for %s", testDomain)
			}

			// Test multi-domain comma-separated lookup
			multiDomainStr := "other.domain.com, " + testDomain + ", third.domain.com"
			foundCrtMulti, foundKeyMulti := findCaddyCertificate(multiDomainStr)
			if foundCrtMulti != foundCrt || foundKeyMulti != foundKey {
				t.Fatalf("expected multi-domain lookup to find %s, got crt=%s, key=%s", testDomain, foundCrtMulti, foundKeyMulti)
			}
		}
	}
}

