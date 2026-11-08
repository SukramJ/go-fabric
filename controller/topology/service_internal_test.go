// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/thread/brdiscovery"
)

// fakeMember is a node with a fixed image; read runs onRead.
type fakeMember struct {
	img    Image
	onRead func(ctx context.Context, paths []client.AttributePath)
}

func (m *fakeMember) image() (Image, bool) { return m.img, true }

func (m *fakeMember) read(ctx context.Context, paths []client.AttributePath) error {
	if m.onRead != nil {
		m.onRead(ctx, paths)
	}
	return nil
}

// harness is matterjs-server's makeHarness (NetworkTopologyServiceTest.ts:166-203,
// matterjs-server at 0bd06f22) on a fake clock: a member list the test
// swaps, every emitted snapshot recorded.
type harness struct {
	t       *testing.T
	fake    *clocktest.Fake
	svc     *Service
	mu      sync.Mutex
	members []member
	emitted []Topology
}

func newHarness(t *testing.T, cfg Config, imgs ...Image) *harness {
	t.Helper()
	h := &harness{t: t, fake: clocktest.New(time.Unix(1735689600, 0))}
	h.set(imgs...)
	cfg.Clock = h.fake
	if cfg.Debounce == 0 {
		cfg.Debounce = 5 * time.Millisecond
	}
	if cfg.Periodic == 0 {
		cfg.Periodic = 10 * time.Minute // far out, as the harness's 600 s
	}
	h.svc = newService(h.list, cfg)
	h.svc.OnChanged(func(t Topology) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.emitted = append(h.emitted, t)
	})
	t.Cleanup(h.svc.Close)
	return h
}

func (h *harness) list() []member {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.members)
}

func (h *harness) set(imgs ...Image) {
	ms := make([]member, len(imgs))
	for i := range imgs {
		ms[i] = &fakeMember{img: imgs[i]}
	}
	h.setMembers(ms...)
}

func (h *harness) setMembers(ms ...member) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.members = ms
}

func (h *harness) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.emitted)
}

func (h *harness) last() Topology {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.emitted[len(h.emitted)-1]
}

func (h *harness) want(n int, after string) {
	h.t.Helper()
	if got := h.count(); got != n {
		h.t.Fatalf("%s: %d emitted, want %d", after, got, n)
	}
}

func router(id uint64, rloc uint16) Image {
	return mkThread(id, threadOpts{role: role(5), rloc16: ptr(rloc)})
}

// TestServiceDebounce ports "collapses a burst of attribute reports into a
// single debounced emit" and "does not re-emit when a rebuild produces an
// identical graph" (NetworkTopologyServiceTest.ts:462-488, matterjs-server
// at 0bd06f22), and holds the debounce trailing: each change restarts it
// (#scheduleRebuild, NetworkTopologyService.ts:260-268).
func TestServiceDebounce(t *testing.T) {
	h := newHarness(t, Config{Debounce: 10 * time.Millisecond}, router(1, 1024))
	h.svc.changed()
	h.svc.changed()
	h.fake.Advance(8 * time.Millisecond)
	h.svc.changed()
	h.fake.Advance(8 * time.Millisecond)
	h.want(0, "a change 8 ms before")
	h.fake.Advance(2 * time.Millisecond)
	h.want(1, "the burst's debounce")

	h.svc.changed()
	h.fake.Advance(10 * time.Millisecond)
	h.want(1, "an identical graph")
}

// TestServiceLifecycle ports "does not re-emit when only node/connection
// ordering changes" and "re-emits when node lifecycle events change the
// graph" (NetworkTopologyServiceTest.ts:490-533, matterjs-server at
// 0bd06f22).
func TestServiceLifecycle(t *testing.T) {
	a := mkThread(1, threadOpts{role: role(5), rloc16: ptr[uint16](1024), neighbours: fullMesh()[1].ThreadNetworkDiagnostics.NeighborTable})
	b := mkThread(2, threadOpts{role: role(5), rloc16: ptr[uint16](1025), neighbours: fullMesh()[0].ThreadNetworkDiagnostics.NeighborTable[:1]})
	h := newHarness(t, Config{}, a, b)
	step := func() {
		h.svc.changed()
		h.fake.Advance(5 * time.Millisecond)
	}
	step()
	h.want(1, "the first build")
	h.set(b, a)
	step()
	h.want(1, "the same graph in another order")

	h.set(router(1, 1024))
	step()
	h.want(2, "node 2 gone")
	step()
	h.want(2, "no change")
	off := router(1, 1024)
	off.Available = false
	h.set(off)
	step()
	h.want(3, "node 1 offline")
	if h.last().Nodes[0].Available {
		t.Error("the offline node is available")
	}
	h.set()
	step()
	h.want(4, "node 1 decommissioned")
	if len(h.last().Nodes) != 0 {
		t.Errorf("nodes after removal: %+v", h.last().Nodes)
	}
}

