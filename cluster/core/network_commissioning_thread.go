// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	tbrmdef "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/thread"
)

// ThreadNetworkCommissioning is the NetworkCommissioning cluster (0x0031)
// with the Thread feature over a Thread Border Router's
// [tbrmdef.BorderRouter], the one cluster/threadbr's management server
// manages. Both clusters then reflect one network, as the border router's
// device type demands of an endpoint that also supports the secondary
// network interface device type (matter.js
// packages/node/src/devices/thread-border-router.ts:34-39 at 862ad75f;
// ADR 0027 decision 3). MaxNetworks is 1.
//
// matter.js's server is a generated stub with no Thread branch
// (packages/node/src/behaviors/network-commissioning/
// NetworkCommissioningServer.ts at 862ad75f), so the commands, statuses
// and list rules are connectedhomeip's at 6170af84:
// src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp for
// the commands and src/platform/OpenThread/
// GenericNetworkCommissioningThreadDriver.cpp (GTD below) for the one
// staged network, its fail-safe backup and the Networks list. ScanNetworks
// has no radio scan to run: it answers the border router's own network
// from its active dataset (ADR 0027 owner decision 3).
type ThreadNetworkCommissioning struct {
	// mu guards every field below dataVersion.
	mu          sync.Mutex
	dataVersion cluster.DataVersionTracker

	br            tbrmdef.BorderRouter
	failSafeArmed func() bool
	setBreadcrumb func(uint64)
	scanMax       uint8
	connectMax    uint8
	threadVersion uint16

	// staging is mStagingNetwork (GTD :38-44): the network the commands
	// edit, attached by ConnectNetwork. backup is the fail-safe's copy
	// (FailSafeNetworkConfig, BackupConfiguration GTD :300-314), nil for
	// none.
	staging []byte
	backup  []byte
	// connected and enabled are what the last read of the border router
	// found: attached to the staged network, and its interface up.
	connected bool
	enabled   bool
	busy      bool

	lastStatus     *uint8
	lastNetworkID  []byte
	lastConnectErr *int32
}

// ThreadNetworkCommissioningConfig drives [NewThreadNetworkCommissioning].
type ThreadNetworkCommissioningConfig struct {
	// BorderRouter is the border router; required.
	BorderRouter tbrmdef.BorderRouter
	// IsFailSafeArmed reports whether a fail-safe is armed; the network
	// commands need one (EnsureFailsafeIsArmed, NetworkCommissioningCluster.cpp:
	// 155-172). Nil reads as disarmed until [ThreadNetworkCommissioning.AttachFailSafe].
	IsFailSafeArmed func() bool
	// SetBreadcrumb writes GeneralCommissioning's Breadcrumb
	// (UpdateBreadcrumb, :589-593); nil writes none.
	SetBreadcrumb func(uint64)
	// ScanMaxTimeSeconds and ConnectMaxTimeSeconds; 0 are the Thread
	// driver's 10 and 20 (GenericNetworkCommissioningThreadDriver.h:87-90).
	ScanMaxTimeSeconds, ConnectMaxTimeSeconds uint8
}

// threadNetcommInst is the definition bound to the Thread feature.
var threadNetcommInst = mustInstance(netcommdef.Definition, spec.Options{Features: NetworkCommFeatureThread})

// threadCapabilities is SupportedThreadFeatures of a border router: a
// border-router-capable full Thread device that can route
// (GetSupportedThreadFeatures, GTD :383-394, with
// CHIP_DEVICE_CONFIG_THREAD_BORDER_ROUTER and _FTD set).
const threadCapabilities = netcommdef.ThreadCapabilitiesIsBorderRouterCapable |
	netcommdef.ThreadCapabilitiesIsRouterCapable | netcommdef.ThreadCapabilitiesIsFullThreadDevice

