// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// origins records the origins of the OnOff changes a callback saw.
type origins struct {
	mu   sync.Mutex
	seen []contract.Origin
}

func (o *origins) add(c contract.Change) {
	if !slices.Contains(c.Attributes, onoffdef.AttrOnOff) && !slices.Contains(c.Attributes, onoffdef.AttrOnTime) &&
		!slices.Contains(c.Attributes, lvldef.AttrOnLevel) {
		return
	}
	o.mu.Lock()
	o.seen = append(o.seen, c.Origin)
	o.mu.Unlock()
}

func (o *origins) get() []contract.Origin {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.seen)
}

// TestOnChangedReportsTheOrigin: the typed OnChanged and Device.OnChanged
// report a controller's write, a command and the host's Set each with its
// origin, inline — the callback ran before the controller's request
// returned (brief BU §3; matter.js hands each $Changed observer the
// session of the change, Datasource.ts:1473-1476).
func TestOnChangedReportsTheOrigin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	typed, untyped, level := &origins{}, &origins{}, &origins{}
	light.OnOff().OnChanged(func(_ onoffdef.State, c contract.Change) { typed.add(c) })
	light.OnChanged(onoffdef.ClusterID, untyped.add)
	light.LevelControl().OnChanged(func(_ lvldef.State, c contract.Change) { level.add(c) })
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, _ := light.EndpointNumber()

	// A controller's write, a command, the host's Set.
	if err := fabrictest.Write(ctx, pair, attrPath(ep, onoffdef.ClusterID, onoffdef.AttrOnTime), uintValue(9)); err != nil {
		t.Fatal(err)
	}
	if got := typed.get(); len(got) != 1 {
		t.Fatalf("after the write returned the callback ran %d times, want once (inline)", len(got))
	}
	on := im.ConcreteCommandPath{Endpoint: ep, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdOn}
	if _, err := fabrictest.Invoke(ctx, pair, on, nil); err != nil {
		t.Fatal(err)
	}
	if got := typed.get(); len(got) != 2 {
		t.Fatalf("after the command returned the callback ran %d times, want twice (inline)", len(got))
	}
	if err := pair.Device.Act(func(*node.Node) error { return light.OnOff().Set(onoffdef.Patch{OnOff: new(false)}) }); err != nil {
		t.Fatal(err)
	}
	want := []contract.Origin{contract.OriginControllerWrite, contract.OriginCommand, contract.OriginHostSet}
	if got := typed.get(); !slices.Equal(got, want) {
		t.Errorf("typed OnChanged origins %v, want %v", got, want)
	}
	if got := untyped.get(); !slices.Equal(got, want) {
		t.Errorf("Device.OnChanged origins %v, want %v", got, want)
	}
	// The kit's LevelControl: a write and the host's Set.
	if err := fabrictest.Write(ctx, pair, attrPath(ep, lvldef.ClusterID, lvldef.AttrOnLevel), uintValue(10)); err != nil {
		t.Fatal(err)
	}
	if err := pair.Device.Act(func(*node.Node) error { return light.LevelControl().Set(lvldef.Patch{OnLevel: new(uint8(20))}) }); err != nil {
		t.Fatal(err)
	}
	if got, want := level.get(), []contract.Origin{contract.OriginControllerWrite, contract.OriginHostSet}; !slices.Equal(got, want) {
		t.Errorf("LevelControl origins %v, want %v", got, want)
	}
}

// TestOnChangedAsyncDrainsAtStop: the asynchronous variants queue on the
// device's goroutine, in order, and the node's Stop returns once every
// queued callback ran (ADR 0026 principle 5).
func TestOnChangedAsyncDrainsAtStop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var typed, untyped []bool
	light.OnOff().OnChangedAsync(func(s onoffdef.State, _ contract.Change) {
		mu.Lock()
		typed = append(typed, s.OnOff)
		mu.Unlock()
	})
	light.OnChangedAsync(onoffdef.ClusterID, func(c contract.Change) {
		mu.Lock()
		untyped = append(untyped, c.Origin == contract.OriginHostSet)
		mu.Unlock()
	})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	for _, on := range []bool{true, false, true} {
		if err := light.OnOff().Set(onoffdef.Patch{OnOff: new(on)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := pair.Device.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(typed, []bool{true, false, true}) || !slices.Equal(untyped, []bool{true, true, true}) {
		t.Errorf("after Stop the queue delivered %v and %v, want every change in order", typed, untyped)
	}
	// A server wrapped outside a device has no queue: it delivers nothing.
	onoffdef.Wrap(light.Server(onoffdef.ClusterID)).OnChangedAsync(func(onoffdef.State, contract.Change) {
		t.Error("OnChangedAsync without the device's queue delivered")
	})
	if err := light.OnOff().Set(onoffdef.Patch{OnOff: new(true)}); err != nil {
		t.Fatal(err)
	}
}
