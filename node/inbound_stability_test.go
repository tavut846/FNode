package node

import (
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus/hooks/test"

	"github.com/tavut846/FNode/conf"
)

const (
	stableBase = `"protocol":"anytls","server_port":443,"server_name":"a.example"`
	routesA    = `"routes":[{"id":1,"match":["x.com"],"action":"block"}]`
	routesB    = `"routes":[{"id":1,"match":["y.com"],"action":"block"}]`
)

// The panel sends the same config twice in different shapes: over HTTP with
// base_config (the intervals) and over the WebSocket (full sync on every
// connect) without it. That must never rebuild the inbound: a rebuild drops
// every live connection, and idle AnyTLS/Hysteria2 clients only notice later.
func TestController_ConfigShapeDoesNotRebuildInbound(t *testing.T) {
	c, core, client := certTestController(t, &conf.CertConfig{CertMode: "none"})
	hook := test.NewGlobal()
	defer hook.Reset()

	httpBody := func(routes string, push, pull int) []byte {
		return []byte(`{` + stableBase + `,` + routes + `,"base_config":{"push_interval":` + itoa(push) + `,"pull_interval":` + itoa(pull) + `}}`)
	}
	wsBody := func(routes string) []byte { return []byte(`{` + stableBase + `,` + routes + `}`) }
	apply := func(body []byte) {
		t.Helper()
		n, err := client.ParseNodeConfig(body)
		if err != nil {
			t.Fatal(err)
		}
		if n == nil {
			return
		}
		c.mu.Lock()
		_ = c.applyUpdate(n, nil, nil)
		c.mu.Unlock()
	}
	rebuilds := func() int { return core.delNode + core.addNode }

	// startup: the HTTP config, then the WebSocket's full sync of the same config
	first, err := client.ParseNodeConfig(httpBody(routesA, 60, 60))
	if err != nil || first == nil {
		t.Fatalf("parse: %v", err)
	}
	c.info = first
	c.nodeInfoMonitorPeriodic.Interval = first.PullInterval
	apply(wsBody(routesA))
	if rebuilds() != 0 || core.routing != 0 {
		t.Fatalf("the WebSocket full sync of an unchanged config touched the node: rebuilds=%d routing=%d", rebuilds(), core.routing)
	}
	if c.info.PullInterval != 60*time.Second {
		t.Errorf("the intervals were forgotten: %v", c.info.PullInterval)
	}

	// a routing change over the WebSocket: routes only, no rebuild
	apply(wsBody(routesB))
	if rebuilds() != 0 || core.routing != 1 {
		t.Fatalf("a routing change must not rebuild the inbound: rebuilds=%d routing=%d", rebuilds(), core.routing)
	}

	// the same config over HTTP again with other intervals: intervals apply, no rebuild
	apply(httpBody(routesB, 30, 45))
	if rebuilds() != 0 {
		t.Fatalf("an interval change rebuilt the inbound")
	}
	if c.nodeInfoMonitorPeriodic.Interval != 45*time.Second || c.userReportPeriodic.Interval != 30*time.Second {
		t.Errorf("intervals not applied: pull=%v push=%v", c.nodeInfoMonitorPeriodic.Interval, c.userReportPeriodic.Interval)
	}

	// a real inbound change rebuilds once and says why
	apply(wsBody(strings.Replace(routesB, "y.com", "z.com", 1)))
	apply([]byte(`{` + strings.Replace(stableBase, "443", "444", 1) + `,` + routesB + `}`))
	if core.delNode != 1 || core.addNode != 1 {
		t.Fatalf("a port change must rebuild the inbound exactly once: del=%d add=%d", core.delNode, core.addNode)
	}
	var logged bool
	for _, e := range hook.AllEntries() {
		if strings.Contains(e.Message, "recreating its inbound") && e.Data["reason"] == "inbound config changed" {
			logged = true
		}
	}
	if !logged {
		t.Error("the rebuild was not logged with its reason")
	}
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Replace(time.Duration(n).String(), "ns", "", 1))
}
