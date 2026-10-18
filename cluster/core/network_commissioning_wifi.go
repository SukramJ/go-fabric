// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// WiFiNetworkCommissioning is the NetworkCommissioning cluster (0x0031)
// with the Wi-Fi feature, driven by a host [contract.WiFiDriver]. The
// module's nodes are Ethernet-attached ([NetworkCommissioning]); this
// server exists so the commissioner's network steps can be run against
// the module's own node — the reference daemon's --wifi-sim, TESTING
// ONLY.
//
// matter.js's server is a generated stub
// (packages/node/src/behaviors/network-commissioning/NetworkCommissioningServer.ts
// at 0ceb202d), so the command semantics, the statuses and the list
// rules are connectedhomeip's at the harness pin:
// src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp
// (the brief's network-commissioning.cpp is that file at the pin) for
// the commands, and src/platform/Linux/NetworkCommissioningWiFiDriver.cpp
// for the network list, generalized from its one network to MaxNetworks.
type WiFiNetworkCommissioning struct {
	mu          sync.Mutex
	dataVersion cluster.DataVersionTracker

	driver        contract.WiFiDriver
	failSafeArmed func() bool
	setBreadcrumb func(uint64)
	maxNetworks   uint8
	scanMax       uint8
	connectMax    uint8

	interfaceEnabled bool
	// networks is the staged list, committed the list a CommissioningComplete
	// confirmed, restored when the failsafe expires (CommitConfiguration,
	// RevertConfiguration).
	networks  []wifiNetwork
	committed []wifiNetwork
	busy      bool

	lastStatus     *uint8
	lastNetworkID  []byte // empty reads as null (ReadAttribute :1087-1092)
	lastConnectErr *int32
}

type wifiNetwork struct {
	ssid, credentials []byte
	connected         bool
}

// WiFiNetworkCommissioningConfig drives [NewWiFiNetworkCommissioning].
type WiFiNetworkCommissioningConfig struct {
	// Driver scans and connects. Required.
	Driver contract.WiFiDriver
	// IsFailSafeArmed reports whether a failsafe is armed
	// (GeneralCommissioning.FailSafeArmed); the network commands need one
	// (EnsureFailsafeIsArmed, NetworkCommissioningCluster.cpp:155-162).
	IsFailSafeArmed func() bool
	// SetBreadcrumb writes GeneralCommissioning's Breadcrumb
	// (UpdateBreadcrumb, :589-593); nil writes none.
	SetBreadcrumb func(uint64)
	// MaxNetworks is the number of networks the list holds; 0 is 1, the
	// Linux driver's (NetworkCommissioningDriver.h:85).
	MaxNetworks uint8
	// ScanMaxTimeSeconds and ConnectMaxTimeSeconds; 0 are the Linux
	// driver's 10 and 20 (NetworkCommissioningDriver.h:86-87).
	ScanMaxTimeSeconds, ConnectMaxTimeSeconds uint8
}

// wifiNetcommInst is the definition bound to the Wi-Fi feature.
var wifiNetcommInst = mustInstance(netcommdef.Definition, spec.Options{Features: NetworkCommFeatureWiFi})

// Wi-Fi limits: kMaxWiFiSSIDLength and kMaxWiFiKeyLength
// (src/include/platform/internal/DeviceNetworkInfo.h:35-36), kMaxNetworkIDLen
// (src/include/platform/NetworkCommissioning.h:55) and the credential
// lengths of NetworkCommissioningCluster.cpp:82-89.
const (
	maxWiFiSSIDLength    = 32
	maxWiFiKeyLength     = 64
	maxNetworkIDLength   = 32
	wifiCredOpen         = 0
	wifiCredWEP64        = 5
	wifiCredMinWPAPSK    = 8
	wifiCredMaxWPAPSK    = 63
	wifiCredWPAPSKHexLen = 64
)

// NewWiFiNetworkCommissioning returns the Wi-Fi NetworkCommissioning
// server: an empty network list, the interface enabled, the last
// networking status, network id and connect error null.
func NewWiFiNetworkCommissioning(cfg WiFiNetworkCommissioningConfig) (*WiFiNetworkCommissioning, error) {
	if cfg.Driver == nil {
		return nil, errors.New("matter: Wi-Fi NetworkCommissioning needs a driver")
	}
	w := &WiFiNetworkCommissioning{
		driver: cfg.Driver, failSafeArmed: cfg.IsFailSafeArmed, setBreadcrumb: cfg.SetBreadcrumb,
		maxNetworks: cfg.MaxNetworks, scanMax: cfg.ScanMaxTimeSeconds, connectMax: cfg.ConnectMaxTimeSeconds,
		interfaceEnabled: true,
	}
	if w.maxNetworks == 0 {
		w.maxNetworks = 1
	}
	if w.scanMax == 0 {
		w.scanMax = 10
	}
	if w.connectMax == 0 {
		w.connectMax = 20
	}
	if w.failSafeArmed == nil {
		w.failSafeArmed = func() bool { return false }
	}
	w.dataVersion.Bump()
	return w, nil
}

