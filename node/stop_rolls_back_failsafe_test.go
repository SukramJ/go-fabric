// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"net"
	"testing"

	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/controller/commissioner"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/secure/sessionparams"
)

// TestStopRollsBackAnArmedFailSafe (review II, R2-P6-fsm-2411, D-comm-4
// (d)): Stop with a fail-safe armed after AddNOC — CommissioningComplete
// never sent — expires it and runs its rollback: the uncommitted fabric
// goes, and the host hears OnFabricRemoved before Stop returns, as
// matter.js's DeviceCommissioner.close closes the fail-safe context,
// which rolls back while armed (DeviceCommissioner.ts:326-340,
// FailsafeContext.ts:194-203 at db021da0).
func TestStopRollsBackAnArmedFailSafe(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	u := site.UncommissionedPair(fabrictest.PairOptions{})
	n := u.Device.Node()
	var rec removalRecorder
	rec.observe(n)
	ctx := parityCtx(t)
	err := site.Run(ctx, func(ctx context.Context) error {
		_, err := u.Controller.Commissioner().Commission(ctx, commissioner.Options{
			Fabric: u.Controller.Fabric(), Passcode: u.Device.Passcode(), Addresses: []*net.UDPAddr{u.Device.Addr()},
			FinalizeCommissioning: func(context.Context, handshake.PeerAddress, *discovery.Device, *sessionparams.Parameters) error {
				return nil
			},
		})
		return err
	})
	if err != nil {
		t.Fatalf("commissioning up to the transition: %v", err)
	}
	if f, err := n.FabricInfos(ctx); err != nil || len(f) != 1 || !n.Root().GeneralCommissioning.FailSafeArmed() {
		t.Fatalf("fabrics %+v (%v), armed %v after AddNOC; want one, armed", f, err, n.Root().GeneralCommissioning.FailSafeArmed())
	}
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("OnFabricRemoved before Stop: %+v", got)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := rec.take(); len(got) != 1 {
		t.Fatalf("OnFabricRemoved by the time Stop returned: %+v; want once, for the uncommitted fabric", got)
	}
}
