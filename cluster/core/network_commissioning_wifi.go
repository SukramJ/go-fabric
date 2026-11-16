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
	// switchMu serialises the changes of InterfaceEnabled: the driver's
	// switch runs outside mu, and the value it switched to is set before
	// the next change asks the driver ([WiFiNetworkCommissioning.switchInterface]).
	switchMu sync.Mutex
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
	// loadErr is the store's error reading the stored networks at
	// construction; while set, no list is stored (saveLocked).
	loadErr error
	logger  *slog.Logger
	clk     clock.Clock

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
	// busy is the driver's turn: a command's scan or connect, claimed in
	// the same hold as MatterInvoke's check and released by the command
	// when it returns — also after a fail-safe expiry abandoned it — or a
	// rejoin attempt (rejoining). busyGen names the holder (review II,
	// N3-net-1, N3-finnet-2).
	busy    bool
	busyGen uint64
	// cmdCancel cancels the driver call of the ConnectNetwork that runs;
	// the fail-safe's expiry abandons it with this (review II,
	// R2-P6-conc-1021). nil while none runs.
	cmdCancel context.CancelFunc
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
	// runs; rejoinCancel cancels the running attempt and rejoinDone closes
	// once it ended (nil while none runs).
	life         context.Context
	rejoinTimer  clock.Timer
	rejoinDelay  time.Duration
	rejoining    bool
	rejoinCancel context.CancelFunc
	rejoinDone   chan struct{}
	// disconnectOnFailedRejoin: the fail-safe's revert left the radio on
	// a network it removed; a failed rejoin then takes the radio off it
	// (contract.WiFiDisconnector).
	disconnectOnFailedRejoin bool
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
	w.committed, w.loadErr = w.loadCommitted(context.Background()) //nolint:contextcheck // the constructor takes no context; it reads the store once
	w.networks = slices.Clone(w.committed)
	w.dataVersion.Bump()
	return w, nil
}

// loadCommitted reads the stored committed networks (Init, :77-128): none
// without a store, a stored value or one that decodes; at most
// MaxNetworks of them. A store that cannot read the setting — a sealed
// value that does not open (store.ErrUnseal), a store without the sealer
// (store.ErrNoSealer), an I/O error — is its error: the server then
// starts with no network in force and refuses to store a list
// ([WiFiNetworkCommissioning.StoreNetworks]), so the stored value is
// never overwritten with one built over a list it could not read (review
// II, seal F5). That refusal fails a commissioning closed only where
// StoreNetworks runs inside the fail-safe's durable commit
// ([GeneralCommissioning.SetFailSafeCommit], as the node wires it):
// without that wiring OnCommissioningComplete's own save refuses the same
// way, but its failure is only logged and the commissioning completes
// with the networks in force in memory alone (review II, DR5b-F-04).
func (w *WiFiNetworkCommissioning) loadCommitted(ctx context.Context) ([]wifiNetwork, error) {
	if w.store == nil {
		return nil, nil
	}
	raw, ok, err := w.store.GetSetting(ctx, wifiNetworksKey)
	if err != nil {
		w.log().Error("netcomm.wifi.load_failed", slog.Any("err", err))
		return nil, fmt.Errorf("core: WiFiNetworkCommissioning: read the stored networks: %w", err)
	}
	if !ok {
		return nil, nil
	}
	var stored []storedWiFiNetwork
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		w.log().Warn("netcomm.wifi.load_failed", slog.Any("err", err))
		return nil, nil
	}
	var list []wifiNetwork
	for _, n := range stored {
		if len(n.SSID) == 0 || len(n.SSID) > maxWiFiSSIDLength || len(list) == int(w.maxNetworks) {
			continue
		}
		list = append(list, wifiNetwork{ssid: n.SSID, credentials: n.Credentials})
	}
	return list, nil
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
	if w.loadErr != nil {
		// The stored list could not be read: writing would replace it
		// with one built without it (seal F5).
		return fmt.Errorf("core: WiFiNetworkCommissioning: the stored networks could not be read, not overwriting them: %w", w.loadErr)
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

// Start reconnects to the first committed network when the node starts,
// unless InterfaceEnabled is false: while it is, no rejoin runs.
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
	if watcher, ok := w.driver.(contract.WiFiLinkWatcher); ok {
		// The radio's own link changes, for the node's lifetime
		// (R2-P5-port-09); a loss starts no rejoin here — the driver's
		// radio rejoins by itself.
		go watcher.WatchLink(ctx, w.linkChanged)
	}
	if len(w.committed) == 0 || !w.interfaceEnabled {
		w.mu.Unlock()
		return
	}
	target := w.committed[0]
	attempt := w.beginAttemptLocked(ctx)
	w.mu.Unlock()
	w.runAttempt(attempt, target)
}