var (
	_ contract.ClusterServer                  = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterAttributeLister         = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterDataVersion             = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterAttributeReadPrivilege  = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterCommandLister           = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*WiFiNetworkCommissioning)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (w *WiFiNetworkCommissioning) MatterClusterID() uint32 { return netcommClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (w *WiFiNetworkCommissioning) MatterDataVersion() uint32 { return w.dataVersion.Current() }

// MatterAttributes implements [contract.ClusterAttributeLister]: the
// Wi-Fi feature's attributes.
func (w *WiFiNetworkCommissioning) MatterAttributes() []uint32 {
	return wifiNetcommInst.MatterAttributes()
}

// MatterReportable lists the subscribe-able attributes.
func (w *WiFiNetworkCommissioning) MatterReportable() []uint32 {
	return []uint32{netcommAttrNetworks, netcommAttrInterfaceEnabled, netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue}
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// ScanNetworks, AddOrUpdateWiFiNetwork, RemoveNetwork, ConnectNetwork,
// ReorderNetwork.
func (w *WiFiNetworkCommissioning) MatterAcceptedCommands() []uint32 {
	return wifiNetcommInst.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (w *WiFiNetworkCommissioning) MatterGeneratedCommands() []uint32 {
	return wifiNetcommInst.MatterGeneratedCommands()
}

// MinReadPrivilege implements [contract.ClusterAttributeReadPrivilege].
func (w *WiFiNetworkCommissioning) MinReadPrivilege(attrID uint32) uint8 {
	return wifiNetcommInst.MinReadPrivilege(attrID)
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege].
func (w *WiFiNetworkCommissioning) MinWritePrivilege(attrID uint32) uint8 {
	return wifiNetcommInst.MinWritePrivilege(attrID)
}

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege]:
// Administer for every command.
func (w *WiFiNetworkCommissioning) MinInvokePrivilege(cmdID uint32) uint8 {
	return wifiNetcommInst.MinInvokePrivilege(cmdID)
}

// wifiBands is SupportedWiFiBands: 2.4 GHz.
type wifiBands []netcommdef.WiFiBandEnum

func (b wifiBands) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	for _, v := range b {
		enc.PutUint(tlv.AnonymousTag(), uint64(v))
	}
	_ = enc.EndContainer()
}

// MatterRead implements [contract.ClusterServer]
// (ReadAttribute, NetworkCommissioningCluster.cpp:1067-1113).
func (w *WiFiNetworkCommissioning) MatterRead(attrID uint32) (any, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch attrID {
	case netcommAttrMaxNetworks:
		return w.maxNetworks, true
	case netcommAttrNetworks:
		out := make(spec.List[netcommdef.NetworkInfoStruct], 0, len(w.networks))
		for _, n := range w.networks {
			out = append(out, netcommdef.NetworkInfoStruct{NetworkId: bytes.Clone(n.ssid), Connected: n.connected})
		}
		return out, true
	case netcommAttrScanMaxTimeSeconds:
		return w.scanMax, true
	case netcommAttrConnectMaxTimeSeconds:
		return w.connectMax, true
	case netcommAttrInterfaceEnabled:
		return w.interfaceEnabled, true
	case netcommAttrLastNetworkingStatus:
		if w.lastStatus == nil {
			return nil, true
		}
		return *w.lastStatus, true
	case netcommAttrLastNetworkID:
		if len(w.lastNetworkID) == 0 {
			return nil, true
		}
		return bytes.Clone(w.lastNetworkID), true
	case netcommAttrLastConnectErrorValue:
		if w.lastConnectErr == nil {
			return nil, true
		}
		return *w.lastConnectErr, true
	case netcommdef.AttrSupportedWiFiBands:
		return wifiBands{netcommdef.WiFiBandV2G4}, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return wifiNetcommInst.ReadGlobal(attrID)
	}
	return nil, false
}

// MatterWrite handles InterfaceEnabled (WriteAttribute, :1115-1136).
func (w *WiFiNetworkCommissioning) MatterWrite(_ context.Context, attrID uint32, value any) error {
	if attrID != netcommAttrInterfaceEnabled {
		_, err := wifiNetcommInst.ValidateWrite(attrID, value, nil)
		return refusedWrite{sentinel: fmt.Errorf("matter: NetworkCommissioning attribute 0x%04X is read-only", attrID), status: err}
	}
	v, ok := value.(bool)
	if !ok {
		return fmt.Errorf("matter: InterfaceEnabled write expected bool, got %T", value)
	}
	w.mu.Lock()
	w.interfaceEnabled = v
	w.dataVersion.Bump()
	w.mu.Unlock()
	return nil
}

// netcommStatusError is a command refused with an IM status.
type netcommStatusError struct {
	code im.StatusCode
	msg  string
}

func (e netcommStatusError) Error() string                   { return e.msg }
func (e netcommStatusError) MatterStatusCode() im.StatusCode { return e.code }

func netcommStatus(code im.StatusCode, format string, args ...any) error {
	return netcommStatusError{code: code, msg: fmt.Sprintf(format, args...)}
}

// MatterInvoke implements [contract.ClusterServer] (InvokeCommand,
// :1138-1194): Busy while a scan or a connect runs.
func (w *WiFiNetworkCommissioning) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	w.mu.Lock()
	if w.busy {
		w.mu.Unlock()
		return nil, netcommStatus(im.StatusBusy, "matter: NetworkCommissioning is busy")
	}
	w.mu.Unlock()
	switch cmdID {
	case netcommdef.CmdScanNetworks:
		req, ok := fields.(netcommdef.ScanNetworksRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "matter: ScanNetworks fields %T", fields)
		}
		return w.scanNetworks(ctx, req)
	case netcommdef.CmdAddOrUpdateWiFiNetwork:
		req, ok := fields.(netcommdef.AddOrUpdateWiFiNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "matter: AddOrUpdateWiFiNetwork fields %T", fields)
		}
		return w.addOrUpdate(req)
	case netcommdef.CmdRemoveNetwork:
		req, ok := fields.(netcommdef.RemoveNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "matter: RemoveNetwork fields %T", fields)
		}
		return w.remove(req)
	case netcommdef.CmdConnectNetwork:
		req, ok := fields.(netcommdef.ConnectNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "matter: ConnectNetwork fields %T", fields)
		}
		return w.connect(ctx, req)
	case netcommdef.CmdReorderNetwork:
		req, ok := fields.(netcommdef.ReorderNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "matter: ReorderNetwork fields %T", fields)
		}
		return w.reorder(req)
	}
	return nil, im.UnsupportedCommandf("matter: NetworkCommissioning command 0x%02X not supported (Wi-Fi)", cmdID)
}

