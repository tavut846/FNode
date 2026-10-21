package node

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/common/sysinfo"
	"github.com/tavut846/FNode/conf"
	vCore "github.com/tavut846/FNode/core"
)

// Machine runs every node the panel assigns to one Xboard machine (machine
// mode, per-server token). It keeps one Controller per node, one machine
// WebSocket shared by all of them, re-polls the node list, and reports the
// machine status.
type Machine struct {
	cfg    conf.NodeConfig
	core   vCore.Core
	client *panel.Client

	mu sync.Mutex
	// syncMu serialises Sync (poll loop, WS sync.nodes) and Close.
	syncMu      sync.Mutex
	closed      bool
	controllers map[int]*Controller
	warned      map[int]bool

	ws      *panel.WSClient
	shipper *logship.Shipper
	sampler *sysinfo.Sampler
	cancel  context.CancelFunc
	done    chan struct{}
	// shipCancel stops the log shipper, which runs from the first start
	// attempt so a machine that cannot start still reports why.
	shipCancel context.CancelFunc
	// skip reports node ids a standalone config entry already runs.
	skip     func(id int) bool
	pull     time.Duration
	push     time.Duration
	interval chan struct{}

	// newController is replaced in tests.
	newController func(n panel.MachineNode) (*Controller, error)
}

// NewMachine prepares a machine from a node entry with MachineID set.
func NewMachine(cfg conf.NodeConfig, core vCore.Core) (*Machine, error) {
	api := cfg.ApiConfig
	api.NodeID = 0
	client, err := panel.New(&api)
	if err != nil {
		return nil, err
	}
	m := &Machine{
		cfg:         cfg,
		core:        core,
		client:      client,
		controllers: make(map[int]*Controller),
		warned:      make(map[int]bool),
		sampler:     sysinfo.NewSampler(),
		shipper:     logship.NewShipper(client),
		pull:        60 * time.Second,
		push:        60 * time.Second,
		interval:    make(chan struct{}, 1),
	}
	m.newController = m.startController
	return m, nil
}

func (m *Machine) logger() *log.Entry {
	return log.WithFields(log.Fields{"machine_id": m.cfg.ApiConfig.MachineID, "panel": m.cfg.ApiConfig.APIHost})
}

// Start lists the machine's nodes, starts them, then connects the machine
// WebSocket and the background loop.
func (m *Machine) Start() error {
	if m.shipCancel == nil {
		logship.Default.Register(m.shipper)
		ctx, cancel := context.WithCancel(context.Background())
		m.shipCancel = cancel
		go m.shipper.Run(ctx)
	}
	resp, err := m.client.GetMachineNodes()
	if err != nil {
		return fmt.Errorf("get machine %d nodes error: %s", m.cfg.ApiConfig.MachineID, err)
	}
	m.applyIntervals(resp.BaseConfig)
	m.prepareWS()
	m.Sync(resp.Nodes)
	m.logger().Infof("Machine started with %d nodes", len(m.RunningIDs()))

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	if m.ws != nil {
		go m.ws.Run(ctx)
	}
	go m.loop(ctx)
	m.reportStatus()
	return nil
}

// Close stops the loop, the WebSocket and every node.
func (m *Machine) Close() {
	if m.cancel != nil {
		m.cancel()
		<-m.done
	}
	m.syncMu.Lock()
	m.closed = true
	m.syncMu.Unlock()
	m.mu.Lock()
	ctrls := m.controllers
	m.controllers = make(map[int]*Controller)
	m.mu.Unlock()
	for id, c := range ctrls {
		if err := c.Close(); err != nil {
			m.logger().WithFields(log.Fields{"node_id": id, "err": err}).Error("Close machine node failed")
		}
	}
	if m.shipCancel != nil {
		m.shipCancel()
	}
	logship.Default.Unregister(m.shipper)
}

