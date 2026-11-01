// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
)

// --- fakes ---

type fakeSource struct {
	servers []contract.ClusterServer
}

func (f fakeSource) MatterDeviceType() uint16 { return 0x010A }
func (f fakeSource) MatterClusterServers() []contract.ClusterServer {
	return f.servers
}

type fakeBoolMeasurement struct{}

func (fakeBoolMeasurement) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementOccupancy
}
func (fakeBoolMeasurement) MatterBoolValue() (value, observed bool) { return true, true }

type fakeServer struct{ id uint32 }

func (s fakeServer) MatterClusterID() uint32 { return s.id }
func (s fakeServer) MatterRead(_ uint32) (any, bool) {
	return nil, false
}

func (s fakeServer) MatterWrite(_ context.Context, _ uint32, _ any) error {
	return errors.New("read-only")
}

func (s fakeServer) MatterInvoke(_ context.Context, _ uint32, _ any) (any, error) {
	return nil, errors.New("no commands")
}
func (s fakeServer) MatterReportable() []uint32 { return nil }

// --- tests ---

// fakeMomentarySwitchSource implements both
// [contract.MeasurementSource] (with MomentarySwitch) and
// the wire.GenericSwitchSource shape, mirroring how a HM Button
// surfaces in the live model. The struct lives in the test file so
// it can pretend to be a Button without pulling the model package.
type fakeMomentarySwitchSource struct {
	supportsLong bool
}

func (f fakeMomentarySwitchSource) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementMomentarySwitch
}
func (f fakeMomentarySwitchSource) MatterSwitchPositions() uint8        { return 2 }
func (f fakeMomentarySwitchSource) MatterSwitchSupportsLongPress() bool { return f.supportsLong }

// TestClusterServersNilEndpoint verifies that a nil endpoint returns nil.
func TestClusterServersNilEndpoint(t *testing.T) {
	t.Parallel()
	if got := ClusterServers(nil); got != nil {
		t.Errorf("ClusterServers(nil) = %v, want nil", got)
	}
}

// TestClusterServersMomentarySwitch verifies that an endpoint whose
// measurement source advertises MomentarySwitch + the GenericSwitchSource
// shape gets a single GenericSwitch (cluster ID 0x003B) cluster
// server materialised against the endpoint ID.
func TestClusterServersMomentarySwitch(t *testing.T) {
	t.Parallel()
	ep := &Endpoint{
		ID:          17,
		Measurement: fakeMomentarySwitchSource{supportsLong: true},
	}
	servers := ClusterServers(ep)
	// A bridged endpoint's surface: Identify, the inner cluster, the
	// Descriptor and BridgedDeviceBasicInformation (an unnamed endpoint
	// serves an empty NodeLabel; it keeps its Descriptor since 2026-10-30).
	if ids := clusterIDsOf(servers); !hasID(ids, 0x003B) || !hasID(ids, 0x001D) || !hasID(ids, 0x0039) {
		t.Fatalf("ClusterServers() = %#x, want GenericSwitch 0x003B, Descriptor and BridgedDeviceBasicInformation", ids)
	}
}

// TestClusterServersRootReturnsNil verifies that the root endpoint (ID=0) always returns nil.
func TestClusterServersRootReturnsNil(t *testing.T) {
	t.Parallel()
	ep := &Endpoint{
		ID:     0,
		Source: fakeSource{servers: []contract.ClusterServer{fakeServer{id: 0x1234}}},
	}
	if got := ClusterServers(ep); got != nil {
		t.Errorf("ClusterServers(root) = %v, want nil", got)
	}
}

// TestClusterServersFromSource verifies that a Source-backed endpoint returns a fresh independent copy.
func TestClusterServersFromSource(t *testing.T) {
	t.Parallel()
	sentinel := fakeServer{id: 0x1234}
	ep := &Endpoint{
		ID:     2,
		Source: fakeSource{servers: []contract.ClusterServer{sentinel}},
	}

	first := ClusterServers(ep)
	if ids := clusterIDsOf(first); !hasID(ids, 0x1234) || !hasID(ids, 0x001D) || !hasID(ids, 0x0039) {
		t.Fatalf("ClusterServers() = %#x, want the source's 0x1234, Descriptor and BridgedDeviceBasicInformation", ids)
	}

	// Mutate the returned slice to verify independence.
	first[0] = fakeServer{id: 0xDEAD}

	second := ClusterServers(ep)
	if ids := clusterIDsOf(second); len(second) != len(first) || hasID(ids, 0xDEAD) {
		t.Errorf("second call: %#x — ClusterServers not returning a fresh slice", ids)
	}
}