func (w *WiFiNetworkCommissioning) breadcrumb(bc *uint64) {
	if bc != nil && w.setBreadcrumb != nil {
		w.setBreadcrumb(*bc)
	}
}

func (w *WiFiNetworkCommissioning) setStatusLocked(s netcommdef.NetworkCommissioningStatusEnum) {
	v := uint8(s)
	w.lastStatus = &v
}

func (w *WiFiNetworkCommissioning) indexLocked(id []byte) int {
	return slices.IndexFunc(w.networks, func(n wifiNetwork) bool { return bytes.Equal(n.ssid, id) })
}

func statusOK(s netcommdef.NetworkCommissioningStatusEnum) bool {
	return s == netcommdef.NetworkCommissioningStatusSuccess
}

func debugText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// configResponse is a NetworkConfigResponse with the index on success
// (FillDebugTextAndNetworkIndex, :143-153).
func configResponse(status netcommdef.NetworkCommissioningStatusEnum, index int) netcommdef.NetworkConfigResponse {
	r := netcommdef.NetworkConfigResponse{NetworkingStatus: status}
	if statusOK(status) {
		i := uint8(index) //nolint:gosec // bounded by MaxNetworks
		r.NetworkIndex = &i
	}
	return r
}

// scanNetworks is HandleScanNetworks (:309-371) and OnFinished (:930-961):
// an SSID longer than 32 bytes is a ConstraintError (and UnknownError the
// last status); an empty one is a scan of every network; a directed scan
// without results is NetworkNotFound; the status is the last networking
// status; success commits the breadcrumb.
func (w *WiFiNetworkCommissioning) scanNetworks(ctx context.Context, req netcommdef.ScanNetworksRequest) (any, error) {
	var ssid []byte
	if req.Ssid != nil && !req.Ssid.Null {
		ssid = req.Ssid.Value
	}
	w.mu.Lock()
	if len(ssid) > maxWiFiSSIDLength {
		w.setStatusLocked(netcommdef.NetworkCommissioningStatusUnknownError)
		w.dataVersion.Bump()
		w.mu.Unlock()
		return nil, netcommStatus(im.StatusConstraintError, "matter: ScanNetworks SSID of %d bytes", len(ssid))
	}
	w.busy = true
	w.mu.Unlock()
	code, text, results := w.driver.ScanNetworks(ctx, ssid)
	status := netcommdef.NetworkCommissioningStatusEnum(code)
	if statusOK(status) && len(ssid) > 0 && len(results) == 0 {
		status = netcommdef.NetworkCommissioningStatusNetworkNotFound
	}
	w.mu.Lock()
	w.busy = false
	w.setStatusLocked(status)
	w.dataVersion.Bump()
	w.mu.Unlock()
	resp := netcommdef.ScanNetworksResponse{NetworkingStatus: status, DebugText: debugText(text)}
	list := make([]netcommdef.WiFiInterfaceScanResultStruct, 0, len(results))
	for _, r := range results {
		e := netcommdef.WiFiInterfaceScanResultStruct{Security: netcommdef.WiFiSecurityBitmap(r.Security), Ssid: bytes.Clone(r.SSID), Bssid: bytes.Clone(r.BSSID), Channel: r.Channel}
		if r.HasBand {
			b := netcommdef.WiFiBandEnum(r.Band)
			e.WiFiBand = &b
		}
		if r.HasRSSI {
			v := r.RSSI
			e.Rssi = &v
		}
		list = append(list, e)
	}
	resp.WiFiScanResults = &list
	if statusOK(status) {
		w.breadcrumb(req.Breadcrumb)
	}
	return resp, nil
}

