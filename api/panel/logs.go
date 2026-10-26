package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/tavut846/FNode/common/logship"
)

// LogsPath is the CustomNode plugin's log endpoint.
const LogsPath = "/api/v2/custom-node/logs"

// LogsRequest is the body of POST /api/v2/custom-node/logs.
type LogsRequest struct {
	MachineID int             `json:"machine_id,omitempty"`
	Token     string          `json:"token"`
	NodeID    int             `json:"node_id,omitempty"`
	Entries   []logship.Entry `json:"entries"`
	// Client and Version identify FNode to the plugin (connection status).
	Client  string `json:"client,omitempty"`
	Version string `json:"version,omitempty"`
	// MissingOutbounds asks the plugin for outbound tags (node id -> tags)
	// that the node's routes use but the node does not have.
	MissingOutbounds map[string][]string `json:"missing_outbounds,omitempty"`
	// Dropped counts entries lost to FNode's full buffer since the last push.
	Dropped int `json:"dropped,omitempty"`
}

// BuildLogsRequest builds the body with the same credentials as the node API:
// machine_id + machine token in machine mode, server token + node_id in node
// mode (node_id at the top level is for node mode only).
func (c *Client) BuildLogsRequest(entries []logship.Entry) LogsRequest {
	if entries == nil {
		entries = []logship.Entry{}
	}
	req := LogsRequest{Token: c.Token, Entries: entries, Client: "fnode", Version: Version}
	if c.MachineID > 0 {
		req.MachineID = c.MachineID
	} else {
		req.NodeID = c.NodeId
	}
	return req
}

// ParseLogsResponse decodes {"data":{"log_mode","accepted","flush_interval","node_modes"}}.
func ParseLogsResponse(body []byte) (logship.Response, error) {
	var out struct {
		Data struct {
			LogMode       string          `json:"log_mode"`
			Accepted      int             `json:"accepted"`
			FlushInterval int             `json:"flush_interval"`
			NodeModes     json.RawMessage `json:"node_modes"`
			// standby contract (CustomNode 1.13): both optional
			Ship          *bool `json:"ship"`
			RetainSeconds int   `json:"retain_seconds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return logship.Response{}, fmt.Errorf("decode log response: %w", err)
	}
	resp := logship.Response{LogMode: out.Data.LogMode, Accepted: out.Data.Accepted, FlushInterval: out.Data.FlushInterval,
		Ship: out.Data.Ship, RetainSeconds: out.Data.RetainSeconds}
	if raw := out.Data.NodeModes; len(raw) > 0 && string(raw) != "null" {
		// {"12":"debug"} — PHP sends [] for an empty map
		modes := map[string]string{}
		if err := json.Unmarshal(raw, &modes); err == nil || string(raw) == "[]" {
			resp.HasNodeModes = true
			resp.NodeModes = make(map[int]string, len(modes))
			for k, v := range modes {
				if id, err := strconv.Atoi(k); err == nil {
					resp.NodeModes[id] = v
				}
			}
		}
	}
	return resp, nil
}

// SendLogs implements logship.Sender. A 404 (plugin missing) is reported as
// logship.ErrNotFound.
func (c *Client) SendLogs(ctx context.Context, entries []logship.Entry) (logship.Response, error) {
	return c.SendLogsMeta(ctx, entries, logship.Meta{})
}

// SendLogsMeta implements logship.MetaSender.
func (c *Client) SendLogsMeta(ctx context.Context, entries []logship.Entry, meta logship.Meta) (logship.Response, error) {
	req := c.BuildLogsRequest(entries)
	req.Dropped = meta.Dropped
	if len(meta.Missing) > 0 {
		req.MissingOutbounds = make(map[string][]string, len(meta.Missing))
		for id, tags := range meta.Missing {
			req.MissingOutbounds[strconv.Itoa(id)] = tags
		}
	}
	r, err := c.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		ForceContentType("application/json").
		SetBody(req).
		Post(LogsPath)
	if err != nil {
		return logship.Response{}, err
	}
	if r.StatusCode() == http.StatusNotFound {
		return logship.Response{}, logship.ErrNotFound
	}
	if r.StatusCode() >= 400 {
		return logship.Response{}, fmt.Errorf("log push failed: HTTP %d", r.StatusCode())
	}
	return ParseLogsResponse(r.Body())
}
