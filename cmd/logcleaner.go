package cmd

import (
	"os"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/tavut846/FNode/common/exec"
)

// DefaultCommonLogPaths defines standard log locations for FNode and Caddy
var DefaultCommonLogPaths = []string{
	"/var/log/FNode.log",
	"/var/log/fnode.log",
	"/var/log/fnode.error.log",
	"/usr/local/FNode/box.log",
	"/usr/local/FNode/fnode.log",
	"/etc/FNode/box.log",
	"/etc/FNode/fnode.log",
	"/var/log/caddy/caddy.log",
	"/var/log/caddy/access.log",
}

// CleanLogs cleans systemd journal logs (if supported on Linux) and truncates known log files
func CleanLogs(extraPaths ...string) []string {
	// Rotate and vacuum systemd journal for FNode and caddy on Linux
	if runtime.GOOS == "linux" {
		_, _ = exec.RunCommandByShell("journalctl --rotate && journalctl --vacuum-time=1s --unit=FNode.service")
		_, _ = exec.RunCommandByShell("journalctl --vacuum-time=1s --unit=caddy.service 2>/dev/null")
		_, _ = exec.RunCommandByShell("journalctl --vacuum-size=20M")
	}

	var cleaned []string
	allPaths := append([]string{}, DefaultCommonLogPaths...)
	for _, p := range extraPaths {
		if p != "" {
			allPaths = append(allPaths, p)
		}
	}

	seen := make(map[string]bool)
	for _, p := range allPaths {
		if seen[p] {
			continue
		}
		seen[p] = true

		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			if err := os.Truncate(p, 0); err == nil {
				cleaned = append(cleaned, p)
			}
		}
	}
	return cleaned
}

// startAutoLogCleaner launches a background routine to clean logs every interval (default 7 days)
func startAutoLogCleaner(interval time.Duration, extraPaths ...string) func() {
	if interval <= 0 {
		interval = 7 * 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	stopChan := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				cleaned := CleanLogs(extraPaths...)
				log.WithFields(log.Fields{
					"cleaned_files": len(cleaned),
					"interval":      interval.String(),
				}).Info("Auto cleaned up logs (7-day interval)")
			case <-stopChan:
				ticker.Stop()
				return
			}
		}
	}()

	return func() {
		close(stopChan)
	}
}
