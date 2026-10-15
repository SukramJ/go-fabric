// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// manualClock is an exchange.Clock whose timers fire in Advance.
type manualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualTimer
}

type manualTimer struct {
	at      time.Time
	f       func()
	stopped bool
}

func (t *manualTimer) Stop() bool { was := !t.stopped; t.stopped = true; return was }

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) AfterFunc(d time.Duration, f func()) exchange.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &manualTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

func (c *manualClock) armed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped {
			n++
		}
	}
	return n
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*manualTimer
	for _, t := range c.timers {
		if !t.stopped && !t.at.After(c.now) {
			t.stopped = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// failingStore fails Save while fail is set.
type failingStore struct {
	MemoryStore
	mu    sync.Mutex
	fail  bool
	saves int
}

func (s *failingStore) Save(nodeID string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error {
	s.mu.Lock()
	s.saves++
	fail := s.fail
	s.mu.Unlock()
	if fail {
		return errors.New("store down")
	}
	return s.MemoryStore.Save(nodeID, ep, cluster, values, version)
}

func (s *failingStore) setFail(f bool) {
	s.mu.Lock()
	s.fail = f
	s.mu.Unlock()
}

func uintNode(v uint64) spec.Node {
	return spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: v}}
}

func boolNode(v bool) spec.Node {
	t := tlv.TypeBoolFalse
	if v {
		t = tlv.TypeBoolTrue
	}
	return spec.Node{El: tlv.Element{Type: t, Bool: v}}
}

func listNode(vs ...uint64) spec.Node {
	n := spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}
	for _, v := range vs {
		n.Children = append(n.Children, uintNode(v))
	}
	return n
}

func ptr[T any](v T) *T { return &v }

// stored loads one cluster of the store.
func stored(t *testing.T, st Store, ep uint16, cluster uint32) (values map[uint32]spec.Node, version *uint32, ok bool) {
	t.Helper()
	eps, err := st.Load("n")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eps {
		if e.Number != ep {
			continue
		}
		for _, c := range e.Clusters {
			if c.ID != cluster {
				continue
			}
			out := map[uint32]spec.Node{}
			for k, b := range c.Values {
				n, err := decodeValue(b)
				if err != nil {
					t.Fatal(err)
				}
				out[k] = n
			}
			return out, c.Version, true
		}
	}
	return nil, nil, false
}

func newTestPersister(st Store, interval time.Duration, clock exchange.Clock) (*persister, *sync.Mutex) {
	var image sync.Mutex
	return &persister{store: st, nodeID: "n", interval: interval, clock: clock, log: slog.New(slog.DiscardHandler), image: &image}, &image
}

// TestCacheWriteThrough: without an interval every externalSet is written
// at once (DatasourceCache.ts:161-163); a nil value deletes its key
// (EndpointStore.ts:104-105); the version follows (:151-154).
func TestCacheWriteThrough(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	p, image := newTestPersister(st, 0, &manualClock{})
	image.Lock()
	c := newCache(p, 1, 6, nil, nil)
	c.externalSet(map[uint32]*spec.Node{0: ptr(boolNode(true)), 0xFFFB: ptr(listNode(0, 0xFFFB))}, ptr(uint32(7)))
	image.Unlock()
	values, version, ok := stored(t, st, 1, 6)
	if !ok || version == nil || *version != 7 || !values[0].El.Bool || len(values[0xFFFB].Children) != 2 {
		t.Fatalf("stored %v version %v", values, version)
	}
	image.Lock()
	c.externalSet(map[uint32]*spec.Node{0: nil}, nil)
	c.invalidateVersion()
	c.externalSet(map[uint32]*spec.Node{0xFFFC: ptr(uintNode(1))}, nil)
	image.Unlock()
	values, version, _ = stored(t, st, 1, 6)
	if _, has := values[0]; has || version != nil || values[0xFFFC].El.Uint != 1 {
		t.Fatalf("after delete and invalidation: %v version %v", values, version)
	}
	image.Lock()
	c.erase()
	c.externalSet(map[uint32]*spec.Node{1: ptr(uintNode(1))}, nil)
	image.Unlock()
	if _, _, ok := stored(t, st, 1, 6); ok {
		t.Fatal("an erased cache is still stored, or wrote again")
	}
}

