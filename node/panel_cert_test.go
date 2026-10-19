package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/task"
	"github.com/tavut846/FNode/conf"
	"github.com/tavut846/FNode/limiter"
)

func certTestController(t *testing.T, local *conf.CertConfig) (*Controller, *fakeCore, *panel.Client) {
	t.Helper()
	old := panelCertDir
	panelCertDir = t.TempDir()
	t.Cleanup(func() { panelCertDir = old })
	client, err := panel.New(&conf.ApiConfig{APIHost: "http://127.0.0.1:1", NodeID: 468, NodeType: "anytls", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	tag := "cert-test-" + t.Name()
	users := []panel.UserInfo{{Id: 1, Uuid: "u1"}}
	lim := limiter.AddLimiter(tag, &conf.LimitConfig{}, users, map[int]int{})
	t.Cleanup(func() { limiter.DeleteLimiter(tag) })
	core := &fakeCore{}
	c := NewController(core, client, &conf.Options{Name: tag, CertConfig: local})
	c.tag, c.limiter, c.userList = tag, lim, users
	c.info = &panel.NodeInfo{Id: 468, Type: "anytls", InboundHash: "old"}
	c.nodeInfoMonitorPeriodic = &task.Task{Interval: 60e9}
	c.userReportPeriodic = &task.Task{Interval: 60e9}
	return c, core, client
}

func anytlsConfig(certConfig string) []byte {
	return []byte(`{"server_port":443,"server_name":"a.example","base_config":{"push_interval":60,"pull_interval":60},"cert_config":` + certConfig + `}`)
}

// A dashboard cert_config whose files are missing keeps the running node,
// logs an error naming node, path and source, and is retried on the next
// config check until the files appear.
func TestController_PanelCertMissingThenPresent(t *testing.T) {
	c, core, client := certTestController(t, &conf.CertConfig{CertMode: "none", Email: "local@example"})
	dir := t.TempDir()
	crt, key := filepath.Join(dir, "a.crt"), filepath.Join(dir, "a.key")
	body := anytlsConfig(`{"cert_mode":"file","domain":"no-caddy.invalid","cert_file":"` + filepath.ToSlash(crt) + `","key_file":"` + filepath.ToSlash(key) + `"}`)

	hook := test.NewGlobal()
	defer hook.Reset()
	newN, err := client.ParseNodeConfig(body)
	if err != nil || newN == nil {
		t.Fatalf("parse: %v", err)
	}
	c.mu.Lock()
	_ = c.applyUpdate(newN, nil, nil)
	c.mu.Unlock()
	if core.addNode != 0 || core.delNode != 0 {
		t.Fatalf("node rebuilt without its certificate: add %d del %d", core.addNode, core.delNode)
	}
	if !c.certPending {
		t.Fatal("certPending not set")
	}
	var found bool
	for _, e := range hook.AllEntries() {
		msg := e.Message + " " + func() string {
			s, _ := e.Data["err"].(error)
			if s != nil {
				return s.Error()
			}
			return ""
		}()
		if e.Level == log.ErrorLevel && strings.Contains(msg, "node 468") && strings.Contains(msg, "dashboard cert_config") &&
			strings.Contains(msg, filepath.ToSlash(crt)) {
			found = true
		}
	}
	if !found {
		t.Errorf("no error log naming node, source and path: %+v", hook.AllEntries())
	}

	// Next check: the same config comes back (cache forgotten); files exist now.
	if err := os.WriteFile(crt, []byte("cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	newN, err = client.ParseNodeConfig(body)
	if err != nil || newN == nil {
		t.Fatalf("config not returned again after a cert failure: %v", err)
	}
	c.mu.Lock()
	_ = c.applyUpdate(newN, nil, nil)
	c.mu.Unlock()
	if core.addNode != 1 || c.certPending {
		t.Fatalf("node not started once the files exist: add %d pending %v", core.addNode, c.certPending)
	}
	cc := c.Options.CertConfig
	if cc.CertMode != "file" || cc.CertFile != filepath.ToSlash(crt) || cc.KeyFile != filepath.ToSlash(key) || cc.Email != "local@example" {
		t.Errorf("effective CertConfig: %+v", cc)
	}
	if c.localCert.CertMode != "none" {
		t.Errorf("local CertConfig changed: %+v", c.localCert)
	}
}

func TestController_PanelCertContentAndLocalFallback(t *testing.T) {
	c, _, client := certTestController(t, &conf.CertConfig{CertMode: "self", CertDomain: "local.example",
		CertFile: filepath.Join(t.TempDir(), "l.crt"), KeyFile: filepath.Join(t.TempDir(), "l.key")})

	n, err := client.ParseNodeConfig(anytlsConfig(`{"cert_mode":"content","cert_content":"-----CERT-----\n","key_content":"-----KEY-----\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.requestCert(n); err != nil {
		t.Fatalf("content mode: %v", err)
	}
	cc := c.Options.CertConfig
	if cc.CertMode != "file" || cc.CertDomain != "a.example" {
		t.Errorf("content mode config: %+v", cc)
	}
	if b, _ := os.ReadFile(cc.CertFile); string(b) != "-----CERT-----\n" {
		t.Errorf("cert content not written to %s: %q", cc.CertFile, b)
	}
	if b, _ := os.ReadFile(cc.KeyFile); string(b) != "-----KEY-----\n" {
		t.Errorf("key content not written to %s: %q", cc.KeyFile, b)
	}

	// No panel cert_config: the local one is used again.
	n, err = client.ParseNodeConfig([]byte(`{"server_port":443}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.requestCert(n); err != nil {
		t.Fatalf("local self mode: %v", err)
	}
	if cc := c.Options.CertConfig; cc.CertMode != "self" || cc.CertDomain != "local.example" {
		t.Errorf("local fallback: %+v", cc)
	}

	// Neither: a clear error instead of a nil dereference.
	c.localCert = nil
	if err := c.requestCert(n); err == nil || !strings.Contains(err.Error(), "no CertConfig") {
		t.Errorf("no cert config: %v", err)
	}
}
