// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im/subscription"
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
// Datasource.ts:679-688) — ColorControl's remainingTime alone
// (packages/node/src/behaviors/color-control/ColorControlServer.ts:1967-1984).
// A change a controller's write made keeps the dispatcher's advance, and
// a change while a command runs against the cluster advances at its report
// when the command's commit did not report it.
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
		h.bridge.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
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
		if held := h.bridge.invokeCommits.end(key); len(held) != 0 {
			t.Fatalf("the command's commit holds %v, want nothing reported", held)
		}
		srv.ReportFrom(contract.OriginCommand, ccdef.AttrCurrentHue)
		step("its report after the command", 4)
	})
}
