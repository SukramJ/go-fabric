// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

const (
	liveFabricID   uint64 = 0x0000_0000_0000_0F0C
	liveController uint64 = 112233
	liveNode       uint64 = 0x0000_0000_0000_4322
)

// plug is a bridged OnOffPlugInUnit holding an onoff.Server.
type plug struct{ srv *onoff.Server }

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

// live is a node with one bridged plug commissioned by the module's own
// commissioner, whose context carries the subscription registry and the
// CASE responder (§4) — a fabrictest pair on the in-memory network and
// the site's fake clock.
type live struct {
	site   *fabrictest.Site
	pair   *fabrictest.Pair
	reg    *interaction.Subscriptions
	peer   *peer.Peer
	c      *interaction.Interaction
	plugEP uint16
}

func newLive(t *testing.T) *live {
	t.Helper()
	return newLiveWith(t, discardLog())
}

// siteSlots runs the package's fabrictest sites one at a time, as
// controller/client's and node's do: a fake site clock moves only while
// the whole test process is quiet (fabrictest Site.Run, Site.Eventually),
// so busy sites in parallel starve each other's clocks — under the race
// detector past the wall-clock budgets of the waits below
// (TestLiveSustainedSurvivesRestart, a named flake of gate 1). One at a
// time, every site reaches its quiet points at once.
var siteSlots = make(chan struct{}, 1)

// holdSite takes the package's site slot until the test ends.
func holdSite(t *testing.T) {
	t.Helper()
	siteSlots <- struct{}{}
	t.Cleanup(func() { <-siteSlots })
}

// newLiveWith is newLive with the controller's logger.
func newLiveWith(t *testing.T, regLog *slog.Logger) *live {
	t.Helper()
	holdSite(t)
	site := fabrictest.NewSite(t, fabrictest.Options{})
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, TimeSource: site.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{
			Bridged: []node.Endpoint{&plug{srv: srv}},
			Configure: func(c *node.Config) {
				c.Commissioning.Passcode = testPasscode
				c.BasicInformation.ProductName, c.BasicInformation.NodeLabel, c.BasicInformation.SerialNumber = "interaction live test", "live node", "IL-1"
			},
		},
		Controller: fabrictest.ControllerOptions{FabricID: liveFabricID, NodeID: liveController, Label: "live", Logger: regLog},
		Commission: fabrictest.CommissionOptions{NodeID: liveNode, Edit: func(o *commissioner.Options) { o.OnAttestationFailure = attestation.Accept() }},
	})
	lv := &live{site: site, pair: pair, reg: pair.Controller.Subscriptions(), peer: pair.Peer, c: pair.Interaction}
	ep, ok := pair.Device.EndpointOf("plug:1")
	if !ok {
		t.Fatal("the plug has no endpoint")
	}
	lv.plugEP = ep
	return lv
}

// run runs fn under the site's clock (fabrictest.Site.Run).
func (lv *live) run(ctx context.Context, fn func(ctx context.Context) error) error {
	return lv.site.Run(ctx, fn)
}

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func stringValue(v string) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: v}})
}

func (lv *live) onOff() im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: lv.plugEP, Cluster: onoff.ClusterID, Attribute: onoff.AttrOnOff, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func (lv *live) toggle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := lv.run(ctx, func(ctx context.Context) error {
		_, err := lv.c.Invoke(ctx, im.ConcreteCommandPath{Endpoint: lv.plugEP, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}, nil, interaction.InvokeOptions{})
		return err
	}); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
}

