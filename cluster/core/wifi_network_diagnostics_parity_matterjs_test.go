// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// wifiLink is a WiFiReporter test double: every value known or none, and
// the ResetCounts calls counted.
type wifiLink struct {
	known  bool
	bssid  []byte
	resets int
}

func (w *wifiLink) BSSID() ([]byte, bool) { return w.bssid, w.known }
func (w *wifiLink) SecurityType() (wifidiagdef.SecurityTypeEnum, bool) {
	return wifidiagdef.SecurityTypeWpa2, w.known
}

func (w *wifiLink) WiFiVersion() (wifidiagdef.WiFiVersionEnum, bool) {
	return wifidiagdef.WiFiVersionAx, w.known
}
func (w *wifiLink) ChannelNumber() (uint16, bool)          { return 36, w.known }
func (w *wifiLink) RSSI() (int8, bool)                     { return -55, w.known }
func (w *wifiLink) BeaconLostCount() (uint32, bool)        { return 1, w.known }
func (w *wifiLink) BeaconRxCount() (uint32, bool)          { return 2, w.known }
func (w *wifiLink) PacketMulticastRxCount() (uint32, bool) { return 3, w.known }
func (w *wifiLink) PacketMulticastTxCount() (uint32, bool) { return 4, w.known }
func (w *wifiLink) PacketUnicastRxCount() (uint32, bool)   { return 5, w.known }
func (w *wifiLink) PacketUnicastTxCount() (uint32, bool)   { return 6, w.known }
func (w *wifiLink) CurrentMaxRate() (uint64, bool)         { return 7, w.known }
func (w *wifiLink) OverrunCount() (uint64, bool)           { return 8, w.known }
func (w *wifiLink) ResetCounts()                           { w.resets++ }

var wifiBSSID = []byte{0x02, 0x00, 0x5E, 0x10, 0x00, 0x01}

// The attribute ids by group, element file
// (wi-fi-network-diagnostics.element.ts:27-69).
var (
	wifiMandatory = []uint32{
		wifidiagdef.AttrBssid, wifidiagdef.AttrSecurityType, wifidiagdef.AttrWiFiVersion,
		wifidiagdef.AttrChannelNumber, wifidiagdef.AttrRssi,
	}
	wifiPacketCounters = []uint32{
		wifidiagdef.AttrBeaconRxCount, wifidiagdef.AttrPacketMulticastRxCount, wifidiagdef.AttrPacketMulticastTxCount,
		wifidiagdef.AttrPacketUnicastRxCount, wifidiagdef.AttrPacketUnicastTxCount,
	}
	wifiErrorCounters = []uint32{wifidiagdef.AttrBeaconLostCount, wifidiagdef.AttrOverrunCount}
)

