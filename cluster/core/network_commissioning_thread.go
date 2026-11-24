// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	tbrmdef "thingfabric.dev/go-fabric/cluster/spec/threadborderroutermanagement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/thread"
)

// ThreadNetworkCommissioning is the NetworkCommissioning cluster (0x0031)
// with the Thread feature over a Thread Border Router's
// [tbrmdef.BorderRouter], the one cluster/threadbr's management server
// manages. Both clusters then reflect one network, as the border router's
// device type demands of an endpoint that also supports the secondary
// network interface device type (matter.js
// packages/node/src/devices/thread-border-router.ts:34-39 at b25ed960;
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
	failSafe      func() (fabricIndex uint8, armed bool)
	failSafeArmed func() bool
	// blindWarned logs, once, that only the deprecated fabric-blind port
	// answers the gate (armedFor); logger is where
	// ([ThreadNetworkCommissioningConfig.Logger]), nil slog.Default().
	blindWarned   sync.Once
	logger        *slog.Logger
	setBreadcrumb func(uint64)
	scanMax       uint8
	connectMax    uint8
	threadVersion uint16

	// staging is mStagingNetwork (GTD :38-44): the network the commands
	// edit, attached by ConnectNetwork. backup is the fail-safe's copy
	// (FailSafeNetworkConfig, BackupConfiguration GTD :301-315), nil for
	// none.
	staging []byte
	backup  []byte
	// connected and enabled are what the last read of the border router
	// found: attached to the staged network, and its interface up.
	connected bool
	enabled   bool
	// busy is held by a running ScanNetworks or ConnectNetwork, claimed
	// in the same hold as MatterInvoke's check (busyGen names the claim)
	// and released by the command itself when it returns, also after a
	// fail-safe expiry: one border-router call of a command at a time
	// (review II, R2-P6-conc-1026).
	busy    bool
	busyGen uint64
	// expiries counts the fail-safe expiries (OnFailSafeExpired): a
	// ConnectNetwork whose attach outlives the window that admitted it
	// sees it moved and drops its result; connectCancel cancels that
	// attach at the expiry (review II, R2-P6-fsm-1001).
	expiries      uint64
	connectCancel context.CancelFunc

	lastStatus     *uint8
	lastNetworkID  []byte
	lastConnectErr *int32

	// changes reports what moves outside the cluster's own commands — the
	// fail-safe's expiry, the re-attach after it, Refresh
	// ([ThreadNetworkCommissioning.OnAttributeChange]; R3-par-3051).
	changes cluster.AttributeChanges
}

// ThreadNetworkCommissioningConfig drives [NewThreadNetworkCommissioning].
type ThreadNetworkCommissioningConfig struct {
	// BorderRouter is the border router; required.
	BorderRouter tbrmdef.BorderRouter
	// FailSafe is the node's fail-safe port — armed, and for which fabric
	// ([GeneralCommissioning.FailSafeFabric]); the network commands need
	// the window of their accessing fabric (EnsureFailsafeIsArmed over
	// IsFailSafeArmed(GetAccessingFabricIndex()),
	// NetworkCommissioningCluster.cpp:155-172, :380, :568, :609, :644 at
	// 6170af84; by_design BD-chip-FailSafeOfTheAccessingFabric). Nil reads
	// as disarmed until [ThreadNetworkCommissioning.AttachFailSafe] wires
	// it, unless IsFailSafeArmed is set.
	FailSafe func() (fabricIndex uint8, armed bool)
	// IsFailSafeArmed is the fabric-blind port (armed for any fabric);
	// consulted only while FailSafe is nil. While it alone answers,
	// AddOrUpdateThreadNetwork, RemoveNetwork and ConnectNetwork pass in
	// any fabric's fail-safe window, and the server logs that once
	// (netcomm.failsafe_blind_port).
	//
	// Deprecated: wire FailSafe, or [ThreadNetworkCommissioning.AttachFailSafe],
	// the fabric-aware port ([GeneralCommissioning.FailSafeFabric]),
	// instead; this field keeps its behaviour until it goes and is
	// removable in v0.28.0 (review II, R2-C2-sec-1006).
	IsFailSafeArmed func() bool
	// SetBreadcrumb writes GeneralCommissioning's Breadcrumb
	// (UpdateBreadcrumb, :589-593); nil writes none.
	SetBreadcrumb func(uint64)
	// ScanMaxTimeSeconds and ConnectMaxTimeSeconds; 0 are the Thread
	// driver's 10 and 20 (GenericNetworkCommissioningThreadDriver.h:87-90).
	ScanMaxTimeSeconds, ConnectMaxTimeSeconds uint8
	// Logger receives the server's lines — the one warning that only the
	// deprecated IsFailSafeArmed port answers the gate; nil is
	// slog.Default(), as the Wi-Fi server's
	// ([WiFiNetworkCommissioningConfig.Logger]).
	Logger *slog.Logger
}

