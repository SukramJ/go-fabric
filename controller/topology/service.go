// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/clock"
	generaldiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
)

// The defaults of [Config] — matterjs-server's at 0bd06f22
// (NetworkTopologyService.ts:133-136).
const (
	DefaultDebounce           = 2 * time.Second
	DefaultPeriodic           = 60 * time.Second
	DefaultRefreshTimeout     = 30 * time.Second
	DefaultRefreshConcurrency = 4
)

// Config configures a [Service]; the zero value is matterjs-server's
// defaults on the system clock, without change hooks.
type Config struct {
	// Debounce is the trailing debounce of a change-driven rebuild: every
	// change restarts it; 0 is [DefaultDebounce].
	Debounce time.Duration
	// Periodic is the interval of the periodic rebuild, which catches what
	// arrives without a change hook; 0 is [DefaultPeriodic].
	Periodic time.Duration
	// RefreshTimeout bounds a [Service.Refresh]: no read starts after it;
	// 0 is [DefaultRefreshTimeout].
	RefreshTimeout time.Duration
	// RefreshConcurrency caps the reads of a refresh in flight at once; 0
	// is [DefaultRefreshConcurrency].
	RefreshConcurrency int
	// Clock paces the debounce, the periodic rebuild and the refresh
	// deadline and stamps CollectedAt; nil is the system clock. The
	// controller's own (controller.Config.Clock) is the natural choice.
	Clock clock.Clock
	// Changes carries the nodes' hooks to the service; nil leaves the
	// service with its periodic rebuild and [Service.Refresh] alone.
	Changes *Changes
}

// The refresh paths, endpoint 0 — matterjs-server's THREAD_REFRESH_PATHS
// and WIFI_REFRESH_PATHS at 0bd06f22 (NetworkTopologyService.ts:55, :61):
// a Thread node's neighbour and route tables, interfaces, RLOC16, role,
// network name and extended PAN id; a Wi-Fi node's BSSID, channel, RSSI
// and network list. The route table is read as there, for the supplement
// this port leaves out.
var (
	threadRefreshPaths = []client.AttributePath{
		client.AttributeAt(0, threaddiagdef.ClusterID, threaddiagdef.AttrNeighborTable),
		client.AttributeAt(0, threaddiagdef.ClusterID, threaddiagdef.AttrRouteTable),
		client.AttributeAt(0, generaldiagdef.ClusterID, generaldiagdef.AttrNetworkInterfaces),
		client.AttributeAt(0, threaddiagdef.ClusterID, threaddiagdef.AttrRloc16),
		client.AttributeAt(0, threaddiagdef.ClusterID, threaddiagdef.AttrRoutingRole),
		client.AttributeAt(0, threaddiagdef.ClusterID, threaddiagdef.AttrNetworkName),
		client.AttributeAt(0, threaddiagdef.ClusterID, threaddiagdef.AttrExtendedPanId),
	}
	wifiRefreshPaths = []client.AttributePath{
		client.AttributeAt(0, wifidiagdef.ClusterID, wifidiagdef.AttrBssid),
		client.AttributeAt(0, wifidiagdef.ClusterID, wifidiagdef.AttrChannelNumber),
		client.AttributeAt(0, wifidiagdef.ClusterID, wifidiagdef.AttrRssi),
		client.AttributeAt(0, netcommdef.ClusterID, netcommdef.AttrNetworks),
	}
)

// member is one node the service builds from and refreshes: a
// client.Node, or a test's fake.
type member interface {
	image() (Image, bool)
	read(ctx context.Context, paths []client.AttributePath) error
}

// clientMember is a commissioned client.Node.
type clientMember struct{ n *client.Node }

func (m clientMember) image() (Image, bool) { return ImageOf(m.n) }

// read re-reads paths fabric-filtered, every value whatever the cached
// versions (IncludeKnownVersions): the report lands in the node's image.
func (m clientMember) read(ctx context.Context, paths []client.AttributePath) error {
	_, err := m.n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: paths}, IncludeKnownVersions: true})
	return err
}

