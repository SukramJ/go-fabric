// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/DelayReportDataTest.ts at
// 862ad75f: an InvokeRequest's DelayReportData holding off the next report
// of the subscriptions that select an endpoint the invoke dispatches to.
//
// matter.js invokes and subscribes on a MockServerNode with the
// "delay-report-data" forward feature switched on; the module acts on
// DelayReportData always (BD-Matter-DelayReportData), so the case "while
// forward Matter features are off" has no Go counterpart. The port
// subscribes and invokes over the wire from a commissioned fabrictest pair
// and steps the site's fake clock itself; a report's time is the clock when
// it reaches the controller, which on the in-memory network without delay
// is when the device sent it.
//
// matter.js's SEND_DELAY — the time a change takes to go out without a
// deferral, 50 ms — is the subscription engine's tick in Go, 250 ms
// (im/subscription Config.TickInterval; the shared ticker is
// L10-D11 in notes/parity/by_design.md): every window that reads
// "SEND_DELAY" in matter.js reads sendDelay here. The OnOffLight is a
// bridged endpoint below the Aggregator. The random jitter is drawn from
// math/rand in Go, where matter.js's MockCrypto is constant, so the jitter
// case asserts the window the jitter may fall in.
package node_test

import (
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/identify"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// sendDelay is matter.js's SEND_DELAY in Go: the subscription engine's
// tick (im/subscription's default TickInterval).
const sendDelay = 250 * time.Millisecond

func TestDelayReportDataParity(t *testing.T) {
	t.Parallel()

	drd := func(minMs, jitterMs uint16) *im.DelayReportData {
		return &im.DelayReportData{DelayMinMs: minMs, DelayJitterWindowMs: jitterMs}
	}

	t.Run("defers the next report by DelayMinMs", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
	})

	t.Run("adds a random jitter below DelayJitterWindowMs", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 500))
		d.expectReportAt(t, r, start, time.Second, 1500*time.Millisecond+sendDelay)
	})

	t.Run("defers whether or not the command succeeds", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		// OnWithTimedOff without its fields fails.
		a := rawInvoke(t, d.pair, im.InvokeRequest{
			Invokes:         []im.CommandInvocation{{Path: cmdPath(d.lightEP, onoff.ClusterID, onoff.CmdOnWithTimedOff)}},
			DelayReportData: drd(1000, 0),
		})
		if a.status != nil || len(a.resp.Responses) != 1 || a.resp.Responses[0].Status.Status == im.StatusSuccess {
			t.Fatalf("OnWithTimedOff without fields: %+v", a)
		}
		d.setOnOff(t, true)
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
	})

	t.Run("keeps a deferral that ends earlier than a new one", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(2000, 0))
		d.advanceTo(t, start.Add(500*time.Millisecond))
		d.toggle(t, d.lightEP, drd(5000, 0))
		d.expectReportAt(t, r, start, 2*time.Second, 2*time.Second+sendDelay)
	})

	t.Run("shortens a deferral that ends later than a new one", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(5000, 0))
		d.advanceTo(t, start.Add(time.Second))
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.expectReportAt(t, r, start, 2*time.Second, 2*time.Second+sendDelay)
	})

	t.Run("applies a new deferral after an earlier one ended", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
		start = d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
	})

	t.Run("holds a report that is already waiting for its send delay", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.setOnOff(t, true)
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
	})

	t.Run("defers for a wildcard invoke on the endpoints it expands to", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.invoke(t, wildcardCmd(onoff.ClusterID, onoff.CmdToggle), drd(1000, 0))
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
	})

	t.Run("defers a subscription of another fabric", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		ctx := parityCtx(t)
		if err := d.pair.Device.OpenWindow(ctx, 0); err != nil {
			t.Fatal(err)
		}
		other, err := d.site.AddController(fabrictest.ControllerOptions{FabricID: 2, NodeID: 0x2222}).Commission(ctx, d.pair.Device, fabrictest.CommissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		r := d.subscribeOn(t, other, []im.ConcreteAttributePath{d.onOffPath()}, nil, 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
	})

	t.Run("still reports within the send interval under a stream of deferring invokes", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 10)
		start := d.now()
		for d.now().Sub(start) < 17*time.Second {
			d.toggle(t, d.lightEP, drd(60000, 0))
			d.advanceTo(t, d.now().Add(40*time.Millisecond))
		}
		sent := r.times()
		if len(sent) != 2 {
			t.Fatalf("%d reports in 17 s (%v), want the 2 the send interval forces", len(sent), sent)
		}
		previous := start
		for _, at := range sent {
			if gap := at.Sub(previous); gap > 8*time.Second {
				t.Fatalf("a report %v after the previous one, more than the 8 s send interval", gap)
			}
			previous = at
		}
	})

	t.Run("still reports after the send delay under a stream of invokes without delay", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		for range 10 {
			d.toggle(t, d.lightEP, drd(0, 0))
			d.advanceTo(t, d.now().Add(40*time.Millisecond))
		}
		sent := r.times()
		if len(sent) == 0 || sent[0].Sub(start) > sendDelay {
			t.Fatalf("reports %v, want the first within %v of %v", sent, sendDelay, start)
		}
	})

	t.Run("reschedules a held report when a later invoke shortens the deferral", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(5000, 0))
		d.advanceTo(t, start.Add(time.Second))
		// Identify changes no OnOff attribute.
		a := rawInvoke(t, d.pair, im.InvokeRequest{
			Invokes:         []im.CommandInvocation{{Path: cmdPath(d.lightEP, identify.ClusterID, identify.CmdIdentify), Fields: identify.IdentifyRequest{IdentifyTime: 0}}},
			DelayReportData: drd(1000, 0),
		})
		if a.status != nil {
			t.Fatalf("Identify: StatusResponse 0x%02x", uint8(*a.status))
		}
		d.expectReportAt(t, r, start, 2*time.Second, 2*time.Second+sendDelay)
	})

	t.Run("sends no extra report after a deferred one", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 10)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.advanceTo(t, start.Add(time.Second+sendDelay))
		if n := len(r.times()); n != 1 {
			t.Fatalf("%d reports after the deferral, want 1", n)
		}
		d.advanceTo(t, d.now().Add(7*time.Second))
		if n := len(r.times()); n != 1 {
			t.Fatalf("%d reports 7 s later, want still 1", n)
		}
	})

	t.Run("does not hold a report past the subscription's next scheduled report", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 10)
		start := d.now()
		d.toggle(t, d.lightEP, drd(60000, 0))
		d.expectReportAt(t, r, start, 8*time.Second-sendDelay, 8*time.Second+sendDelay)
	})

	t.Run("defers only the next report", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
		start = d.now()
		d.toggle(t, d.lightEP, nil)
		d.expectReportAt(t, r, start, 0, sendDelay)
	})

	t.Run("does not defer a subscription that selects no targeted endpoint", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, nodeLabelAt(0), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.relabel(t)
		d.expectReportAt(t, r, start, 0, sendDelay)
	})

	t.Run("defers a subscription whose path selects every endpoint", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, wildNodeLabel(), 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.relabel(t)
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
	})

	t.Run("defers a subscription whose event path selects a targeted endpoint", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribeOn(t, d.pair, []im.ConcreteAttributePath{nodeLabelAt(0)},
			[]im.ConcreteEventPath{{Endpoint: d.lightEP, Cluster: onoff.ClusterID, Event: 0, HasEndpoint: true, HasCluster: true, HasEvent: true}}, 60)
		start := d.now()
		d.toggle(t, d.lightEP, drd(1000, 0))
		d.relabel(t)
		d.expectReportAt(t, r, start, time.Second, time.Second+sendDelay)
	})

	t.Run("does not defer when no command of the invoke can be dispatched", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, d.onOffPath(), 60)
		start := d.now()
		// No OnOff cluster on endpoint 0.
		d.toggle(t, 0, drd(1000, 0))
		d.setOnOff(t, true)
		d.expectReportAt(t, r, start, 0, sendDelay)
	})

	t.Run("does not defer a subscription of every endpoint when no command can be dispatched", func(t *testing.T) {
		t.Parallel()
		d := newDeferHost(t)
		r := d.subscribe(t, wildNodeLabel(), 60)
		start := d.now()
		d.toggle(t, 0, drd(1000, 0))
		d.relabel(t)
		d.expectReportAt(t, r, start, 0, sendDelay)
	})
}