// threadNetcommInst is the definition bound to the Thread feature.
var threadNetcommInst = mustInstance(netcommdef.Definition, spec.Options{Features: NetworkCommFeatureThread})

// threadCapabilities is SupportedThreadFeatures of a border router: a
// border-router-capable full Thread device that can route
// (GetSupportedThreadFeatures, GTD :387-398, with
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
		br: cfg.BorderRouter, failSafe: cfg.FailSafe, failSafeArmed: cfg.IsFailSafeArmed, setBreadcrumb: cfg.SetBreadcrumb,
		logger:  cfg.Logger,
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
	_ contract.AttributeChangeNotifier        = (*ThreadNetworkCommissioning)(nil)
)

// OnAttributeChange implements [contract.AttributeChangeNotifier]: the
// fail-safe's expiry ([ThreadNetworkCommissioning.OnFailSafeExpired]), the
// re-attach of the reverted network after an abandoned ConnectNetwork and
// [ThreadNetworkCommissioning.Refresh] change Networks, InterfaceEnabled
// and — when no network is left after the revert and one of them held a
// value — LastNetworkingStatus, LastNetworkID and LastConnectErrorValue
// outside a command of this
// cluster (the bridge reports a command's effect on the cluster it
// invoked), so each tells the listeners once the lock is released, as the
// Wi-Fi server does ([WiFiNetworkCommissioning.OnAttributeChange];
// R3-par-3051). connectedhomeip reports at the same point:
// ReportNetworksListChanged() after the revert and the three setters, each
// NotifyAttributeChanged (NetworkCommissioningCluster.cpp:250-281,
// :992-1014 at 6170af84).
func (n *ThreadNetworkCommissioning) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return n.changes.OnAttributeChange(cb)
}

// AttachFailSafe wires the node's fail-safe after construction, as
// [ThreadNetworkCommissioningConfig.FailSafe] does.
func (n *ThreadNetworkCommissioning) AttachFailSafe(failSafe func() (fabricIndex uint8, armed bool), setBreadcrumb func(uint64)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.failSafe, n.setBreadcrumb = failSafe, setBreadcrumb
}

// Refresh reads the border router's attachment and interface again
// (Networks' Connected, InterfaceEnabled) and reports a change
// ([ThreadNetworkCommissioning.OnAttributeChange]).
func (n *ThreadNetworkCommissioning) Refresh(ctx context.Context) {
	n.mu.Lock()
	staging := slices.Clone(n.staging)
	n.mu.Unlock()
	enabled, connected := n.probe(ctx, staging)
	n.mu.Lock()
	moved := n.setLinkLocked(enabled, connected)
	n.mu.Unlock()
	n.changes.Notify(moved...)
}

// setLinkLocked takes what a read of the border router found and answers
// the attributes it moved: Networks when Connected changed,
// InterfaceEnabled when the interface did; the DataVersion advances when
// either did. Caller holds mu.
func (n *ThreadNetworkCommissioning) setLinkLocked(enabled, connected bool) []uint32 {
	var moved []uint32
	if connected != n.connected {
		moved = append(moved, netcommAttrNetworks)
	}
	if enabled != n.enabled {
		moved = append(moved, netcommAttrInterfaceEnabled)
	}
	n.enabled, n.connected = enabled, connected
	if len(moved) > 0 {
		n.dataVersion.Bump()
	}
	return moved
}

