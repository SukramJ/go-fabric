// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package wire is the ESPHome native API as far as a Bluetooth proxy client
// needs it: the protobuf messages, hand-written, the plaintext framing and
// the Noise framing. Both ends are here — the client of package esphome and
// the fake proxy of esphometest — so the two cannot drift apart; the tests
// pin the bytes against values derived from the sources by hand.
//
// Every message cites its definition in aioesphomeapi's api.proto
// (https://github.com/esphome/aioesphomeapi, aioesphomeapi/api.proto at
// 3b2a4a2d, MIT): the message id and the field numbers and types are taken
// from there. Encoding follows protobuf's proto3 rules: a field at its
// default value is not written, unknown fields are skipped, and a repeated
// scalar is read packed or unpacked.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The message ids, the `option (id)` of each message (api.proto at the
// line given).
const (
	TypeHelloRequest                                uint16 = 1   // :110-111
	TypeHelloResponse                               uint16 = 2   // :131-132
	TypeDisconnectRequest                           uint16 = 5   // :186-187
	TypeDisconnectResponse                          uint16 = 6   // :198-199
	TypePingRequest                                 uint16 = 7   // :207-208
	TypePingResponse                                uint16 = 8   // :213-214
	TypeDeviceInfoRequest                           uint16 = 9   // :219-220
	TypeDeviceInfoResponse                          uint16 = 10  // :273-274
	TypeSubscribeBluetoothLEAdvertisementsRequest   uint16 = 66  // :1862-1863
	TypeBluetoothLEAdvertisementResponse            uint16 = 67  // :1879-1881
	TypeBluetoothDeviceRequest                      uint16 = 68  // :1926-1927
	TypeBluetoothDeviceConnectionResponse           uint16 = 69  // :1937-1938
	TypeBluetoothGATTGetServicesRequest             uint16 = 70  // :1948-1949
	TypeBluetoothGATTGetServicesResponse            uint16 = 71  // :1992-1993
	TypeBluetoothGATTGetServicesDoneResponse        uint16 = 72  // :2001-2002
	TypeBluetoothGATTReadRequest                    uint16 = 73  // :2009-2010
	TypeBluetoothGATTReadResponse                   uint16 = 74  // :2018-2019
	TypeBluetoothGATTWriteRequest                   uint16 = 75  // :2030-2031
	TypeBluetoothGATTWriteDescriptorRequest         uint16 = 77  // :2051-2052
	TypeBluetoothGATTNotifyRequest                  uint16 = 78  // :2062-2063
	TypeBluetoothGATTNotifyDataResponse             uint16 = 79  // :2072-2073
	TypeSubscribeBluetoothConnectionsFreeRequest    uint16 = 80  // :2083-2084
	TypeBluetoothConnectionsFreeResponse            uint16 = 81  // :2089-2090
	TypeBluetoothGATTErrorResponse                  uint16 = 82  // :2102-2103
	TypeBluetoothGATTWriteResponse                  uint16 = 83  // :2112-2113
	TypeBluetoothGATTNotifyResponse                 uint16 = 84  // :2121-2122
	TypeUnsubscribeBluetoothLEAdvertisementsRequest uint16 = 87  // :2150-2151
	TypeBluetoothLERawAdvertisementsResponse        uint16 = 93  // :1906-1907
	TypeDeviceCapabilitiesRequest                   uint16 = 149 // :373-374
	TypeDeviceCapabilitiesResponse                  uint16 = 150 // :423-424
)

// BluetoothDeviceRequestType values (api.proto:1916-1924) and the
// subscription flag of SubscribeBluetoothLEAdvertisementsRequest.
const (
	DeviceRequestDisconnect            uint32 = 1 // :1918
	DeviceRequestConnectV3WithCache    uint32 = 4 // :1921
	DeviceRequestConnectV3WithoutCache uint32 = 5 // :1922
	// SubscriptionFlagRawAdvertisements asks for
	// BluetoothLERawAdvertisementsResponse (aioesphomeapi/model.py:158-159).
	SubscriptionFlagRawAdvertisements uint32 = 1
)

