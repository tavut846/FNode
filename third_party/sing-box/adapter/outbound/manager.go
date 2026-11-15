package outbound

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ adapter.OutboundManager = (*Manager)(nil)

type Manager struct {
	registry                adapter.OutboundRegistry
	endpoint                adapter.EndpointManager
	defaultTag              string
	access                  sync.RWMutex
	outbounds               []adapter.Outbound
	outboundByTag           map[string]adapter.Outbound
	defaultOutbound         adapter.Outbound
	defaultOutboundFallback func() (adapter.Outbound, error)
	// started is true once the box finished starting; outbounds created after
	// that (FNode applies panel outbounds at runtime) are started here, each in
	// its own scope so Remove can close just that outbound.
	started bool
	scopes  map[adapter.Outbound]*adapter.Scope
}

func NewManager(registry adapter.OutboundRegistry, endpoint adapter.EndpointManager, defaultTag string) *Manager {
	return &Manager{
		registry:      registry,
		endpoint:      endpoint,
		defaultTag:    defaultTag,
		outboundByTag: make(map[string]adapter.Outbound),
		scopes:        make(map[adapter.Outbound]*adapter.Scope),
	}
}

func (m *Manager) Initialize(defaultOutboundFallback func() (adapter.Outbound, error)) {
	m.defaultOutboundFallback = defaultOutboundFallback
}

func (m *Manager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(m.closeRuntime)
	}
	if stage == adapter.StartStateStarted {
		defer func() {
			m.access.Lock()
			m.started = true
			m.access.Unlock()
		}()
	}
	m.access.Lock()
	if stage == adapter.StartStateInitialize {
		if m.defaultTag != "" && m.defaultOutbound == nil {
			defaultEndpoint, loaded := m.endpoint.Get(m.defaultTag)
			if !loaded {
				m.access.Unlock()
				return E.New("default outbound not found: ", m.defaultTag)
			}
			m.defaultOutbound = defaultEndpoint
		}
		if m.defaultOutbound == nil {
			directOutbound, err := m.defaultOutboundFallback()
			if err != nil {
				m.access.Unlock()
				return E.Cause(err, "create direct outbound for fallback")
			}
			m.outbounds = append(m.outbounds, directOutbound)
			m.outboundByTag[directOutbound.Tag()] = directOutbound
			m.defaultOutbound = directOutbound
		}
	}
	outbounds := m.outbounds
	m.access.Unlock()
	if stage == adapter.StartStateStart {
		return m.startOutbounds(scope, append(outbounds, common.Map(m.endpoint.Endpoints(), func(it adapter.Endpoint) adapter.Outbound { return it })...))
	}
	for _, outbound := range outbounds {
		lifecycle, isLifecycle := outbound.(adapter.Lifecycle)
		if !isLifecycle {
			continue
		}
		name := "outbound/" + outbound.Type() + "[" + outbound.Tag() + "]"
		err := scope.Start(name, lifecycle, stage)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) closeRuntime() error {
	m.access.Lock()
	m.started = false
	scopes := m.scopes
	m.scopes = make(map[adapter.Outbound]*adapter.Scope)
	m.access.Unlock()
	var err error
	for _, scope := range scopes {
		err = E.Append(err, scope.Close(), func(err error) error { return err })
	}
	return err
}

func (m *Manager) startOutbounds(scope *adapter.Scope, outbounds []adapter.Outbound) error {
	started := make(map[string]bool)
	for {
		canContinue := false
	startOne:
		for _, outboundToStart := range outbounds {
			outboundTag := outboundToStart.Tag()
			if started[outboundTag] {
				continue
			}
			dependencies := outboundToStart.Dependencies()
			for _, dependency := range dependencies {
				if !started[dependency] {
					continue startOne
				}
			}
			started[outboundTag] = true
			canContinue = true
			if endpoint, isEndpoint := outboundToStart.(adapter.Endpoint); isEndpoint {
				err := m.endpoint.StartEndpoint(endpoint)
				if err != nil {
					return err
				}
				continue
			}
			lifecycle, isLifecycle := outboundToStart.(adapter.Lifecycle)
			if !isLifecycle {
				continue
			}
			name := "outbound/" + outboundToStart.Type() + "[" + outboundTag + "]"
			err := scope.Start(name, lifecycle, adapter.StartStateStart)
			if err != nil {
				return err
			}
		}
		if len(started) == len(outbounds) {
			break
		}
		if canContinue {
			continue
		}
		currentOutbound := common.Find(outbounds, func(it adapter.Outbound) bool {
			return !started[it.Tag()]
		})
		var lintOutbound func(oTree []string, oCurrent adapter.Outbound) error
		lintOutbound = func(oTree []string, oCurrent adapter.Outbound) error {
			problemOutboundTag := common.Find(oCurrent.Dependencies(), func(it string) bool {
				return !started[it]
			})
			if common.Contains(oTree, problemOutboundTag) {
				return E.New("circular outbound dependency: ", strings.Join(oTree, " -> "), " -> ", problemOutboundTag)
			}
			problemOutbound := common.Find(outbounds, func(it adapter.Outbound) bool {
				return it.Tag() == problemOutboundTag
			})
			if problemOutbound == nil {
				return E.New("dependency[", problemOutboundTag, "] not found for outbound[", oCurrent.Tag(), "]")
			}
			return lintOutbound(append(oTree, problemOutboundTag), problemOutbound)
		}
		return lintOutbound([]string{currentOutbound.Tag()}, currentOutbound)
	}
	return nil
}

