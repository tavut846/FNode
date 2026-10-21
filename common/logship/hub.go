package logship

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SkipField marks a logrus entry that must not be shipped (the shipper's own
// diagnostics, and lines already shipped through another path).
const SkipField = "logship"

type nodeRoute struct {
	shipper *Shipper
	nodeID  int
}

// Hub routes entries from the global log sources to shippers. An entry that
// names a known node tag goes to that node's shipper; anything else (global
// FNode messages) goes to every shipper.
type Hub struct {
	mu       sync.RWMutex
	shippers map[*Shipper]struct{}
	byTag    map[string]nodeRoute
	floorFns map[int]func(Level)
	nextFn   int
	floor    Level
	// pending holds outbound requests for node tags not mapped yet (the core
	// applies routes before the controller maps its tag).
	pending map[string][]string
}

// Default is the process-wide hub used by the logrus hook and the sing-box
// log capture.
var Default = NewHub()

func NewHub() *Hub {
	return &Hub{
		shippers: make(map[*Shipper]struct{}),
		byTag:    make(map[string]nodeRoute),
		floorFns: make(map[int]func(Level)),
		floor:    Off,
		pending:  make(map[string][]string),
	}
}

// Register adds a shipper; its mode changes feed the hub's floor.
func (h *Hub) Register(s *Shipper) {
	h.mu.Lock()
	h.shippers[s] = struct{}{}
	h.mu.Unlock()
	s.OnModeChange(h.recomputeFloor)
	h.recomputeFloor()
}

// Unregister removes a shipper and every tag mapped to it.
func (h *Hub) Unregister(s *Shipper) {
	h.mu.Lock()
	delete(h.shippers, s)
	for tag, r := range h.byTag {
		if r.shipper == s {
			delete(h.byTag, tag)
		}
	}
	h.mu.Unlock()
	h.recomputeFloor()
}

// MapNode makes entries for the node tag go to s with the given node id.
func (h *Hub) MapNode(tag string, nodeID int, s *Shipper) {
	if tag == "" {
		return
	}
	h.mu.Lock()
	h.byTag[tag] = nodeRoute{shipper: s, nodeID: nodeID}
	tags, ok := h.pending[tag]
	delete(h.pending, tag)
	h.mu.Unlock()
	if ok {
		s.SetMissing(nodeID, tags)
	}
}

// ReportMissing records the outbound tags a node's routes need but FNode does
// not have (empty clears it); its shipper asks the panel for them.
func (h *Hub) ReportMissing(nodeTag string, tags []string) {
	if nodeTag == "" {
		return
	}
	h.mu.Lock()
	r, ok := h.byTag[nodeTag]
	if !ok {
		if len(tags) == 0 {
			delete(h.pending, nodeTag)
		} else {
			h.pending[nodeTag] = append([]string(nil), tags...)
		}
	}
	h.mu.Unlock()
	if ok {
		r.shipper.SetMissing(r.nodeID, tags)
	}
}

// UnmapNode forgets a node tag (and its outbound request).
func (h *Hub) UnmapNode(tag string) {
	h.mu.Lock()
	r, ok := h.byTag[tag]
	delete(h.byTag, tag)
	delete(h.pending, tag)
	h.mu.Unlock()
	if ok {
		r.shipper.SetMissing(r.nodeID, nil)
	}
}

// Floor is the most verbose level any shipper wants; Off when none does.
func (h *Hub) Floor() Level {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.floor
}

// OnFloorChange registers fn, which is called now and whenever the floor
// changes. The returned function unregisters it. Sources use the floor to
// raise their own log level so that the entries the panel wants exist.
func (h *Hub) OnFloorChange(fn func(Level)) (remove func()) {
	h.mu.Lock()
	id := h.nextFn
	h.nextFn++
	h.floorFns[id] = fn
	floor := h.floor
	h.mu.Unlock()
	fn(floor)
	return func() {
		h.mu.Lock()
		delete(h.floorFns, id)
		h.mu.Unlock()
	}
}

func (h *Hub) recomputeFloor() {
	h.mu.Lock()
	floor := Off
	for s := range h.shippers {
		if l := s.MaxLevel(); l > floor {
			floor = l
		}
	}
	changed := floor != h.floor
	h.floor = floor
	fns := make([]func(Level), 0, len(h.floorFns))
	if changed {
		for _, fn := range h.floorFns {
			fns = append(fns, fn)
		}
	}
	h.mu.Unlock()
	for _, fn := range fns {
		fn(floor)
	}
}

// Emit queues one log entry. nodeTag is the controller tag when the source
// knows it ("" otherwise); tag is the entry's own tag (logger name).
func (h *Hub) Emit(level Level, nodeTag, tag, message string, at time.Time) {
	if level > h.Floor() {
		return
	}
	e := Entry{Time: float64(at.UnixNano()) / 1e9, Tag: tag, Message: message}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if r, ok := h.byTag[nodeTag]; ok && nodeTag != "" {
		e.NodeID = r.nodeID
		r.shipper.Add(level, e)
		return
	}
	for s := range h.shippers {
		s.Add(level, e)
	}
}

// ---- logrus integration ----

func levelOfLogrus(l logrus.Level) Level {
	switch {
	case l <= logrus.ErrorLevel:
		return Error
	case l == logrus.WarnLevel:
		return Warn
	case l == logrus.InfoLevel:
		return Info
	default:
		return Debug
	}
}

func logrusLevelOf(l Level) logrus.Level {
	switch l {
	case Error:
		return logrus.ErrorLevel
	case Warn:
		return logrus.WarnLevel
	case Info:
		return logrus.InfoLevel
	default:
		return logrus.DebugLevel
	}
}

type logrusBridge struct {
	hub *Hub
	// consoleLevel is the level the operator configured; the formatter drops
	// console output above it, so raising the logger level for the panel does
	// not make the console noisier.
	mu           sync.RWMutex
	consoleLevel logrus.Level
	inner        logrus.Formatter
}

func (b *logrusBridge) Levels() []logrus.Level { return logrus.AllLevels }

func (b *logrusBridge) Fire(e *logrus.Entry) error {
	if _, skip := e.Data[SkipField]; skip {
		return nil
	}
	level := levelOfLogrus(e.Level)
	if level > b.hub.Floor() {
		return nil
	}
	nodeTag, _ := e.Data["tag"].(string)
	b.hub.Emit(level, nodeTag, nodeTag, e.Message, e.Time)
	return nil
}

func (b *logrusBridge) Format(e *logrus.Entry) ([]byte, error) {
	b.mu.RLock()
	console := b.consoleLevel
	b.mu.RUnlock()
	if e.Level > console {
		return nil, nil
	}
	return b.inner.Format(e)
}

func (b *logrusBridge) apply(floor Level) {
	b.mu.RLock()
	console := b.consoleLevel
	b.mu.RUnlock()
	target := console
	if floor != Off {
		if want := logrusLevelOf(floor); want > target {
			target = want
		}
	}
	if logrus.GetLevel() != target {
		logrus.SetLevel(target)
	}
}

// InstallLogrus ships FNode's own logs: it adds a hook, wraps the formatter so
// the console keeps the configured level, and raises the logger level while a
// panel wants more verbose entries. Call it after the log level and output are
// configured. The returned function undoes it.
func InstallLogrus(hub *Hub) (uninstall func()) {
	b := &logrusBridge{hub: hub, consoleLevel: logrus.GetLevel(), inner: logrus.StandardLogger().Formatter}
	logrus.AddHook(b)
	logrus.SetFormatter(b)
	remove := hub.OnFloorChange(b.apply)
	return func() {
		remove()
		logrus.SetFormatter(b.inner)
		hooks := make(logrus.LevelHooks)
		for level, list := range logrus.StandardLogger().Hooks {
			for _, h := range list {
				if h != logrus.Hook(b) {
					hooks[level] = append(hooks[level], h)
				}
			}
		}
		logrus.StandardLogger().ReplaceHooks(hooks)
		b.mu.RLock()
		console := b.consoleLevel
		b.mu.RUnlock()
		logrus.SetLevel(console)
	}
}
