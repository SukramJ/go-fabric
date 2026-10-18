// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// The network steps of the flow (steps 16 and 17). Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:1527-1965 at
// 0ceb202d with its helpers (:895-1075, :1967-1998) and constants
// (:292, :304-305).

// WiFiNetwork are the credentials of the Wi-Fi network the device is to
// join — matter.js wifiNetwork (ControllerCommissioningFlow.ts:102-105).
type WiFiNetwork struct {
	SSID        string
	Credentials string
}

// ThreadNetwork is the Thread network the device is to join — matter.js
// threadNetwork (:110-113) with the operational dataset as bytes (the
// hex string matter.js takes is the caller's to decode).
type ThreadNetwork struct {
	// NetworkName, when set, must match the dataset's; empty takes the
	// dataset's (resolveThreadNetworkName, :263-286).
	NetworkName string
	// OperationalDataset is the Thread operational dataset (MeshCoP TLVs).
	OperationalDataset []byte
}

// Error kinds of the network steps (:236-254).
var (
	// ErrWifiOrThreadNetworkCredentialsNotConfigured: the device has no
	// Ethernet and no connected network, and no credentials were given
	// (:236).
	ErrWifiOrThreadNetworkCredentialsNotConfigured = errors.New("Wi-Fi or Thread network credentials not configured") //nolint:revive,staticcheck // matter.js's class, by name
	// ErrWifiNetworkSetupFailed: the device refused the Wi-Fi network or
	// could not connect to it (:239).
	ErrWifiNetworkSetupFailed = errors.New("Wi-Fi network setup failed") //nolint:revive,staticcheck // matter.js's class, by name
	// ErrThreadNetworkSetupFailed: the device refused the Thread network
	// or could not connect to it (:242).
	ErrThreadNetworkSetupFailed = errors.New("Thread network setup failed") //nolint:revive,staticcheck // matter.js's class, by name
	// ErrThreadNetworkNameMismatch: the given network name contradicts the
	// dataset's (:254).
	ErrThreadNetworkNameMismatch = errors.New("Thread network name mismatch") //nolint:revive,staticcheck // matter.js's class, by name
)

// Timing of the network steps.
const (
	// BTPIdleAliveInterval is the period of the failsafe re-arm on a BLE
	// channel during long actions, which keeps the BTP session alive
	// (BTP_IDLE_ALIVE_INTERVAL, :292).
	BTPIdleAliveInterval = 25 * time.Second
	// MinNetworkScanTimeout and MinNetworkConnectTimeout floor the
	// device's ScanMaxTimeSeconds and ConnectMaxTimeSeconds (:304-305).
	MinNetworkScanTimeout    = 60 * time.Second
	MinNetworkConnectTimeout = 60 * time.Second
)

// meshCopNetworkName is the Network Name MeshCoP TLV type (matter.js
// packages/protocol/src/thread/meshcopTlvTypes.ts:18).
const meshCopNetworkName = 3

// errThreadTLV is a dataset BasicTlv.walk refuses
// (packages/protocol/src/thread/BasicTlvCodec.ts).
var errThreadTLV = errors.New("truncated Thread TLV")

// threadNetworkNameOf is the Network Name of a Thread operational
// dataset: the MeshCoP TLVs walked as BasicTlv.walk does (one-byte type,
// one-byte length, 0xFF for a two-byte big-endian length), the Network
// Name TLV read as UTF-8 (OperationalDataset.ts:53-69, :133-136). Only
// what the flow needs: the other TLVs are not validated. ok is false
// without a Network Name TLV.
func threadNetworkNameOf(dataset []byte) (name string, ok bool, err error) {
	for off := 0; off < len(dataset); {
		if off+2 > len(dataset) {
			return "", false, fmt.Errorf("%w: header at offset %d", errThreadTLV, off)
		}
		typ, l, start := dataset[off], int(dataset[off+1]), off+2
		if l == 0xff {
			if off+4 > len(dataset) {
				return "", false, fmt.Errorf("%w: extended-length header at offset %d", errThreadTLV, off)
			}
			l, start = int(dataset[off+2])<<8|int(dataset[off+3]), off+4
		}
		end := start + l
		if end > len(dataset) {
			return "", false, fmt.Errorf("%w: value at offset %d (type=%d, length=%d)", errThreadTLV, off, typ, l)
		}
		if typ == meshCopNetworkName {
			name, ok = string(dataset[start:end]), true
		}
		off = end
	}
	return name, ok, nil
}

