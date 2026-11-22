// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/cluster/spec"
	hepa "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
	"thingfabric.dev/go-fabric/contract"
)

// gateSink holds every write until its gate opens, after telling entered.
type gateSink struct {
	entered chan uint64
	gate    map[uint64]chan struct{}
}

func (k *gateSink) MatterWriteAttribute(_ context.Context, _ uint32, v any) error {
	n, _ := v.(uint32)
	k.entered <- uint64(n)
	<-k.gate[uint64(n)]
	return nil
}

// TestOverlappingWritesLeaveNoCommitOpen (review II, Fable read 5b-write
// R1): two controller writes of one server that start together, whichever
// is released first, each advance the version once, and the server's
// commit window is closed after them: three host Sets advance it three
// times.
func TestOverlappingWritesLeaveNoCommitOpen(t *testing.T) {
	t.Parallel()
	for _, order := range [][2]uint64{{1000, 2000}, {2000, 1000}} {
		sk := &gateSink{entered: make(chan uint64, 2), gate: map[uint64]chan struct{}{1000: make(chan struct{}), 2000: make(chan struct{})}}
		srv := hepaServer(t, spec.ServerConfig{Sink: sk})
		v0 := srv.MatterDataVersion()
		var wg sync.WaitGroup
		for _, v := range []uint64{1000, 2000} {
			wg.Go(func() {
				if _, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, v); err != nil {
					t.Errorf("write %d: %v", v, err)
				}
			})
		}
		// The first write to enter its Sink is released first or last.
		first := <-sk.entered
		if first == order[0] {
			close(sk.gate[order[0]])
			<-sk.entered
			close(sk.gate[order[1]])
		} else {
			close(sk.gate[first])
			<-sk.entered
			close(sk.gate[order[0]])
		}
		wg.Wait()
		if d := srv.MatterDataVersion() - v0; d != 2 {
			t.Errorf("order %v: two writes advanced the version %d times, want 2", order, d)
		}
		v1 := srv.MatterDataVersion()
		for i := range 3 {
			if err := srv.Set(hepa.AttrCondition, uint8(10+i)); err != nil {
				t.Fatal(err)
			}
		}
		if d := srv.MatterDataVersion() - v1; d != 3 {
			t.Errorf("order %v: three host Sets after the writes advanced the version %d times, want 3", order, d)
		}
	}
}

// nestingSink writes the server again, once, with the ctx it was handed.
type nestingSink struct {
	srv    *spec.Server
	nested bool
}

func (k *nestingSink) MatterWriteAttribute(ctx context.Context, _ uint32, _ any) error {
	if k.nested {
		return nil
	}
	k.nested = true
	_, err := k.srv.MatterWriteCommit(ctx, hepa.AttrLastChangedTime, uint64(7))
	return err
}

// TestNestedWriteJoinsTheOpenCommit: a write the Sink makes with the ctx
// it was handed joins the open write's commit — no wait for the window,
// one version advance for both.
func TestNestedWriteJoinsTheOpenCommit(t *testing.T) {
	t.Parallel()
	sk := &nestingSink{}
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	sk.srv = srv
	v0 := srv.MatterDataVersion()
	got, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []uint32{hepa.AttrLastChangedTime}) {
		t.Errorf("the write named %v, want LastChangedTime", got)
	}
	if d := srv.MatterDataVersion() - v0; d != 1 {
		t.Errorf("the nested write advanced the version %d times, want once", d)
	}
	if v, _ := srv.MatterRead(hepa.AttrLastChangedTime); v != uint32(1000) {
		t.Errorf("LastChangedTime = %v, want the outer write's 1000", v)
	}
}

// TestConcurrentWritesAndSets: controller writes and host Sets of one
// server run together under the race detector; afterwards a Set advances
// the version.
func TestConcurrentWritesAndSets(t *testing.T) {
	t.Parallel()
	sk := &siblingSink{}
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	sk.srv = srv
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 20 {
				_, _ = srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000+i*100+j))
			}
		})
		wg.Go(func() {
			for j := range 20 {
				_ = srv.Set(hepa.AttrCondition, uint8(j))
			}
		})
	}
	wg.Wait()
	v := srv.MatterDataVersion()
	c, _ := srv.MatterRead(hepa.AttrCondition)
	n, _ := c.(uint8)
	if err := srv.Set(hepa.AttrCondition, (n+1)%100); err != nil {
		t.Fatal(err)
	}
	if srv.MatterDataVersion() == v {
		t.Fatal("a host Set after the concurrent writes advanced no version")
	}
}