func (m *Manager) Outbounds() []adapter.Outbound {
	m.access.RLock()
	defer m.access.RUnlock()
	return m.outbounds
}

func (m *Manager) Outbound(tag string) (adapter.Outbound, bool) {
	m.access.RLock()
	outbound, found := m.outboundByTag[tag]
	m.access.RUnlock()
	if found {
		return outbound, true
	}
	return m.endpoint.Get(tag)
}

func (m *Manager) Default() adapter.Outbound {
	m.access.RLock()
	defer m.access.RUnlock()
	return m.defaultOutbound
}

// Create adds an outbound. After the box has started the outbound is started at
// once, and an existing outbound with the same tag is replaced.
func (m *Manager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, inboundType string, options any) error {
	if tag == "" {
		return os.ErrInvalid
	}
	outbound, err := m.registry.CreateOutbound(ctx, router, logger, tag, inboundType, options)
	if err != nil {
		return err
	}
	m.access.Lock()
	started := m.started
	m.access.Unlock()
	var scope *adapter.Scope
	if started {
		if lifecycle, isLifecycle := outbound.(adapter.Lifecycle); isLifecycle {
			scope = adapter.NewScope(ctx, logger)
			name := "outbound/" + outbound.Type() + "[" + tag + "]"
			for _, stage := range adapter.ListStartStages {
				if err = scope.Start(name, lifecycle, stage); err != nil {
					scope.Close()
					return err
				}
			}
		}
	}
	m.access.Lock()
	if existing, exists := m.outboundByTag[tag]; exists {
		m.removeLocked(existing)
		oldScope := m.scopes[existing]
		delete(m.scopes, existing)
		m.access.Unlock()
		if oldScope != nil {
			err = oldScope.Close()
		} else if started {
			err = common.Close(existing)
		}
		m.access.Lock()
		if err != nil {
			logger.Warn(E.Cause(err, "close replaced outbound[", tag, "]"))
		}
	}
	m.outbounds = append(m.outbounds, outbound)
	m.outboundByTag[tag] = outbound
	if scope != nil {
		m.scopes[outbound] = scope
	}
	if tag == m.defaultTag || (m.defaultTag == "" && m.defaultOutbound == nil) {
		m.defaultOutbound = outbound
	}
	m.access.Unlock()
	return nil
}

func (m *Manager) removeLocked(outbound adapter.Outbound) {
	delete(m.outboundByTag, outbound.Tag())
	index := common.Index(m.outbounds, func(it adapter.Outbound) bool {
		return it == outbound
	})
	if index != -1 {
		m.outbounds = append(m.outbounds[:index:index], m.outbounds[index+1:]...)
	}
	if m.defaultOutbound == outbound {
		if len(m.outbounds) > 0 {
			m.defaultOutbound = m.outbounds[0]
		} else {
			m.defaultOutbound = nil
		}
	}
}

// Remove stops and forgets an outbound; it refuses while another outbound
// depends on it.
func (m *Manager) Remove(tag string) error {
	m.access.Lock()
	outbound, found := m.outboundByTag[tag]
	if !found {
		m.access.Unlock()
		return os.ErrInvalid
	}
	var dependBy []string
	for _, other := range m.outbounds {
		if other != outbound && common.Contains(other.Dependencies(), tag) {
			dependBy = append(dependBy, other.Tag())
		}
	}
	if len(dependBy) > 0 {
		m.access.Unlock()
		return E.New("outbound[", tag, "] is depended by ", strings.Join(dependBy, ", "))
	}
	m.removeLocked(outbound)
	scope := m.scopes[outbound]
	delete(m.scopes, outbound)
	m.access.Unlock()
	if scope != nil {
		return scope.Close()
	}
	return common.Close(outbound)
}
