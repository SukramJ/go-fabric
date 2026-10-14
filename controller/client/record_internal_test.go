// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

func fullRecord() Record {
	at := time.UnixMilli(1_700_000_000_123)
	yes := false
	return Record{
		PeerAddress:           &handshake.PeerAddress{FabricIndex: 2, NodeID: 0x4323},
		CaseAuthenticatedTags: []uint32{0x0001_0001},
		Addresses: []NetworkAddress{
			{IP: netip.MustParseAddr("fe80::1%eth0"), Port: 5540, TTL: 120 * time.Second, DiscoveredAt: at},
			{IP: netip.MustParseAddr("192.0.2.7"), Port: 5541},
		},
		Hostname: "host", DiscoveredAt: at, CommissionedAt: at.Add(time.Second), FabricIndexOnPeer: 3, TTL: 90 * time.Second,
		DeviceIdentifier: "ABCD", Discriminator: ptr(uint16(3840)), CommissioningMode: 2,
		VendorID: ptr(uint16(0xFFF1)), ProductID: ptr(uint16(0x8001)), DeviceType: ptr(uint32(0x10A)), DeviceName: "plug",
		RotatingIdentifier: "RI", PairingHint: ptr(uint32(33)), PairingInstructions: "press",
		ReportedSessionParameters: &sessionparams.Parameters{SessionIdleInterval: 500, SessionActiveInterval: 300, DataModelRevision: 19, SpecificationVersion: 0x01040200},
		AdvertisedIntervals:       &AdvertisedIntervals{IdleInterval: ptr(500 * time.Millisecond), ActiveThreshold: ptr(4 * time.Second)},
		TCPUnsupported:            true,
		TCPSupport:                &discovery.SupportedTransports{TCPServer: true},
		LongIdleTimeOperatingMode: &yes,
	}
}

// TestRecordRoundTrip: every field of the record survives its stored form;
// the empty record stores nothing.
func TestRecordRoundTrip(t *testing.T) {
	t.Parallel()
	in := fullRecord()
	values, err := in.encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != recordFields {
		t.Fatalf("%d ids, want %d", len(values), recordFields)
	}
	for id, b := range values {
		if b == nil {
			t.Fatalf("field %d absent", id)
		}
	}
	out, err := decodeRecord(values)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip\n got %+v\nwant %+v", out, in)
	}
	empty, err := Record{}.encode()
	if err != nil {
		t.Fatal(err)
	}
	for id, b := range empty {
		if b != nil {
			t.Fatalf("empty record stores field %d", id)
		}
	}
	// Empty, non-nil lists are kept as such.
	lists, _ := Record{CaseAuthenticatedTags: []uint32{}, Addresses: []NetworkAddress{}}.encode()
	got, err := decodeRecord(lists)
	if err != nil || got.CaseAuthenticatedTags == nil || got.Addresses == nil {
		t.Fatalf("empty lists %+v %v", got, err)
	}
	// A clone shares nothing.
	c := in.clone()
	*c.PeerAddress = handshake.PeerAddress{}
	c.Addresses[0].Port = 1
	*c.AdvertisedIntervals.IdleInterval = 0
	if in.PeerAddress.NodeID != 0x4323 || in.Addresses[0].Port != 5540 || *in.AdvertisedIntervals.IdleInterval != 500*time.Millisecond {
		t.Fatal("clone shares state")
	}
}

// TestRecordDecodeErrors: a malformed field fails the load.
func TestRecordDecodeErrors(t *testing.T) {
	t.Parallel()
	good, _ := fullRecord().encode()
	for id := range uint32(recordFields) {
		values := map[uint32][]byte{id: good[recDeviceIdentifier]}
		if id == recDeviceIdentifier || id == recHostname || id == recDeviceName || id == recRotatingIdentifier || id == recPairingInstructions {
			values[id] = good[recFabricIndexOnPeer]
		}
		if _, err := decodeRecord(values); err == nil {
			t.Errorf("field %d: a value of the wrong type decoded", id)
		}
	}
	if _, err := decodeRecord(map[uint32][]byte{recAddresses: {0xFF}}); err == nil {
		t.Error("an unreadable value decoded")
	}
	bad, _ := Record{Addresses: []NetworkAddress{{}}}.encode()
	if _, err := decodeRecord(bad); err == nil {
		t.Error("an invalid IP decoded")
	}
}

// TestRecordApplyPeer: CommissioningClient.#update — the peer's operational
// address replaces the addresses as the one address; its CATs and reported
// parameters are taken; its discovery data replaces the TXT fields.
func TestRecordApplyPeer(t *testing.T) {
	t.Parallel()
	r := fullRecord()
	dn := "renamed"
	r.applyPeer(peer.Descriptor{
		OperationalAddress:        &net.UDPAddr{IP: net.ParseIP("fd00::9"), Port: 5550},
		CaseAuthenticatedTags:     []uint32{7},
		ReportedSessionParameters: &sessionparams.Parameters{SessionIdleInterval: 1},
		DiscoveryData:             &discovery.TXTData{DN: &dn},
	})
	if len(r.Addresses) != 1 || r.Addresses[0].String() != "[fd00::9]:5550" || !r.Addresses[0].DiscoveredAt.IsZero() {
		t.Fatalf("addresses %+v", r.Addresses)
	}
	if len(r.CaseAuthenticatedTags) != 1 || r.ReportedSessionParameters.SessionIdleInterval != 1 || r.TCPUnsupported {
		t.Fatalf("record %+v", r)
	}
	// The TXT keys a record announces replace the record's; VP absent
	// keeps the ids (RemoteDescriptor.ts:214-234).
	if r.DeviceName != "renamed" || r.DeviceType != nil || r.PairingHint != nil || r.AdvertisedIntervals != nil || r.VendorID == nil {
		t.Fatalf("TXT fields %+v", r)
	}
	// Without an operational address or discovery data the addresses and
	// TXT fields stay; the reported parameters stay without new ones.
	r.applyPeer(peer.Descriptor{})
	if len(r.Addresses) != 1 || r.DeviceName != "renamed" || r.ReportedSessionParameters == nil || r.CaseAuthenticatedTags != nil {
		t.Fatalf("after an empty descriptor %+v", r)
	}
}

