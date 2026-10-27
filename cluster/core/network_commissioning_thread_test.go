// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// The Thread NetworkCommissioning server against connectedhomeip at the
// harness pin 6170af84: src/app/clusters/network-commissioning/
// NetworkCommissioningCluster.cpp and src/platform/OpenThread/
// GenericNetworkCommissioningThreadDriver.cpp (GTD). matter.js's server is
// a generated stub (NetworkCommissioningServer.ts at 862ad75f).

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/thread"
)

// threadDataset is the dataset of chip's
// TestThreadBorderRouterManagementCluster.cpp:303-308 (Channel 11, PAN ID
// 0xc115, Extended PAN ID dead00beef00cafe, "OpenThread").
var threadDataset = []byte{
	0x0e, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x0b, 0x35, 0x06,
	0x00, 0x04, 0x00, 0x1f, 0xff, 0xe0, 0x02, 0x08, 0xde, 0xaa, 0x00, 0xbe, 0xef, 0x00, 0xca, 0xef, 0x07,
	0x08, 0xfd, 0xde, 0xad, 0x00, 0xbe, 0xef, 0x00, 0x00, 0x05, 0x10, 0xb7, 0x28, 0x08, 0x04, 0x85, 0xcf,
	0xc5, 0x25, 0x7f, 0x68, 0x4c, 0x54, 0x9d, 0x6a, 0x57, 0x5e, 0x03, 0x0a, 0x4f, 0x70, 0x65, 0x6e, 0x54,
	0x68, 0x72, 0x65, 0x61, 0x64, 0x01, 0x02, 0xc1, 0x15, 0x04, 0x10, 0xcb, 0x13, 0x47, 0xeb, 0x0c, 0xd4,
	0xb3, 0x5c, 0xd1, 0x42, 0xda, 0x5e, 0x6d, 0xf1, 0x8b, 0x88, 0x0c, 0x04, 0x02, 0xa0, 0xf7, 0xf8,
}

var threadExtPANID = []byte{0xde, 0xaa, 0x00, 0xbe, 0xef, 0x00, 0xca, 0xef}

// otherDataset is threadDataset on another Extended PAN ID.
func otherDataset() []byte {
	out := slices.Clone(threadDataset)
	i := bytes.Index(out, threadExtPANID)
	out[i] = 0x11
	return out
}

// simBorderRouter is a border router in memory: SetActiveDataset makes the
// dataset active and attaches, the empty one clears it.
type simBorderRouter struct {
	mu        sync.Mutex
	active    []byte
	attached  bool
	enabled   bool
	failSet   error
	failRead  error
	activated [][]byte
	ext       []byte
}

func (b *simBorderRouter) BorderRouterName() string { return "sim" }
func (b *simBorderRouter) BorderAgentID(context.Context) ([]byte, error) {
	return make([]byte, 16), nil
}
func (b *simBorderRouter) ThreadVersion(context.Context) (uint16, error) { return 5, nil }
func (b *simBorderRouter) InterfaceEnabled(context.Context) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled, b.failRead
}

func (b *simBorderRouter) Dataset(_ context.Context, pending bool) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failRead != nil {
		return nil, b.failRead
	}
	if pending || len(b.active) == 0 {
		return nil, nil
	}
	return slices.Clone(b.active), nil
}

func (b *simBorderRouter) SetActiveDataset(_ context.Context, ds []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.activated = append(b.activated, slices.Clone(ds))
	if b.failSet != nil {
		return b.failSet
	}
	b.active, b.attached, b.enabled = slices.Clone(ds), len(ds) > 0, len(ds) > 0
	return nil
}
func (b *simBorderRouter) CommitActiveDataset(context.Context) error       { return nil }
func (b *simBorderRouter) RevertActiveDataset(context.Context) error       { return nil }
func (b *simBorderRouter) SetPendingDataset(context.Context, []byte) error { return nil }
func (b *simBorderRouter) Diagnostics(context.Context) (thread.NodeDiagnostics, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	role := thread.RoleDisabled
	if b.attached {
		role = thread.RoleLeader
	}
	return thread.NodeDiagnostics{Role: role, ExtAddress: b.ext}, b.failRead
}

