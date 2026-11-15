package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tavut846/FNode/conf"
)

var configIgnorePath string

var configIgnoreCommand = cobra.Command{
	Use:   "ignore-panel-outbounds",
	Short: "Print whether this binary reads IgnorePanelOutbounds as on or off",
	Long: `Load the config the way the server does and print what the running binary
will use: "on", "off" or "tags a,b". Only binaries that know the option have
this command, so FNode.sh uses it to tell "switch is off" from "binary too old".`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return configIgnore(cmd.OutOrStdout())
	},
}

func init() {
	configIgnoreCommand.Flags().StringVarP(&configIgnorePath, "config", "c", "/etc/FNode/config.json", "config file path")
	configCommand.AddCommand(&configIgnoreCommand)
}

// configIgnore prints the effective IgnorePanelOutbounds state, after the
// top-level key has been merged into every sing core (as the server does).
func configIgnore(w io.Writer) error {
	c := conf.New()
	if err := c.LoadFromPath(configIgnorePath); err != nil {
		return fmt.Errorf("load %s: %w", configIgnorePath, err)
	}
	c.SetGeneratedDirs(configIgnorePath)
	all := c.IgnorePanelOutbounds
	var tags []string
	for _, core := range c.CoresConfig {
		if core.SingConfig == nil {
			continue
		}
		all = all || core.SingConfig.IgnorePanelOutbounds
		for _, t := range core.SingConfig.IgnorePanelOutboundTags {
			tags = append(tags, t)
		}
	}
	if len(c.CoresConfig) == 0 {
		tags = c.IgnorePanelOutboundTags
	}
	switch {
	case all:
		fmt.Fprintln(w, "on")
	case len(tags) > 0:
		fmt.Fprintf(w, "tags %s\n", strings.Join(tags, ","))
	default:
		fmt.Fprintln(w, "off")
	}
	return nil
}
