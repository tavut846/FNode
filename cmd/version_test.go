package cmd

import (
	"testing"
)

func TestNextPatchVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0.0.1", "0.0.2"},
		{"v0.0.1", "0.0.2"},
		{"0.0.12", "0.0.13"},
		{"v0.0.12", "0.0.13"},
		{"1.2.3", "1.2.4"},
		{"", "0.0.2"},
		{"invalid", "0.0.2"},
	}

	for _, tt := range tests {
		result := NextPatchVersion(tt.input)
		if result != tt.expected {
			t.Errorf("NextPatchVersion(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestNextUnusedPatchVersion(t *testing.T) {
	tests := []struct {
		latest       string
		existingTags []string
		expected     string
	}{
		{
			latest:       "0.0.12",
			existingTags: []string{"0.0.13", "0.0.14"},
			expected:     "0.0.15",
		},
		{
			latest:       "0.0.13",
			existingTags: []string{"0.0.1", "0.0.12", "0.0.13"},
			expected:     "0.0.14",
		},
		{
			latest:       "0.0.1",
			existingTags: []string{"v0.0.2", "0.0.3"},
			expected:     "0.0.4",
		},
		{
			latest:       "0.0.9",
			existingTags: []string{"0.0.10"},
			expected:     "0.0.11",
		},
	}

	for _, tt := range tests {
		result := NextUnusedPatchVersion(tt.latest, tt.existingTags)
		if result != tt.expected {
			t.Errorf("NextUnusedPatchVersion(%q, %v) = %q, expected %q", tt.latest, tt.existingTags, result, tt.expected)
		}
	}
}

func TestFormatDevTag(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		commitNum []int
		expected  string
	}{
		{"Rolling dev tag without commit", "0.0.2", nil, "0.0.2-pre"},
		{"Rolling dev tag with v prefix", "v0.0.2", nil, "0.0.2-pre"},
		{"Numbered dev tag with commit", "0.0.13", []int{9}, "0.0.13-pre-9"},
		{"Zero commit returns rolling dev tag", "0.0.13", []int{0}, "0.0.13-pre"},
		{"Negative commit returns rolling dev tag", "0.0.13", []int{-2}, "0.0.13-pre"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDevTag(tt.version, tt.commitNum...)
			if result != tt.expected {
				t.Errorf("FormatDevTag(%q, %v) = %q, expected %q", tt.version, tt.commitNum, result, tt.expected)
			}
		})
	}
}

