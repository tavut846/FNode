package conf

import (
	"testing"
)

func TestNewSingConfig_Defaults(t *testing.T) {
	cfg := NewSingConfig()
	if cfg == nil {
		t.Fatal("NewSingConfig returned nil")
	}
	if !cfg.DisableIPv6 {
		t.Errorf("expected DisableIPv6 to be true by default, got false")
	}
	if cfg.DomainStrategy != "prefer_ipv4" {
		t.Errorf("expected DomainStrategy to be prefer_ipv4, got %s", cfg.DomainStrategy)
	}
	if cfg.ConnectTimeout != 5 {
		t.Errorf("expected ConnectTimeout to be 5, got %d", cfg.ConnectTimeout)
	}
	if !cfg.NtpConfig.Enable {
		t.Errorf("expected NTP Enable to be true, got false")
	}
}
