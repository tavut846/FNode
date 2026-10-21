package sing

import "github.com/tavut846/FNode/common/logship"

// reportMissingOutbounds tells the log shippers which outbound tags each
// node's routes need but FNode does not have, so the panel (CustomNode
// plugin) can add those outbounds to the node. Nodes without missing tags are
// reported too, which clears an earlier request.
func reportMissingOutbounds(missing map[string][]string, nodes []nodeRouting) {
	for _, n := range nodes {
		logship.Default.ReportMissing(n.tag, missing[n.tag])
	}
}
