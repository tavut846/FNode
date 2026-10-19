package panel

import (
	"encoding/json"
	"fmt"
	"sort"
)

// MachineNode is one node assigned to an Xboard machine.
type MachineNode struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// MachineBaseConfig carries the panel's intervals in seconds.
type MachineBaseConfig struct {
	PushInterval int `json:"push_interval"`
	PullInterval int `json:"pull_interval"`
}

// MachineNodesResponse is the response of POST /api/v2/server/machine/nodes.
type MachineNodesResponse struct {
	Nodes      []MachineNode     `json:"nodes"`
	BaseConfig MachineBaseConfig `json:"base_config"`
}

// ParseMachineNodes decodes a machine/nodes response body.
func ParseMachineNodes(body []byte) (*MachineNodesResponse, error) {
	out := &MachineNodesResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("decode machine nodes error: %s", err)
	}
	return out, nil
}

// GetMachineNodes lists the nodes assigned to this machine.
func (c *Client) GetMachineNodes() (*MachineNodesResponse, error) {
	const path = "/api/v2/server/machine/nodes"
	r, err := c.client.R().ForceContentType("application/json").Post(path)
	if err != nil {
		return nil, c.checkResponse(r, path, err)
	}
	if err = c.checkResponse(r, path, err); err != nil {
		return nil, err
	}
	return ParseMachineNodes(r.Body())
}

// MachineStatus is one sample of machine load. Net speeds are bytes/second;
// a negative value means unknown (no previous sample yet).
type MachineStatus struct {
	CPU         float64
	MemTotal    uint64
	MemUsed     uint64
	SwapTotal   uint64
	SwapUsed    uint64
	DiskTotal   uint64
	DiskUsed    uint64
	NetInSpeed  float64
	NetOutSpeed float64
}

// MachineStatusPayload builds the body of POST /api/v2/server/machine/status
// (validated by MachineController::status). "client" and "version" are
// FNode extras the panel ignores, used to tell FNode apart from xboard-node.
func MachineStatusPayload(s MachineStatus, version string) map[string]any {
	cpu := s.CPU
	if cpu < 0 {
		cpu = 0
	}
	if cpu > 100 {
		cpu = 100
	}
	p := map[string]any{
		"cpu":     cpu,
		"mem":     map[string]any{"total": s.MemTotal, "used": min(s.MemUsed, s.MemTotal)},
		"swap":    map[string]any{"total": s.SwapTotal, "used": min(s.SwapUsed, s.SwapTotal)},
		"disk":    map[string]any{"total": s.DiskTotal, "used": min(s.DiskUsed, s.DiskTotal)},
		"client":  "fnode",
		"version": version,
	}
	if s.NetInSpeed >= 0 && s.NetOutSpeed >= 0 {
		p["net"] = map[string]any{"in_speed": s.NetInSpeed, "out_speed": s.NetOutSpeed}
	}
	return p
}

// ReportMachineStatus posts the machine load to the panel.
func (c *Client) ReportMachineStatus(s MachineStatus, version string) error {
	const path = "/api/v2/server/machine/status"
	r, err := c.client.R().
		SetHeader("Content-Type", "application/json").
		ForceContentType("application/json").
		SetBody(MachineStatusPayload(s, version)).
		Post(path)
	return c.checkResponse(r, path, err)
}

// DiffMachineNodes compares the running node ids with the panel's list and
// returns the nodes to start and the ids to stop, both sorted by id.
func DiffMachineNodes(running []int, wanted []MachineNode) (add []MachineNode, remove []int) {
	have := make(map[int]bool, len(running))
	for _, id := range running {
		have[id] = true
	}
	want := make(map[int]bool, len(wanted))
	for _, n := range wanted {
		if n.ID <= 0 || want[n.ID] {
			continue
		}
		want[n.ID] = true
		if !have[n.ID] {
			add = append(add, n)
		}
	}
	for _, id := range running {
		if !want[id] {
			remove = append(remove, id)
		}
	}
	sort.Slice(add, func(i, j int) bool { return add[i].ID < add[j].ID })
	sort.Ints(remove)
	return add, remove
}