// funcSink runs fn for every write that reaches the Sink.
type funcSink struct {
	fn func(ctx context.Context, attrID uint32, v any) error
}

func (k *funcSink) MatterWriteAttribute(ctx context.Context, attrID uint32, v any) error {
	return k.fn(ctx, attrID, v)
}

// within fails t unless fn returns within the bound: a hang is a failure,
// not a stuck test binary.
func within(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	wait, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-wait.Done():
		t.Fatalf("%s did not return within 10 s", name)
	}
}

// TestListenerWritingTheServerCompletes (Fable read 5b-write S1): a change
// listener that writes the server again, with no ctx of the write it heard,
// completes — the listeners are told after the window is released.
func TestListenerWritingTheServerCompletes(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	var wrote bool
	var inner error
	srv.OnAttributeChange(func(contract.Change) {
		if wrote {
			return
		}
		wrote = true
		_, inner = srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(2000))
	})
	v0 := srv.MatterDataVersion()
	within(t, "the write whose listener writes the server", func() {
		if _, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000)); err != nil {
			t.Error(err)
		}
	})
	if inner != nil {
		t.Fatalf("the listener's write: %v", inner)
	}
	if d := srv.MatterDataVersion() - v0; d != 2 {
		t.Errorf("two writes advanced the version %d times, want 2", d)
	}
}

// TestCrossServerWritesJoin (Fable read 5b-write S2): A's Sink writes B and
// B's Sink writes A, each with the ctx it was handed — the write of A
// inside B's Sink joins A's open commit, and every write completes.
func TestCrossServerWritesJoin(t *testing.T) {
	t.Parallel()
	ka, kb := &funcSink{}, &funcSink{}
	a, b := hepaServer(t, spec.ServerConfig{Sink: ka}), hepaServer(t, spec.ServerConfig{Sink: kb})
	var aNested, bNested bool
	ka.fn = func(ctx context.Context, _ uint32, _ any) error {
		if aNested {
			return nil
		}
		aNested = true
		_, err := b.MatterWriteCommit(ctx, hepa.AttrLastChangedTime, uint64(500))
		return err
	}
	kb.fn = func(ctx context.Context, _ uint32, _ any) error {
		if bNested {
			return nil
		}
		bNested = true
		_, err := a.MatterWriteCommit(ctx, hepa.AttrLastChangedTime, uint64(7))
		return err
	}
	va, vb := a.MatterDataVersion(), b.MatterDataVersion()
	within(t, "A → B → A", func() {
		if _, err := a.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000)); err != nil {
			t.Error(err)
		}
	})
	if da, db := a.MatterDataVersion()-va, b.MatterDataVersion()-vb; da != 1 || db != 1 {
		t.Errorf("versions advanced A %d, B %d; want once each", da, db)
	}
}