// resolveThreadNetworkName is resolveThreadNetworkName (:256-286): the
// dataset's name when none is given, the given one verified against the
// dataset's; a dataset that cannot be decoded keeps the given name.
func resolveThreadNetworkName(t *ThreadNetwork, log *slog.Logger) (string, error) {
	datasetName, has, err := threadNetworkNameOf(t.OperationalDataset)
	if err != nil {
		log.Warn("matter.controller.commissioning.thread_dataset_undecodable", slog.String("err", err.Error()))
		return t.NetworkName, nil
	}
	if t.NetworkName == "" {
		return datasetName, nil
	}
	if has && datasetName != t.NetworkName {
		return "", newError(ErrThreadNetworkNameMismatch, fmt.Sprintf("Thread network name %q does not match the name %q in the supplied operational dataset", t.NetworkName, datasetName), nil)
	}
	return t.NetworkName, nil
}

// decodeNetworks decodes a Networks attribute value.
func decodeNetworks(n spec.Node) ([]netcomm.NetworkInfoStruct, error) {
	return spec.DecodeList(func(e spec.Node) (netcomm.NetworkInfoStruct, error) {
		var v netcomm.NetworkInfoStruct
		err := v.DecodeTLV(e)
		return v, err
	})(n)
}

// rootNetwork is the root endpoint's NetworkCommissioning FeatureMap and
// Networks, as collected (:1597-1602).
func (f *flow) rootNetwork() (featureMap uint32, networks []netcomm.NetworkInfoStruct) {
	for _, n := range f.data.networkFeatures {
		if n.endpoint == rootEndpoint {
			featureMap = n.featureMap
			break
		}
	}
	for _, n := range f.data.networkStatus {
		if n.endpoint == rootEndpoint {
			networks = n.networks
			break
		}
	}
	return featureMap, networks
}

func (f *flow) concurrentNotFalse() bool {
	return f.data.supportsConcurrentConnection == nil || *f.data.supportsConcurrentConnection
}

// validateNetwork is step 16.1 (#validateNetwork, :1537-1575): without
// Wi-Fi or Thread credentials, a device with neither an Ethernet
// interface (or no NetworkCommissioning at all) nor a connected network
// cannot be commissioned.
func (f *flow) validateNetwork(context.Context) (stepResult, error) {
	if f.data.networkFeatures == nil || f.data.networkStatus == nil {
		return 0, newError(ErrCommissioning, "Initial information collection failed. No network features or status collected. This should never happen.", nil)
	}
	wifi := f.opts.WiFiNetwork
	thread := f.opts.ThreadNetwork
	if (wifi == nil || wifi.SSID == "" || wifi.Credentials == "") && (thread == nil || len(thread.OperationalDataset) == 0) {
		anyEthernet := len(f.data.networkFeatures) == 0
		for _, n := range f.data.networkFeatures {
			if n.featureMap&uint32(netcomm.FeatureEthernetNetworkInterface) != 0 {
				anyEthernet = true
			}
		}
		anyConnected := len(f.data.networkStatus) == 0
		for _, s := range f.data.networkStatus {
			for _, n := range s.networks {
				if n.Connected {
					anyConnected = true
				}
			}
		}
		if !anyEthernet && !anyConnected {
			return 0, newError(ErrWifiOrThreadNetworkCredentialsNotConfigured, "No Wi-Fi/Thread network credentials are configured for commissioning and no Ethernet interface is available on the device and no interface already connected.", nil)
		}
	}
	return stepSuccess, nil
}

