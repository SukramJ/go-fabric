// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// plug is a bridged OnOffPlugInUnit holding an onoff.Server.
type plug struct{ srv *onoff.Server }

func newPlug(t *testing.T) *plug {
	t.Helper()
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	return &plug{srv: srv}
}

func (p *plug) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey("plug:1"), DeviceAddress: "plug:1", ChannelAddress: "plug:1:0",
		DeviceType: onoff.DeviceTypeOnOffPlugInUnit, FriendlyName: "plug", Source: p,
	}
}

func (p *plug) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }

func (p *plug) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

func rootAttr(cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 0, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func onOffPath(ep uint16) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: onoff.ClusterID, Attribute: onoff.AttrOnOff, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func stringValue(v string) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: v}})
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// pairWithPlug is a commissioned pair whose device serves one plug.
func pairWithPlug(t *testing.T, opts fabrictest.Options) (*fabrictest.Site, *fabrictest.Pair, *plug, uint16) {
	t.Helper()
	site := fabrictest.NewSite(t, opts)
	pl := newPlug(t)
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{pl}}})
	ep, ok := p.Device.EndpointOf("plug:1")
	if !ok {
		t.Fatal("the plug has no endpoint")
	}
	return site, p, pl, ep
}

// TestCommissionedPair: the pair is commissioned in memory on a fake clock
// that commissioning did not need to move; the device's fabric is the
// controller's, the peer holds the CASE session; read, write (plain and
// timed), invoke (plain and timed) and an unknown attribute's status go
// over the wire.
func TestCommissionedPair(t *testing.T) {
	t.Parallel()
	site, p, _, ep := pairWithPlug(t, fabrictest.Options{})
	ctx := testContext(t)
	if site.Fake() == nil || site.Net() == nil || site.Loopback() {
		t.Fatal("the default site is not in memory on a fake clock")
	}
	if !p.Commissioned() || !p.Peer.HasSession() {
		t.Fatal("no CASE session after commissioning")
	}
	fabrics, err := p.Device.Node().Fabrics(ctx)
	if err != nil || len(fabrics) != 1 || fabrics[0].NodeID != p.Address.NodeID {
		t.Fatalf("device fabrics %+v, %v", fabrics, err)
	}
	if got := p.Device.Addr().String(); got != "[fe80::2]:5540" {
		t.Fatalf("device address %s", got)
	}
	label := rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	d, err := fabrictest.Read(ctx, p, label)
	if err != nil || d.Value.El.String != "device2" {
		t.Fatalf("NodeLabel %+v, %v", d, err)
	}
	for i, write := range []func(context.Context, *fabrictest.Pair, im.ConcreteAttributePath, spec.Encodable) error{fabrictest.Write, fabrictest.WriteTimed} {
		want := []string{"plain", "timed"}[i]
		if err := write(ctx, p, label, stringValue(want)); err != nil {
			t.Fatalf("write %s: %v", want, err)
		}
		if d, err := fabrictest.Read(ctx, p, label); err != nil || d.Value.El.String != want {
			t.Fatalf("NodeLabel after the %s write: %+v, %v", want, d, err)
		}
	}
	var se *interaction.StatusError
	if err := fabrictest.Write(ctx, p, rootAttr(basicinfo.ClusterID, 0x7777), stringValue("x")); !errors.As(err, &se) || se.Path == nil {
		t.Fatalf("unknown attribute written: %v", err)
	}
	if _, err := fabrictest.Read(ctx, p, rootAttr(basicinfo.ClusterID, 0x7777)); !errors.As(err, &se) || se.Status != im.StatusUnsupportedAttribute {
		t.Fatalf("unknown attribute read: %v", err)
	}
	toggle := im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}
	if res, err := fabrictest.Invoke(ctx, p, toggle, nil); err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("Toggle: %+v, %v", res, err)
	}
	if res, err := fabrictest.InvokeTimed(ctx, p, toggle, nil); err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("timed Toggle: %+v, %v", res, err)
	}
	if d, err := fabrictest.Read(ctx, p, onOffPath(ep)); err != nil || d.Value.El.Bool {
		t.Fatalf("OnOff after two toggles: %+v, %v", d, err)
	}
	if _, err := fabrictest.Invoke(ctx, p, im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: 0x77}, nil); !errors.As(err, &se) {
		t.Fatalf("unknown command: %v", err)
	}
	if ix, err := fabrictest.Connect(ctx, p); err != nil || ix != p.Interaction {
		t.Fatalf("Connect: %v", err)
	}
}

// TestEventsAndSubscribe: the StartUp event reads back; a subscription's
// priming report carries OnOff, and a change made locally (Act) reaches
// the controller once the site's clock passes the subscription's floor.
func TestEventsAndSubscribe(t *testing.T) {
	t.Parallel()
	site, p, pl, ep := pairWithPlug(t, fabrictest.Options{})
	ctx := testContext(t)
	events, err := fabrictest.Events(ctx, p, im.ConcreteEventPath{Endpoint: 0, Cluster: basicinfo.ClusterID, Event: basicinfo.EventStartUp, HasEndpoint: true, HasCluster: true, HasEvent: true})
	if err != nil || len(events) == 0 {
		t.Fatalf("StartUp events %+v, %v", events, err)
	}
	reports := make(chan *interaction.Report, 16)
	sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{onOffPath(ep)}, MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Minute),
		Updated: func(r *interaction.Report) { reports <- r },
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()
	before := site.Clock().Now()
	if err := p.Device.Act(func(*node.Node) error { pl.srv.Set(true); return nil }); err != nil {
		t.Fatal(err)
	}
	var on bool
	if err := site.Eventually(ctx, "the report of OnOff true", func() bool {
		select {
		case r := <-reports:
			v, ok := r.Value(ep, onoff.ClusterID, onoff.AttrOnOff)
			on = ok && v.El.Bool
		default:
		}
		return on
	}); err != nil {
		t.Fatal(err)
	}
	if moved := site.Clock().Now().Sub(before); moved <= 0 {
		t.Fatalf("the report arrived without the clock moving (%v)", moved)
	}
}