// TestRecordDevice: the record as discovery data and back.
func TestRecordDevice(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_800_000_000_000)
	vp, dn, sii, icd := "65521+0", "lamp", 400*time.Millisecond, uint8(1)
	d := &discovery.Device{
		TXTData:    discovery.TXTData{VP: &vp, DN: &dn, SII: &sii, ICD: &icd},
		InstanceID: "CAFE", D: 3840, CM: 1, Hostname: "lamp-host",
		Addresses: []discovery.Address{{IP: netip.MustParseAddr("fd00::2"), Port: 5540}},
	}
	var r Record
	r.applyDevice(d, now)
	if r.DeviceIdentifier != "CAFE" || r.Hostname != "lamp-host" || *r.Discriminator != 3840 || r.CommissioningMode != 1 ||
		!r.DiscoveredAt.Equal(now) || r.TTL != 0 || *r.VendorID != 65521 || r.ProductID != nil || r.DeviceName != "lamp" ||
		*r.AdvertisedIntervals.IdleInterval != sii || !*r.LongIdleTimeOperatingMode {
		t.Fatalf("record %+v", r)
	}
	if len(r.Addresses) != 1 || !r.Addresses[0].DiscoveredAt.Equal(now) || r.Addresses[0].TTL != 0 {
		t.Fatalf("addresses %+v", r.Addresses)
	}
	// A device without addresses keeps the known ones (:199-201).
	r.applyDevice(&discovery.Device{InstanceID: "CAFE"}, now.Add(time.Minute))
	if len(r.Addresses) != 1 || !r.DiscoveredAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("after a device without addresses %+v", r)
	}
	back := fullRecord()
	back.PeerAddress = nil
	dev := back.device()
	if dev.InstanceID != "ABCD" || dev.D != 3840 || dev.CM != 2 || *dev.VP != "65521+32769" || *dev.DT != 0x10A || *dev.PH != 33 || *dev.PI != "press" ||
		*dev.SII != 500*time.Millisecond || dev.SAI != nil || !dev.T.TCPServer || *dev.ICD != 0 || len(dev.Addresses) != 2 || dev.Hostname != "host" {
		t.Fatalf("device %+v", dev)
	}
	// An operational record carries no commissionable keys (:145-162).
	if op := fullRecord().device(); op.D != 0 || op.CM != 0 {
		t.Fatalf("operational device %+v", op)
	}
	kd := fullRecord().knownDescriptor()
	if kd.Address.NodeID != 0x4323 || kd.OperationalAddress.Port != 5540 || kd.CaseAuthenticatedTags[0] != 0x0001_0001 || kd.DiscoveryData == nil {
		t.Fatalf("known descriptor %+v", kd)
	}
}

// TestNodeRecord: a node loads its record from the store and keeps it out
// of the image; updateRecord saves it and logs the address changes.
func TestNodeRecord(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	values, _ := fullRecord().encode()
	_ = st.Save("peer1", 0, recordCluster, values, nil)
	logs := &logRecorder{}
	n, err := New(Config{ID: "peer1", Store: st, Interaction: interaction.Bind(nil, interaction.Config{}), Logger: slog.New(logs)})
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := n.PeerAddress(); !ok || a.NodeID != 0x4323 || !n.IsCommissioned() || n.Record().Hostname != "host" {
		t.Fatalf("record %+v", n.Record())
	}
	if root := n.Endpoint(0); root == nil || root.Cluster(recordCluster) != nil {
		t.Fatal("the record is in the image")
	}
	n.updateRecord(func(r *Record) { r.Addresses = r.Addresses[:1] })
	n.updateRecord(func(r *Record) { r.Addresses = nil })
	n.updateRecord(func(r *Record) { r.Addresses = []NetworkAddress{{IP: netip.MustParseAddr("fd00::3"), Port: 1}} })
	n.updateRecord(func(r *Record) { r.PeerAddress = nil })
	for _, want := range []string{"operational_address_changed", "operational_address_cleared", "operational_address_set"} {
		if !logs.has("matter.controller.client." + want) {
			t.Errorf("no %s", want)
		}
	}
	again, err := New(Config{ID: "peer1", Store: st, Interaction: interaction.Bind(nil, interaction.Config{})})
	if err != nil {
		t.Fatal(err)
	}
	if again.IsCommissioned() || len(again.Record().Addresses) != 1 {
		t.Fatalf("reloaded record %+v", again.Record())
	}
	// A malformed stored record fails the load.
	_ = st.Save("bad", 0, recordCluster, map[uint32][]byte{recHostname: {0xFF}}, nil)
	if _, err := New(Config{ID: "bad", Store: st, Interaction: interaction.Bind(nil, interaction.Config{})}); err == nil {
		t.Fatal("a malformed record loaded")
	}
	if err := n.Erase(t.Context()); err != nil || n.IsCommissioned() || n.Record().Hostname != "" {
		t.Fatalf("Erase kept the record: %v", err)
	}
}
