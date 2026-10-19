package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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
}

// BuildLogsRequest builds the body with the same credentials as the node API:
// machine_id + machine token in machine mode, server token + node_id in node
// mode (node_id at the top level is for node mode only).
func (c *Client) BuildLogsRequest(entries []logship.Entry) LogsRequest {
	if entries == nil {
		entries = []logship.Entry{}
	}
	req := LogsRequest{Token: c.Token, Entries: entries}
	if c.MachineID > 0 {
		req.MachineID = c.MachineID
	} else {
		req.NodeID = c.NodeId
	}
	return req
}

// ParseLogsResponse decodes {"data":{"log_mode","accepted","flush_interval"}}.
func ParseLogsResponse(body []byte) (logship.Response, error) {
	var out struct {
		Data struct {
			LogMode       string `json:"log_mode"`
			Accepted      int    `json:"accepted"`
			FlushInterval int    `json:"flush_interval"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return logship.Response{}, fmt.Errorf("decode log response: %w", err)
	}
	return logship.Response{LogMode: out.Data.LogMode, Accepted: out.Data.Accepted, FlushInterval: out.Data.FlushInterval}, nil
}

// SendLogs implements logship.Sender. A 404 (plugin missing) is reported as
// logship.ErrNotFound.
func (c *Client) SendLogs(ctx context.Context, entries []logship.Entry) (logship.Response, error) {
	r, err := c.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		ForceContentType("application/json").
		SetBody(c.BuildLogsRequest(entries)).
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
