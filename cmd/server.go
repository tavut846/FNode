package cmd

import (
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tavut846/FNode/common/logship"
	"github.com/tavut846/FNode/conf"
	vCore "github.com/tavut846/FNode/core"
	"github.com/tavut846/FNode/limiter"
	"github.com/tavut846/FNode/node"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	config string
	watch  bool
)

var serverCommand = cobra.Command{
	Use:   "server",
	Short: "Run FNode server",
	Run:   serverHandle,
	Args:  cobra.NoArgs,
}

func init() {
	serverCommand.PersistentFlags().
		StringVarP(&config, "config", "c",
			"/etc/FNode/config.json", "config file path")
	serverCommand.PersistentFlags().
		BoolVarP(&watch, "watch", "w",
			true, "watch file path change")
	command.AddCommand(&serverCommand)
}

func serverHandle(_ *cobra.Command, _ []string) {
	showVersion()
	c := conf.New()
	err := c.LoadFromPath(config)
	if err != nil {
		log.WithField("err", err).Error("Load config file failed")
		return
	}
	// info is the default: start-up, panel connection and reload messages
	// are visible without the per-request noise of debug
	switch strings.ToLower(strings.TrimSpace(c.LogConfig.Level)) {
	case "debug", "trace":
		log.SetLevel(log.DebugLevel)
	case "warn", "warning":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}
	if c.LogConfig.Output != "" {
		f, err := os.OpenFile(c.LogConfig.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.WithField("err", err).Error("Open log file failed, using stdout instead")
		} else {
			log.SetOutput(f)
		}
	}
	// Ship FNode and sing-box logs to the panel when its CustomNode plugin asks
	// for them (the console keeps the configured level)
	logship.InstallLogrus(logship.Default)
	// Start automatic log cleaner every 7 days
	stopCleaner := startAutoLogCleaner(7*24*time.Hour, c.LogConfig.Output)
	defer stopCleaner()

	limiter.Init()
	log.Info("Start FNode...")
	c.SetGeneratedDirs(config)
	vc, err := vCore.NewCore(c.CoresConfig)
	if err != nil {
		log.WithField("err", err).Error("new core failed")
		return
	}
	err = vc.Start()
	if err != nil {
		log.WithField("err", err).Error("Start core failed")
		return
	}
	defer vc.Close()
	log.Info("Core ", vc.Type(), " started")
	nodes := node.New()
	err = nodes.Start(c.NodeConfig, vc)
	if err != nil {
		log.WithField("err", err).Error("Run nodes failed")
		return
	}
	log.Info("Nodes started")

	// reload restarts the core and the nodes. It runs for a config change and,
	// with Cores[].NetworkRecovery "restart", when resets do not bring the
	// network back; the lock keeps the two apart.
	var reloadMu sync.Mutex
	var reload func()
	hookRestart := func(core vCore.Core) {
		if r, ok := core.(vCore.NetworkRecoverer); ok {
			// run in a goroutine: reload closes the core that called the hook
			r.SetRestartHook(func() { go reload() })
		}
	}
	reload = func() {
		reloadMu.Lock()
		defer reloadMu.Unlock()
		nodes.Close()
		if cerr := vc.Close(); cerr != nil {
			log.WithField("err", cerr).Error("Restart node failed")
			return
		}
		c.SetGeneratedDirs(config)
		newCore, nerr := vCore.NewCore(c.CoresConfig)
		if nerr != nil {
			log.WithField("err", nerr).Error("New core failed")
			return
		}
		vc = newCore
		if serr := vc.Start(); serr != nil {
			log.WithField("err", serr).Error("Start core failed")
			return
		}
		hookRestart(vc)
		log.Info("Core ", vc.Type(), " restarted")
		if nserr := nodes.Start(c.NodeConfig, vc); nserr != nil {
			log.WithField("err", nserr).Error("Run nodes failed")
			return
		}
		log.Info("Nodes restarted")
		runtime.GC()
	}
	hookRestart(vc)
	if watch {
		err = c.Watch(config, reload)
		if err != nil {
			log.WithField("err", err).Error("start watch failed")
			return
		}
	}
	// clear memory
	runtime.GC()
	// wait exit signal
	{
		osSignals := make(chan os.Signal, 1)
		signal.Notify(osSignals, syscall.SIGINT, syscall.SIGTERM)
		<-osSignals
	}
}
