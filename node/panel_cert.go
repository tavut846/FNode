package node

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

// panelCertDir holds certificates FNode writes for a dashboard cert_config:
// the PEM of content mode, and self/http/dns certs without a path.
var panelCertDir = "/etc/FNode/panel_cert"

// certConfigFor returns the certificate config of a node: the dashboard's
// cert_config when the panel sets one (mode not none), otherwise a copy of
// the local Options.CertConfig. fromPanel tells which.
func (c *Controller) certConfigFor(node *panel.NodeInfo) (cc *conf.CertConfig, fromPanel bool, err error) {
	pc := node.PanelCert()
	if pc == nil {
		if c.localCert == nil {
			return nil, false, fmt.Errorf("no CertConfig: set CertConfig in the local config or cert_config in the dashboard")
		}
		local := *c.localCert
		return &local, false, nil
	}
	cc = &conf.CertConfig{
		CertMode:   pc.CertMode,
		CertDomain: pc.Domain,
		CertFile:   pc.CertFile,
		KeyFile:    pc.KeyFile,
		Provider:   pc.DNSProvider,
		Email:      pc.Email,
		DNSEnv:     pc.DNSEnv,
		HTTPPort:   pc.HTTPPort,
	}
	if c.localCert != nil {
		cc.RejectUnknownSni = c.localCert.RejectUnknownSni
		if cc.Email == "" {
			cc.Email = c.localCert.Email
		}
	}
	if cc.CertDomain == "" && node.Common != nil {
		cc.CertDomain = node.Common.ServerName
	}
	stem := filepath.Join(panelCertDir, fmt.Sprintf("node-%d", node.Id))
	switch cc.CertMode {
	case "content":
		if pc.CertContent == "" || pc.KeyContent == "" {
			return nil, true, fmt.Errorf("content mode needs cert_content and key_content")
		}
		cc.CertMode = "file"
		cc.CertFile, cc.KeyFile = stem+".crt", stem+".key"
		if err = writeIfChanged(cc.CertFile, pc.CertContent, 0o644); err == nil {
			err = writeIfChanged(cc.KeyFile, pc.KeyContent, 0o600)
		}
		if err != nil {
			return nil, true, fmt.Errorf("write cert_content: %w", err)
		}
	case "self", "http", "dns":
		if cc.CertFile == "" || cc.KeyFile == "" {
			cc.CertFile, cc.KeyFile = stem+".crt", stem+".key"
		}
	}
	return cc, true, nil
}

// writeIfChanged writes data to path unless it already holds exactly that.
func writeIfChanged(path, data string, mode os.FileMode) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, []byte(data)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(data), mode)
}

// requestCert prepares the certificate of a TLS node from the dashboard
// cert_config or the local CertConfig and, on success, makes it the node's
// CertConfig (the core reads the cert paths from there). An error names the
// node, the missing path and where the setting came from.
func (c *Controller) requestCert(node *panel.NodeInfo) error {
	cc, fromPanel, err := c.certConfigFor(node)
	if err == nil {
		err = c.obtainCert(cc)
	}
	if err != nil {
		if fromPanel {
			return fmt.Errorf("node %d: dashboard cert_config (%s mode): %w; the node starts once this is fixed",
				node.Id, node.PanelCert().CertMode, err)
		}
		return err
	}
	c.Options.CertConfig = cc
	return nil
}
