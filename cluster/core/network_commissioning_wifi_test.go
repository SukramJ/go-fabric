// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// The Wi-Fi NetworkCommissioning server against connectedhomeip at the
// harness pin (6170af8461b10b1766044122ac83332c6d00ab20):
// src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp
// and src/platform/Linux/NetworkCommissioningWiFiDriver.cpp. matter.js's
// server is a generated stub (NetworkCommissioningServer.ts at 0ceb202d).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

type simDriver struct {
	mu         sync.Mutex
	connects   [][]byte
	connStatus uint8
	scanStatus uint8
	results    []contract.WiFiScanResult
}

func (d *simDriver) ScanNetworks(_ context.Context, _ []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return d.scanStatus, "", d.results
}

func (d *simDriver) ConnectNetwork(_ context.Context, ssid, _ []byte) (status uint8, debugText string, errorValue int32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connects = append(d.connects, bytes.Clone(ssid))
	if d.connStatus != 0 {
		return d.connStatus, "auth", -7
	}
	return 0, "", 0
}

func newWiFiNC(t *testing.T, armed *bool, crumbs *[]uint64) (*WiFiNetworkCommissioning, *simDriver) {
	t.Helper()
	d := &simDriver{}
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
		Driver: d, IsFailSafeArmed: func() bool { return *armed },
		SetBreadcrumb: func(v uint64) { *crumbs = append(*crumbs, v) },
		// A held clock: the rejoin a revert arms stays armed, so the
		// commands below meet no join of their own.
		Clock: clocktest.New(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w, d
}

func wifiStatusOf(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var se im.StatusCodeError
	if !errors.As(err, &se) {
		t.Fatalf("error %v carries no status", err)
	}
	return se.MatterStatusCode()
}

func bc(v uint64) *uint64 { return &v }

func TestWiFiNetworkCommissioningAttributes(t *testing.T) {
	armed := false
	var crumbs []uint64
	w, _ := newWiFiNC(t, &armed, &crumbs)
	if _, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{}); err == nil {
		t.Fatal("no driver accepted")
	}
	if fm, _ := w.MatterRead(cluster.AttrGlobalFeatureMap); fm != NetworkCommFeatureWiFi {
		t.Fatalf("FeatureMap %v", fm)
	}
	for attr, want := range map[uint32]any{
		netcommAttrMaxNetworks: uint8(1), netcommAttrScanMaxTimeSeconds: uint8(10), netcommAttrConnectMaxTimeSeconds: uint8(20),
		netcommAttrInterfaceEnabled: true, netcommAttrLastNetworkingStatus: nil, netcommAttrLastNetworkID: nil, netcommAttrLastConnectErrorValue: nil,
	} {
		if v, ok := w.MatterRead(attr); !ok || v != want {
			t.Errorf("attribute 0x%04X = %v, want %v", attr, v, want)
		}
	}
	if v, ok := w.MatterRead(netcommdef.AttrSupportedWiFiBands); !ok {
		t.Fatal("no SupportedWiFiBands")
	} else {
		enc := tlv.NewEncoder()
		v.(spec.Encodable).EncodeTLV(enc, tlv.AnonymousTag())
		if b, _ := enc.Bytes(); !bytes.Equal(b, []byte{0x16, 0x04, 0x00, 0x18}) {
			t.Fatalf("SupportedWiFiBands %x", b)
		}
	}
	if _, ok := w.MatterRead(0x7777); ok {
		t.Fatal("unknown attribute read")
	}
	for _, id := range []uint32{netcommAttrScanMaxTimeSeconds, netcommAttrConnectMaxTimeSeconds, netcommdef.AttrSupportedWiFiBands} {
		if !wifiNetcommInst.Serves(id) {
			t.Errorf("Wi-Fi attribute 0x%04X not served", id)
		}
	}
	if len(w.MatterAcceptedCommands()) != 5 || len(w.MatterGeneratedCommands()) != 3 {
		t.Fatalf("commands %v / %v", w.MatterAcceptedCommands(), w.MatterGeneratedCommands())
	}
	if w.MinInvokePrivilege(netcommdef.CmdConnectNetwork) != 5 || w.MatterClusterID() != netcommClusterID || w.MatterDataVersion() == 0 || len(w.MatterAttributes()) == 0 || len(w.MatterReportable()) == 0 {
		t.Fatal("privileges or identity")
	}
	if err := w.MatterWrite(context.Background(), netcommAttrInterfaceEnabled, false); wifiStatusOf(t, err) != im.StatusInvalidAction {
		t.Fatalf("InterfaceEnabled false without a switch: %v, want INVALID_ACTION", err)
	}
	if err := w.MatterWrite(context.Background(), netcommAttrInterfaceEnabled, 1); err == nil {
		t.Fatal("a non-bool write accepted")
	}
	if err := w.MatterWrite(context.Background(), netcommAttrMaxNetworks, uint8(2)); wifiStatusOf(t, err) != im.StatusUnsupportedWrite {
		t.Fatalf("MaxNetworks written: %v, want UNSUPPORTED_WRITE", err)
	}
	// A refusal the definition does not judge is still the server's
	// "read-only" (G2-10): UNSUPPORTED_WRITE, never FAILURE.
	if got := (refusedWrite{sentinel: errors.New("core: read-only")}).MatterStatusCode(); got != im.StatusUnsupportedWrite {
		t.Fatalf("an unjudged refusal answers %v, want UNSUPPORTED_WRITE", got)
	}
	if w.MinReadPrivilege(netcommAttrNetworks) != 5 || w.MinWritePrivilege(netcommAttrInterfaceEnabled) != 5 {
		t.Fatal("attribute privileges")
	}
}

