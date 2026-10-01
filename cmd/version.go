package cmd

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

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

// NextPatchVersion calculates the next incremental patch version from a semantic version string.
// If the input is empty or invalid, it defaults to "0.0.2".
func NextPatchVersion(latestTag string) string {
	clean := strings.TrimPrefix(strings.TrimSpace(latestTag), "v")
	parts := strings.Split(clean, ".")
	if len(parts) != 3 {
		return "0.0.2"
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "0.0.2"
	}
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}

// FormatDevTag formats a pre-release version tag in the form <targetVersion>-pre-<commitNum>.
func FormatDevTag(targetVersion string, commitNum int) string {
	if commitNum <= 0 {
		commitNum = 1
	}
	targetVersion = strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	return fmt.Sprintf("%s-pre-%d", targetVersion, commitNum)
}

// ResolveReleaseMetadata determines the release tag, pre-release state, and latest state based on the ref and git context.
func ResolveReleaseMetadata(refName string, latestStableTag string, commitsSince int) (tagName string, isPreRelease bool, isLatest bool) {
	ref := strings.TrimSpace(refName)
	cleanRef := strings.TrimPrefix(ref, "refs/heads/")
	cleanRef = strings.TrimPrefix(cleanRef, "refs/tags/")

	if cleanRef == "master" || cleanRef == "main" {
		tagName = NextPatchVersion(latestStableTag)
		return tagName, false, true
	}

	if cleanRef == "dev" || cleanRef == "dev_new" || strings.HasPrefix(cleanRef, "dev/") {
		target := NextPatchVersion(latestStableTag)
		tagName = FormatDevTag(target, commitsSince)
		return tagName, true, false
	}

	// Tag or other ref
	tagName = strings.TrimPrefix(cleanRef, "v")
	if strings.Contains(tagName, "-pre-") || strings.Contains(tagName, "-") {
		return tagName, true, false
	}
	return tagName, false, true
}

