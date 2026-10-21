// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/tlv"
)

// The node's commissioning record — matter.js CommissioningClient.State's
// nonvolatile fields (behavior/system/commissioning/CommissioningClient.ts:927-1091
// at 0ceb202d) — is stored with the image like the settings, on endpoint
// 0 under a second cluster id no cluster has, one attribute id per field.
const (
	recordCluster uint32 = 0xFFFF_FFFE

	recPeerAddress               uint32 = 0  // :931-932
	recCaseAuthenticatedTags     uint32 = 1  // :939-940
	recAddresses                 uint32 = 2  // :946-947
	recHostname                  uint32 = 3  // :958-959
	recDiscoveredAt              uint32 = 4  // :964-965
	recCommissionedAt            uint32 = 5  // :982-983
	recFabricIndexOnPeer         uint32 = 6  // :992-993
	recTTL                       uint32 = 7  // :998-999
	recDeviceIdentifier          uint32 = 8  // :1004-1005
	recDiscriminator             uint32 = 9  // :1010-1011
	recCommissioningMode         uint32 = 10 // :1016-1017
	recVendorID                  uint32 = 11 // :1022-1023
	recProductID                 uint32 = 12 // :1028-1029
	recDeviceType                uint32 = 13 // :1034-1035
	recDeviceName                uint32 = 14 // :1040-1041
	recReportedSessionParameters uint32 = 15 // :1064-1065
	recTCPUnsupported            uint32 = 16 // :1077-1078
	recRotatingIdentifier        uint32 = 17 // :1046-1047
	recPairingHint               uint32 = 18 // :1052-1053
	recPairingInstructions       uint32 = 19 // :1058-1059
	recAdvertisedIntervals       uint32 = 20 // :1070-1071
	recTCPSupport                uint32 = 21 // :1083-1084
	recLongIdleTimeOperatingMode uint32 = 22 // :1089-1090
	recordFields                        = 23
)

// Record is a node's commissioning record: who the node is on our fabric,
// where it was found and what it announced. It is matter.js
// CommissioningClient.State's nonvolatile part (CommissioningClient.ts:927-1091);
// onlineAt and offlineAt (:967-977) are not persisted there and not kept
// here. A zero value is unknown.
type Record struct {
	// PeerAddress is our fabric index and the node's node id; nil while
	// the node is not commissioned (:928-932).
	PeerAddress *handshake.PeerAddress
	// CaseAuthenticatedTags are our CATs for CASE sessions to the node,
	// nil when unknown (:934-940).
	CaseAuthenticatedTags []uint32
	// Addresses are the node's known network addresses; nil when the node
	// has not been located (:942-947).
	Addresses []NetworkAddress
	// Hostname is the host the commissionable advertisement's SRV record
	// named (:949-959).
	Hostname string
	// DiscoveredAt is when the node was discovered (:961-965); stored in
	// milliseconds.
	DiscoveredAt time.Time
	// CommissionedAt is when the node was commissioned, zero when it is
	// not (:979-983); stored in milliseconds.
	CommissionedAt time.Time
	// FabricIndexOnPeer is the fabric index the node assigned us, 0 when
	// unknown (:985-993) — not PeerAddress.FabricIndex, which is ours.
	FabricIndexOnPeer uint8
	// TTL is the discovery record's time to live, 0 when unknown
	// (:995-999); stored in seconds. An unknown TTL expires after
	// [DefaultTTL] (Peers.ts:959-966).
	TTL time.Duration
	// DeviceIdentifier is the commissionable instance id (:1001-1005).
	DeviceIdentifier string
	// Discriminator is the long discriminator, nil when unknown
	// (:1007-1011).
	Discriminator *uint16
	// CommissioningMode is the last known commissioning mode (:1013-1017).
	CommissioningMode uint8
	// VendorID and ProductID are the announced ids, nil when unknown or
	// announced as 0 (:1019-1029, RemoteDescriptor.ts:214-221).
	VendorID, ProductID *uint16
	// DeviceType is the announced device type, nil when unknown
	// (:1031-1035).
	DeviceType *uint32
	// DeviceName is the announced device name (:1037-1041).
	DeviceName string
	// RotatingIdentifier is the announced rotating id (:1043-1047).
	RotatingIdentifier string
	// PairingHint is the announced pairing hint, nil when unknown
	// (:1049-1053).
	PairingHint *uint32
	// PairingInstructions are the announced pairing instructions
	// (:1055-1059).
	PairingInstructions string
	// ReportedSessionParameters are the session parameters the node
	// reported in its most recent session, nil when none (:1061-1065).
	ReportedSessionParameters *sessionparams.Parameters
	// AdvertisedIntervals are the SII, SAI and SAT the node announced, nil
	// when it announced none (:1067-1071).
	AdvertisedIntervals *AdvertisedIntervals
	// TCPUnsupported is set when a TCP session reported no TCP server
	// support (:1073-1078); this module has no TCP client and never sets
	// it, but keeps what a store holds.
	TCPUnsupported bool
	// TCPSupport is the announced transports bitmap (T), nil when unknown
	// (:1080-1084).
	TCPSupport *discovery.SupportedTransports
	// LongIdleTimeOperatingMode is the announced ICD mode, nil when
	// unknown (:1086-1090).
	LongIdleTimeOperatingMode *bool
}

