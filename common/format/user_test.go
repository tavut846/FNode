package format

import (
	"testing"
)

func TestUserTag(t *testing.T) {
	tag := "[https://domain.com]-shadowsocks:296"
	uuid := "11c9573c-abcd-1234-5678-0123456789ab"
	expected := "[https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab"
	if got := UserTag(tag, uuid); got != expected {
		t.Errorf("UserTag() = %v, want %v", got, expected)
	}
}

func TestUserEmail(t *testing.T) {
	tag := "[https://domain.com]-shadowsocks:296"
	uuid := "11c9573c-abcd-1234-5678-0123456789ab"
	expected := "email: [https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab"
	if got := UserEmail(tag, uuid); got != expected {
		t.Errorf("UserEmail() = %v, want %v", got, expected)
	}
}

func TestExtractUUID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "v2bx email format",
			input:    "email: [https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab",
			expected: "11c9573c-abcd-1234-5678-0123456789ab",
		},
		{
			name:     "tag pipe uuid format",
			input:    "[https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab",
			expected: "11c9573c-abcd-1234-5678-0123456789ab",
		},
		{
			name:     "plain uuid format",
			input:    "11c9573c-abcd-1234-5678-0123456789ab",
			expected: "11c9573c-abcd-1234-5678-0123456789ab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractUUID(tt.input); got != tt.expected {
				t.Errorf("ExtractUUID(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractTagUUID(t *testing.T) {
	inboundTag := "[https://domain.com]-shadowsocks:296"
	tests := []struct {
		name     string
		inbound  string
		user     string
		expected string
	}{
		{
			name:     "v2bx email format",
			inbound:  inboundTag,
			user:     "email: [https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab",
			expected: "[https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab",
		},
		{
			name:     "tag pipe uuid format",
			inbound:  inboundTag,
			user:     "[https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab",
			expected: "[https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab",
		},
		{
			name:     "plain uuid format",
			inbound:  inboundTag,
			user:     "11c9573c-abcd-1234-5678-0123456789ab",
			expected: "[https://domain.com]-shadowsocks:296|11c9573c-abcd-1234-5678-0123456789ab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractTagUUID(tt.inbound, tt.user); got != tt.expected {
				t.Errorf("ExtractTagUUID(%q, %q) = %v, want %v", tt.inbound, tt.user, got, tt.expected)
			}
		})
	}
}
