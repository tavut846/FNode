package node

import (
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/common/task"
	vCore "github.com/tavut846/FNode/core"
	"github.com/tavut846/FNode/limiter"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) startTasks(node *panel.NodeInfo) {
	// fetch node info task
	c.nodeInfoMonitorPeriodic = &task.Task{
		Interval: node.PullInterval,
		Execute:  c.nodeInfoMonitor,
	}
	// fetch user list task
	c.userReportPeriodic = &task.Task{
		Interval: node.PushInterval,
		Execute:  c.reportUserTrafficTask,
	}
	log.WithField("tag", c.tag).Info("Start monitor node status")
	// delay to start nodeInfoMonitor
	_ = c.nodeInfoMonitorPeriodic.Start(false)
	log.WithField("tag", c.tag).Info("Start report node status")
	_ = c.userReportPeriodic.Start(false)
	if node.Security == panel.Tls {
		switch c.CertConfig.CertMode {
		case "none", "", "file", "self":
		default:
			c.renewCertPeriodic = &task.Task{
				Interval: time.Hour * 24,
				Execute:  c.renewCertTask,
			}
			log.WithField("tag", c.tag).Info("Start renew cert")
			// delay to start renewCert
			_ = c.renewCertPeriodic.Start(true)
		}
	}
	if c.LimitConfig.EnableDynamicSpeedLimit {
		c.traffic = make(map[string]int64)
		c.dynamicSpeedLimitPeriodic = &task.Task{
			Interval: time.Duration(c.LimitConfig.DynamicSpeedLimitConfig.Periodic) * time.Second,
			Execute:  c.SpeedChecker,
		}
		log.Printf("[%s: %d] Start dynamic speed limit", c.apiClient.NodeType, c.apiClient.NodeId)
	}
}

func (c *Controller) nodeInfoMonitor() (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	if c.rebuild {
		// The last reload failed half way: fetch the full config again
		c.apiClient.ResetConfigCache()
	}
	// While the panel WebSocket is connected it pushes config, users and
	// devices, but the HTTP checks below still run: they are ETag-cached
	// (304 when unchanged) and catch what the panel did not push.
	ws := c.wsConnected() && !c.rebuild
	// get node info: picks up config and route changes the panel did not
	// push (hidden nodes, missed events)
	newN, err := c.apiClient.GetNodeInfo()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get node info failed")
		return nil
	}
	// get user info. GET /user is also the panel's heartbeat: Xboard marks
	// the node as running only when it is called (a 304 counts), so it must
	// run every pull interval even with the WebSocket up.
	newU, err := c.apiClient.GetUserList()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get user list failed")
		if !ws {
			return nil
		}
		newU = nil
	}
	if ws {
		// The panel pushes devices over the WebSocket
		if newN != nil {
			log.WithField("tag", c.tag).Info("Node config changed on the panel (picked up by the HTTP check)")
		}
		return c.applyUpdate(newN, newU, nil)
	}
	// get user alive
	newA, err := c.apiClient.GetUserAlive()
	if err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Get alive list failed")
		return nil
	}
	return c.applyUpdate(newN, newU, newA)
}

// failReload marks the node for a full rebuild on the next poll after a
// reload failed half way (the old inbound may already be gone).
func (c *Controller) failReload(msg string, err error) {
	c.rebuild = true
	log.WithFields(log.Fields{
		"tag": c.tag,
		"err": err,
	}).Error(msg + ", retrying on the next config check")
}

// applyUpdate applies a changed node config (newN), user list (newU) and alive
// list (newA); any of them may be nil when unchanged. The caller holds c.mu.
func (c *Controller) applyUpdate(newN *panel.NodeInfo, newU []panel.UserInfo, newA map[int]int) (err error) {
	if newN != nil && c.info != nil {
		// a config pushed over the WebSocket has no intervals: keep the known ones
		if newN.PullInterval == 0 {
			newN.PullInterval = c.info.PullInterval
		}
		if newN.PushInterval == 0 {
			newN.PushInterval = c.info.PushInterval
		}
	}
	if newN != nil && !c.rebuild && c.applyRoutingOnly(newN) {
		newN = nil
	}
	// Check cert first before deleting the old node so a cert error doesn't
	// kill the running node. The config is fetched again on the next check
	// until the certificate is there.
	if newN != nil && newN.Security == panel.Tls {
		if err = c.requestCert(newN); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Request cert failed, keeping running node")
			c.apiClient.ResetConfigCache()
			newN = nil
		}
	}
	if newN != nil {
		reason, oldHash := "inbound config changed", ""
		if c.info != nil {
			oldHash = c.info.InboundHash
		}
		if c.rebuild {
			reason = "the previous reload failed"
		}
		c.info = newN
		// nodeInfo changed
		if newU != nil {
			c.userList = newU
		}
		c.traffic = make(map[string]int64)

		// Remove old node
		log.WithFields(log.Fields{
			"tag":    c.tag,
			"reason": reason,
			"hash":   shortHash(oldHash) + " -> " + shortHash(newN.InboundHash),
		}).Info("Node changed, recreating its inbound (live connections are dropped)")
		if err = c.server.DelNode(c.tag); err != nil && !c.rebuild {
			c.failReload("Delete node failed", err)
			return nil
		}

		oldTag := c.tag
		if len(c.Options.Name) == 0 {
			c.tag = c.buildNodeTag(newN)
		}
		if c.tag != oldTag && c.shipper != nil {
			logship.Default.UnmapNode(oldTag)
			logship.Default.MapNode(c.tag, c.apiClient.NodeId, c.shipper)
		}
		// Update limiter (keep the current alive list when none was fetched)
		if newA == nil && c.limiter != nil {
			newA = c.limiter.AliveList
		}
		limiter.DeleteLimiter(oldTag)
		l := limiter.AddLimiter(c.tag, &c.LimitConfig, c.userList, newA)
		c.limiter = l

		// update alive list
		if newA != nil {
			c.limiter.AliveList = newA
		}
		// Update rule
		err = c.limiter.UpdateRule(&newN.Rules)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Update Rule failed")
			return nil
		}

		// add new node
		err = c.server.AddNode(c.tag, newN, c.Options)
		if err != nil {
			c.failReload("Add node failed", err)
			return nil
		}
		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			Users:    c.userList,
			NodeInfo: newN,
		})
		if err != nil {
			c.failReload("Add users failed", err)
			return nil
		}
		c.rebuild = false
		c.applyIntervals(newN)
		log.WithField("tag", c.tag).Infof("Added %d new users", len(c.userList))
		// exit
		return nil
	}
	// update alive list
	if newA != nil {
		c.limiter.AliveList = newA
	}
	// node no changed, check users
	if len(newU) == 0 {
		return nil
	}
	return c.applyUserList(newU)
}