// Wire types and the field numbers of the nested messages.
const (
	wireVarint, wireFixed64, wireBytes        = 0, 1, 2
	wireFixed32                               = 5
	bluetoothProxyCapabilitiesFlagsField      = 1  // api.proto:389
	bluetoothProxyCapabilitiesMACField        = 2  // api.proto:391
	deviceCapabilitiesBluetoothProxyField     = 1  // api.proto:427
	deviceInfoNameField                       = 2  // api.proto:282
	deviceInfoMACField                        = 3  // api.proto:285
	deviceInfoBluetoothProxyFeatureFlagsField = 15 // api.proto:312
	deviceInfoBluetoothMACField               = 18 // api.proto:328
)

// Bluetooth proxy feature flags (aioesphomeapi/model.py:138-146).
const (
	FeaturePassiveScan       uint32 = 1 << 0
	FeatureActiveConnections uint32 = 1 << 1
	FeatureRemoteCaching     uint32 = 1 << 2
	FeaturePairing           uint32 = 1 << 3
	FeatureCacheClearing     uint32 = 1 << 4
	FeatureRawAdvertisements uint32 = 1 << 5
)

// ErrMalformed is a message that does not decode as protobuf.
var ErrMalformed = errors.New("wire: malformed protobuf message")

// Message is one API message: its id and its encoding.
type Message interface {
	// Type is the message id.
	Type() uint16
	// Marshal is the protobuf encoding.
	Marshal() []byte
}

// ---- encoding ----

// AppendVarint appends v as a protobuf base-128 varint.
func AppendVarint(b []byte, v uint64) []byte {
	return binary.AppendUvarint(b, v)
}

func appendTag(b []byte, field, wt int) []byte {
	return AppendVarint(b, uint64(field)<<3|uint64(wt)) //nolint:gosec // field numbers are small and positive
}

func appendUint(b []byte, field int, v uint64) []byte {
	if v == 0 {
		return b
	}
	return AppendVarint(appendTag(b, field, wireVarint), v)
}

// appendUintForce writes v even at zero, as a repeated element is written.
func appendUintForce(b []byte, field int, v uint64) []byte {
	return AppendVarint(appendTag(b, field, wireVarint), v)
}

func appendBool(b []byte, field int, v bool) []byte {
	if !v {
		return b
	}
	return appendUint(b, field, 1)
}

// appendInt32 is protobuf's int32: a negative value is sign-extended to
// ten bytes.
func appendInt32(b []byte, field int, v int32) []byte {
	return appendUint(b, field, uint64(int64(v))) //nolint:gosec // the two's-complement sign extension protobuf mandates
}

// appendSint32 is protobuf's sint32: zigzag.
func appendSint32(b []byte, field int, v int32) []byte {
	return appendUint(b, field, zigzag32(v))
}

// zigzag32 is the sint32 encoding of v.
func zigzag32(v int32) uint64 {
	return uint64(uint32((v << 1) ^ (v >> 31))) //nolint:gosec // the zigzag mapping reinterprets the bits
}

func appendBytes(b []byte, field int, v []byte) []byte {
	if len(v) == 0 {
		return b
	}
	return appendBytesForce(b, field, v)
}

func appendBytesForce(b []byte, field int, v []byte) []byte {
	b = appendTag(b, field, wireBytes)
	b = AppendVarint(b, uint64(len(v)))
	return append(b, v...)
}

func appendString(b []byte, field int, v string) []byte {
	return appendBytes(b, field, []byte(v))
}

// ---- decoding ----

// field is one decoded field: a varint's value, or a length-delimited
// field's bytes.
type field struct {
	num   int
	wt    int
	value uint64
	data  []byte
}

