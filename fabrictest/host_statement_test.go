// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	bridgedinfodef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestHostStatementName is #238: the host's name changes on the host's
// side and reaches the controller without becoming the persisted
// NodeLabel. A controller's label, written later, wins over every later
// host name, after a restart too; the device stays bound throughout.
func TestHostStatementName(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	plug, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug"), Name: "Kitchen"})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{plug}}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ep, ok := plug.EndpointNumber()
	if !ok {
		t.Fatal("the plug has no endpoint number")
	}
	label := func() string {
		t.Helper()
		v, err := fabrictest.ReadCluster(ctx, pair, ep, bridgedinfodef.ClusterID)
		if err != nil {
			t.Fatal(err)
		}
		st, _ := bridgedinfodef.StateFrom(v)
		return st.NodeLabel
	}
	if got := label(); got != "Kitchen" {
		t.Fatalf("NodeLabel %q, want Kitchen", got)
	}

	// The host renames the device on its side: served at once, and the
	// device keeps its binding.
	plug.Update(ctx, func(s *device.HostStatement) { s.Name = "Kitchen plug" })
	if got := label(); got != "Kitchen plug" {
		t.Errorf("after the host's name: NodeLabel %q, want Kitchen plug", got)
	}
	if n, ok := plug.EndpointNumber(); !ok || n != ep {
		t.Errorf("after Update: endpoint %d %v, want %d", n, ok, ep)
	}
	plug.SetReachable(ctx, false)
	if plug.Reachable() {
		t.Error("SetReachable(false) after Update did not reach the device")
	}
	plug.SetReachable(ctx, true)

	// The host's name was not persisted: a device under the same key with
	// another name serves its own name, where a persisted label would win.
	again, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug"), Name: "Kitchen"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pair.Device.Act(func(n *node.Node) error { return n.Aggregator().Replace(ctx, device.StringKey("plug"), again) }); err != nil {
		t.Fatal(err)
	}
	if got := label(); got != "Kitchen" {
		t.Errorf("after a replacement: NodeLabel %q, want Kitchen (the host's name was persisted)", got)
	}
	plug = again

	// A controller writes a label: it wins over a later host name.
	if err := fabrictest.Write(ctx, pair, client.AttributeAt(ep, bridgedinfodef.ClusterID, bridgedinfodef.AttrNodeLabel), spec.String("Coffee")); err != nil {
		t.Fatal(err)
	}
	plug.Update(ctx, func(s *device.HostStatement) { s.Name = "Kitchen socket" })
	if got := label(); got != "Coffee" {
		t.Errorf("after the controller's label and a host name: NodeLabel %q, want Coffee", got)
	}
	if err := pair.Device.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if got := label(); got != "Coffee" {
		t.Errorf("after a restart: NodeLabel %q, want the persisted Coffee", got)
	}
}

// TestNodeLabelCap is #239: the kit caps a name at the NodeLabel maximum
// of the generated definition, at a rune boundary, and ComposeNodeLabel
// is the facade's composition.
func TestNodeLabelCap(t *testing.T) {
	t.Parallel()
	max := int(bridgedinfodef.Definition.Attribute(bridgedinfodef.AttrNodeLabel).Constraint.Max.Int)
	long := strings.Repeat("ä", 20) // 40 bytes
	plug, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug"), Name: long})
	if err != nil {
		t.Fatal(err)
	}
	check := func(what, got string) {
		t.Helper()
		if len(got) > max || !utf8.ValidString(got) || !strings.HasPrefix(long, got) {
			t.Errorf("%s: %q (%d bytes), want a valid prefix of at most %d bytes", what, got, len(got), max)
		}
	}
	check("New", plug.Spec().FriendlyName)
	plug.Rename(context.Background(), long+"x")
	check("Rename", plug.Spec().FriendlyName)
	plug.Update(context.Background(), func(s *device.HostStatement) { s.Name = long })
	check("Update", plug.Spec().FriendlyName)
	if got := device.ComposeNodeLabel("Lamp", "ch 2"); got != "Lamp (ch 2)" {
		t.Errorf("ComposeNodeLabel %q", got)
	}
	if got := device.ComposeNodeLabel(strings.Repeat("a", 30), "suffix"); len(got) != max {
		t.Errorf("ComposeNodeLabel over the cap: %q", got)
	}
}
