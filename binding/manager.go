// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package binding

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// Entry is one Binding entry: the Binding cluster's TargetStruct as the
// server keeps it.
type Entry = mattercore.TargetStruct

// Kind is what a binding entry resolved to — matter.js BindingResolution's
// `kind` (BindingManager.ts:25-28).
type Kind uint8

// Kinds.
const (
	// KindClient: another node, reached over a CASE session.
	KindClient Kind = iota + 1
	// KindGroup: a group, reached by group-addressed messages.
	KindGroup
	// KindServer: another endpoint of this node (a self-binding).
	KindServer
)

// String is matter.js's kind name.
func (k Kind) String() string {
	switch k {
	case KindClient:
		return "client"
	case KindGroup:
		return "group"
	case KindServer:
		return "server"
	default:
		return "unknown"
	}
}

// Resolution is a binding entry resolved into the peer it directs —
// matter.js BindingResolution (BindingManager.ts:25-28). Where matter.js
// hands over a materialised endpoint carrying the declared client
// behaviors, a Resolution names the target endpoint and the clients.
type Resolution struct {
	Kind Kind
	// Entry is the binding entry.
	Entry Entry
	// SourceEndpoint is the endpoint whose Binding server holds the entry.
	SourceEndpoint uint16
	// Peer is the bound node's peer (KindClient).
	Peer *peer.Peer
	// Interaction is the interaction with Peer, connecting when it has no
	// session (KindClient).
	Interaction *interaction.Interaction
	// Endpoint is the target endpoint: the bound node's (KindClient), ours
	// (KindServer), or — as matter.js materialises a group's endpoint under
	// the source endpoint's number (BindingManager.ts:471) — the source
	// endpoint (KindGroup).
	Endpoint uint16
	// Group sends to the bound group (KindGroup).
	Group GroupTarget
	// Clients are the source endpoint's declared client clusters the entry
	// directs, bindable ones only — what matter.js installs on the
	// resolution's endpoint (BindingManager.ts:339, :472, :579-583).
	Clients []uint32
}

// Peers is where the manager takes the peer of a node entry from —
// matter.js `node.peers.forAddress` (BindingManager.ts:333);
// client/peer.Set satisfies it.
type Peers interface {
	For(addr handshake.PeerAddress) (*peer.Peer, error)
}

// GroupTarget sends to one group — the IM face of a group peer, matter.js
// ClientGroupInteraction (packages/node/src/node/client/
// ClientGroupInteraction.ts:33-82): invoke only, group-addressed, no
// answer. The group sender of package groups provides it.
type GroupTarget interface {
	Invoke(ctx context.Context, path im.ConcreteCommandPath, fields spec.Encodable) error
}

// GroupSender gives the [GroupTarget] of a fabric's group — matter.js's
// group peer `peers.forAddress({fabricIndex, NodeId.fromGroupId(group)})`
// (BindingManager.ts:458-461).
type GroupSender interface {
	Sender(ctx context.Context, fabricIndex uint8, groupID uint16) (GroupTarget, error)
}

// GroupSenderFunc adapts a function to [GroupSender].
type GroupSenderFunc func(ctx context.Context, fabricIndex uint8, groupID uint16) (GroupTarget, error)

// Sender implements [GroupSender].
func (f GroupSenderFunc) Sender(ctx context.Context, fabricIndex uint8, groupID uint16) (GroupTarget, error) {
	return f(ctx, fabricIndex, groupID)
}

// Topology is what the manager reads of this node: its fabrics, its
// endpoints and their servers, each endpoint's declared client clusters
// and the fabrics' group keys.
type Topology interface {
	// NodeID is our node id on the fabric; false when the fabric is not
	// ours — matter.js `fabrics.has` / `fabrics.for(index).nodeId`
	// (BindingManager.ts:276, :645-650).
	NodeID(fabricIndex uint8) (uint64, bool)
	// HasEndpoint reports whether the endpoint exists (:284).
	HasEndpoint(endpoint uint16) bool
	// HasServer reports whether the endpoint serves the cluster
	// (#endpointHasClusterServer, :550-554).
	HasServer(endpoint uint16, cluster uint32) bool
	// ClientClusters are the endpoint's declared client clusters — its
	// type's clientClusters (:566).
	ClientClusters(endpoint uint16) []uint32
	// HasGroupKey reports whether the fabric maps the group to a key set
	// it holds (#holdsGroupKey, :482-490).
	HasGroupKey(fabricIndex uint8, groupID uint16) bool
}

