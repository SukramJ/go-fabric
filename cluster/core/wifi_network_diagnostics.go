// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
)

// WiFiReporter is the host port for WiFiNetworkDiagnostics, read on every
// attribute read — connectedhomeip's DiagnosticDataProvider Wi-Fi getters.
// Each value method answers ok=false when the host cannot tell, and the
// attribute then reads as null: every attribute of the cluster is nullable
// (quality X, matter.js wi-fi-network-diagnostics.element.ts:28-69), and
// connectedhomeip encodes a failed provider read as null
// (src/app/clusters/wifi-network-diagnostics-server/
// WiFiNetworkDiagnosticsCluster.cpp ReadIfSupported :41-58, at the harness
// pin 6170af8461b10b1766044122ac83332c6d00ab20).
//
// A Linux host reads these from nl80211 (or `iw dev <if> link` and
// `iw dev <if> station dump`); the module carries no reporter of its own.
type WiFiReporter interface {
	// BSSID is the access point's BSSID, 6 bytes (constraint "6",
	// element file :28). An empty value reads as null as well, as
	// connectedhomeip's ReadWiFiBssId does (:62-81).
	BSSID() (bssid []byte, ok bool)
	// SecurityType is the security of the current association.
	SecurityType() (wifidiagdef.SecurityTypeEnum, bool)
	// WiFiVersion is the 802.11 version in use.
	WiFiVersion() (wifidiagdef.WiFiVersionEnum, bool)
	// ChannelNumber is the channel the interface is on.
	ChannelNumber() (uint16, bool)
	// RSSI is the current signal strength in dBm, within -120 to 0
	// (element file :41).
	RSSI() (int8, bool)
	// BeaconLostCount is the ERRCNT counter of missed beacons.
	BeaconLostCount() (uint32, bool)
	// BeaconRxCount is the PKTCNT counter of received beacons.
	BeaconRxCount() (uint32, bool)
	// PacketMulticastRxCount is the PKTCNT counter of multicast frames received.
	PacketMulticastRxCount() (uint32, bool)
	// PacketMulticastTxCount is the PKTCNT counter of multicast frames sent.
	PacketMulticastTxCount() (uint32, bool)
	// PacketUnicastRxCount is the PKTCNT counter of unicast frames received.
	PacketUnicastRxCount() (uint32, bool)
	// PacketUnicastTxCount is the PKTCNT counter of unicast frames sent.
	PacketUnicastTxCount() (uint32, bool)
	// CurrentMaxRate is the current maximum PHY rate in bits per second
	// (optional attribute).
	CurrentMaxRate() (uint64, bool)
	// OverrunCount is the ERRCNT counter of dropped packets.
	OverrunCount() (uint64, bool)
	// ResetCounts restarts the ERRCNT and PKTCNT counters at 0
	// (connectedhomeip's ResetWiFiNetworkDiagnosticsCounts).
	ResetCounts()
}