// TestCacheBuffered: with an interval the changes wait for the node's flush
// timer (ClientCacheBuffer.ts:159-173) and are written in one pass
// (:103-153); a failing write keeps its keys for the next cycle
// (DatasourceCache.ts:297-303); stop flushes (:94-101).
func TestCacheBuffered(t *testing.T) {
	t.Parallel()
	st := &failingStore{}
	clock := &manualClock{}
	p, image := newTestPersister(st, 5*time.Second, clock)
	image.Lock()
	a := newCache(p, 1, 6, nil, nil)
	b := newCache(p, 2, 6, nil, nil)
	a.externalSet(map[uint32]*spec.Node{0: ptr(boolNode(true))}, ptr(uint32(1)))
	b.externalSet(map[uint32]*spec.Node{0: ptr(boolNode(false))}, nil)
	a.externalSet(map[uint32]*spec.Node{0: ptr(boolNode(false))}, nil)
	image.Unlock()
	if clock.armed() != 1 {
		t.Fatalf("%d timers armed, want one per node", clock.armed())
	}
	if _, _, ok := stored(t, st, 1, 6); ok {
		t.Fatal("written before the flush")
	}
	st.setFail(true)
	clock.Advance(5 * time.Second)
	if _, _, ok := stored(t, st, 1, 6); ok || clock.armed() != 1 {
		t.Fatalf("a failed flush wrote, or was not re-armed (%d timers)", clock.armed())
	}
	st.setFail(false)
	clock.Advance(5 * time.Second)
	values, version, ok := stored(t, st, 1, 6)
	if !ok || values[0].El.Bool || version == nil || *version != 1 {
		t.Fatalf("after the retry: %v version %v", values, version)
	}
	if _, _, ok := stored(t, st, 2, 6); !ok {
		t.Fatal("the second cache was not flushed")
	}
	saves := st.saves
	clock.Advance(5 * time.Second)
	if st.saves != saves || clock.armed() != 0 {
		t.Fatal("a clean flush wrote again or re-armed")
	}
	image.Lock()
	a.externalSet(map[uint32]*spec.Node{1: ptr(uintNode(3))}, nil)
	b.externalSet(map[uint32]*spec.Node{1: ptr(uintNode(3))}, nil)
	b.discard()
	image.Unlock()
	p.stop()
	if values, _, _ := stored(t, st, 1, 6); values[1].El.Uint != 3 {
		t.Fatal("stop did not flush")
	}
	if values, _, _ := stored(t, st, 2, 6); len(values) != 1 {
		t.Fatal("a discarded cache was flushed")
	}
	image.Lock()
	a.externalSet(map[uint32]*spec.Node{2: ptr(uintNode(3))}, nil)
	image.Unlock()
	if clock.armed() != 0 {
		t.Fatal("a stopped persister armed its timer")
	}
	p.restart()
	if clock.armed() != 1 {
		t.Fatal("restart did not re-arm for the pending change")
	}
}

