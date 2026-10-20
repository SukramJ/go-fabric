// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// TestDeviceClientStackOnTheSite: two devices commissioned onto one
// controller's fabric reach each other over CASE on the site. Device A's
// client stack (node.Client, on the site's network and DNS-SD) opens a
// session to device B and toggles B's light, which B's access control
// grants A's node id Operate on.
func TestDeviceClientStackOnTheSite(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	c := site.AddController(fabrictest.ControllerOptions{})
	a := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) { cfg.Client.Enabled = true }})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light:1"), Name: "light", Clock: site.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	b := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{light}})
	if _, err := c.Commission(ctx, a, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Commission(ctx, b, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	if b.Peers() != nil {
		t.Fatal("device B has a client stack without node.Client.Enabled")
	}
	fa := onlyFabric(ctx, t, a)
	fb := onlyFabric(ctx, t, b)
	if err := b.Store().ReplaceACL(ctx, fb.FabricIndex, []store.ACLEntry{
		{FabricIndex: fb.FabricIndex, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{bridgetest.DefaultControllerNodeID}},
		{FabricIndex: fb.FabricIndex, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE, Subjects: []uint64{fa.NodeID}},
	}); err != nil {
		t.Fatal(err)
	}
	ep, ok := b.EndpointOf("light:1")
	if !ok {
		t.Fatal("the light has no endpoint")
	}
	p, err := a.Peers().For(handshake.PeerAddress{FabricIndex: fa.FabricIndex, NodeID: fb.NodeID})
	if err != nil {
		t.Fatal(err)
	}
	ia := interaction.ForPeer(p, interaction.Config{Clock: site.Clock()})
	err = site.Run(ctx, func(ctx context.Context) error {
		_, err := ia.Invoke(ctx, im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}, nil, interaction.InvokeOptions{})
		return err
	})
	if err != nil {
		t.Fatalf("device A's Toggle on device B: %v", err)
	}
	if v, _ := light.OnOff().Underlying().MatterRead(onoff.AttrOnOff); v != true {
		t.Fatalf("B's light OnOff %v after A's Toggle", v)
	}
}

// onlyFabric is the device's one fabric.
func onlyFabric(ctx context.Context, t *testing.T, d *fabrictest.Device) node.FabricInfo {
	t.Helper()
	fabrics, err := d.Node().FabricInfos(ctx)
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("device %d fabrics %+v, %v", d.Index(), fabrics, err)
	}
	return fabrics[0]
}