// TestServicePeriodic ports "emits once from the periodic rebuild and
// stays quiet while the graph is stable" (NetworkTopologyServiceTest.ts:572-580,
// matterjs-server at 0bd06f22).
func TestServicePeriodic(t *testing.T) {
	h := newHarness(t, Config{Periodic: 15 * time.Millisecond}, router(1, 1024))
	h.fake.Advance(60 * time.Millisecond)
	h.want(1, "four periodic ticks")
	if got := h.fake.Armed(); len(got) != 1 {
		t.Errorf("armed after the ticks: %v", got)
	}
}

// TestServiceRefreshFanOut ports "fans out reads to online Thread/Wi-Fi
// nodes and skips offline/other nodes" (NetworkTopologyServiceTest.ts:636-658,
// matterjs-server at 0bd06f22) and "emits after a refresh that changes
// the graph" (:706-719).
func TestServiceRefreshFanOut(t *testing.T) {
	h := newHarness(t, Config{})
	var mu sync.Mutex
	reads := map[uint64][]client.AttributePath{}
	mk := func(img Image) *fakeMember {
		m := &fakeMember{img: img}
		m.onRead = func(_ context.Context, paths []client.AttributePath) {
			mu.Lock()
			defer mu.Unlock()
			reads[m.img.NodeID] = paths
		}
		return m
	}
	offline := router(3, 1026)
	offline.Available = false
	eth := Image{NodeID: 4, Available: true, NetworkFeatures: 4}
	thread := mk(router(1, 1024))
	h.setMembers(thread, mk(mkWifi(2, ptr[int8](-50), false)), mk(offline), mk(eth))
	topo, err := h.svc.Refresh(context.Background())
	if err != nil || len(topo.Nodes) == 0 {
		t.Fatalf("Refresh = %+v, %v", topo, err)
	}
	if len(reads) != 2 || !slices.Equal(reads[1], threadRefreshPaths) || !slices.Equal(reads[2], wifiRefreshPaths) {
		t.Errorf("reads = %v", reads)
	}
	h.want(1, "the first refresh")

	thread.onRead = func(context.Context, []client.AttributePath) { h.set(router(1, 1024), router(2, 1025)) }
	h.setMembers(thread)
	if _, err := h.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.want(2, "a refresh that surfaced node 2")
	if len(h.last().Nodes) != 2 {
		t.Errorf("nodes = %+v", h.last().Nodes)
	}
}

// blocking is a member list whose reads report their start and wait for
// the test's release.
type blocking struct {
	started chan uint64
	release chan struct{}
	active  atomic.Int32
	max     atomic.Int32
}

func newBlocking() *blocking {
	return &blocking{started: make(chan uint64, 16), release: make(chan struct{})}
}

func (b *blocking) member(img Image) *fakeMember {
	return &fakeMember{img: img, onRead: func(context.Context, []client.AttributePath) {
		n := b.active.Add(1)
		for {
			m := b.max.Load()
			if n <= m || b.max.CompareAndSwap(m, n) {
				break
			}
		}
		b.started <- img.NodeID
		<-b.release
		b.active.Add(-1)
	}}
}

func recv(t *testing.T, ch chan uint64) uint64 {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("no read started")
	}
	return 0
}