// Service keeps the graph of a controller's commissioned nodes current
// and tells its observers when it moved — matterjs-server's
// NetworkTopologyService at 0bd06f22 (NetworkTopologyService.ts:132-303).
// It is safe for concurrent use; [Service.Close] stops it.
//
// A rebuild runs after a node's change (a report or read applied, a
// connection-state change, a commission, a decommission) once
// [Config].Debounce passed without another, every [Config].Periodic, and
// after a [Service.Refresh]. matterjs-server at 0bd06f22 schedules a
// rebuild for an attribute change of NetworkCommissioning,
// GeneralDiagnostics, ThreadNetworkDiagnostics and WiFiNetworkDiagnostics
// only (NetworkTopologyService.ts:46, :161-166); client.Hooks.Changed carries no
// path, so here every applied report schedules one — the debounce
// collapses a burst, and the hash keeps an unchanged graph from reaching
// the observers.
type Service struct {
	members func() []member
	cfg     Config
	clk     clock.Clock

	// emitMu serialises build, hash comparison and observer calls.
	emitMu   sync.Mutex
	lastHash string

	mu        sync.Mutex
	closed    bool
	debounce  clock.Timer
	periodic  clock.Timer
	inflight  *flight
	observers map[int]func(Topology)
	nextObs   int
	life      context.Context //nolint:containedctx // the service's lifetime, cancelled by Close: a refresh's reads outlive their caller
	cancel    context.CancelFunc
}

// flight is one refresh, shared by the callers that arrive while it runs.
type flight struct {
	done chan struct{}
	topo Topology
}

// New starts a service over ctrl's commissioned nodes (controller.Nodes).
// cfg.Changes, installed on ctrl's configuration before controller.New,
// brings the nodes' changes; the periodic rebuild starts at once.
func New(ctrl *controller.Controller, cfg Config) *Service {
	return newService(func() []member {
		nodes := ctrl.Nodes()
		out := make([]member, len(nodes))
		for i, n := range nodes {
			out[i] = clientMember{n}
		}
		return out
	}, cfg)
}

func newService(members func() []member, cfg Config) *Service {
	if cfg.Debounce <= 0 {
		cfg.Debounce = DefaultDebounce
	}
	if cfg.Periodic <= 0 {
		cfg.Periodic = DefaultPeriodic
	}
	if cfg.RefreshTimeout <= 0 {
		cfg.RefreshTimeout = DefaultRefreshTimeout
	}
	if cfg.RefreshConcurrency <= 0 {
		cfg.RefreshConcurrency = DefaultRefreshConcurrency
	}
	s := &Service{members: members, cfg: cfg, clk: clock.Or(cfg.Clock), observers: map[int]func(Topology){}}
	s.life, s.cancel = context.WithCancel(context.Background())
	s.mu.Lock()
	s.armPeriodicLocked()
	s.mu.Unlock()
	if cfg.Changes != nil {
		cfg.Changes.attach(s)
	}
	return s
}

// armPeriodicLocked arms the next periodic rebuild
// (NetworkTopologyService.ts:174, matterjs-server at 0bd06f22).
func (s *Service) armPeriodicLocked() {
	s.periodic = s.clk.AfterFunc(s.cfg.Periodic, func() {
		s.rebuild()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.closed {
			s.armPeriodicLocked()
		}
	})
}

// Topology builds a snapshot from the nodes' images as they stand, without
// telling the observers — matterjs-server's getTopology at 0bd06f22
// (NetworkTopologyService.ts:179-182).
func (s *Service) Topology() Topology {
	return Build(s.images(), s.clk.Now())
}

func (s *Service) images() []Image {
	var out []Image
	for _, m := range s.members() {
		if img, ok := m.image(); ok {
			out = append(out, img)
		}
	}
	return out
}

// OnChanged calls fn with every snapshot that differs from the one before
// it — the first included — until the returned function is called. fn
// runs on the service's goroutine, one call at a time; it must not call
// [Service.Refresh], which waits for that goroutine.
func (s *Service) OnChanged(fn func(Topology)) (unsubscribe func()) {
	s.mu.Lock()
	id := s.nextObs
	s.nextObs++
	s.observers[id] = fn
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.observers, id)
			s.mu.Unlock()
		})
	}
}

