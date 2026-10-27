// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package threadbr_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tnd "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/thread"
)

// What each ThreadNetworkDiagnostics attribute reads follows connectedhomeip's
// DirectThreadNetworkDiagnosticsProvider.cpp at 6170af84 (DTNDP); the
// border router's records are the shapes of matter.js thread-br-client's
// OTBR fixtures (test/fixtures/otbr-rest/diagnostics.json.ts at 862ad75f):
// a router 0x4800 whose Route64 lists routers 5, 18 (itself), 29, 55 and
// 61, and a child.

func u16(v uint16) *uint16 { return &v }

func meshDiagnostics() thread.NodeDiagnostics {
	self := thread.DiagnosticResponse{
		ExtMACAddress: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x02},
		Rloc16:        u16(0x4800),
		Mode:          &thread.Mode{RxOnWhenIdle: true, FullThreadDevice: true, FullNetworkData: true},
		Route64: &thread.Route64{IDSequence: 198, Entries: []thread.Route64Entry{
			{RouterID: 5, LinkQualityOut: 3, LinkQualityIn: 2, RouteCost: 2},
			{RouterID: 18, LinkQualityOut: 0, LinkQualityIn: 0, RouteCost: 1},
			{RouterID: 29, LinkQualityOut: 3, LinkQualityIn: 3, RouteCost: 1},
		}},
		ChildTable:  []thread.ChildTableEntry{{TimeoutExponent: 12, TimeoutSeconds: 256, ChildID: 3, Mode: thread.Mode{}}},
		MacCounters: &thread.MacCounters{IfInUcastPkts: 13666, IfInBroadcastPkts: 25593, IfOutUcastPkts: 12232, IfOutBroadcastPkts: 8189, IfInErrors: 14},
		MleCounters: &thread.MleCounters{DetachedRole: 1, ChildRole: 2, RouterRole: 3, LeaderRole: 4, AttachAttempts: 5, PartitionIDChanges: 6, BetterPartitionAttachAttempts: 7, ParentChanges: 8},
	}
	router5 := thread.DiagnosticResponse{ExtMACAddress: []byte{0, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x05}, Rloc16: u16(5 << 10)}
	return thread.NodeDiagnostics{
		Role:       thread.RoleRouter,
		ExtAddress: self.ExtMACAddress,
		Rloc16:     0x4800,
		LeaderData: thread.LeaderData{PartitionID: 305419896, Weighting: 64, DataVersion: 147, StableDataVersion: 205, LeaderRouterID: 61},
		Self:       &self,
		Mesh:       []thread.DiagnosticResponse{self, router5},
	}
}

func newDiagnostics(t *testing.T, br *fakeBorderRouter, features tnd.Feature) *threadbr.DiagnosticsServer {
	t.Helper()
	s, err := threadbr.NewDiagnostics(context.Background(), threadbr.DiagnosticsConfig{BorderRouter: br, Features: features})
	mustT(t, err)
	return s
}

func read(t *testing.T, s *threadbr.DiagnosticsServer, id uint32) any {
	t.Helper()
	v, ok := s.MatterRead(id)
	if !ok {
		t.Fatalf("attribute 0x%04X not served", id)
	}
	return v
}