// TestServiceRefreshConcurrency ports "caps the number of concurrent reads
// at refreshConcurrency" (NetworkTopologyServiceTest.ts:660-678,
// matterjs-server at 0bd06f22).
func TestServiceRefreshConcurrency(t *testing.T) {
	h := newHarness(t, Config{RefreshConcurrency: 2})
	b := newBlocking()
	ms := make([]member, 0, 5)
	for i := range uint64(5) {
		ms = append(ms, b.member(router(i+1, uint16(1024+i))))
	}
	h.setMembers(ms...)
	done := make(chan error, 1)
	go func() { _, err := h.svc.Refresh(context.Background()); done <- err }()
	recv(t, b.started)
	recv(t, b.started)
	for range 3 {
		b.release <- struct{}{}
		recv(t, b.started)
	}
	for range 2 {
		b.release <- struct{}{}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := b.max.Load(); got != 2 {
		t.Errorf("most reads at once = %d, want 2", got)
	}
}

// TestServiceRefreshDeadline ports "stops starting reads once the refresh
// deadline expires" (NetworkTopologyServiceTest.ts:680-704, matterjs-server
// at 0bd06f22).
func TestServiceRefreshDeadline(t *testing.T) {
	h := newHarness(t, Config{RefreshConcurrency: 1, RefreshTimeout: 20 * time.Millisecond})
	b := newBlocking()
	h.setMembers(b.member(router(1, 1024)), b.member(router(2, 1025)), b.member(router(3, 1026)))
	done := make(chan error, 1)
	go func() { _, err := h.svc.Refresh(context.Background()); done <- err }()
	if got := recv(t, b.started); got != 1 {
		t.Fatalf("first read of node %d", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.fake.Resolve(ctx, func() error { return <-done }); err != nil {
		t.Fatal(err)
	}
	close(b.release)
	h.fake.Yield()
	select {
	case n := <-b.started:
		t.Errorf("node %d read after the deadline", n)
	default:
	}
}

// TestServiceRefreshShared ports "coalesces concurrent callers onto a
// single read fan-out" (NetworkTopologyServiceTest.ts:721-746,
// matterjs-server at 0bd06f22).
func TestServiceRefreshShared(t *testing.T) {
	h := newHarness(t, Config{})
	b := newBlocking()
	h.setMembers(b.member(router(1, 1024)), b.member(router(2, 1025)))
	type res struct {
		t   Topology
		err error
	}
	out := make(chan res, 2)
	go func() { t, err := h.svc.Refresh(context.Background()); out <- res{t, err} }()
	recv(t, b.started)
	go func() { t, err := h.svc.Refresh(context.Background()); out <- res{t, err} }()
	recv(t, b.started)
	b.release <- struct{}{}
	b.release <- struct{}{}
	r1, r2 := <-out, <-out
	if r1.err != nil || r2.err != nil || len(r1.t.Nodes) != 2 || &r1.t.Nodes[0] != &r2.t.Nodes[0] {
		t.Fatalf("callers got %+v and %+v", r1, r2)
	}
	select {
	case n := <-b.started:
		t.Fatalf("a second read of node %d", n)
	default:
	}
	go func() { _, err := h.svc.Refresh(context.Background()); out <- res{err: err} }()
	recv(t, b.started)
	recv(t, b.started)
	b.release <- struct{}{}
	b.release <- struct{}{}
	if r := <-out; r.err != nil {
		t.Fatal(r.err)
	}

	// A caller's context bounds its wait, not the shared refresh.
	go func() { _, err := h.svc.Refresh(context.Background()); out <- res{err: err} }()
	recv(t, b.started)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.svc.Refresh(ctx); err == nil {
		t.Error("a cancelled caller waited")
	}
	recv(t, b.started)
	close(b.release)
	if r := <-out; r.err != nil {
		t.Fatal(r.err)
	}
}

// TestServiceClose ports "stops emitting and refresh() returns a snapshot
// without emitting" and "does not emit when stop() lands mid-refresh"
// (NetworkTopologyServiceTest.ts:750-794, matterjs-server at 0bd06f22).
func TestServiceClose(t *testing.T) {
	h := newHarness(t, Config{}, router(1, 1024))
	h.svc.changed()
	h.fake.Advance(5 * time.Millisecond)
	h.want(1, "a change")
	h.svc.Close()
	h.svc.Close()
	h.svc.changed()
	h.fake.Advance(time.Hour)
	h.want(1, "a change after Close")
	topo, err := h.svc.Refresh(context.Background())
	if err != nil || len(topo.Nodes) != 1 {
		t.Fatalf("Refresh after Close = %+v, %v", topo, err)
	}
	h.want(1, "a refresh after Close")

	h = newHarness(t, Config{RefreshTimeout: time.Hour}, router(1, 1024))
	b := newBlocking()
	m := b.member(router(1, 1024))
	read := m.onRead
	m.onRead = func(ctx context.Context, p []client.AttributePath) {
		h.set(router(1, 1024), router(2, 1025))
		read(ctx, p)
	}
	h.setMembers(m)
	done := make(chan Topology, 1)
	go func() { topo, _ := h.svc.Refresh(context.Background()); done <- topo }()
	recv(t, b.started)
	h.svc.Close()
	close(b.release)
	if topo := <-done; len(topo.Nodes) != 2 {
		t.Errorf("the caller's snapshot: %+v", topo)
	}
	h.want(0, "a refresh Close landed in")
}

// TestServiceObservers: OnChanged's unsubscribe stops the calls, once,
// and Topology builds without telling anyone.
func TestServiceObservers(t *testing.T) {
	h := newHarness(t, Config{}, router(1, 1024))
	var n atomic.Int32
	unsub := h.svc.OnChanged(func(Topology) { n.Add(1) })
	if got := h.svc.Topology(); len(got.Nodes) != 1 || !got.CollectedAt.Equal(h.fake.Now()) {
		t.Errorf("Topology = %+v", got)
	}
	h.want(0, "Topology")
	h.svc.changed()
	h.fake.Advance(5 * time.Millisecond)
	unsub()
	unsub()
	h.set(router(1, 1024), router(2, 1025))
	h.svc.changed()
	h.fake.Advance(5 * time.Millisecond)
	if n.Load() != 1 {
		t.Errorf("unsubscribed observer called %d times, want 1", n.Load())
	}
	h.want(2, "two graphs")
}

// TestChangesInstall: Install wraps HooksFor — else the template
// Node.Hooks — so a node's change, connection-state change, commission and
// decommission each schedule a rebuild of the attached service after the
// host's own hook ran; a closed service is detached.
func TestChangesInstall(t *testing.T) {
	var host []string
	note := func(s string) func() { return func() { host = append(host, s) } }
	var changes Changes
	cfg := controller.Config{}
	cfg.Node.Hooks = client.Hooks{Changed: note("template")}
	changes.Install(&cfg)
	if hooks := cfg.HooksFor("peer1"); hooks.Changed == nil {
		t.Fatal("no Changed hook")
	} else {
		hooks.Changed()
	}
	if !slices.Equal(host, []string{"template"}) {
		t.Fatalf("template hook: %v", host)
	}
	cfg = controller.Config{HooksFor: func(id string) client.Hooks {
		return client.Hooks{
			Changed: note("changed " + id), ConnectionStateChanged: func(client.ConnectionState) { host = append(host, "state") },
			Commissioned: note("commissioned"), Decommissioned: note("decommissioned"),
		}
	}}
	changes.Install(&cfg)
	h := newHarness(t, Config{Changes: &changes}, router(1, 1024))
	hooks := cfg.HooksFor("peer2")
	for i, fire := range []func(){hooks.Changed, func() { hooks.ConnectionStateChanged(client.Connected) }, hooks.Commissioned, hooks.Decommissioned} {
		h.set(router(1, uint16(2000+i)))
		fire()
		h.fake.Advance(5 * time.Millisecond)
		h.want(i+1, "hook "+[]string{"Changed", "ConnectionStateChanged", "Commissioned", "Decommissioned"}[i])
	}
	if !slices.Equal(host[1:], []string{"changed peer2", "state", "commissioned", "decommissioned"}) {
		t.Errorf("host hooks: %v", host)
	}
	h.svc.Close()
	if len(changes.subs) != 0 {
		t.Errorf("a closed service stays attached: %d", len(changes.subs))
	}
	bare := changes.wrap(client.Hooks{})
	bare.Changed()
	bare.ConnectionStateChanged(client.Connected)
	bare.Commissioned()
	bare.Decommissioned()
}

// fakeSource is a Source whose images the test swaps.
type fakeSource struct {
	mu   sync.Mutex
	imgs []Image
}

func (f *fakeSource) Images() []Image {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.imgs)
}

func (f *fakeSource) set(imgs ...Image) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imgs = imgs
}

func nodeIDs(t Topology) []string {
	ids := make([]string, len(t.Nodes))
	for i := range t.Nodes {
		ids[i] = t.Nodes[i].ID
	}
	slices.Sort(ids)
	return ids
}

// TestServiceSourceMerges ports "merges aux-source nodes into the graph,
// with edges resolving across sources" (NetworkTopologyServiceTest.ts:584-602,
// matterjs-server at 0bd06f22): a primary node's neighbour matched by
// RLOC16 to a source's node gives a strong link, and registering
// schedules the rebuild.
func TestServiceSourceMerges(t *testing.T) {
	primary := mkThread(1, threadOpts{role: role(5), rloc16: ptr[uint16](1024), neighbours: []threaddiagdef.NeighborTableStruct{
		neighbour(0, 1025, 3, ptr[int8](-40), false),
	}})
	h := newHarness(t, Config{}, primary)
	h.svc.AddSource(&fakeSource{imgs: []Image{mkThread(2, threadOpts{role: role(3), rloc16: ptr[uint16](1025)})}})
	h.fake.Advance(5 * time.Millisecond)
	h.want(1, "a registration")
	topo := h.last()
	if got := nodeIDs(topo); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("nodes = %v", got)
	}
	i := slices.IndexFunc(topo.Connections, func(c Connection) bool { return c.Source == "1" && c.Target == "2" })
	if i < 0 || topo.Connections[i].Strength != StrengthStrong {
		t.Errorf("connections = %+v", topo.Connections)
	}
}