// Config configures a [Manager].
type Config struct {
	// Topology is this node; required.
	Topology Topology
	// Peers gives the peer of a node entry; nil is a node without its
	// client stack, whose node entries are dropped.
	Peers Peers
	// Groups gives the sender of a group entry; nil drops group entries
	// once their key exists.
	Groups GroupSender
	// Interaction configures the interaction of a client resolution.
	Interaction interaction.Config
	// Logger receives the diagnostics; nil discards them.
	Logger *slog.Logger
}

// ErrNoTopology is [New] without a Topology.
var ErrNoTopology = errors.New("binding: a Manager needs a Topology")

// item is one registered entry — matter.js QueueItem
// (BindingManager.ts:23). Its address is its identity
// (#isRegisteredGroup compares items, :543-548); seq orders a wake.
type item struct {
	source uint16
	entry  Entry
	key    string
	seq    uint64
}

// pendingEntry is a client entry waiting for its peer's session; its
// address identifies it (BindingManager.ts:30, :623-626).
type pendingEntry struct{ cancel func() }

// record is the tracking of one source endpoint — matter.js ServerRecord
// (BindingManager.ts:34-44). Its `groups` map, every registered group
// entry, is the manager's registered: matter.js keeps it at the time of
// the call, not at the time of the resolution.
type record struct {
	pending     map[string]*pendingEntry
	established map[string]Resolution
	// dormant are the entries that resolve once something observes
	// established.
	dormant map[string]*item
}

func (r *record) empty() bool {
	return len(r.pending) == 0 && len(r.established) == 0 && len(r.dormant) == 0
}

// regKey identifies a registered group entry: its source endpoint and its
// entry key.
type regKey struct {
	source uint16
	key    string
}

// handler is one OnEstablished or OnRemoved registration.
type handler struct{ fn func(Resolution) }

// attachment is a Binding server the manager listens to.
type attachment struct {
	b    *mattercore.Binding
	stop func()
}

// Manager resolves the entries of the node's Binding servers — matter.js
// BindingManager (BindingManager.ts:46-710). Every change of its state
// runs on one worker goroutine, in the order it was asked for, as
// matter.js's single event loop orders it; the OnEstablished and
// OnRemoved handlers run there too, so a handler that blocks delays the
// manager's other work — long work belongs in a goroutine of its own.
// It is safe for concurrent use.
type Manager struct {
	cfg  Config
	log  *slog.Logger
	link peerLink

	life   context.Context //nolint:containedctx // the manager's lifetime, cancelled by Close
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// mu guards the queue, the handlers, the attachments, the registered
	// group entries and closed.
	mu          sync.Mutex
	queue       []func()
	wake        chan struct{}
	closing     bool
	closed      bool
	established []*handler
	removed     []*handler
	attached    map[uint16]attachment
	rechecks    map[uint8]bool
	seq         uint64
	// registered is every registered group entry, resolved or waiting for
	// the fabric to hold a key for its group. Mirrors matter.js
	// packages/node/src/behaviors/binding/BindingManager.ts ServerRecord
	// .groups (:39-40): register and unregister change it when they are
	// called (:119-141), disposeServer drops a server's when it is called
	// (:191-195), so a resolution under way on the worker sees an
	// unregister or a disposal that came in meanwhile (#isRegisteredGroup,
	// :528-532).
	registered map[regKey]*item

	// state is the worker's; stateMu lets Established read it.
	stateMu sync.Mutex
	records map[uint16]*record
}

// New returns a manager over cfg and starts its worker; [Manager.Close]
// ends it.
func New(cfg Config) (*Manager, error) {
	if cfg.Topology == nil {
		return nil, ErrNoTopology
	}
	m := &Manager{
		cfg: cfg, log: cfg.Logger, link: realLink{},
		wake: make(chan struct{}, 1), attached: map[uint16]attachment{}, rechecks: map[uint8]bool{},
		registered: map[regKey]*item{}, records: map[uint16]*record{},
	}
	if m.log == nil {
		m.log = slog.New(slog.DiscardHandler)
	}
	if m.cfg.Interaction.Logger == nil {
		m.cfg.Interaction.Logger = m.log
	}
	m.life, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go m.run()
	return m, nil
}

