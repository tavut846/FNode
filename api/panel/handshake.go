package panel

import (
	"encoding/json"
	"fmt"
)

// HandshakeResponse is the response of POST /api/v2/server/handshake.
type HandshakeResponse struct {
	WebSocket WSConfig `json:"websocket"`
}

// WSConfig tells the node whether the panel's WebSocket push is available.
type WSConfig struct {
	Enabled bool   `json:"enabled"`
	WSURL   string `json:"ws_url,omitempty"`
}

// Handshake asks the panel whether WebSocket push is enabled. The request
// carries the same node_type, node_id and token query parameters as UniProxy.
func (c *Client) Handshake() (*HandshakeResponse, error) {
	const path = "/api/v2/server/handshake"
	r, err := c.client.R().
		ForceContentType("application/json").
		Post(path)
	if err != nil {
		return nil, c.checkResponse(r, path, err)
	}
	if r == nil {
		return nil, fmt.Errorf("received nil response")
	}
	if err = c.checkResponse(r, path, err); err != nil {
		return nil, err
	}
	return ParseHandshake(r.Body())
}

// ParseHandshake decodes a handshake response body.
func ParseHandshake(body []byte) (*HandshakeResponse, error) {
	hs := &HandshakeResponse{}
	if err := json.Unmarshal(body, hs); err != nil {
		return nil, fmt.Errorf("decode handshake error: %s", err)
	}
	return hs, nil
}