// TestWiFiInterfaceEnabledDataVersionAdvancesOnlyOnAChange: a write or a
// restore of InterfaceEnabled that leaves the value as it is leaves the
// DataVersion; one that changes it advances it once. Mirrors matter.js
// packages/node/src/behavior/state/managed/Datasource.ts:1456-1457 and
// :1476-1480 at db021da0 (#noteIfChanged returns for an equal value; the
// version advances only when the commit changed something), as the
// Ethernet server does.
func TestWiFiInterfaceEnabledDataVersionAdvancesOnlyOnAChange(t *testing.T) {
	t.Parallel()
	w, _ := newSwitchingWiFiNC(t)
	before := w.MatterDataVersion()
	if err := w.MatterWrite(t.Context(), netcommAttrInterfaceEnabled, true); err != nil {
		t.Fatal(err)
	}
	if err := w.SetAttributes(map[uint32]any{netcommAttrInterfaceEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if got := w.MatterDataVersion(); got != before {
		t.Errorf("an unchanged InterfaceEnabled moved the DataVersion %d -> %d", before, got)
	}
	if err := w.MatterWrite(t.Context(), netcommAttrInterfaceEnabled, false); err != nil {
		t.Fatal(err)
	}
	if got := w.MatterDataVersion(); got != before+1 {
		t.Errorf("a changed InterfaceEnabled moved the DataVersion %d -> %d, want +1", before, got)
	}
}

// switchDriver is a simDriver that can switch its interface
// (contract.WiFiInterfaceSwitcher): it records each switch and answers
// err, leaving the interface as it was then.
type switchDriver struct {
	*simDriver
	switchMu sync.Mutex
	switches []bool
	err      error
}

func (d *switchDriver) SetInterfaceEnabled(_ context.Context, enabled bool) error {
	d.switchMu.Lock()
	defer d.switchMu.Unlock()
	d.switches = append(d.switches, enabled)
	return d.err
}

func (d *switchDriver) switched() []bool {
	d.switchMu.Lock()
	defer d.switchMu.Unlock()
	return slices.Clone(d.switches)
}

func (d *switchDriver) answer(err error) {
	d.switchMu.Lock()
	defer d.switchMu.Unlock()
	d.err = err
}

func newSwitchingWiFiNC(t *testing.T) (*WiFiNetworkCommissioning, *switchDriver) {
	t.Helper()
	d := &switchDriver{simDriver: &simDriver{}}
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, Clock: clocktest.New(time.Unix(0, 0))})
	if err != nil {
		t.Fatal(err)
	}
	return w, d
}

