package panel

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Multiplex is the node's "multiplex" block from the panel (Xboard's
// protocol_settings.multiplex), sent for vless, vmess and trojan. The server
// side uses Enabled, Padding and Brutal; Protocol, MaxConnections, MinStreams
// and MaxStreams are client options and are parsed but not used.
type Multiplex struct {
	// Enabled is nil when the panel did not send a value (the local config
	// then decides); a value, true or false, wins over the local config.
	Enabled        *bool
	Padding        bool
	Brutal         MultiplexBrutal
	Protocol       string
	MaxConnections int
	MinStreams     int
	MaxStreams     int
}

type MultiplexBrutal struct {
	Enabled  bool
	UpMbps   int
	DownMbps int
}

// Specified reports whether the panel said whether multiplex is on or off.
func (m *Multiplex) Specified() bool { return m != nil && m.Enabled != nil }

// UnmarshalJSON is lenient: PHP sends [] for an empty array, null for unset
// fields, and numbers or booleans may arrive as strings. Anything that is not
// an object leaves the block unset instead of failing the whole node config.
func (m *Multiplex) UnmarshalJSON(data []byte) error {
	*m = Multiplex{}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	if b, ok := looseBool(raw["enabled"]); ok {
		m.Enabled = &b
	}
	m.Padding, _ = looseBool(raw["padding"])
	if s, ok := raw["protocol"].(string); ok {
		m.Protocol = strings.TrimSpace(s)
	}
	m.MaxConnections = looseInt(raw["max_connections"])
	m.MinStreams = looseInt(raw["min_streams"])
	m.MaxStreams = looseInt(raw["max_streams"])
	if br, ok := raw["brutal"].(map[string]any); ok {
		m.Brutal.Enabled, _ = looseBool(br["enabled"])
		m.Brutal.UpMbps = looseInt(br["up_mbps"])
		m.Brutal.DownMbps = looseInt(br["down_mbps"])
	}
	return nil
}

// looseBool reads a bool the way a PHP panel may send it: true/false, 0/1,
// "0"/"1", "true"/"false", "on"/"off", "yes"/"no". ok is false for null,
// missing and unrecognised values.
func looseBool(v any) (val bool, ok bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case float64:
		return x != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off", "":
			return false, x != ""
		}
	}
	return false, false
}

// looseInt reads a whole number sent as a number or a numeric string.
func looseInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		if n, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			return int(n)
		}
	}
	return 0
}
