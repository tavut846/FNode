package inbound

import (
	"context"
	"os"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ adapter.InboundManager = (*Manager)(nil)

type Manager struct {
	registry     adapter.InboundRegistry
	endpoint     adapter.EndpointManager
	access       sync.Mutex
	inbounds     []adapter.Inbound
	inboundByTag map[string]adapter.Inbound
	// started is true once the box finished starting; inbounds created after
	// that (FNode adds nodes at runtime) are started here, each in its own
	// scope so Remove can close just that inbound.
	started bool
	scopes  map[adapter.Inbound]*adapter.Scope
}

func NewManager(registry adapter.InboundRegistry, endpoint adapter.EndpointManager) *Manager {
	return &Manager{
		registry:     registry,
		endpoint:     endpoint,
		inboundByTag: make(map[string]adapter.Inbound),
		scopes:       make(map[adapter.Inbound]*adapter.Scope),
	}
}

func (m *Manager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	m.access.Lock()
	inbounds := m.inbounds
	m.access.Unlock()
	if stage == adapter.StartStateInitialize {
		// runtime-created inbounds have their own scopes: close them with the box
		scope.Add(m.closeRuntime)
	}
	for _, inbound := range inbounds {
		name := "inbound/" + inbound.Type() + "[" + inbound.Tag() + "]"
		err := scope.Start(name, inbound, stage)
		if err != nil {
			return err
		}
	}
	if stage == adapter.StartStateStarted {
		m.access.Lock()
		m.started = true
		m.access.Unlock()
	}
	return nil
}

func (m *Manager) closeRuntime() error {
	m.access.Lock()
	m.started = false
	scopes := m.scopes
	m.scopes = make(map[adapter.Inbound]*adapter.Scope)
	m.access.Unlock()
	var err error
	for _, scope := range scopes {
		err = E.Append(err, scope.Close(), func(err error) error { return err })
	}
	return err
}

func (m *Manager) Inbounds() []adapter.Inbound {
	m.access.Lock()
	defer m.access.Unlock()
	return m.inbounds
}

func (m *Manager) Get(tag string) (adapter.Inbound, bool) {
	m.access.Lock()
	inbound, found := m.inboundByTag[tag]
	m.access.Unlock()
	if found {
		return inbound, true
	}
	return m.endpoint.Get(tag)
}

// Create adds an inbound. After the box has started the inbound is started at
// once. An existing inbound with the same tag is replaced (it is closed first).
func (m *Manager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, outboundType string, options any) error {
	inbound, err := m.registry.Create(ctx, router, logger, tag, outboundType, options)
	if err != nil {
		return err
	}
	m.access.Lock()
	started := m.started
	existing, loaded := m.inboundByTag[tag]
	m.access.Unlock()
	var scope *adapter.Scope
	if loaded {
		// replace: the old one still holds the port, so it stops before the new one binds
		if err = m.Remove(tag); err != nil {
			return E.Cause(err, "close inbound/", existing.Type(), "[", tag, "]")
		}
	}
	if started {
		scope = adapter.NewScope(ctx, logger)
		name := "inbound/" + inbound.Type() + "[" + tag + "]"
		for _, stage := range adapter.ListStartStages {
			if err = scope.Start(name, inbound, stage); err != nil {
				scope.Close()
				return err
			}
		}
	}
	m.access.Lock()
	defer m.access.Unlock()
	if _, loaded = m.inboundByTag[tag]; loaded {
		if scope != nil {
			scope.Close()
		}
		return E.New("duplicate inbound tag: ", tag)
	}
	m.inbounds = append(m.inbounds, inbound)
	m.inboundByTag[tag] = inbound
	if scope != nil {
		m.scopes[inbound] = scope
	}
	return nil
}

// Remove stops and forgets a runtime-created inbound.
func (m *Manager) Remove(tag string) error {
	m.access.Lock()
	inbound, found := m.inboundByTag[tag]
	if !found {
		m.access.Unlock()
		return os.ErrInvalid
	}
	delete(m.inboundByTag, tag)
	index := common.Index(m.inbounds, func(it adapter.Inbound) bool {
		return it == inbound
	})
	if index != -1 {
		m.inbounds = append(m.inbounds[:index:index], m.inbounds[index+1:]...)
	}
	scope := m.scopes[inbound]
	delete(m.scopes, inbound)
	m.access.Unlock()
	if scope != nil {
		return scope.Close()
	}
	// loaded from the box configuration: its lifetime is the box's, nothing to close here
	return nil
}
