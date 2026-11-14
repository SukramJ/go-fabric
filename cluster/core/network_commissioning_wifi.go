// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
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
	// mu guards busy, committed, interfaceEnabled, lastConnectErr, lastNetworkID, lastStatus and networks.
	mu          sync.Mutex
	dataVersion cluster.DataVersionTracker
	// changes reports what moves outside the cluster's own commands — the
	// failsafe revert, the rejoin at Start
	// ([WiFiNetworkCommissioning.OnAttributeChange]).
	changes cluster.AttributeChanges

	driver        contract.WiFiDriver
	failSafe      func() (fabricIndex uint8, armed bool)
	failSafeArmed func() bool
	setBreadcrumb func(uint64)
	maxNetworks   uint8
	scanMax       uint8
	connectMax    uint8
	store         store.Settings
	logger        *slog.Logger
	clk           clock.Clock

	interfaceEnabled bool
	// networks is the staged list, committed the list a CommissioningComplete
	// confirmed, restored when the failsafe expires (CommitConfiguration,
	// RevertConfiguration).
	networks  []wifiNetwork
	committed []wifiNetwork
	// stored is the list StoreNetworks wrote ahead of the fabric's
	// commit, which OnCommissioningComplete then commits in memory without
	// a second write; nil when none is outstanding.
	stored []wifiNetwork
	busy   bool
	// link is the SSID the driver last joined; after a join that failed,
	// the network a [contract.WiFiLinkReporter] driver reports the radio on,
	// nil without one (the cluster cannot tell where the radio is then). A
	// network's connected flag is derived from it, as chip's Linux driver
	// reports the network the link is on (WiFiNetworkIterator::Next,
	// NetworkCommissioningWiFiDriver.cpp:275-310).
	link []byte
	// life is Start's context, the server's lifetime: the rejoin of the
	// committed network runs under it ([WiFiNetworkCommissioning.rejoin]).
	// rejoinTimer is the armed retry, rejoinDelay the backoff of the next
	// one (0 before the first failure), rejoining set while an attempt
	// runs.
	life        context.Context
	rejoinTimer clock.Timer
	rejoinDelay time.Duration
	rejoining   bool
	// expiries counts the fail-safe expiries (OnFailSafeExpired): a
	// ConnectNetwork whose driver call outlives the window that admitted it
	// sees it moved and drops its result.
	expiries uint64

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
	// FailSafe is the node's fail-safe port — armed, and for which fabric
	// ([GeneralCommissioning.FailSafeFabric]); the network commands need
	// the window of their accessing fabric (EnsureFailsafeIsArmed over
	// IsFailSafeArmed(GetAccessingFabricIndex()),
	// NetworkCommissioningCluster.cpp:155-172, :380, :568, :609 at
	// 6170af84; by_design BD-chip-FailSafeOfTheAccessingFabric).
	FailSafe func() (fabricIndex uint8, armed bool)
	// IsFailSafeArmed is the fabric-blind port (armed for any fabric,
	// GeneralCommissioning.FailSafeArmed); consulted only while FailSafe
	// is nil. Both nil reads as disarmed.
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
	// Store keeps the committed networks — SSID and credentials — across
	// restarts, as chip's Linux driver keeps its network in the KVS
	// (CommitConfiguration, Init: src/platform/Linux/
	// NetworkCommissioningWiFiDriver.cpp:135-167, :77-128 at 6170af84);
	// nil keeps them in memory.
	Store store.Settings
	// Logger logs a store failure; nil is slog.Default().
	Logger *slog.Logger
	// Clock is the clock the rejoin of the committed network backs off
	// on; nil is [clock.System].
	Clock clock.Clock
}

// wifiNetworksKey is the setting the committed networks are stored under.
const wifiNetworksKey = "netcomm.wifi.networks"