// DTNDP :98-176, :295-323, :606-689: a commissioned border router.
func TestDiagnosticsReadsTheBorderRouter(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.active, br.diag = chipDataset, meshDiagnostics()
	s := newDiagnostics(t, br, 0)
	for id, want := range map[uint32]any{
		tnd.AttrChannel:           uint16(11),
		tnd.AttrRoutingRole:       uint8(tnd.RoutingRoleRouter),
		tnd.AttrNetworkName:       "OpenThread",
		tnd.AttrPanId:             uint16(0xc115),
		tnd.AttrExtendedPanId:     uint64(0xdeaa00beef00caef),
		tnd.AttrPartitionId:       uint32(305419896),
		tnd.AttrWeighting:         uint16(64),
		tnd.AttrDataVersion:       uint16(147),
		tnd.AttrStableDataVersion: uint16(205),
		tnd.AttrLeaderRouterId:    uint8(61),
		tnd.AttrExtAddress:        uint64(0x0011223344556602),
		tnd.AttrRloc16:            uint16(0x4800),
	} {
		got := read(t, s, id)
		if r, ok := got.(tnd.RoutingRoleEnum); ok {
			got = uint8(r)
		}
		if got != want {
			t.Errorf("attribute 0x%04X = %v (%T), want %v", id, got, got, want)
		}
	}
	if mlp := read(t, s, tnd.AttrMeshLocalPrefix).([]byte); !bytes.Equal(mlp, []byte{64, 0xfd, 0xde, 0xad, 0x00, 0xbe, 0xef, 0x00, 0x00}) {
		t.Errorf("MeshLocalPrefix = %x", mlp)
	}
	if sp := read(t, s, tnd.AttrSecurityPolicy).(tnd.SecurityPolicy); sp.RotationTime != 0x02a0 || sp.Flags != 0xf7f8 {
		t.Errorf("SecurityPolicy = %+v", sp)
	}
	if m := read(t, s, tnd.AttrChannelPage0Mask).([]byte); !bytes.Equal(m, []byte{0x00, 0x1f, 0xff, 0xe0}) {
		t.Errorf("ChannelPage0Mask = %x", m)
	}
	c := read(t, s, tnd.AttrOperationalDatasetComponents).(tnd.OperationalDatasetComponents)
	if !c.ActiveTimestampPresent || c.PendingTimestampPresent || !c.MasterKeyPresent || !c.ChannelMaskPresent || c.DelayPresent || !c.PskcPresent {
		t.Errorf("OperationalDatasetComponents = %+v", c)
	}
	if faults, ok := read(t, s, tnd.AttrActiveNetworkFaultsList).([]any); !ok || len(faults) != 0 {
		t.Errorf("ActiveNetworkFaultsList = %v", faults)
	}
}

// DTNDP :184-293: the tables from the border router's own record.
func TestDiagnosticsTables(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.active, br.diag = chipDataset, meshDiagnostics()
	s := newDiagnostics(t, br, 0)
	routes := read(t, s, tnd.AttrRouteTable).(spec.List[tnd.RouteTableStruct])
	if len(routes) != 3 {
		t.Fatalf("RouteTable = %+v", routes)
	}
	if r := routes[0]; r.RouterId != 5 || r.Rloc16 != 0x1400 || r.ExtAddress != 0x0011223344556605 || r.NextHop != 5 ||
		r.PathCost != 2 || r.LqiIn != 2 || r.LqiOut != 3 || !r.Allocated || !r.LinkEstablished {
		t.Errorf("route to 5 = %+v", r)
	}
	if r := routes[1]; r.RouterId != 18 || r.LinkEstablished || r.NextHop != 63 || r.ExtAddress != 0x0011223344556602 {
		t.Errorf("route to itself = %+v", r)
	}
	if r := routes[2]; r.ExtAddress != 0 {
		t.Errorf("route to an unknown router = %+v", r)
	}
	neighbors := read(t, s, tnd.AttrNeighborTable).(spec.List[tnd.NeighborTableStruct])
	if len(neighbors) != 3 {
		t.Fatalf("NeighborTable = %+v", neighbors)
	}
	if n := neighbors[0]; !n.IsChild || n.Rloc16 != 0x4803 || n.RxOnWhenIdle || n.FullThreadDevice || !n.AverageRssi.Null || !n.LastRssi.Null {
		t.Errorf("child = %+v", n)
	}
	if n := neighbors[1]; n.IsChild || n.Rloc16 != 0x1400 || n.Lqi != 2 || !n.RxOnWhenIdle || !n.FullThreadDevice || !n.FullNetworkData {
		t.Errorf("router neighbor = %+v", n)
	}
}