// fields decodes b, calling f for each field in order.
func fields(b []byte, f func(field) error) error {
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return ErrMalformed
		}
		b = b[n:]
		fl := field{num: int(tag >> 3), wt: int(tag & 7)} //nolint:gosec // a field number fits an int
		if fl.num == 0 {
			return ErrMalformed
		}
		switch fl.wt {
		case wireVarint:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return ErrMalformed
			}
			fl.value, b = v, b[n:]
		case wireFixed64:
			if len(b) < 8 {
				return ErrMalformed
			}
			fl.value, b = binary.LittleEndian.Uint64(b), b[8:]
		case wireFixed32:
			if len(b) < 4 {
				return ErrMalformed
			}
			fl.value, b = uint64(binary.LittleEndian.Uint32(b)), b[4:]
		case wireBytes:
			l, n := binary.Uvarint(b)
			if n <= 0 || l > uint64(len(b)-n) { //nolint:gosec // len(b)-n is not negative
				return ErrMalformed
			}
			fl.data, b = b[n:n+int(l)], b[n+int(l):] //nolint:gosec // bounded by len(b) above
		default:
			return fmt.Errorf("%w: wire type %d", ErrMalformed, fl.wt)
		}
		if err := f(fl); err != nil {
			return err
		}
	}
	return nil
}

func (f field) u32() uint32 { return uint32(f.value) } //nolint:gosec // proto3 truncates a uint32 the same way

func (f field) i32() int32 { return int32(f.value) } //nolint:gosec // proto3 int32 is the low 32 bits

func (f field) s32() int32 {
	v := uint32(f.value) //nolint:gosec // sint32 is the low 32 bits, zigzag-encoded
	return int32(v>>1) ^ -int32(v&1)
}

func (f field) boolean() bool { return f.value != 0 }

func (f field) clone() []byte { return append([]byte(nil), f.data...) }

// repeatedU64 appends a repeated uint64 element: one varint, or a packed run.
func (f field) repeatedU64(dst []uint64) ([]uint64, error) {
	if f.wt == wireVarint {
		return append(dst, f.value), nil
	}
	if f.wt != wireBytes {
		return dst, ErrMalformed
	}
	b := f.data
	for len(b) > 0 {
		v, n := binary.Uvarint(b)
		if n <= 0 {
			return dst, ErrMalformed
		}
		dst, b = append(dst, v), b[n:]
	}
	return dst, nil
}

// ---- the connection messages ----

// HelloRequest is api.proto:110-127.
type HelloRequest struct {
	ClientInfo      string // 1
	APIVersionMajor uint32 // 2
	APIVersionMinor uint32 // 3
}

// Type implements Message.
func (*HelloRequest) Type() uint16 { return TypeHelloRequest }

// Marshal implements Message.
func (m *HelloRequest) Marshal() []byte {
	b := appendString(nil, 1, m.ClientInfo)
	b = appendUint(b, 2, uint64(m.APIVersionMajor))
	return appendUint(b, 3, uint64(m.APIVersionMinor))
}

// Unmarshal decodes b.
func (m *HelloRequest) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			m.ClientInfo = string(f.data)
		case 2:
			m.APIVersionMajor = f.u32()
		case 3:
			m.APIVersionMinor = f.u32()
		}
		return nil
	})
}

// HelloResponse is api.proto:131-151.
type HelloResponse struct {
	APIVersionMajor uint32 // 1
	APIVersionMinor uint32 // 2
	ServerInfo      string // 3
	Name            string // 4
}

// Type implements Message.
func (*HelloResponse) Type() uint16 { return TypeHelloResponse }

// Marshal implements Message.
func (m *HelloResponse) Marshal() []byte {
	b := appendUint(nil, 1, uint64(m.APIVersionMajor))
	b = appendUint(b, 2, uint64(m.APIVersionMinor))
	b = appendString(b, 3, m.ServerInfo)
	return appendString(b, 4, m.Name)
}

