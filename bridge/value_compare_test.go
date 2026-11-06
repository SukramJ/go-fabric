// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// freshStructServer answers attribute 0 with a struct allocated per read,
// its fields nested pointers — the shape a server returns that builds its
// state view on demand (ClosureControl's OverallCurrentState).
type freshStructServer struct {
	mu    sync.Mutex
	latch bool
}

func (s *freshStructServer) MatterClusterID() uint32 { return 0x0104 }

func (s *freshStructServer) MatterRead(attrID uint32) (any, bool) {
	if attrID != 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	latch, secure := s.latch, false
	return &wire.ClosureOverallCurrentState{Latch: &latch, SecureState: &secure, HasLatch: true}, true
}

func (s *freshStructServer) MatterWrite(context.Context, uint32, any) error { return nil }

func (s *freshStructServer) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	if cmdID == 1 {
		s.mu.Lock()
		s.latch = !s.latch
		s.mu.Unlock()
	}
	return nil, nil
}

func (s *freshStructServer) MatterReportable() []uint32 { return []uint32{0} }

var _ contract.ClusterServer = (*freshStructServer)(nil)

// freshStructDispatcher reads attribute 0 of the server it wraps and
// invokes it, the dispatcher's path for a bridged cluster.
type freshStructDispatcher struct{ srv *freshStructServer }

func (d freshStructDispatcher) Read(_ context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	v, _ := d.srv.MatterRead(0)
	return []im.ReadResult{{Path: im.ConcreteAttributePath{
		Endpoint: p.Endpoint, Cluster: p.Cluster, Attribute: 0,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}, Value: im.AttributeValue{Value: v}, Status: im.StatusSuccess}}
}

func (d freshStructDispatcher) Write(context.Context, im.ConcreteAttributePath, im.AttributeValue) []im.WriteResult {
	return nil
}

func (d freshStructDispatcher) Invoke(ctx context.Context, p im.ConcreteCommandPath, f any) im.InvokeResult {
	_, _ = d.srv.MatterInvoke(ctx, p.Command, f)
	return im.InvokeResult{Path: p, Status: im.StatusSuccess}
}

// TestInvokeChangesCompareByValue: a server that answers every read with a
// fresh struct of nested pointers reports no change after a command that
// left the value alone, and reports a changed nested field. Mirrors
// matter.js's Datasource, which notes a property only when the committed
// value is not isDeepEqual to the old one (Datasource.ts #noteIfChanged).
// Found by TC-CLCTRL-5.1 (#419): step 4i matched a stale Latch report.
func TestInvokeChangesCompareByValue(t *testing.T) {
	t.Parallel()
	ep := &endpointpkg.Endpoint{ID: 7, DeviceType: 0x0230, SourceKey: endpointpkg.StringKey("c"), Scope: "s", DeviceAddress: "d"}
	b := newReachabilityBridge(t, ep)
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	state := im.ConcreteAttributePath{Endpoint: 7, Cluster: 0x0104, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{state},
	}); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)
	d := freshStructDispatcher{srv: &freshStructServer{}}

	invoke := func(cmd uint32) int {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		req := im.InvokeRequest{Invokes: []im.CommandInvocation{{
			Path: im.ConcreteCommandPath{Endpoint: 7, Cluster: 0x0104, Command: cmd, HasEndpoint: true, HasCluster: true, HasCommand: true},
		}}}
		before := b.snapshotInvokedClusters(ctx, d, req)
		_ = im.HandleInvokeRequest(ctx, d, req)
		b.reportInvokeChanges(ctx, d, before)
		mgr.Tick(ctx, time.Now().Add(2*time.Second))
		spy.mu.Lock()
		defer spy.mu.Unlock()
		n := len(spy.calls)
		spy.calls = nil
		return n
	}
	if n := invoke(0); n != 0 {
		t.Fatalf("a command that changed nothing reported %d times", n)
	}
	if n := invoke(1); n != 1 {
		t.Fatalf("a command that moved the nested Latch reported %d times, want 1", n)
	}
}

// TestPathValuesCompareByValue: a notifier's change filter treats two
// fresh structs of equal content as unchanged and a moved nested field as
// changed.
func TestPathValuesCompareByValue(t *testing.T) {
	t.Parallel()
	srv := &freshStructServer{}
	ep := &endpointpkg.Endpoint{ID: 5}
	ep.PublishClusterServers([]contract.ClusterServer{srv})
	state := im.ConcreteAttributePath{Endpoint: 5, Cluster: 0x0104, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	v := newPathValues(ep, []im.ConcreteAttributePath{state})
	if got := v.changed(); len(got) != 0 {
		t.Fatalf("nothing moved: %v reported", got)
	}
	_, _ = srv.MatterInvoke(t.Context(), 1, nil)
	if got := v.changed(); len(got) != 1 || got[0] != state {
		t.Fatalf("the nested Latch moved: %v, want it reported", got)
	}
	if got := v.changed(); len(got) != 0 {
		t.Fatalf("after the report: %v, want quiet", got)
	}
}

// unencodable is a type the attribute writer does not know: it goes on the
// wire as null, so the comparison falls back to the Go value.
type unencodable struct{ n *int }

// TestAttributeReadingEqual pins the comparison: wire form first, the Go
// value behind a null.
func TestAttributeReadingEqual(t *testing.T) {
	t.Parallel()
	one, otherOne, two := 1, 1, 2
	cases := []struct {
		name string
		a, b im.AttributeValue
		want bool
	}{
		{"same scalar", im.AttributeValue{Value: uint16(3)}, im.AttributeValue{Value: uint16(3)}, true},
		{"moved scalar", im.AttributeValue{Value: uint16(3)}, im.AttributeValue{Value: uint16(4)}, false},
		{"null both ways", im.AttributeValue{IsNull: true}, im.AttributeValue{Value: nil}, true},
		{"null against a value", im.AttributeValue{IsNull: true}, im.AttributeValue{Value: uint8(0)}, false},
		{"unknown type, equal pointee", im.AttributeValue{Value: unencodable{&one}}, im.AttributeValue{Value: unencodable{&otherOne}}, true},
		{"unknown type, moved pointee", im.AttributeValue{Value: unencodable{&one}}, im.AttributeValue{Value: unencodable{&two}}, false},
		{"unknown type against null", im.AttributeValue{Value: &unencodable{&one}}, im.AttributeValue{IsNull: true}, false},
	}
	for _, c := range cases {
		if got := readingOf(c.a).equal(readingOf(c.b)); got != c.want {
			t.Errorf("%s: equal = %v, want %v", c.name, got, c.want)
		}
	}
	if derefValue((*int)(nil)) != nil || derefValue(nil) != nil {
		t.Fatal("derefValue of nil")
	}
}
