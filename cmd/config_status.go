package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tavut846/FNode/conf"
	"github.com/tavut846/FNode/core/sing"
)

var (
	configStatusPath string
	configStatusDir  string
)

var configStatusCommand = cobra.Command{
	Use:   "status",
	Short: "Print the panel, masked token and node tags of each config entry",
	Long: `Print, for each Nodes entry of the config: the panel it connects to, its
token (first and last 5 characters) and the node tags it runs. Tags come from
the generated <node>.effective.json files of the running FNode. A node the
panel assigned to a machine (installed from the dashboard) is marked --Panel;
a node entry written in config.json shows its tag alone.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return configStatus(cmd.OutOrStdout())
	},
}

func init() {
	configStatusCommand.Flags().StringVarP(&configStatusPath, "config", "c", "/etc/FNode/config.json", "config file path")
	configStatusCommand.Flags().StringVar(&configStatusDir, "dir", "", "generated files directory (default: GeneratedDir, or generated/ next to the config)")
	configCommand.AddCommand(&configStatusCommand)
}

// maskToken shows the first and last 5 characters of a token. Shorter tokens
// would be shown whole that way, so they keep only their first character.
func maskToken(t string) string {
	switch {
	case t == "":
		return "(empty)"
	case len(t) > 10:
		return t[:5] + "..." + t[len(t)-5:]
	default:
		return t[:1] + "..."
	}
}

// entryOwnsTag reports whether a running node tag belongs to this entry.
// Tags are "[ApiHost]-type:id" unless the entry sets Options.Name.
func entryOwnsTag(n conf.NodeConfig, tag string) bool {
	if n.Options.Name != "" {
		return tag == n.Options.Name
	}
	rest, ok := strings.CutPrefix(tag, "["+n.ApiConfig.APIHost+"]-")
	if !ok {
		return false
	}
	if n.ApiConfig.IsMachine() {
		return true
	}
	return strings.HasSuffix(rest, ":"+strconv.Itoa(n.ApiConfig.NodeID))
}

func configStatus(w io.Writer) error {
	c := conf.New()
	if err := c.LoadFromPath(configStatusPath); err != nil {
		return fmt.Errorf("load %s: %w", configStatusPath, err)
	}
	dir := configStatusDir
	if dir == "" {
		dir = c.GeneratedDir(configStatusPath)
	}
	var tags []string
	if files, err := sing.ReadEffectiveFiles(dir); err == nil {
		for _, f := range files {
			tags = append(tags, f.Meta.Node)
		}
	}
	if len(c.NodeConfig) == 0 {
		fmt.Fprintf(w, "Panel:  (no Nodes entry in %s)\n", configStatusPath)
		return nil
	}
	// Node entries claim their tags first, so a machine entry on the same
	// panel does not list them too.
	claimed := make(map[string]bool)
	for _, n := range c.NodeConfig {
		if !n.ApiConfig.IsMachine() {
			for _, t := range tags {
				if entryOwnsTag(n, t) {
					claimed[t] = true
				}
			}
		}
	}
	for i, n := range c.NodeConfig {
		if i > 0 {
			fmt.Fprintln(w)
		}
		api := n.ApiConfig
		mode := fmt.Sprintf("node %d", api.NodeID)
		if api.IsMachine() {
			mode = fmt.Sprintf("machine %d, from dashboard", api.MachineID)
		}
		fmt.Fprintf(w, "Panel:  %s (%s)\n", api.APIHost, mode)
		fmt.Fprintf(w, "Token:  %s\n", maskToken(api.Token()))
		var own []string
		for _, t := range tags {
			if !entryOwnsTag(n, t) {
				continue
			}
			if api.IsMachine() {
				if claimed[t] {
					continue
				}
				t += " --Panel"
			}
			own = append(own, t)
		}
		if len(own) == 0 {
			fmt.Fprintln(w, "Node:   (none running; FNode not started or not connected to the panel yet)")
			continue
		}
		for j, t := range own {
			label := "Node:  "
			if j > 0 {
				label = "       "
			}
			fmt.Fprintf(w, "%s %s\n", label, t)
		}
	}
	return nil
}