// TestClusterServersFromMeasurement verifies that a Measurement-backed endpoint returns OccupancySensing.
func TestClusterServersFromMeasurement(t *testing.T) {
	t.Parallel()
	ep := &Endpoint{
		ID:          2,
		Measurement: fakeBoolMeasurement{},
	}
	servers := ClusterServers(ep)
	// OccupancySensing cluster ID per Matter Application Cluster Spec 1.5.1.
	const wantID uint32 = 0x0406
	if ids := clusterIDsOf(servers); !hasID(ids, wantID) || !hasID(ids, 0x001D) || !hasID(ids, 0x0039) {
		t.Errorf("ClusterServers() = %#x, want OccupancySensing 0x%04X, Descriptor and BridgedDeviceBasicInformation", ids, wantID)
	}
}

// TestClusterServersBothNilReturnsNil verifies that an endpoint with neither Source nor Measurement returns nil.
func TestClusterServersBothNilReturnsNil(t *testing.T) {
	t.Parallel()
	// Use ID >= 2 (bridged range) to avoid taking the root/aggregator branch.
	ep := &Endpoint{ID: 3}
	if got := ClusterServers(ep); got != nil {
		t.Errorf("ClusterServers(no source/measurement) = %v, want nil", got)
	}
}

// TestBridgedNodeRevisionFromSchema asserts that bridged endpoints
// emit `BridgedNode` (0x0013) with the revision sourced from the
// codegen'd schema table, NOT a hardcoded constant. Mirrors the V3.1
// audit P1-6 fix: previously `materialize.go` had `Revision: 3`
// hardcoded, which would silently desync the next time matter.js
// bumps the bridged-node revision and the schema is regenerated.
// The lookup goes via `deviceTypeRevision(0x0013)` →
// `schema.DeviceTypeRevision(0x13)`.
func TestBridgedNodeRevisionFromSchema(t *testing.T) {
	t.Parallel()
	// matter.js HEAD `bridged-node.element.ts` defaults revision = 3
	// at Matter 1.5.1. The codegen'd schema lookup MUST match.
	got := deviceTypeRevision(uint16(matterDeviceTypeBridgedNode))
	if got == 1 {
		// `deviceTypeRevision` falls back to 1 when the schema entry
		// is missing — that would be a regression. Schema codegen must
		// produce an entry for 0x0013.
		t.Errorf("deviceTypeRevision(0x0013) = 1 (schema fallback); want explicit schema entry")
	}
	if got < 3 {
		t.Errorf("deviceTypeRevision(0x0013) = %d; matter.js HEAD baseline is 3 (Matter 1.5)", got)
	}
}

// TestClusterServersBridgedNodeHostTypeListedOnce verifies that a bridged
// endpoint whose host device type is BridgedNode itself lists BridgedNode
// once in its Descriptor's DeviceTypeList — the Descriptor keeps the first
// entry of a device type (matter.js 0ceb202d packages/node/src/behaviors/
// descriptor/DescriptorServer.ts:104-108).
func TestClusterServersBridgedNodeHostTypeListedOnce(t *testing.T) {
	t.Parallel()
	dt := uint16(matterDeviceTypeBridgedNode)
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{{
		StableKey: StringKey("node"), DeviceType: dt, FriendlyName: "Node",
		Source: deviceTypeSource{dt: dt, servers: []contract.ClusterServer{dtServer{id: 0x0006}}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range topo.Endpoints {
		if ep.IsRoot() || ep.IsAggregator() {
			continue
		}
		for _, srv := range ClusterServers(ep) {
			if srv.MatterClusterID() != 0x001D {
				continue
			}
			v, _ := srv.MatterRead(0x0000)
			list, _ := v.([]mattercore.DeviceTypeStruct)
			if len(list) != 1 || list[0].DeviceType != matterDeviceTypeBridgedNode {
				t.Errorf("DeviceTypeList = %v, want BridgedNode once", list)
			}
			return
		}
		t.Fatalf("endpoint %d has no Descriptor", ep.ID)
	}
	t.Fatal("no bridged endpoint assembled")
}

// clusterIDsOf lists the cluster ids of servers in order.
func clusterIDsOf(servers []contract.ClusterServer) []uint32 {
	ids := make([]uint32, 0, len(servers))
	for _, s := range servers {
		ids = append(ids, s.MatterClusterID())
	}
	return ids
}

func hasID(ids []uint32, id uint32) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
