package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version   = "unknown"   // use ldflags replace
	commit    = "unknown"   // use ldflags replace
	buildDate = "unknown"   // use ldflags replace
	codename  = "FNode"
	intro     = "A V2board backend based on multi core"
)

var versionCommand = cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(_ *cobra.Command, _ []string) {
		showVersion()
	},
}

func init() {
	command.AddCommand(&versionCommand)
}

func showVersion() {
	fmt.Println(`
  _/      _/    _/_/    _/        _/      _/
 _/      _/  _/    _/  _/_/_/      _/  _/
_/      _/      _/    _/    _/      _/
 _/  _/      _/      _/    _/    _/  _/
  _/      _/_/_/_/  _/_/_/    _/      _/
                                                `)
	fmt.Printf("%s\n", codename)
	fmt.Printf("  Version:    %s\n", version)
	fmt.Printf("  Commit:     %s\n", commit)
	fmt.Printf("  Build Date: %s\n", buildDate)
	fmt.Printf("  Description:%s\n", intro)
}