// TestServiceSourceChanged ports "rebuilds when the aux source reports a
// node added" (NetworkTopologyServiceTest.ts:604-618, matterjs-server at
// 0bd06f22): the host's Changed is the source's nodeAdded.
func TestServiceSourceChanged(t *testing.T) {
	h := newHarness(t, Config{})
	src := &fakeSource{imgs: []Image{router(2, 1025)}}
	h.svc.AddSource(src)
	h.fake.Advance(5 * time.Millisecond)
	h.want(1, "a registration")
	src.set(router(2, 1025), router(3, 1026))
	h.svc.Changed()
	h.fake.Advance(5 * time.Millisecond)
	h.want(2, "a source's change")
	if n := len(h.last().Nodes); n != 2 {
		t.Errorf("nodes = %d, want 2", n)
	}
}

// TestServiceSourceNotRead ports "never issues refresh() reads to
// aux-source nodes and is idempotent per source"
// (NetworkTopologyServiceTest.ts:620-633, matterjs-server at 0bd06f22).
func TestServiceSourceNotRead(t *testing.T) {
	h := newHarness(t, Config{})
	var mu sync.Mutex
	var reads []uint64
	m := &fakeMember{img: router(1, 1024)}
	m.onRead = func(context.Context, []client.AttributePath) {
		mu.Lock()
		defer mu.Unlock()
		reads = append(reads, m.img.NodeID)
	}
	h.setMembers(m)
	src := &fakeSource{imgs: []Image{router(2, 1025)}}
	h.svc.AddSource(src)
	h.svc.AddSource(src)()
	topo, err := h.svc.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(topo.Nodes) != 2 {
		t.Errorf("nodes = %v, want 2: a duplicate source merged twice", nodeIDs(topo))
	}
	if !slices.Equal(reads, []uint64{1}) {
		t.Errorf("reads = %v, want [1]", reads)
	}
}