// networkTimeouts reads ScanMaxTimeSeconds and ConnectMaxTimeSeconds, each
// floored at 60 s (:1622-1635, :1810-1823).
func (f *flow) networkTimeouts(ctx context.Context) (scan, connect time.Duration, err error) {
	v, ok, err := f.readValues(ctx, rootAttr(netcomm.ClusterID, netcomm.AttrScanMaxTimeSeconds), rootAttr(netcomm.ClusterID, netcomm.AttrConnectMaxTimeSeconds))
	if err != nil {
		return 0, 0, err
	}
	seconds := func(i int) time.Duration {
		if !ok[i] {
			return 0
		}
		s, err := spec.DecodeUint[uint8](8)(v[i])
		if err != nil {
			return 0
		}
		return time.Duration(s) * time.Second
	}
	return max(seconds(0), MinNetworkScanTimeout), max(seconds(1), MinNetworkConnectTimeout), nil
}

// statusText is NetworkCommissioningStatus[status] with the number.
func statusText(s netcomm.NetworkCommissioningStatusEnum) string {
	return fmt.Sprintf("%s (%d)", enumName(netcomm.NetworkCommissioningStatusEnumDef, uint64(s)), s)
}

// scanHintSuffix is the hint matter.js appends to a setup failure.
func scanHintSuffix(hint string) string {
	if hint == "" {
		return ""
	}
	return " (" + hint + " - verify network name and availability)"
}

// configureNetworkWifi is step 16.2 (#configureNetworkWifi, :1577-1756):
// skipped without credentials, without the root's Wi-Fi feature, or when
// the root's first network is connected already; the scan for the SSID
// on a concurrent device, its failures only a hint; the failsafe armed for
// the connect and the CASE reconnect; AddOrUpdateWiFiNetwork with the
// breadcrumb, the network then read back by its index; ConnectNetwork
// unless it is connected. A dropped channel ends the step successfully
// and the flow goes on as non-concurrent.
func (f *flow) configureNetworkWifi(ctx context.Context) (stepResult, error) {
	wifi := f.opts.WiFiNetwork
	if wifi == nil {
		return stepSkipped, nil
	}
	if f.data.networkFeatures != nil && f.data.networkStatus != nil {
		fm, nets := f.rootNetwork()
		if fm&uint32(netcomm.FeatureWiFiNetworkInterface) == 0 {
			f.log.Debug("matter.controller.commissioning.no_wifi_interface")
			return stepSkipped, nil
		}
		if len(nets) > 0 && nets[0].Connected {
			f.log.Debug("matter.controller.commissioning.wifi_already_connected")
			f.data.successfullyConnected = true
			return stepSkipped, nil
		}
	}
	ssid, credentials := []byte(wifi.SSID), []byte(wifi.Credentials)
	scanMax, connectMax, err := f.networkTimeouts(ctx)
	if err != nil {
		return 0, err
	}
	hint := ""
	if f.concurrentNotFalse() {
		bc := f.nextBreadcrumb()
		scan, why, err := f.scanNetworks(ctx, netcomm.ScanNetworksRequest{Ssid: &spec.Nullable[[]byte]{Value: ssid}, Breadcrumb: &bc}, scanMax)
		switch {
		case err != nil:
			return 0, err
		case scan == nil:
			hint = "scan failed: " + why
		case scan.NetworkingStatus != netcomm.NetworkCommissioningStatusSuccess:
			hint = "scan failed: status=" + statusText(scan.NetworkingStatus) + debugSuffix(", ", scan.DebugText)
		case scan.WiFiScanResults == nil || len(*scan.WiFiScanResults) == 0:
			hint = "network not found in scan results"
		}
		if hint != "" {
			f.log.Warn("matter.controller.commissioning.wifi_scan", slog.String("ssid", wifi.SSID), slog.String("hint", hint))
		}
	}
	if err := f.ensureFailsafeTimerFor(ctx, f.connectNetworkFailsafeTime(connectMax)); err != nil {
		return 0, err
	}
	network, dropped, err := f.withChannelDropFallback(func() (netcomm.NetworkInfoStruct, error) {
		bc := f.nextBreadcrumb()
		res, err := f.invoke(ctx, netcomm.ClusterID, netcomm.CmdAddOrUpdateWiFiNetwork, netcomm.AddOrUpdateWiFiNetworkRequest{Ssid: ssid, Credentials: credentials, Breadcrumb: &bc}, true)
		if err != nil {
			return netcomm.NetworkInfoStruct{}, err
		}
		var resp netcomm.NetworkConfigResponse
		if err := response(res, "NetworkConfigResponse", resp.DecodeTLV); err != nil {
			return netcomm.NetworkInfoStruct{}, err
		}
		if resp.NetworkingStatus != netcomm.NetworkCommissioningStatusSuccess {
			return netcomm.NetworkInfoStruct{}, newError(ErrWifiNetworkSetupFailed, "Commissionee failed to add WiFi network \""+wifi.SSID+"\""+debugSuffix(": ", resp.DebugText)+scanHintSuffix(hint), nil)
		}
		if resp.NetworkIndex == nil {
			return netcomm.NetworkInfoStruct{}, newError(ErrWifiNetworkSetupFailed, "Commissionee did not return network index", nil)
		}
		return f.networkAt(ctx, *resp.NetworkIndex, ErrWifiNetworkSetupFailed)
	})
	if err != nil {
		return 0, err
	}
	if dropped {
		return stepSuccess, nil
	}
	if network.Connected {
		f.data.successfullyConnected = true
		return stepSuccess, nil
	}
	conn, dropped, err := f.invokeConnectNetwork(ctx, network.NetworkId, connectMax)
	if err != nil {
		return 0, err
	}
	if dropped {
		return stepSuccess, nil
	}
	if conn.NetworkingStatus != netcomm.NetworkCommissioningStatusSuccess {
		return 0, newError(ErrWifiNetworkSetupFailed, "Commissionee failed to connect to WiFi network \""+wifi.SSID+"\""+debugSuffix(": ", conn.DebugText)+scanHintSuffix(hint), nil)
	}
	f.data.successfullyConnected = true
	return stepSuccess, nil
}