// waitOnOff waits for a report carrying OnOff = want, moving the site's
// clock while it waits.
func waitOnOff(t *testing.T, lv *live, reports <-chan *interaction.Report, want bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := lv.site.Eventually(ctx, fmt.Sprintf("a report with OnOff = %v", want), func() bool {
		for {
			select {
			case r := <-reports:
				if v, ok := r.Value(lv.plugEP, onoff.ClusterID, onoff.AttrOnOff); ok && v.El.Bool == want {
					return true
				}
			default:
				return false
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// wait waits for cond, moving the site's clock while it waits.
func (lv *live) wait(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := lv.site.Eventually(ctx, what, cond); err != nil {
		t.Fatal(err)
	}
}

// TestLiveWrite: Write NodeLabel over the peer's CASE session — plain and
// timed — and read it back (ClientInteraction.ts:365-429).
func TestLiveWrite(t *testing.T) {
	t.Parallel()
	lv := newLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	label := rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	for i, opts := range []interaction.WriteOptions{{}, {Timed: true}} {
		want := fmt.Sprintf("written %d", i)
		var res interaction.WriteStatuses
		err := lv.run(ctx, func(ctx context.Context) error {
			var err error
			res, err = lv.c.Write(ctx, []interaction.WriteRequest{{Path: label, Value: stringValue(want)}}, opts)
			return err
		})
		if err != nil || len(res) != 1 || res.Err() != nil {
			t.Fatalf("Write %+v: %+v, %v", opts, res, err)
		}
		var r *interaction.Report
		if err := lv.run(ctx, func(ctx context.Context) error {
			var err error
			r, err = lv.c.Read(ctx, label)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if v, ok := r.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel); !ok || v.El.String != want {
			t.Fatalf("NodeLabel %+v after writing %q", v, want)
		}
	}
	// A failing path: an unknown attribute.
	var res interaction.WriteStatuses
	err := lv.run(ctx, func(ctx context.Context) error {
		var err error
		res, err = lv.c.Write(ctx, []interaction.WriteRequest{{Path: rootAttr(basicinfo.ClusterID, 0x7777), Value: stringValue("x")}}, interaction.WriteOptions{})
		return err
	})
	var se *interaction.StatusError
	if err != nil || !errors.As(res.Err(), &se) {
		t.Fatalf("unknown attribute: %+v, %v", res, err)
	}
}

// TestLiveSubscribe: a subscription to OnOff on the bridged plug — the
// priming report, a Toggle and the pushed report through Updated (§3d), a
// keepalive, ReplaceSubscriptions closing the earlier subscription, and a
// subscription we closed answered InvalidSubscription so the node drops it.
func TestLiveSubscribe(t *testing.T) {
	t.Parallel()
	unknown := make(chan uint64, 8)
	lv := newLiveWith(t, slog.New(&recordHandler{msg: "matter.controller.interaction.report_for_unknown_subscription", key: "subscription", seen: unknown}))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	reports := make(chan *interaction.Report, 16)
	keepalives := make(chan struct{}, 16)
	var sub *interaction.Subscription
	err := lv.run(ctx, func(ctx context.Context) error {
		var err error
		sub, err = lv.c.Subscribe(ctx, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{lv.onOff()}, MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(2 * time.Second),
			Updated:           func(r *interaction.Report) { reports <- r },
			KeepaliveReceived: func() { keepalives <- struct{}{} },
		})
		return err
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	waitOnOff(t, lv, reports, false)
	lv.toggle(t)
	waitOnOff(t, lv, reports, true)
	lv.wait(t, "a keepalive", 30*time.Second, func() bool {
		select {
		case <-keepalives:
			return true
		default:
			return false
		}
	})

	var second *interaction.Subscription
	err = lv.run(ctx, func(ctx context.Context) error {
		var err error
		second, err = lv.c.Subscribe(ctx, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{lv.onOff()}, ReplaceSubscriptions: true, Updated: func(r *interaction.Report) { reports <- r }})
		return err
	})
	if err != nil {
		t.Fatalf("second Subscribe: %v", err)
	}
	select {
	case <-sub.Done():
	default:
		t.Fatal("ReplaceSubscriptions left the earlier subscription open")
	}
	// We forget the subscription; the node's next report on it is answered
	// InvalidSubscription (ClientSubscriptionHandler.ts:88-104).
	second.Close()
	lv.toggle(t)
	var id uint64
	lv.wait(t, "the refusal of the node's report on the closed subscription", 30*time.Second, func() bool {
		select {
		case id = <-unknown:
			return true
		default:
			return false
		}
	})
	if id != uint64(second.ID()) {
		t.Fatalf("InvalidSubscription for %d, want %d", id, second.ID())
	}
}

// TestLiveLivenessTimeout: on the site's fake clock a subscription whose
// deadline passes closes (ClientSubscriptions.ts:224-277). The network
// drops every datagram once the subscription stands, so no report of the
// node, whose clock moves with the registry's, keeps it alive.
func TestLiveLivenessTimeout(t *testing.T) {
	t.Parallel()
	lv := newLive(t)
	clock := &fakeClock{lv.site.Fake()}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var sub *interaction.Subscription
	err := lv.run(ctx, func(ctx context.Context) error {
		var err error
		sub, err = lv.c.Subscribe(ctx, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{lv.onOff()}, MaxIntervalCeiling: interaction.Interval(time.Minute)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	lv.site.Net().Set(fabrictest.NetOptions{Loss: 1})
	timeout := sub.MaxInterval() + 2*lv.c.MaximumPeerResponseTime(interaction.SubscriptionProcessingTime, false)
	clock.waitTimer(t, timeout)
	clock.Advance(timeout)
	select {
	case <-sub.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("no timeout")
	}
}

// TestLiveSustainedSurvivesRestart: a sustained subscription to OnOff;
// the node stops and starts again on its database; the restarted node
// re-establishes its persisted subscription by opening CASE to the
// controller (the CASE responder, §1), the session joins the peer, and the
// reports arrive through the report handler (§3d) — the re-established
// priming report, then a Toggle's.
func TestLiveSustainedSurvivesRestart(t *testing.T) {
	t.Parallel()
	lv := newLive(t)
	reports := make(chan *interaction.Report, 16)
	var sub *interaction.Subscription
	err := lv.run(context.Background(), func(ctx context.Context) error {
		var err error
		sub, err = lv.c.Subscribe(ctx, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{lv.onOff()}, Sustain: true, MaxIntervalCeiling: interaction.Interval(time.Minute),
			Updated: func(r *interaction.Report) { reports <- r },
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	waitOnOff(t, lv, reports, false)
	lv.wait(t, "the subscription active", 10*time.Second, sub.Active)
	waitPersisted(t, lv, 1)
	id := sub.ID()
	// The restarted node re-establishes the subscription inside Start and
	// waits there for the priming report's StatusResponse; under Site.Run
	// the site's Fake moves while it waits, so a retransmission the answer
	// needs fires instead of blocking Start for good.
	restartCtx, cancelRestart := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelRestart()
	if err := lv.run(restartCtx, lv.pair.Device.Restart); err != nil {
		t.Fatal(err)
	}
	waitOnOff(t, lv, reports, false)
	s := lv.peer.NewestSession()
	if s == nil || s.Addr().String() != lv.pair.Device.Bridge().LocalAddr() {
		t.Fatalf("the peer's newest session is not the node's inbound one: %v", s)
	}
	if sub.ID() != id || !sub.Active() {
		t.Fatalf("subscription %d active %v, want %d", sub.ID(), sub.Active(), id)
	}
	lv.toggle(t)
	waitOnOff(t, lv, reports, true)
}

// recordHandler is a slog handler that reports the uint64 attribute key of
// every record with message msg.
type recordHandler struct {
	msg  string
	key  string
	seen chan<- uint64
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *recordHandler) WithGroup(string) slog.Handler            { return h }
func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message != h.msg {
		return nil
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == h.key {
			select {
			case h.seen <- a.Value.Uint64():
			default:
			}
		}
		return true
	})
	return nil
}

// waitPersisted waits until the node holds want persisted subscriptions,
// moving the site's clock (the node's persist loop runs on it).
func waitPersisted(t *testing.T, lv *live, want int) {
	t.Helper()
	var rows int
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if werr := lv.site.Eventually(ctx, fmt.Sprintf("%d persisted subscriptions", want), func() bool {
		stored, lerr := lv.pair.Device.Store().LoadServerSubscriptions(context.Background())
		rows, err = len(stored), lerr
		return err == nil && rows == want
	}); werr != nil {
		t.Fatalf("%d persisted subscriptions (%v), want %d: %v", rows, err, want, werr)
	}
}