// beginAttemptLocked starts a rejoin attempt under parent: the driver is
// the attempt's (busy, rejoining) until [WiFiNetworkCommissioning.runAttempt]
// ends it, and a command cancels it through rejoinCancel. w.mu is held.
func (w *WiFiNetworkCommissioning) beginAttemptLocked(parent context.Context) context.Context {
	attempt, cancel := context.WithCancel(parent)
	w.busy, w.rejoining = true, true
	w.busyGen++
	w.rejoinCancel, w.rejoinDone = cancel, make(chan struct{})
	return attempt
}

// runAttempt joins target as the attempt begun by beginAttemptLocked and
// ends it: a join that succeeded resets the backoff, one that failed arms
// the next attempt with it, and one a command cancelled arms the next
// after the backoff's current wait, at least wifiRejoinFirstDelay, so the
// command has its turn (review II, R2-P3-dr-net-06).
func (w *WiFiNetworkCommissioning) runAttempt(attempt context.Context, target wifiNetwork) {
	ok := w.joinCommitted(attempt, target)
	w.mu.Lock()
	defer w.mu.Unlock()
	cancelled := attempt.Err() != nil
	w.rejoinCancel()
	close(w.rejoinDone)
	w.rejoinCancel, w.rejoinDone = nil, nil
	w.busy, w.rejoining = false, false
	switch {
	case ok:
		w.rejoinDelay = 0
		w.disconnectOnFailedRejoin = false
	case cancelled:
		w.armRejoinLocked(max(w.rejoinDelay, wifiRejoinFirstDelay))
	default:
		if w.disconnectOnFailedRejoin && !w.onCommittedLocked() {
			// The fail-safe's revert left the radio on a network it
			// removed, and the committed one could not be rejoined: the
			// radio leaves the reverted network (R2-P5-port-10).
			w.disconnectOnFailedRejoin = false
			w.armDisconnectLocked()
		}
		w.backoffLocked()
	}
}

// onCommittedLocked reports whether the link is on a committed network.
// w.mu is held.
func (w *WiFiNetworkCommissioning) onCommittedLocked() bool {
	return len(w.link) > 0 && slices.ContainsFunc(w.committed, func(n wifiNetwork) bool { return bytes.Equal(n.ssid, w.link) })
}

// linkChanged is a [contract.WiFiLinkWatcher] report: the link, the
// connected flags and the last networking status, network id and connect
// error follow the radio, and the change is reported. It starts no rejoin
// (R2-P5-port-09).
func (w *WiFiNetworkCommissioning) linkChanged(c contract.WiFiLinkChange) {
	w.mu.Lock()
	switch {
	case c.Connected:
		w.link = bytes.Clone(c.SSID)
	case len(c.SSID) == 0 || bytes.Equal(c.SSID, w.link):
		w.link = nil
	}
	w.markConnectedLocked()
	if len(c.SSID) > 0 {
		w.lastNetworkID = bytes.Clone(c.SSID)
	}
	if c.ErrorValue != nil {
		v := *c.ErrorValue
		w.lastConnectErr = &v
	} else {
		w.lastConnectErr = nil
	}
	w.setStatusLocked(netcommdef.NetworkCommissioningStatusEnum(c.Status))
	w.dataVersion.Bump()
	w.mu.Unlock()
	w.changes.Notify(netcommAttrNetworks, netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue)
}