// applyUserList diffs newU against the running users and adds/removes the
// difference. The caller holds c.mu.
func (c *Controller) applyUserList(newU []panel.UserInfo) (err error) {
	deleted, added := compareUserList(c.userList, newU)
	if len(deleted) > 0 {
		// have deleted users
		err = c.server.DelUsers(deleted, c.tag, c.info)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Delete users failed")
			return nil
		}
	}
	if len(added) > 0 {
		// have added users
		_, err = c.server.AddUsers(&vCore.AddUsersParams{
			Tag:      c.tag,
			NodeInfo: c.info,
			Users:    added,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Add users failed")
			return nil
		}
	}
	if len(added) > 0 || len(deleted) > 0 {
		// update Limiter
		c.limiter.UpdateUser(c.tag, added, deleted)
		if err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("limiter users failed")
			return nil
		}
		// clear traffic record
		if c.LimitConfig.EnableDynamicSpeedLimit {
			for i := range deleted {
				delete(c.traffic, deleted[i].Uuid)
			}
		}
	}
	c.userList = newU
	if len(added)+len(deleted) != 0 {
		log.WithField("tag", c.tag).
			Infof("%d user deleted, %d user added", len(deleted), len(added))
	}
	return nil
}

// applyRoutingOnly handles a config change that only touches routing fields
// (routes, custom_outbounds, custom_routes, custom_route_rules): outbounds and
// rules are reconciled in place and the inbound keeps its connections.
// It returns false when the inbound must be rebuilt instead.
func (c *Controller) applyRoutingOnly(newN *panel.NodeInfo) bool {
	if c.info == nil || newN.InboundHash == "" || c.info.InboundHash != newN.InboundHash {
		return false
	}
	if c.info.RoutingHash == newN.RoutingHash {
		// same inbound, same routing: only the shape or the intervals differ
		c.info = newN
		c.applyIntervals(newN)
		log.WithField("tag", c.tag).Debug("Node config unchanged apart from intervals")
		return true
	}
	ru, ok := c.server.(vCore.RoutingUpdater)
	if !ok {
		return false
	}
	if err := ru.UpdateNodeRouting(c.tag, newN); err != nil {
		log.WithFields(log.Fields{
			"tag": c.tag,
			"err": err,
		}).Error("Apply routing change failed, rebuilding node")
		return false
	}
	if c.limiter != nil {
		if err := c.limiter.UpdateRule(&newN.Rules); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Error("Update Rule failed")
		}
	}
	c.info = newN
	log.WithField("tag", c.tag).Info("Node routing changed, applied without inbound reload")
	return true
}

// applyIntervals restarts the poll and report tasks when the panel changed
// their intervals (0 means the config did not carry one).
func (c *Controller) applyIntervals(n *panel.NodeInfo) {
	if c.nodeInfoMonitorPeriodic != nil && n.PullInterval != 0 && c.nodeInfoMonitorPeriodic.Interval != n.PullInterval {
		c.nodeInfoMonitorPeriodic.Interval = n.PullInterval
		c.nodeInfoMonitorPeriodic.Close()
		_ = c.nodeInfoMonitorPeriodic.Start(false)
	}
	if c.userReportPeriodic != nil && n.PushInterval != 0 && c.userReportPeriodic.Interval != n.PushInterval {
		c.userReportPeriodic.Interval = n.PushInterval
		c.userReportPeriodic.Close()
		_ = c.userReportPeriodic.Start(false)
	}
}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func (c *Controller) SpeedChecker() error {
	for u, t := range c.traffic {
		if t >= c.LimitConfig.DynamicSpeedLimitConfig.Traffic {
			err := c.limiter.UpdateDynamicSpeedLimit(c.tag, u,
				c.LimitConfig.DynamicSpeedLimitConfig.SpeedLimit,
				time.Now().Add(time.Duration(c.LimitConfig.DynamicSpeedLimitConfig.ExpireTime)*time.Minute))
			if err != nil {
				log.WithField("err", err).Error("Update dynamic speed limit failed")
			}
			delete(c.traffic, u)
		}
	}
	return nil
}
