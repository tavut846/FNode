//go:build !(linux || darwin || freebsd)

package sysinfo

func diskUsage(string) (total, used uint64) { return 0, 0 }
