package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
)

// Watcher monitors configuration files and related assets for changes and triggers reload.
type Watcher struct {
	watcher      *fsnotify.Watcher
	filePath     string
	reload       func()
	conf         *Conf
	watchedPaths map[string]struct{}
	watchedDirs  map[string]struct{}
	debounce     time.Duration
	mu           sync.Mutex
	timer        *time.Timer
	stopCh       chan struct{}
	stopped      bool
}

// Watch starts watching the main configuration file and its related assets (e.g. OriginalPath).
func (p *Conf) Watch(filePath string, reload func(), extraPaths ...string) error {
	_, err := p.WatchWithWatcher(filePath, reload, extraPaths...)
	return err
}

// WatchWithWatcher starts watching files and returns the Watcher instance for lifecycle management.
func (p *Conf) WatchWithWatcher(filePath string, reload func(), extraPaths ...string) (*Watcher, error) {
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve abs config path error: %w", err)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("new watcher error: %w", err)
	}

	w := &Watcher{
		watcher:      fsw,
		filePath:     absFilePath,
		reload:       reload,
		conf:         p,
		watchedPaths: make(map[string]struct{}),
		watchedDirs:  make(map[string]struct{}),
		debounce:     1 * time.Second,
		stopCh:       make(chan struct{}),
	}

	w.updatePaths(extraPaths)
	go w.loop()

	return w, nil
}

// SetDebounce updates the debounce duration (useful for fast unit tests).
func (w *Watcher) SetDebounce(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.debounce = d
}

// Close stops the watcher and frees filesystem resources.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return nil
	}
	w.stopped = true
	if w.timer != nil {
		w.timer.Stop()
	}
	close(w.stopCh)
	w.mu.Unlock()
	return w.watcher.Close()
}

func (w *Watcher) updatePaths(extraPaths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	pathsToWatch := make(map[string]struct{})
	pathsToWatch[w.filePath] = struct{}{}

	for _, ep := range extraPaths {
		if ep != "" {
			if abs, err := filepath.Abs(ep); err == nil {
				pathsToWatch[abs] = struct{}{}
			}
		}
	}

	if w.conf != nil {
		for _, core := range w.conf.CoresConfig {
			if core.SingConfig != nil && core.SingConfig.OriginalPath != "" {
				if abs, err := filepath.Abs(core.SingConfig.OriginalPath); err == nil {
					pathsToWatch[abs] = struct{}{}
				}
			}
		}
		for _, node := range w.conf.NodeConfig {
			if node.Options.CertConfig != nil {
				if node.Options.CertConfig.CertFile != "" {
					if abs, err := filepath.Abs(node.Options.CertConfig.CertFile); err == nil {
						pathsToWatch[abs] = struct{}{}
					}
				}
				if node.Options.CertConfig.KeyFile != "" {
					if abs, err := filepath.Abs(node.Options.CertConfig.KeyFile); err == nil {
						pathsToWatch[abs] = struct{}{}
					}
				}
			}
		}
	}

	w.watchedPaths = pathsToWatch

	// Register directories and files in fsnotify
	for p := range pathsToWatch {
		dir := filepath.Dir(p)
		if _, exists := w.watchedDirs[dir]; !exists {
			if err := w.watcher.Add(dir); err == nil {
				w.watchedDirs[dir] = struct{}{}
			}
		}
		_ = w.watcher.Add(p)
	}
}

func (w *Watcher) loop() {
	for {
		select {
		case <-w.stopCh:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Op == fsnotify.Chmod {
				continue
			}

			cleanName := filepath.Clean(event.Name)
			baseName := filepath.Base(cleanName)

			// Ignore editor temporary / lock files
			if strings.HasSuffix(baseName, "~") ||
				strings.HasSuffix(baseName, ".swp") ||
				strings.HasSuffix(baseName, ".tmp") ||
				strings.HasPrefix(baseName, ".#") ||
				baseName == "4913" {
				continue
			}

			w.mu.Lock()
			matched := false
			for target := range w.watchedPaths {
				if cleanName == target || (filepath.Dir(cleanName) == filepath.Dir(target) && baseName == filepath.Base(target)) {
					matched = true
					break
				}
			}
			if !matched {
				w.mu.Unlock()
				continue
			}

			// Debounce reload
			if w.timer == nil {
				w.timer = time.AfterFunc(w.debounce, w.triggerReload)
			} else {
				w.timer.Reset(w.debounce)
			}
			w.mu.Unlock()

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.WithField("err", err).Warn("Config watcher error")
		}
	}
}

func (w *Watcher) triggerReload() {
	w.mu.Lock()
	w.timer = nil
	w.mu.Unlock()

	// 1. Validate that the new configuration loads and parses cleanly
	newConf := New()
	if err := newConf.LoadFromPath(w.filePath); err != nil {
		log.WithField("err", err).Error("Config reload aborted: configuration parsing error, keeping current running config")
		return
	}

	// 2. Validate that declared OriginalPath files are accessible if configured
	for _, core := range newConf.CoresConfig {
		if core.SingConfig != nil && core.SingConfig.OriginalPath != "" {
			if _, err := os.Stat(core.SingConfig.OriginalPath); err != nil {
				log.WithField("err", err).Errorf("Config reload aborted: OriginalPath %q not accessible, keeping current running config", core.SingConfig.OriginalPath)
				return
			}
		}
	}

	log.Info("Configuration or related file changed, reloading...")
	*w.conf = *newConf
	w.updatePaths(nil)

	if w.reload != nil {
		w.reload()
	}
	log.Info("Configuration reloaded successfully")
}
