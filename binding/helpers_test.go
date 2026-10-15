// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package binding

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/im"
)

// logEntry is one captured log record.
type logEntry struct {
	level  slog.Level
	msg    string
	reason string
}

// logSink captures the manager's log records.
type logSink struct {
	mu  sync.Mutex
	got []logEntry
}

func (s *logSink) Enabled(context.Context, slog.Level) bool { return true }

func (s *logSink) Handle(_ context.Context, r slog.Record) error {
	e := logEntry{level: r.Level, msg: r.Message}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "reason" {
			e.reason = a.Value.String()
		}
		return true
	})
	s.mu.Lock()
	s.got = append(s.got, e)
	s.mu.Unlock()
	return nil
}

func (s *logSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *logSink) WithGroup(string) slog.Handler      { return s }

// reasons are the captured reasons at level.
func (s *logSink) reasons(level slog.Level) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, e := range s.got {
		if e.level == level && e.reason != "" {
			out = append(out, e.reason)
		}
	}
	return out
}

// fakeTopology is a node: our node id per fabric, the endpoints with their
// servers and declared clients, the group keys.
type fakeTopology struct {
	mu      sync.Mutex
	nodeIDs map[uint8]uint64
	servers map[uint16][]uint32
	clients map[uint16][]uint32
	keys    map[[2]uint16]bool
}

func newFakeTopology() *fakeTopology {
	return &fakeTopology{
		nodeIDs: map[uint8]uint64{1: 100},
		servers: map[uint16][]uint32{1: {0x0003, 0x001E}, 2: {0x0006}},
		clients: map[uint16][]uint32{1: {0x0003, 0x0006}},
		keys:    map[[2]uint16]bool{},
	}
}

func (f *fakeTopology) NodeID(fabricIndex uint8) (uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.nodeIDs[fabricIndex]
	return id, ok
}

func (f *fakeTopology) HasEndpoint(ep uint16) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.servers[ep]
	return ok
}

func (f *fakeTopology) HasServer(ep uint16, cluster uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.servers[ep], cluster)
}

func (f *fakeTopology) ClientClusters(ep uint16) []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.clients[ep])
}

func (f *fakeTopology) HasGroupKey(fabricIndex uint8, group uint16) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys[[2]uint16{uint16(fabricIndex), group}]
}

func (f *fakeTopology) setKey(fabricIndex uint8, group uint16, held bool) {
	f.mu.Lock()
	f.keys[[2]uint16{uint16(fabricIndex), group}] = held
	f.mu.Unlock()
}

func (f *fakeTopology) setClients(ep uint16, clients ...uint32) {
	f.mu.Lock()
	f.clients[ep] = clients
	f.mu.Unlock()
}

// fakePeers hands out one peer value per address; the manager never
// calls the peer's own methods in these tests (fakeLink stands in).
type fakePeers struct {
	mu    sync.Mutex
	peers map[handshake.PeerAddress]*peer.Peer
	err   error
}

func newFakePeers() *fakePeers {
	return &fakePeers{peers: map[handshake.PeerAddress]*peer.Peer{}}
}

func (f *fakePeers) For(addr handshake.PeerAddress) (*peer.Peer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	p := f.peers[addr]
	if p == nil {
		p = &peer.Peer{}
		f.peers[addr] = p
	}
	return p, nil
}

// fakeLink is the peers' sessions and connection attempts.
type fakeLink struct {
	mu        sync.Mutex
	sessions  map[*peer.Peer]bool
	observers map[*peer.Peer][]*func()
	connects  map[*peer.Peer]int
}

func newFakeLink() *fakeLink {
	return &fakeLink{sessions: map[*peer.Peer]bool{}, observers: map[*peer.Peer][]*func(){}, connects: map[*peer.Peer]int{}}
}

func (l *fakeLink) connect(ctx context.Context, p *peer.Peer) error {
	l.mu.Lock()
	l.connects[p]++
	l.mu.Unlock()
	return nil
}

func (l *fakeLink) hasSession(p *peer.Peer) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sessions[p]
}

func (l *fakeLink) observe(p *peer.Peer, fn func()) func() {
	f := &fn
	l.mu.Lock()
	l.observers[p] = append(l.observers[p], f)
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.observers[p] = slices.DeleteFunc(l.observers[p], func(x *func()) bool { return x == f })
	}
}

// sessionAdded gives p a session and tells its observers.
func (l *fakeLink) sessionAdded(p *peer.Peer) {
	l.mu.Lock()
	l.sessions[p] = true
	obs := slices.Clone(l.observers[p])
	l.mu.Unlock()
	for _, f := range obs {
		(*f)()
	}
}

func (l *fakeLink) observing(p *peer.Peer) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.observers[p])
}

func (l *fakeLink) connected(p *peer.Peer) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.connects[p]
}

// fakeGroupTarget records what is sent to the group.
type fakeGroupTarget struct {
	fabricIndex uint8
	group       uint16
}

func (fakeGroupTarget) Invoke(context.Context, im.ConcreteCommandPath, spec.Encodable) error {
	return nil
}

// fakeGroups is the group sender; it records the calls.
type fakeGroups struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (g *fakeGroups) Sender(_ context.Context, fabricIndex uint8, group uint16) (GroupTarget, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.err != nil {
		return nil, g.err
	}
	return fakeGroupTarget{fabricIndex: fabricIndex, group: group}, nil
}

// harness is a manager over the fakes.
type harness struct {
	m     *Manager
	topo  *fakeTopology
	peers *fakePeers
	link  *fakeLink
	group *fakeGroups
	log   *logSink

	mu          sync.Mutex
	established []Resolution
	removed     []Resolution
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	h := &harness{topo: newFakeTopology(), peers: newFakePeers(), link: newFakeLink(), group: &fakeGroups{}, log: &logSink{}}
	cfg := Config{Topology: h.topo, Peers: h.peers, Groups: h.group, Logger: slog.New(h.log)}
	if mutate != nil {
		mutate(&cfg)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m.link = h.link
	h.m = m
	m.OnRemoved(func(r Resolution) {
		h.mu.Lock()
		h.removed = append(h.removed, r)
		h.mu.Unlock()
	})
	t.Cleanup(m.Close)
	return h
}

// listen registers the established recorder.
func (h *harness) listen() (stop func()) {
	return h.m.OnEstablished(func(r Resolution) {
		h.mu.Lock()
		h.established = append(h.established, r)
		h.mu.Unlock()
	})
}

// flush waits until the worker ran everything queued so far, twice, so
// work the first round queued runs too.
func (h *harness) flush() {
	for range 3 {
		done := make(chan struct{})
		h.m.post(func() { close(done) })
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			panic("binding: the worker is stuck")
		}
	}
}

func (h *harness) takeEstablished() []Resolution {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.established
	h.established = nil
	return out
}

func (h *harness) takeRemoved() []Resolution {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.removed
	h.removed = nil
	return out
}

var errNoPeer = errors.New("no such peer")

func peerAddr(fabricIndex uint8, node uint64) handshake.PeerAddress {
	return handshake.PeerAddress{FabricIndex: fabricIndex, NodeID: node}
}

// waitFor polls cond for up to ten seconds.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