// configureNetworkThread is step 16.3 (#configureNetworkThread,
// :1758-1958): skipped once a network connected, without a dataset,
// without the root's Thread feature, or when the root's first network is
// connected; the network name resolved; the scan when a name is known on
// a concurrent device, its failures only a hint;
// AddOrUpdateThreadNetwork, the network read back, ConnectNetwork.
func (f *flow) configureNetworkThread(ctx context.Context) (stepResult, error) {
	if f.data.successfullyConnected {
		f.log.Info("matter.controller.commissioning.thread_skipped_connected")
		return stepSkipped, nil
	}
	thread := f.opts.ThreadNetwork
	if thread == nil {
		return stepSkipped, nil
	}
	if f.data.networkFeatures != nil && f.data.networkStatus != nil {
		fm, nets := f.rootNetwork()
		if fm&uint32(netcomm.FeatureThreadNetworkInterface) == 0 {
			f.log.Debug("matter.controller.commissioning.no_thread_interface")
			return stepSkipped, nil
		}
		if len(nets) > 0 && nets[0].Connected {
			f.log.Debug("matter.controller.commissioning.thread_already_connected")
			return stepSkipped, nil
		}
	}
	name, err := resolveThreadNetworkName(thread, f.log)
	if err != nil {
		return 0, err
	}
	scanMax, connectMax, err := f.networkTimeouts(ctx)
	if err != nil {
		return 0, err
	}
	hint, err := f.threadScanHint(ctx, name, scanMax)
	if err != nil {
		return 0, err
	}
	if err := f.ensureFailsafeTimerFor(ctx, f.connectNetworkFailsafeTime(connectMax)); err != nil {
		return 0, err
	}
	network, dropped, err := f.withChannelDropFallback(func() (netcomm.NetworkInfoStruct, error) {
		bc := f.nextBreadcrumb()
		res, err := f.invoke(ctx, netcomm.ClusterID, netcomm.CmdAddOrUpdateThreadNetwork, netcomm.AddOrUpdateThreadNetworkRequest{OperationalDataset: thread.OperationalDataset, Breadcrumb: &bc}, true)
		if err != nil {
			return netcomm.NetworkInfoStruct{}, err
		}
		var resp netcomm.NetworkConfigResponse
		if err := response(res, "NetworkConfigResponse", resp.DecodeTLV); err != nil {
			return netcomm.NetworkInfoStruct{}, err
		}
		if resp.NetworkingStatus != netcomm.NetworkCommissioningStatusSuccess {
			return netcomm.NetworkInfoStruct{}, newError(ErrThreadNetworkSetupFailed, "Commissionee failed to add Thread network: status="+statusText(resp.NetworkingStatus)+debugSuffix(", ", resp.DebugText)+scanHintSuffix(hint), nil)
		}
		if resp.NetworkIndex == nil {
			return netcomm.NetworkInfoStruct{}, newError(ErrThreadNetworkSetupFailed, "Commissionee did not return network index", nil)
		}
		return f.networkAt(ctx, *resp.NetworkIndex, ErrThreadNetworkSetupFailed)
	})
	if err != nil {
		return 0, err
	}
	if dropped || network.Connected {
		return stepSuccess, nil
	}
	conn, dropped, err := f.invokeConnectNetwork(ctx, network.NetworkId, connectMax)
	if err != nil {
		return 0, err
	}
	if dropped {
		return stepSuccess, nil
	}
	if conn.NetworkingStatus != netcomm.NetworkCommissioningStatusSuccess {
		return 0, newError(ErrThreadNetworkSetupFailed, "Commissionee failed to connect to Thread network: status="+statusText(conn.NetworkingStatus)+debugSuffix(", ", conn.DebugText)+scanHintSuffix(hint), nil)
	}
	return stepSuccess, nil
}