// TestCrossServerWritesDoNotDeadlock (Fable read 5b-write S2): two
// servers whose Sinks write each other, written at once from two
// goroutines, each Sink waiting for the other's to start: the second wait
// that would close the cycle is refused, and both writes return.
func TestCrossServerWritesDoNotDeadlock(t *testing.T) {
	t.Parallel()
	ka, kb := &funcSink{}, &funcSink{}
	a, b := hepaServer(t, spec.ServerConfig{Sink: ka}), hepaServer(t, spec.ServerConfig{Sink: kb})
	var started sync.WaitGroup
	started.Add(2)
	cross := func(other *spec.Server, first *sync.Once) func(context.Context, uint32, any) error {
		return func(ctx context.Context, _ uint32, v any) error {
			if n, _ := v.(uint32); n != 1000 {
				return nil
			}
			first.Do(func() { started.Done(); started.Wait() })
			_, err := other.MatterWriteCommit(ctx, hepa.AttrLastChangedTime, uint64(9))
			return err
		}
	}
	var oa, ob sync.Once
	ka.fn, kb.fn = cross(b, &oa), cross(a, &ob)
	var refused int
	var mu sync.Mutex
	within(t, "two writes whose Sinks write each other", func() {
		var wg sync.WaitGroup
		for _, srv := range []*spec.Server{a, b} {
			wg.Go(func() {
				if _, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000)); err != nil {
					mu.Lock()
					refused++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
	})
	if refused != 1 {
		t.Errorf("%d of the two writes refused, want the one whose wait closed the cycle", refused)
	}
}

// TestWindowWaitRules (Fable read 5b-write S3, S4, S5): inside a synctest
// bubble, with a first write held in its Sink — a write whose ctx was kept
// from an earlier, finished write waits for the window instead of joining;
// a second write of the value the first stores announces nothing once it
// enters; and a waiting write ends with its ctx.
func TestWindowWaitRules(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		var mu sync.Mutex
		var kept context.Context
		heard := map[uint32]int{}
		k := &funcSink{fn: func(ctx context.Context, _ uint32, v any) error {
			n, _ := v.(uint32)
			mu.Lock()
			heard[n]++
			if n == 1 {
				kept = ctx
			}
			mu.Unlock()
			if n == 1000 {
				<-gate
			}
			return nil
		}}
		srv := hepaServer(t, spec.ServerConfig{Sink: k})
		if _, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1)); err != nil {
			t.Fatal(err)
		}
		v0 := srv.MatterDataVersion()
		first := make(chan error, 1)
		go func() {
			_, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000))
			first <- err
		}()
		synctest.Wait()
		// S3: the kept ctx joins no later write.
		keptDone := make(chan error, 1)
		go func() { _, err := srv.MatterWriteCommit(kept, hepa.AttrLastChangedTime, uint64(3)); keptDone <- err }()
		// S5: a waiting write ends with its ctx.
		ctx, cancel := context.WithCancel(context.Background())
		cancelled := make(chan error, 1)
		go func() { _, err := srv.MatterWriteCommit(ctx, hepa.AttrLastChangedTime, uint64(4)); cancelled <- err }()
		synctest.Wait()
		select {
		case <-keptDone:
			t.Fatal("a write with a kept ctx joined the open window of another write")
		default:
		}
		cancel()
		synctest.Wait()
		if err := <-cancelled; !errors.Is(err, context.Canceled) {
			t.Errorf("the cancelled wait ended with %v, want context.Canceled", err)
		}
		close(gate)
		for _, ch := range []chan error{first, keptDone} {
			if err := <-ch; err != nil {
				t.Error(err)
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if heard[4] != 0 {
			t.Error("the cancelled write reached the Sink")
		}
		if d := srv.MatterDataVersion() - v0; d != 2 {
			t.Errorf("the version advanced %d times, want the first write's and the kept-ctx write's", d)
		}
	})
}

// TestSecondWriteOfTheSameValueAnnouncesNothing (Fable read 5b-write S4):
// a write of the value another write is storing waits for the window and
// then finds the value held: its Sink is not called, it names nothing and
// advances nothing (matter.js notes no field whose value is equal,
// Datasource.ts:1476-1481 at db021da0).
func TestSecondWriteOfTheSameValueAnnouncesNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		var mu sync.Mutex
		calls := 0
		k := &funcSink{fn: func(context.Context, uint32, any) error {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				<-gate
			}
			return nil
		}}
		srv := hepaServer(t, spec.ServerConfig{Sink: k})
		v0 := srv.MatterDataVersion()
		first := make(chan error, 1)
		go func() {
			_, err := srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000))
			first <- err
		}()
		synctest.Wait()
		var second []uint32
		secondDone := make(chan error, 1)
		go func() {
			var err error
			second, err = srv.MatterWriteCommit(context.Background(), hepa.AttrLastChangedTime, uint64(1000))
			secondDone <- err
		}()
		synctest.Wait()
		close(gate)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-secondDone; err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if calls != 1 || len(second) != 0 {
			t.Errorf("the Sink was called %d times and the second write named %v; want once and nothing", calls, second)
		}
		if d := srv.MatterDataVersion() - v0; d != 1 {
			t.Errorf("the version advanced %d times, want once", d)
		}
	})
}
