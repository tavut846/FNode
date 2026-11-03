package limiter

import (
	"testing"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/format"
	"github.com/tavut846/FNode/conf"
)

// One call per accepted proxy connection (hy2: one per QUIC stream); the hot
// path of core/sing/hook.go.
func benchCheckLimit(b *testing.B, speed int) {
	tag := "bench"
	uuid := "11111111-2222-3333-4444-555555555555"
	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{Id: 1, Uuid: uuid, SpeedLimit: speed}}, map[int]int{})
	defer DeleteLimiter(tag)
	taguuid := format.UserTag(tag, uuid)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, rej := l.CheckLimit(taguuid, "1.2.3.4", true, true); rej {
				b.Fatal("rejected")
			}
		}
	})
}

func BenchmarkCheckLimitNoSpeedLimit(b *testing.B) { benchCheckLimit(b, 0) }
func BenchmarkCheckLimitSpeedLimited(b *testing.B) { benchCheckLimit(b, 100) }
