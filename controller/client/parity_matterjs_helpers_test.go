// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// The helpers of the ports of packages/node/test/node/Client*Test.ts and
// its siblings at 862ad75f: matter.js MockSite (node/src/testing/mock-site.ts)
// is fabrictest's Site, controller.peers the controller's client.Registry,
// and a pair commissioned as MockSite.addCommissionedPair does it
// (mock-site.ts:156, controller.peers.commission) is
// fabrictest.Controller.CommissionNode.

// parityCtx bounds one port on the wall clock.
func parityCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// siteSlots runs the package's fabrictest sites one at a time. A fake site
// clock moves only while the whole test process is quiet (fabrictest
// Site.Run, the docs' "Limits"), so busy sites in parallel starve each
// other's clocks — under the race detector past the tests' wall-clock
// budgets; one at a time, every site reaches its quiet points at once.
var siteSlots = make(chan struct{}, 1)

// holdSite takes the package's site slot until the test ends.
func holdSite(t *testing.T) {
	t.Helper()
	siteSlots <- struct{}{}
	t.Cleanup(func() { <-siteSlots })
}

// quietSite is a site whose hosts log nowhere; it holds the site slot
// until the test ends.
func quietSite(t *testing.T) *fabrictest.Site {
	t.Helper()
	holdSite(t)
	return fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
}

// clientPairOptions shapes a client pair.
type clientPairOptions struct {
	// rootOnly leaves out MockServerNode's default OnOffLight
	// (`{ device: ServerNode.RootEndpoint }`); the Go node keeps its
	// Aggregator.
	rootOnly bool
	// commission configures the registry's commission.
	commission client.CommissionOptions
	// controller configures the controller.
	controller fabrictest.ControllerOptions
	// configure edits the device's configuration.
	configure func(*node.Config)
}

// clientPair is MockSite.addCommissionedPair with
// subscribedPeer(controller, "peer1") (node-helpers.ts:548): a controller
// and a device with an OnOffLight, commissioned through the controller's
// registry, the node's default subscription active (unless the commission
// turned it off).
type clientPair struct {
	site  *fabrictest.Site
	pair  *fabrictest.Pair
	node  *client.Node
	light *device.OnOffLight
	// lightEP is the light's endpoint, 0 without one.
	lightEP uint16
}

// newClientPair builds and commissions a client pair.
func newClientPair(t *testing.T, o clientPairOptions) *clientPair {
	t.Helper()
	site := quietSite(t)
	cp := &clientPair{site: site}
	topo := fabrictest.Topology{Configure: o.configure}
	if !o.rootOnly {
		l, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light:1"), Name: "light:1", Clock: site.Clock()})
		if err != nil {
			t.Fatal(err)
		}
		cp.light = l
		topo.Bridged = []node.Endpoint{l}
	}
	cp.pair = site.UncommissionedPair(fabrictest.PairOptions{Device: topo, Controller: o.controller})
	n, err := cp.pair.Controller.CommissionNode(parityCtx(t), cp.pair.Device, o.commission)
	if err != nil {
		t.Fatal(err)
	}
	cp.node = n
	if !o.rootOnly {
		ep, ok := cp.pair.Device.EndpointOf("light:1")
		if !ok {
			t.Fatal("the light has no endpoint")
		}
		cp.lightEP = ep
	}
	if o.commission.AutoSubscribe == nil || *o.commission.AutoSubscribe {
		cp.eventually(t, "the default subscription", n.SubscriptionActive)
	}
	return cp
}

// eventually waits for cond, moving the site's clock.
func (cp *clientPair) eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	if err := cp.site.Eventually(parityCtx(t), what, cond); err != nil {
		t.Fatal(err)
	}
}

// run runs fn under the site, failing the test on an error.
func (cp *clientPair) run(t *testing.T, fn func(ctx context.Context) error) {
	t.Helper()
	if err := cp.site.Run(parityCtx(t), fn); err != nil {
		t.Fatal(err)
	}
}

// advanceStep is the step of [clientPair.advance].
const advanceStep = 2 * time.Second

// advance moves the site's fake clock by d in steps of advanceStep (or a
// sixtieth of d when that is longer), the
// site settling after each, so the work a step's timers start runs before
// the next — MockTime.advance stepping the timers in order. (Moving
// deadline by deadline under Site.Run is too slow for minutes of fake
// time; a single jump fires an exchange's timeouts before its answers.)
func (cp *clientPair) advance(t *testing.T, d time.Duration) {
	t.Helper()
	// A long advance takes at most about sixty steps.
	unit := max(advanceStep, d/60)
	for d > 0 {
		step := min(d, unit)
		cp.site.Fake().Advance(step)
		d -= step
		if err := cp.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
	}
}

// rootPath is a path to every attribute of endpoint 0.
func rootPath() im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true}
}

// clusterPath is a path to every attribute of a cluster on an endpoint.
func clusterPath(ep uint16, cluster uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, HasEndpoint: true, HasCluster: true}
}

// attributePath is a concrete attribute path.
func attributePath(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// clustersOf is ClientSubscribeTest.ts:75-84 clustersOf: the data
// version of each cluster with attribute data in r.
func clustersOf(r *interaction.Report, into map[uint32]uint32) {
	if r == nil {
		return
	}
	for i := range r.Attributes {
		into[r.Attributes[i].Path.Cluster] = r.Attributes[i].DataVersion
	}
}

// boolPtr is a *bool.
func boolPtr(v bool) *bool { return &v }

// newLight is an OnOffLight on the site's clock.
func newLight(site *fabrictest.Site, key string) (*device.OnOffLight, error) {
	return device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key, Clock: site.Clock()})
}

// mustAddr parses an IP address.
func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }
