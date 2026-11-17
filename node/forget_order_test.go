// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"testing"

	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// erasingHook is a credential store that calls onDelete once after the
// first DeleteSettings it served.
type erasingHook struct {
	store.Port
	onDelete func()
}

func (s *erasingHook) DeleteSettings(ctx context.Context, keys ...string) error {
	err := s.Port.DeleteSettings(ctx, keys...)
	if f := s.onDelete; f != nil {
		s.onDelete = nil
		f()
	}
	return err
}

// TestForgetErasesAfterTheEndpointLeftTheWire (review II, R2-P6-conc-2405,
// N-finp-3): Forget erases what is stored under a key only after the
// reassembly took its endpoint off the wire, so a controller's write
// that arrives at the moment of the erasure — here a NodeLabel write,
// stored through the endpoint's state — no longer reaches the endpoint
// and stores nothing under the erased key; an Add of the key afterwards
// starts without the old label.
func TestForgetErasesAfterTheEndpointLeftTheWire(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const key = "switch:1"
	st := &erasingHook{Port: memstore.New()}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n := newNode(t, cfg)
	build := func() node.Endpoint {
		sw, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey(key), Name: key})
		if err != nil {
			t.Fatal(err)
		}
		return sw
	}
	if err := n.Aggregator().Add(ctx, build()); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ep, ok := endpointOf(t, n, key)
	if !ok {
		t.Fatal("no endpoint")
	}
	path := attrPath(ep, bdbidef.ClusterID, bdbidef.AttrNodeLabel)
	if got := dispatchWrite(t, n, path, "before"); got != im.StatusSuccess {
		t.Fatalf("NodeLabel write: %v", got)
	}
	var late im.StatusCode
	st.onDelete = func() {
		res := endpoint.NewTopologyDispatcher(n.Bridge().Topology()).Write(ctx, path, im.AttributeValue{Value: "late"})
		if len(res) == 1 {
			late = res[0].Status
		}
	}
	if err := n.Aggregator().Forget(ctx, endpoint.StringKey(key)); err != nil {
		t.Fatal(err)
	}
	if late == im.StatusSuccess {
		t.Errorf("a NodeLabel write at the erasure answered SUCCESS: the forgotten endpoint was still served")
	}
	if v, ok, err := st.GetSetting(ctx, "bridged_node_label."+key); err != nil || ok && v != "" {
		t.Errorf("stored NodeLabel after Forget: %q (found %v, %v), want none", v, ok, err)
	}
	if err := n.Aggregator().Add(ctx, build()); err != nil {
		t.Fatal(err)
	}
	ep, _ = endpointOf(t, n, key)
	if got := dispatchRead(t, n, attrPath(ep, bdbidef.ClusterID, bdbidef.AttrNodeLabel)); got == "late" || got == "before" {
		t.Errorf("NodeLabel %q after Forget and Add, want the declaration's", got)
	}
}

// TestForgetAndAddInOneBatchStartsNew (review II, R2-P6-conc-2405): a key
// forgotten and added again in one batch is served after the erasure —
// a fresh endpoint number and none of the forgotten device's stored
// state — as a Forget and a later Add are.
func TestForgetAndAddInOneBatchStartsNew(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const key = "switch:1"
	st := memstore.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n := newNode(t, cfg)
	build := func() node.Endpoint {
		sw, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey(key), Name: key})
		if err != nil {
			t.Fatal(err)
		}
		return sw
	}
	if err := n.Aggregator().Add(ctx, build()); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := endpointOf(t, n, key)
	if got := dispatchWrite(t, n, attrPath(before, bdbidef.ClusterID, bdbidef.AttrNodeLabel), "old"); got != im.StatusSuccess {
		t.Fatalf("NodeLabel write: %v", got)
	}
	err := n.Aggregator().Partition(node.Scope).Batch(ctx, func(b *node.Batch) error {
		if err := b.Forget(endpoint.StringKey(key)); err != nil {
			return err
		}
		return b.Add(build())
	})
	if err != nil {
		t.Fatal(err)
	}
	after, ok := endpointOf(t, n, key)
	if !ok {
		t.Fatal("the key added again in the batch is not served")
	}
	if after == before {
		t.Errorf("endpoint %d after Forget and Add in one batch, want a fresh number", after)
	}
	if got := dispatchRead(t, n, attrPath(after, bdbidef.ClusterID, bdbidef.AttrNodeLabel)); got == "old" {
		t.Errorf("NodeLabel %q after Forget and Add in one batch, want the declaration's", got)
	}
}
