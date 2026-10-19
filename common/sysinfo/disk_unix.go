//go:build linux || darwin || freebsd

package sysinfo

import "syscall"

func diskUsage(path string) (total, used uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := uint64(st.Bsize)
	total = st.Blocks * bsize
	free := st.Bfree * bsize
	if free < total {
		used = total - free
	}
	return total, used
}