// storedWiFiNetwork is one committed network as stored: chip's KVS keeps
// the SSID and the credentials (kWiFiSSIDKeyName, kWiFiCredentialsKeyName);
// whether it is connected comes from the link, not the store.
type storedWiFiNetwork struct {
	SSID        []byte `json:"ssid"`
	Credentials []byte `json:"credentials"`
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
// server: the committed networks the Store holds (none without one), not
// connected, the interface enabled, the last networking status, network
// id and connect error null (NetworkCommissioningCluster.cpp:200-214 at
// 6170af84). [WiFiNetworkCommissioning.Start] reconnects.
func NewWiFiNetworkCommissioning(cfg WiFiNetworkCommissioningConfig) (*WiFiNetworkCommissioning, error) {
	if cfg.Driver == nil {
		return nil, errors.New("core: Wi-Fi NetworkCommissioning needs a driver")
	}
	w := &WiFiNetworkCommissioning{
		driver: cfg.Driver, failSafe: cfg.FailSafe, failSafeArmed: cfg.IsFailSafeArmed, setBreadcrumb: cfg.SetBreadcrumb,
		maxNetworks: cfg.MaxNetworks, scanMax: cfg.ScanMaxTimeSeconds, connectMax: cfg.ConnectMaxTimeSeconds,
		store: cfg.Store, logger: cfg.Logger, clk: clock.Or(cfg.Clock), interfaceEnabled: true,
		life: context.Background(),
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
	w.committed = w.loadCommitted(context.Background()) //nolint:contextcheck // the constructor takes no context; it reads the store once
	w.networks = slices.Clone(w.committed)
	w.dataVersion.Bump()
	return w, nil
}

// loadCommitted reads the stored committed networks (Init, :77-128): none
// without a store, a stored value or one that decodes; at most
// MaxNetworks of them.
func (w *WiFiNetworkCommissioning) loadCommitted(ctx context.Context) []wifiNetwork {
	if w.store == nil {
		return nil
	}
	raw, ok, err := w.store.GetSetting(ctx, wifiNetworksKey)
	if err != nil || !ok {
		if err != nil {
			w.log().Warn("netcomm.wifi.load_failed", slog.Any("err", err))
		}
		return nil
	}
	var stored []storedWiFiNetwork
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		w.log().Warn("netcomm.wifi.load_failed", slog.Any("err", err))
		return nil
	}
	var list []wifiNetwork
	for _, n := range stored {
		if len(n.SSID) == 0 || len(n.SSID) > maxWiFiSSIDLength || len(list) == int(w.maxNetworks) {
			continue
		}
		list = append(list, wifiNetwork{ssid: n.SSID, credentials: n.Credentials})
	}
	return list
}

// saveCommittedLocked stores the committed networks (CommitConfiguration,
// :135-167); w.mu is held. A failure is logged.
func (w *WiFiNetworkCommissioning) saveCommittedLocked(ctx context.Context) {
	if err := w.saveLocked(ctx, w.committed); err != nil {
		w.log().Warn("netcomm.wifi.save_failed", slog.Any("err", err))
	}
}

// saveLocked writes list as the stored committed networks; nil without a
// store. w.mu is held.
func (w *WiFiNetworkCommissioning) saveLocked(ctx context.Context, list []wifiNetwork) error {
	if w.store == nil {
		return nil
	}
	stored := make([]storedWiFiNetwork, 0, len(list))
	for _, n := range list {
		stored = append(stored, storedWiFiNetwork{SSID: n.ssid, Credentials: n.credentials})
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("core: WiFiNetworkCommissioning: encode the networks: %w", err)
	}
	if err := w.store.SetSetting(ctx, wifiNetworksKey, string(raw)); err != nil {
		return fmt.Errorf("core: WiFiNetworkCommissioning: store the networks: %w", err)
	}
	return nil
}

// StoreNetworks writes the staged networks as the stored committed list,
// ahead of the fabric's commit: CommissioningComplete runs it inside its
// durable commit ([GeneralCommissioning.SetFailSafeCommit]), before the
// commit of the fabric row, so a store that refuses the list fails
// CommissioningComplete with nothing committed, and the window that
// remains — a crash after this write and before the fabric's — leaves a
// node on its network whose fabric the next start rolls back, not a
// fabric on a node without a stored network (review II, R2-P2-store-01;
// connectedhomeip commits the network after the fabric and ignores the
// result, NetworkCommissioningCluster.cpp:984-990 at 6170af84 — the order
// is this module's own). The list in force is not changed:
// OnCommissioningComplete commits it. restore writes the committed list
// back, for a commit that fails after this write; its own failure is
// logged.
func (w *WiFiNetworkCommissioning) StoreNetworks(ctx context.Context) (restore func(ctx context.Context), err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	staged := slices.Clone(w.networks)
	if err := w.saveLocked(ctx, staged); err != nil {
		return nil, err
	}
	w.stored = staged
	return func(ctx context.Context) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.stored = nil
		w.saveCommittedLocked(ctx)
	}, nil
}

