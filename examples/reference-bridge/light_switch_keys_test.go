// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/binding"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/im"
)

// TestLightSwitchEntryKeys: a binding entry's key names every field that
// makes it a distinct target, an absent field as "" (entryKey,
// LightSwitchTestInstance.ts:65-68 at 862ad75f), and the server's
// generated entry and the node's resolved entry key alike.
func TestLightSwitchEntryKeys(t *testing.T) {
	t.Parallel()
	node, ep, cl := uint64(5), uint16(0), uint32(6)
	group := uint16(0x1234)
	cases := []struct {
		name string
		res  mattercore.TargetStruct
		def  bindingdef.TargetStruct
		want string
	}{
		{
			"node with endpoint 0 and cluster",
			mattercore.TargetStruct{FabricIndex: 1, Node: node, Endpoint: ep, EndpointPresent: true, Cluster: cl},
			bindingdef.TargetStruct{FabricIndex: 1, Node: &node, Endpoint: &ep, Cluster: &cl},
			"1/5//0/6",
		},
		{
			"group alone",
			mattercore.TargetStruct{FabricIndex: 2, Group: group},
			bindingdef.TargetStruct{FabricIndex: 2, Group: &group},
			"2//4660//",
		},
	}
	for _, c := range cases {
		if got := entryKey(c.res); got != c.want {
			t.Errorf("%s: entryKey = %q, want %q", c.name, got, c.want)
		}
		if got := defEntryKey(c.def); got != c.want {
			t.Errorf("%s: defEntryKey = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestLightSwitchSendRefusals: the switch refuses a send before its
// endpoint is mounted, refuses to resolve without the node's binding
// manager, and sends to no resolution of a kind it does not know.
func TestLightSwitchSendRefusals(t *testing.T) {
	t.Parallel()
	sw, err := newDemoLightSwitch(newFleet(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sw.sendOnOff(ctx, 1, "on"); err == nil || !strings.Contains(err.Error(), "no endpoint") {
		t.Errorf("send before the endpoint is mounted: %v", err)
	}
	if _, err := sw.resolveTargets(ctx, 1); err == nil || !strings.Contains(err.Error(), "binding manager") {
		t.Errorf("resolve without a binding manager: %v", err)
	}
	if err := sendTo(ctx, binding.Resolution{}, onoff.CmdOn); err == nil {
		t.Error("a resolution of no kind was sent to")
	}
	attachLightSwitch(nil, nil)

	// With the manager attached: an entry the manager resolved is a target;
	// one it has not resolved yet is waited for, until the context ends.
	b, ok := sw.dev.Server(bindingdef.ClusterID).(*mattercore.Binding)
	if !ok {
		t.Fatal("the switch serves no Binding")
	}
	fabricCtx := im.WithFabricFilter(ctx, true, 1)
	entry := mattercore.TargetStruct{Node: 0x99, Endpoint: 1, FabricIndex: 1}
	if err := b.MatterWrite(im.WithListAppendWrite(fabricCtx), bindingdef.AttrBinding, []mattercore.TargetStruct{entry}); err != nil {
		t.Fatal(err)
	}
	sw.mu.Lock()
	sw.attached = true
	sw.resolved[resolutionKey{1, entryKey(entry)}] = binding.Resolution{Kind: binding.KindClient, Entry: entry, SourceEndpoint: 1}
	sw.mu.Unlock()
	targets, err := sw.resolveTargets(ctx, 1)
	if err != nil || len(targets) != 1 || targets[0].Entry != entry {
		t.Errorf("resolveTargets with the entry resolved: %v, %v", targets, err)
	}
	sw.mu.Lock()
	delete(sw.resolved, resolutionKey{1, entryKey(entry)})
	sw.mu.Unlock()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := sw.resolveTargets(cancelled, 1); err == nil {
		t.Error("resolveTargets with the entry unresolved returned before the context ended")
	}
}
