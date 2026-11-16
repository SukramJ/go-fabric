// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/contract"
)

// blockFirstChange holds the first change notification of srv until
// release is closed and reports, on entered, that it is held: a change
// still on its way to the store while a second one runs.
func blockFirstChange(srv contract.AttributeChangeNotifier) (entered, release chan struct{}) {
	var first atomic.Bool
	first.Store(true)
	entered, release = make(chan struct{}), make(chan struct{})
	srv.OnAttributeChange(func(contract.Change) {
		if first.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
	})
	return entered, release
}

// settle yields until done holds a value or a bounded number of yields
// passed: a second change that is not serialised behind the held first one
// completes meanwhile, one that is waits — without a wall-clock wait.
func settle[T any](done chan T) {
	for range 10000 {
		if len(done) > 0 {
			return
		}
		runtime.Gosched()
	}
}

// TestProviderChangesDoNotLoseEachOther interleaves two changes of the
// session table: a removal held between its publication and its commit,
// and a new session stored meanwhile. Each change applies to the table
// the one before it left, so the table and CurrentSessions both end with
// session 2 only. Before, each change cloned the table, released the lock
// and stored its copy: the removal's copy overwrote the new session in the
// table while the attribute kept both (review II, R2-P6-conc-1601).
func TestProviderChangesDoNotLoseEachOther(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t, nil)
	video := []uint16{1}
	if err := p.UpsertSession(camera.ProviderSession{Id: 1, FabricIndex: 1, PeerNodeId: 1, VideoStreams: &video}); err != nil {
		t.Fatal(err)
	}
	entered, release := blockFirstChange(p)
	removed := make(chan struct{})
	go func() { p.RemoveSession(1); close(removed) }() // fabric 1's EndSession
	<-entered
	stored := make(chan error, 1)
	go func() { // fabric 2's SolicitOffer, while the removal is on its way
		stored <- p.UpsertSession(camera.ProviderSession{Id: 2, FabricIndex: 2, PeerNodeId: 2, VideoStreams: &video})
	}()
	settle(stored)
	close(release)
	<-removed
	if err := <-stored; err != nil {
		t.Fatal(err)
	}
	if s := p.Sessions(); len(s) != 1 || s[0].Id != 2 {
		t.Errorf("Sessions() = %+v, want session 2 only", s)
	}
	attr, _ := p.MatterRead(wpdef.AttrCurrentSessions)
	if l, ok := attr.(spec.List[camera.ProviderSession]); !ok || len(l) != 1 || l[0].Id != 2 {
		t.Errorf("CurrentSessions = %#v, want session 2 only", attr)
	}
}