func (w *WiFiNetworkCommissioning) log() *slog.Logger {
	if w.logger != nil {
		return w.logger
	}
	return slog.Default()
}

// Rejoin backoff: the first retry after a failed join of the committed
// network waits wifiRejoinFirstDelay — connectedhomeip's
// CHIP_DEVICE_CONFIG_WIFI_STATION_RECONNECT_INTERVAL, 5000 ms, "the
// interval at which the chip platform will attempt to reconnect to the
// configured WiFi network" (src/include/platform/CHIPDeviceConfig.h:406-414
// at 6170af84); each further one doubles the wait, up to
// wifiRejoinMaxDelay (this module's number).
const (
	wifiRejoinFirstDelay = 5 * time.Second
	wifiRejoinMaxDelay   = 5 * time.Minute
)

// Start reconnects to the first committed network when the node starts.
// chip's Linux driver loads its network at Init (:77-128) and the link,
// which the committed configuration brings back up (CommitConfig, :164),
// reports itself through OnNetworkingStatusChange
// (NetworkCommissioningCluster.cpp:283-307 at 6170af84): the network id
// becomes LastNetworkID and the status LastNetworkingStatus. Here the
// driver's ConnectNetwork is re-issued, bounded by ConnectMaxTimeSeconds,
// and its result recorded the same way, the network marked connected on
// success. Without a committed network Start does nothing; no breadcrumb
// is written.
//
// ctx is the server's lifetime: a join that fails is tried again on the
// Clock with a backoff (wifiRejoinFirstDelay doubling to
// wifiRejoinMaxDelay) until one succeeds or ctx ends, each attempt
// recorded as the first — as is the rejoin a fail-safe expiry or a
// CommissioningComplete starts when the link is on no committed network.
// An attempt waits while a fail-safe is armed or a command runs, and holds
// the driver alone while it runs, the first at Start included: a command
// answers Busy then, and a fail-safe expiry does not release it. chip
// leaves the reconnect to wpa_supplicant, which holds the network it
// saved (CommitConfig, :1118-1135 of
// ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp); this
// module saves none there (BD-chip-WpaSupplicantDriver), so the retry is
// the cluster's (review II, R2-P3-net-01).
func (w *WiFiNetworkCommissioning) Start(ctx context.Context) {
	w.mu.Lock()
	w.life = ctx
	context.AfterFunc(ctx, w.stopRejoin)
	if len(w.committed) == 0 {
		w.mu.Unlock()
		return
	}
	target := w.committed[0]
	w.busy, w.rejoining = true, true
	w.mu.Unlock()
	ok := w.joinCommitted(ctx, target)
	w.mu.Lock()
	w.busy, w.rejoining = false, false
	if !ok {
		w.backoffLocked()
	}
	w.mu.Unlock()
}