// NewThreadNetworkCommissioning returns the Thread NetworkCommissioning
// server: the staged network is the border router's active dataset (Init
// reads the provision, GTD :46-50), the last networking status, network
// id and connect error null.
func NewThreadNetworkCommissioning(ctx context.Context, cfg ThreadNetworkCommissioningConfig) (*ThreadNetworkCommissioning, error) {
	if cfg.BorderRouter == nil {
		return nil, errors.New("core: Thread NetworkCommissioning needs a border router")
	}
	n := &ThreadNetworkCommissioning{
		br: cfg.BorderRouter, failSafeArmed: cfg.IsFailSafeArmed, setBreadcrumb: cfg.SetBreadcrumb,
		scanMax: cfg.ScanMaxTimeSeconds, connectMax: cfg.ConnectMaxTimeSeconds,
	}
	if n.scanMax == 0 {
		n.scanMax = 10
	}
	if n.connectMax == 0 {
		n.connectMax = 20
	}
	if v, err := n.br.ThreadVersion(ctx); err == nil {
		n.threadVersion = v
	}
	if active, err := n.br.Dataset(ctx, false); err == nil {
		n.staging = slices.Clone(active)
	}
	n.enabled, n.connected = n.probe(ctx, n.staging)
	n.dataVersion.Bump()
	return n, nil
}

var (
	_ contract.ClusterServer                  = (*ThreadNetworkCommissioning)(nil)
	_ contract.ClusterAttributeLister         = (*ThreadNetworkCommissioning)(nil)
	_ contract.ClusterDataVersion             = (*ThreadNetworkCommissioning)(nil)
	_ contract.ClusterAttributeReadPrivilege  = (*ThreadNetworkCommissioning)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*ThreadNetworkCommissioning)(nil)
	_ contract.ClusterCommandLister           = (*ThreadNetworkCommissioning)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*ThreadNetworkCommissioning)(nil)
)