// probe reads the border router's interface and, as
// ThreadNetworkIterator::Next's connected (GTD :359-385), whether the
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
// :354-385), SupportedThreadFeatures and ThreadVersion (:1102-1108).
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
// in-process stack (SetEnabled, GTD :252-266), and the module wraps no
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

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the only
// write accepted is the InterfaceEnabled value the border router already
// has, so no write changes the server.
func (n *ThreadNetworkCommissioning) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return false, n.MatterWrite(ctx, attrID, value)
}

// MatterInvoke implements [contract.ClusterServer] (InvokeCommand,
// :1138-1194): Busy while a scan or a connect runs.
func (n *ThreadNetworkCommissioning) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	n.mu.Lock()
	if n.busy {
		n.mu.Unlock()
		return nil, netcommStatus(im.StatusBusy, "core: NetworkCommissioning is busy")
	}
	if cmdID == netcommdef.CmdScanNetworks || cmdID == netcommdef.CmdConnectNetwork {
		// Checked and claimed in one hold: two commands never both pass.
		n.busy = true
		n.busyGen++
		defer n.releaseBusy(n.busyGen)
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
		return n.addOrUpdate(ctx, req)
	case netcommdef.CmdRemoveNetwork:
		req, ok := fields.(netcommdef.RemoveNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: RemoveNetwork fields %T", fields)
		}
		return n.remove(ctx, req)
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

// armedFor is connectedhomeip's EnsureFailsafeIsArmed(failSafeContext,
// handler.GetAccessingFabricIndex()) (NetworkCommissioningCluster.cpp:155-172,
// :380, :568, :609, :644 at 6170af84): the window must be armed, and armed
// for the accessing fabric (FailSafeContext.h:76-79). A fabric-blind port
// ([ThreadNetworkCommissioningConfig.IsFailSafeArmed]) answers for any
// fabric, as before, and is logged once (R2-C2-sec-1006).
func (n *ThreadNetworkCommissioning) armedFor(ctx context.Context) bool {
	n.mu.Lock()
	failSafe, armed, logger := n.failSafe, n.failSafeArmed, n.logger
	n.mu.Unlock()
	if failSafe != nil {
		_, accessing := im.FabricFilterFromContext(ctx)
		return FailSafeArmedFor(failSafe, accessing)
	}
	if armed == nil {
		return false
	}
	if logger == nil {
		logger = slog.Default()
	}
	n.blindWarned.Do(func() { warnBlindFailSafePort(logger, "thread") })
	return armed()
}

// expiryWindow is the count of fail-safe expiries, read before the gate
// (armedFor): a command compares it again in the hold that writes, and an
// expiry in between fails it with FAILSAFE_REQUIRED — its backup would
// outlive the window that admitted it, with no expiry left to revert it
// (review II, R3-fsm-3055).
func (n *ThreadNetworkCommissioning) expiryWindow() uint64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.expiries
}

// errWindowExpired is the FAILSAFE_REQUIRED of a command whose window
// expired after its gate passed ([ThreadNetworkCommissioning.expiryWindow],
// [WiFiNetworkCommissioning.expiryWindow]).
func errWindowExpired(cmd string) error {
	return netcommStatus(im.StatusFailsafeRequired, "core: %s: the failsafe expired while the command ran", cmd)
}

// releaseBusy ends the command's claim gen.
func (n *ThreadNetworkCommissioning) releaseBusy(gen uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.busyGen == gen {
		n.busy = false
	}
}

// breadcrumb is UpdateBreadcrumb (:589-593). It runs without n.mu: the
// write reaches GeneralCommissioning's listeners and the host's hook, which
// may read this server (review II, R2-P6-conc-1011; the Wi-Fi server's
// R2-P1-core-07).
func (n *ThreadNetworkCommissioning) breadcrumb(bc *uint64) {
	n.mu.Lock()
	set := n.setBreadcrumb
	n.mu.Unlock()
	if bc != nil && set != nil {
		set(*bc)
	}
}