// joinCommitted joins target, bounded by ConnectMaxTimeSeconds, and
// records the result as Start does; it reports whether the join
// succeeded. After a failed join the link is where the driver reports the
// radio ([WiFiNetworkCommissioning.linkAfterFailure]).
func (w *WiFiNetworkCommissioning) joinCommitted(ctx context.Context, target wifiNetwork) bool {
	jctx, cancel := context.WithTimeout(ctx, time.Duration(w.connectMax)*time.Second)
	defer cancel()
	code, _, errValue := w.driver.ConnectNetwork(jctx, target.ssid, target.credentials)
	status := netcommdef.NetworkCommissioningStatusEnum(code)
	ok := statusOK(status)
	var link []byte
	if ok {
		link = bytes.Clone(target.ssid)
	} else {
		link = w.linkAfterFailure(ctx)
	}
	w.mu.Lock()
	w.link = link
	if ok {
		w.lastConnectErr = nil
	} else {
		v := errValue
		w.lastConnectErr = &v
	}
	w.markConnectedLocked()
	w.lastNetworkID = bytes.Clone(target.ssid)
	w.setStatusLocked(status)
	w.dataVersion.Bump()
	w.mu.Unlock()
	w.changes.Notify(netcommAttrNetworks, netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue)
	return ok
}

// linkAfterFailure is where the radio is after a join that did not
// succeed: the network a [contract.WiFiLinkReporter] driver reports —
// wifi/wpasupplicant puts the radio back on the network it was on — and
// nil, on no network as far as the cluster can tell, without one or when
// the driver cannot tell (review II, R2-P3-dr-net-01, R2-P3-dr-net-04).
// It runs without w.mu; the driver bounds its own wait.
func (w *WiFiNetworkCommissioning) linkAfterFailure(ctx context.Context) []byte {
	r, ok := w.driver.(contract.WiFiLinkReporter)
	if !ok {
		return nil
	}
	ssid, known := r.CurrentNetwork(context.WithoutCancel(ctx))
	if !known || len(ssid) == 0 {
		return nil
	}
	return bytes.Clone(ssid)
}

// markConnectedLocked derives each network's connected flag from the
// link. w.mu is held.
func (w *WiFiNetworkCommissioning) markConnectedLocked() {
	for j := range w.networks {
		w.networks[j].connected = len(w.link) > 0 && bytes.Equal(w.networks[j].ssid, w.link)
	}
}

// offCommittedLocked reports whether there is a committed network and the
// link is on none of them. w.mu is held.
func (w *WiFiNetworkCommissioning) offCommittedLocked() bool {
	if len(w.committed) == 0 {
		return false
	}
	return len(w.link) == 0 || !slices.ContainsFunc(w.committed, func(n wifiNetwork) bool { return bytes.Equal(n.ssid, w.link) })
}

// rejoinNowLocked arms an attempt at once on the clock, the backoff from
// its beginning, when the link is on no committed network. w.mu is held.
func (w *WiFiNetworkCommissioning) rejoinNowLocked() {
	if !w.offCommittedLocked() || w.life.Err() != nil {
		return
	}
	w.rejoinDelay = 0
	w.armRejoinLocked(0)
}

// backoffLocked arms the next attempt, the wait doubled from
// wifiRejoinFirstDelay up to wifiRejoinMaxDelay. w.mu is held.
func (w *WiFiNetworkCommissioning) backoffLocked() {
	if w.rejoinDelay == 0 {
		w.rejoinDelay = wifiRejoinFirstDelay
	} else {
		w.rejoinDelay = min(2*w.rejoinDelay, wifiRejoinMaxDelay)
	}
	w.armRejoinLocked(w.rejoinDelay)
}

// armRejoinLocked arms an attempt after d on the clock, replacing an
// armed one. w.mu is held.
func (w *WiFiNetworkCommissioning) armRejoinLocked(d time.Duration) {
	if w.life.Err() != nil {
		return
	}
	if w.rejoinTimer != nil {
		w.rejoinTimer.Stop()
	}
	w.rejoinTimer = w.clk.AfterFunc(d, w.rejoin)
}

// stopRejoin disarms the retry once the lifetime ended.
func (w *WiFiNetworkCommissioning) stopRejoin() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.rejoinTimer != nil {
		w.rejoinTimer.Stop()
		w.rejoinTimer = nil
	}
}