// TestWiFiInterfaceEnabledWithoutASwitch: a driver that cannot switch its
// interface leaves InterfaceEnabled true — a write of false answers
// INVALID_ACTION, a write of true succeeds and changes nothing, and a
// stored false restored after a restart is not served. Mirrors
// connectedhomeip src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp:1120-1132
// at 6170af84 (WriteAttribute: CHIP_ERROR_UNSUPPORTED_CHIP_FEATURE from
// the driver's SetEnabled answers InvalidAction, citing spec 11.9.6.5) and
// src/include/platform/NetworkCommissioning.h:225-234 (the base driver's
// SetEnabled returns that error, its GetEnabled true). matter.js's server
// is the generated stub (NetworkCommissioningServer.ts), a plain writable
// attribute.
func TestWiFiInterfaceEnabledWithoutASwitch(t *testing.T) {
	t.Parallel()
	armed := false
	var crumbs []uint64
	w, _ := newWiFiNC(t, &armed, &crumbs)
	before := w.MatterDataVersion()
	if err := w.MatterWrite(t.Context(), netcommAttrInterfaceEnabled, false); wifiStatusOf(t, err) != im.StatusInvalidAction {
		t.Fatalf("InterfaceEnabled false: %v, want INVALID_ACTION", err)
	}
	if err := w.MatterWrite(t.Context(), netcommAttrInterfaceEnabled, true); err != nil {
		t.Fatalf("InterfaceEnabled true: %v", err)
	}
	if err := w.SetAttributes(map[uint32]any{netcommAttrInterfaceEnabled: false}); err == nil {
		t.Error("a restored false was taken without a switch")
	}
	if v, _ := w.MatterRead(netcommAttrInterfaceEnabled); v != true {
		t.Errorf("InterfaceEnabled %v, want true", v)
	}
	if got := w.MatterDataVersion(); got != before {
		t.Errorf("DataVersion %d -> %d, want unchanged", before, got)
	}
}

