package sing

import (
	"github.com/sagernet/sing-box/option"
	logrus "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

// resolveMultiplex builds the multiplex options of a vless, vmess, trojan or
// shadowsocks inbound. The panel decides when it sends an "enabled" value
// (Xboard's multiplex form: the subscription gives clients the same block, so
// a client that turns multiplex on talks smux to the server); the local
// config.json (SingOptions.Multiplex) is the fallback when the panel sends
// none. Padding and Brutal come from the same side as "enabled".
//
// One info line per node build says whether multiplex is on and where that
// came from. Vision (xtls-rprx-vision) is not special-cased: the server wraps
// the inbound the same way for every flow.
//
// Brutal needs an up and a down rate of at least 1 Mbps, or sing-box refuses
// to create the inbound; a node with Brutal and no rates runs without Brutal.
func resolveMultiplex(tag string, info *panel.NodeInfo, local *conf.MultiplexConfig) *option.InboundMultiplexOptions {
	var out *option.InboundMultiplexOptions
	source := "local config"
	if info != nil && info.Common != nil && info.Common.Multiplex.Specified() {
		source = "panel"
		pm := info.Common.Multiplex
		if !*pm.Enabled {
			logrus.WithFields(logrus.Fields{"tag": tag, "source": source}).Info("Multiplex is off")
			return nil
		}
		out = &option.InboundMultiplexOptions{Enabled: true, Padding: pm.Padding}
		if pm.Brutal.Enabled {
			if pm.Brutal.UpMbps >= 1 && pm.Brutal.DownMbps >= 1 {
				out.Brutal = &option.BrutalOptions{Enabled: true, UpMbps: pm.Brutal.UpMbps, DownMbps: pm.Brutal.DownMbps}
			} else {
				logrus.WithFields(logrus.Fields{"tag": tag, "up_mbps": pm.Brutal.UpMbps, "down_mbps": pm.Brutal.DownMbps}).
					Warn("Multiplex Brutal needs up_mbps and down_mbps of at least 1 in the dashboard; running multiplex without Brutal")
			}
		}
	} else if local == nil {
		source = "not configured"
	} else {
		out = &option.InboundMultiplexOptions{
			Enabled: local.Enabled,
			Padding: local.Padding,
			Brutal: &option.BrutalOptions{
				Enabled:  local.Brutal.Enabled,
				UpMbps:   local.Brutal.UpMbps,
				DownMbps: local.Brutal.DownMbps,
			},
		}
	}
	if out == nil || !out.Enabled {
		logrus.WithFields(logrus.Fields{"tag": tag, "source": source}).Info("Multiplex is off")
		return out
	}
	brutal := out.Brutal != nil && out.Brutal.Enabled
	logrus.WithFields(logrus.Fields{"tag": tag, "source": source, "padding": out.Padding, "brutal": brutal}).Info("Multiplex is on")
	if brutal {
		logrus.WithFields(logrus.Fields{"tag": tag, "source": source}).Warn("Multiplex Brutal is on: TCP Brutal needs the brutal kernel module on this server (lsmod | grep brutal)")
	}
	return out
}
