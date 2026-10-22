// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
)

// oneServer is a bridged endpoint's source holding one server.
type oneServer struct{ srv contract.ClusterServer }

func (o oneServer) MatterDeviceType() uint16 { return 0x0013 } // BridgedNode
func (o oneServer) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{o.srv}
}

// dispatched answers a controller's write of attrID and invoke of cmdID
// on srv through the endpoint dispatcher, the path a controller's request
// takes: the status is the one the dispatcher reads from the server's
// error by type (endpoint/dispatcher.go classifyError).
func dispatched(t *testing.T, srv contract.ClusterServer, attrID, cmdID uint32) (write, invoke im.StatusCode) {
	t.Helper()
	topo := &endpoint.Topology{Endpoints: []*endpoint.Endpoint{
		{ID: 0}, {ID: 1, DeviceType: 0x000E}, {ID: 2, Source: oneServer{srv}},
	}}
	d := endpoint.NewTopologyDispatcher(topo)
	cluster := srv.MatterClusterID()
	// A fabric's request: the Groups and ScenesManagement commands are
	// fabric-scoped and are refused for no fabric before the command is.
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	results := d.Write(ctx, im.ConcreteAttributePath{
		Endpoint: 2, Cluster: cluster, Attribute: attrID,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}, im.AttributeValue{Value: uint8(1)})
	if len(results) != 1 {
		t.Fatalf("cluster 0x%04X: %d write results", cluster, len(results))
	}
	r := d.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 2, Cluster: cluster, Command: cmdID, HasEndpoint: true}, nil)
	return results[0].Status, r.Status
}

// TestRefusalsAnswerTheirStatus: the servers whose refusal once read as
// "read-only" or "unknown command" text (gate 2 G2-10, the dispatcher's
// text fallback gone) answer a write of a read-only attribute with
// UNSUPPORTED_WRITE and an unknown command with UNSUPPORTED_COMMAND, not
// FAILURE.
func TestRefusalsAnswerTheirStatus(t *testing.T) {
	t.Parallel()
	scenes, err := core.NewScenesManagement(core.ScenesConfig{State: core.NewScenesState(nil)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		srv    contract.ClusterServer
		attrID uint32
	}{
		{"ICDManagement", core.NewICDManagement(), 0x0000},
		{"NetworkCommissioning", core.NewNetworkCommissioning(core.NetworkCommissioningConfig{}), 0x0000},
		{"Groups", newGroupsFixture(t, 4).ep3, 0x0000},
		{"ScenesManagement", scenes, 0x0001},
		{"GeneralDiagnostics", core.NewGeneralDiagnostics(0), 0x0000},
		{"Schedules", wire.NewSchedulesServer(nil), wire.SchedulesAttrSchedules},
		{"ScenesManagement stub", wire.ScenesManagement{}, 0x0001},
	} {
		write, invoke := dispatched(t, tc.srv, tc.attrID, 0xFE)
		if write != im.StatusUnsupportedWrite {
			t.Errorf("%s: write of a read-only attribute answers %v, want UNSUPPORTED_WRITE", tc.name, write)
		}
		if invoke != im.StatusUnsupportedCommand {
			t.Errorf("%s: an unknown command answers %v, want UNSUPPORTED_COMMAND", tc.name, invoke)
		}
	}
	// DiagnosticLogs serves no attribute of its own: a write of one it does
	// not serve is UNSUPPORTED_ATTRIBUTE, as the definition judges it.
	write, invoke := dispatched(t, core.NewDiagnosticLogs(), 0x0000, 0xFE)
	if write != im.StatusUnsupportedAttribute || invoke != im.StatusUnsupportedCommand {
		t.Errorf("DiagnosticLogs: write %v, invoke %v; want UNSUPPORTED_ATTRIBUTE, UNSUPPORTED_COMMAND", write, invoke)
	}
}
