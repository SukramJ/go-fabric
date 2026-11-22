// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	gencommdef "thingfabric.dev/go-fabric/cluster/spec/generalcommissioning"
	groupsdef "thingfabric.dev/go-fabric/cluster/spec/groups"
	opcredsdef "thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// answeringServer is a server of cluster id whose every command answers
// resp: a hand-written cluster/core server's shape as the dispatcher sees
// it.
type answeringServer struct {
	handFormServer
	resp any
}

func (s answeringServer) MatterInvoke(context.Context, uint32, any) (any, error) { return s.resp, nil }

// TestResponseCheckReachesEveryServer (decision 339, H-2 of the P2-H
// post-read): the response check runs in the dispatcher with the request
// gate, after the server ([responseHeld]), so it reaches the hand-written
// cluster/core servers as every other: an absent response of a command
// whose response has a mandatory field is FAILURE (F-b25ed960-30, matter.js
// ProtocolService.ts:624-625, :717-733 at b25ed960), the generated form of
// a response is judged, a handler's deliberate NoResponse is not, and —
// with no response writer set (a host's own dispatcher) — a hand-written
// response form is not judged; the bridge sets its writer (decision 343).
func TestResponseCheckReachesEveryServer(t *testing.T) {
	for _, c := range []struct {
		name    string
		cluster uint32
		cmd     uint32
		resp    any
		status  im.StatusCode
	}{
		{"GeneralCommissioning ArmFailSafe, no response", gencommdef.ClusterID, gencommdef.CmdArmFailSafe, nil, im.StatusFailure},
		{"OperationalCredentials CertificateChainRequest, no response", opcredsdef.ClusterID, opcredsdef.CmdCertificateChainRequest, nil, im.StatusFailure},
		{"Groups AddGroup, no response", groupsdef.ClusterID, groupsdef.CmdAddGroup, nil, im.StatusFailure},
		{"Groups AddGroup, a nil generated response", groupsdef.ClusterID, groupsdef.CmdAddGroup, (*groupsdef.AddGroupResponse)(nil), im.StatusFailure},
		{"Groups AddGroup, the generated response", groupsdef.ClusterID, groupsdef.CmdAddGroup, groupsdef.AddGroupResponse{GroupId: 1}, im.StatusSuccess},
		{"GeneralCommissioning ArmFailSafe, the hand response (not judged)", gencommdef.ClusterID, gencommdef.CmdArmFailSafe, mattercore.ArmFailSafeResponse{}, im.StatusSuccess},
		{"Groups AddGroup, NoResponse", groupsdef.ClusterID, groupsdef.CmdAddGroup, im.NoResponse{}, im.StatusSuccess},
		{"GeneralCommissioning CommissioningComplete, no response", gencommdef.ClusterID, gencommdef.CmdCommissioningComplete, nil, im.StatusFailure},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := answeringServer{handFormServer{c.cluster}, c.resp}
			d := gateDispatcher(t, store.PrivilegeAdminister, srv)
			path := im.ConcreteCommandPath{Endpoint: 2, HasEndpoint: true, Cluster: c.cluster, Command: c.cmd}
			// A request the gate passes on to the server.
			req := spec.HandForm{Generated: opcredsdef.CertificateChainRequestRequest{CertificateType: 1}, Hand: mattercore.CertificateChainRequest{CertificateType: 1}}
			if res := d.Invoke(caseCtx(false), path, req); res.Status != c.status {
				t.Errorf("Invoke: %v, want %v", res.Status, c.status)
			}
			if res := d.InvokeAuthorized(caseCtx(false), im.ConcreteCommandPath{Cluster: c.cluster, Command: c.cmd}, req, nil); len(res) != 1 || res[0].Status != c.status {
				t.Errorf("InvokeAuthorized (wildcard, group): %+v, want one %v", res, c.status)
			}
			if err := responseHeld(context.Background(), srv, c.cmd, nil, c.resp, nil); spec.StatusOf(err) != c.status {
				t.Errorf("responseHeld: %v", err)
			}
		})
	}
}