// run is the worker: it runs the queued work in order until Close.
func (m *Manager) run() {
	defer m.wg.Done()
	for {
		m.mu.Lock()
		if len(m.queue) == 0 {
			if m.closed {
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
			<-m.wake
			continue
		}
		fn := m.queue[0]
		m.queue[0] = nil
		m.queue = m.queue[1:]
		m.mu.Unlock()
		fn()
	}
}

// post queues fn for the worker; once Close began it is dropped.
func (m *Manager) post(fn func()) {
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return
	}
	m.queue = append(m.queue, fn)
	m.mu.Unlock()
	m.signal()
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Attach makes the manager resolve the entries of the Binding server on
// endpoint: every entry it holds now (BindingServer.ts:120-126) and every
// entry a later write adds or removes (:127-157). A server already
// attached to the endpoint is detached first ([Manager.Detach]);
// attaching the same server again does nothing.
func (m *Manager) Attach(endpoint uint16, b *mattercore.Binding) {
	if b == nil {
		return
	}
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return
	}
	prev, had := m.attached[endpoint]
	if had && prev.b == b {
		m.mu.Unlock()
		return
	}
	stop := b.OnChange(func(_ uint8, added, removed []mattercore.TargetStruct) {
		// BindingServer registers the added entries, then unregisters
		// the removed ones (:136-148).
		for _, e := range added {
			m.Register(endpoint, e)
		}
		for _, e := range removed {
			m.Unregister(endpoint, e)
		}
	})
	m.attached[endpoint] = attachment{b: b, stop: stop}
	if had {
		m.forgetRegisteredLocked(endpoint)
	}
	m.mu.Unlock()
	if had {
		prev.stop()
		m.post(func() { m.disposeServer(endpoint) })
	}
	for _, e := range b.Entries() {
		m.Register(endpoint, e)
	}
}

// Detach stops resolving the entries of endpoint's Binding server and
// emits removed for each of its established bindings — matter.js
// BindingManager.disposeServer (BindingManager.ts:191-228), which the
// server's disposal calls.
func (m *Manager) Detach(endpoint uint16) {
	m.mu.Lock()
	a, had := m.attached[endpoint]
	delete(m.attached, endpoint)
	m.forgetRegisteredLocked(endpoint)
	m.mu.Unlock()
	if had {
		a.stop()
	}
	m.post(func() { m.disposeServer(endpoint) })
}

// Attached are the endpoints whose Binding server the manager listens
// to, ascending.
func (m *Manager) Attached() []uint16 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]uint16, 0, len(m.attached))
	for ep := range m.attached {
		out = append(out, ep)
	}
	slices.Sort(out)
	return out
}

// Register resolves entry of the Binding server on sourceEndpoint in the
// background — matter.js BindingManager.register (BindingManager.ts:
// 119-129), which records a group entry as registered at once and
// resolves it later. An entry already registered under its key is left
// alone.
func (m *Manager) Register(sourceEndpoint uint16, entry Entry) {
	m.mu.Lock()
	m.seq++
	it := &item{source: sourceEndpoint, entry: entry, key: entry.Key(), seq: m.seq}
	if entry.HasGroup() && !entry.HasNode() && !m.closing {
		k := regKey{source: sourceEndpoint, key: it.key}
		if m.registered[k] == nil {
			m.registered[k] = it
		}
	}
	m.mu.Unlock()
	m.post(func() {
		if m.known(sourceEndpoint, it.key) {
			return
		}
		m.resolveAndEmit(it)
	})
}

// known reports whether an entry of key is tracked: pending, established
// or dormant.
func (m *Manager) known(source uint16, key string) bool {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	r := m.records[source]
	if r == nil {
		return false
	}
	_, p := r.pending[key]
	_, e := r.established[key]
	_, d := r.dormant[key]
	return p || e || d
}

// forgetRegisteredLocked drops the registered group entries of a source
// endpoint. Called with mu held.
func (m *Manager) forgetRegisteredLocked(source uint16) {
	for k := range m.registered {
		if k.source == source {
			delete(m.registered, k)
		}
	}
}