// Unmarshal decodes b.
func (m *HelloResponse) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			m.APIVersionMajor = f.u32()
		case 2:
			m.APIVersionMinor = f.u32()
		case 3:
			m.ServerInfo = string(f.data)
		case 4:
			m.Name = string(f.data)
		}
		return nil
	})
}

// Empty is a message without fields: PingRequest (api.proto:207-211),
// PingResponse (:213-217), DeviceInfoRequest (:219-223),
// DisconnectResponse (:198-205), SubscribeBluetoothConnectionsFreeRequest
// (:2083-2087), UnsubscribeBluetoothLEAdvertisementsRequest (:2150-2154),
// DeviceCapabilitiesRequest (:373-377).
type Empty uint16

// Type implements Message.
func (m Empty) Type() uint16 { return uint16(m) }

// Marshal implements Message.
func (Empty) Marshal() []byte { return nil }

// DisconnectRequest is api.proto:186-196.
type DisconnectRequest struct {
	Reason uint32 // 1, DisconnectReason
}

// Type implements Message.
func (*DisconnectRequest) Type() uint16 { return TypeDisconnectRequest }

// Marshal implements Message.
func (m *DisconnectRequest) Marshal() []byte { return appendUint(nil, 1, uint64(m.Reason)) }

// DeviceInfoResponse is the part of api.proto:273-330 a proxy client reads.
type DeviceInfoResponse struct {
	Name                       string // 2 (:282)
	MACAddress                 string // 3 (:285)
	BluetoothProxyFeatureFlags uint32 // 15 (:312)
	BluetoothMACAddress        string // 18 (:328)
}

// Type implements Message.
func (*DeviceInfoResponse) Type() uint16 { return TypeDeviceInfoResponse }

// Marshal implements Message.
func (m *DeviceInfoResponse) Marshal() []byte {
	b := appendString(nil, deviceInfoNameField, m.Name)
	b = appendString(b, deviceInfoMACField, m.MACAddress)
	b = appendUint(b, deviceInfoBluetoothProxyFeatureFlagsField, uint64(m.BluetoothProxyFeatureFlags))
	return appendString(b, deviceInfoBluetoothMACField, m.BluetoothMACAddress)
}

// Unmarshal decodes b.
func (m *DeviceInfoResponse) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		switch f.num {
		case deviceInfoNameField:
			m.Name = string(f.data)
		case deviceInfoMACField:
			m.MACAddress = string(f.data)
		case deviceInfoBluetoothProxyFeatureFlagsField:
			m.BluetoothProxyFeatureFlags = f.u32()
		case deviceInfoBluetoothMACField:
			m.BluetoothMACAddress = string(f.data)
		}
		return nil
	})
}

// DeviceCapabilitiesResponse is the bluetooth_proxy part of
// api.proto:423-435: BluetoothProxyCapabilities (:387-392) in field 1.
type DeviceCapabilitiesResponse struct {
	BluetoothProxyFeatureFlags uint32 // 1.1
	BluetoothMACAddress        string // 1.2
}

// Type implements Message.
func (*DeviceCapabilitiesResponse) Type() uint16 { return TypeDeviceCapabilitiesResponse }

// Marshal implements Message.
func (m *DeviceCapabilitiesResponse) Marshal() []byte {
	sub := appendUint(nil, bluetoothProxyCapabilitiesFlagsField, uint64(m.BluetoothProxyFeatureFlags))
	sub = appendString(sub, bluetoothProxyCapabilitiesMACField, m.BluetoothMACAddress)
	return appendBytes(nil, deviceCapabilitiesBluetoothProxyField, sub)
}

// Unmarshal decodes b.
func (m *DeviceCapabilitiesResponse) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		if f.num != deviceCapabilitiesBluetoothProxyField || f.wt != wireBytes {
			return nil
		}
		return fields(f.data, func(g field) error {
			switch g.num {
			case bluetoothProxyCapabilitiesFlagsField:
				m.BluetoothProxyFeatureFlags = g.u32()
			case bluetoothProxyCapabilitiesMACField:
				m.BluetoothMACAddress = string(g.data)
			}
			return nil
		})
	})
}