// TestParityMatterJS_WiFiNetworkDiagnosticsDefinition: the generated
// definition is the element file's — revision 1, PKTCNT bit 0 and ERRCNT
// bit 1, the thirteen attribute ids with their types and qualities, every
// attribute nullable, ResetCounts under ERRCNT, the three info events.
func TestParityMatterJS_WiFiNetworkDiagnosticsDefinition(t *testing.T) {
	t.Parallel()
	def := wifidiagdef.Definition
	if def.ID != 0x0036 || def.Revision != 1 {
		t.Errorf("cluster 0x%04X revision %d, want 0x0036 revision 1 (element file :20-21)", def.ID, def.Revision)
	}
	if wifidiagdef.FeaturePacketCounts != 1<<0 || wifidiagdef.FeatureErrorCounts != 1<<1 {
		t.Errorf("features PKTCNT %d ERRCNT %d, want bits 0 and 1 (element file :24-25)",
			wifidiagdef.FeaturePacketCounts, wifidiagdef.FeatureErrorCounts)
	}
	for _, want := range []struct {
		id          uint32
		name        string
		typ         string
		conformance string
		changes     bool // quality "C"; every attribute is "X"
	}{
		{0x00, "Bssid", "octstr", "M", false},
		{0x01, "SecurityType", "SecurityTypeEnum", "M", false},
		{0x02, "WiFiVersion", "WiFiVersionEnum", "M", false},
		{0x03, "ChannelNumber", "uint16", "M", false},
		{0x04, "Rssi", "int8", "M", true},
		{0x05, "BeaconLostCount", "uint32", "ERRCNT", true},
		{0x06, "BeaconRxCount", "uint32", "PKTCNT", true},
		{0x07, "PacketMulticastRxCount", "uint32", "PKTCNT", true},
		{0x08, "PacketMulticastTxCount", "uint32", "PKTCNT", true},
		{0x09, "PacketUnicastRxCount", "uint32", "PKTCNT", true},
		{0x0A, "PacketUnicastTxCount", "uint32", "PKTCNT", true},
		{0x0B, "CurrentMaxRate", "uint64", "O", true},
		{0x0C, "OverrunCount", "uint64", "ERRCNT", true},
	} {
		a := def.Attribute(want.id)
		if a == nil {
			t.Errorf("attribute 0x%02X missing", want.id)
			continue
		}
		if a.Name != want.name || a.Type.Name != want.typ || a.Conformance.Text != want.conformance || a.Quality != (spec.Quality{Nullable: true, ChangesOmitted: want.changes}) {
			t.Errorf("attribute 0x%02X = %s %s %q %+v, want %s %s %q X(C=%v)", want.id, a.Name, a.Type.Name, a.Conformance.Text, a.Quality,
				want.name, want.typ, want.conformance, want.changes)
		}
	}
	if c := def.Command(wifidiagdef.CmdResetCounts, spec.Request); c == nil || c.Conformance.Text != "ERRCNT" {
		t.Errorf("ResetCounts %+v, want conformance ERRCNT (element file :83)", c)
	}
	for id, name := range map[uint32]string{0: "Disconnection", 1: "AssociationFailure", 2: "ConnectionStatus"} {
		if e := def.Event(id); e == nil || e.Name != name || e.Priority != spec.PriorityInfo {
			t.Errorf("event 0x%02X %+v, want %s, info (element file :70-82)", id, e, name)
		}
	}
}

