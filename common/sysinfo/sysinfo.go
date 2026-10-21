// Package sysinfo samples machine load (CPU, memory, swap, disk, network)
// for the Xboard machine status report. It reads /proc on Linux; elsewhere
// the values it cannot read stay zero.
package sysinfo

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Status is one sample. Net speeds are bytes/second, -1 when unknown (the
// first sample has no previous counters to compare with).
type Status struct {
	CPU         float64
	MemTotal    uint64
	MemUsed     uint64
	SwapTotal   uint64
	SwapUsed    uint64
	DiskTotal   uint64
	DiskUsed    uint64
	NetInSpeed  float64
	NetOutSpeed float64
}

// Sampler keeps the previous CPU and network counters between samples.
type Sampler struct {
	mu        sync.Mutex
	procRoot  string
	diskPath  string
	cpuIdle   uint64
	cpuTotal  uint64
	hasCPU    bool
	netRx     uint64
	netTx     uint64
	netAt     time.Time
	hasNet    bool
	statDisk  func(path string) (total, used uint64)
	now       func() time.Time
	readFileF func(string) (string, error)
}

// NewSampler returns a sampler for the root filesystem.
func NewSampler() *Sampler {
	return &Sampler{
		procRoot:  "/proc",
		diskPath:  "/",
		statDisk:  diskUsage,
		now:       time.Now,
		readFileF: readFile,
	}
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// Collect takes a sample.
func (s *Sampler) Collect() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{NetInSpeed: -1, NetOutSpeed: -1}

	if data, err := s.readFileF(s.procRoot + "/stat"); err == nil {
		if idle, total, ok := ParseCPUStat(data); ok {
			if s.hasCPU && total > s.cpuTotal {
				dt := float64(total - s.cpuTotal)
				di := float64(idle - s.cpuIdle)
				if idle < s.cpuIdle {
					di = 0
				}
				st.CPU = (1 - di/dt) * 100
			}
			s.cpuIdle, s.cpuTotal, s.hasCPU = idle, total, true
		}
	}
	if data, err := s.readFileF(s.procRoot + "/meminfo"); err == nil {
		st.MemTotal, st.MemUsed, st.SwapTotal, st.SwapUsed = ParseMemInfo(data)
	}
	if s.statDisk != nil {
		st.DiskTotal, st.DiskUsed = s.statDisk(s.diskPath)
	}
	if data, err := s.readFileF(s.procRoot + "/net/dev"); err == nil {
		rx, tx := ParseNetDev(data)
		now := s.now()
		if s.hasNet {
			if secs := now.Sub(s.netAt).Seconds(); secs > 0 && rx >= s.netRx && tx >= s.netTx {
				st.NetInSpeed = float64(rx-s.netRx) / secs
				st.NetOutSpeed = float64(tx-s.netTx) / secs
			}
		}
		s.netRx, s.netTx, s.netAt, s.hasNet = rx, tx, now, true
	}
	if st.CPU < 0 {
		st.CPU = 0
	}
	if st.CPU > 100 {
		st.CPU = 100
	}
	return st
}

// ParseCPUStat reads the aggregate "cpu" line of /proc/stat and returns the
// idle (idle + iowait) and total jiffies.
func ParseCPUStat(data string) (idle, total uint64, ok bool) {
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		for i, f := range fields[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			// guest and guest_nice (fields 9, 10) are already in user/nice
			if i >= 8 {
				break
			}
			total += v
			if i == 3 || i == 4 {
				idle += v
			}
		}
		return idle, total, true
	}
	return 0, 0, false
}

// ParseMemInfo reads /proc/meminfo and returns bytes. Used memory is
// MemTotal - MemAvailable (MemFree + Buffers + Cached on old kernels).
func ParseMemInfo(data string) (memTotal, memUsed, swapTotal, swapUsed uint64) {
	vals := make(map[string]uint64)
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		key, rest, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
			v *= 1024
		}
		vals[key] = v
	}
	memTotal = vals["MemTotal"]
	avail, ok := vals["MemAvailable"]
	if !ok {
		avail = vals["MemFree"] + vals["Buffers"] + vals["Cached"]
	}
	if avail < memTotal {
		memUsed = memTotal - avail
	}
	swapTotal = vals["SwapTotal"]
	if free := vals["SwapFree"]; free < swapTotal {
		swapUsed = swapTotal - free
	}
	return
}

// skipInterface filters loopback and virtual interfaces out of the traffic sum.
func skipInterface(name string) bool {
	for _, p := range []string{"lo", "docker", "veth", "br-", "virbr", "vnet", "tun", "tap", "sing", "utun"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// ParseNetDev sums received and transmitted bytes of physical interfaces in
// /proc/net/dev.
func ParseNetDev(data string) (rx, tx uint64) {
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		name, rest, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if skipInterface(name) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		r, err1 := strconv.ParseUint(fields[0], 10, 64)
		t, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		rx += r
		tx += t
	}
	return
}