func (n *ThreadNetworkCommissioning) setStatusLocked(s netcommdef.NetworkCommissioningStatusEnum) {
	v := uint8(s)
	n.lastStatus = &v
}

// backupLocked is BackupConfiguration (GTD :301-315): the staged network
// as it was when the fail-safe's first change came, once.
func (n *ThreadNetworkCommissioning) backupLocked() {
	if n.backup == nil {
		n.backup = append([]byte{}, n.staging...)
	}
}

// matchLocked is MatchesNetworkId (GTD :284-299): NetworkIDNotFound for
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
	n.setStatusLocked(status)
	n.dataVersion.Bump()
	n.mu.Unlock()
	if statusOK(status) {
		n.breadcrumb(req.Breadcrumb)
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
// driver's AddOrUpdateNetwork (GTD :156-176): a failsafe is required
// (:570); a dataset that does not frame or is not commissioned is
// OutOfRange; a staged commissioned network of another Extended PAN ID is
// BoundsExceeded (one network); otherwise the staged network is backed up
// and replaced, the breadcrumb written and the list changed (:578-582).
func (n *ThreadNetworkCommissioning) addOrUpdate(ctx context.Context, req netcommdef.AddOrUpdateThreadNetworkRequest) (any, error) {
	window := n.expiryWindow()
	if !n.armedFor(ctx) {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: AddOrUpdateThreadNetwork needs an armed failsafe")
	}
	xp, ok := extPANIDOf(req.OperationalDataset)
	if !ok {
		return configResponse(netcommdef.NetworkCommissioningStatusOutOfRange, 0), nil
	}
	n.mu.Lock()
	if n.expiries != window {
		n.mu.Unlock()
		return nil, errWindowExpired("AddOrUpdateThreadNetwork")
	}
	if staged, commissioned := extPANIDOf(n.staging); commissioned && !bytes.Equal(staged, xp) {
		n.mu.Unlock()
		return configResponse(netcommdef.NetworkCommissioningStatusBoundsExceeded, 0), nil
	}
	n.backupLocked()
	n.staging = slices.Clone(req.OperationalDataset)
	n.connected = false
	n.dataVersion.Bump()
	n.mu.Unlock()
	n.breadcrumb(req.Breadcrumb)
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, 0), nil
}

// remove is HandleRemoveNetwork (:603-632) over the driver's
// RemoveNetwork (GTD :178-191): a failsafe is required; an unknown
// network is NetworkIDNotFound; the staged network is backed up and
// cleared, and the last status, network id and connect error with it.
func (n *ThreadNetworkCommissioning) remove(ctx context.Context, req netcommdef.RemoveNetworkRequest) (any, error) {
	window := n.expiryWindow()
	if !n.armedFor(ctx) {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: RemoveNetwork needs an armed failsafe")
	}
	n.mu.Lock()
	if n.expiries != window {
		n.mu.Unlock()
		return nil, errWindowExpired("RemoveNetwork")
	}
	if st := n.matchLocked(req.NetworkId); !statusOK(st) {
		n.mu.Unlock()
		return configResponse(st, 0), nil
	}
	n.backupLocked()
	n.staging, n.connected = nil, false
	n.lastNetworkID, n.lastConnectErr, n.lastStatus = nil, nil, nil
	n.dataVersion.Bump()
	n.mu.Unlock()
	n.breadcrumb(req.Breadcrumb)
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, 0), nil
}

// reorder is HandleReorderNetwork (:712-730) over the driver's
// ReorderNetwork (GTD :193-203): an unknown network is
// NetworkIDNotFound, an index other than 0 OutOfRange.
func (n *ThreadNetworkCommissioning) reorder(req netcommdef.ReorderNetworkRequest) (any, error) {
	n.mu.Lock()
	st := n.matchLocked(req.NetworkId)
	n.mu.Unlock()
	if !statusOK(st) {
		return configResponse(st, 0), nil
	}
	if req.NetworkIndex != 0 {
		return configResponse(netcommdef.NetworkCommissioningStatusOutOfRange, 0), nil
	}
	n.breadcrumb(req.Breadcrumb)
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, 0), nil
}