// TestParityMatterJS_WiFiNetworkDiagnostics: the generated server
// (matter.js WiFiNetworkDiagnosticsServer.ts:14) serving the host's values,
// null where the host cannot tell (chip WiFiNetworkDiagnosticsCluster.cpp
// :41-81), the counters with their feature (:207-220) and ResetCounts with
// ERRCNT alone (:224-248).
func TestParityMatterJS_WiFiNetworkDiagnostics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	bare, err := NewWiFiNetworkDiagnostics(WiFiNetworkDiagnosticsConfig{Reporter: &wifiLink{}})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, bare, wifidiagdef.Definition, 0)
	if attrs := bare.MatterAttributes(); !slices.Equal(attrs, wifiMandatory) {
		t.Errorf("bare server serves %v, want the five mandatory %v", attrs, wifiMandatory)
	}
	for _, id := range slices.Concat(wifiPacketCounters, wifiErrorCounters, []uint32{wifidiagdef.AttrCurrentMaxRate}) {
		if _, ok := bare.MatterRead(id); ok {
			t.Errorf("attribute 0x%04X served without its feature or opt-in", id)
		}
	}
	if len(bare.MatterAcceptedCommands()) != 0 {
		t.Errorf("bare server accepts %v", bare.MatterAcceptedCommands())
	}
	if _, err := bare.MatterInvoke(ctx, wifidiagdef.CmdResetCounts, wifidiagdef.ResetCountsRequest{}); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("ResetCounts without ERRCNT: %v", err)
	}
	for _, id := range wifiMandatory {
		if v, ok := bare.MatterRead(id); !ok || v != nil {
			t.Errorf("unknown attribute 0x%04X = %v (%v), want null", id, v, ok)
		}
	}

	link := &wifiLink{known: true, bssid: wifiBSSID}
	features := wifidiagdef.FeaturePacketCounts | wifidiagdef.FeatureErrorCounts
	srv, err := NewWiFiNetworkDiagnostics(WiFiNetworkDiagnosticsConfig{
		Reporter: link, Features: features, Attributes: []uint32{wifidiagdef.AttrCurrentMaxRate},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, wifidiagdef.Definition, uint32(features))
	for id, want := range map[uint32]any{
		wifidiagdef.AttrSecurityType:           uint8(wifidiagdef.SecurityTypeWpa2),
		wifidiagdef.AttrWiFiVersion:            uint8(wifidiagdef.WiFiVersionAx),
		wifidiagdef.AttrChannelNumber:          uint16(36),
		wifidiagdef.AttrRssi:                   int8(-55),
		wifidiagdef.AttrBeaconLostCount:        uint32(1),
		wifidiagdef.AttrBeaconRxCount:          uint32(2),
		wifidiagdef.AttrPacketMulticastRxCount: uint32(3),
		wifidiagdef.AttrPacketMulticastTxCount: uint32(4),
		wifidiagdef.AttrPacketUnicastRxCount:   uint32(5),
		wifidiagdef.AttrPacketUnicastTxCount:   uint32(6),
		wifidiagdef.AttrCurrentMaxRate:         uint64(7),
		wifidiagdef.AttrOverrunCount:           uint64(8),
	} {
		if v, ok := srv.MatterRead(id); !ok || v != want {
			t.Errorf("attribute 0x%04X = %v (%T, %v), want %v (%T)", id, v, v, ok, want, want)
		}
	}
	if v, ok := srv.MatterRead(wifidiagdef.AttrBssid); !ok || !bytes.Equal(v.([]byte), wifiBSSID) {
		t.Errorf("Bssid = %v (%v)", v, ok)
	}
	// connectedhomeip encodes an empty BSSID the provider read without an
	// error as null (ReadWiFiBssId :68-74).
	link.bssid = nil
	if v, ok := srv.MatterRead(wifidiagdef.AttrBssid); !ok || v != nil {
		t.Errorf("empty Bssid = %v (%v), want null", v, ok)
	}
	link.known = false
	for _, id := range slices.Concat(wifiPacketCounters, wifiErrorCounters, []uint32{wifidiagdef.AttrCurrentMaxRate, wifidiagdef.AttrRssi}) {
		if v, ok := srv.MatterRead(id); !ok || v != nil {
			t.Errorf("unknown attribute 0x%04X = %v (%v), want null", id, v, ok)
		}
	}

	if !slices.Equal(srv.MatterAcceptedCommands(), []uint32{wifidiagdef.CmdResetCounts}) {
		t.Errorf("accepted %v, want ResetCounts", srv.MatterAcceptedCommands())
	}
	if res, err := srv.MatterInvoke(ctx, wifidiagdef.CmdResetCounts, wifidiagdef.ResetCountsRequest{}); err != nil || res != nil || link.resets != 1 {
		t.Errorf("ResetCounts: %v %v, %d resets", res, err, link.resets)
	}

	// PKTCNT alone: the packet counters, no error counters, no ResetCounts.
	pkt, err := NewWiFiNetworkDiagnostics(WiFiNetworkDiagnosticsConfig{Reporter: link, Features: wifidiagdef.FeaturePacketCounts})
	if err != nil {
		t.Fatal(err)
	}
	if want := slices.Concat(wifiMandatory, []uint32{
		wifidiagdef.AttrBeaconRxCount, wifidiagdef.AttrPacketMulticastRxCount,
		wifidiagdef.AttrPacketMulticastTxCount, wifidiagdef.AttrPacketUnicastRxCount, wifidiagdef.AttrPacketUnicastTxCount,
	}); !slices.Equal(pkt.MatterAttributes(), want) {
		t.Errorf("PKTCNT server serves %v, want %v", pkt.MatterAttributes(), want)
	}
	if _, err := pkt.MatterInvoke(ctx, wifidiagdef.CmdResetCounts, nil); statusOf(err) != im.StatusUnsupportedCommand || link.resets != 1 {
		t.Errorf("ResetCounts without ERRCNT: %v, %d resets", err, link.resets)
	}

	if _, err := NewWiFiNetworkDiagnostics(WiFiNetworkDiagnosticsConfig{}); !errors.Is(err, ErrNilReporter) {
		t.Errorf("no reporter: %v", err)
	}
	if _, err := NewWiFiNetworkDiagnostics(WiFiNetworkDiagnosticsConfig{Reporter: link, Attributes: []uint32{wifidiagdef.AttrBeaconLostCount}}); err == nil {
		t.Error("BeaconLostCount accepted without ERRCNT")
	}
}

