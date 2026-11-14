// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/store"
)

// notifyingCounterServer is counterServer that also reports its own
// change, as a server keeping its own attribute state does: through
// OnAttributeChange (origin OriginCommand inside its command) or, with
// valueChanged, through OnMatterValueChanged.
type notifyingCounterServer struct {
	counterServer
	mu  sync.Mutex
	cbs []func(contract.Change)
}

func (s *notifyingCounterServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cbs = append(s.cbs, cb)
	return func() {}
}

func (s *notifyingCounterServer) fire(origin contract.Origin) {
	s.mu.Lock()
	cbs := append([]func(contract.Change){}, s.cbs...)
	s.mu.Unlock()
	for _, cb := range cbs {
		cb(contract.Change{Attributes: []uint32{0}, Origin: origin})
	}
}

func (s *notifyingCounterServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	s.n.Add(1)
	s.fire(contract.OriginCommand)
	return nil, nil
}

// valueNotifyingCounterServer reports its change through
// [contract.ChangeNotifier].
type valueNotifyingCounterServer struct {
	counterServer
	mu  sync.Mutex
	cbs []func()
}

func (s *valueNotifyingCounterServer) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cbs = append(s.cbs, cb)
	return func() {}
}

func (s *valueNotifyingCounterServer) fire() {
	s.mu.Lock()
	cbs := append([]func(){}, s.cbs...)
	s.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

func (s *valueNotifyingCounterServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	s.n.Add(1)
	s.fire()
	return nil, nil
}

// TestCommandAdvancesTheDataVersionOnce (review II, R2-P2-dr-req-03): a
// command is one commit and advances its cluster's DataVersion once,
// whether the server reports its own change during the command, reports
// it as a value change, or reports nothing and the before/after comparison
// finds it (matter.js Datasource.ts:1440-1448, advanceVersion once per
// commit). A change the server reports outside a command still advances
// the version by itself, once.
func TestCommandAdvancesTheDataVersionOnce(t *testing.T) {
	t.Parallel()
	type server struct {
		name    string
		srv     contract.ClusterServer
		n       func() uint32
		outside func() // a change the server reports outside a command; nil: none
	}
	compared := &counterServer{}
	attribute := &notifyingCounterServer{}
	value := &valueNotifyingCounterServer{}
	for _, tc := range []server{
		{"compared", compared, compared.n.Load, nil},
		{"attribute change", attribute, attribute.n.Load, func() { attribute.n.Add(1); attribute.fire(contract.OriginHostSet) }},
		{"value change", value, value.n.Load, func() { value.n.Add(1); value.fire() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
				return nil, []contract.ClusterServer{tc.srv}
			})
			h.allowAll()
			h.bridge.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, (&reachAttrReporterSpy{}).report, nil))
			root := h.bridge.Topology().FindByID(0)
			cluster := tc.srv.MatterClusterID()
			before := root.ClusterDataVersion(cluster)
			h.invoke(0, cluster, 0, nil)
			if tc.n() != 1 {
				t.Fatalf("the command ran %d times, want 1", tc.n())
			}
			if after := root.ClusterDataVersion(cluster); after-before != 1 {
				t.Fatalf("the command advanced the DataVersion by %d (%d → %d), want 1", after-before, before, after)
			}
			if tc.outside == nil {
				return
			}
			before = root.ClusterDataVersion(cluster)
			tc.outside()
			if after := root.ClusterDataVersion(cluster); after-before != 1 {
				t.Fatalf("a change outside a command advanced the DataVersion by %d, want 1", after-before)
			}
		})
	}
}