// TestServiceSourceRemove: the remove function drops the source once and
// schedules the rebuild that emits the graph without its node.
func TestServiceSourceRemove(t *testing.T) {
	h := newHarness(t, Config{}, router(1, 1024))
	remove := h.svc.AddSource(&fakeSource{imgs: []Image{router(2, 1025)}})
	h.fake.Advance(5 * time.Millisecond)
	h.want(1, "a registration")
	remove()
	remove()
	h.fake.Advance(5 * time.Millisecond)
	h.want(2, "a removal")
	if got := nodeIDs(h.last()); !slices.Equal(got, []string{"1"}) {
		t.Errorf("nodes after removal = %v", got)
	}
}

// TestServiceSourceClosed: a closed service registers nothing, ignores
// Changed and drops the sources it had.
func TestServiceSourceClosed(t *testing.T) {
	h := newHarness(t, Config{}, router(1, 1024))
	h.svc.AddSource(&fakeSource{imgs: []Image{router(2, 1025)}})
	h.svc.Close()
	h.svc.AddSource(&fakeSource{imgs: []Image{router(3, 1026)}})()
	h.svc.AddSource(nil)
	h.svc.Changed()
	h.fake.Advance(time.Hour)
	h.want(0, "a closed service")
	if got := nodeIDs(h.svc.Topology()); !slices.Equal(got, []string{"1"}) {
		t.Errorf("nodes after Close = %v", got)
	}
}