// rejoin is one attempt to bring the link back onto the first committed
// network: none once the link is on a committed network or the lifetime
// ended, or another attempt runs; while a fail-safe is armed or a command
// runs, the attempt is armed again after the same wait — the administrator's
// network change decides then, and its fail-safe's expiry starts a rejoin
// of its own. A failed attempt arms the next with the backoff.
func (w *WiFiNetworkCommissioning) rejoin() {
	armed := w.anyFailSafeArmed() // GeneralCommissioning's lock, not under w.mu
	w.mu.Lock()
	w.rejoinTimer = nil
	life := w.life
	switch {
	case life.Err() != nil || !w.offCommittedLocked():
		w.rejoinDelay = 0
		w.mu.Unlock()
		return
	case w.rejoining:
		// The running attempt arms the next one when it fails.
		w.mu.Unlock()
		return
	case armed || w.busy:
		w.armRejoinLocked(max(w.rejoinDelay, wifiRejoinFirstDelay))
		w.mu.Unlock()
		return
	}
	target := w.committed[0]
	w.busy, w.rejoining = true, true
	w.mu.Unlock()
	ok := w.joinCommitted(life, target)
	w.mu.Lock()
	w.busy, w.rejoining = false, false
	if ok {
		w.rejoinDelay = 0
	} else {
		w.backoffLocked()
	}
	w.mu.Unlock()
}

// anyFailSafeArmed reports whether a fail-safe is armed, for any fabric.
func (w *WiFiNetworkCommissioning) anyFailSafeArmed() bool {
	if w.failSafe != nil {
		_, armed := w.failSafe()
		return armed
	}
	return w.failSafeArmed != nil && w.failSafeArmed()
}

var (
	_ contract.ClusterServer                  = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterAttributeLister         = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterDataVersion             = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterAttributeReadPrivilege  = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterCommandLister           = (*WiFiNetworkCommissioning)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*WiFiNetworkCommissioning)(nil)
	_ contract.AttributeChangeNotifier        = (*WiFiNetworkCommissioning)(nil)
)

// OnAttributeChange implements [contract.AttributeChangeNotifier]: the
// failsafe revert and the rejoin at [WiFiNetworkCommissioning.Start]
// change Networks and the last-connect attributes outside a command of
// this cluster (the bridge reports a command's effect on the cluster it
// invoked); chip marks them dirty where they change
// (NetworkCommissioningCluster.cpp ReportNetworksListChanged in
// OnFailSafeTimerExpired :992-1014, the Set… of OnNetworkingStatusChange
// :283-307). Found by TC-CNET-4.9, whose subscription cache missed the
// Networks an ArmFailSafe(0) restored.
func (w *WiFiNetworkCommissioning) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return w.changes.OnAttributeChange(cb)
}

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
		return refusedWrite{sentinel: fmt.Errorf("core: NetworkCommissioning attribute 0x%04X is read-only", attrID), status: err}
	}
	v, ok := value.(bool)
	if !ok {
		return fmt.Errorf("core: InterfaceEnabled write expected bool, got %T", value)
	}
	w.mu.Lock()
	w.interfaceEnabled = v
	w.dataVersion.Bump()
	w.mu.Unlock()
	return nil
}

// SetAttributes implements [spec.AttributeSetter] for InterfaceEnabled,
// as [NetworkCommissioning.SetAttributes]: the node restores a
// controller's write of it through this after a restart.
func (w *WiFiNetworkCommissioning) SetAttributes(values map[uint32]any) error {
	enabled, set, err := netcommInterfaceEnabledOf(wifiNetcommInst, values)
	if err != nil || !set {
		return err
	}
	w.mu.Lock()
	w.interfaceEnabled = enabled
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
		return nil, netcommStatus(im.StatusBusy, "core: NetworkCommissioning is busy")
	}
	w.mu.Unlock()
	switch cmdID {
	case netcommdef.CmdScanNetworks:
		req, ok := fields.(netcommdef.ScanNetworksRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: ScanNetworks fields %T", fields)
		}
		return w.scanNetworks(ctx, req)
	case netcommdef.CmdAddOrUpdateWiFiNetwork:
		req, ok := fields.(netcommdef.AddOrUpdateWiFiNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: AddOrUpdateWiFiNetwork fields %T", fields)
		}
		return w.addOrUpdate(ctx, req)
	case netcommdef.CmdRemoveNetwork:
		req, ok := fields.(netcommdef.RemoveNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: RemoveNetwork fields %T", fields)
		}
		return w.remove(ctx, req)
	case netcommdef.CmdConnectNetwork:
		req, ok := fields.(netcommdef.ConnectNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: ConnectNetwork fields %T", fields)
		}
		return w.connect(ctx, req)
	case netcommdef.CmdReorderNetwork:
		req, ok := fields.(netcommdef.ReorderNetworkRequest)
		if !ok {
			return nil, netcommStatus(im.StatusInvalidCommand, "core: ReorderNetwork fields %T", fields)
		}
		return w.reorder(req)
	}
	return nil, im.UnsupportedCommandf("core: NetworkCommissioning command 0x%02X not supported (Wi-Fi)", cmdID)
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