// WiFiNetworkDiagnosticsConfig carries the construction parameters of a
// WiFiNetworkDiagnostics server.
type WiFiNetworkDiagnosticsConfig struct {
	// Reporter answers the reads and ResetCounts; required.
	Reporter WiFiReporter
	// Features is the FeatureMap: FeaturePacketCounts (PKTCNT) serves the
	// five packet counters, FeatureErrorCounts (ERRCNT) BeaconLostCount,
	// OverrunCount and the ResetCounts command.
	Features wifidiagdef.Feature
	// Attributes lists the optional attributes served: AttrCurrentMaxRate.
	// A feature-gated attribute is served with its feature and refused
	// without it.
	Attributes []uint32
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// WiFiNetworkDiagnostics is a WiFiNetworkDiagnostics (0x0036) server on the
// generated definition (cluster/spec/wifinetworkdiagnostics, ADR 0013).
//
// Mirrors matter.js packages/node/src/behaviors/wi-fi-network-diagnostics/
// WiFiNetworkDiagnosticsServer.ts, `export class
// WiFiNetworkDiagnosticsServer extends WiFiNetworkDiagnosticsBehavior {}`
// (:14): the generated behavior with no logic of its own. What matter.js
// does not model is connectedhomeip's WiFiDiagnosticsServerCluster at the
// harness pin:
//
//   - Every attribute is the host's, read through its reporter on every
//     read (ReadAttribute :114-176), so a change is not reported to a
//     subscriber; null where the host cannot tell (:41-58, :62-81).
//   - The counters are served with their feature: BeaconLostCount and
//     OverrunCount with ERRCNT, the packet counters with PKTCNT
//     (Attributes :207-220).
//   - ResetCounts is accepted with ERRCNT only (AcceptedCommands :224-234,
//     conformance "ERRCNT", element file :83); it resets the host's
//     counters and answers SUCCESS whatever the reset did (InvokeCommand
//     :242-248).
//   - The three events are the host's to raise
//     ([WiFiNetworkDiagnostics.OnDisconnectionDetected],
//     [WiFiNetworkDiagnostics.OnAssociationFailureDetected],
//     [WiFiNetworkDiagnostics.OnConnectionStatusChanged]), as the
//     platform raises them through the cluster's delegate (:84-112).
type WiFiNetworkDiagnostics struct {
	*spec.Server

	mu       sync.Mutex
	endpoint uint16
}

// wifiDiagEvents are the three events (element file :70-82, all optional),
// every one of which connectedhomeip's cluster can generate (:84-112).
var wifiDiagEvents = []uint32{
	wifidiagdef.EventDisconnection,
	wifidiagdef.EventAssociationFailure,
	wifidiagdef.EventConnectionStatus,
}

// NewWiFiNetworkDiagnostics builds a WiFiNetworkDiagnostics server over
// cfg.Reporter.
func NewWiFiNetworkDiagnostics(cfg WiFiNetworkDiagnosticsConfig) (*WiFiNetworkDiagnostics, error) {
	if cfg.Reporter == nil {
		return nil, fmt.Errorf("core: WiFiNetworkDiagnostics: %w", ErrNilReporter)
	}
	// The handler is registered either way; without ERRCNT the instance
	// does not accept the command and it is never dispatched.
	reporter := cfg.Reporter
	srv, err := spec.NewServer(wifidiagdef.Definition, spec.Options{
		Features:   uint32(cfg.Features),
		Attributes: cfg.Attributes,
		Events:     wifiDiagEvents,
	}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Source:      wifiSource{cfg.Reporter},
		Handlers: map[uint32]spec.CommandHandler{wifidiagdef.CmdResetCounts: func(context.Context, any) (any, error) {
			reporter.ResetCounts()
			return nil, nil
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("core: WiFiNetworkDiagnostics: %w", err)
	}
	return &WiFiNetworkDiagnostics{Server: srv}, nil
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it when it mounts the server (0 until then, the root).
func (w *WiFiNetworkDiagnostics) SetEndpoint(endpoint uint16) {
	w.mu.Lock()
	w.endpoint = endpoint
	w.mu.Unlock()
}

func (w *WiFiNetworkDiagnostics) eventEndpoint() uint16 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.endpoint
}

// OnDisconnectionDetected emits Disconnection (event 0x00, info) with the
// 802.11 reason code — connectedhomeip's OnDisconnectionDetected (:84-92).
func (w *WiFiNetworkDiagnostics) OnDisconnectionDetected(reasonCode uint16) error {
	return w.Emit(w.eventEndpoint(), wifidiagdef.EventDisconnection, wifidiagdef.DisconnectionEvent{ReasonCode: reasonCode})
}

// OnAssociationFailureDetected emits AssociationFailure (event 0x01, info)
// with the cause and the 802.11 status code — connectedhomeip's
// OnAssociationFailureDetected (:94-102).
func (w *WiFiNetworkDiagnostics) OnAssociationFailureDetected(cause wifidiagdef.AssociationFailureCauseEnum, status uint16) error {
	return w.Emit(w.eventEndpoint(), wifidiagdef.EventAssociationFailure, wifidiagdef.AssociationFailureEvent{
		AssociationFailureCause: cause, Status: status,
	})
}

// OnConnectionStatusChanged emits ConnectionStatus (event 0x02, info) —
// connectedhomeip's OnConnectionStatusChanged (:104-112).
func (w *WiFiNetworkDiagnostics) OnConnectionStatusChanged(status wifidiagdef.ConnectionStatusEnum) error {
	return w.Emit(w.eventEndpoint(), wifidiagdef.EventConnectionStatus, wifidiagdef.ConnectionStatusEvent{ConnectionStatus: status})
}

// wifiSource answers the served attributes from the host, in the stored
// form the reply encoder writes: a uint8 for an enum, nil for null.
type wifiSource struct{ r WiFiReporter }

// MatterAttribute implements [spec.Source]; the server asks only for the
// attributes it serves.
func (s wifiSource) MatterAttribute(attrID uint32) (any, bool) {
	switch attrID {
	case wifidiagdef.AttrBssid:
		if v, ok := s.r.BSSID(); ok && len(v) > 0 {
			return v, true
		}
		return nil, true
	case wifidiagdef.AttrSecurityType:
		v, ok := s.r.SecurityType()
		return nullable(uint8(v), ok), true
	case wifidiagdef.AttrWiFiVersion:
		v, ok := s.r.WiFiVersion()
		return nullable(uint8(v), ok), true
	case wifidiagdef.AttrChannelNumber:
		v, ok := s.r.ChannelNumber()
		return nullable(v, ok), true
	case wifidiagdef.AttrRssi:
		v, ok := s.r.RSSI()
		return nullable(v, ok), true
	case wifidiagdef.AttrCurrentMaxRate:
		v, ok := s.r.CurrentMaxRate()
		return nullable(v, ok), true
	case wifidiagdef.AttrOverrunCount:
		v, ok := s.r.OverrunCount()
		return nullable(v, ok), true
	}
	if read, isCounter := s.counter32(attrID); isCounter {
		v, ok := read()
		return nullable(v, ok), true
	}
	return nil, false
}

// counter32 returns the reporter method of a uint32 counter.
func (s wifiSource) counter32(attrID uint32) (func() (uint32, bool), bool) {
	switch attrID {
	case wifidiagdef.AttrBeaconLostCount:
		return s.r.BeaconLostCount, true
	case wifidiagdef.AttrBeaconRxCount:
		return s.r.BeaconRxCount, true
	case wifidiagdef.AttrPacketMulticastRxCount:
		return s.r.PacketMulticastRxCount, true
	case wifidiagdef.AttrPacketMulticastTxCount:
		return s.r.PacketMulticastTxCount, true
	case wifidiagdef.AttrPacketUnicastRxCount:
		return s.r.PacketUnicastRxCount, true
	case wifidiagdef.AttrPacketUnicastTxCount:
		return s.r.PacketUnicastTxCount, true
	}
	return nil, false
}

// nullable is v when ok, else nil (null).
func nullable[T any](v T, ok bool) any {
	if !ok {
		return nil
	}
	return v
}
