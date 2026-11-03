package format

import "testing"

// Run for every user on each user-list update (core/sing/user.go) and in the limiter.
func BenchmarkUserEmail(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = UserEmail("[https://domain.com]-hysteria2:296", "11c9573c-abcd-1234-5678-0123456789ab")
	}
}

func BenchmarkUserTag(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = UserTag("[https://domain.com]-hysteria2:296", "11c9573c-abcd-1234-5678-0123456789ab")
	}
}
