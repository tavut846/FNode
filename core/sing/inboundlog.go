package sing

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing-box/option"
	logrus "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

// idleWarnAfter is how long an inbound may run without a single accepted
// connection before FNode says so once.
const idleWarnAfter = 10 * time.Minute

// inboundTLSAndMux returns the TLS and multiplex options of the inbound types
// that carry them.
func inboundTLSAndMux(opts any) (*option.InboundTLSOptions, *option.InboundMultiplexOptions) {
	switch o := opts.(type) {
	case *option.VLESSInboundOptions:
		return o.TLS, o.Multiplex
	case *option.VMessInboundOptions:
		return o.TLS, o.Multiplex
	case *option.TrojanInboundOptions:
		return o.TLS, o.Multiplex
	case *option.ShadowsocksInboundOptions:
		return nil, o.Multiplex
	}
	return nil, nil
}

// inboundFields describes an inbound for the start-up info line: node id, tag,
// type, listen address, and for Reality the server name, handshake dest, how
// many short ids, the flow and whether multiplex is on. Keys, short ids and
// users never appear.
func inboundFields(tag string, info *panel.NodeInfo, c *conf.Options, in option.Inbound) logrus.Fields {
	f := logrus.Fields{"tag": tag, "type": in.Type}
	if info != nil {
		f["node_id"] = info.Id
		if info.Common != nil {
			f["listen"] = net.JoinHostPort(c.ListenIP, strconv.Itoa(info.Common.ServerPort))
		}
	}
	tls, mux := inboundTLSAndMux(in.Options)
	f["multiplex"] = mux != nil && mux.Enabled
	if tls != nil && tls.Reality != nil && tls.Reality.Enabled {
		f["reality"] = true
		f["server_name"] = tls.ServerName
		f["dest"] = net.JoinHostPort(tls.Reality.Handshake.Server, strconv.Itoa(int(tls.Reality.Handshake.ServerPort)))
		f["short_ids"] = len(tls.Reality.ShortID)
	} else if tls != nil && tls.Enabled {
		f["tls"] = true
	}
	if info != nil && info.VAllss != nil && info.Type == "vless" {
		flow := info.VAllss.Flow
		if flow == "" {
			flow = "none"
		}
		f["flow"] = flow
	}
	return f
}

// logInboundError logs an inbound that could not be built or started, with the
// node and what usually causes it.
func logInboundError(tag string, info *panel.NodeInfo, c *conf.Options, what string, err error) {
	f := logrus.Fields{"tag": tag, "err": err.Error()}
	if info != nil {
		f["node_id"] = info.Id
		f["type"] = info.Type
		if info.Common != nil {
			f["port"] = info.Common.ServerPort
		}
	}
	msg := fmt.Sprintf("Inbound %s: %v", what, err)
	switch low := strings.ToLower(err.Error()); {
	case strings.Contains(low, "address already in use") || strings.Contains(low, "only one usage of each socket"):
		msg += " (the port is already used by another program or node; check `ss -ltnp`)"
	case strings.Contains(low, "permission denied") || strings.Contains(low, "access permissions"):
		msg += " (the process may not bind this port; ports below 1024 need root or CAP_NET_BIND_SERVICE)"
	}
	logrus.WithFields(f).Error(msg)
}

// armIdleWarning starts the "nobody has connected" watch of an inbound.
func (b *Sing) armIdleWarning(tag, typ string, port int) {
	after := b.idleWarnAfter
	if after <= 0 {
		after = idleWarnAfter
	}
	b.idleMu.Lock()
	defer b.idleMu.Unlock()
	if b.idleTimers == nil {
		b.idleTimers = make(map[string]*time.Timer)
	}
	if old := b.idleTimers[tag]; old != nil {
		old.Stop()
	}
	b.idleTimers[tag] = time.AfterFunc(after, func() { b.warnIfIdle(tag, typ, port, after) })
}

func (b *Sing) disarmIdleWarning(tag string) {
	b.idleMu.Lock()
	defer b.idleMu.Unlock()
	if t := b.idleTimers[tag]; t != nil {
		t.Stop()
		delete(b.idleTimers, tag)
	}
}

func (b *Sing) disarmAllIdleWarnings() {
	b.idleMu.Lock()
	defer b.idleMu.Unlock()
	for tag, t := range b.idleTimers {
		t.Stop()
		delete(b.idleTimers, tag)
	}
}

func (b *Sing) warnIfIdle(tag, typ string, port int, after time.Duration) {
	b.users.mapLock.RLock()
	_, present := b.inboundOptions[tag]
	users := len(b.inboundUsers[tag])
	b.users.mapLock.RUnlock()
	if !present || b.hookServer == nil || b.hookServer.Accepted(tag) > 0 {
		return
	}
	window := after.Round(time.Minute)
	if after < time.Minute {
		window = after.Round(time.Millisecond)
	}
	msg := fmt.Sprintf("No client has connected to %s:%d in %s - check the firewall / cloud security list for this port, the DNS name of the node host, and the client's config", typ, port, window)
	if users == 0 {
		msg += " (this node has 0 users)"
	}
	logrus.WithFields(logrus.Fields{"tag": tag, "type": typ, "port": port, "users": users}).Warn(msg)
}