// AttachFailSafe wires the node's fail-safe after construction, as
// [ThreadNetworkCommissioningConfig] does.
func (n *ThreadNetworkCommissioning) AttachFailSafe(failSafe func() (fabricIndex uint8, armed bool), setBreadcrumb func(uint64)) {
	var armed func() bool
	if failSafe != nil {
		armed = func() bool { _, a := failSafe(); return a }
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.failSafeArmed, n.setBreadcrumb = armed, setBreadcrumb
}

// Refresh reads the border router's attachment and interface again
// (Networks' Connected, InterfaceEnabled) and reports a change.
func (n *ThreadNetworkCommissioning) Refresh(ctx context.Context) {
	n.mu.Lock()
	staging := slices.Clone(n.staging)
	n.mu.Unlock()
	enabled, connected := n.probe(ctx, staging)
	n.mu.Lock()
	defer n.mu.Unlock()
	if connected != n.connected || enabled != n.enabled {
		n.enabled, n.connected = enabled, connected
		n.dataVersion.Bump()
	}
}

// probe reads the border router's interface and, as
// ThreadNetworkIterator::Next's connected (GTD :345-373), whether the
// staged network is connected: the border router is attached and its
// active dataset names the same Extended PAN ID. A border router that
// does not answer is neither enabled nor connected. It runs without the
// lock: the port is the border router, an HTTP round trip away.
func (n *ThreadNetworkCommissioning) probe(ctx context.Context, staging []byte) (enabled, connected bool) {
	on, err := n.br.InterfaceEnabled(ctx)
	enabled = on && err == nil
	staged, ok := extPANIDOf(staging)
	if !ok {
		return enabled, false
	}
	diag, err := n.br.Diagnostics(ctx)
	if err != nil || !diag.Role.Attached() {
		return enabled, false
	}
	active, err := n.br.Dataset(ctx, false)
	if err != nil {
		return enabled, false
	}
	current, ok := extPANIDOf(active)
	return enabled, ok && bytes.Equal(staged, current)
}

// extPANIDOf is the Extended PAN ID of a commissioned dataset
// (IsCommissioned and GetExtendedPanId, ThreadOperationalDataset.cpp:
// 188-237).
func extPANIDOf(ds []byte) ([]byte, bool) {
	v, err := thread.NewDatasetView(ds)
	if err != nil || !v.IsCommissioned() {
		return nil, false
	}
	xp, err := v.ExtPANID()
	return xp, err == nil
}

// MatterClusterID implements [contract.ClusterServer].
func (n *ThreadNetworkCommissioning) MatterClusterID() uint32 { return netcommClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (n *ThreadNetworkCommissioning) MatterDataVersion() uint32 { return n.dataVersion.Current() }

// MatterAttributes implements [contract.ClusterAttributeLister]: the
// Thread feature's attributes.
func (n *ThreadNetworkCommissioning) MatterAttributes() []uint32 {
	return threadNetcommInst.MatterAttributes()
}

// MatterReportable lists the subscribe-able attributes.
func (n *ThreadNetworkCommissioning) MatterReportable() []uint32 {
	return []uint32{netcommAttrNetworks, netcommAttrInterfaceEnabled, netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue}
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// ScanNetworks, AddOrUpdateThreadNetwork, RemoveNetwork, ConnectNetwork,
// ReorderNetwork.
func (n *ThreadNetworkCommissioning) MatterAcceptedCommands() []uint32 {
	return threadNetcommInst.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (n *ThreadNetworkCommissioning) MatterGeneratedCommands() []uint32 {
	return threadNetcommInst.MatterGeneratedCommands()
}

// MinReadPrivilege implements [contract.ClusterAttributeReadPrivilege].
func (n *ThreadNetworkCommissioning) MinReadPrivilege(attrID uint32) uint8 {
	return threadNetcommInst.MinReadPrivilege(attrID)
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege].
func (n *ThreadNetworkCommissioning) MinWritePrivilege(attrID uint32) uint8 {
	return threadNetcommInst.MinWritePrivilege(attrID)
}

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege]:
// Administer for every command.
func (n *ThreadNetworkCommissioning) MinInvokePrivilege(cmdID uint32) uint8 {
	return threadNetcommInst.MinInvokePrivilege(cmdID)
}

// MatterRead implements [contract.ClusterServer] (ReadAttribute,
// NetworkCommissioningCluster.cpp:1067-1113): MaxNetworks 1 (GTD
// GetMaxNetworks, GenericNetworkCommissioningThreadDriver.h:103), the
// staged network with its Extended PAN ID as the network id (GTD
// :340-373), SupportedThreadFeatures and ThreadVersion (:1102-1108).
func (n *ThreadNetworkCommissioning) MatterRead(attrID uint32) (any, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch attrID {
	case netcommAttrMaxNetworks:
		return uint8(1), true
	case netcommAttrNetworks:
		out := spec.List[netcommdef.NetworkInfoStruct]{}
		if xp, ok := extPANIDOf(n.staging); ok {
			out = append(out, netcommdef.NetworkInfoStruct{NetworkId: slices.Clone(xp), Connected: n.connected})
		}
		return out, true
	case netcommAttrScanMaxTimeSeconds:
		return n.scanMax, true
	case netcommAttrConnectMaxTimeSeconds:
		return n.connectMax, true
	case netcommAttrInterfaceEnabled:
		return n.enabled, true
	case netcommAttrLastNetworkingStatus:
		if n.lastStatus == nil {
			return nil, true
		}
		return *n.lastStatus, true
	case netcommAttrLastNetworkID:
		if len(n.lastNetworkID) == 0 {
			return nil, true
		}
		return bytes.Clone(n.lastNetworkID), true
	case netcommAttrLastConnectErrorValue:
		if n.lastConnectErr == nil {
			return nil, true
		}
		return *n.lastConnectErr, true
	case netcommdef.AttrSupportedThreadFeatures:
		return threadCapabilities, true
	case netcommdef.AttrThreadVersion:
		return n.threadVersion, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return threadNetcommInst.ReadGlobal(attrID)
	}
	return nil, false
}

// errThreadInterfaceEnabled refuses an InterfaceEnabled write: the
// border router's Thread interface is switched by the border router's
// own management, not through this cluster — chip's driver switches its
// in-process stack (SetEnabled, GTD :270-282), and the module wraps no
// mutating border-router operation outside the fail-safe-guarded dataset
// path (ADR 0027 decision 4).
var errThreadInterfaceEnabled = errors.New("core: the border router's Thread interface is not switched through NetworkCommissioning")

// MatterWrite handles InterfaceEnabled (WriteAttribute, :1115-1136): the
// value the border router already has is accepted, another refused with
// FAILURE ([errThreadInterfaceEnabled]).
func (n *ThreadNetworkCommissioning) MatterWrite(_ context.Context, attrID uint32, value any) error {
	if attrID != netcommAttrInterfaceEnabled {
		_, err := threadNetcommInst.ValidateWrite(attrID, value, nil)
		return refusedWrite{sentinel: fmt.Errorf("core: NetworkCommissioning attribute 0x%04X is read-only", attrID), status: err}
	}
	v, ok := value.(bool)
	if !ok {
		return fmt.Errorf("core: InterfaceEnabled write expected bool, got %T", value)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if v != n.enabled {
		return netcommStatus(im.StatusFailure, "%v", errThreadInterfaceEnabled)
	}
	return nil
}

// MatterInvoke implements [contract.ClusterServer] (InvokeCommand,
// :1138-1194): Busy while a scan or a connect runs.
func (n *ThreadNetworkCommissioning) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	n.mu.Lock()
	if n.busy {
		n.mu.Unlock()
		return nil, netcommStatus(im.StatusBusy, "core: NetworkCommissioning is busy")
	}
	n.mu.Unlock()
	switch cmdID {
	case netcommdef.CmdScanNetworks:
		req, ok := fields.(netcommdef.ScanNetworksRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: ScanNetworks fields %T", fields)
		}
		return n.scanNetworks(ctx, req)
	case netcommdef.CmdAddOrUpdateThreadNetwork:
		req, ok := fields.(netcommdef.AddOrUpdateThreadNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: AddOrUpdateThreadNetwork fields %T", fields)
		}
		return n.addOrUpdate(req)
	case netcommdef.CmdRemoveNetwork:
		req, ok := fields.(netcommdef.RemoveNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: RemoveNetwork fields %T", fields)
		}
		return n.remove(req)
	case netcommdef.CmdConnectNetwork:
		req, ok := fields.(netcommdef.ConnectNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: ConnectNetwork fields %T", fields)
		}
		return n.connect(ctx, req)
	case netcommdef.CmdReorderNetwork:
		req, ok := fields.(netcommdef.ReorderNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: ReorderNetwork fields %T", fields)
		}
		return n.reorder(req)
	}
	return nil, im.UnsupportedCommandf("core: NetworkCommissioning command 0x%02X not supported (Thread)", cmdID)
}

func (n *ThreadNetworkCommissioning) armed() bool {
	n.mu.Lock()
	armed := n.failSafeArmed
	n.mu.Unlock()
	return armed != nil && armed()
}

// breadcrumbLocked is UpdateBreadcrumb (:589-593).
func (n *ThreadNetworkCommissioning) breadcrumbLocked(bc *uint64) {
	if bc != nil && n.setBreadcrumb != nil {
		n.setBreadcrumb(*bc)
	}
}

func (n *ThreadNetworkCommissioning) setStatusLocked(s netcommdef.NetworkCommissioningStatusEnum) {
	v := uint8(s)
	n.lastStatus = &v
}

// backupLocked is BackupConfiguration (GTD :300-314): the staged network
// as it was when the fail-safe's first change came, once.
func (n *ThreadNetworkCommissioning) backupLocked() {
	if n.backup == nil {
		n.backup = append([]byte{}, n.staging...)
	}
}

// matchLocked is MatchesNetworkId (GTD :396-409): NetworkIDNotFound for
// an uncommissioned staged network or another Extended PAN ID.
func (n *ThreadNetworkCommissioning) matchLocked(id []byte) netcommdef.NetworkCommissioningStatusEnum {
	xp, ok := extPANIDOf(n.staging)
	if !ok || !bytes.Equal(xp, id) {
		return netcommdef.NetworkCommissioningStatusNetworkIdNotFound
	}
	return netcommdef.NetworkCommissioningStatusSuccess
}

// scanNetworks is HandleScanNetworks (:309-371) and OnFinished
// (:904-928) for a border router, which has no radio scan to offer (ADR
// 0027 owner decision 3): one ThreadInterfaceScanResult of its active
// dataset — PAN ID, Extended PAN ID, Network Name, Channel — with its
// Thread version and extended address, status Success; the list is empty
// while no commissioned dataset is configured. The status is the last
// networking status; success commits the breadcrumb (:924-927). A Wi-Fi
// SSID in the request is accepted, as chip accepts it (:351-359).
func (n *ThreadNetworkCommissioning) scanNetworks(ctx context.Context, req netcommdef.ScanNetworksRequest) (any, error) {
	n.mu.Lock()
	n.busy = true
	n.mu.Unlock()
	results := []netcommdef.ThreadInterfaceScanResultStruct{}
	status := netcommdef.NetworkCommissioningStatusSuccess
	active, err := n.br.Dataset(ctx, false)
	if err != nil {
		status = netcommdef.NetworkCommissioningStatusUnknownError
	} else if r, ok := scanResult(active); ok {
		r.Version = uint8(min(n.threadVersion, 0xff)) //nolint:gosec // bounded by min
		if diag, err := n.br.Diagnostics(ctx); err == nil && len(diag.ExtAddress) == 8 {
			r.ExtendedAddress = slices.Clone(diag.ExtAddress)
		}
		results = append(results, r)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.busy = false
	n.setStatusLocked(status)
	n.dataVersion.Bump()
	if statusOK(status) {
		n.breadcrumbLocked(req.Breadcrumb)
	}
	return netcommdef.ScanNetworksResponse{NetworkingStatus: status, ThreadScanResults: &results}, nil
}

// scanResult is the ThreadInterfaceScanResult of a commissioned active
// dataset, read through chip's view; its extended address is the
// border router's own (8 zero bytes until known).
func scanResult(ds []byte) (netcommdef.ThreadInterfaceScanResultStruct, bool) {
	v, err := thread.NewDatasetView(ds)
	if err != nil || !v.IsCommissioned() {
		return netcommdef.ThreadInterfaceScanResultStruct{}, false
	}
	pan, err1 := v.PANID()
	xp, err2 := v.ExtPANID()
	ch, err3 := v.Channel()
	name, err4 := v.NetworkName()
	if err := errors.Join(err1, err2, err3, err4); err != nil || name == "" {
		return netcommdef.ThreadInterfaceScanResultStruct{}, false
	}
	return netcommdef.ThreadInterfaceScanResultStruct{
		PanId: pan, ExtendedPanId: binary.BigEndian.Uint64(xp), NetworkName: name, Channel: ch,
		ExtendedAddress: make([]byte, 8),
	}, true
}

// addOrUpdate is HandleAddOrUpdateThreadNetwork (:561-587) over the
// driver's AddOrUpdateNetwork (GTD :158-176): a failsafe is required
// (:570); a dataset that does not frame or is not commissioned is
// OutOfRange; a staged commissioned network of another Extended PAN ID is
// BoundsExceeded (one network); otherwise the staged network is backed up
// and replaced, the breadcrumb written and the list changed (:578-582).
func (n *ThreadNetworkCommissioning) addOrUpdate(req netcommdef.AddOrUpdateThreadNetworkRequest) (any, error) {
	if !n.armed() {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: AddOrUpdateThreadNetwork needs an armed failsafe")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	xp, ok := extPANIDOf(req.OperationalDataset)
	if !ok {
		return configResponse(netcommdef.NetworkCommissioningStatusOutOfRange, 0), nil
	}
	if staged, commissioned := extPANIDOf(n.staging); commissioned && !bytes.Equal(staged, xp) {
		return configResponse(netcommdef.NetworkCommissioningStatusBoundsExceeded, 0), nil
	}
	n.backupLocked()
	n.staging = slices.Clone(req.OperationalDataset)
	n.connected = false
	n.dataVersion.Bump()
	n.breadcrumbLocked(req.Breadcrumb)
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, 0), nil
}

// remove is HandleRemoveNetwork (:603-632) over the driver's
// RemoveNetwork (GTD :178-191): a failsafe is required; an unknown
// network is NetworkIDNotFound; the staged network is backed up and
// cleared, and the last status, network id and connect error with it.
func (n *ThreadNetworkCommissioning) remove(req netcommdef.RemoveNetworkRequest) (any, error) {
	if !n.armed() {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: RemoveNetwork needs an armed failsafe")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if st := n.matchLocked(req.NetworkId); !statusOK(st) {
		return configResponse(st, 0), nil
	}
	n.backupLocked()
	n.staging, n.connected = nil, false
	n.lastNetworkID, n.lastConnectErr, n.lastStatus = nil, nil, nil
	n.dataVersion.Bump()
	n.breadcrumbLocked(req.Breadcrumb)
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, 0), nil
}

// reorder is HandleReorderNetwork (:712-730) over the driver's
// ReorderNetwork (GTD :193-203): an unknown network is
// NetworkIDNotFound, an index other than 0 OutOfRange.
func (n *ThreadNetworkCommissioning) reorder(req netcommdef.ReorderNetworkRequest) (any, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if st := n.matchLocked(req.NetworkId); !statusOK(st) {
		return configResponse(st, 0), nil
	}
	if req.NetworkIndex != 0 {
		return configResponse(netcommdef.NetworkCommissioningStatusOutOfRange, 0), nil
	}
	n.breadcrumbLocked(req.Breadcrumb)
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, 0), nil
}

// connect is HandleConnectNetwork (:634-683) and OnResult (:843-902)
// over the driver's ConnectNetwork (GTD :205-268): a network id over 32
// bytes is a ConstraintError; a failsafe is required; an unknown network
// is NetworkIDNotFound; otherwise the configuration is backed up and the
// border router attaches the staged network (AttachToThreadNetwork,
// through the port's SetActiveDataset). The result is the
// ConnectNetworkResponse, the last networking status and network id, the
// connect error (null on success); success writes the breadcrumb. A
// failed attach is UnknownError (GTD :258-262).
func (n *ThreadNetworkCommissioning) connect(ctx context.Context, req netcommdef.ConnectNetworkRequest) (any, error) {
	if len(req.NetworkId) > maxNetworkIDLength {
		return nil, netcommStatus(im.StatusConstraintError, "core: ConnectNetwork network id of %d bytes", len(req.NetworkId))
	}
	if !n.armed() {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: ConnectNetwork needs an armed failsafe")
	}
	n.mu.Lock()
	status := n.matchLocked(req.NetworkId)
	staged := slices.Clone(n.staging)
	if statusOK(status) {
		n.backupLocked()
		n.busy = true
	}
	n.mu.Unlock()
	var errValue int32
	var enabled, connected bool
	if statusOK(status) {
		if err := n.br.SetActiveDataset(ctx, staged); err != nil {
			status, errValue = netcommdef.NetworkCommissioningStatusUnknownError, -1
		} else {
			enabled, connected = n.probe(context.WithoutCancel(ctx), staged)
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.busy = false
	resp := netcommdef.ConnectNetworkResponse{NetworkingStatus: status, ErrorValue: spec.Nullable[int32]{Null: true}}
	if statusOK(status) {
		n.lastConnectErr = nil
		n.enabled, n.connected = enabled, connected
	} else {
		resp.ErrorValue = spec.Nullable[int32]{Value: errValue}
		v := errValue
		n.lastConnectErr = &v
	}
	n.lastNetworkID = bytes.Clone(req.NetworkId)
	n.setStatusLocked(status)
	n.dataVersion.Bump()
	if statusOK(status) {
		n.breadcrumbLocked(req.Breadcrumb)
	}
	return resp, nil
}

// OnCommissioningComplete drops the fail-safe's backup
// (CommitConfiguration, NetworkCommissioningCluster.cpp:984-990; GTD
// :97-106).
func (n *ThreadNetworkCommissioning) OnCommissioningComplete(context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.backup = nil
}

// OnFailSafeExpired restores the backed-up network (RevertConfiguration,
// :992-1014; GTD :108-156): without a backup nothing changed since the
// fail-safe was armed; with one the staged network is the backup and the
// border router attaches it — the empty dataset of a border router that
// had none clears its active dataset, as chip attaches an empty one. The
// backup goes either way.
func (n *ThreadNetworkCommissioning) OnFailSafeExpired(ctx context.Context) {
	n.mu.Lock()
	backup := n.backup
	n.backup = nil
	n.busy = false
	if backup != nil {
		n.staging = backup
		if len(backup) == 0 {
			n.lastNetworkID, n.lastConnectErr, n.lastStatus = nil, nil, nil
		}
	}
	staging := slices.Clone(n.staging)
	n.mu.Unlock()
	if backup != nil {
		_ = n.br.SetActiveDataset(ctx, backup)
	}
	enabled, connected := n.probe(ctx, staging)
	n.mu.Lock()
	defer n.mu.Unlock()
	n.enabled, n.connected = enabled, connected
	n.dataVersion.Bump()
}
