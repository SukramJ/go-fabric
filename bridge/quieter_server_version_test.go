// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/levelcontrol"
	"thingfabric.dev/go-fabric/cluster/opstate"
	ccdef "thingfabric.dev/go-fabric/cluster/spec/colorcontrol"
	lvldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/tlv"
)

// colourServer is a bridged ColorControl server that reports as
// cluster/light's does: every change to its change listeners at once, the
// quieter ("Q") attributes to its report listeners by their throttle.
type colourServer struct {
	cluster.AttributeChanges
}

func (*colourServer) MatterClusterID() uint32 { return ccdef.ClusterID }

func (*colourServer) MatterRead(uint32) (any, bool)                          { return uint8(0), true }
func (*colourServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*colourServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*colourServer) MatterReportable() []uint32                             { return nil }

// TestQuieterServerAttributeAdvancesAtTheChange: on a bridged endpoint the
// DataVersion of a cluster whose server throttles a quieter attribute's
// report advances when the attribute changes — one advance for a change of
// a quieter and a plain attribute together —, and the throttle's report
// advances nothing more; RemainingTime, which matter.js serves from an
// accessor, advances at its report, which is its change. matter.js
// advances the version at the commit that changed a value, a quieter one's
// included (packages/node/src/behavior/state/managed/Datasource.ts:1456-1457
// at db021da0), and at a quiet emit only for a dynamic property
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:258-261,
// Datasource.ts:679-688) — of the two clusters here, ColorControl's
// remainingTime
// (packages/node/src/behaviors/color-control/ColorControlServer.ts:1967-1984).
// A change a controller's write made keeps the dispatcher's advance, and
// a change while a command runs against the cluster advances at the
// command's end, whether or not its report went out inside the command,
// and its report after the command advances nothing (review II,
// R2-P4-fr-im-02).
func TestQuieterServerAttributeAdvancesAtTheChange(t *testing.T) {
	t.Parallel()

	t.Run("OperationalState CountdownTime", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.Unix(5000, 0))
		srv, err := opstate.NewServer(opstate.Config{
			States:        []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateError}},
			CountdownTime: true, Clock: clk,
		})
		if err != nil {
			t.Fatal(err)
		}
		h := newAppHarness(t, appDevice{deviceType: 0x0073, servers: []contract.ClusterServer{srv}})
		h.bridge.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
		ep := h.bridge.Topology().FindByID(h.endpoints[0x0073])
		v0 := ep.ClusterDataVersion(srv.MatterClusterID())
		step := func(when string, advance uint32) {
			t.Helper()
			if got := ep.ClusterDataVersion(srv.MatterClusterID()); got != v0+advance {
				t.Fatalf("%s: DataVersion %d, want %d", when, got, v0+advance)
			}
		}
		u := func(v uint32) *uint32 { return &v }
		if err := srv.SetCountdownTime(u(600)); err != nil {
			t.Fatal(err)
		}
		step("from null, reported at once", 1)
		if err := srv.SetCountdownTime(u(599)); err != nil {
			t.Fatal(err)
		}
		step("a change the throttle holds", 2)
		clk.Advance(time.Second)
		step("the throttle's deferred report", 2)
	})

	t.Run("ColorControl", func(t *testing.T) {
		t.Parallel()
		srv := &colourServer{}
		h := newAppHarness(t, appDevice{deviceType: 0x0073, servers: []contract.ClusterServer{srv}})
		mgr := subscription.NewManager(subscription.Config{}, nil, nil)
		h.bridge.AttachSubscriptionManager(mgr)
		ep := h.bridge.Topology().FindByID(h.endpoints[0x0073])
		v0 := ep.ClusterDataVersion(ccdef.ClusterID)
		step := func(when string, advance uint32) {
			t.Helper()
			if got := ep.ClusterDataVersion(ccdef.ClusterID); got != v0+advance {
				t.Fatalf("%s: DataVersion %d, want %d", when, got, v0+advance)
			}
		}
		host := contract.OriginHostSet
		srv.ChangedFrom(host, ccdef.AttrCurrentHue, ccdef.AttrColorMode)
		srv.ReportFrom(host, ccdef.AttrColorMode)
		step("a quieter and a plain attribute in one change", 1)
		srv.ChangedFrom(host, ccdef.AttrCurrentHue)
		step("a change the throttle holds", 2)
		srv.ReportFrom(host, ccdef.AttrCurrentHue)
		step("the throttle's report", 2)
		srv.NotifyFrom(contract.OriginCommand, ccdef.AttrRemainingTime)
		step("RemainingTime", 3)
		srv.NotifyFrom(contract.OriginControllerWrite, ccdef.AttrColorMode)
		step("a controller's write", 3)

		key := invokedCluster{ep.ID, ccdef.ClusterID}
		h.bridge.invokeCommits.begin(key)
		srv.ChangedFrom(contract.OriginCommand, ccdef.AttrCurrentHue)
		step("a change while a command runs", 3)
		held, advance := h.bridge.invokeCommits.end(key)
		if len(held) != 0 || !advance {
			t.Fatalf("the command's commit holds %v, advance %v; want nothing reported and the advance owed", held, advance)
		}
		reportCommandChange(h.bridge.Topology(), mgr, key, held, advance)
		step("the command's end", 4)
		srv.ReportFrom(contract.OriginCommand, ccdef.AttrCurrentHue)
		step("its report after the command", 4)
	})
}