// ---- advertisements ----

// SubscribeBluetoothLEAdvertisementsRequest is api.proto:1862-1868.
type SubscribeBluetoothLEAdvertisementsRequest struct {
	Flags uint32 // 1
}

// Type implements Message.
func (*SubscribeBluetoothLEAdvertisementsRequest) Type() uint16 {
	return TypeSubscribeBluetoothLEAdvertisementsRequest
}

// Marshal implements Message.
func (m *SubscribeBluetoothLEAdvertisementsRequest) Marshal() []byte {
	return appendUint(nil, 1, uint64(m.Flags))
}

// Unmarshal decodes b.
func (m *SubscribeBluetoothLEAdvertisementsRequest) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		if f.num == 1 {
			m.Flags = f.u32()
		}
		return nil
	})
}

// RawAdvertisement is BluetoothLERawAdvertisement (api.proto:1897-1904):
// the advertisement's AD structures, unparsed.
type RawAdvertisement struct {
	Address     uint64 // 1
	RSSI        int32  // 2, sint32
	AddressType uint32 // 3
	Data        []byte // 4
}

// BluetoothLERawAdvertisementsResponse is api.proto:1906-1914.
type BluetoothLERawAdvertisementsResponse struct {
	Advertisements []RawAdvertisement // 1
}

// Type implements Message.
func (*BluetoothLERawAdvertisementsResponse) Type() uint16 {
	return TypeBluetoothLERawAdvertisementsResponse
}

// Marshal implements Message. The address, the RSSI and the data are
// written even at zero ((force) = true, :1899-1903).
func (m *BluetoothLERawAdvertisementsResponse) Marshal() []byte {
	var b []byte
	for _, a := range m.Advertisements {
		sub := appendUintForce(nil, 1, a.Address)
		sub = appendUintForce(sub, 2, zigzag32(a.RSSI))
		sub = appendUint(sub, 3, uint64(a.AddressType))
		sub = appendBytesForce(sub, 4, a.Data)
		b = appendBytesForce(b, 1, sub)
	}
	return b
}

// Unmarshal decodes b.
func (m *BluetoothLERawAdvertisementsResponse) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		if f.num != 1 || f.wt != wireBytes {
			return nil
		}
		var a RawAdvertisement
		if err := fields(f.data, func(g field) error {
			switch g.num {
			case 1:
				a.Address = g.value
			case 2:
				a.RSSI = g.s32()
			case 3:
				a.AddressType = g.u32()
			case 4:
				a.Data = g.clone()
			}
			return nil
		}); err != nil {
			return err
		}
		m.Advertisements = append(m.Advertisements, a)
		return nil
	})
}

// ServiceData is BluetoothServiceData (api.proto:1871-1877).
type ServiceData struct {
	UUID string // 1
	Data []byte // 3
}

// BluetoothLEAdvertisementResponse is the parsed advertisement of
// api.proto:1879-1895, deprecated, which a proxy without raw advertisements
// sends.
type BluetoothLEAdvertisementResponse struct {
	Address     uint64        // 1
	Name        []byte        // 2
	RSSI        int32         // 3, sint32
	ServiceData []ServiceData // 5
	AddressType uint32        // 7
}

// Type implements Message.
func (*BluetoothLEAdvertisementResponse) Type() uint16 { return TypeBluetoothLEAdvertisementResponse }

// Marshal implements Message.
func (m *BluetoothLEAdvertisementResponse) Marshal() []byte {
	b := appendUint(nil, 1, m.Address)
	b = appendBytes(b, 2, m.Name)
	b = appendSint32(b, 3, m.RSSI)
	for _, sd := range m.ServiceData {
		sub := appendString(nil, 1, sd.UUID)
		sub = appendBytes(sub, 3, sd.Data)
		b = appendBytesForce(b, 5, sub)
	}
	return appendUint(b, 7, uint64(m.AddressType))
}