// prepareWS creates (without connecting) the machine WebSocket when the
// panel enables it, so controllers started next can share it.
func (m *Machine) prepareWS() {
	hs, err := m.client.Handshake()
	if err != nil {
		m.logger().WithField("err", err).Info("Panel handshake unavailable, using HTTP polling")
		return
	}
	if !hs.WebSocket.Enabled {
		m.logger().Info("Panel WebSocket push is disabled, using HTTP polling")
		return
	}
	if !strings.HasPrefix(hs.WebSocket.WSURL, "ws://") && !strings.HasPrefix(hs.WebSocket.WSURL, "wss://") {
		m.logger().WithField("ws_url", hs.WebSocket.WSURL).
			Error("Panel ws_url is malformed (must start with ws:// or wss://), using HTTP polling. Fix server_ws_url in the panel settings")
		return
	}
	ws, err := panel.NewMachineWSClient(hs.WebSocket.WSURL, m.client.Token, m.cfg.ApiConfig.MachineID, m.handleWSEvent, m.handleWSStatus)
	if err != nil {
		m.logger().WithFields(log.Fields{"ws_url": hs.WebSocket.WSURL, "err": err}).Error("Panel ws_url is malformed, using HTTP polling")
		return
	}
	m.ws = ws
	m.logger().WithField("url", ws.URL()).Info("Connecting to panel machine WebSocket")
}

func (m *Machine) applyIntervals(bc panel.MachineBaseConfig) {
	pull := time.Duration(bc.PullInterval) * time.Second
	if pull < 30*time.Second {
		pull = 60 * time.Second
	}
	push := time.Duration(bc.PushInterval) * time.Second
	if push < 10*time.Second {
		push = 60 * time.Second
	}
	m.mu.Lock()
	changed := pull != m.pull || push != m.push
	m.pull, m.push = pull, push
	m.mu.Unlock()
	if changed {
		select {
		case m.interval <- struct{}{}:
		default:
		}
	}
}

func (m *Machine) intervals() (time.Duration, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pull, m.push
}

func (m *Machine) loop(ctx context.Context) {
	defer close(m.done)
	pull, push := m.intervals()
	discover := time.NewTicker(pull)
	status := time.NewTicker(push)
	defer discover.Stop()
	defer status.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.interval:
			pull, push = m.intervals()
			discover.Reset(pull)
			status.Reset(push)
		case <-discover.C:
			m.rediscover()
		case <-status.C:
			m.reportStatus()
		}
	}
}

// rediscover re-polls the machine's node list.
func (m *Machine) rediscover() {
	resp, err := m.client.GetMachineNodes()
	if err != nil {
		m.logger().WithField("err", err).Warn("Machine node discovery failed")
		return
	}
	m.applyIntervals(resp.BaseConfig)
	m.Sync(resp.Nodes)
}

// RunningIDs returns the ids of the running nodes.
func (m *Machine) RunningIDs() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]int, 0, len(m.controllers))
	for id := range m.controllers {
		ids = append(ids, id)
	}
	return ids
}