// maxNetworksInScanResponse bounds the results of a ScanNetworksResponse:
// connectedhomeip's kMaxNetworksInScanResponse
// (src/app/clusters/network-commissioning/constants.h:26-28 at 6170af84,
// "each item will cost ~60 bytes in TLV, thus 15 is a safe upper bound of
// scan results"), at which WifiScanResponseToTLV::EncodeTo stops
// (WifiScanResponse.cpp:65-69), so the response fits its message whatever
// the number of access points in range (review II, R2-P3-net-04).
const maxNetworksInScanResponse = 15

// scanNetworks is HandleScanNetworks (:309-371) and OnFinished (:930-961):
// an SSID longer than 32 bytes is a ConstraintError (and UnknownError the
// last status); an empty one is a scan of every network; a directed scan
// without results is NetworkNotFound; the status is the last networking
// status; success commits the breadcrumb. The response carries at most
// maxNetworksInScanResponse results: chip's Wi-Fi response keeps the
// first ones in the driver's order, here the strongest are kept, those
// without a known RSSI last — as chip's Thread response keeps its
// strongest (ThreadScanResponse.cpp:49-88).
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
		return nil, netcommStatus(im.StatusConstraintError, "core: ScanNetworks SSID of %d bytes", len(ssid))
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
	if len(results) > maxNetworksInScanResponse {
		results = slices.Clone(results)
		slices.SortStableFunc(results, func(a, b contract.WiFiScanResult) int {
			switch {
			case a.HasRSSI != b.HasRSSI:
				if a.HasRSSI {
					return -1
				}
				return 1
			case !a.HasRSSI:
				return 0
			}
			return int(b.RSSI) - int(a.RSSI)
		})
		results = results[:maxNetworksInScanResponse]
	}
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

// armedFor is connectedhomeip's EnsureFailsafeIsArmed(failSafeContext,
// handler.GetAccessingFabricIndex()) (NetworkCommissioningCluster.cpp:155-172,
// :380, :568, :609 at 6170af84): the window must be armed, and armed for
// the accessing fabric (FailSafeContext.h:76-79). A fabric-blind port
// ([WiFiNetworkCommissioningConfig.IsFailSafeArmed]) answers for any
// fabric, as before.
func (w *WiFiNetworkCommissioning) armedFor(ctx context.Context) bool {
	if w.failSafe != nil {
		_, accessing := im.FabricFilterFromContext(ctx)
		return FailSafeArmedFor(w.failSafe, accessing)
	}
	return w.failSafeArmed != nil && w.failSafeArmed()
}