// fakeBorderRouters is the test's BrEvents and makeBrEvents with its brs list
// (NetworkTopologyServiceTest.ts:108-119, matterjs-server at 0bd06f22).
type fakeBorderRouters struct {
	mu      sync.Mutex
	entries []brdiscovery.Entry
	fns     map[int]func(brdiscovery.Event)
	next    int
}

func (f *fakeBorderRouters) List() []brdiscovery.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.entries)
}

func (f *fakeBorderRouters) OnChange(fn func(brdiscovery.Event)) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fns == nil {
		f.fns = map[int]func(brdiscovery.Event){}
	}
	id := f.next
	f.next++
	f.fns[id] = fn
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.fns, id)
	}
}

func (f *fakeBorderRouters) set(kind brdiscovery.EventKind, entries ...brdiscovery.Entry) {
	f.mu.Lock()
	f.entries = entries
	fns := make([]func(brdiscovery.Event), 0, len(f.fns))
	for _, fn := range f.fns {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn(brdiscovery.Event{Kind: kind, Entry: entries[0]})
	}
}

func (f *fakeBorderRouters) observers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fns)
}

// TestServiceBorderRouters ports "rebuilds against the current Border
// Router registry when a BR is discovered" and "rebuilds when a Border
// Router entry is updated (e.g. its network name resolves)"
// (NetworkTopologyServiceTest.ts:535-570, matterjs-server at 0bd06f22):
// the registry's events schedule a debounced rebuild
// (NetworkTopologyService.ts:170-172), and Close stops listening.
func TestServiceBorderRouters(t *testing.T) {
	brs := &fakeBorderRouters{}
	node1 := mkThread(1, threadOpts{role: role(5), rloc16: ptr[uint16](1024), neighbours: []threaddiagdef.NeighborTableStruct{
		neighbour(brExt, 61440, 2, ptr[int8](-70), true),
	}})
	h := newHarness(t, Config{BorderRouters: brs}, node1)
	h.svc.changed()
	h.fake.Advance(5 * time.Millisecond)
	h.want(1, "the first build")
	if n := byID(h.last())[brUnkID]; n.Kind != KindThreadUnknown {
		t.Fatalf("before the registry knows it: %+v", n)
	}

	unnamed := makeBr()
	unnamed.NetworkName = ""
	brs.set(brdiscovery.EventAdded, unnamed)
	h.fake.Advance(5 * time.Millisecond)
	h.want(2, "a border router added")
	m := byID(h.last())
	if m[brID].Kind != KindBorderRouter || slices.ContainsFunc(h.last().Nodes, func(n Node) bool { return n.Kind == KindThreadUnknown }) {
		t.Fatalf("after the registry added it: %+v", h.last().Nodes)
	}

	named := makeBr()
	named.NetworkName = "ResolvedNet"
	brs.set(brdiscovery.EventUpdated, named)
	h.fake.Advance(5 * time.Millisecond)
	h.want(3, "a border router updated")
	if got := byID(h.last())[brID].NetworkName; got != "ResolvedNet" {
		t.Fatalf("network name %q", got)
	}

	h.svc.Close()
	if brs.observers() != 0 {
		t.Fatal("Close kept listening to the registry")
	}
}