// validCredentials is the credential length rule of :417-445.
func validCredentials(c []byte) bool {
	switch n := len(c); {
	case n == wifiCredOpen, n == wifiCredWEP64, n >= wifiCredMinWPAPSK && n <= wifiCredMaxWPAPSK:
		return true
	case n == wifiCredWPAPSKHexLen:
		for _, b := range c {
			if (b < '0' || b > '9') && (b < 'a' || b > 'f') && (b < 'A' || b > 'F') {
				return false
			}
		}
		return true
	}
	return false
}

// addOrUpdate is HandleAddOrUpdateWiFiNetwork (:373-451) over the Linux
// driver's AddOrUpdateNetwork (NetworkCommissioningWiFiDriver.cpp:175-199):
// a failsafe is required; an empty or overlong SSID and an invalid
// credential length are ConstraintError; an SSID in the list is updated,
// a new one appended unless the list is full (BoundsExceeded); success
// writes the breadcrumb and changes the list.
func (w *WiFiNetworkCommissioning) addOrUpdate(req netcommdef.AddOrUpdateWiFiNetworkRequest) (any, error) {
	if !w.failSafeArmed() {
		return nil, netcommStatus(im.StatusFailsafeRequired, "matter: AddOrUpdateWiFiNetwork needs an armed failsafe")
	}
	if len(req.Ssid) == 0 || len(req.Ssid) > maxWiFiSSIDLength {
		return nil, netcommStatus(im.StatusConstraintError, "matter: AddOrUpdateWiFiNetwork ssid")
	}
	if !validCredentials(req.Credentials) {
		return nil, netcommStatus(im.StatusConstraintError, "matter: AddOrUpdateWiFiNetwork credentials of %d bytes", len(req.Credentials))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	status := netcommdef.NetworkCommissioningStatusSuccess
	i := w.indexLocked(req.Ssid)
	switch {
	case len(req.Credentials) > maxWiFiKeyLength:
		status = netcommdef.NetworkCommissioningStatusOutOfRange
	case i >= 0:
		w.networks[i].credentials = bytes.Clone(req.Credentials)
	case len(w.networks) >= int(w.maxNetworks):
		status = netcommdef.NetworkCommissioningStatusBoundsExceeded
	default:
		w.networks = append(w.networks, wifiNetwork{ssid: bytes.Clone(req.Ssid), credentials: bytes.Clone(req.Credentials)})
		i = len(w.networks) - 1
	}
	if statusOK(status) {
		w.dataVersion.Bump()
		w.breadcrumb(req.Breadcrumb)
	}
	return configResponse(status, i), nil
}

// remove is HandleRemoveNetwork (:603-632) over the driver's
// RemoveNetwork (:201-210): a failsafe is required; an unknown network
// is NetworkIDNotFound; with the last network gone the last status,
// network id and connect error are cleared.
func (w *WiFiNetworkCommissioning) remove(req netcommdef.RemoveNetworkRequest) (any, error) {
	if !w.failSafeArmed() {
		return nil, netcommStatus(im.StatusFailsafeRequired, "matter: RemoveNetwork needs an armed failsafe")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	i := w.indexLocked(req.NetworkId)
	if i < 0 {
		return configResponse(netcommdef.NetworkCommissioningStatusNetworkIdNotFound, 0), nil
	}
	w.networks = slices.Delete(w.networks, i, i+1)
	w.dataVersion.Bump()
	w.breadcrumb(req.Breadcrumb)
	if len(w.networks) == 0 {
		w.lastNetworkID, w.lastConnectErr, w.lastStatus = nil, nil, nil
	}
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, i), nil
}

// reorder is HandleReorderNetwork (:712-730) over the driver's
// ReorderNetwork (:212-219): an unknown network is NetworkIDNotFound, an
// index past the list OutOfRange. chip asks for no failsafe here.
func (w *WiFiNetworkCommissioning) reorder(req netcommdef.ReorderNetworkRequest) (any, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	i := w.indexLocked(req.NetworkId)
	switch {
	case i < 0:
		return configResponse(netcommdef.NetworkCommissioningStatusNetworkIdNotFound, 0), nil
	case int(req.NetworkIndex) >= len(w.networks):
		return configResponse(netcommdef.NetworkCommissioningStatusOutOfRange, 0), nil
	}
	n := w.networks[i]
	w.networks = slices.Insert(slices.Delete(w.networks, i, i+1), int(req.NetworkIndex), n)
	w.dataVersion.Bump()
	w.breadcrumb(req.Breadcrumb)
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, int(req.NetworkIndex)), nil
}