// wifiEvents records the emitted events.
type wifiEvents struct {
	endpoints []uint16
	ids       []uint32
	payloads  []any
	prios     []contract.EventPriority
}

func (e *wifiEvents) MatterEmitEvent(endpoint uint16, clusterID, eventID uint32, data any, priority contract.EventPriority) {
	if clusterID != wifidiagdef.ClusterID {
		return
	}
	e.endpoints = append(e.endpoints, endpoint)
	e.ids = append(e.ids, eventID)
	e.payloads = append(e.payloads, data)
	e.prios = append(e.prios, priority)
}

// TestParityMatterJS_WiFiNetworkDiagnosticsEvents: the three events are
// emitted on the host's word, info priority, on the mounted endpoint, with
// the element file's field ids (:70-82) — Disconnection {0: ReasonCode},
// AssociationFailure {0: AssociationFailureCause, 1: Status},
// ConnectionStatus {0: ConnectionStatus} — as chip generates them
// (WiFiNetworkDiagnosticsCluster.cpp :84-112).
func TestParityMatterJS_WiFiNetworkDiagnosticsEvents(t *testing.T) {
	t.Parallel()
	srv, err := NewWiFiNetworkDiagnostics(WiFiNetworkDiagnosticsConfig{Reporter: &wifiLink{}})
	if err != nil {
		t.Fatal(err)
	}
	// Without an emitter the event is dropped, not an error.
	if err := srv.OnDisconnectionDetected(1); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(srv.MatterEvents(), []uint32{0, 1, 2}) {
		t.Errorf("events %v, want the three", srv.MatterEvents())
	}
	rec := &wifiEvents{}
	srv.SetMatterEventEmitter(rec)
	srv.SetEndpoint(0)
	if err := srv.OnDisconnectionDetected(0x0102); err != nil {
		t.Fatal(err)
	}
	if err := srv.OnAssociationFailureDetected(wifidiagdef.AssociationFailureCauseAuthenticationFailed, 0x0011); err != nil {
		t.Fatal(err)
	}
	if err := srv.OnConnectionStatusChanged(wifidiagdef.ConnectionStatusNotConnected); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rec.ids, []uint32{0, 1, 2}) || !slices.Equal(rec.endpoints, []uint16{0, 0, 0}) {
		t.Fatalf("emitted %v on %v", rec.ids, rec.endpoints)
	}
	wire := [][]byte{
		{0x15, 0x25, 0x00, 0x02, 0x01, 0x18},             // {0: 0x0102}
		{0x15, 0x24, 0x00, 0x02, 0x24, 0x01, 0x11, 0x18}, // {0: 2, 1: 0x11}
		{0x15, 0x24, 0x00, 0x01, 0x18},                   // {0: 1}
	}
	for i, want := range wire {
		if rec.prios[i] != contract.EventPriorityInfo {
			t.Errorf("event %d priority %v, want info", i, rec.prios[i])
		}
		enc := tlv.NewEncoder()
		rec.payloads[i].(spec.Encodable).EncodeTLV(enc, tlv.AnonymousTag())
		if got, err := enc.Bytes(); err != nil || !bytes.Equal(got, want) {
			t.Errorf("event %d TLV % X, want % X", i, got, want)
		}
	}
}
