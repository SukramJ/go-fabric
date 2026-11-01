// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"

	cctl "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	cdim "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
)

// TestTimedCommandsFromTheModel drives the fleet's real EVSE, closure panel
// and garage through the IM invoke path: a request command the generated
// definition marks "T" (EnergyEvse.EnableCharging, ClosureDimension.SetTarget,
// ClosureControl.MoveTo) answers NEEDS_TIMED_INTERACTION outside a timed
// interaction and passes the gate inside one, as matter.js reads
// `limits.timed` from the command's model
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:292-295).
// Each server answers the question itself (contract.TimedCommander); the
// untimed ClosureControl.Stop stays ungated.
func TestTimedCommandsFromTheModel(t *testing.T) {
	_, br := startFleetBridge(t)
	topo := br.Topology()
	d := endpoint.NewTopologyDispatcher(topo)

	find := func(clusterID uint32) (uint16, contract.ClusterServer) {
		for _, ep := range topo.Endpoints {
			if ep == nil {
				continue
			}
			for _, srv := range endpoint.ClusterServers(ep) {
				if srv != nil && srv.MatterClusterID() == clusterID {
					return ep.ID, srv
				}
			}
		}
		t.Fatalf("the fleet mounts no cluster 0x%04X", clusterID)
		return 0, nil
	}

	cases := []struct {
		name    string
		cluster uint32
		command uint32
		timed   bool
	}{
		{"EnergyEvse.EnableCharging", evse.ClusterID, evse.CmdEnableCharging, true},
		{"ClosureDimension.SetTarget", cdim.ClusterID, cdim.CmdSetTarget, true},
		{"ClosureControl.MoveTo", cctl.ClusterID, cctl.CmdMoveTo, true},
		{"ClosureControl.Stop", cctl.ClusterID, cctl.CmdStop, false},
	}
	for _, tc := range cases {
		epID, srv := find(tc.cluster)
		tcSrv, ok := srv.(contract.TimedCommander)
		if !ok {
			t.Fatalf("%s: server %T does not implement contract.TimedCommander", tc.name, srv)
		}
		if got := tcSrv.IsTimedCommand(tc.command); got != tc.timed {
			t.Errorf("%s: server IsTimedCommand = %v, want %v", tc.name, got, tc.timed)
		}
		// The decode status stops the command right after the gates, so a
		// passed gate is visible without driving the device.
		inv := im.CommandInvocation{
			Path: im.ConcreteCommandPath{
				Endpoint: epID, Cluster: tc.cluster, Command: tc.command,
				HasEndpoint: true, HasCluster: true, HasCommand: true,
			},
			DecodeStatus: im.StatusConstraintError,
		}
		req := im.InvokeRequest{Invokes: []im.CommandInvocation{inv}}
		for _, timed := range []bool{false, true} {
			// Over PASE: the ACL gate fails closed for a request without a
			// fabric, and the gates under test follow it.
			ctx := im.WithAuthModePASE(im.WithTimedInteraction(context.Background(), timed))
			resp := im.HandleInvokeRequest(ctx, d, req)
			if len(resp.Responses) != 1 || !resp.Responses[0].IsStatus {
				t.Fatalf("%s timed=%v: responses %+v", tc.name, timed, resp.Responses)
			}
			want := im.StatusConstraintError
			if tc.timed && !timed {
				want = im.StatusNeedsTimedInteraction
			}
			if got := resp.Responses[0].Status.Status; got != want {
				t.Errorf("%s timed=%v: status 0x%02X, want 0x%02X", tc.name, timed, got, want)
			}
		}
	}
}
