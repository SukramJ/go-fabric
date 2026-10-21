// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/clock"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestAccessors: what a device and a controller expose for a test, a host
// topology in place of the Aggregator, an unsupported event's status, and
// a site on the wall clock, whose Run only runs the work.
func TestAccessors(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	if site.Fake() != nil || site.Logger() == nil {
		t.Fatal("a wall-clock site with a fake")
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Snapshotter: endpointtest.NewEmptySnapshotter()}})
	ctx := testContext(t)
	d, c := p.Device, p.Controller
	if d.Index() != 2 || c.Index() != 1 {
		t.Fatalf("indexes %d %d", d.Index(), c.Index())
	}
	if d.Passcode() != fabrictest.DefaultPasscode || d.Discriminator() != fabrictest.DefaultDiscriminator {
		t.Fatalf("passcode %d discriminator %d", d.Passcode(), d.Discriminator())
	}
	if d.Node().Aggregator() != nil || len(d.Endpoints()) < 2 {
		t.Fatalf("host topology: aggregator %v, %d endpoints", d.Node().Aggregator(), len(d.Endpoints()))
	}
	if _, ok := d.EndpointOf("nothing"); ok {
		t.Fatal("an endpoint for an unknown key")
	}
	ops := 0
	for _, svc := range d.Advertiser().Active() {
		if svc.ServiceType == mdns.ServiceTypeOperational {
			ops++
		}
	}
	if ops != 1 {
		t.Fatalf("%d operational records", ops)
	}
	if d.Store() == nil || d.Bridge() == nil {
		t.Fatal("no store or bridge")
	}
	if c.Exchanges() == nil || c.Names() == nil || c.CA() == nil || c.Peers() == nil || c.Resumption() == nil ||
		c.Commissioner() == nil || c.Registry() == nil || c.Subscriptions() == nil || c.Addr() == nil {
		t.Fatal("a controller part is missing")
	}
	if c.Fabric().FabricID != fabrictest.DefaultFabricID {
		t.Fatalf("fabric %d", c.Fabric().FabricID)
	}
	if site.Net() == nil || site.Net().Options() != (fabrictest.NetOptions{}) {
		t.Fatal("the network's options")
	}
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := fabrictest.ReadWith(ctx, p, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)}})
	if err != nil || len(r.Attributes) != 1 {
		t.Fatalf("ReadWith: %+v, %v", r, err)
	}
	site.Close()
	site.Close() // idempotent
}
