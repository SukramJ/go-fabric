// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
)

// TestBridgedNodeLabelWriteSticks pins a written
// BridgedDeviceBasicInformation NodeLabel: it reads back (the server is
// rebuilt per dispatch, so the label lives in the endpoint's state), it
// survives a reassembly, it reaches the host through OnNodeLabelWritten,
// and a label the host restores through Spec.NodeLabel is served after a
// restart. matter.js keeps a written nodeLabel in the endpoint's persisted
// state. Found by the CHIP Python harness: TC-IDM-4.3 writes NodeLabel on
// every bridged endpoint and waits for the new value in a report — the
// write answered SUCCESS and the value never changed.
func TestBridgedNodeLabelWriteSticks(t *testing.T) {
	t.Parallel()
	var written []string
	cfg := endpoint.Config{
		VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "Bridge",
		OnNodeLabelWritten: func(key endpoint.SourceKey, label string) {
			written = append(written, string(key.(endpoint.StringKey))+"="+label)
		},
	}
	store := endpointtest.NewFakeStore()
	a, err := endpoint.New(store, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := endpoint.Spec{
		StableKey: endpoint.StringKey("k1"), FriendlyName: "Flow Meter",
		DeviceType: contract.MeasurementClassDeviceType(contract.MeasurementFlow), Measurement: labelFlow{},
	}
	assemble := func(a *endpoint.Assembler, s endpoint.Spec) (*endpoint.TopologyDispatcher, uint16) {
		t.Helper()
		topo, err := a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "s", ModelComplete: true, Endpoints: []endpoint.Spec{s}}})
		if err != nil {
			t.Fatal(err)
		}
		return endpoint.NewTopologyDispatcher(topo), topo.Bridged()[0].ID
	}
	label := func(d *endpoint.TopologyDispatcher, ep uint16) any {
		t.Helper()
		res := d.Read(context.Background(), im.ConcreteAttributePath{
			Endpoint: ep, Cluster: 0x0039, Attribute: 0x0005,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		})
		return res[0].Value.Value
	}

	d, ep := assemble(a, spec)
	if got := label(d, ep); got != "Flow Meter" {
		t.Fatalf("initial NodeLabel %v", got)
	}
	res := d.Write(context.Background(), im.ConcreteAttributePath{
		Endpoint: ep, Cluster: 0x0039, Attribute: 0x0005,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}, im.AttributeValue{Value: "Garden"})
	if len(res) != 1 || res[0].Status != im.StatusSuccess {
		t.Fatalf("write %+v", res)
	}
	if got := label(d, ep); got != "Garden" {
		t.Errorf("NodeLabel after the write = %v, want Garden", got)
	}
	if len(written) != 1 || written[0] != "k1=Garden" {
		t.Errorf("OnNodeLabelWritten saw %v", written)
	}
	if d2, ep2 := assemble(a, spec); label(d2, ep2) != "Garden" {
		t.Error("the written label did not survive a reassembly")
	}

	// A restart: a new assembler, the host restoring what it persisted.
	a2, err := endpoint.New(store, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored := spec
	restored.NodeLabel = "Garden"
	if d3, ep3 := assemble(a2, restored); label(d3, ep3) != "Garden" {
		t.Error("the restored label was not served after a restart")
	}
}

// labelFlow is a flow reading, the simplest bridged endpoint there is.
type labelFlow struct{}

func (labelFlow) MatterMeasurementClass() contract.MeasurementClass { return contract.MeasurementFlow }
func (labelFlow) MatterFloatValue() (float64, bool)                 { return 1.5, true }