// TestMemoryStore: Load returns copies in order; the erase paths.
func TestMemoryStore(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	if eps, err := st.Load("n"); err != nil || len(eps) != 0 {
		t.Fatalf("empty store: %v %v", eps, err)
	}
	b, _ := encodeValue(uintNode(9))
	for _, ep := range []uint16{2, 0, 1} {
		for _, c := range []uint32{6, 0x1D} {
			if err := st.Save("n", ep, c, map[uint32][]byte{0: b}, ptr(uint32(ep))); err != nil {
				t.Fatal(err)
			}
		}
	}
	eps, _ := st.Load("n")
	if len(eps) != 3 || eps[0].Number != 0 || eps[2].Number != 2 || eps[1].Clusters[0].ID != 6 || *eps[2].Clusters[1].Version != 2 {
		t.Fatalf("load %+v", eps)
	}
	eps[0].Clusters[0].Values[0][0] = 0xFF
	if again, _ := st.Load("n"); again[0].Clusters[0].Values[0][0] == 0xFF {
		t.Fatal("Load does not copy")
	}
	_ = st.EraseCluster("n", 2, 6)
	_ = st.EraseEndpoint("n", 1)
	eps, _ = st.Load("n")
	if len(eps) != 2 || len(eps[1].Clusters) != 1 {
		t.Fatalf("after the erases %+v", eps)
	}
	_ = st.Save("m", 0, 6, map[uint32][]byte{0: b}, nil)
	if ids, err := st.NodeIDs(); err != nil || !slices.Equal(ids, []string{"m", "n"}) {
		t.Fatalf("NodeIDs %v %v", ids, err)
	}
	_ = st.EraseNode("n")
	if eps, _ := st.Load("n"); len(eps) != 0 {
		t.Fatal("EraseNode left endpoints")
	}
	if ids, _ := st.NodeIDs(); !slices.Equal(ids, []string{"m"}) {
		t.Fatalf("NodeIDs after EraseNode %v", ids)
	}
}

// TestMemoryStoreConcurrent exercises the store from many goroutines (run
// it under -race).
func TestMemoryStoreConcurrent(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	b, _ := encodeValue(uintNode(1))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprint("n", i%2)
			for j := range 50 {
				_ = st.Save(id, uint16(j%3), 6, map[uint32][]byte{uint32(j): b}, nil) //nolint:gosec // a test index
				_, _ = st.Load(id)
				if j%10 == 0 {
					_ = st.EraseEndpoint(id, 1)
				}
			}
		}()
	}
	wg.Wait()
}

// TestSettingsRoundTrip: the settings and the default subscription survive
// their stored form.
func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	floor := 2 * time.Second
	in := settings{disabled: true, autoSubscribe: true, maxEventNumber: 1 << 40, defaultSubscription: &SubscribeOptions{
		Attributes:       []im.ConcreteAttributePath{{Endpoint: 1, HasEndpoint: true, Cluster: 6, HasCluster: true}, {}},
		Events:           []im.ConcreteEventPath{},
		NoFabricFilter:   true,
		MinIntervalFloor: &floor,
	}}
	values, err := in.encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := decodeSettings(values)
	if err != nil {
		t.Fatal(err)
	}
	ds := out.defaultSubscription
	if !out.disabled || !out.autoSubscribe || out.maxEventNumber != 1<<40 || ds == nil || len(ds.Attributes) != 2 ||
		ds.Attributes[0] != in.defaultSubscription.Attributes[0] || ds.Events == nil || len(ds.Events) != 0 ||
		!ds.NoFabricFilter || ds.KeepSubscriptions || *ds.MinIntervalFloor != floor || ds.MaxIntervalCeiling != nil {
		t.Fatalf("round trip %+v %+v", out, ds)
	}
	ev := &SubscribeOptions{Events: []im.ConcreteEventPath{{Node: 1, HasNode: true, Event: 2, HasEvent: true, IsUrgent: true}}, MaxIntervalCeiling: &floor}
	b, _ := encodeSubscribeOptions(ev)
	n, _ := decodeValue(b)
	back, err := decodeSubscribeOptions(n)
	if err != nil || back.Attributes != nil || back.Events[0] != ev.Events[0] || *back.MaxIntervalCeiling != floor {
		t.Fatalf("event paths %+v %v", back, err)
	}
	if _, err := decodeSettings(map[uint32][]byte{settingDisabled: {0x04, 0x01}}); err == nil {
		t.Fatal("a malformed setting decoded")
	}
	if _, err := decodeSettings(map[uint32][]byte{settingDisabled: {0x15}}); err == nil {
		t.Fatal("a truncated setting decoded")
	}
}
