package sing

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/tavut846/FNode/common/format"
	"github.com/tavut846/FNode/common/rate"

	"github.com/tavut846/FNode/limiter"

	"github.com/tavut846/FNode/common/counter"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.ConnectionTracker = (*HookServer)(nil)

type HookServer struct {
	counter sync.Map //map[string]*counter.TrafficCounter
	// accepted counts the connections that reached the router per inbound tag
	// (map[string]*atomic.Int64); a Reality probe that fails the handshake never
	// gets here.
	accepted sync.Map
}

func (h *HookServer) markAccepted(tag string) {
	c, ok := h.accepted.Load(tag)
	if !ok {
		c, _ = h.accepted.LoadOrStore(tag, new(atomic.Int64))
	}
	c.(*atomic.Int64).Add(1)
}

// Accepted returns how many connections inbound tag has handed to the router.
func (h *HookServer) Accepted(tag string) int64 {
	if c, ok := h.accepted.Load(tag); ok {
		return c.(*atomic.Int64).Load()
	}
	return 0
}

func (h *HookServer) ModeList() []string {
	return nil
}

func (h *HookServer) RoutedConnection(_ context.Context, conn net.Conn, m adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) net.Conn {
	h.markAccepted(m.Inbound)
	l, err := limiter.GetLimiter(m.Inbound)
	if err != nil {
		log.Warn("get limiter for ", m.Inbound, " error: ", err)
		return conn
	}
	taguuid := format.ExtractTagUUID(m.Inbound, m.User)
	userUUID := format.ExtractUUID(m.User)
	ip := m.Source.Addr.String()
	if b, r := l.CheckLimit(taguuid, ip, true, true); r {
		conn.Close()
		log.Error("[", m.Inbound, "] ", "Limited ", m.User, " by ip or conn")
		return conn
	} else if b != nil {
		conn = rate.NewConnRateLimiter(conn, b)
	}
	if l != nil {
		destStr := m.Destination.AddrString()
		protocol := m.Protocol
		if l.CheckDomainRule(destStr) {
			log.Error(fmt.Sprintf(
				"User %s access domain %s reject by rule",
				m.User,
				destStr))
			conn.Close()
			return conn
		}
		if len(protocol) != 0 {
			if l.CheckProtocolRule(protocol) {
				log.Error(fmt.Sprintf(
					"User %s access protocol %s reject by rule",
					m.User,
					protocol))
				conn.Close()
				return conn
			}
		}
	}
	var t *counter.TrafficCounter
	if c, ok := h.counter.Load(m.Inbound); !ok {
		t = counter.NewTrafficCounter()
		h.counter.Store(m.Inbound, t)
	} else {
		t = c.(*counter.TrafficCounter)
	}
	conn = counter.NewConnCounter(conn, t.GetCounter(userUUID))
	return conn
}

func (h *HookServer) RoutedPacketConnection(_ context.Context, conn N.PacketConn, m adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) N.PacketConn {
	h.markAccepted(m.Inbound)
	l, err := limiter.GetLimiter(m.Inbound)
	if err != nil {
		log.Warn("get limiter for ", m.Inbound, " error: ", err)
		return conn
	}
	ip := m.Source.Addr.String()
	taguuid := format.ExtractTagUUID(m.Inbound, m.User)
	userUUID := format.ExtractUUID(m.User)
	if b, r := l.CheckLimit(taguuid, ip, false, false); r {
		conn.Close()
		log.Error("[", m.Inbound, "] ", "Limited ", m.User, " by ip or conn")
		return conn
	} else if b != nil {
		//conn = rate.NewPacketConnCounter(conn, b)
	}
	if l != nil {
		destStr := m.Destination.AddrString()
		protocol := m.Destination.Network()
		if l.CheckDomainRule(destStr) {
			log.Error(fmt.Sprintf(
				"User %s access domain %s reject by rule",
				m.User,
				destStr))
			conn.Close()
			return conn
		}
		if len(protocol) != 0 {
			if l.CheckProtocolRule(protocol) {
				log.Error(fmt.Sprintf(
					"User %s access protocol %s reject by rule",
					m.User,
					protocol))
				conn.Close()
				return conn
			}
		}
	}
	var t *counter.TrafficCounter
	if c, ok := h.counter.Load(m.Inbound); !ok {
		t = counter.NewTrafficCounter()
		h.counter.Store(m.Inbound, t)
	} else {
		t = c.(*counter.TrafficCounter)
	}
	conn = counter.NewPacketConnCounter(conn, t.GetCounter(userUUID))
	return conn
}