// threadScanHint is the Thread scan of :1822-1859: only with a network
// name on a concurrent device; its failures, and a scan without the
// network, are a hint.
func (f *flow) threadScanHint(ctx context.Context, name string, scanMax time.Duration) (string, error) {
	if name == "" {
		f.log.Info("matter.controller.commissioning.thread_scan_skipped", slog.String("reason", "no network name"))
		return "", nil
	}
	if !f.concurrentNotFalse() {
		return "", nil
	}
	bc := f.nextBreadcrumb()
	scan, why, err := f.scanNetworks(ctx, netcomm.ScanNetworksRequest{Breadcrumb: &bc}, scanMax)
	hint := ""
	switch {
	case err != nil:
		return "", err
	case scan == nil:
		hint = "scan failed: " + why
	case scan.NetworkingStatus != netcomm.NetworkCommissioningStatusSuccess:
		hint = "scan failed: status=" + statusText(scan.NetworkingStatus) + debugSuffix(", ", scan.DebugText)
	case scan.ThreadScanResults == nil || len(*scan.ThreadScanResults) == 0:
		hint = "no Thread networks found in scan results"
	default:
		found := false
		for _, r := range *scan.ThreadScanResults {
			if r.NetworkName == name {
				found = true
			}
		}
		if !found {
			hint = "network \"" + name + "\" not found in scan results"
		}
	}
	if hint != "" {
		f.log.Warn("matter.controller.commissioning.thread_scan", slog.String("name", name), slog.String("hint", hint))
	}
	return hint, nil
}

func debugSuffix(sep string, text *string) string {
	if text == nil || *text == "" {
		return ""
	}
	return sep + *text
}