// TestNetworkLossAndDelay: with every datagram lost a read fails once the
// controller's retransmissions — stepped on the fake clock — run out; with
// a delay and duplicates it succeeds; a filter drops one chosen datagram
// and MRP recovers it.
func TestNetworkLossAndDelay(t *testing.T) {
	t.Parallel()
	site, p, _, _ := pairWithPlug(t, fabrictest.Options{})
	ctx := testContext(t)
	label := rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	site.Net().Set(fabrictest.NetOptions{Loss: 1})
	_, err := fabrictest.Read(ctx, p, label)
	if err == nil {
		t.Fatal("a read over a network that loses everything succeeded")
	}
	t.Logf("read with full loss: %v", err)
	site.Net().Set(fabrictest.NetOptions{Delay: 300 * time.Millisecond, Duplicate: 1, Seed: 3})
	if got := site.Net().Options(); got.Delay != 300*time.Millisecond || got.Seed != 3 {
		t.Fatalf("options %+v", got)
	}
	if _, err := fabrictest.Connect(ctx, p); err != nil {
		t.Fatalf("Connect after the loss: %v", err)
	}
	if _, err := fabrictest.Read(ctx, p, label); err != nil {
		t.Fatalf("read with delay and duplicates: %v", err)
	}
	site.Net().Set(fabrictest.NetOptions{})
	dropped := 0
	site.Net().Filter(func(d fabrictest.Datagram) bool {
		if dropped == 0 && d.To.String() == p.Device.Addr().String() {
			dropped++
			return false
		}
		return true
	})
	sentBefore := site.Net().Sent()
	if _, err := fabrictest.Read(ctx, p, label); err != nil {
		t.Fatalf("read with one datagram dropped: %v", err)
	}
	site.Net().Filter(nil)
	if dropped != 1 || site.Net().Sent()-sentBefore < 3 {
		t.Fatalf("dropped %d, sent %d", dropped, site.Net().Sent()-sentBefore)
	}
}

// TestUncommissionedPairAndRegistry: an uncommissioned pair refuses the
// wire helpers; commissioned afterwards it yields the registry's client
// node for its peer address.
func TestUncommissionedPairAndRegistry(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	u := site.UncommissionedPair(fabrictest.PairOptions{Controller: fabrictest.ControllerOptions{FabricID: 7, NodeID: 99}})
	ctx := testContext(t)
	if _, err := fabrictest.Read(ctx, u, rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)); !errors.Is(err, fabrictest.ErrNotCommissioned) {
		t.Fatalf("read on an uncommissioned pair: %v", err)
	}
	if _, err := u.Node(); err == nil {
		t.Fatal("a client node of an uncommissioned pair")
	}
	p, err := u.Controller.Commission(ctx, u.Device, fabrictest.CommissionOptions{NodeID: 0x4321})
	if err != nil {
		t.Fatal(err)
	}
	if p.Address.NodeID != 0x4321 || p.Controller.Fabric().FabricID != 7 {
		t.Fatalf("address %+v, fabric %d", p.Address, p.Controller.Fabric().FabricID)
	}
	n, err := p.Node()
	if err != nil || n == nil {
		t.Fatalf("client node: %v", err)
	}
	if got := p.Controller.Registry().GetByAddress(p.Address); got != n {
		t.Fatal("the registry does not hold the client node")
	}
	if len(site.Devices()) != 1 || len(site.Controllers()) != 1 {
		t.Fatalf("%d devices, %d controllers", len(site.Devices()), len(site.Controllers()))
	}
}

// TestLoopbackPair: on loopback the pair commissions over real sockets on
// [::1] and the site's DNS-SD carries the device's records.
func TestLoopbackPair(t *testing.T) {
	t.Parallel()
	_, p, _, ep := pairWithPlug(t, fabrictest.Options{Loopback: true})
	ctx := testContext(t)
	if ip := p.Device.Addr().IP; !ip.IsLoopback() || p.Device.Addr().Port == 0 {
		t.Fatalf("device address %v", p.Device.Addr())
	}
	if _, err := fabrictest.Read(ctx, p, onOffPath(ep)); err != nil {
		t.Fatal(err)
	}
}

// TestDeviceRestart: a restarted device keeps its fabric and its endpoint
// numbers, and answers the controller again.
func TestDeviceRestart(t *testing.T) {
	t.Parallel()
	_, p, _, ep := pairWithPlug(t, fabrictest.Options{})
	ctx := testContext(t)
	if err := p.Device.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if again, ok := p.Device.EndpointOf("plug:1"); !ok || again != ep {
		t.Fatalf("plug endpoint %d after the restart, was %d", again, ep)
	}
	if err := p.Device.Start(ctx); err == nil {
		t.Fatal("a running device started again")
	}
	if _, err := fabrictest.Connect(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := fabrictest.Read(ctx, p, onOffPath(ep)); err != nil {
		t.Fatalf("read after the restart: %v", err)
	}
	if err := p.Device.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Device.Stop(ctx); err != nil {
		t.Fatalf("a second Stop: %v", err)
	}
}