// addOrUpdate is HandleAddOrUpdateWiFiNetwork (:373-451) over the Linux
// driver's AddOrUpdateNetwork (NetworkCommissioningWiFiDriver.cpp:175-199):
// a failsafe is required; an empty or overlong SSID and an invalid
// credential length are ConstraintError; an SSID in the list is updated,
// a new one appended unless the list is full (BoundsExceeded); success
// writes the breadcrumb and changes the list.
func (w *WiFiNetworkCommissioning) addOrUpdate(ctx context.Context, req netcommdef.AddOrUpdateWiFiNetworkRequest) (any, error) {
	if !w.armedFor(ctx) {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: AddOrUpdateWiFiNetwork needs an armed failsafe")
	}
	if len(req.Ssid) == 0 || len(req.Ssid) > maxWiFiSSIDLength {
		return nil, netcommStatus(im.StatusConstraintError, "core: AddOrUpdateWiFiNetwork ssid")
	}
	if !validCredentials(req.Credentials) {
		return nil, netcommStatus(im.StatusConstraintError, "core: AddOrUpdateWiFiNetwork credentials of %d bytes", len(req.Credentials))
	}
	w.mu.Lock()
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
	var crumb *uint64
	if statusOK(status) {
		w.dataVersion.Bump()
		crumb = req.Breadcrumb
	}
	w.mu.Unlock()
	// The Breadcrumb's write reaches GeneralCommissioning's listeners and
	// the host's hook; it runs without this cluster's lock (review II,
	// R2-P1-core-07), as scanNetworks' does.
	w.breadcrumb(crumb)
	return configResponse(status, i), nil
}

// remove is HandleRemoveNetwork (:603-632) over the driver's
// RemoveNetwork (:201-210): a failsafe is required; an unknown network
// is NetworkIDNotFound; with the last network gone the last status,
// network id and connect error are cleared.
func (w *WiFiNetworkCommissioning) remove(ctx context.Context, req netcommdef.RemoveNetworkRequest) (any, error) {
	if !w.armedFor(ctx) {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: RemoveNetwork needs an armed failsafe")
	}
	w.mu.Lock()
	i := w.indexLocked(req.NetworkId)
	if i < 0 {
		w.mu.Unlock()
		return configResponse(netcommdef.NetworkCommissioningStatusNetworkIdNotFound, 0), nil
	}
	w.networks = slices.Delete(w.networks, i, i+1)
	w.dataVersion.Bump()
	if len(w.networks) == 0 {
		w.lastNetworkID, w.lastConnectErr, w.lastStatus = nil, nil, nil
	}
	w.mu.Unlock()
	w.breadcrumb(req.Breadcrumb) // without the lock (R2-P1-core-07)
	return configResponse(netcommdef.NetworkCommissioningStatusSuccess, i), nil
}

// reorder is HandleReorderNetwork (:712-730) over the driver's
// ReorderNetwork (:212-219): an unknown network is NetworkIDNotFound, an
// index past the list OutOfRange. chip asks for no failsafe here.
func (w *WiFiNetworkCommissioning) reorder(req netcommdef.ReorderNetworkRequest) (any, error) {
	w.mu.Lock()
	i := w.indexLocked(req.NetworkId)
	switch {
	case i < 0:
		w.mu.Unlock()
		return configResponse(netcommdef.NetworkCommissioningStatusNetworkIdNotFound, 0), nil
	case int(req.NetworkIndex) >= len(w.networks):
		w.mu.Unlock()
		return configResponse(netcommdef.NetworkCommissioningStatusOutOfRange, 0), nil
	}
	n := w.networks[i]
	w.networks = slices.Insert(slices.Delete(w.networks, i, i+1), int(req.NetworkIndex), n)
	w.dataVersion.Bump()
	w.mu.Unlock()
	w.breadcrumb(req.Breadcrumb) // without the lock (R2-P1-core-07)
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
		return nil, netcommStatus(im.StatusConstraintError, "core: ConnectNetwork network id of %d bytes", len(req.NetworkId))
	}
	if !w.armedFor(ctx) {
		return nil, netcommStatus(im.StatusFailsafeRequired, "core: ConnectNetwork needs an armed failsafe")
	}
	w.mu.Lock()
	window := w.expiries
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
	var link []byte
	if i >= 0 {
		var code uint8
		code, text, errValue = w.driver.ConnectNetwork(ctx, target.ssid, target.credentials)
		status = netcommdef.NetworkCommissioningStatusEnum(code)
		if !statusOK(status) {
			link = w.linkAfterFailure(ctx)
		}
	}
	w.mu.Lock()
	resp := netcommdef.ConnectNetworkResponse{NetworkingStatus: status, DebugText: debugText(text), ErrorValue: spec.Nullable[int32]{Null: true}}
	if w.expiries != window {
		// The fail-safe that admitted the command expired while the driver
		// ran, and OnFailSafeExpired reverted the networks: the result
		// touches neither the reverted state nor the Breadcrumb.
		// connectedhomeip releases the pending command on the expiry
		// (NetworkCommissioningCluster.cpp:1002 at 6170af84) and its
		// OnResult returns on the released handle (:850-857) (review II,
		// R2-P1-core-06).
		w.mu.Unlock()
		if !statusOK(status) {
			resp.ErrorValue = spec.Nullable[int32]{Value: errValue}
		}
		return resp, nil
	}
	w.busy = false
	if statusOK(status) {
		w.lastConnectErr = nil
		w.link = bytes.Clone(req.NetworkId)
	} else {
		resp.ErrorValue = spec.Nullable[int32]{Value: errValue}
		v := errValue
		w.lastConnectErr = &v
		if i >= 0 {
			// The driver's join failed: the radio is where the driver
			// reports it, on no network when it reports none or cannot tell
			// (review II, R2-P3-net-02, R2-P3-dr-net-04).
			w.link = link
		}
	}
	w.markConnectedLocked()
	w.lastNetworkID = bytes.Clone(req.NetworkId)
	w.setStatusLocked(status)
	w.dataVersion.Bump()
	w.mu.Unlock()
	if statusOK(status) {
		w.breadcrumb(req.Breadcrumb) // without the lock (R2-P1-core-07)
	}
	return resp, nil
}