// DTNDP :104-145: the role as chip maps otDeviceRole.
func TestDiagnosticsRoutingRole(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		role thread.DeviceRole
		mode *thread.Mode
		want tnd.RoutingRoleEnum
	}{
		{thread.RoleDisabled, nil, tnd.RoutingRoleUnspecified},
		{thread.RoleDetached, nil, tnd.RoutingRoleUnassigned},
		{thread.RoleRouter, nil, tnd.RoutingRoleRouter},
		{thread.RoleLeader, nil, tnd.RoutingRoleLeader},
		{thread.RoleChild, nil, tnd.RoutingRoleReed},
		{thread.RoleChild, &thread.Mode{RxOnWhenIdle: true, FullThreadDevice: true}, tnd.RoutingRoleReed},
		{thread.RoleChild, &thread.Mode{RxOnWhenIdle: true}, tnd.RoutingRoleEndDevice},
		{thread.RoleChild, &thread.Mode{}, tnd.RoutingRoleSleepyEndDevice},
		{"unknown", nil, tnd.RoutingRoleUnspecified},
	} {
		br := newFake()
		br.notFound = true
		br.diag = thread.NodeDiagnostics{Role: c.role, Self: &thread.DiagnosticResponse{Mode: c.mode}}
		s := newDiagnostics(t, br, 0)
		if got := read(t, s, tnd.AttrRoutingRole); got != uint8(c.want) {
			t.Errorf("%s %+v: RoutingRole = %v, want %v", c.role, c.mode, got, c.want)
		}
	}
}

// DTNDP :65-92: without a configured dataset the nullable attributes are
// null; a border router that does not answer reads so too, and Refresh
// returns its error.
func TestDiagnosticsUnconfigured(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound = true
	br.diag = meshDiagnostics()
	s := newDiagnostics(t, br, 0)
	for _, id := range []uint32{tnd.AttrChannel, tnd.AttrNetworkName, tnd.AttrPanId, tnd.AttrExtendedPanId, tnd.AttrMeshLocalPrefix,
		tnd.AttrPartitionId, tnd.AttrSecurityPolicy, tnd.AttrChannelPage0Mask, tnd.AttrOperationalDatasetComponents, tnd.AttrExtAddress, tnd.AttrRloc16} {
		if v := read(t, s, id); v != nil {
			t.Errorf("attribute 0x%04X = %v, want null", id, v)
		}
	}
	if v := read(t, s, tnd.AttrRoutingRole); v != uint8(tnd.RoutingRoleRouter) {
		t.Errorf("RoutingRole = %v: thread provides it unconfigured too (:70-71)", v)
	}
	br.set(func(f *fakeBorderRouter) { f.diagErr, f.datasetErr = errors.New("down"), errors.New("down") })
	if err := s.Refresh(context.Background()); err == nil {
		t.Error("Refresh of a silent border router reported no error")
	}
	if v := read(t, s, tnd.AttrRoutingRole); v != uint8(tnd.RoutingRoleUnspecified) {
		t.Errorf("RoutingRole of a silent border router = %v", v)
	}
	if l := read(t, s, tnd.AttrRouteTable).(spec.List[tnd.RouteTableStruct]); len(l) != 0 {
		t.Errorf("RouteTable = %v", l)
	}
	// A dataset that frames but carries none of the network's sub-TLVs
	// is not commissioned (IsCommissioned).
	br.set(func(f *fakeBorderRouter) { f.diagErr, f.datasetErr, f.active = nil, nil, timestamped(1) })
	mustT(t, s.Refresh(context.Background()))
	if v := read(t, s, tnd.AttrChannel); v != nil {
		t.Errorf("Channel of an uncommissioned dataset = %v", v)
	}
}