// threadStatusOf is the IM status of err, Success for nil.
func threadStatusOf(t *testing.T, err error) im.StatusCode {
	t.Helper()
	if err == nil {
		return im.StatusSuccess
	}
	return wifiStatusOf(t, err)
}

func newThreadNC(t *testing.T, br *simBorderRouter, armed *bool, crumbs *[]uint64) *ThreadNetworkCommissioning {
	t.Helper()
	n, err := NewThreadNetworkCommissioning(context.Background(), ThreadNetworkCommissioningConfig{
		BorderRouter: br, IsFailSafeArmed: func() bool { return *armed },
		SetBreadcrumb: func(v uint64) { *crumbs = append(*crumbs, v) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func networksOf(t *testing.T, n *ThreadNetworkCommissioning) spec.List[netcommdef.NetworkInfoStruct] {
	t.Helper()
	v, _ := n.MatterRead(netcommAttrNetworks)
	return v.(spec.List[netcommdef.NetworkInfoStruct])
}

// The attributes: MaxNetworks 1 (GenericNetworkCommissioningThreadDriver.h
// :103), the Thread feature, the border router's capabilities and
// version, the staged network from the active dataset (GTD :46-50,
// :340-373).
func TestThreadNetworkCommissioningAttributes(t *testing.T) {
	t.Parallel()
	br := &simBorderRouter{active: slices.Clone(threadDataset), attached: true, enabled: true}
	armed, crumbs := false, []uint64{}
	n := newThreadNC(t, br, &armed, &crumbs)
	for id, want := range map[uint32]any{
		netcommAttrMaxNetworks:                 uint8(1),
		netcommAttrScanMaxTimeSeconds:          uint8(10),
		netcommAttrConnectMaxTimeSeconds:       uint8(20),
		netcommAttrInterfaceEnabled:            true,
		netcommdef.AttrSupportedThreadFeatures: threadCapabilities,
		netcommdef.AttrThreadVersion:           uint16(5),
		cluster.AttrGlobalFeatureMap:           NetworkCommFeatureThread,
	} {
		if v, _ := n.MatterRead(id); v != want {
			t.Errorf("attribute 0x%04X = %v (%T), want %v", id, v, v, want)
		}
	}
	for _, id := range []uint32{netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue} {
		if v, ok := n.MatterRead(id); !ok || v != nil {
			t.Errorf("attribute 0x%04X = %v, want null", id, v)
		}
	}
	nets := networksOf(t, n)
	if len(nets) != 1 || !bytes.Equal(nets[0].NetworkId, threadExtPANID) || !nets[0].Connected {
		t.Errorf("Networks = %+v", nets)
	}
	if !slices.Contains(n.MatterAttributes(), netcommdef.AttrThreadVersion) || slices.Contains(n.MatterAttributes(), netcommdef.AttrSupportedWiFiBands) {
		t.Errorf("attribute list %v", n.MatterAttributes())
	}
	if _, ok := n.MatterRead(0x7777); ok {
		t.Error("an unknown attribute was served")
	}
	if n.MatterClusterID() != netcommClusterID || n.MatterDataVersion() == 0 || len(n.MatterReportable()) == 0 ||
		len(n.MatterAcceptedCommands()) != 5 || len(n.MatterGeneratedCommands()) == 0 ||
		n.MinInvokePrivilege(netcommdef.CmdConnectNetwork) == 0 || n.MinReadPrivilege(netcommAttrNetworks) == 0 ||
		n.MinWritePrivilege(netcommAttrInterfaceEnabled) == 0 {
		t.Error("the plumbing does not answer")
	}
	if _, err := NewThreadNetworkCommissioning(context.Background(), ThreadNetworkCommissioningConfig{}); err == nil {
		t.Error("no border router was accepted")
	}
}

// ScanNetworks (:309-371, :904-928) under ADR 0027 owner decision 3: the
// border router's own network from its active dataset; an empty list
// without one.
func TestThreadNetworkCommissioningScan(t *testing.T) {
	t.Parallel()
	br := &simBorderRouter{active: slices.Clone(threadDataset), attached: true, enabled: true, ext: []byte{1, 2, 3, 4, 5, 6, 7, 8}}
	armed, crumbs := false, []uint64{}
	n := newThreadNC(t, br, &armed, &crumbs)
	resp, err := n.MatterInvoke(context.Background(), netcommdef.CmdScanNetworks, netcommdef.ScanNetworksRequest{Breadcrumb: bc(4)})
	if err != nil {
		t.Fatal(err)
	}
	r := resp.(netcommdef.ScanNetworksResponse)
	if r.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess || r.ThreadScanResults == nil || len(*r.ThreadScanResults) != 1 {
		t.Fatalf("ScanNetworksResponse = %+v", r)
	}
	got := (*r.ThreadScanResults)[0]
	if got.PanId != 0xc115 || got.ExtendedPanId != 0xdeaa00beef00caef || got.NetworkName != "OpenThread" || got.Channel != 11 ||
		got.Version != 5 || !bytes.Equal(got.ExtendedAddress, br.ext) {
		t.Errorf("scan result = %+v", got)
	}
	if !slices.Equal(crumbs, []uint64{4}) {
		t.Errorf("breadcrumbs %v", crumbs)
	}
	if v, _ := n.MatterRead(netcommAttrLastNetworkingStatus); v != uint8(0) {
		t.Errorf("LastNetworkingStatus = %v", v)
	}
	br.active = nil
	resp, _ = n.MatterInvoke(context.Background(), netcommdef.CmdScanNetworks, netcommdef.ScanNetworksRequest{})
	if r := resp.(netcommdef.ScanNetworksResponse); len(*r.ThreadScanResults) != 0 || r.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess {
		t.Errorf("scan without a dataset = %+v", r)
	}
	br.failRead = errors.New("down")
	resp, _ = n.MatterInvoke(context.Background(), netcommdef.CmdScanNetworks, netcommdef.ScanNetworksRequest{})
	if r := resp.(netcommdef.ScanNetworksResponse); r.NetworkingStatus != netcommdef.NetworkCommissioningStatusUnknownError {
		t.Errorf("scan of a silent border router = %+v", r)
	}
}

// AddOrUpdate, Remove, Reorder, Connect and the fail-safe (GTD :97-268;
// NetworkCommissioningCluster.cpp :561-730, :843-902, :984-1014).
func TestThreadNetworkCommissioningCommands(t *testing.T) {
	t.Parallel()
	br := &simBorderRouter{}
	armed, crumbs := false, []uint64{}
	n := newThreadNC(t, br, &armed, &crumbs)
	ctx := context.Background()
	invoke := func(cmd uint32, req any) (any, im.StatusCode) {
		resp, err := n.MatterInvoke(ctx, cmd, req)
		return resp, threadStatusOf(t, err)
	}
	statusIn := func(resp any) netcommdef.NetworkCommissioningStatusEnum {
		switch r := resp.(type) {
		case netcommdef.NetworkConfigResponse:
			return r.NetworkingStatus
		case netcommdef.ConnectNetworkResponse:
			return r.NetworkingStatus
		}
		t.Fatalf("response %T", resp)
		return 0
	}
	add := netcommdef.AddOrUpdateThreadNetworkRequest{OperationalDataset: threadDataset, Breadcrumb: bc(1)}
	if _, st := invoke(netcommdef.CmdAddOrUpdateThreadNetwork, add); st != im.StatusFailsafeRequired {
		t.Errorf("unarmed AddOrUpdate = %v", st)
	}
	if _, st := invoke(netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: threadExtPANID}); st != im.StatusFailsafeRequired {
		t.Errorf("unarmed Remove = %v", st)
	}
	if _, st := invoke(netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID}); st != im.StatusFailsafeRequired {
		t.Errorf("unarmed Connect = %v", st)
	}
	armed = true
	if resp, _ := invoke(netcommdef.CmdAddOrUpdateThreadNetwork, netcommdef.AddOrUpdateThreadNetworkRequest{OperationalDataset: []byte{0x0e, 0x08, 0, 0, 0, 0, 0, 0, 0, 1}}); statusIn(resp) != netcommdef.NetworkCommissioningStatusOutOfRange {
		t.Errorf("an uncommissioned dataset = %v", statusIn(resp))
	}
	if resp, _ := invoke(netcommdef.CmdAddOrUpdateThreadNetwork, add); statusIn(resp) != netcommdef.NetworkCommissioningStatusSuccess {
		t.Fatalf("AddOrUpdate = %v", statusIn(resp))
	}
	if nets := networksOf(t, n); len(nets) != 1 || nets[0].Connected {
		t.Errorf("Networks after add = %+v", nets)
	}
	if resp, _ := invoke(netcommdef.CmdAddOrUpdateThreadNetwork, netcommdef.AddOrUpdateThreadNetworkRequest{OperationalDataset: otherDataset()}); statusIn(resp) != netcommdef.NetworkCommissioningStatusBoundsExceeded {
		t.Errorf("a second network = %v", statusIn(resp))
	}
	if resp, _ := invoke(netcommdef.CmdReorderNetwork, netcommdef.ReorderNetworkRequest{NetworkId: threadExtPANID, NetworkIndex: 1}); statusIn(resp) != netcommdef.NetworkCommissioningStatusOutOfRange {
		t.Errorf("Reorder to 1 = %v", statusIn(resp))
	}
	if resp, _ := invoke(netcommdef.CmdReorderNetwork, netcommdef.ReorderNetworkRequest{NetworkId: []byte{9}}); statusIn(resp) != netcommdef.NetworkCommissioningStatusNetworkIdNotFound {
		t.Errorf("Reorder of an unknown network = %v", statusIn(resp))
	}
	if resp, _ := invoke(netcommdef.CmdReorderNetwork, netcommdef.ReorderNetworkRequest{NetworkId: threadExtPANID, Breadcrumb: bc(2)}); statusIn(resp) != netcommdef.NetworkCommissioningStatusSuccess {
		t.Errorf("Reorder to 0 = %v", statusIn(resp))
	}
	if _, st := invoke(netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: make([]byte, 33)}); st != im.StatusConstraintError {
		t.Errorf("an overlong network id = %v", st)
	}
	if resp, _ := invoke(netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte{9}}); statusIn(resp) != netcommdef.NetworkCommissioningStatusNetworkIdNotFound {
		t.Errorf("Connect to an unknown network = %v", statusIn(resp))
	}
	resp, _ := invoke(netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID, Breadcrumb: bc(3)})
	if statusIn(resp) != netcommdef.NetworkCommissioningStatusSuccess || !resp.(netcommdef.ConnectNetworkResponse).ErrorValue.Null {
		t.Fatalf("Connect = %+v", resp)
	}
	if !bytes.Equal(br.active, threadDataset) {
		t.Error("the border router was not given the staged dataset")
	}
	if nets := networksOf(t, n); len(nets) != 1 || !nets[0].Connected {
		t.Errorf("Networks after connect = %+v", nets)
	}
	if !slices.Equal(crumbs, []uint64{1, 2, 3}) {
		t.Errorf("breadcrumbs %v", crumbs)
	}
	// The fail-safe expires: the backup (no network) is restored and the
	// border router's active dataset cleared.
	n.OnFailSafeExpired(ctx)
	if len(networksOf(t, n)) != 0 || len(br.active) != 0 {
		t.Errorf("after the fail-safe: Networks %v, active %x", networksOf(t, n), br.active)
	}
	if v, _ := n.MatterRead(netcommAttrLastNetworkingStatus); v != nil {
		t.Errorf("LastNetworkingStatus after the revert = %v", v)
	}
	// Commissioned: the backup goes, and a later expiry keeps the network.
	invoke(netcommdef.CmdAddOrUpdateThreadNetwork, add)
	invoke(netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID})
	n.OnCommissioningComplete(ctx)
	n.OnFailSafeExpired(ctx)
	if len(networksOf(t, n)) != 1 {
		t.Error("a committed network was reverted")
	}
	// Remove backs up and clears; a failed attach is UnknownError.
	if resp, _ := invoke(netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte{9}}); statusIn(resp) != netcommdef.NetworkCommissioningStatusNetworkIdNotFound {
		t.Errorf("Remove of an unknown network = %v", statusIn(resp))
	}
	if resp, _ := invoke(netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: threadExtPANID}); statusIn(resp) != netcommdef.NetworkCommissioningStatusSuccess || len(networksOf(t, n)) != 0 {
		t.Errorf("Remove = %v", statusIn(resp))
	}
	invoke(netcommdef.CmdAddOrUpdateThreadNetwork, add)
	br.failSet = errors.New("attach failed")
	resp, _ = invoke(netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID})
	if r := resp.(netcommdef.ConnectNetworkResponse); r.NetworkingStatus != netcommdef.NetworkCommissioningStatusUnknownError || r.ErrorValue.Null {
		t.Errorf("a failed attach = %+v", r)
	}
	if v, _ := n.MatterRead(netcommAttrLastConnectErrorValue); v == nil {
		t.Error("LastConnectErrorValue is null after a failed attach")
	}
	if v, _ := n.MatterRead(netcommAttrLastNetworkID); !bytes.Equal(v.([]byte), threadExtPANID) {
		t.Errorf("LastNetworkID = %v", v)
	}
	for _, cmd := range []uint32{netcommdef.CmdScanNetworks, netcommdef.CmdAddOrUpdateThreadNetwork, netcommdef.CmdRemoveNetwork, netcommdef.CmdConnectNetwork, netcommdef.CmdReorderNetwork} {
		if _, st := invoke(cmd, "foreign"); st != im.StatusInvalidCommand {
			t.Errorf("command 0x%02X with a foreign payload = %v", cmd, st)
		}
	}
	if _, st := invoke(netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{}); st != im.StatusUnsupportedCommand {
		t.Errorf("AddOrUpdateWiFiNetwork = %v", st)
	}
}