// TestWiFiInterfaceEnabledSwitchesTheInterface: with a driver that
// switches its interface, a write that changes InterfaceEnabled switches
// it and the attribute reads what the driver did — a refusal leaves the
// interface and the attribute as they were, one the driver cannot carry
// out answering INVALID_ACTION and any other FAILURE; a write or restore
// of the value already held reaches the driver no more. Mirrors
// connectedhomeip NetworkCommissioningCluster.cpp:1120-1132 at 6170af84
// (SetInterfaceEnabled is the driver's SetEnabled; its error answers the
// write) and NetworkCommissioningCluster.h:134,144 (the attribute is the
// driver's GetEnabled).
func TestWiFiInterfaceEnabledSwitchesTheInterface(t *testing.T) {
	t.Parallel()
	w, d := newSwitchingWiFiNC(t)
	read := func() any {
		t.Helper()
		v, _ := w.MatterRead(netcommAttrInterfaceEnabled)
		return v
	}
	if err := w.MatterWrite(t.Context(), netcommAttrInterfaceEnabled, true); err != nil {
		t.Fatal(err)
	}
	if got := d.switched(); len(got) != 0 {
		t.Fatalf("an unchanged InterfaceEnabled switched the interface %v", got)
	}

	d.answer(errors.New("rfkill"))
	if err := w.MatterWrite(t.Context(), netcommAttrInterfaceEnabled, false); err == nil || errors.As(err, new(im.StatusCodeError)) {
		t.Fatalf("a switch the driver failed: %v, want FAILURE", err)
	}
	d.answer(fmt.Errorf("not now: %w", errors.ErrUnsupported))
	if err := w.MatterWrite(t.Context(), netcommAttrInterfaceEnabled, false); wifiStatusOf(t, err) != im.StatusInvalidAction {
		t.Fatalf("a switch the driver cannot carry out: %v, want INVALID_ACTION", err)
	}
	if v := read(); v != true {
		t.Fatalf("InterfaceEnabled %v after refused switches, want true", v)
	}

	d.answer(nil)
	before := w.MatterDataVersion()
	if err := w.MatterWrite(t.Context(), netcommAttrInterfaceEnabled, false); err != nil {
		t.Fatal(err)
	}
	if v := read(); v != false || w.MatterDataVersion() != before+1 {
		t.Fatalf("InterfaceEnabled %v, DataVersion %d -> %d after the switch off, want false and +1", v, before, w.MatterDataVersion())
	}
	if err := w.SetAttributes(map[uint32]any{netcommAttrInterfaceEnabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := w.SetAttributes(map[uint32]any{netcommAttrInterfaceEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if v := read(); v != true {
		t.Fatalf("InterfaceEnabled %v after the restored true, want true", v)
	}
	if got, want := d.switched(), []bool{false, false, false, true}; !slices.Equal(got, want) {
		t.Errorf("switches %v, want %v", got, want)
	}
}

// TestWiFiNetworkCommissioningCommands follows a commissioning: add,
// connect, the list, the breadcrumbs, and the refusals of chip's handlers.
func TestWiFiNetworkCommissioningCommands(t *testing.T) {
	ctx := context.Background()
	armed := false
	var crumbs []uint64
	w, d := newWiFiNC(t, &armed, &crumbs)
	add := func(ssid, cred string, b *uint64) (netcommdef.NetworkConfigResponse, error) {
		r, err := w.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte(ssid), Credentials: []byte(cred), Breadcrumb: b})
		resp, _ := r.(netcommdef.NetworkConfigResponse)
		return resp, err
	}
	// No failsafe: FailsafeRequired (:155-162).
	if _, err := add("lab", "secret12", nil); wifiStatusOf(t, err) != im.StatusFailsafeRequired {
		t.Fatal("added without a failsafe")
	}
	armed = true
	// The SSID and credential rules (:383-445).
	for _, c := range [][2]string{{"", "secret12"}, {strings.Repeat("s", 33), "secret12"}, {"lab", "1234"}, {"lab", strings.Repeat("x", 64)}} {
		if _, err := add(c[0], c[1], nil); wifiStatusOf(t, err) != im.StatusConstraintError {
			t.Errorf("add(%q, %q) not refused", c[0], c[1])
		}
	}
	if r, err := add("lab", "secret12", bc(5)); err != nil || r.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess || r.NetworkIndex == nil || *r.NetworkIndex != 0 {
		t.Fatalf("add = %+v, %v", r, err)
	}
	// Open, WEP-64 and a 64-hex PSK are valid lengths; an update of the
	// same SSID keeps its index.
	if r, _ := add("lab", strings.Repeat("a1", 32), nil); r.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess || *r.NetworkIndex != 0 {
		t.Fatalf("update = %+v", r)
	}
	// A second SSID on a one-network list: BoundsExceeded (driver :180).
	if r, _ := add("other", "", nil); r.NetworkingStatus != netcommdef.NetworkCommissioningStatusBoundsExceeded || r.NetworkIndex != nil {
		t.Fatalf("second network = %+v", r)
	}
	// ConnectNetwork: an unknown network, then the network.
	r, err := w.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("nope")})
	if cr := r.(netcommdef.ConnectNetworkResponse); err != nil || cr.NetworkingStatus != netcommdef.NetworkCommissioningStatusNetworkIdNotFound || cr.ErrorValue.Null {
		t.Fatalf("connect unknown = %+v, %v", r, err)
	}
	r, err = w.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("lab"), Breadcrumb: bc(6)})
	if cr := r.(netcommdef.ConnectNetworkResponse); err != nil || cr.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess || !cr.ErrorValue.Null {
		t.Fatalf("connect = %+v, %v", r, err)
	}
	if v, _ := w.MatterRead(netcommAttrNetworks); len(v.(spec.List[netcommdef.NetworkInfoStruct])) != 1 || !v.(spec.List[netcommdef.NetworkInfoStruct])[0].Connected {
		t.Fatalf("Networks %v", v)
	}
	if v, _ := w.MatterRead(netcommAttrLastNetworkID); !bytes.Equal(v.([]byte), []byte("lab")) {
		t.Fatalf("LastNetworkID %v", v)
	}
	if v, _ := w.MatterRead(netcommAttrLastNetworkingStatus); v != uint8(0) {
		t.Fatalf("LastNetworkingStatus %v", v)
	}
	if len(d.connects) != 1 || string(d.connects[0]) != "lab" {
		t.Fatalf("driver connects %q", d.connects)
	}
	// A refused connect records the error value (OnResult :864-873).
	d.connStatus = uint8(netcommdef.NetworkCommissioningStatusAuthFailure)
	r, _ = w.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("lab")})
	if cr := r.(netcommdef.ConnectNetworkResponse); cr.NetworkingStatus != netcommdef.NetworkCommissioningStatusAuthFailure || cr.ErrorValue.Value != -7 {
		t.Fatalf("refused connect %+v", cr)
	}
	if v, _ := w.MatterRead(netcommAttrLastConnectErrorValue); v != int32(-7) {
		t.Fatalf("LastConnectErrorValue %v", v)
	}
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: make([]byte, 33)}); wifiStatusOf(t, err) != im.StatusConstraintError {
		t.Fatal("overlong network id")
	}
	// Reorder (:712-730): unknown, out of range, in place.
	for _, c := range []struct {
		id   string
		idx  uint8
		want netcommdef.NetworkCommissioningStatusEnum
	}{{"nope", 0, netcommdef.NetworkCommissioningStatusNetworkIdNotFound}, {"lab", 1, netcommdef.NetworkCommissioningStatusOutOfRange}, {"lab", 0, netcommdef.NetworkCommissioningStatusSuccess}} {
		r, _ := w.MatterInvoke(ctx, netcommdef.CmdReorderNetwork, netcommdef.ReorderNetworkRequest{NetworkId: []byte(c.id), NetworkIndex: c.idx, Breadcrumb: bc(7)})
		if r.(netcommdef.NetworkConfigResponse).NetworkingStatus != c.want {
			t.Errorf("reorder(%s, %d) = %+v", c.id, c.idx, r)
		}
	}
	// The failsafe expires before CommissioningComplete: the list reverts.
	w.OnFailSafeExpired()
	if v, _ := w.MatterRead(netcommAttrNetworks); len(v.(spec.List[netcommdef.NetworkInfoStruct])) != 0 {
		t.Fatalf("Networks after the revert %v", v)
	}
	if v, _ := w.MatterRead(netcommAttrLastNetworkingStatus); v != nil {
		t.Fatalf("LastNetworkingStatus after the revert %v", v)
	}
	// Committed by CommissioningComplete, a revert keeps it; Remove empties it.
	_, _ = add("lab", "secret12", nil)
	w.OnCommissioningComplete(t.Context())
	w.OnFailSafeExpired()
	if v, _ := w.MatterRead(netcommAttrNetworks); len(v.(spec.List[netcommdef.NetworkInfoStruct])) != 1 {
		t.Fatalf("committed Networks %v", v)
	}
	r, _ = w.MatterInvoke(ctx, netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("nope")})
	if r.(netcommdef.NetworkConfigResponse).NetworkingStatus != netcommdef.NetworkCommissioningStatusNetworkIdNotFound {
		t.Fatalf("remove unknown %+v", r)
	}
	r, _ = w.MatterInvoke(ctx, netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("lab"), Breadcrumb: bc(8)})
	if rr := r.(netcommdef.NetworkConfigResponse); rr.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess || *rr.NetworkIndex != 0 {
		t.Fatalf("remove %+v", r)
	}
	armed = false
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("lab")}); wifiStatusOf(t, err) != im.StatusFailsafeRequired {
		t.Fatal("removed without a failsafe")
	}
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("lab")}); wifiStatusOf(t, err) != im.StatusFailsafeRequired {
		t.Fatal("connected without a failsafe")
	}
	if got := crumbs; len(got) != 4 || got[0] != 5 || got[1] != 6 || got[2] != 7 || got[3] != 8 {
		t.Fatalf("breadcrumbs %v", got)
	}
	// Wrong field types and unknown commands.
	for _, cmd := range []uint32{netcommdef.CmdScanNetworks, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.CmdRemoveNetwork, netcommdef.CmdConnectNetwork, netcommdef.CmdReorderNetwork} {
		if _, err := w.MatterInvoke(ctx, cmd, nil); wifiStatusOf(t, err) != im.StatusInvalidCommand {
			t.Errorf("command 0x%02X with no fields", cmd)
		}
	}
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateThreadNetwork, nil); !errors.Is(err, im.ErrUnsupportedCommand) {
		t.Fatalf("Thread on a Wi-Fi interface: %v", err)
	}
}

