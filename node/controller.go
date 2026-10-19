package node

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/common/task"
	"github.com/tavut846/FNode/conf"
	vCore "github.com/tavut846/FNode/core"
	"github.com/tavut846/FNode/limiter"
	log "github.com/sirupsen/logrus"
)

type Controller struct {
	server                    vCore.Core
	apiClient                 *panel.Client
	tag                       string
	limiter                   *limiter.Limiter
	traffic                   map[string]int64
	userList                  []panel.UserInfo
	aliveMap                  map[int]int
	info                      *panel.NodeInfo
	nodeInfoMonitorPeriodic   *task.Task
	userReportPeriodic        *task.Task
	renewCertPeriodic         *task.Task
	dynamicSpeedLimitPeriodic *task.Task
	onlineIpReportPeriodic    *task.Task
	lastReportedUIDs          map[int]struct{}
	// mu serialises the HTTP poll and panel WebSocket events.
	mu       sync.Mutex
	closed   bool
	// rebuild is set when a reload failed after the old inbound was removed;
	// the next poll fetches the full config and recreates the node.
	rebuild  bool
	ws       *panel.WSClient
	wsCancel context.CancelFunc
	// sharedWS is the machine connection in machine mode (events are routed
	// to this controller by the Machine); nil for a standalone node.
	sharedWS *panel.WSClient
	// shipper sends this node's logs to the panel; the machine's shipper in
	// machine mode, otherwise created in startLogs.
	shipper    *logship.Shipper
	ownShipper bool
	logCancel  context.CancelFunc
	*conf.Options
}

// ErrNoUsers means the panel has no user for the node yet (no plan in the
// node's groups). The node is retried until users appear.
var ErrNoUsers = errors.New("the panel has no users for this node yet (assign the node to a group with an active plan)")

// NewController return a Node controller with default parameters.
func NewController(server vCore.Core, api *panel.Client, config *conf.Options) *Controller {
	controller := &Controller{
		server:           server,
		Options:          config,
		apiClient:        api,
		lastReportedUIDs: make(map[int]struct{}),
	}
	return controller
}

// Start implement the Start() function of the service interface
func (c *Controller) Start() error {
	// First fetch Node Info
	var err error
	node, err := c.apiClient.GetNodeInfo()
	if err != nil && c.apiClient.NodeType != "" {
		// A NodeType in config.json that differs from the panel node makes the
		// v1 API answer "server does not exist". Retry with the panel's type.
		configured := c.apiClient.NodeType
		if c.apiClient.UseAutoType() {
			if n, err2 := c.apiClient.GetNodeInfo(); err2 == nil && n != nil {
				log.WithFields(log.Fields{"node_id": c.apiClient.NodeId, "config_type": configured, "panel_type": n.Type}).
					Warn("NodeType in config.json does not match the panel node, using the panel's type. Remove NodeType or fix it in config.json")
				node, err = n, nil
			}
		}
	}
	if err != nil {
		return fmt.Errorf("get node info error: %s", err)
	}
	if node == nil {
		return errors.New("get node info error: empty config")
	}
	// Update user
	c.userList, err = c.apiClient.GetUserList()
	if err != nil {
		return fmt.Errorf("get user list error: %s", err)
	}
	if len(c.userList) == 0 {
		return ErrNoUsers
	}
	c.aliveMap, err = c.apiClient.GetUserAlive()
	if err != nil {
		return fmt.Errorf("failed to get user alive list: %s", err)
	}
	if len(c.Options.Name) == 0 {
		c.tag = c.buildNodeTag(node)
	} else {
		c.tag = c.Options.Name
	}

	// add limiter
	l := limiter.AddLimiter(c.tag, &c.LimitConfig, c.userList, c.aliveMap)
	// add rule limiter
	if err = l.UpdateRule(&node.Rules); err != nil {
		return fmt.Errorf("update rule error: %s", err)
	}
	c.limiter = l
	if node.Security == panel.Tls {
		err = c.requestCert()
		if err != nil {
			return fmt.Errorf("request cert error: %s", err)
		}
	}
	// Add new tag
	err = c.server.AddNode(c.tag, node, c.Options)
	if err != nil {
		return fmt.Errorf("add new node error: %s", err)
	}
	added, err := c.server.AddUsers(&vCore.AddUsersParams{
		Tag:      c.tag,
		Users:    c.userList,
		NodeInfo: node,
	})
	if err != nil {
		return fmt.Errorf("add users error: %s", err)
	}
	log.WithField("tag", c.tag).Infof("Added %d new users", added)
	c.info = node
	// Hold mu so a WebSocket event cannot run before the tasks exist
	c.mu.Lock()
	c.startLogs()
	c.startWS()
	c.startTasks(node)
	c.mu.Unlock()
	return nil
}

// Close implement the Close() function of the service interface
func (c *Controller) Close() error {
	c.stopWS()
	c.stopLogs()
	// Wait for an in-flight poll or WS event, then refuse new ones
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	if c.tag == "" {
		// Start failed before the node existed in the core
		return nil
	}
	limiter.DeleteLimiter(c.tag)
	if c.nodeInfoMonitorPeriodic != nil {
		c.nodeInfoMonitorPeriodic.Close()
	}
	if c.userReportPeriodic != nil {
		c.userReportPeriodic.Close()
	}
	if c.renewCertPeriodic != nil {
		c.renewCertPeriodic.Close()
	}
	if c.dynamicSpeedLimitPeriodic != nil {
		c.dynamicSpeedLimitPeriodic.Close()
	}
	if c.onlineIpReportPeriodic != nil {
		c.onlineIpReportPeriodic.Close()
	}
	err := c.server.DelNode(c.tag)
	if err != nil {
		return fmt.Errorf("del node error: %s", err)
	}
	return nil
}

func (c *Controller) buildNodeTag(node *panel.NodeInfo) string {
	return fmt.Sprintf("[%s]-%s:%d", c.apiClient.APIHost, node.Type, node.Id)
}

// startLogs maps this node's tag to its panel log shipper, creating (and
// running) a shipper of its own unless the machine provided one.
func (c *Controller) startLogs() {
	if c.shipper == nil {
		c.shipper = logship.NewShipper(c.apiClient)
		c.ownShipper = true
		logship.Default.Register(c.shipper)
		ctx, cancel := context.WithCancel(context.Background())
		c.logCancel = cancel
		go c.shipper.Run(ctx)
	}
	logship.Default.MapNode(c.tag, c.apiClient.NodeId, c.shipper)
}

func (c *Controller) stopLogs() {
	logship.Default.UnmapNode(c.tag)
	if c.ownShipper {
		if c.logCancel != nil {
			c.logCancel()
		}
		logship.Default.Unregister(c.shipper)
	}
}