// engineLevelPort is a dimmer whose level moves when the server hands it a
// MoveToLevel, as the kit's transition engine drives a host.
type engineLevelPort struct {
	notifyingLevelPort
	level uint8
}

func (p *engineLevelPort) CurrentLevel() (uint8, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.level, true
}

func (p *engineLevelPort) MoveToLevel(_ context.Context, req lvldef.MoveToLevelRequest) error {
	p.mu.Lock()
	p.level = req.Level
	p.mu.Unlock()
	return nil
}

// TestQuieterChangeDuringACommandAdvancesAtTheCommand: CurrentLevel of the
// kit's engine LevelControl without LT, changed by two MoveToLevel within
// one quiet interval, advances the cluster's DataVersion at each command —
// the second change too, whose report the throttle holds back — and a
// re-wiring before the throttle's report loses no advance. matter.js
// advances the version at the commit that changed a value
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0), a quieter property's included, whenever its report goes out.
func TestQuieterChangeDuringACommandAdvancesAtTheCommand(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(5000, 0))
	port := &engineLevelPort{level: 10}
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: port, Transitions: &levelcontrol.Transitions{Clock: clk}})
	h := newAppHarness(t, appDevice{deviceType: 0x0101, servers: []contract.ClusterServer{srv}})
	h.bridge.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	epID := h.endpoints[0x0101]
	ep := h.bridge.Topology().FindByID(epID)
	v0 := ep.ClusterDataVersion(lvldef.ClusterID)
	step := func(when string, advance uint32) {
		t.Helper()
		if got := ep.ClusterDataVersion(lvldef.ClusterID); got != v0+advance {
			t.Fatalf("%s: DataVersion %d, want %d", when, got, v0+advance)
		}
	}
	moveTo := func(level uint64) {
		t.Helper()
		reply := h.invoke(epID, lvldef.ClusterID, lvldef.CmdMoveToLevel, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), level)
			enc.PutUint(tlv.ContextTag(1), 0)
			enc.PutUint(tlv.ContextTag(2), 0)
			enc.PutUint(tlv.ContextTag(3), 0)
		})
		if _, _, status, _ := invokeResult(t, reply); status != 0 {
			t.Fatalf("MoveToLevel %d: status 0x%02X", level, status)
		}
		if l, _ := port.CurrentLevel(); uint64(l) != level {
			t.Fatalf("MoveToLevel %d: the port is at %d", level, l)
		}
	}
	moveTo(50)
	step("the first MoveToLevel", 1)
	moveTo(100)
	step("a second MoveToLevel within the quiet interval", 2)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := h.bridge.Reassemble(ctx); err != nil {
		t.Fatal(err)
	}
	clk.Advance(cluster.QuietMinimumInterval)
	step("the throttle's report after a re-wiring", 2)
}
