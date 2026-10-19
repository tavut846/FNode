package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/common/logship"
)

// WebSocket events pushed by the Xboard panel (app/WebSocket, NodeSyncService).
const (
	WSEventSyncConfig    = "sync.config"
	WSEventSyncUsers     = "sync.users"
	WSEventSyncUserDelta = "sync.user.delta"
	WSEventSyncDevices   = "sync.devices"
	// WSEventSyncNodes is a machine-level event: the machine's node set changed.
	WSEventSyncNodes = "sync.nodes"
	// WSEventLogMode is pushed by the CustomNode plugin: {"mode","node_id"|null}.
	WSEventLogMode = "custom_node.log_mode"
)

// WSEvent is a parsed data event.
type WSEvent struct {
	Type   string
	NodeID int
	// Config is the raw "config" object of sync.config; it has the same shape
	// as the HTTP /api/v1/server/UniProxy/config body.
	Config json.RawMessage
	// Users is the full user list of sync.users.
	Users []UserInfo
	// DeltaAction ("add" or "remove") and DeltaUsers come from sync.user.delta.
	// Removed users carry only their id.
	DeltaAction string
	DeltaUsers  []UserInfo
	// Devices maps a user id to its online IPs (sync.devices).
	Devices map[int][]string
	// Nodes is the new node list of sync.nodes (machine connections only).
	Nodes []MachineNode
	// LogMode is the mode of custom_node.log_mode; NodeID 0 means the whole
	// connection (the payload's node_id is null).
	LogMode string
}

type wsMessage struct {
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp int64           `json:"timestamp,omitempty"`
}

// isEmptyJSON reports a missing value, null, or a PHP empty array ([]),
// which the panel emits for an empty object.
func isEmptyJSON(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null")) || bytes.Equal(t, []byte("[]"))
}

// ParseWSMessage parses one WebSocket text message. It returns the event name
// and, for data events, the parsed event (nil for ping, auth.success, ...).
func ParseWSMessage(data []byte) (string, *WSEvent, error) {
	var msg wsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return "", nil, fmt.Errorf("decode ws message: %w", err)
	}
	ev := &WSEvent{Type: msg.Event}
	var payload struct {
		NodeID int             `json:"node_id"`
		Config json.RawMessage `json:"config"`
		Users  json.RawMessage `json:"users"`
		Action string          `json:"action"`
	}
	switch msg.Event {
	case WSEventLogMode:
		var p struct {
			Mode   string `json:"mode"`
			NodeID *int   `json:"node_id"`
		}
		if isEmptyJSON(msg.Data) {
			return msg.Event, nil, fmt.Errorf("custom_node.log_mode: empty payload")
		}
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			return msg.Event, nil, fmt.Errorf("custom_node.log_mode: decode payload: %w", err)
		}
		if _, ok := logship.ParseMode(p.Mode); !ok {
			return msg.Event, nil, fmt.Errorf("custom_node.log_mode: unknown mode %q", p.Mode)
		}
		ev.LogMode = p.Mode
		if p.NodeID != nil {
			ev.NodeID = *p.NodeID
		}
		return msg.Event, ev, nil
	case WSEventSyncNodes:
		var p struct {
			Nodes []MachineNode `json:"nodes"`
		}
		if !isEmptyJSON(msg.Data) {
			if err := json.Unmarshal(msg.Data, &p); err != nil {
				return msg.Event, nil, fmt.Errorf("sync.nodes: decode payload: %w", err)
			}
		}
		ev.Nodes = p.Nodes
		if ev.Nodes == nil {
			ev.Nodes = []MachineNode{}
		}
		return msg.Event, ev, nil
	case WSEventSyncConfig, WSEventSyncUsers, WSEventSyncUserDelta, WSEventSyncDevices:
		if isEmptyJSON(msg.Data) {
			return msg.Event, nil, fmt.Errorf("%s: empty payload", msg.Event)
		}
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			return msg.Event, nil, fmt.Errorf("%s: decode payload: %w", msg.Event, err)
		}
		ev.NodeID = payload.NodeID
	default:
		return msg.Event, nil, nil
	}

	switch msg.Event {
	case WSEventSyncConfig:
		if isEmptyJSON(payload.Config) {
			return msg.Event, nil, fmt.Errorf("sync.config: missing config")
		}
		ev.Config = payload.Config
		var id struct {
			NodeID int `json:"node_id"`
		}
		if ev.NodeID == 0 && json.Unmarshal(payload.Config, &id) == nil {
			ev.NodeID = id.NodeID
		}
	case WSEventSyncUsers:
		if !isEmptyJSON(payload.Users) {
			if err := json.Unmarshal(payload.Users, &ev.Users); err != nil {
				return msg.Event, nil, fmt.Errorf("sync.users: decode users: %w", err)
			}
		}
		if ev.Users == nil {
			ev.Users = []UserInfo{}
		}
	case WSEventSyncUserDelta:
		ev.DeltaAction = payload.Action
		if ev.DeltaAction != "add" && ev.DeltaAction != "remove" {
			return msg.Event, nil, fmt.Errorf("sync.user.delta: unknown action %q", payload.Action)
		}
		if !isEmptyJSON(payload.Users) {
			if err := json.Unmarshal(payload.Users, &ev.DeltaUsers); err != nil {
				return msg.Event, nil, fmt.Errorf("sync.user.delta: decode users: %w", err)
			}
		}
		if len(ev.DeltaUsers) == 0 {
			return msg.Event, nil, fmt.Errorf("sync.user.delta: no users")
		}
	case WSEventSyncDevices:
		ev.Devices = make(map[int][]string)
		if !isEmptyJSON(payload.Users) {
			var raw map[string][]string
			if err := json.Unmarshal(payload.Users, &raw); err != nil {
				return msg.Event, nil, fmt.Errorf("sync.devices: decode users: %w", err)
			}
			for k, ips := range raw {
				uid, err := strconv.Atoi(k)
				if err != nil {
					continue
				}
				ev.Devices[uid] = ips
			}
		}
	}
	return msg.Event, ev, nil
}