// Refresh re-reads the network diagnostics of every available Thread and
// Wi-Fi node, at most [Config].RefreshConcurrency at once, then rebuilds,
// tells the observers when the graph moved and returns the snapshot —
// matterjs-server's refresh at 0bd06f22 (NetworkTopologyService.ts:207-244).
// Callers that arrive while a refresh runs share it. A failed read is
// skipped. Once [Config].RefreshTimeout passed no further read starts and
// the refresh builds from what it has; the reads in flight run on (until
// [Service.Close]) and their reports reach the next rebuild
// (:196-203, :287-303). ctx bounds the caller's wait only. After Close
// it returns a snapshot without reading or telling anyone.
func (s *Service) Refresh(ctx context.Context) (Topology, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.Topology(), nil
	}
	f := s.inflight
	if f == nil {
		f = &flight{done: make(chan struct{})}
		s.inflight = f
		go s.runRefresh(f)
	}
	s.mu.Unlock()
	select {
	case <-f.done:
		return f.topo, nil
	case <-ctx.Done():
		return Topology{}, ctx.Err()
	}
}

func (s *Service) runRefresh(f *flight) {
	defer func() {
		s.mu.Lock()
		if s.inflight == f {
			s.inflight = nil
		}
		s.mu.Unlock()
		close(f.done)
	}()
	var tasks []func()
	for _, m := range s.members() {
		img, ok := m.image()
		if !ok || !img.Available {
			continue
		}
		var paths []client.AttributePath
		switch networkType(img.NetworkFeatures) {
		case NetworkThread:
			paths = threadRefreshPaths
		case NetworkWiFi:
			paths = wifiRefreshPaths
		default:
			continue
		}
		tasks = append(tasks, func() { _ = m.read(s.life, paths) })
	}
	s.runWithDeadline(tasks)
	t := s.Topology()
	s.emitIfChanged(t)
	f.topo = t
}

// runWithDeadline runs tasks on RefreshConcurrency workers until all
// returned or the deadline passed; after it no worker starts a task —
// matterjs-server's runWithConcurrency at 0bd06f22
// (NetworkTopologyService.ts:550-564).
func (s *Service) runWithDeadline(tasks []func()) {
	if len(tasks) == 0 {
		return
	}
	var expired atomic.Bool
	var next atomic.Int64
	all := make(chan struct{})
	var wg sync.WaitGroup
	for range min(s.cfg.RefreshConcurrency, len(tasks)) {
		wg.Go(func() {
			for !expired.Load() {
				i := int(next.Add(1)) - 1
				if i >= len(tasks) {
					return
				}
				tasks[i]()
			}
		})
	}
	go func() {
		wg.Wait()
		close(all)
	}()
	deadline := s.clk.NewTimer(s.cfg.RefreshTimeout)
	defer deadline.Stop()
	select {
	case <-all:
	case <-deadline.C():
		expired.Store(true)
	case <-s.life.Done():
		expired.Store(true)
	}
}

// changed schedules a rebuild after the debounce, restarting a pending
// one — matterjs-server's #scheduleRebuild at 0bd06f22
// (NetworkTopologyService.ts:260-268).
func (s *Service) changed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.debounce != nil {
		s.debounce.Stop()
	}
	s.debounce = s.clk.AfterFunc(s.cfg.Debounce, s.rebuild)
}

// rebuild builds and tells the observers when the graph moved.
func (s *Service) rebuild() { s.emitIfChanged(s.Topology()) }

// emitIfChanged tells the observers of t when its hash differs from the
// last one told — matterjs-server's #emitIfChanged at 0bd06f22
// (NetworkTopologyService.ts:279-285); a closed service tells no one
// (:240-242).
func (s *Service) emitIfChanged(t Topology) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	obs := make([]func(Topology), 0, len(s.observers))
	for _, id := range sortedKeys(s.observers) {
		obs = append(obs, s.observers[id])
	}
	s.mu.Unlock()
	h := hashTopology(t)
	if h == s.lastHash {
		return
	}
	s.lastHash = h
	for _, fn := range obs {
		fn(t)
	}
}

