package node

import (
	"context"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/api/panel"
)

// startWS asks the panel for its WebSocket endpoint and, when push is
// enabled, keeps a connection for this node. Every failure falls back to the
// HTTP polling that is already running.
func (c *Controller) startWS() {
	if c.sharedWS != nil {
		// Machine mode: the machine's connection carries this node's events
		c.ws = c.sharedWS
		return
	}
	hs, err := c.apiClient.Handshake()
	if err != nil {
		log.WithFields(log.Fields{"tag": c.tag, "err": err}).Info("Panel handshake unavailable, using HTTP polling")
		return
	}
	if !hs.WebSocket.Enabled {
		log.WithField("tag", c.tag).Info("Panel WebSocket push is disabled, using HTTP polling")
		return
	}
	if !strings.HasPrefix(hs.WebSocket.WSURL, "ws://") && !strings.HasPrefix(hs.WebSocket.WSURL, "wss://") {
		log.WithFields(log.Fields{"tag": c.tag, "ws_url": hs.WebSocket.WSURL}).
			Error("Panel ws_url is malformed (must start with ws:// or wss://), using HTTP polling. Fix server_ws_url in the panel settings")
		return
	}
	ws, err := panel.NewWSClient(hs.WebSocket.WSURL, c.apiClient.Token, c.apiClient.NodeId, c.handleWSEvent, c.handleWSStatus)
	if err != nil {
		log.WithFields(log.Fields{"tag": c.tag, "ws_url": hs.WebSocket.WSURL, "err": err}).
			Error("Panel ws_url is malformed, using HTTP polling")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.ws = ws
	c.wsCancel = cancel
	log.WithFields(log.Fields{"tag": c.tag, "url": ws.URL()}).Info("Connecting to panel WebSocket")
	go ws.Run(ctx)
}

func (c *Controller) stopWS() {
	if c.wsCancel != nil {
		c.wsCancel()
	}
}

func (c *Controller) wsConnected() bool {
	return c.ws != nil && c.ws.IsConnected()
}

func (c *Controller) handleWSStatus(connected bool) {
	if !connected {
		log.WithField("tag", c.tag).Warn("Panel WebSocket down, HTTP polling resumed")
	}
}

// handleWSEvent applies one panel push event. Events are delivered in order
// from the node's WS goroutine and serialised with the HTTP poll by c.mu.
func (c *Controller) handleWSEvent(ev *panel.WSEvent) {
	if ev.NodeID != 0 && ev.NodeID != c.apiClient.NodeId {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	switch ev.Type {
	case panel.WSEventSyncConfig:
		newN, err := c.apiClient.ParseNodeConfig(ev.Config)
		if err != nil {
			log.WithFields(log.Fields{"tag": c.tag, "err": err}).Error("Parse pushed node config failed")
			return
		}
		if newN == nil {
			log.WithField("tag", c.tag).Debug("Pushed node config unchanged")
			return
		}
		log.WithField("tag", c.tag).Info("Panel pushed a node config change")
		_ = c.applyUpdate(newN, nil, nil)
	case panel.WSEventSyncUsers:
		if len(ev.Users) == 0 {
			return
		}
		_ = c.applyUserList(ev.Users)
	case panel.WSEventSyncUserDelta:
		_ = c.applyUserList(applyUserDelta(c.userList, ev.DeltaAction, ev.DeltaUsers))
	case panel.WSEventSyncDevices:
		if c.limiter != nil {
			c.limiter.AliveList = devicesToAlive(ev.Devices)
		}
	}
}

// applyUserDelta returns the user list after a sync.user.delta event.
// "add" inserts or updates users by id; "remove" drops them by id (the panel
// sends only the id for removals).
func applyUserDelta(current []panel.UserInfo, action string, users []panel.UserInfo) []panel.UserInfo {
	byID := make(map[int]panel.UserInfo, len(users))
	for _, u := range users {
		byID[u.Id] = u
	}
	out := make([]panel.UserInfo, 0, len(current)+len(users))
	for _, u := range current {
		if d, ok := byID[u.Id]; ok {
			if action == "add" && d.Uuid != "" {
				out = append(out, d)
				delete(byID, u.Id)
			}
			continue
		}
		out = append(out, u)
	}
	if action == "add" {
		for _, u := range users {
			if _, pending := byID[u.Id]; pending && u.Uuid != "" {
				out = append(out, u)
			}
		}
	}
	return out
}

// devicesToAlive converts the panel's device map (uid -> IPs) to the alive
// count map the limiter uses.
func devicesToAlive(devices map[int][]string) map[int]int {
	alive := make(map[int]int, len(devices))
	for uid, ips := range devices {
		alive[uid] = len(ips)
	}
	return alive
}