// armDisconnectLocked takes the radio off its network on the clock, with a
// [contract.WiFiDisconnector] driver; nothing without one or once the
// lifetime ended. w.mu is held.
func (w *WiFiNetworkCommissioning) armDisconnectLocked() {
	if _, ok := w.driver.(contract.WiFiDisconnector); !ok || w.life.Err() != nil {
		return
	}
	w.clk.AfterFunc(0, w.disconnect)
}

// disconnect calls the driver's DisconnectNetwork, bounded by
// ConnectMaxTimeSeconds, and records the radio as on no network once it
// left.
func (w *WiFiNetworkCommissioning) disconnect() {
	d, ok := w.driver.(contract.WiFiDisconnector)
	if !ok {
		return
	}
	w.mu.Lock()
	life := w.life
	w.mu.Unlock()
	if life.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(life, time.Duration(w.connectMax)*time.Second)
	defer cancel()
	if err := d.DisconnectNetwork(ctx); err != nil {
		w.log().Warn("matter.wifi.disconnect", slog.Any("err", err))
		return
	}
	w.mu.Lock()
	w.link = nil
	w.markConnectedLocked()
	w.dataVersion.Bump()
	w.mu.Unlock()
	w.changes.Notify(netcommAttrNetworks)
}

// joinCommitted joins target, bounded by ConnectMaxTimeSeconds, and
// records the result as Start does; it reports whether the join
// succeeded. After a failed join the link is where the driver reports the
// radio ([WiFiNetworkCommissioning.linkAfterFailure]). A join whose ctx
// ended — a command cancelled the attempt, or the lifetime ended —
// records the link only: nothing failed on the air.
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
	w.markConnectedLocked()
	w.dataVersion.Bump()
	if !ok && ctx.Err() != nil {
		w.mu.Unlock()
		w.changes.Notify(netcommAttrNetworks)
		return false
	}
	if ok {
		w.lastConnectErr = nil
	} else {
		v := errValue
		w.lastConnectErr = &v
	}
	w.lastNetworkID = bytes.Clone(target.ssid)
	w.setStatusLocked(status)
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
// armed one; none once the lifetime ended or while the interface is
// disabled. w.mu is held.
func (w *WiFiNetworkCommissioning) armRejoinLocked(d time.Duration) {
	if w.life.Err() != nil || !w.interfaceEnabled {
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
	w.disarmRejoinLocked()
}

// disarmRejoinLocked stops the armed retry. w.mu is held.
func (w *WiFiNetworkCommissioning) disarmRejoinLocked() {
	if w.rejoinTimer != nil {
		w.rejoinTimer.Stop()
		w.rejoinTimer = nil
	}
}

// setInterfaceEnabledLocked sets InterfaceEnabled. The rejoin of the
// committed network runs only while it is true: false disarms the armed
// attempt and cancels a running one, true again starts the rejoin at once
// when the link is on no committed network (review II, R2-P3-dr-net-07).
// Only a change advances the DataVersion, as the Ethernet server's write.
// Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:1456-1457
// and :1476-1480 at db021da0 (#noteIfChanged returns for an equal value;
// advanceVersion runs only when the commit changed something). w.mu is held.
func (w *WiFiNetworkCommissioning) setInterfaceEnabledLocked(v bool) (changed bool) {
	was := w.interfaceEnabled
	w.interfaceEnabled = v
	if was != v {
		w.dataVersion.Bump()
	}
	switch {
	case was && !v:
		w.disarmRejoinLocked()
		w.rejoinDelay = 0
		if w.rejoinCancel != nil {
			w.rejoinCancel()
		}
	case !was && v:
		w.rejoinNowLocked()
	}
	return was != v
}

// rejoin is one attempt to bring the link back onto the first committed
// network: none once the link is on a committed network, the lifetime
// ended or the interface is disabled, or another attempt runs; while a fail-safe is armed or a command
// runs, the attempt is armed again after the same wait — the administrator's
// network change decides then, and its fail-safe's expiry starts a rejoin
// of its own. A failed attempt arms the next with the backoff.
func (w *WiFiNetworkCommissioning) rejoin() {
	armed := w.anyFailSafeArmed() // GeneralCommissioning's lock, not under w.mu
	w.mu.Lock()
	w.rejoinTimer = nil
	life := w.life
	switch {
	case life.Err() != nil || !w.offCommittedLocked() || !w.interfaceEnabled:
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
	attempt := w.beginAttemptLocked(life)
	w.mu.Unlock()
	w.runAttempt(attempt, target)
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

// MatterWrite handles InterfaceEnabled (WriteAttribute, :1115-1136):
// [WiFiNetworkCommissioning.switchInterface].
func (w *WiFiNetworkCommissioning) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if attrID != netcommAttrInterfaceEnabled {
		_, err := wifiNetcommInst.ValidateWrite(attrID, value, nil)
		return refusedWrite{sentinel: fmt.Errorf("core: NetworkCommissioning attribute 0x%04X is read-only", attrID), status: err}
	}
	v, ok := value.(bool)
	if !ok {
		return fmt.Errorf("core: InterfaceEnabled write expected bool, got %T", value)
	}
	_, err := w.switchInterface(ctx, v)
	return err
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// write of [WiFiNetworkCommissioning.MatterWrite], changed when it moved
// InterfaceEnabled — the comparison that advances the server's own
// version ([WiFiNetworkCommissioning.switchInterface]).
func (w *WiFiNetworkCommissioning) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	if attrID != netcommAttrInterfaceEnabled {
		return false, w.MatterWrite(ctx, attrID, value)
	}
	v, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("core: InterfaceEnabled write expected bool, got %T", value)
	}
	return w.switchInterface(ctx, v)
}

// SetAttributes implements [spec.AttributeSetter] for InterfaceEnabled,
// as [NetworkCommissioning.SetAttributes]: the node restores a
// controller's write of it through this after a restart. The value is
// set as a write's is ([WiFiNetworkCommissioning.switchInterface]): a
// stored false a driver without a [contract.WiFiInterfaceSwitcher] cannot
// carry out — one an earlier version took without switching anything —
// leaves InterfaceEnabled true, the interface's state, and is returned as
// the error.
func (w *WiFiNetworkCommissioning) SetAttributes(values map[uint32]any) error {
	enabled, set, err := netcommInterfaceEnabledOf(wifiNetcommInst, values)
	if err != nil || !set {
		return err
	}
	// SetAttributes takes no context; the driver bounds its own wait
	// (contract.WiFiInterfaceSwitcher).
	_, err = w.switchInterface(context.Background(), enabled) //nolint:contextcheck // see above
	return err
}

// switchInterface sets InterfaceEnabled to v through the driver's
// [contract.WiFiInterfaceSwitcher]: a value already held reaches the
// driver no more, a change switches the interface first and is set only
// once the driver switched it. A driver without the switch, or one whose
// error wraps errors.ErrUnsupported, answers INVALID_ACTION and leaves
// the value; any other error of the driver answers with that error.
// Mirrors connectedhomeip
// src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp:1120-1132
// at 6170af84 (WriteAttribute: SetInterfaceEnabled is the driver's
// SetEnabled, NetworkCommissioningCluster.h:134;
// CHIP_ERROR_UNSUPPORTED_CHIP_FEATURE answers InvalidAction per spec
// 11.9.6.5, any other error the write's status) and
// src/include/platform/NetworkCommissioning.h:225-234 (a driver without
// the feature: SetEnabled unsupported, GetEnabled true). Unlike chip, a
// write of the value held does not call the driver, and a write of true
// to a driver without the switch succeeds: the interface is on.
func (w *WiFiNetworkCommissioning) switchInterface(ctx context.Context, v bool) (changed bool, err error) {
	w.switchMu.Lock()
	defer w.switchMu.Unlock()
	w.mu.Lock()
	held := w.interfaceEnabled
	w.mu.Unlock()
	if held == v {
		return false, nil
	}
	err = errors.ErrUnsupported
	if sw, ok := w.driver.(contract.WiFiInterfaceSwitcher); ok {
		err = sw.SetInterfaceEnabled(ctx, v)
	}
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		return false, netcommStatus(im.StatusInvalidAction, "core: InterfaceEnabled %t: the Wi-Fi driver cannot switch its interface: %v", v, err)
	case err != nil:
		return false, fmt.Errorf("core: InterfaceEnabled %t: the Wi-Fi driver did not switch its interface: %w", v, err)
	}
	w.mu.Lock()
	changed = w.setInterfaceEnabledLocked(v)
	w.mu.Unlock()
	return changed, nil
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
// :1138-1194): Busy while a command's scan or connect runs — the command
// claims the driver in the hold of this check (review II, N3-net-1). A rejoin
// attempt of the committed network that runs is cancelled instead and
// the command waits for it to end, then takes its turn: neither
// reference's commissioner retries a failed command at the network step —
// matter.js's flow terminates commissioning on a StatusResponseError
// (packages/protocol/src/peer/ControllerCommissioningFlow.ts:437-462 at
// db021da0), connectedhomeip completes the AddOrUpdateWiFiNetwork stage
// with the failure (OnBasicFailure, src/controller/CHIPDeviceController.cpp:2157-2162,
// :4287 at 6170af84) and AutoCommissioner goes to kCleanup on any error
// (src/controller/AutoCommissioner.cpp:465-468) — so a Busy from the
// node's own retry would fail a commissioning (review II, R2-P3-dr-net-06).
// A command whose ctx ends while it waits is answered Busy.
func (w *WiFiNetworkCommissioning) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	w.mu.Lock()
	for w.busy {
		if !w.rejoining {
			w.mu.Unlock()
			return nil, netcommStatus(im.StatusBusy, "core: NetworkCommissioning is busy")
		}
		w.rejoinCancel()
		done := w.rejoinDone
		w.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, netcommStatus(im.StatusBusy, "core: NetworkCommissioning is busy")
		}
		w.mu.Lock()
	}
	if cmdID == netcommdef.CmdScanNetworks || cmdID == netcommdef.CmdConnectNetwork {
		// The driver's turn is claimed in the hold that found it free
		// (review II, N3-net-1).
		w.busy = true
		w.busyGen++
		defer w.releaseCommand(w.busyGen, w.expiries)
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

// releaseCommand ends the command's turn gen. A command a fail-safe expiry
// overtook (the count moved from window) held the driver past the expiry,
// so the rejoin the expiry armed waited for it: it starts now (review II,
// R2-P6-conc-1021, N3-finnet-2).
func (w *WiFiNetworkCommissioning) releaseCommand(gen, window uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.busyGen != gen {
		return
	}
	w.busy = false
	if w.expiries != window {
		w.rejoinNowLocked()
	}
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
	scanMax := w.scanMax
	w.mu.Unlock()
	// The driver's scan is bounded by the ScanMaxTimeSeconds the server
	// advertises (review II, R2-P6-bound-1012).
	sctx, cancel := context.WithTimeout(ctx, time.Duration(scanMax)*time.Second)
	code, text, results := w.driver.ScanNetworks(sctx, ssid)
	cancel()
	status := netcommdef.NetworkCommissioningStatusEnum(code)
	if statusOK(status) && len(ssid) > 0 && len(results) == 0 {
		status = netcommdef.NetworkCommissioningStatusNetworkNotFound
	}
	w.mu.Lock()
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
	// The driver's join is bounded by the ConnectMaxTimeSeconds the server
	// advertises (review II, R2-P6-bound-1012), and the fail-safe's expiry
	// cancels it (cmdCancel).
	join, cancel := context.WithTimeout(ctx, time.Duration(w.connectMax)*time.Second)
	defer cancel()
	if i >= 0 {
		target = w.networks[i]
		w.cmdCancel = cancel
	}
	w.mu.Unlock()
	status := netcommdef.NetworkCommissioningStatusNetworkIdNotFound
	var text string
	var errValue int32
	var link []byte
	if i >= 0 {
		var code uint8
		code, text, errValue = w.driver.ConnectNetwork(join, target.ssid, target.credentials)
		status = netcommdef.NetworkCommissioningStatusEnum(code)
		if !statusOK(status) {
			link = w.linkAfterFailure(ctx)
		}
	}
	w.mu.Lock()
	w.cmdCancel = nil
	resp := netcommdef.ConnectNetworkResponse{NetworkingStatus: status, DebugText: debugText(text), ErrorValue: spec.Nullable[int32]{Null: true}}
	if w.expiries != window {
		// The fail-safe that admitted the command expired while the driver
		// ran, and OnFailSafeExpired reverted the networks: the result
		// touches neither the reverted state nor the Breadcrumb, and the
		// command is answered with nothing ([im.NoResponse]; review II,
		// R2-P1-core-06, R2-P6-fsm-1001, F-db021da0-10).
		// Follows connectedhomeip
		// src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp
		// at 6170af84: the fail-safe expiry reverts the driver and releases
		// the pending command handle (:1001-1002), and a driver result that
		// arrives afterwards returns before it writes LastNetworkID,
		// LastNetworkingStatus, LastConnectErrorValue or the Breadcrumb and
		// before it adds a ConnectNetworkResponse (:845-857).
		w.mu.Unlock()
		return im.NoResponse{}, nil
	}
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
// (network_commissioning_thread.go) (review II, R2-P3-net-02). With a
// [contract.WiFiDisconnector] driver the radio leaves the network it is
// on when no committed network is left, and when it is on a network the
// revert removed and the rejoin of the committed network failed
// (R2-P5-port-10).
func (w *WiFiNetworkCommissioning) OnFailSafeExpired() {
	w.mu.Lock()
	w.expiries++
	w.stored = nil
	w.networks = slices.Clone(w.committed)
	w.markConnectedLocked()
	switch {
	case w.cmdCancel != nil:
		// The ConnectNetwork this window admitted is abandoned: its join is
		// cancelled, its result dropped (connect), and it keeps the driver's
		// turn until it returned; the rejoin armed below waits for it, so
		// two joins never run on the driver at once (review II,
		// R2-P6-conc-1021, N3-finnet-2).
		w.cmdCancel()
	case !w.rejoining:
		// A scan's turn is freed for the rejoin, as before; a rejoin
		// attempt keeps the driver until it ends (R2-P3-dr-net-05,
		// R2-P3-rr-dev-05).
		w.busy = false
	}
	if len(w.networks) == 0 {
		w.lastNetworkID, w.lastConnectErr, w.lastStatus = nil, nil, nil
	}
	w.dataVersion.Bump()
	switch {
	case len(w.committed) == 0:
		// Nothing committed: the radio leaves whatever the commissioning
		// joined (R2-P5-port-10).
		w.armDisconnectLocked()
	case len(w.link) > 0 && !w.onCommittedLocked():
		// On a network the revert removed: the rejoin below runs, and
		// when it fails the radio leaves that network.
		w.disconnectOnFailedRejoin = true
	}
	w.rejoinNowLocked()
	w.mu.Unlock()
	w.changes.Notify(netcommAttrNetworks, netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue)
}