func nodeLabelAt(ep uint16) im.ConcreteAttributePath {
	return attrPath(ep, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
}

func wildNodeLabel() im.ConcreteAttributePath {
	p := nodeLabelAt(0)
	p.HasEndpoint = false
	return p
}

// deferHost is a commissioned device with one OnOffLight, matter.js's
// createNode (DelayReportDataTest.ts:48-53).
type deferHost struct {
	*invokeHost
	lightEP uint16
}

func newDeferHost(t *testing.T, more ...endpointSpec) *deferHost {
	t.Helper()
	h := newInvokeHostOn(t, steppedSite(t), store.PrivilegeAdminister, append([]endpointSpec{lightSpec("light:1")}, more...)...)
	return &deferHost{invokeHost: h, lightEP: h.ep("light:1")}
}

func (d *deferHost) onOffPath() im.ConcreteAttributePath {
	return attrPath(d.lightEP, onoff.ClusterID, onoff.AttrOnOff)
}

func (d *deferHost) now() time.Time { return d.site.Clock().Now() }

// reports records the time of every report after the priming one.
type reports struct {
	mu sync.Mutex
	at []time.Time
}

func (r *reports) times() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.at...)
}

// subscribe is matter.js's subscribe (DelayReportDataTest.ts:55-64): one
// path, MinIntervalFloor 0, the given MaxIntervalCeiling in seconds.
func (d *deferHost) subscribe(t *testing.T, path im.ConcreteAttributePath, ceiling int) *reports {
	t.Helper()
	return d.subscribeOn(t, d.pair, []im.ConcreteAttributePath{path}, nil, ceiling)
}