func TestResolveReleaseMetadata(t *testing.T) {
	tests := []struct {
		name            string
		refName         string
		latestStableTag string
		commitsSince    int
		expectedTag     string
		expectedPre     bool
		expectedLatest  bool
	}{
		{
			name:            "Master push upgrade from 0.0.1",
			refName:         "master",
			latestStableTag: "0.0.1",
			commitsSince:    1,
			expectedTag:     "0.0.2",
			expectedPre:     false,
			expectedLatest:  true,
		},
		{
			name:            "Stable branch upgrade from 0.0.13",
			refName:         "refs/heads/stable",
			latestStableTag: "0.0.13",
			commitsSince:    1,
			expectedTag:     "0.0.14",
			expectedPre:     false,
			expectedLatest:  true,
		},
		{
			name:            "Master push upgrade from 0.0.12",
			refName:         "refs/heads/master",
			latestStableTag: "0.0.12",
			commitsSince:    5,
			expectedTag:     "0.0.13",
			expectedPre:     false,
			expectedLatest:  true,
		},
		{
			name:            "Dev push upgrade from 0.0.1",
			refName:         "dev",
			latestStableTag: "0.0.1",
			commitsSince:    1,
			expectedTag:     "0.0.2-pre",
			expectedPre:     true,
			expectedLatest:  false,
		},
		{
			name:            "Dev_new push upgrade from 0.0.12",
			refName:         "refs/heads/dev_new",
			latestStableTag: "0.0.12",
			commitsSince:    9,
			expectedTag:     "0.0.13-pre",
			expectedPre:     true,
			expectedLatest:  false,
		},
		{
			name:            "Explicit release tag",
			refName:         "refs/tags/0.0.15",
			latestStableTag: "0.0.12",
			commitsSince:    0,
			expectedTag:     "0.0.15",
			expectedPre:     false,
			expectedLatest:  true,
		},
		{
			name:            "Explicit numbered pre-release tag",
			refName:         "refs/tags/v0.0.15-pre-2",
			latestStableTag: "0.0.12",
			commitsSince:    0,
			expectedTag:     "0.0.15-pre-2",
			expectedPre:     true,
			expectedLatest:  false,
		},
		{
			name:            "Explicit rolling pre-release tag",
			refName:         "refs/tags/v0.0.15-pre",
			latestStableTag: "0.0.12",
			commitsSince:    0,
			expectedTag:     "0.0.15-pre",
			expectedPre:     true,
			expectedLatest:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, isPre, isLatest := ResolveReleaseMetadata(tt.refName, tt.latestStableTag, tt.commitsSince)
			if tag != tt.expectedTag || isPre != tt.expectedPre || isLatest != tt.expectedLatest {
				t.Errorf("ResolveReleaseMetadata(%q, %q, %d) = (%q, %v, %v), expected (%q, %v, %v)",
					tt.refName, tt.latestStableTag, tt.commitsSince, tag, isPre, isLatest, tt.expectedTag, tt.expectedPre, tt.expectedLatest)
			}
		})
	}
}

func TestShouldCleanupPreRelease(t *testing.T) {
	tests := []struct {
		name          string
		preReleaseTag string
		officialTag   string
		expected      bool
	}{
		{
			name:          "Matching rolling pre-release tag",
			preReleaseTag: "0.0.22-pre",
			officialTag:   "0.0.22",
			expected:      true,
		},
		{
			name:          "Matching rolling pre-release with v prefix",
			preReleaseTag: "v0.0.22-pre",
			officialTag:   "v0.0.22",
			expected:      true,
		},
		{
			name:          "Older numbered pre-release tag",
			preReleaseTag: "0.0.21-pre-1",
			officialTag:   "0.0.22",
			expected:      true,
		},
		{
			name:          "Older rolling pre-release tag",
			preReleaseTag: "0.0.20-pre",
			officialTag:   "0.0.22",
			expected:      true,
		},
		{
			name:          "Future pre-release tag should NOT be cleaned up",
			preReleaseTag: "0.0.23-pre",
			officialTag:   "0.0.22",
			expected:      false,
		},
		{
			name:          "Official stable tag is not a pre-release",
			preReleaseTag: "0.0.22",
			officialTag:   "0.0.22",
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldCleanupPreRelease(tt.preReleaseTag, tt.officialTag)
			if result != tt.expected {
				t.Errorf("ShouldCleanupPreRelease(%q, %q) = %v, expected %v",
					tt.preReleaseTag, tt.officialTag, result, tt.expected)
			}
		})
	}
}

func TestResolveReleaseTarget(t *testing.T) {
	official := "0.0.21"
	prerelease := "0.0.22-pre"

	tests := []struct {
		target   string
		expected string
	}{
		{"", official},
		{"latest", official},
		{"LATEST", official},
		{"pre", prerelease},
		{"PRE", prerelease},
		{"prerelease", prerelease},
		{"pre-release", prerelease},
		{"0.0.20", "0.0.20"},
		{"0.0.22-pre", "0.0.22-pre"},
		{"v1.0.0", "v1.0.0"},
	}

	for _, tt := range tests {
		t.Run("target_"+tt.target, func(t *testing.T) {
			result := ResolveReleaseTarget(tt.target, official, prerelease)
			if result != tt.expected {
				t.Errorf("ResolveReleaseTarget(%q, %q, %q) = %q, expected %q",
					tt.target, official, prerelease, result, tt.expected)
			}
		})
	}
}