// ADR 0027 owner decision 2: MleCounts and MacCounts, from the record's
// MLE and MAC counters (DTNDP :325-371, :379-389, :481-491); PacketCounts
// and ErrorCounts are refused.
func TestDiagnosticsCounters(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.active, br.diag = chipDataset, meshDiagnostics()
	s := newDiagnostics(t, br, threadbr.DiagnosticsFeatures)
	if fm := read(t, s, cluster.AttrGlobalFeatureMap); fm != uint32(threadbr.DiagnosticsFeatures) {
		t.Errorf("FeatureMap = %v", fm)
	}
	for id, want := range map[uint32]any{
		tnd.AttrDetachedRoleCount: uint16(1), tnd.AttrChildRoleCount: uint16(2), tnd.AttrRouterRoleCount: uint16(3),
		tnd.AttrLeaderRoleCount: uint16(4), tnd.AttrAttachAttemptCount: uint16(5), tnd.AttrPartitionIdChangeCount: uint16(6),
		tnd.AttrBetterPartitionAttachAttemptCount: uint16(7), tnd.AttrParentChangeCount: uint16(8),
		tnd.AttrTxUnicastCount: uint32(12232), tnd.AttrTxBroadcastCount: uint32(8189),
		tnd.AttrRxUnicastCount: uint32(13666), tnd.AttrRxBroadcastCount: uint32(25593),
	} {
		if got := read(t, s, id); got != want {
			t.Errorf("attribute 0x%04X = %v (%T), want %v", id, got, got, want)
		}
	}
	if slices.Contains(s.MatterAttributes(), tnd.AttrTxTotalCount) {
		t.Error("TxTotalCount, which the record does not carry, is served")
	}
	// A record without counters reads 0, as chip's fallback (:733-736).
	br.set(func(f *fakeBorderRouter) { f.diag.Self = nil })
	mustT(t, s.Refresh(context.Background()))
	if got := read(t, s, tnd.AttrRxUnicastCount); got != uint32(0) {
		t.Errorf("RxUnicastCount without a record = %v", got)
	}
	if _, err := threadbr.NewDiagnostics(context.Background(), threadbr.DiagnosticsConfig{BorderRouter: br, Features: tnd.FeaturePacketCounts}); !errors.Is(err, threadbr.ErrDiagnosticsFeature) {
		t.Errorf("PacketCounts = %v, want ErrDiagnosticsFeature", err)
	}
	if _, err := threadbr.NewDiagnostics(context.Background(), threadbr.DiagnosticsConfig{}); !errors.Is(err, threadbr.ErrNoBorderRouter) {
		t.Errorf("no border router = %v", err)
	}
	if err := s.SetAttributes(map[uint32]any{tnd.AttrRxUnicastCount: uint32(5)}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set = %v, want ErrNotHeld", err)
	}
}

// A border router that does not answer at construction starts the server
// unconfigured; the change of a later refresh is reported.
func TestDiagnosticsStartsUnconfiguredAndReports(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.diagErr = errors.New("down")
	s := newDiagnostics(t, br, 0)
	if v := read(t, s, tnd.AttrChannel); v != nil {
		t.Errorf("Channel = %v", v)
	}
	dirty := watch(s)
	br.set(func(f *fakeBorderRouter) { f.diagErr, f.active, f.diag = nil, chipDataset, meshDiagnostics() })
	mustT(t, s.Refresh(context.Background()))
	if !dirty.has(tnd.AttrChannel) || !dirty.has(tnd.AttrRouteTable) {
		t.Error("the refresh's changes were not reported")
	}
	if _, err := s.MatterInvoke(caseCtx(), tnd.CmdResetCounts, tnd.ResetCountsRequest{}); err == nil {
		t.Error("ResetCounts, ErrorCounts' command, was accepted")
	}
	if err := s.MatterWrite(context.Background(), tnd.AttrChannel, uint16(1)); err == nil {
		t.Error("a write was accepted")
	}
	if s.MatterDataVersion() == 0 {
		t.Error("no data version")
	}
}

// channelPage0Mask: the page-0 entry among several, a short mask filled to
// four bytes, a dataset without a page-0 entry all zeros.
func TestDiagnosticsChannelPage0Mask(t *testing.T) {
	t.Parallel()
	base := chipDataset[:len(chipDataset)-0]
	for _, c := range []struct {
		mask []byte
		want []byte
	}{
		{[]byte{0x02, 0x01, 0xaa, 0x00, 0x02, 0x12, 0x34}, []byte{0x12, 0x34, 0x00, 0x00}},
		{[]byte{0x02, 0x04, 1, 2, 3, 4}, []byte{0, 0, 0, 0}},
		{[]byte{0x00, 0x09, 1, 2, 3, 4}, []byte{1, 2, 3, 4}},
	} {
		ds := replaceChannelMask(base, c.mask)
		br := newFake()
		br.active, br.diag = ds, meshDiagnostics()
		s := newDiagnostics(t, br, 0)
		if got := read(t, s, tnd.AttrChannelPage0Mask).([]byte); !bytes.Equal(got, c.want) {
			t.Errorf("mask %x: ChannelPage0Mask = %x, want %x", c.mask, got, c.want)
		}
	}
}

// replaceChannelMask is ds with its Channel Mask sub-TLV's value mask.
func replaceChannelMask(ds, mask []byte) []byte {
	entries, _ := thread.WalkTLVs(ds)
	for i := range entries {
		if entries[i].Type == thread.TLVChannelMask {
			entries[i].Value = mask
		}
	}
	out, _ := thread.EncodeTLVs(entries)
	return out
}
