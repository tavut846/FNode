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

func TestFormatDevTag(t *testing.T) {
	tests := []struct {
		version   string
		commitNum int
		expected  string
	}{
		{"0.0.2", 1, "0.0.2-pre-1"},
		{"v0.0.2", 5, "0.0.2-pre-5"},
		{"0.0.13", 9, "0.0.13-pre-9"},
		{"0.0.13", 0, "0.0.13-pre-1"},
		{"0.0.13", -2, "0.0.13-pre-1"},
	}

	for _, tt := range tests {
		result := FormatDevTag(tt.version, tt.commitNum)
		if result != tt.expected {
			t.Errorf("FormatDevTag(%q, %d) = %q, expected %q", tt.version, tt.commitNum, result, tt.expected)
		}
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
			expectedTag:     "0.0.2-pre-1",
			expectedPre:     true,
			expectedLatest:  false,
		},
		{
			name:            "Dev_new push upgrade from 0.0.12",
			refName:         "refs/heads/dev_new",
			latestStableTag: "0.0.12",
			commitsSince:    9,
			expectedTag:     "0.0.13-pre-9",
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
			name:            "Explicit pre-release tag",
			refName:         "refs/tags/v0.0.15-pre-2",
			latestStableTag: "0.0.12",
			commitsSince:    0,
			expectedTag:     "0.0.15-pre-2",
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
