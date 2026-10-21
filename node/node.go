package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	vCore "github.com/tavut846/FNode/core"
)

// Retry delays for a node or machine that failed to start (panel unreachable
// at boot, no users yet, a bad certificate...). FNode keeps trying instead of
// staying offline until the next restart.
var (
	retryMin = 10 * time.Second
	retryMax = 2 * time.Minute
)

type Node struct {
	mu          sync.Mutex
	controllers []*Controller
	machines    []*Machine
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func New() *Node {
	return &Node{}
}

// panelKey identifies a panel node across config entries: the same panel host
// and node id is the same node, whatever the token or mode.
func panelKey(host string, nodeID int) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(host)), "/") + "#" + fmt.Sprint(nodeID)
}

// StandaloneKeys returns the panel keys of the standalone (non-machine) entries.
func StandaloneKeys(nodes []conf.NodeConfig) map[string]bool {
	keys := make(map[string]bool)
	for _, n := range nodes {
		if !n.ApiConfig.IsMachine() && n.ApiConfig.NodeID > 0 {
			keys[panelKey(n.ApiConfig.APIHost, n.ApiConfig.NodeID)] = true
		}
	}
	return keys
}

// Start starts every entry. An entry that fails is logged and retried in the
// background with backoff, so one unreachable panel or one broken node never
// keeps the others (or itself, later) offline. It only returns an error for
// entries that can never work (invalid config).
func (n *Node) Start(nodes []conf.NodeConfig, core vCore.Core) error {
	ctx, cancel := context.WithCancel(context.Background())
	n.mu.Lock()
	n.controllers = nil
	n.machines = nil
	n.cancel = cancel
	n.mu.Unlock()

	standalone := StandaloneKeys(nodes)
	seen := make(map[string]bool)
	var errs []error
	for i := range nodes {
		cfg := nodes[i]
		if cfg.ApiConfig.IsMachine() {
			// Machine mode: one entry runs every node the panel assigns to the machine.
			// Nodes also listed as standalone entries are left to those entries.
			m, err := NewMachine(cfg, core)
			if err != nil {
				errs = append(errs, fmt.Errorf("start machine [%s-%d] error: %s", cfg.ApiConfig.APIHost, cfg.ApiConfig.MachineID, err))
				continue
			}
			m.skip = func(id int) bool { return standalone[panelKey(cfg.ApiConfig.APIHost, id)] }
			n.mu.Lock()
			n.machines = append(n.machines, m)
			n.mu.Unlock()
			name := fmt.Sprintf("machine [%s-%d]", cfg.ApiConfig.APIHost, cfg.ApiConfig.MachineID)
			n.startWithRetry(ctx, name, m.Start)
			continue
		}
		key := panelKey(cfg.ApiConfig.APIHost, cfg.ApiConfig.NodeID)
		if seen[key] {
			log.WithFields(log.Fields{"panel": cfg.ApiConfig.APIHost, "node_id": cfg.ApiConfig.NodeID}).
				Warn("The same panel node is listed twice in config.json, skipping the duplicate")
			continue
		}
		seen[key] = true
		if _, err := panel.New(&cfg.ApiConfig); err != nil {
			errs = append(errs, fmt.Errorf("node [%s-%d] error: %s", cfg.ApiConfig.APIHost, cfg.ApiConfig.NodeID, err))
			continue
		}
		name := fmt.Sprintf("node [%s-%s-%d]", cfg.ApiConfig.APIHost, cfg.ApiConfig.NodeType, cfg.ApiConfig.NodeID)
		n.startWithRetry(ctx, name, func() error { return n.startController(cfg, core) })
	}
	return errors.Join(errs...)
}

// startController creates and starts one standalone node controller.
func (n *Node) startController(cfg conf.NodeConfig, core vCore.Core) error {
	p, err := panel.New(&cfg.ApiConfig)
	if err != nil {
		return err
	}
	opts := cfg.Options
	c := NewController(core, p, &opts)
	if err = c.Start(); err != nil {
		_ = c.Close()
		return err
	}
	n.mu.Lock()
	n.controllers = append(n.controllers, c)
	n.mu.Unlock()
	return nil
}

// startWithRetry runs start now and, when it fails, again in the background
// with backoff until it succeeds or the node set is closed.
func (n *Node) startWithRetry(ctx context.Context, name string, start func() error) {
	err := start()
	if err == nil {
		return
	}
	logStartFailure(name, err, retryMin)
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		delay := retryMin
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if ctx.Err() != nil {
				return
			}
			if err := start(); err != nil {
				delay *= 2
				if delay > retryMax {
					delay = retryMax
				}
				logStartFailure(name, err, delay)
				continue
			}
			log.Infof("%s started after retry", name)
			return
		}
	}()
}

func logStartFailure(name string, err error, next time.Duration) {
	entry := log.WithFields(log.Fields{"err": err, "retry_in": next.String()})
	if errors.Is(err, ErrNoUsers) {
		entry.Warnf("%s is waiting for users", name)
		return
	}
	entry.Errorf("Start %s failed, retrying", name)
}

func (n *Node) Close() {
	n.mu.Lock()
	cancel := n.cancel
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// A retry in flight finishes its start first, so it is closed below
	n.wg.Wait()
	n.mu.Lock()
	machines, controllers := n.machines, n.controllers
	n.machines, n.controllers = nil, nil
	n.mu.Unlock()
	for _, m := range machines {
		m.Close()
	}
	for _, c := range controllers {
		if err := c.Close(); err != nil {
			log.WithFields(log.Fields{"tag": c.tag, "err": err}).Error("Close node failed")
		}
	}
}
