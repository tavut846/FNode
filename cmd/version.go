package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version   = "unknown" // use ldflags replace
	commit    = "unknown" // use ldflags replace
	buildDate = "unknown" // use ldflags replace
	codename  = "FNode"
	intro     = "A V2board backend based on multi core"
)

var versionCommand = cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(cmd *cobra.Command, _ []string) {
		short, _ := cmd.Flags().GetBool("short")
		if short {
			fmt.Println(version)
			return
		}
		showVersion()
	},
}

func init() {
	versionCommand.Flags().BoolP("short", "s", false, "only print version")
	command.AddCommand(&versionCommand)
}

func showVersion() {
	fmt.Println("--------------------------------------------------")
	fmt.Printf("Codename:    %s\n", codename)
	fmt.Printf("Version:     %s\n", version)
	fmt.Printf("Commit:      %s\n", commit)
	fmt.Printf("Build Date:  %s\n", buildDate)
	fmt.Printf("Platform:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Description: %s\n", intro)
	fmt.Println("--------------------------------------------------")
}