func sortedKeys(m map[int]func(Topology)) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Close stops the timers, cancels the reads of a refresh in flight and
// detaches from [Config].Changes; nothing is told afterwards
// (NetworkTopologyService.ts:247-258, matterjs-server at 0bd06f22). It
// is idempotent.
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.debounce != nil {
		s.debounce.Stop()
	}
	if s.periodic != nil {
		s.periodic.Stop()
	}
	s.mu.Unlock()
	s.cancel()
	if s.cfg.Changes != nil {
		s.cfg.Changes.detach(s)
	}
}

// hashTopology is the graph's identity without CollectedAt: the nodes
// sorted by id, the connections by source, target and network —
// matterjs-server's hashTopology at 0bd06f22
// (NetworkTopologyService.ts:530-539). Reordering is not a change.
func hashTopology(t Topology) string {
	nodes := slices.Clone(t.Nodes)
	slices.SortStableFunc(nodes, func(a, b Node) int { return strings.Compare(a.ID, b.ID) })
	conns := slices.Clone(t.Connections)
	slices.SortStableFunc(conns, func(a, b Connection) int {
		if c := strings.Compare(a.Source, b.Source); c != 0 {
			return c
		}
		if c := strings.Compare(a.Target, b.Target); c != 0 {
			return c
		}
		return strings.Compare(string(a.Network), string(b.Network))
	})
	b, _ := json.Marshal(struct {
		Nodes       []Node
		Connections []Connection
	}{nodes, conns})
	return string(b)
}

// Changes carries the hooks of a controller's nodes to the [Service]s
// built with it ([Config].Changes). The zero value is ready; install it on
// the controller's configuration before controller.New:
//
//	var changes topology.Changes
//	changes.Install(&cfg)
//	ctrl, err := controller.New(ctx, cfg)
//	…
//	svc := topology.New(ctrl, topology.Config{Changes: &changes, Clock: cfg.Clock})
//	defer svc.Close()
type Changes struct {
	mu   sync.Mutex
	subs []*Service
}

// Install wraps cfg's node hooks — cfg.HooksFor, else the template
// cfg.Node.Hooks — so a node's applied report or read (Changed), its
// connection-state change, its commission and its decommission also
// schedule a rebuild of the attached services — matterjs-server's
// attributeChanged, nodeAvailabilityChanged, nodeAdded and
// nodeDecommissioned at 0bd06f22 (NetworkTopologyService.ts:161-169).
// The host's hooks run first and keep their behaviour; the service's part
// only restarts a timer, so it is safe on the node's job queue. The
// composition follows the controller's own OTA hooks.
func (c *Changes) Install(cfg *controller.Config) {
	user := cfg.HooksFor
	template := cfg.Node.Hooks
	cfg.HooksFor = func(id string) client.Hooks {
		h := template
		if user != nil {
			h = user(id)
		}
		return c.wrap(h)
	}
}

func (c *Changes) wrap(h client.Hooks) client.Hooks {
	changed, state, commissioned, decommissioned := h.Changed, h.ConnectionStateChanged, h.Commissioned, h.Decommissioned
	h.Changed = func() {
		if changed != nil {
			changed()
		}
		c.notify()
	}
	h.ConnectionStateChanged = func(st client.ConnectionState) {
		if state != nil {
			state(st)
		}
		c.notify()
	}
	h.Commissioned = func() {
		if commissioned != nil {
			commissioned()
		}
		c.notify()
	}
	h.Decommissioned = func() {
		if decommissioned != nil {
			decommissioned()
		}
		c.notify()
	}
	return h
}

func (c *Changes) attach(s *Service) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = append(c.subs, s)
}

func (c *Changes) detach(s *Service) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = slices.DeleteFunc(c.subs, func(x *Service) bool { return x == s })
}

func (c *Changes) notify() {
	c.mu.Lock()
	subs := slices.Clone(c.subs)
	c.mu.Unlock()
	for _, s := range subs {
		s.changed()
	}
}