// OnCommissioningComplete keeps the staged networks and stores them
// (CommitConfiguration, NetworkCommissioningCluster.cpp:984-990; the Linux
// driver's KVS put, NetworkCommissioningWiFiDriver.cpp:135-167). ctx is the
// CommissioningComplete request's context; the store write runs under it.
//
// When StoreNetworks wrote the list ahead of the fabric's commit, the
// stored list is committed in memory and not written again.
//
// A link on none of the committed networks starts their rejoin
// ([WiFiNetworkCommissioning.Start]).
func (w *WiFiNetworkCommissioning) OnCommissioningComplete(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	defer w.rejoinNowLocked()
	if w.stored != nil {
		w.committed, w.stored = w.stored, nil
		return
	}
	w.committed = slices.Clone(w.networks)
	w.saveCommittedLocked(ctx)
}

// OnFailSafeExpired restores the committed networks (RevertConfiguration,
// :992-1014), each connected when the link is on it; with none left the
// last status, network id and connect error are cleared. The change is
// reported ([WiFiNetworkCommissioning.OnAttributeChange]).
//
// The radio is restored as well: when the link is on none of the
// committed networks — a join under the fail-safe failed, or joined a
// network the revert removed — the first committed network is rejoined
// at once and, while that fails, with the backoff of
// [WiFiNetworkCommissioning.Start]. connectedhomeip's Linux driver
// restores its record only (RevertConfiguration,
// src/platform/Linux/NetworkCommissioningWiFiDriver.cpp:169-173 at
// 6170af84); the Thread server here restores its dataset
// (network_commissioning_thread.go) (review II, R2-P3-net-02).
func (w *WiFiNetworkCommissioning) OnFailSafeExpired() {
	w.mu.Lock()
	w.expiries++
	w.stored = nil
	w.networks = slices.Clone(w.committed)
	w.markConnectedLocked()
	if !w.rejoining {
		// A command's join the expiry abandons (its result is dropped,
		// connect) frees the driver's turn; a rejoin attempt still holds the
		// driver and keeps it until it ends (review II, R2-P3-dr-net-05).
		w.busy = false
	}
	if len(w.networks) == 0 {
		w.lastNetworkID, w.lastConnectErr, w.lastStatus = nil, nil, nil
	}
	w.dataVersion.Bump()
	w.rejoinNowLocked()
	w.mu.Unlock()
	w.changes.Notify(netcommAttrNetworks, netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue)
}