// Unmarshal decodes b.
func (m *BluetoothLEAdvertisementResponse) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			m.Address = f.value
		case 2:
			m.Name = f.clone()
		case 3:
			m.RSSI = f.s32()
		case 5:
			var sd ServiceData
			if err := fields(f.data, func(g field) error {
				switch g.num {
				case 1:
					sd.UUID = string(g.data)
				case 3:
					sd.Data = g.clone()
				}
				return nil
			}); err != nil {
				return err
			}
			m.ServiceData = append(m.ServiceData, sd)
		case 7:
			m.AddressType = f.u32()
		}
		return nil
	})
}

// ---- connections ----

// BluetoothDeviceRequest is api.proto:1926-1935.
type BluetoothDeviceRequest struct {
	Address        uint64 // 1
	RequestType    uint32 // 2, BluetoothDeviceRequestType
	HasAddressType bool   // 3
	AddressType    uint32 // 4
}

// Type implements Message.
func (*BluetoothDeviceRequest) Type() uint16 { return TypeBluetoothDeviceRequest }

// Marshal implements Message.
func (m *BluetoothDeviceRequest) Marshal() []byte {
	b := appendUint(nil, 1, m.Address)
	b = appendUint(b, 2, uint64(m.RequestType))
	b = appendBool(b, 3, m.HasAddressType)
	return appendUint(b, 4, uint64(m.AddressType))
}

// Unmarshal decodes b.
func (m *BluetoothDeviceRequest) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			m.Address = f.value
		case 2:
			m.RequestType = f.u32()
		case 3:
			m.HasAddressType = f.boolean()
		case 4:
			m.AddressType = f.u32()
		}
		return nil
	})
}

// BluetoothDeviceConnectionResponse is api.proto:1937-1946.
type BluetoothDeviceConnectionResponse struct {
	Address   uint64 // 1
	Connected bool   // 2
	MTU       uint32 // 3
	Error     int32  // 4
}

// Type implements Message.
func (*BluetoothDeviceConnectionResponse) Type() uint16 {
	return TypeBluetoothDeviceConnectionResponse
}

// Marshal implements Message.
func (m *BluetoothDeviceConnectionResponse) Marshal() []byte {
	b := appendUint(nil, 1, m.Address)
	b = appendBool(b, 2, m.Connected)
	b = appendUint(b, 3, uint64(m.MTU))
	return appendInt32(b, 4, m.Error)
}

// Unmarshal decodes b.
func (m *BluetoothDeviceConnectionResponse) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			m.Address = f.value
		case 2:
			m.Connected = f.boolean()
		case 3:
			m.MTU = f.u32()
		case 4:
			m.Error = f.i32()
		}
		return nil
	})
}

// BluetoothConnectionsFreeResponse is api.proto:2089-2100.
type BluetoothConnectionsFreeResponse struct {
	Free      uint32   // 1
	Limit     uint32   // 2
	Allocated []uint64 // 3
}

// Type implements Message.
func (*BluetoothConnectionsFreeResponse) Type() uint16 { return TypeBluetoothConnectionsFreeResponse }

// Marshal implements Message: the allocated addresses one by one, zero
// skipped ((fixed_array_skip_zero) = true, :2096-2099).
func (m *BluetoothConnectionsFreeResponse) Marshal() []byte {
	b := appendUint(nil, 1, uint64(m.Free))
	b = appendUint(b, 2, uint64(m.Limit))
	for _, a := range m.Allocated {
		b = appendUint(b, 3, a)
	}
	return b
}

// Unmarshal decodes b.
func (m *BluetoothConnectionsFreeResponse) Unmarshal(b []byte) error {
	return fields(b, func(f field) (err error) {
		switch f.num {
		case 1:
			m.Free = f.u32()
		case 2:
			m.Limit = f.u32()
		case 3:
			m.Allocated, err = f.repeatedU64(m.Allocated)
		}
		return err
	})
}