// networkAt reads the root's Networks and returns the entry at index
// (:1699-1711, :1896-1911).
func (f *flow) networkAt(ctx context.Context, index uint8, kind error) (netcomm.NetworkInfoStruct, error) {
	v, ok, err := f.readValues(ctx, rootAttr(netcomm.ClusterID, netcomm.AttrNetworks))
	if err != nil {
		return netcomm.NetworkInfoStruct{}, err
	}
	var nets []netcomm.NetworkInfoStruct
	if ok[0] {
		if nets, err = decodeNetworks(v[0]); err != nil {
			return netcomm.NetworkInfoStruct{}, fmt.Errorf("%w: Networks: %w", errUnexpectedResponse, err)
		}
	}
	if int(index) >= len(nets) {
		return netcomm.NetworkInfoStruct{}, newError(kind, fmt.Sprintf("Commissionee did not return network with index %d", index), nil)
	}
	return nets[index], nil
}

// scanNetworks is #scanNetworks (:1959-1998): the failsafe armed for the
// scan, ScanNetworks with the scan time as processing time. A refusal
// with a status, a missing response or none in time is no failure: the
// reason is returned instead of the response. Any other error is.
func (f *flow) scanNetworks(ctx context.Context, req netcomm.ScanNetworksRequest, scanTime time.Duration) (*netcomm.ScanNetworksResponse, string, error) {
	if err := f.ensureFailsafeTimerFor(ctx, scanTime); err != nil {
		return nil, "", err
	}
	res, err := f.invokeWith(ctx, netcomm.ClusterID, netcomm.CmdScanNetworks, req, interaction.InvokeOptions{ExpectedProcessingTime: scanTime})
	if err != nil {
		var se *interaction.StatusError
		var pm *exchange.PeerMessageMissingError
		if errors.As(err, &se) || errors.As(err, &pm) {
			return nil, err.Error(), nil
		}
		return nil, "", err
	}
	if !res.HasFields {
		return nil, "no ScanNetworksResponse received", nil
	}
	var resp netcomm.ScanNetworksResponse
	if err := resp.DecodeTLV(res.Fields); err != nil {
		return nil, "", fmt.Errorf("%w: ScanNetworksResponse: %w", errUnexpectedResponse, err)
	}
	return &resp, "", nil
}

// invokeConnectNetwork is #invokeConnectNetwork (:1049-1075): the failsafe
// armed, ConnectNetwork with the breadcrumb and the connect time as
// processing time; a dropped channel is reported, not an error.
func (f *flow) invokeConnectNetwork(ctx context.Context, networkID []byte, connectMax time.Duration) (netcomm.ConnectNetworkResponse, bool, error) {
	if err := f.ensureFailsafeTimerFor(ctx, f.connectNetworkFailsafeTime(connectMax)); err != nil {
		return netcomm.ConnectNetworkResponse{}, false, err
	}
	return f.withChannelDropFallbackConnect(func() (netcomm.ConnectNetworkResponse, error) {
		bc := f.nextBreadcrumb()
		res, err := f.invokeWith(ctx, netcomm.ClusterID, netcomm.CmdConnectNetwork, netcomm.ConnectNetworkRequest{NetworkId: networkID, Breadcrumb: &bc}, interaction.InvokeOptions{ExpectedProcessingTime: connectMax})
		if err != nil {
			return netcomm.ConnectNetworkResponse{}, err
		}
		var resp netcomm.ConnectNetworkResponse
		if err := response(res, "ConnectNetworkResponse", resp.DecodeTLV); err != nil {
			return netcomm.ConnectNetworkResponse{}, err
		}
		return resp, nil
	})
}

// connectNetworkFailsafeTime is #connectNetworkFailsafeTime (:953-975):
// on BLE, or for a device that declared itself non-concurrent, the
// failsafe covers the connect and the CASE reconnect, at least the
// default failsafe; else the connect time.
func (f *flow) connectNetworkFailsafeTime(connectMax time.Duration) time.Duration {
	if f.ble || !f.concurrentNotFalse() {
		return max(connectMax+CASEReconnectTimeout, f.defaultFailSafe)
	}
	return connectMax
}