// AdvertisedIntervals are the session intervals a node announces in DNS-SD
// — CommissioningClient.AdvertisedIntervals (CommissioningClient.ts:836-845).
// A nil field was not announced.
type AdvertisedIntervals struct {
	IdleInterval, ActiveInterval, ActiveThreshold *time.Duration
}

// NetworkAddress is one network address of a node with its lifespan —
// CommissioningClient.NetworkAddress (CommissioningClient.ts:883-920) for
// UDP, the only transport here.
type NetworkAddress struct {
	IP   netip.Addr
	Port uint16
	// TTL is the address record's time to live, 0 when unknown: it then
	// expires after [DefaultTTL] (Peers.ts:959-966).
	TTL time.Duration
	// DiscoveredAt is when the address was discovered; zero never expires
	// (Peers.ts:960-965).
	DiscoveredAt time.Time
}

// UDPAddr is the address as a *net.UDPAddr.
func (a NetworkAddress) UDPAddr() *net.UDPAddr {
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(a.IP, a.Port))
}

// String formats the address as host:port, IPv6 in brackets.
func (a NetworkAddress) String() string { return netip.AddrPortFrom(a.IP, a.Port).String() }

// clone deep-copies r.
func (r Record) clone() Record {
	c := r
	if r.PeerAddress != nil {
		a := *r.PeerAddress
		c.PeerAddress = &a
	}
	c.CaseAuthenticatedTags = slices.Clone(r.CaseAuthenticatedTags)
	c.Addresses = slices.Clone(r.Addresses)
	c.Discriminator = clonePtr(r.Discriminator)
	c.VendorID, c.ProductID = clonePtr(r.VendorID), clonePtr(r.ProductID)
	c.DeviceType = clonePtr(r.DeviceType)
	c.PairingHint = clonePtr(r.PairingHint)
	c.ReportedSessionParameters = clonePtr(r.ReportedSessionParameters)
	c.TCPSupport = clonePtr(r.TCPSupport)
	c.LongIdleTimeOperatingMode = clonePtr(r.LongIdleTimeOperatingMode)
	if r.AdvertisedIntervals != nil {
		ai := AdvertisedIntervals{
			IdleInterval:    clonePtr(r.AdvertisedIntervals.IdleInterval),
			ActiveInterval:  clonePtr(r.AdvertisedIntervals.ActiveInterval),
			ActiveThreshold: clonePtr(r.AdvertisedIntervals.ActiveThreshold),
		}
		c.AdvertisedIntervals = &ai
	}
	return c
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// encode is the record's stored form: every field's id, nil for an
// absent one so a Save deletes what the record no longer holds.
func (r Record) encode() (map[uint32][]byte, error) {
	out := make(map[uint32][]byte, recordFields)
	for id := range uint32(recordFields) {
		out[id] = nil
	}
	put := func(id uint32, f func(e *tlv.Encoder)) error {
		e := tlv.NewEncoder()
		f(e)
		b, err := e.Bytes()
		if err != nil {
			return fmt.Errorf("record field %d: %w", id, err)
		}
		out[id] = b
		return nil
	}
	anon := tlv.AnonymousTag()
	var puts []func() error
	add := func(cond bool, id uint32, f func(e *tlv.Encoder)) {
		if cond {
			puts = append(puts, func() error { return put(id, f) })
		}
	}
	add(r.PeerAddress != nil, recPeerAddress, func(e *tlv.Encoder) {
		e.StartStruct(anon)
		e.PutUint(tlv.ContextTag(0), uint64(r.PeerAddress.FabricIndex))
		e.PutUint(tlv.ContextTag(1), r.PeerAddress.NodeID)
		_ = e.EndContainer()
	})
	add(r.CaseAuthenticatedTags != nil, recCaseAuthenticatedTags, func(e *tlv.Encoder) {
		e.StartArray(anon)
		for _, c := range r.CaseAuthenticatedTags {
			e.PutUint(anon, uint64(c))
		}
		_ = e.EndContainer()
	})
	add(r.Addresses != nil, recAddresses, func(e *tlv.Encoder) {
		e.StartArray(anon)
		for _, a := range r.Addresses {
			e.StartStruct(anon)
			e.PutUTF8(tlv.ContextTag(0), a.IP.String())
			e.PutUint(tlv.ContextTag(1), uint64(a.Port))
			if a.TTL > 0 {
				e.PutUint(tlv.ContextTag(2), uint64(a.TTL/time.Second)) //nolint:gosec // a positive TTL
			}
			if !a.DiscoveredAt.IsZero() {
				e.PutUint(tlv.ContextTag(3), uint64(a.DiscoveredAt.UnixMilli())) //nolint:gosec // a discovery time is after the epoch
			}
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
	})
	str := func(id uint32, s string) {
		add(s != "", id, func(e *tlv.Encoder) { e.PutUTF8(anon, s) })
	}
	millis := func(id uint32, t time.Time) {
		add(!t.IsZero(), id, func(e *tlv.Encoder) { e.PutUint(anon, uint64(t.UnixMilli())) }) //nolint:gosec // a record time is after the epoch
	}
	str(recHostname, r.Hostname)
	millis(recDiscoveredAt, r.DiscoveredAt)
	millis(recCommissionedAt, r.CommissionedAt)
	add(r.FabricIndexOnPeer != 0, recFabricIndexOnPeer, func(e *tlv.Encoder) { e.PutUint(anon, uint64(r.FabricIndexOnPeer)) })
	add(r.TTL > 0, recTTL, func(e *tlv.Encoder) { e.PutUint(anon, uint64(r.TTL/time.Second)) }) //nolint:gosec // a positive TTL
	str(recDeviceIdentifier, r.DeviceIdentifier)
	add(r.Discriminator != nil, recDiscriminator, func(e *tlv.Encoder) { e.PutUint(anon, uint64(*r.Discriminator)) })
	add(r.CommissioningMode != 0, recCommissioningMode, func(e *tlv.Encoder) { e.PutUint(anon, uint64(r.CommissioningMode)) })
	add(r.VendorID != nil, recVendorID, func(e *tlv.Encoder) { e.PutUint(anon, uint64(*r.VendorID)) })
	add(r.ProductID != nil, recProductID, func(e *tlv.Encoder) { e.PutUint(anon, uint64(*r.ProductID)) })
	add(r.DeviceType != nil, recDeviceType, func(e *tlv.Encoder) { e.PutUint(anon, uint64(*r.DeviceType)) })
	str(recDeviceName, r.DeviceName)
	add(r.ReportedSessionParameters != nil, recReportedSessionParameters, func(e *tlv.Encoder) { r.ReportedSessionParameters.Encode(e, anon) })
	add(r.TCPUnsupported, recTCPUnsupported, func(e *tlv.Encoder) { e.PutBool(anon, true) })
	str(recRotatingIdentifier, r.RotatingIdentifier)
	add(r.PairingHint != nil, recPairingHint, func(e *tlv.Encoder) { e.PutUint(anon, uint64(*r.PairingHint)) })
	str(recPairingInstructions, r.PairingInstructions)
	add(r.AdvertisedIntervals != nil, recAdvertisedIntervals, func(e *tlv.Encoder) {
		e.StartStruct(anon)
		for i, d := range []*time.Duration{r.AdvertisedIntervals.IdleInterval, r.AdvertisedIntervals.ActiveInterval, r.AdvertisedIntervals.ActiveThreshold} {
			if d != nil {
				e.PutUint(tlv.ContextTag(uint8(i+1)), uint64(d.Milliseconds())) //nolint:gosec // TXTData bounds the intervals
			}
		}
		_ = e.EndContainer()
	})
	add(r.TCPSupport != nil, recTCPSupport, func(e *tlv.Encoder) { e.PutUint(anon, uint64(encodeTransports(*r.TCPSupport))) })
	add(r.LongIdleTimeOperatingMode != nil, recLongIdleTimeOperatingMode, func(e *tlv.Encoder) { e.PutBool(anon, *r.LongIdleTimeOperatingMode) })
	for _, p := range puts {
		if err := p(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// encodeTransports is SupportedTransportsSchema.encode: bit 1 TCP client,
// bit 2 TCP server (discovery.SupportedTransports).
func encodeTransports(t discovery.SupportedTransports) uint8 {
	var v uint8
	if t.TCPClient {
		v |= 1 << 1
	}
	if t.TCPServer {
		v |= 1 << 2
	}
	return v
}

// decodeRecord reads a stored record; an absent field keeps its zero
// value.
func decodeRecord(values map[uint32][]byte) (Record, error) {
	var r Record
	for id, b := range values {
		if b == nil {
			continue
		}
		if id == recReportedSessionParameters {
			p, err := decodeSessionParameters(b)
			if err != nil {
				return r, fmt.Errorf("record field %d: %w", id, err)
			}
			r.ReportedSessionParameters = &p
			continue
		}
		n, err := decodeValue(b)
		if err != nil {
			return r, fmt.Errorf("record field %d: %w", id, err)
		}
		if err := r.decodeField(id, n); err != nil {
			return r, fmt.Errorf("record field %d: %w", id, err)
		}
	}
	return r, nil
}

// stringField is the record's string field of id, nil for another id.
func (r *Record) stringField(id uint32) *string {
	switch id {
	case recHostname:
		return &r.Hostname
	case recDeviceIdentifier:
		return &r.DeviceIdentifier
	case recDeviceName:
		return &r.DeviceName
	case recRotatingIdentifier:
		return &r.RotatingIdentifier
	case recPairingInstructions:
		return &r.PairingInstructions
	}
	return nil
}

func (r *Record) decodeField(id uint32, n spec.Node) error {
	if dst := r.stringField(id); dst != nil {
		v, err := spec.DecodeString(n)
		*dst = v
		return err
	}
	var err error
	millis := func(dst *time.Time) {
		var ms uint64
		if ms, err = spec.DecodeUint[uint64](64)(n); err == nil {
			*dst = time.UnixMilli(int64(ms)) //nolint:gosec // stored by encode
		}
	}
	switch id {
	case recPeerAddress:
		var a handshake.PeerAddress
		err = spec.DecodeFields(n, "peerAddress",
			spec.Bind(0, "fabricIndex", true, &a.FabricIndex, spec.DecodeUint[uint8](8)),
			spec.Bind(1, "nodeId", true, &a.NodeID, spec.DecodeUint[uint64](64)))
		r.PeerAddress = &a
	case recCaseAuthenticatedTags:
		r.CaseAuthenticatedTags, err = spec.DecodeList(spec.DecodeUint[uint32](32))(n)
		if r.CaseAuthenticatedTags == nil {
			r.CaseAuthenticatedTags = []uint32{}
		}
	case recAddresses:
		r.Addresses, err = spec.DecodeList(decodeNetworkAddress)(n)
		if r.Addresses == nil {
			r.Addresses = []NetworkAddress{}
		}
	case recDiscoveredAt:
		millis(&r.DiscoveredAt)
	case recCommissionedAt:
		millis(&r.CommissionedAt)
	case recFabricIndexOnPeer:
		r.FabricIndexOnPeer, err = spec.DecodeUint[uint8](8)(n)
	case recTTL:
		var s uint64
		s, err = spec.DecodeUint[uint64](32)(n)
		r.TTL = time.Duration(s) * time.Second //nolint:gosec // 32 bits of seconds
	case recDiscriminator:
		r.Discriminator, err = decodePtr(n, spec.DecodeUint[uint16](16))
	case recCommissioningMode:
		r.CommissioningMode, err = spec.DecodeUint[uint8](8)(n)
	case recVendorID:
		r.VendorID, err = decodePtr(n, spec.DecodeUint[uint16](16))
	case recProductID:
		r.ProductID, err = decodePtr(n, spec.DecodeUint[uint16](16))
	case recDeviceType:
		r.DeviceType, err = decodePtr(n, spec.DecodeUint[uint32](32))
	case recTCPUnsupported:
		r.TCPUnsupported, err = spec.DecodeBool(n)
	case recPairingHint:
		r.PairingHint, err = decodePtr(n, spec.DecodeUint[uint32](32))
	case recAdvertisedIntervals:
		var ai AdvertisedIntervals
		err = spec.DecodeFields(n, "advertisedIntervals",
			spec.Bind(1, "idleInterval", false, &ai.IdleInterval, spec.DecodeOptional(decodeMillis)),
			spec.Bind(2, "activeInterval", false, &ai.ActiveInterval, spec.DecodeOptional(decodeMillis)),
			spec.Bind(3, "activeThreshold", false, &ai.ActiveThreshold, spec.DecodeOptional(decodeMillis)))
		r.AdvertisedIntervals = &ai
	case recTCPSupport:
		var v uint8
		v, err = spec.DecodeUint[uint8](8)(n)
		r.TCPSupport = &discovery.SupportedTransports{TCPClient: v&(1<<1) != 0, TCPServer: v&(1<<2) != 0}
	case recLongIdleTimeOperatingMode:
		r.LongIdleTimeOperatingMode, err = decodePtr(n, spec.DecodeBool)
	}
	return err
}

func decodePtr[T any](n spec.Node, d spec.Decoder[T]) (*T, error) {
	v, err := d(n)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func decodeNetworkAddress(n spec.Node) (NetworkAddress, error) {
	var a NetworkAddress
	var ip string
	var ttl, at *uint64
	err := spec.DecodeFields(n, "networkAddress",
		spec.Bind(0, "ip", true, &ip, spec.DecodeString),
		spec.Bind(1, "port", true, &a.Port, spec.DecodeUint[uint16](16)),
		spec.Bind(2, "ttl", false, &ttl, spec.DecodeOptional(spec.DecodeUint[uint64](32))),
		spec.Bind(3, "discoveredAt", false, &at, spec.DecodeOptional(spec.DecodeUint[uint64](64))))
	if err != nil {
		return a, err
	}
	if a.IP, err = netip.ParseAddr(ip); err != nil {
		return a, err
	}
	if ttl != nil {
		a.TTL = time.Duration(*ttl) * time.Second //nolint:gosec // 32 bits of seconds
	}
	if at != nil {
		a.DiscoveredAt = time.UnixMilli(int64(*at)) //nolint:gosec // stored by encode
	}
	return a, nil
}

func decodeSessionParameters(b []byte) (sessionparams.Parameters, error) {
	dec := tlv.NewDecoder(b)
	el, err := dec.Next()
	if err != nil {
		return sessionparams.Parameters{}, err
	}
	if el.Type != tlv.TypeStructure {
		return sessionparams.Parameters{}, fmt.Errorf("session parameters: %v, want a structure", el.Type)
	}
	return sessionparams.DecodeFields(dec)
}

// applyTXT copies the discovery data into the record as matter.js
// RemoteDescriptor.toLongForm does with its short-form keys
// (behavior/system/commissioning/RemoteDescriptor.ts:214-234): the vendor
// and product only when VP is announced, 0 meaning unspecified (:214-221);
// every other key replaced, an absent one clearing what the record held
// (:223-234).
func (r *Record) applyTXT(t discovery.TXTData) {
	if t.VP != nil {
		vendor, product, _ := strings.Cut(*t.VP, "+")
		r.VendorID, r.ProductID = nonZeroID(vendor), nonZeroID(product)
	}
	r.AdvertisedIntervals = advertisable(AdvertisedIntervals{IdleInterval: t.SII, ActiveInterval: t.SAI, ActiveThreshold: t.SAT})
	r.DeviceType = clonePtr(t.DT)
	r.DeviceName = deref(t.DN)
	r.RotatingIdentifier = deref(t.RI)
	r.PairingHint = clonePtr(t.PH)
	r.PairingInstructions = deref(t.PI)
	r.TCPSupport = clonePtr(t.T)
	r.LongIdleTimeOperatingMode = nil
	if t.ICD != nil {
		v := *t.ICD == 1
		r.LongIdleTimeOperatingMode = &v
	}
}

// txt is the record's discovery data in short form —
// RemoteDescriptor.fromLongForm's TXT keys (RemoteDescriptor.ts:91-143).
func (r Record) txt() discovery.TXTData {
	var t discovery.TXTData
	if r.VendorID != nil {
		vp := strconv.Itoa(int(*r.VendorID))
		if r.ProductID != nil {
			vp += "+" + strconv.Itoa(int(*r.ProductID))
		}
		t.VP = &vp
	}
	t.DT = clonePtr(r.DeviceType)
	t.DN = strPtr(r.DeviceName)
	t.RI = strPtr(r.RotatingIdentifier)
	if r.PairingHint != nil {
		t.PH = clonePtr(r.PairingHint)
		t.PI = strPtr(r.PairingInstructions)
	}
	if r.AdvertisedIntervals != nil {
		if ai := advertisable(*r.AdvertisedIntervals); ai != nil {
			t.SII, t.SAI, t.SAT = ai.IdleInterval, ai.ActiveInterval, ai.ActiveThreshold
		}
	}
	t.T = clonePtr(r.TCPSupport)
	if r.LongIdleTimeOperatingMode != nil {
		var icd uint8
		if *r.LongIdleTimeOperatingMode {
			icd = 1
		}
		t.ICD = &icd
	}
	return t
}

// The bounds of an interval DNS-SD advertises (Core § 4.3.4): matter.js
// SessionIntervals.maxAdvertisedInterval (one hour) for SII and SAI,
// maxActiveThreshold (65535 ms) for SAT (SessionIntervals.ts:57, :63).
const (
	maxAdvertisedInterval = time.Hour
	maxActiveThreshold    = 65535 * time.Millisecond
)

// advertisable keeps the intervals a DNS-SD advertisement can carry and
// drops the others, nil when none remains. Mirrors matter.js
// SessionIntervals.advertisable (packages/protocol/src/session/
// SessionIntervals.ts:99-118 at 862ad75f): SII and SAI whole milliseconds
// from 0 to an hour, SAT whole milliseconds from 1 to 65535 ("DiscoveryData
// treats an SAT of 0 as absent, so a stored 0 must not apply either"),
// which RemoteDescriptor applies both into the long form and out of it
// (RemoteDescriptor.ts:119, :223-227).
func advertisable(ai AdvertisedIntervals) *AdvertisedIntervals {
	keep := func(v *time.Duration, lowest, highest time.Duration) *time.Duration {
		if v == nil || *v%time.Millisecond != 0 || *v < lowest || *v > highest {
			return nil
		}
		c := *v
		return &c
	}
	out := AdvertisedIntervals{
		IdleInterval:    keep(ai.IdleInterval, 0, maxAdvertisedInterval),
		ActiveInterval:  keep(ai.ActiveInterval, 0, maxAdvertisedInterval),
		ActiveThreshold: keep(ai.ActiveThreshold, time.Millisecond, maxActiveThreshold),
	}
	if out.IdleInterval == nil && out.ActiveInterval == nil && out.ActiveThreshold == nil {
		return nil
	}
	return &out
}

// device is the record as the commissioner's discovery data —
// RemoteDescriptor.fromLongForm (RemoteDescriptor.ts:52-165): the
// addresses, and the discriminator and commissioning mode only while the
// node is not operational (:145-162).
func (r Record) device() *discovery.Device {
	d := &discovery.Device{TXTData: r.txt(), InstanceID: r.DeviceIdentifier, Hostname: r.Hostname}
	for _, a := range r.Addresses {
		d.Addresses = append(d.Addresses, discovery.Address{IP: a.IP, Port: a.Port})
	}
	if r.PeerAddress == nil {
		if r.Discriminator != nil {
			d.D = *r.Discriminator
		}
		d.CM = r.CommissioningMode
	}
	return d
}

// applyDevice refreshes the record from a commissionable device the
// scanner found — RemoteDescriptor.toLongForm (RemoteDescriptor.ts:167-245)
// over the device: discovered now (:191-193); the TTL unknown, so it is
// [DefaultTTL] at expiry (Peers.ts:959-966) — as in matter.js, whose
// scanner states no TTL on a commissionable device or its addresses
// (CommissionableMdnsScanner.ts:470-496, IpService.ts:316 at 0ceb202d); the
// addresses only when there are any (:199-201), each discovered now; the
// instance id (:203-205), the host (:210-212), the TXT keys (applyTXT),
// the discriminator and commissioning mode (:236-242).
func (r *Record) applyDevice(d *discovery.Device, now time.Time) {
	r.DiscoveredAt = now
	r.TTL = 0
	if len(d.Addresses) > 0 {
		r.Addresses = make([]NetworkAddress, 0, len(d.Addresses))
		for _, a := range d.Addresses {
			r.Addresses = append(r.Addresses, NetworkAddress{IP: a.IP, Port: a.Port, DiscoveredAt: now})
		}
	}
	if d.InstanceID != "" {
		r.DeviceIdentifier = d.InstanceID
	}
	r.Hostname = d.Hostname
	r.applyTXT(d.TXTData)
	disc := d.D
	r.Discriminator = &disc
	r.CommissioningMode = d.CM
}

// applyPeer takes the peer's descriptor — CommissioningClient.#update
// (CommissioningClient.ts:721-741): the reported session parameters when
// the peer has any (:726-728); the TCP flag (:729, the Go descriptor has
// none: false); the discovery data as toLongForm takes it (:735, nothing
// without); the operational address as the one address, as matter.js
// does pending the TODO at :737 (:736-739); the CATs (:740).
func (r *Record) applyPeer(d peer.Descriptor) {
	if d.ReportedSessionParameters != nil {
		p := *d.ReportedSessionParameters
		r.ReportedSessionParameters = &p
	}
	r.TCPUnsupported = false
	if d.DiscoveryData != nil {
		r.applyTXT(*d.DiscoveryData)
	}
	if a := d.OperationalAddress; a != nil {
		ap := a.AddrPort()
		r.Addresses = []NetworkAddress{{IP: ap.Addr(), Port: ap.Port()}}
	}
	r.CaseAuthenticatedTags = slices.Clone(d.CaseAuthenticatedTags)
}

// knownDescriptor is the protocol peer the record describes —
// CommissioningClient.#bindPeer's addKnownPeer (CommissioningClient.ts:699-706):
// the first address as the operational one, the discovery data, the CATs,
// the reported session parameters.
func (r Record) knownDescriptor() peer.Descriptor {
	d := peer.Descriptor{CaseAuthenticatedTags: slices.Clone(r.CaseAuthenticatedTags), ReportedSessionParameters: clonePtr(r.ReportedSessionParameters)}
	if r.PeerAddress != nil {
		d.Address = *r.PeerAddress
	}
	if len(r.Addresses) > 0 {
		d.OperationalAddress = r.Addresses[0].UDPAddr()
	}
	txt := r.txt()
	d.DiscoveryData = &txt
	return d
}

func nonZeroID(s string) *uint16 {
	v, err := strconv.ParseUint(s, 10, 16)
	if err != nil || v == 0 {
		return nil
	}
	id := uint16(v)
	return &id
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// addressList formats addresses for the change log.
func addressList(as []NetworkAddress) string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.String())
	}
	return strings.Join(out, ", ")
}

// Record is a copy of the node's commissioning record.
func (n *Node) Record() Record {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.record.clone()
}

// PeerAddress is the node's address on our fabric; ok is false while the
// node is not commissioned.
func (n *Node) PeerAddress() (addr handshake.PeerAddress, ok bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.record.PeerAddress == nil {
		return handshake.PeerAddress{}, false
	}
	return *n.record.PeerAddress, true
}

// IsCommissioned reports whether the node has a peer address — matter.js
// lifecycle.isCommissioned (node/NodeLifecycle.ts:94-96), which the
// commissioning state's peerAddress sets (CommissioningClient.ts:586,
// :637-646).
func (n *Node) IsCommissioned() bool {
	_, ok := n.PeerAddress()
	return ok
}

// updateRecord changes the record under the lock and saves it; the
// address-change lines of CommissioningClient.#operationalAddressesChanged
// (CommissioningClient.ts:600-635) are logged at Info.
func (n *Node) updateRecord(f func(r *Record)) {
	n.mu.Lock()
	before := n.record.Addresses
	f(&n.record)
	after := n.record.Addresses
	var addr string
	if n.record.PeerAddress != nil {
		addr = peerAddressString(*n.record.PeerAddress)
	}
	n.saveRecordLocked()
	n.mu.Unlock()
	switch {
	case after == nil && before != nil:
		n.log.Info("matter.controller.client.operational_address_cleared", slog.String("peer", addr))
	case after != nil && before == nil:
		n.log.Info("matter.controller.client.operational_address_set", slog.String("peer", addr), slog.String("to", addressList(after)))
	case after != nil && addressList(before) != addressList(after):
		n.log.Info("matter.controller.client.operational_address_changed", slog.String("peer", addr),
			slog.String("from", addressList(before)), slog.String("to", addressList(after)))
	}
}

// saveRecordLocked persists the record; a store error is logged. The
// caller holds n.mu (or owns n exclusively).
func (n *Node) saveRecordLocked() {
	values, err := n.record.encode()
	if err == nil {
		err = n.store.Save(n.cfg.ID, 0, recordCluster, values, nil)
	}
	if err != nil {
		n.log.Warn("matter.controller.client.store_write_failed", slog.String("what", "record"), slog.String("err", err.Error()))
	}
}

// peerAddressString formats a peer address as matter.js PeerAddress
// prints it (protocol/src/peer/PeerAddress.ts:59-62): "@<fabric index>:<node
// id>", both in hex.
func peerAddressString(a handshake.PeerAddress) string {
	return "@" + strconv.FormatUint(uint64(a.FabricIndex), 16) + ":" + strconv.FormatUint(a.NodeID, 16)
}