// TestWiFiNetworkCommissioningScan: HandleScanNetworks (:309-371) and
// OnFinished (:930-961).
func TestWiFiNetworkCommissioningScan(t *testing.T) {
	ctx := context.Background()
	armed := false
	var crumbs []uint64
	w, d := newWiFiNC(t, &armed, &crumbs)
	scan := func(ssid []byte, b *uint64) netcommdef.ScanNetworksResponse {
		t.Helper()
		req := netcommdef.ScanNetworksRequest{Breadcrumb: b}
		if ssid != nil {
			req.Ssid = &spec.Nullable[[]byte]{Value: ssid}
		}
		r, err := w.MatterInvoke(ctx, netcommdef.CmdScanNetworks, req)
		if err != nil {
			t.Fatal(err)
		}
		return r.(netcommdef.ScanNetworksResponse)
	}
	// A directed scan without results is NetworkNotFound.
	if r := scan([]byte("lab"), bc(1)); r.NetworkingStatus != netcommdef.NetworkCommissioningStatusNetworkNotFound {
		t.Fatalf("directed scan %+v", r)
	}
	if v, _ := w.MatterRead(netcommAttrLastNetworkingStatus); v != uint8(netcommdef.NetworkCommissioningStatusNetworkNotFound) {
		t.Fatalf("LastNetworkingStatus %v", v)
	}
	d.results = []contract.WiFiScanResult{{SSID: []byte("lab"), BSSID: make([]byte, 6), Channel: 6, HasBand: true, HasRSSI: true, RSSI: -40}}
	r := scan([]byte("lab"), bc(2))
	if r.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess || r.WiFiScanResults == nil || len(*r.WiFiScanResults) != 1 || (*r.WiFiScanResults)[0].Rssi == nil {
		t.Fatalf("scan %+v", r)
	}
	if r := scan(nil, nil); r.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess {
		t.Fatalf("undirected scan %+v", r)
	}
	if len(crumbs) != 1 || crumbs[0] != 2 {
		t.Fatalf("breadcrumbs %v", crumbs)
	}
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdScanNetworks, netcommdef.ScanNetworksRequest{Ssid: &spec.Nullable[[]byte]{Value: make([]byte, 33)}}); wifiStatusOf(t, err) != im.StatusConstraintError {
		t.Fatal("overlong SSID scanned")
	}
	if v, _ := w.MatterRead(netcommAttrLastNetworkingStatus); v != uint8(netcommdef.NetworkCommissioningStatusUnknownError) {
		t.Fatalf("LastNetworkingStatus after an overlong SSID %v", v)
	}
	// A command while a scan runs is Busy.
	w.mu.Lock()
	w.busy = true
	w.mu.Unlock()
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdScanNetworks, netcommdef.ScanNetworksRequest{}); wifiStatusOf(t, err) != im.StatusBusy {
		t.Fatal("not busy")
	}
}