// Sync starts nodes added to the machine and stops nodes removed from it.
// A node that fails to start is retried on the next sync.
func (m *Machine) Sync(wanted []panel.MachineNode) {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	if m.closed {
		return
	}
	var supported []panel.MachineNode
	for _, n := range wanted {
		if panel.NormalizeNodeType(n.Type) == "" {
			m.mu.Lock()
			first := !m.warned[n.ID]
			m.warned[n.ID] = true
			m.mu.Unlock()
			if first {
				m.logger().WithFields(log.Fields{"node_id": n.ID, "type": n.Type}).Warn("FNode does not support this node type, skipping it")
			}
			continue
		}
		if m.skip != nil && m.skip(n.ID) {
			m.mu.Lock()
			first := !m.warned[n.ID]
			m.warned[n.ID] = true
			m.mu.Unlock()
			if first {
				m.logger().WithField("node_id", n.ID).Warn("Node is also a standalone entry in config.json; the standalone entry runs it and the machine skips it")
			}
			continue
		}
		supported = append(supported, n)
	}
	add, remove := panel.DiffMachineNodes(m.RunningIDs(), supported)
	for _, id := range remove {
		m.mu.Lock()
		c := m.controllers[id]
		delete(m.controllers, id)
		m.mu.Unlock()
		if c == nil {
			continue
		}
		m.logger().WithField("node_id", id).Info("Node removed from machine, stopping it")
		if err := c.Close(); err != nil {
			m.logger().WithFields(log.Fields{"node_id": id, "err": err}).Error("Stop machine node failed")
		}
	}
	for _, n := range add {
		c, err := m.newController(n)
		if err != nil {
			m.logger().WithFields(log.Fields{"node_id": n.ID, "type": n.Type, "err": err}).Error("Start machine node failed, retrying on the next sync")
			continue
		}
		m.mu.Lock()
		m.controllers[n.ID] = c
		m.mu.Unlock()
		m.logger().WithFields(log.Fields{"node_id": n.ID, "type": n.Type, "name": n.Name}).Info("Machine node started")
	}
}

// nodeOptions returns the per-node Options: a copy of the machine template.
func (m *Machine) nodeOptions(nodeID int) *conf.Options {
	opts := m.cfg.Options
	if opts.Name != "" {
		opts.Name = fmt.Sprintf("%s-%d", opts.Name, nodeID)
	}
	if opts.CertConfig != nil {
		cc := *opts.CertConfig
		opts.CertConfig = &cc
	}
	return &opts
}

func (m *Machine) startController(n panel.MachineNode) (*Controller, error) {
	api := m.cfg.ApiConfig
	api.NodeID = n.ID
	api.NodeType = ""
	client, err := panel.New(&api)
	if err != nil {
		return nil, err
	}
	c := NewController(m.core, client, m.nodeOptions(n.ID))
	c.sharedWS = m.ws
	c.shipper = m.shipper
	if err = c.Start(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (m *Machine) handleWSStatus(connected bool) {
	if !connected {
		m.logger().Warn("Panel machine WebSocket down, HTTP polling resumed")
	}
}

// handleWSEvent routes machine WebSocket events: sync.nodes updates the node
// set, everything else goes to the node named by node_id.
func (m *Machine) handleWSEvent(ev *panel.WSEvent) {
	if ev.Type == panel.WSEventLogMode {
		// node_id null: the whole machine; otherwise that node only
		if mode, ok := logship.ParseMode(ev.LogMode); ok {
			m.shipper.SetMode(ev.NodeID, mode)
		}
		return
	}
	if ev.Type == panel.WSEventSyncNodes {
		m.logger().Infof("Panel pushed the machine node list (%d nodes)", len(ev.Nodes))
		// Starting nodes can take a while (certificates); keep reading the socket
		go m.Sync(ev.Nodes)
		return
	}
	if ev.NodeID == 0 {
		return
	}
	m.mu.Lock()
	c := m.controllers[ev.NodeID]
	m.mu.Unlock()
	if c != nil {
		c.handleWSEvent(ev)
	}
}

// StatusSample converts a sysinfo sample to the panel payload type.
func StatusSample(s sysinfo.Status) panel.MachineStatus {
	return panel.MachineStatus{
		CPU:         s.CPU,
		MemTotal:    s.MemTotal,
		MemUsed:     s.MemUsed,
		SwapTotal:   s.SwapTotal,
		SwapUsed:    s.SwapUsed,
		DiskTotal:   s.DiskTotal,
		DiskUsed:    s.DiskUsed,
		NetInSpeed:  s.NetInSpeed,
		NetOutSpeed: s.NetOutSpeed,
	}
}

func (m *Machine) reportStatus() {
	if err := m.client.ReportMachineStatus(StatusSample(m.sampler.Collect()), panel.Version); err != nil {
		m.logger().WithField("err", err).Warn("Report machine status failed")
	}
}
