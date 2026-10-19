package node

import (
	"fmt"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
	vCore "github.com/tavut846/FNode/core"
)

type Node struct {
	controllers []*Controller
	machines    []*Machine
}

func New() *Node {
	return &Node{}
}

func (n *Node) Start(nodes []conf.NodeConfig, core vCore.Core) error {
	n.controllers = nil
	n.machines = nil
	for i := range nodes {
		if nodes[i].ApiConfig.IsMachine() {
			// Machine mode: one entry runs every node the panel assigns to the machine
			m, err := NewMachine(nodes[i], core)
			if err != nil {
				return fmt.Errorf("start machine [%s-%d] error: %s",
					nodes[i].ApiConfig.APIHost, nodes[i].ApiConfig.MachineID, err)
			}
			if err = m.Start(); err != nil {
				return fmt.Errorf("start machine [%s-%d] error: %s",
					nodes[i].ApiConfig.APIHost, nodes[i].ApiConfig.MachineID, err)
			}
			n.machines = append(n.machines, m)
			continue
		}
		p, err := panel.New(&nodes[i].ApiConfig)
		if err != nil {
			return err
		}
		// Register controller service
		c := NewController(core, p, &nodes[i].Options)
		n.controllers = append(n.controllers, c)
		err = c.Start()
		if err != nil {
			return fmt.Errorf("start node controller [%s-%s-%d] error: %s",
				nodes[i].ApiConfig.APIHost,
				nodes[i].ApiConfig.NodeType,
				nodes[i].ApiConfig.NodeID,
				err)
		}
	}
	return nil
}

func (n *Node) Close() {
	for _, m := range n.machines {
		m.Close()
	}
	n.machines = nil
	for _, c := range n.controllers {
		err := c.Close()
		if err != nil {
			panic(err)
		}
	}
	n.controllers = nil
}