// connect is HandleConnectNetwork (:634-683) and OnResult (:843-902)
// over the driver's ConnectNetwork (GTD :205-250): a network id over 32
// bytes is a ConstraintError; a failsafe is required; an unknown network
// is NetworkIDNotFound; otherwise the configuration is backed up and the
// border router attaches the staged network (AttachToThreadNetwork,
// through the port's SetActiveDataset). The result is the
// ConnectNetworkResponse, the last networking status and network id, the
// connect error (null on success); success writes the breadcrumb. A
// failed attach is UnknownError (GTD :240-244).
func (n *ThreadNetworkCommissioning) connect(ctx context.Context, req netcommdef.ConnectNetworkRequest) (any, error) {
	if len(req.NetworkId) > maxNetworkIDLength {
		return nil, netcommStatus(im.StatusConstraintError, "core: ConnectNetwork network id of %d bytes", len(req.NetworkId))
	}
	window := n.expiryWindow()
	if !n.armedFor(ctx) {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: ConnectNetwork needs an armed failsafe")
	}
	n.mu.Lock()
	if n.expiries != window {
		n.mu.Unlock()
		return nil, errWindowExpired("ConnectNetwork")
	}
	status := n.matchLocked(req.NetworkId)
	staged := slices.Clone(n.staging)
	attach, cancel := context.WithCancel(ctx)
	defer cancel()
	if statusOK(status) {
		n.backupLocked()
		n.connectCancel = cancel
	}
	n.mu.Unlock()
	var errValue int32
	var enabled, connected bool
	if statusOK(status) {
		if err := n.br.SetActiveDataset(attach, staged); err != nil {
			status, errValue = netcommdef.NetworkCommissioningStatusUnknownError, -1
		} else {
			enabled, connected = n.probe(context.WithoutCancel(ctx), staged)
		}
	}
	n.mu.Lock()
	n.connectCancel = nil
	resp := netcommdef.ConnectNetworkResponse{NetworkingStatus: status, ErrorValue: spec.Nullable[int32]{Null: true}}
	if !statusOK(status) {
		resp.ErrorValue = spec.Nullable[int32]{Value: errValue}
	}
	if n.expiries != window {
		// The fail-safe that admitted the command expired while the border
		// router attached, and OnFailSafeExpired reverted the staged
		// network: the result writes no attribute and no Breadcrumb, the
		// reverted dataset is attached again, since this attach may have
		// landed after the expiry's revert, and the command is answered
		// with nothing ([im.NoResponse]; review II, R2-P6-fsm-1001,
		// F-db021da0-10). Follows connectedhomeip
		// src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp
		// at 6170af84, whose one class serves Wi-Fi and Thread: the
		// fail-safe expiry reverts the driver and releases the pending
		// command handle (:1001-1002), and a driver result that arrives
		// afterwards returns before it adds a ConnectNetworkResponse
		// (:845-857).
		n.mu.Unlock()
		n.reattachReverted(context.WithoutCancel(ctx), window)
		return im.NoResponse{}, nil
	}
	defer func() {
		if statusOK(status) {
			n.breadcrumb(req.Breadcrumb)
		}
	}()
	defer n.mu.Unlock()
	if statusOK(status) {
		n.lastConnectErr = nil
		n.enabled, n.connected = enabled, connected
	} else {
		v := errValue
		n.lastConnectErr = &v
	}
	n.lastNetworkID = bytes.Clone(req.NetworkId)
	n.setStatusLocked(status)
	n.dataVersion.Bump()
	return resp, nil
}

// reattachReverted attaches the staged network the fail-safe's expiry
// restored once more, after a ConnectNetwork the expiry abandoned returned:
// its attach may have reached the border router after the expiry's revert.
// It does nothing when a later fail-safe has changed the staged network
// since (a backup exists again) or another expiry followed.
func (n *ThreadNetworkCommissioning) reattachReverted(ctx context.Context, window uint64) {
	n.mu.Lock()
	if n.backup != nil || n.expiries != window+1 {
		n.mu.Unlock()
		return
	}
	staging := slices.Clone(n.staging)
	n.mu.Unlock()
	_ = n.br.SetActiveDataset(ctx, staging) // as the expiry's own revert: attempted once
	enabled, connected := n.probe(ctx, staging)
	n.mu.Lock()
	var moved []uint32
	if n.backup == nil && n.expiries == window+1 {
		moved = n.setLinkLocked(enabled, connected)
	}
	n.mu.Unlock()
	n.changes.Notify(moved...)
}

