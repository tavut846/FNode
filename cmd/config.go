package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tavut846/FNode/conf"
	"github.com/tavut846/FNode/core/sing"
)

var (
	configShowPath    string
	configShowNode    string
	configShowSummary bool
	configShowDir     string
)

var configCommand = cobra.Command{
	Use:   "config",
	Short: "Inspect the configuration FNode is running with",
}

var configShowCommand = cobra.Command{
	Use:   "show",
	Short: "Print the effective sing-box config per node and where each outbound came from",
	Long: `Print the generated <node>.effective.json files: the final sing-box config
FNode applied after merging sing_origin.json, the local CustomOutbounds and the
panel's custom_outbounds/routes, preceded by a summary of outbound tags and
their source (origin, local, panel or builtin).`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return configShow(cmd.OutOrStdout())
	},
}

func init() {
	configShowCommand.Flags().StringVarP(&configShowPath, "config", "c", "/etc/FNode/config.json", "config file path")
	configShowCommand.Flags().StringVarP(&configShowNode, "node", "n", "", "only show this node (tag, file name, or part of the tag)")
	configShowCommand.Flags().BoolVarP(&configShowSummary, "summary", "s", false, "print only the summary, not the JSON")
	configShowCommand.Flags().StringVar(&configShowDir, "dir", "", "generated files directory (default: GeneratedDir, or generated/ next to the config)")
	configCommand.AddCommand(&configShowCommand)
	command.AddCommand(&configCommand)
}

func configShow(w io.Writer) error {
	dir := configShowDir
	if dir == "" {
		c := conf.New()
		if err := c.LoadFromPath(configShowPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v; using the default generated directory\n", err)
			dir = conf.DefaultGeneratedDir(configShowPath)
		} else {
			dir = c.GeneratedDir(configShowPath)
		}
	}
	files, err := sing.ReadEffectiveFiles(dir)
	if err != nil {
		return fmt.Errorf("read generated files in %s: %w (is FNode running with this config?)", dir, err)
	}
	var selected []sing.EffectiveFile
	for _, f := range files {
		if f.Matches(configShowNode) {
			selected = append(selected, f)
		}
	}
	if len(selected) == 0 {
		var tags []string
		for _, f := range files {
			tags = append(tags, f.Meta.Node)
		}
		if configShowNode != "" {
			return fmt.Errorf("no node matches %q in %s; nodes: %s", configShowNode, dir, strings.Join(tags, ", "))
		}
		return fmt.Errorf("no effective config in %s yet (it is written after each apply)", dir)
	}
	for i, f := range selected {
		if i > 0 {
			fmt.Fprintln(w)
		}
		writeSummary(w, f)
		if !configShowSummary {
			fmt.Fprintln(w)
			_, _ = w.Write(f.Data)
		}
	}
	return nil
}

func writeSummary(w io.Writer, f sing.EffectiveFile) {
	m := f.Meta
	fmt.Fprintf(w, "== %s\n", m.Node)
	fmt.Fprintf(w, "   file:      %s\n", f.Path)
	fmt.Fprintf(w, "   generated: %s\n", m.GeneratedAt)
	fmt.Fprintf(w, "   outbounds: %d (origin %d, local %d, panel %d, builtin %d), rules: %d\n",
		len(m.Outbounds), m.Counts[sing.SourceOrigin], m.Counts[sing.SourceLocal],
		m.Counts[sing.SourcePanel], m.Counts[sing.SourceBuiltin], m.Rules)
	outs := append([]sing.OutboundOrigin(nil), m.Outbounds...)
	order := map[string]int{sing.SourcePanel: 0, sing.SourceLocal: 1, sing.SourceOrigin: 2, sing.SourceBuiltin: 3}
	sort.SliceStable(outs, func(i, j int) bool { return order[outs[i].Source] < order[outs[j].Source] })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "   TAG\tTYPE\tSOURCE\tFROM NODE")
	for _, o := range outs {
		fmt.Fprintf(tw, "   %s\t%s\t%s\t%s\n", o.Tag, o.Type, o.Source, o.Node)
	}
	_ = tw.Flush()
}