// InterfaceEnabled follows the border router; a write of its value is
// accepted, another refused; the other attributes are read-only.
func TestThreadNetworkCommissioningInterfaceEnabled(t *testing.T) {
	t.Parallel()
	br := &simBorderRouter{enabled: true}
	armed, crumbs := false, []uint64{}
	n := newThreadNC(t, br, &armed, &crumbs)
	ctx := context.Background()
	if err := n.MatterWrite(ctx, netcommAttrInterfaceEnabled, true); err != nil {
		t.Errorf("write of the served value = %v", err)
	}
	if err := n.MatterWrite(ctx, netcommAttrInterfaceEnabled, false); wifiStatusOf(t, err) != im.StatusFailure {
		t.Errorf("write of false = %v, want FAILURE", err)
	}
	if err := n.MatterWrite(ctx, netcommAttrInterfaceEnabled, "x"); err == nil {
		t.Error("a non-bool write was accepted")
	}
	if err := n.MatterWrite(ctx, netcommAttrMaxNetworks, uint8(2)); err == nil {
		t.Error("a write of MaxNetworks was accepted")
	}
	v0 := n.MatterDataVersion()
	br.enabled = false
	n.Refresh(ctx)
	if v, _ := n.MatterRead(netcommAttrInterfaceEnabled); v != false || n.MatterDataVersion() == v0 {
		t.Errorf("InterfaceEnabled after the border router went down = %v", v)
	}
	n.AttachFailSafe(func() bool { return true }, nil)
	if !n.armed() {
		t.Error("AttachFailSafe did not wire the fail-safe")
	}
	n.Refresh(ctx)
}
