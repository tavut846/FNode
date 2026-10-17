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

// NextUnusedPatchVersion calculates the next incremental patch version that does not conflict with any existing tags.
func NextUnusedPatchVersion(latestTag string, existingTags []string) string {
	next := NextPatchVersion(latestTag)
	existing := make(map[string]bool)
	for _, t := range existingTags {
		clean := strings.TrimPrefix(strings.TrimSpace(t), "v")
		existing[clean] = true
	}
	for existing[next] {
		next = NextPatchVersion(next)
	}
	return next
}

// FormatDevTag formats a pre-release version tag in the form <targetVersion>-pre.
// If an optional commitNum > 0 is provided, it formats as <targetVersion>-pre-<commitNum>.
func FormatDevTag(targetVersion string, commitNum ...int) string {
	targetVersion = strings.TrimPrefix(strings.TrimSpace(targetVersion), "v")
	if len(commitNum) > 0 && commitNum[0] > 0 {
		return fmt.Sprintf("%s-pre-%d", targetVersion, commitNum[0])
	}
	return fmt.Sprintf("%s-pre", targetVersion)
}

// ResolveReleaseMetadata determines the release tag, pre-release state, and latest state based on the ref and git context.
// Master/main/stable builds increment the patch version to create a new Latest Release.
// Dev/dev_new builds create and update a rolling pre-release tag (<targetVersion>-pre).
func ResolveReleaseMetadata(refName string, latestStableTag string, commitsSince int) (tagName string, isPreRelease bool, isLatest bool) {
	ref := strings.TrimSpace(refName)
	cleanRef := strings.TrimPrefix(ref, "refs/heads/")
	cleanRef = strings.TrimPrefix(cleanRef, "refs/tags/")

	if cleanRef == "master" || cleanRef == "main" || cleanRef == "stable" {
		tagName = NextPatchVersion(latestStableTag)
		return tagName, false, true
	}

	if cleanRef == "dev" || cleanRef == "dev_new" || strings.HasPrefix(cleanRef, "dev/") {
		target := NextPatchVersion(latestStableTag)
		tagName = FormatDevTag(target)
		return tagName, true, false
	}

	// Tag or other ref
	tagName = strings.TrimPrefix(cleanRef, "v")
	if strings.Contains(tagName, "-pre") || strings.Contains(tagName, "-") {
		return tagName, true, false
	}
	return tagName, false, true
}

// ShouldCleanupPreRelease determines whether a given pre-release tag should be cleaned up
// when an official version is published. It returns true if preReleaseTag is marked as a
// pre-release and its base version is less than or equal to the official release version.
func ShouldCleanupPreRelease(preReleaseTag, officialTag string) bool {
	preClean := strings.TrimPrefix(strings.TrimSpace(preReleaseTag), "v")
	offClean := strings.TrimPrefix(strings.TrimSpace(officialTag), "v")

	// Must be a pre-release tag (contains "-")
	if !strings.Contains(preClean, "-") {
		return false
	}

	preBase := strings.Split(preClean, "-")[0]
	offBase := strings.Split(offClean, "-")[0]

	preParts := strings.Split(preBase, ".")
	offParts := strings.Split(offBase, ".")

	if len(preParts) != 3 || len(offParts) != 3 {
		return strings.HasPrefix(preClean, offBase+"-pre")
	}

	for i := 0; i < 3; i++ {
		pNum, err1 := strconv.Atoi(preParts[i])
		oNum, err2 := strconv.Atoi(offParts[i])
		if err1 != nil || err2 != nil {
			return strings.HasPrefix(preClean, offBase+"-pre")
		}
		if pNum < oNum {
			return true
		}
		if pNum > oNum {
			return false
		}
	}

	// Base versions are equal (e.g. 0.0.22-pre vs 0.0.22) -> cleanup
	return true
}

// ResolveReleaseTarget resolves the intended release tag based on user selection or argument.
// If target is "pre", "prerelease", or "pre-release", it returns the latest pre-release tag.
// If target is "latest" or empty, it returns the latest official release tag.
// Otherwise, it returns the custom specified version string.
func ResolveReleaseTarget(target, latestOfficial, latestPreRelease string) string {
	t := strings.ToLower(strings.TrimSpace(target))
	switch t {
	case "pre", "prerelease", "pre-release":
		return latestPreRelease
	case "latest", "":
		return latestOfficial
	default:
		return strings.TrimSpace(target)
	}
}