// ---- GATT ----

// UUID is a GATT UUID as the API carries it: a 16- or 32-bit short UUID
// (short_uuid, v1.12+), or the 128-bit UUID as two 64-bit words, high
// first (uuid; aioesphomeapi/model.py:1853-1878).
type UUID struct {
	Words [2]uint64
	Short uint32
}

// Is128 reports whether u is in its 128-bit form.
func (u UUID) Is128() bool { return u.Short == 0 }

// appendUUID writes the uuid words (field 1, both written, as the firmware
// writes a fixed array, unless both are zero); short writes the short UUID
// at the end.
func appendUUID(b []byte, shortField int, u UUID) (out []byte, short func([]byte) []byte) {
	if u.Words[0] != 0 || u.Words[1] != 0 {
		b = appendUintForce(b, 1, u.Words[0])
		b = appendUintForce(b, 1, u.Words[1])
	}
	return b, func(b []byte) []byte { return appendUint(b, shortField, uint64(u.Short)) }
}

func (u *UUID) word(f field, n *int) error {
	ws, err := f.repeatedU64(nil)
	for _, w := range ws {
		if *n < 2 {
			u.Words[*n] = w
		}
		*n++
	}
	return err
}

// GATTDescriptor is BluetoothGATTDescriptor (api.proto:1956-1965).
type GATTDescriptor struct {
	UUID   UUID   // 1, 3
	Handle uint32 // 2
}

// GATTCharacteristic is BluetoothGATTCharacteristic (api.proto:1967-1978).
type GATTCharacteristic struct {
	UUID        UUID             // 1, 5
	Handle      uint32           // 2
	Properties  uint32           // 3
	Descriptors []GATTDescriptor // 4
}

// GATTService is BluetoothGATTService (api.proto:1980-1990).
type GATTService struct {
	UUID            UUID                 // 1, 4
	Handle          uint32               // 2
	Characteristics []GATTCharacteristic // 3
}

func (d *GATTDescriptor) marshal() []byte {
	b, short := appendUUID(nil, 3, d.UUID)
	b = appendUint(b, 2, uint64(d.Handle))
	return short(b)
}

func (c *GATTCharacteristic) marshal() []byte {
	b, short := appendUUID(nil, 5, c.UUID)
	b = appendUint(b, 2, uint64(c.Handle))
	b = appendUint(b, 3, uint64(c.Properties))
	for i := range c.Descriptors {
		b = appendBytesForce(b, 4, c.Descriptors[i].marshal())
	}
	return short(b)
}

func (s *GATTService) marshal() []byte {
	b, short := appendUUID(nil, 4, s.UUID)
	b = appendUint(b, 2, uint64(s.Handle))
	for i := range s.Characteristics {
		b = appendBytesForce(b, 3, s.Characteristics[i].marshal())
	}
	return short(b)
}

func (d *GATTDescriptor) unmarshal(b []byte) error {
	n := 0
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			return d.UUID.word(f, &n)
		case 2:
			d.Handle = f.u32()
		case 3:
			d.UUID.Short = f.u32()
		}
		return nil
	})
}

func (c *GATTCharacteristic) unmarshal(b []byte) error {
	n := 0
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			return c.UUID.word(f, &n)
		case 2:
			c.Handle = f.u32()
		case 3:
			c.Properties = f.u32()
		case 4:
			var d GATTDescriptor
			if err := d.unmarshal(f.data); err != nil {
				return err
			}
			c.Descriptors = append(c.Descriptors, d)
		case 5:
			c.UUID.Short = f.u32()
		}
		return nil
	})
}

func (s *GATTService) unmarshal(b []byte) error {
	n := 0
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			return s.UUID.word(f, &n)
		case 2:
			s.Handle = f.u32()
		case 3:
			var c GATTCharacteristic
			if err := c.unmarshal(f.data); err != nil {
				return err
			}
			s.Characteristics = append(s.Characteristics, c)
		case 4:
			s.UUID.Short = f.u32()
		}
		return nil
	})
}

