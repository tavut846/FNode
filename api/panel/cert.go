package panel

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// CertConfig is the per-node certificate setting the panel sends as
// "cert_config" (xboard-node shape). Older panel data uses "mode" instead of
// "cert_mode". The panel omits it when the mode is none.
type CertConfig struct {
	CertMode    string            `json:"cert_mode"` // none, file, self, http, dns, content
	Domain      string            `json:"domain"`
	Email       string            `json:"email"`
	DNSProvider string            `json:"dns_provider"`
	DNSEnv      map[string]string `json:"dns_env"`
	HTTPPort    int               `json:"http_port"`
	CertFile    string            `json:"cert_file"`
	KeyFile     string            `json:"key_file"`
	CertContent string            `json:"cert_content"`
	KeyContent  string            `json:"key_content"`
}

// UnmarshalJSON is lenient: PHP sends [] for an empty object, and numbers may
// arrive as strings. A value that is not an object leaves the config empty
// instead of failing the whole node config.
func (c *CertConfig) UnmarshalJSON(data []byte) error {
	*c = CertConfig{}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			switch v := raw[k].(type) {
			case string:
				if s := strings.TrimSpace(v); s != "" {
					return s
				}
			case float64:
				return strconv.FormatFloat(v, 'f', -1, 64)
			}
		}
		return ""
	}
	c.CertMode = strings.ToLower(str("cert_mode", "mode"))
	c.Domain = str("domain", "cert_domain")
	c.Email = str("email")
	c.DNSProvider = str("dns_provider", "provider")
	c.CertFile = str("cert_file")
	c.KeyFile = str("key_file")
	// PEM content keeps its own whitespace
	if v, ok := raw["cert_content"].(string); ok {
		c.CertContent = v
	}
	if v, ok := raw["key_content"].(string); ok {
		c.KeyContent = v
	}
	if p := str("http_port"); p != "" {
		c.HTTPPort, _ = strconv.Atoi(p)
	}
	if env, ok := raw["dns_env"].(map[string]any); ok && len(env) > 0 {
		c.DNSEnv = make(map[string]string, len(env))
		for k, v := range env {
			c.DNSEnv[k] = fmt.Sprint(v)
		}
	}
	return nil
}

// Enabled reports whether the panel manages this node's certificate.
func (c *CertConfig) Enabled() bool {
	return c != nil && c.CertMode != "" && c.CertMode != "none"
}

// PanelCert returns the panel cert_config of a node, or nil when the panel
// does not manage its certificate.
func (n *NodeInfo) PanelCert() *CertConfig {
	if n == nil || n.Common == nil || !n.Common.CertConfig.Enabled() {
		return nil
	}
	return n.Common.CertConfig
}
