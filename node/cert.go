package node

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path"
	"strings"
	"time"

	"github.com/tavut846/FNode/common/file"
	log "github.com/sirupsen/logrus"
)

func findCaddyCertificate(domain string) (string, string) {
	if domain == "" {
		return "", ""
	}
	domains := strings.Split(domain, ",")
	for _, rawD := range domains {
		d := strings.TrimSpace(rawD)
		if d == "" {
			continue
		}
		candidates := []string{
			fmt.Sprintf("/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/%s", d),
			fmt.Sprintf("/root/.local/share/caddy/certificates/acme.zerossl.com-v2-dv90/%s", d),
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/root" {
			candidates = append(candidates,
				fmt.Sprintf("%s/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/%s", home, d),
				fmt.Sprintf("%s/.local/share/caddy/certificates/acme.zerossl.com-v2-dv90/%s", home, d),
			)
		}
		candidates = append(candidates,
			fmt.Sprintf("/var/lib/caddy/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/%s", d),
			fmt.Sprintf("/var/lib/caddy/.local/share/caddy/certificates/acme.zerossl.com-v2-dv90/%s", d),
		)

		for _, dir := range candidates {
			crt := path.Join(dir, d+".crt")
			key := path.Join(dir, d+".key")
			if file.IsExist(crt) && file.IsExist(key) {
				return crt, key
			}
		}
	}
	return "", ""
}

func (c *Controller) renewCertTask() error {
	switch c.CertConfig.CertMode {
	case "none", "", "file", "self":
		return nil
	}
	l, err := NewLego(c.CertConfig)
	if err != nil {
		log.WithField("tag", c.tag).Info("new lego error: ", err)
		return nil
	}
	err = l.RenewCert()
	if err != nil {
		log.WithField("tag", c.tag).Info("renew cert error: ", err)
		return nil
	}
	return nil
}

func (c *Controller) requestCert() error {
	switch c.CertConfig.CertMode {
	case "none", "":
	case "file":
		// If cert or key file is not explicitly set, or not found, try auto-detecting from Caddy
		if (c.CertConfig.CertFile == "" || c.CertConfig.KeyFile == "" || !file.IsExist(c.CertConfig.CertFile) || !file.IsExist(c.CertConfig.KeyFile)) && c.CertConfig.CertDomain != "" {
			caddyCert, caddyKey := findCaddyCertificate(c.CertConfig.CertDomain)
			if caddyCert != "" && caddyKey != "" {
				c.CertConfig.CertFile = caddyCert
				c.CertConfig.KeyFile = caddyKey
				log.WithField("tag", c.tag).Infof("auto-detected Caddy certificates for domain %s: cert=%s, key=%s", c.CertConfig.CertDomain, caddyCert, caddyKey)
			}
		}
		if c.CertConfig.CertFile == "" || c.CertConfig.KeyFile == "" {
			return fmt.Errorf("cert file path or key file path not specified")
		}
		if !file.IsExist(c.CertConfig.CertFile) {
			return fmt.Errorf("cert file not found: %s", c.CertConfig.CertFile)
		}
		if !file.IsExist(c.CertConfig.KeyFile) {
			return fmt.Errorf("key file not found: %s", c.CertConfig.KeyFile)
		}
	case "dns", "http":
		if c.CertConfig.CertFile == "" || c.CertConfig.KeyFile == "" {
			return fmt.Errorf("cert file path or key file path not exist")
		}
		if file.IsExist(c.CertConfig.CertFile) && file.IsExist(c.CertConfig.KeyFile) {
			return nil
		}
		l, err := NewLego(c.CertConfig)
		if err != nil {
			return fmt.Errorf("create lego object error: %s", err)
		}
		err = l.CreateCert()
		if err != nil {
			return fmt.Errorf("create lego cert error: %s", err)
		}
	case "self":
		if c.CertConfig.CertFile == "" || c.CertConfig.KeyFile == "" {
			return fmt.Errorf("cert file path or key file path not exist")
		}
		if file.IsExist(c.CertConfig.CertFile) && file.IsExist(c.CertConfig.KeyFile) {
			return nil
		}
		err := generateSelfSslCertificate(
			c.CertConfig.CertDomain,
			c.CertConfig.CertFile,
			c.CertConfig.KeyFile)
		if err != nil {
			return fmt.Errorf("generate self cert error: %s", err)
		}
	default:
		return fmt.Errorf("unsupported certmode: %s", c.CertConfig.CertMode)
	}
	return nil
}

func generateSelfSslCertificate(domain, certPath, keyPath string) error {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{
		Version:      3,
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject: pkix.Name{
			CommonName: domain,
		},
		DNSNames:              []string{domain},
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(30, 0, 0),
	}
	cert, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(certPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	err = pem.Encode(f, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	})
	if err != nil {
		return err
	}
	f, err = os.OpenFile(keyPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	err = pem.Encode(f, &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err != nil {
		return err
	}
	return nil
}