// BluetoothGATTGetServicesResponse is api.proto:1992-1999.
type BluetoothGATTGetServicesResponse struct {
	Address  uint64        // 1
	Services []GATTService // 2
}

// Type implements Message.
func (*BluetoothGATTGetServicesResponse) Type() uint16 { return TypeBluetoothGATTGetServicesResponse }

// Marshal implements Message.
func (m *BluetoothGATTGetServicesResponse) Marshal() []byte {
	b := appendUint(nil, 1, m.Address)
	for i := range m.Services {
		b = appendBytesForce(b, 2, m.Services[i].marshal())
	}
	return b
}

// Unmarshal decodes b.
func (m *BluetoothGATTGetServicesResponse) Unmarshal(b []byte) error {
	return fields(b, func(f field) error {
		switch f.num {
		case 1:
			m.Address = f.value
		case 2:
			var s GATTService
			if err := s.unmarshal(f.data); err != nil {
				return err
			}
			m.Services = append(m.Services, s)
		}
		return nil
	})
}

// AddressHandle is a message of an address and a handle, and of an
// optional third field: BluetoothGATTGetServicesRequest (api.proto:1948-1954,
// address only), BluetoothGATTGetServicesDoneResponse (:2001-2007, address
// only), BluetoothGATTReadRequest (:2009-2016), BluetoothGATTWriteResponse
// (:2112-2119), BluetoothGATTNotifyResponse (:2121-2128),
// BluetoothGATTReadResponse (:2018-2028, data 3),
// BluetoothGATTWriteDescriptorRequest (:2051-2060, data 3),
// BluetoothGATTNotifyDataResponse (:2072-2081, data 3),
// BluetoothGATTNotifyRequest (:2062-2070, enable 3),
// BluetoothGATTErrorResponse (:2102-2110, int32 error 3) and
// BluetoothGATTWriteRequest (:2030-2040, bool response 3, data 4).
type AddressHandle struct {
	MessageType uint16
	Address     uint64 // 1
	Handle      uint32 // 2
	Data        []byte // 3, or 4 for a write request
	Flag        bool   // 3: enable, or response for a write request
	Error       int32  // 3 of the error response
}

// Type implements Message.
func (m *AddressHandle) Type() uint16 { return m.MessageType }

// Marshal implements Message.
func (m *AddressHandle) Marshal() []byte {
	b := appendUint(nil, 1, m.Address)
	b = appendUint(b, 2, uint64(m.Handle))
	switch m.MessageType {
	case TypeBluetoothGATTWriteRequest:
		b = appendBool(b, 3, m.Flag)
		b = appendBytes(b, 4, m.Data)
	case TypeBluetoothGATTNotifyRequest:
		b = appendBool(b, 3, m.Flag)
	case TypeBluetoothGATTErrorResponse:
		b = appendInt32(b, 3, m.Error)
	default:
		b = appendBytes(b, 3, m.Data)
	}
	return b
}

// Unmarshal decodes b as a message of type t.
func (m *AddressHandle) Unmarshal(t uint16, b []byte) error {
	m.MessageType = t
	return fields(b, func(f field) error {
		switch {
		case f.num == 1:
			m.Address = f.value
		case f.num == 2:
			m.Handle = f.u32()
		case f.num == 3 && (t == TypeBluetoothGATTWriteRequest || t == TypeBluetoothGATTNotifyRequest):
			m.Flag = f.boolean()
		case f.num == 3 && t == TypeBluetoothGATTErrorResponse:
			m.Error = f.i32()
		case f.num == 3 && f.wt == wireBytes:
			m.Data = f.clone()
		case f.num == 4 && t == TypeBluetoothGATTWriteRequest:
			m.Data = f.clone()
		}
		return nil
	})
}