// isChannelDrop is #isCommissioningChannelDrop (:977-997): the BLE
// channel gone, or the peer unresponsive (a missing response counts).
func isChannelDrop(err error) bool {
	var pu *exchange.PeerUnresponsiveError
	var pm *exchange.PeerMessageMissingError
	return errors.Is(err, btp.ErrDisconnected) || errors.As(err, &pu) || errors.As(err, &pm)
}

// handleChannelDrop is #handleCommissioningChannelDrop (:999-1012): the
// device is taken as non-concurrent and the re-arm timer stops.
func (f *flow) handleChannelDrop(err error) {
	f.log.Warn("matter.controller.commissioning.channel_dropped", slog.String("err", err.Error()))
	concurrent := false
	f.data.supportsConcurrentConnection = &concurrent
	f.stopArmInterval()
}

// withChannelDropFallback is #withCommissioningChannelDropFallback
// (:1014-1036) for the add step: dropped reports a channel drop.
func (f *flow) withChannelDropFallback(action func() (netcomm.NetworkInfoStruct, error)) (netcomm.NetworkInfoStruct, bool, error) {
	v, err := action()
	if err != nil {
		if !isChannelDrop(err) {
			return v, false, err
		}
		f.handleChannelDrop(err)
		return v, true, nil
	}
	return v, false, nil
}

// withChannelDropFallbackConnect is the same for ConnectNetwork.
func (f *flow) withChannelDropFallbackConnect(action func() (netcomm.ConnectNetworkResponse, error)) (netcomm.ConnectNetworkResponse, bool, error) {
	v, err := action()
	if err != nil {
		if !isChannelDrop(err) {
			return v, false, err
		}
		f.handleChannelDrop(err)
		return v, true, nil
	}
	return v, false, nil
}

// armInterval is the BLE re-arm timer (#armFailsafeInterval, :905-938):
// every BTPIdleAliveInterval the failsafe is re-armed for the larger of
// the default failsafe and what is left, while the PASE session lives and
// the cumulative failsafe has room.
type armInterval struct {
	timer   exchange.Timer
	stopped bool
}

func (f *flow) armIntervalRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.armTimer != nil && !f.armTimer.stopped
}

func (f *flow) stopArmInterval() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.armTimer != nil {
		f.armTimer.stopped = true
		f.armTimer.timer.Stop()
		f.armTimer = nil
	}
}

// startArmInterval (re)starts the timer (:906-938).
func (f *flow) startArmInterval(ctx context.Context) {
	f.stopArmInterval()
	ai := &armInterval{}
	f.mu.Lock()
	f.armTimer = ai
	f.scheduleArm(context.WithoutCancel(ctx), ai)
	f.mu.Unlock()
}

// scheduleArm arms the next period; f.mu is held.
func (f *flow) scheduleArm(ctx context.Context, ai *armInterval) {
	ai.timer = f.c.ctx.Clock.AfterFunc(BTPIdleAliveInterval, func() {
		f.mu.Lock()
		if ai.stopped || f.armTimer != ai {
			f.mu.Unlock()
			return
		}
		ia := f.ia
		expiry := f.commissioningExpiry
		f.mu.Unlock()
		if s := ia.Session(); s != nil && s.IsClosed() {
			f.log.Debug("matter.controller.commissioning.rearm_skipped", slog.String("reason", "session closed"))
			f.stopArmInterval()
			return
		}
		now := f.now()
		if expiry.IsZero() || !now.Before(expiry) {
			f.stopArmInterval()
			return
		}
		f.log.Debug("matter.controller.commissioning.rearm_periodic", slog.Duration("left", expiry.Sub(now)))
		// The next period is armed now; the re-arm runs beside the step.
		f.mu.Lock()
		if !ai.stopped && f.armTimer == ai {
			f.scheduleArm(ctx, ai)
		}
		f.mu.Unlock()
		if _, err := f.armFailsafe(ctx, max(f.defaultFailSafe, f.failSafeTimeLeft())); err != nil {
			f.log.Warn("matter.controller.commissioning.rearm_periodic_failed", slog.String("err", err.Error()))
			f.stopArmInterval()
		}
	})
}