func (d *deferHost) subscribeOn(t *testing.T, p *fabrictest.Pair, paths []im.ConcreteAttributePath, events []im.ConcreteEventPath, ceiling int) *reports {
	t.Helper()
	r := &reports{}
	primed := false
	sub, err := fabrictest.Subscribe(parityCtx(t), p, interaction.SubscribeRequest{
		Attributes: paths, Events: events,
		MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(time.Duration(ceiling) * time.Second),
		Updated: func(*interaction.Report) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !primed {
				primed = true
				return
			}
			r.at = append(r.at, d.site.Clock().Now())
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)
	return r
}

// toggle invokes OnOff Toggle on ep with delay — matter.js toggle
// (DelayReportDataTest.ts:69-89).
func (d *deferHost) toggle(t *testing.T, ep uint16, delay *im.DelayReportData) {
	t.Helper()
	d.invoke(t, cmdPath(ep, onoff.ClusterID, onoff.CmdToggle), delay)
}

func (d *deferHost) invoke(t *testing.T, path im.ConcreteCommandPath, delay *im.DelayReportData) {
	t.Helper()
	if a := rawInvoke(t, d.pair, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: path}}, DelayReportData: delay}); a.status != nil {
		t.Fatalf("invoke: StatusResponse 0x%02x", uint8(*a.status))
	}
}

// setOnOff changes the light's OnOff locally — matter.js setStateOf.
func (d *deferHost) setOnOff(t *testing.T, on bool) {
	t.Helper()
	if err := d.pair.Device.Act(func(*node.Node) error { d.lights["light:1"].Set(on); return nil }); err != nil {
		t.Fatal(err)
	}
}

// relabel changes NodeLabel — matter.js node.set({ basicInformation:
// { nodeLabel } }), here a write over the wire.
func (d *deferHost) relabel(t *testing.T) {
	t.Helper()
	if err := fabrictest.Write(parityCtx(t), d.pair, nodeLabelAt(0), strVal("relabeled")); err != nil {
		t.Fatal(err)
	}
}

// advanceTo steps the site's clock to at, deadline by deadline, letting the
// site settle after each step so a report reaches the controller at the
// time it was sent.
func (d *deferHost) advanceTo(t *testing.T, at time.Time) {
	t.Helper()
	ctx := parityCtx(t)
	fake := d.site.Fake()
	for {
		if err := d.site.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		next, ok := fake.Next()
		if !ok || next.After(at) {
			break
		}
		fake.Advance(max(next.Sub(fake.Now()), 0))
	}
	if rest := at.Sub(fake.Now()); rest > 0 {
		fake.Advance(rest)
	}
	if err := d.site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
}

// expectReportAt is matter.js expectReportAt (DelayReportDataTest.ts:162-171):
// no new report before earliest after start, one by latest, sent within
// [earliest, latest].
func (d *deferHost) expectReportAt(t *testing.T, r *reports, start time.Time, earliest, latest time.Duration) {
	t.Helper()
	before := len(r.times())
	if earliest > 0 {
		d.advanceTo(t, start.Add(earliest-time.Millisecond))
		if got := r.times(); len(got) != before {
			t.Fatalf("a report at %v, before %v", got[before].Sub(start), earliest)
		}
	}
	d.advanceTo(t, start.Add(latest))
	got := r.times()
	if len(got) == before {
		t.Fatalf("no report by %v", latest)
	}
	if at := got[before].Sub(start); at < earliest || at > latest {
		t.Fatalf("the report at %v, want within [%v, %v]", at, earliest, latest)
	}
}
