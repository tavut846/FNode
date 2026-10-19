package sysinfo

import (
	"fmt"
	"testing"
	"time"
)

const memInfo = `MemTotal:        2048000 kB
MemFree:          100000 kB
MemAvailable:    1024000 kB
Buffers:           10000 kB
Cached:           200000 kB
SwapTotal:       1000000 kB
SwapFree:         750000 kB
`

func netDev(rx, tx uint64) string {
	return fmt.Sprintf(`Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 999999 10 0 0 0 0 0 0 999999 10 0 0 0 0 0 0
  eth0: %d 100 0 0 0 0 0 0 %d 100 0 0 0 0 0 0
docker0: 5000 1 0 0 0 0 0 0 5000 1 0 0 0 0 0 0
`, rx, tx)
}

func TestParsers(t *testing.T) {
	idle, total, ok := ParseCPUStat("cpu  100 0 100 700 100 0 0 0 50 0\ncpu0 1 2 3 4\n")
	if !ok || idle != 800 || total != 1000 {
		t.Errorf("cpu: idle %d total %d ok %v", idle, total, ok)
	}
	mt, mu, st, su := ParseMemInfo(memInfo)
	if mt != 2048000*1024 || mu != 1024000*1024 || st != 1000000*1024 || su != 250000*1024 {
		t.Errorf("mem: %d %d %d %d", mt, mu, st, su)
	}
	// Old kernels without MemAvailable
	_, mu, _, _ = ParseMemInfo("MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 100 kB\nCached: 300 kB\n")
	if mu != 500*1024 {
		t.Errorf("mem without MemAvailable: %d", mu)
	}
	rx, tx := ParseNetDev(netDev(1000, 2000))
	if rx != 1000 || tx != 2000 {
		t.Errorf("net: rx %d tx %d (lo and docker must be skipped)", rx, tx)
	}
}

func TestSampler(t *testing.T) {
	files := map[string]string{
		"/proc/stat":    "cpu 100 0 100 700 100 0 0 0\n",
		"/proc/meminfo": memInfo,
		"/proc/net/dev": netDev(1000, 2000),
	}
	now := time.Unix(1000, 0)
	s := NewSampler()
	s.readFileF = func(p string) (string, error) {
		if v, ok := files[p]; ok {
			return v, nil
		}
		return "", fmt.Errorf("missing %s", p)
	}
	s.now = func() time.Time { return now }
	s.statDisk = func(string) (uint64, uint64) { return 100, 40 }

	first := s.Collect()
	if first.CPU != 0 || first.NetInSpeed != -1 || first.DiskUsed != 40 || first.MemUsed == 0 {
		t.Errorf("first sample: %+v", first)
	}
	files["/proc/stat"] = "cpu 200 0 200 1300 100 0 0 0\n" // +800 total, +600 idle => 25%
	files["/proc/net/dev"] = netDev(11000, 4000)
	now = now.Add(10 * time.Second)
	second := s.Collect()
	if second.CPU != 25 {
		t.Errorf("cpu: %v", second.CPU)
	}
	if second.NetInSpeed != 1000 || second.NetOutSpeed != 200 {
		t.Errorf("net speed: in %v out %v", second.NetInSpeed, second.NetOutSpeed)
	}
}