// OnCommissioningComplete drops the fail-safe's backup
// (CommitConfiguration, NetworkCommissioningCluster.cpp:984-990; GTD
// :101-110).
func (n *ThreadNetworkCommissioning) OnCommissioningComplete(context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.backup = nil
}

// OnFailSafeExpired restores the backed-up network (RevertConfiguration,
// :992-1014; GTD :112-154): without a backup nothing changed since the
// fail-safe was armed; with one the staged network is the backup and the
// border router attaches it — the empty dataset of a border router that
// had none clears its active dataset, as chip attaches an empty one. The
// backup goes either way.
//
// The revert is attempted once. When the border router refuses it or
// cannot be reached, nothing retries, logs or reports it: the
// border router stays on the dataset of the commissioning the fail-safe
// rolled back, the server holds the backup as its staged network, and
// Networks reports it not connected (review II, R2-P6-err-1002; chip
// ignores RevertConfiguration's result, NetworkCommissioningCluster.cpp:1001,
// and its driver drops the backup whether or not it was restored,
// GenericNetworkCommissioningThreadDriver.cpp:150-153 at 6170af84).
//
// A ConnectNetwork the window admitted that still runs is abandoned: its
// attach is cancelled, its result writes no attribute and no Breadcrumb,
// and once it returned the reverted network is attached again
// (reattachReverted) (review II, R2-P6-fsm-1001). For the state this
// follows connectedhomeip
// src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp
// at 6170af84: the fail-safe expiry reverts the driver and releases the
// pending command handle (:1001-1002), and a driver result that arrives
// afterwards returns before it writes LastNetworkID, LastNetworkingStatus,
// LastConnectErrorValue or the Breadcrumb and before it adds a
// ConnectNetworkResponse (:845-857), and the command is answered with
// nothing ([im.NoResponse]), as chip sends nothing for it
// (src/app/CommandHandlerImpl.cpp:481-488, :1000;
// src/app/CommandResponseSender.cpp:82-102 at 6170af84).
func (n *ThreadNetworkCommissioning) OnFailSafeExpired(ctx context.Context) {
	n.mu.Lock()
	n.expiries++
	if n.connectCancel != nil {
		// The ConnectNetwork this window admitted is abandoned: its attach
		// is cancelled, and it keeps the command's turn (busy) until it
		// returned and re-attached the reverted network.
		n.connectCancel()
	}
	backup := n.backup
	n.backup = nil
	// Networks is reported after every revert, as the staged network may
	// have moved (OnAttributeChange).
	moved := []uint32{netcommAttrNetworks}
	if backup != nil {
		n.staging = backup
	}
	// With no network left after the revert — Networks reads empty — the
	// three Last… attributes are cleared, whether or not the window changed
	// the staged network: a window that saw only a ScanNetworks leaves its
	// LastNetworkingStatus otherwise (R4-par-4002; the Wi-Fi server's rule).
	// They are reported when one of them held a value.
	if _, ok := extPANIDOf(n.staging); !ok {
		if len(n.lastNetworkID) > 0 || n.lastConnectErr != nil || n.lastStatus != nil {
			moved = append(moved, netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue)
		}
		n.lastNetworkID, n.lastConnectErr, n.lastStatus = nil, nil, nil
	}
	staging := slices.Clone(n.staging)
	n.mu.Unlock()
	if backup != nil {
		_ = n.br.SetActiveDataset(ctx, backup) // attempted once, see the doc
	}
	enabled, connected := n.probe(ctx, staging)
	n.mu.Lock()
	if enabled != n.enabled {
		moved = append(moved, netcommAttrInterfaceEnabled)
	}
	n.enabled, n.connected = enabled, connected
	n.dataVersion.Bump()
	n.mu.Unlock()
	n.changes.Notify(moved...)
}