// ValidateWSURL checks that the panel's ws_url is a usable ws:// or wss:// URL.
func ValidateWSURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("ws_url is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("ws_url %q is malformed: %w", raw, err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("ws_url %q must start with ws:// or wss://", raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("ws_url %q has no host", raw)
	}
	return u, nil
}

// WSClient keeps one WebSocket connection to the panel for one node. It
// reconnects with exponential backoff until its context is cancelled.
type WSClient struct {
	url      string
	logURL   string
	onEvent  func(*WSEvent)
	onStatus func(connected bool)

	connected atomic.Bool

	HandshakeTimeout time.Duration
	// IdleTimeout drops a connection that received nothing (the panel pings
	// every 55s) for this long.
	IdleTimeout    time.Duration
	BackoffInitial time.Duration
	BackoffMax     time.Duration
}

// NewWSClient builds a client for wsURL?token=..&node_id=.. . onEvent is called
// from the client's goroutine for every data event, in order.
func NewWSClient(wsURL, token string, nodeID int, onEvent func(*WSEvent), onStatus func(bool)) (*WSClient, error) {
	return newWSClient(wsURL, token, "node_id", nodeID, onEvent, onStatus)
}

// NewMachineWSClient builds a machine connection (wsURL?token=..&machine_id=..).
// The panel multiplexes every node of the machine over it; node events carry
// node_id, and sync.nodes announces node set changes.
func NewMachineWSClient(wsURL, token string, machineID int, onEvent func(*WSEvent), onStatus func(bool)) (*WSClient, error) {
	return newWSClient(wsURL, token, "machine_id", machineID, onEvent, onStatus)
}

func newWSClient(wsURL, token, idKey string, id int, onEvent func(*WSEvent), onStatus func(bool)) (*WSClient, error) {
	u, err := ValidateWSURL(wsURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("token", token)
	q.Set(idKey, strconv.Itoa(id))
	u.RawQuery = q.Encode()
	logU := *u
	lq := logU.Query()
	lq.Set("token", "***")
	logU.RawQuery = lq.Encode()
	return &WSClient{
		url:              u.String(),
		logURL:           logU.String(),
		onEvent:          onEvent,
		onStatus:         onStatus,
		HandshakeTimeout: 15 * time.Second,
		IdleTimeout:      150 * time.Second,
		BackoffInitial:   time.Second,
		BackoffMax:       60 * time.Second,
	}, nil
}

// URL returns the connection URL with the token masked, for logging.
func (w *WSClient) URL() string { return w.logURL }

func (w *WSClient) IsConnected() bool { return w.connected.Load() }

func (w *WSClient) setConnected(v bool) {
	if w.connected.Swap(v) != v && w.onStatus != nil {
		w.onStatus(v)
	}
}

// Run connects and reconnects until ctx is cancelled.
func (w *WSClient) Run(ctx context.Context) {
	backoff := w.BackoffInitial
	for {
		start := time.Now()
		err := w.connect(ctx)
		w.setConnected(false)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.WithFields(log.Fields{"url": w.logURL, "err": err}).Warn("Panel WebSocket disconnected, using HTTP polling until it reconnects")
		}
		if time.Since(start) > 2*time.Minute {
			backoff = w.BackoffInitial
		}
		wait := backoff
		if j := int64(backoff / 5); j > 0 {
			wait += time.Duration(rand.Int63n(j))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < w.BackoffMax {
			backoff = min(backoff*2, w.BackoffMax)
		}
	}
}

func (w *WSClient) connect(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, w.HandshakeTimeout)
	conn, _, err := websocket.Dial(dialCtx, w.url, &websocket.DialOptions{HTTPHeader: userAgentHeader()})
	cancel()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(10 << 20)

	first := true
	for {
		readCtx, cancelRead := context.WithTimeout(ctx, w.IdleTimeout)
		typ, data, err := conn.Read(readCtx)
		cancelRead()
		if err != nil {
			if ctx.Err() != nil {
				conn.Close(websocket.StatusNormalClosure, "")
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}
		if typ != websocket.MessageText {
			continue
		}
		event, ev, perr := ParseWSMessage(data)
		if first {
			first = false
			if event == "error" {
				var m wsMessage
				_ = json.Unmarshal(data, &m)
				return fmt.Errorf("auth failed: %s", string(m.Data))
			}
			w.setConnected(true)
			log.WithField("url", w.logURL).Info("Panel WebSocket connected; HTTP config/user checks continue as the panel heartbeat")
		}
		switch event {
		case "ping":
			writeCtx, cancelWrite := context.WithTimeout(ctx, 10*time.Second)
			err = conn.Write(writeCtx, websocket.MessageText, []byte(`{"event":"pong"}`))
			cancelWrite()
			if err != nil {
				return fmt.Errorf("write pong: %w", err)
			}
			continue
		case "auth.success":
			continue
		case "error":
			return fmt.Errorf("panel error: %s", string(data))
		}
		if perr != nil {
			log.WithFields(log.Fields{"event": event, "err": perr}).Warn("Ignore invalid panel WebSocket event")
			continue
		}
		if ev != nil && w.onEvent != nil {
			w.onEvent(ev)
		}
	}
}