// connect is HandleConnectNetwork (:634-683, the concurrent path) and
// OnResult (:843-902) over the driver's ConnectNetwork (:221-259): a
// network id over 32 bytes is a ConstraintError; a failsafe is required;
// an unknown network is NetworkIDNotFound; the result is the
// ConnectNetworkResponse, the last networking status and network id,
// the connect error (null on success); success marks the network
// connected and writes the breadcrumb.
func (w *WiFiNetworkCommissioning) connect(ctx context.Context, req netcommdef.ConnectNetworkRequest) (any, error) {
	if len(req.NetworkId) > maxNetworkIDLength {
		return nil, netcommStatus(im.StatusConstraintError, "matter: ConnectNetwork network id of %d bytes", len(req.NetworkId))
	}
	if !w.failSafeArmed() {
		return nil, netcommStatus(im.StatusFailsafeRequired, "matter: ConnectNetwork needs an armed failsafe")
	}
	w.mu.Lock()
	i := w.indexLocked(req.NetworkId)
	var target wifiNetwork
	if i >= 0 {
		target = w.networks[i]
		w.busy = true
	}
	w.mu.Unlock()
	status := netcommdef.NetworkCommissioningStatusNetworkIdNotFound
	var text string
	var errValue int32
	if i >= 0 {
		var code uint8
		code, text, errValue = w.driver.ConnectNetwork(ctx, target.ssid, target.credentials)
		status = netcommdef.NetworkCommissioningStatusEnum(code)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.busy = false
	resp := netcommdef.ConnectNetworkResponse{NetworkingStatus: status, DebugText: debugText(text), ErrorValue: spec.Nullable[int32]{Null: true}}
	if statusOK(status) {
		w.lastConnectErr = nil
		for j := range w.networks {
			w.networks[j].connected = bytes.Equal(w.networks[j].ssid, req.NetworkId)
		}
	} else {
		resp.ErrorValue = spec.Nullable[int32]{Value: errValue}
		v := errValue
		w.lastConnectErr = &v
	}
	w.lastNetworkID = bytes.Clone(req.NetworkId)
	w.setStatusLocked(status)
	w.dataVersion.Bump()
	if statusOK(status) {
		w.breadcrumb(req.Breadcrumb)
	}
	return resp, nil
}

// OnCommissioningComplete keeps the staged networks (CommitConfiguration,
// NetworkCommissioningCluster.cpp:984-990).
func (w *WiFiNetworkCommissioning) OnCommissioningComplete() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.committed = slices.Clone(w.networks)
}

// OnFailSafeExpired restores the committed networks (RevertConfiguration,
// :992-1014); with none left the last status, network id and connect
// error are cleared.
func (w *WiFiNetworkCommissioning) OnFailSafeExpired() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.networks = slices.Clone(w.committed)
	w.busy = false
	if len(w.networks) == 0 {
		w.lastNetworkID, w.lastConnectErr, w.lastStatus = nil, nil, nil
	}
	w.dataVersion.Bump()
}