// Unregister forgets entry of the Binding server on sourceEndpoint in the
// background, emitting removed when it was established — matter.js
// BindingManager.unregister (BindingManager.ts:147-157).
func (m *Manager) Unregister(sourceEndpoint uint16, entry Entry) {
	key := entry.Key()
	// The group entry is no longer registered from now on, also for a
	// resolution under way (BindingManager.ts:131-136).
	m.mu.Lock()
	delete(m.registered, regKey{source: sourceEndpoint, key: key})
	m.mu.Unlock()
	m.post(func() {
		if m.clearDormant(sourceEndpoint, key) || m.clearPending(sourceEndpoint, key, nil) {
			return
		}
		m.dropEstablished(sourceEndpoint, key)
	})
}

// OnEstablished registers fn to learn every binding established from now
// on — matter.js BindingServer.events.established (BindingServer.ts:
// 166-197) — and returns the function that removes it. Entries are
// dormant while no handler is registered; the first one wakes them
// (BindingManager.ts:391-438).
func (m *Manager) OnEstablished(fn func(Resolution)) (stop func()) {
	if fn == nil {
		return func() {}
	}
	h := &handler{fn: fn}
	m.mu.Lock()
	first := len(m.established) == 0
	m.established = append(m.established, h)
	m.mu.Unlock()
	if first {
		// The listener attached: the dormant entries resolve
		// (BindingManager.ts:407-416, #wake :427-438).
		m.post(m.wakeAll)
	}
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.established = slices.DeleteFunc(m.established, func(x *handler) bool { return x == h })
	}
}

// OnRemoved registers fn to learn every established binding that is
// removed — matter.js BindingServer.events.removed (BindingServer.ts:
// 199-212), also for every live binding on [Manager.Detach] and
// [Manager.Close] — and returns the function that removes it.
func (m *Manager) OnRemoved(fn func(Resolution)) (stop func()) {
	if fn == nil {
		return func() {}
	}
	h := &handler{fn: fn}
	m.mu.Lock()
	m.removed = append(m.removed, h)
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.removed = slices.DeleteFunc(m.removed, func(x *handler) bool { return x == h })
	}
}

// Established returns the established bindings, by source endpoint and
// entry key.
func (m *Manager) Established() []Resolution {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	var out []Resolution
	for _, ep := range sortedKeys(m.records) {
		rec := m.records[ep]
		for _, k := range sortedKeys(rec.established) {
			out = append(out, rec.established[k])
		}
	}
	return out
}

// GroupKeysChanged tells the manager that a fabric's group key map or key
// sets changed: the group entries of the fabric that now hold a key
// resolve. Calls coalesce into one recheck, run after the change is
// complete — matter.js #scheduleGroupRecheck (BindingManager.ts:513-520).
func (m *Manager) GroupKeysChanged(fabricIndex uint8) {
	m.mu.Lock()
	if m.closing || m.rechecks[fabricIndex] {
		m.mu.Unlock()
		return
	}
	m.rechecks[fabricIndex] = true
	m.mu.Unlock()
	m.post(func() {
		m.mu.Lock()
		delete(m.rechecks, fabricIndex)
		m.mu.Unlock()
		m.recheckGroups(fabricIndex)
	})
}

// Close detaches every Binding server, emitting removed for each
// established binding (BindingManager.disposeServer, :207-244, as on the
// node's shutdown — BindingServer.ts:52), stops the worker and the
// background connections, and waits for them. Work asked for before Close
// runs first; work asked for after it is dropped. Idempotent; not to be
// called from a handler.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		m.wg.Wait()
		return
	}
	m.closing = true
	atts := m.attached
	m.attached = map[uint16]attachment{}
	// The disposal is the last work: post refuses everything from here on.
	m.queue = append(m.queue, func() {
		m.stateMu.Lock()
		eps := sortedKeys(m.records)
		m.stateMu.Unlock()
		for _, ep := range eps {
			m.disposeServer(ep)
		}
		m.mu.Lock()
		clear(m.registered)
		m.closed = true
		m.mu.Unlock()
		m.cancel()
	})
	m.mu.Unlock()
	for _, a := range atts {
		a.stop()
	}
	m.signal()
	m.wg.Wait()
}

func sortedKeys[K uint8 | uint16 | string, V any](mp map[K]V) []K {
	out := make([]K, 0, len(mp))
	for k := range mp {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
