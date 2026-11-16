// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
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