// TestWiFiNetworkCommissioningFailSafeOfAnotherFabric: the network commands
// need the fail-safe window of their accessing fabric, as connectedhomeip's
// EnsureFailsafeIsArmed asks IsFailSafeArmed(GetAccessingFabricIndex())
// (NetworkCommissioningCluster.cpp:155-172, :380, :568, :609 at 6170af84;
// by_design BD-chip-FailSafeOfTheAccessingFabric). A window another fabric
// armed is FAILSAFE_REQUIRED.
func TestWiFiNetworkCommissioningFailSafeOfAnotherFabric(t *testing.T) {
	w, _ := newWiFiNC(t, new(bool), new([]uint64))
	w.failSafe = func() (uint8, bool) { return 2, true }
	add := netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("home"), Credentials: []byte("secret12")}
	for _, cmd := range []struct {
		name string
		id   uint32
		req  any
	}{
		{"AddOrUpdateWiFiNetwork", netcommdef.CmdAddOrUpdateWiFiNetwork, add},
		{"RemoveNetwork", netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("home")}},
		{"ConnectNetwork", netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("home")}},
	} {
		other := im.WithFabricFilter(context.Background(), false, 1)
		if _, err := w.MatterInvoke(other, cmd.id, cmd.req); wifiStatusOf(t, err) != im.StatusFailsafeRequired {
			t.Errorf("%s under fabric 2's window from fabric 1 = %v, want FAILSAFE_REQUIRED", cmd.name, err)
		}
	}
	own := im.WithFabricFilter(context.Background(), false, 2)
	resp, err := w.MatterInvoke(own, netcommdef.CmdAddOrUpdateWiFiNetwork, add)
	if err != nil {
		t.Fatalf("AddOrUpdateWiFiNetwork from the arming fabric: %v", err)
	}
	if st := resp.(netcommdef.NetworkConfigResponse).NetworkingStatus; st != netcommdef.NetworkCommissioningStatusSuccess {
		t.Errorf("AddOrUpdateWiFiNetwork from the arming fabric = %v", st)
	}
	// Neither port wired: disarmed.
	bare, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: &simDriver{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.MatterInvoke(own, netcommdef.CmdAddOrUpdateWiFiNetwork, add); wifiStatusOf(t, err) != im.StatusFailsafeRequired {
		t.Errorf("no port = %v, want FAILSAFE_REQUIRED", err)
	}
}
