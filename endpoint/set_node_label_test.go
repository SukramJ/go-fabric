// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	bdbidef "thingfabric.dev/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	"thingfabric.dev/go-fabric/contract"
)

// labelStore is a [Persistence] that records the label it stored last
// and refuses while refuse is set.
type labelStore struct {
	mu     sync.Mutex
	refuse error
	last   string
	saves  int
}

func (s *labelStore) SaveNodeLabel(_ context.Context, _ SourceKey, label string) error {
	s.mu.Lock()
	refuse := s.refuse
	s.mu.Unlock()
	if refuse != nil {
		return refuse
	}
	// Widen the window between a write's store and its set, so an
	// unserialised pair of writes interleaves.
	runtime.Gosched()
	s.mu.Lock()
	s.last = label
	s.saves++
	s.mu.Unlock()
	return nil
}

func (*labelStore) LoadScenes(context.Context, SourceKey) ([]byte, error) { return nil, nil }

func (*labelStore) SaveScenes(context.Context, SourceKey, []byte) error { return nil }

func (s *labelStore) stored() (last string, saves int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.saves
}

// TestSetNodeLabelReturnsTheStoresRefusal (R2-P5-apidev-16, decision
// 226): the host's rename is stored before it is served; a refused store
// serves nothing and reaches the caller.
func TestSetNodeLabelReturnsTheStoresRefusal(t *testing.T) {
	t.Parallel()
	refused := errors.New("store refused")
	p := &labelStore{refuse: refused}
	e := &Endpoint{FriendlyName: "spec", SourceKey: StringKey("k"), persistence: p}
	if err := e.SetNodeLabel("renamed"); !errors.Is(err, refused) {
		t.Fatalf("SetNodeLabel = %v, want the store's refusal", err)
	}
	if got := e.NodeLabel(); got != "spec" {
		t.Errorf("a refused rename is served: %q", got)
	}
	p.mu.Lock()
	p.refuse = nil
	p.mu.Unlock()
	if err := e.SetNodeLabel("renamed"); err != nil {
		t.Fatalf("SetNodeLabel: %v", err)
	}
	if got, saves := p.stored(); got != "renamed" || saves != 1 || e.NodeLabel() != "renamed" {
		t.Errorf("stored %q (%d saves), served %q; want one save of %q, served", got, saves, e.NodeLabel(), "renamed")
	}
}

// bdbiOf is the BridgedDeviceBasicInformation server a dispatch builds
// for ep: a new instance per call, as the dispatcher builds one per
// dispatch.
func bdbiOf(t *testing.T, ep *Endpoint) *core.BridgedDeviceBasicInformation {
	t.Helper()
	for _, srv := range ClusterServers(ep) {
		if b, ok := srv.(*core.BridgedDeviceBasicInformation); ok {
			return b
		}
	}
	t.Fatal("no BridgedDeviceBasicInformation server")
	return nil
}

// labelEndpoint is a bridged endpoint with a NodeLabel persistence.
func labelEndpoint(p Persistence) *Endpoint {
	return &Endpoint{
		ID: 3, DeviceType: 0x0100, FriendlyName: "spec", SourceKey: StringKey("k"), persistence: p,
		Source: deviceTypeSource{dt: 0x0100, servers: []contract.ClusterServer{dtServer{id: 0x0006}}},
	}
}

// TestNodeLabelWritesOnTwoServerInstances (F-db021da0-43): two server
// instances of one endpoint — built per dispatch — compare a controller's
// NodeLabel write with the label the endpoint serves under its lock, not
// with the label each instance was built with: a second write of the same
// label changes nothing and stores nothing, and a write back to the label
// both were built with changes it. Mirrors matter.js, whose write locks
// the endpoint's datasource and compares with the held values
// (Datasource.ts:1345-1350, :1446-1457, :1476-1481 at db021da0).
func TestNodeLabelWritesOnTwoServerInstances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := &labelStore{}
	ep := labelEndpoint(p)
	a, b := bdbiOf(t, ep), bdbiOf(t, ep)
	if changed, err := a.MatterWriteChanged(ctx, bdbidef.AttrNodeLabel, "A"); err != nil || !changed {
		t.Fatalf("first write: changed %v, %v", changed, err)
	}
	if changed, err := b.MatterWriteChanged(ctx, bdbidef.AttrNodeLabel, "A"); err != nil || changed {
		t.Errorf("the same label on the second instance: changed %v, %v; want unchanged", changed, err)
	}
	if _, saves := p.stored(); saves != 1 {
		t.Errorf("%d saves for two writes of one label, want 1", saves)
	}
	if changed, err := b.MatterWriteChanged(ctx, bdbidef.AttrNodeLabel, "spec"); err != nil || !changed {
		t.Errorf("a write back to the built-with label: changed %v, %v; want changed", changed, err)
	}
	if last, _ := p.stored(); last != "spec" || ep.NodeLabel() != "spec" {
		t.Errorf("stored %q, served %q; want spec", last, ep.NodeLabel())
	}
}

// TestNodeLabelServedIsStoredLast (R2-P5-apidev-16, decision 226): host
// renames and controller writes (the server's saver hook) running at once
// end with the label served equal to the label stored last.
func TestNodeLabelServedIsStoredLast(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for round := range 50 {
		p := &labelStore{}
		ep := labelEndpoint(p)
		var wg sync.WaitGroup
		for i := range 8 {
			label := fmt.Sprintf("label-%d", i)
			wg.Go(func() {
				if i%2 == 0 {
					if err := ep.SetNodeLabel(label); err != nil {
						t.Errorf("SetNodeLabel: %v", err)
					}
					return
				}
				// A controller's write, on a server instance of its own.
				if _, err := bdbiOf(t, ep).MatterWriteChanged(ctx, bdbidef.AttrNodeLabel, label); err != nil {
					t.Errorf("write: %v", err)
				}
			})
		}
		wg.Wait()
		if stored, _ := p.stored(); ep.NodeLabel() != stored {
			t.Fatalf("round %d: served %q, stored %q", round, ep.NodeLabel(), stored)
		}
	}
}
