package node

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/conf"
)

type recordSender struct {
	mu   sync.Mutex
	last []logship.Entry
}

func (r *recordSender) SendLogs(_ context.Context, e []logship.Entry) (logship.Response, error) {
	r.mu.Lock()
	r.last = e
	r.mu.Unlock()
	return logship.Response{LogMode: "warn", FlushInterval: 10}, nil
}

func logModeEvent(t *testing.T, raw string) *panel.WSEvent {
	t.Helper()
	_, ev, err := panel.ParseWSMessage([]byte(raw))
	if err != nil || ev == nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return ev
}

// custom_node.log_mode on the machine WebSocket: node_id null is the whole
// machine, a node id affects that node only.
func TestMachine_LogModeEvents(t *testing.T) {
	m, _, _ := newTestMachine(t)
	m.shipper = logship.NewShipper(&recordSender{})

	m.handleWSEvent(logModeEvent(t, `{"event":"custom_node.log_mode","data":{"mode":"info","node_id":null}}`))
	if m.shipper.Mode() != logship.Info {
		t.Fatalf("machine-wide mode: %v", m.shipper.Mode())
	}
	m.handleWSEvent(logModeEvent(t, `{"event":"custom_node.log_mode","data":{"mode":"debug","node_id":12}}`))
	if m.shipper.Mode() != logship.Info || m.shipper.MaxLevel() != logship.Debug {
		t.Fatalf("node override: base %v max %v", m.shipper.Mode(), m.shipper.MaxLevel())
	}
	m.shipper.Add(logship.Debug, logship.Entry{NodeID: 12, Message: "node 12"})
	m.shipper.Add(logship.Debug, logship.Entry{NodeID: 13, Message: "node 13"})
	if q, _ := m.shipper.Buffered(); q != 1 {
		t.Errorf("only node 12 ships debug, queued %d", q)
	}
	m.handleWSEvent(logModeEvent(t, `{"event":"custom_node.log_mode","data":{"mode":"off","node_id":null}}`))
	if m.shipper.MaxLevel() != logship.Off {
		t.Errorf("off: %v", m.shipper.MaxLevel())
	}
}

// A standalone node applies the event to its own shipper.
func TestController_LogModeEvent(t *testing.T) {
	apiClient, err := panel.New(&conf.ApiConfig{APIHost: "http://127.0.0.1:1", NodeID: 7, NodeType: "vless", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	c := &Controller{apiClient: apiClient, tag: "t", Options: &conf.Options{}, shipper: logship.NewShipper(&recordSender{})}

	c.handleWSEvent(logModeEvent(t, `{"event":"custom_node.log_mode","data":{"mode":"info","node_id":null}}`))
	if c.shipper.Mode() != logship.Info {
		t.Errorf("mode: %v", c.shipper.Mode())
	}
	c.handleWSEvent(logModeEvent(t, `{"event":"custom_node.log_mode","data":{"mode":"debug","node_id":99}}`))
	if c.shipper.MaxLevel() != logship.Info {
		t.Errorf("an event for another node must be ignored: %v", c.shipper.MaxLevel())
	}
	c.handleWSEvent(logModeEvent(t, `{"event":"custom_node.log_mode","data":{"mode":"debug","node_id":7}}`))
	if c.shipper.MaxLevel() != logship.Debug || c.shipper.Mode() != logship.Info {
		t.Errorf("own node override: base %v max %v", c.shipper.Mode(), c.shipper.MaxLevel())
	}
}

// A controller maps its node tag to the shipper (machine-provided or its own)
// and follows tag changes.
func TestController_LogsMapping(t *testing.T) {
	apiClient, err := panel.New(&conf.ApiConfig{APIHost: "http://127.0.0.1:1", NodeID: 7, NodeType: "vless", Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	c := &Controller{apiClient: apiClient, tag: "map-test-node", Options: &conf.Options{}}
	c.startLogs()
	if c.shipper == nil || !c.ownShipper {
		t.Fatal("a standalone node creates its own shipper")
	}
	logship.Default.Emit(logship.Error, "map-test-node", "map-test-node", "for this node", timeNow())
	if q, _ := c.shipper.Buffered(); q != 1 || c.shipper.MaxLevel() != logship.DefaultMode {
		t.Errorf("entry routed to the node's shipper: queued %d", q)
	}
	c.stopLogs()
	if logship.Default.Floor() != logship.Off {
		t.Errorf("stopLogs unregisters the shipper: floor %v", logship.Default.Floor())
	}
}

func timeNow() time.Time { return time.Now() }
