// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

// wrongType is field num in the wire type the field does not have: bytes
// for a varint field, a varint for a bytes field.
func wrongType(num int, varint bool) []byte {
	if varint {
		return appendBytesForce(nil, num, []byte{0xff})
	}
	return appendUint(nil, num, 7)
}

// cat is the concatenation of bs.
func cat(bs ...[]byte) []byte {
	var out []byte
	for _, b := range bs {
		out = append(out, b...)
	}
	return out
}

// TestWrongWireTypeIsSkipped (R2-P6-wire-2054): a field that arrives in
// another wire type than its message gives it is skipped, as protobuf
// skips an unknown field: the message decodes as if the field were absent
// — no zero value over a decoded one, no empty entry in a repeated field.
// One case per message and per nested message; each field's type is the
// one the message's Marshal writes.
func TestWrongWireTypeIsSkipped(t *testing.T) {
	type fieldType struct {
		num    int
		varint bool
	}
	v := func(nums ...int) []fieldType {
		out := make([]fieldType, len(nums))
		for i, n := range nums {
			out[i] = fieldType{n, true}
		}
		return out
	}
	b := func(nums ...int) []fieldType {
		out := make([]fieldType, len(nums))
		for i, n := range nums {
			out[i] = fieldType{n, false}
		}
		return out
	}
	fs := func(groups ...[]fieldType) []fieldType {
		var out []fieldType
		for _, g := range groups {
			out = append(out, g...)
		}
		return out
	}
	svc := GATTService{UUID: UUID{Short: 0x1800}, Handle: 1, Characteristics: []GATTCharacteristic{
		{UUID: UUID{Short: 0x2a00}, Handle: 2, Properties: 0x02, Descriptors: []GATTDescriptor{{UUID: UUID{Short: 0x2902}, Handle: 3}}},
	}}
	type wireCase struct {
		name string
		// good is the message (or nested message) with every field; wrap,
		// when set, puts a nested message into its outer message.
		good   []byte
		wrap   func(inner []byte) []byte
		fields []fieldType
		decode func([]byte) (any, error)
	}
	cases := []wireCase{
		{
			"HelloRequest", (&HelloRequest{ClientInfo: "c", APIVersionMajor: 1, APIVersionMinor: 19}).Marshal(), nil, fs(b(1), v(2, 3)),
			func(x []byte) (any, error) { var m HelloRequest; err := m.Unmarshal(x); return m, err },
		},
		{
			"HelloResponse", (&HelloResponse{APIVersionMajor: 1, APIVersionMinor: 14, ServerInfo: "s", Name: "n"}).Marshal(), nil, fs(v(1, 2), b(3, 4)),
			func(x []byte) (any, error) { var m HelloResponse; err := m.Unmarshal(x); return m, err },
		},
		{
			"DeviceInfoResponse", (&DeviceInfoResponse{Name: "n", MACAddress: "m", BluetoothProxyFeatureFlags: 7, BluetoothMACAddress: "b"}).Marshal(), nil,
			fs(b(deviceInfoNameField, deviceInfoMACField, deviceInfoBluetoothMACField), v(deviceInfoBluetoothProxyFeatureFlagsField)),
			func(x []byte) (any, error) { var m DeviceInfoResponse; err := m.Unmarshal(x); return m, err },
		},
		{
			"DeviceCapabilitiesResponse", (&DeviceCapabilitiesResponse{BluetoothProxyFeatureFlags: 7, BluetoothMACAddress: "b"}).Marshal(), nil, b(deviceCapabilitiesBluetoothProxyField),
			func(x []byte) (any, error) { var m DeviceCapabilitiesResponse; err := m.Unmarshal(x); return m, err },
		},
		{
			"BluetoothProxyCapabilities", appendUint(nil, bluetoothProxyCapabilitiesFlagsField, 7),
			func(in []byte) []byte { return appendBytesForce(nil, deviceCapabilitiesBluetoothProxyField, in) },
			fs(v(bluetoothProxyCapabilitiesFlagsField), b(bluetoothProxyCapabilitiesMACField)),
			func(x []byte) (any, error) { var m DeviceCapabilitiesResponse; err := m.Unmarshal(x); return m, err },
		},
		{
			"SubscribeBluetoothLEAdvertisementsRequest", (&SubscribeBluetoothLEAdvertisementsRequest{Flags: 1}).Marshal(), nil, v(1),
			func(x []byte) (any, error) {
				var m SubscribeBluetoothLEAdvertisementsRequest
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"BluetoothLERawAdvertisementsResponse", (&BluetoothLERawAdvertisementsResponse{Advertisements: []RawAdvertisement{{Address: 1, RSSI: -5, AddressType: 1, Data: []byte{1}}}}).Marshal(), nil, b(1),
			func(x []byte) (any, error) {
				var m BluetoothLERawAdvertisementsResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"RawAdvertisement", cat(appendUint(nil, 1, 1), appendSint32(nil, 2, -5), appendUint(nil, 3, 1), appendBytes(nil, 4, []byte{1})),
			func(in []byte) []byte { return appendBytesForce(nil, 1, in) }, fs(v(1, 2, 3), b(4)),
			func(x []byte) (any, error) {
				var m BluetoothLERawAdvertisementsResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"BluetoothLEAdvertisementResponse", (&BluetoothLEAdvertisementResponse{Address: 9, Name: []byte("n"), RSSI: -70, AddressType: 1, ServiceData: []ServiceData{{UUID: "u", Data: []byte{1}}}}).Marshal(), nil,
			fs(v(1, 3, 7), b(2, 5)),
			func(x []byte) (any, error) {
				var m BluetoothLEAdvertisementResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"ServiceData", cat(appendString(nil, 1, "u"), appendBytes(nil, 3, []byte{1})),
			func(in []byte) []byte { return appendBytesForce(nil, 5, in) }, b(1, 3),
			func(x []byte) (any, error) {
				var m BluetoothLEAdvertisementResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"BluetoothDeviceRequest", (&BluetoothDeviceRequest{Address: 3, RequestType: 1, HasAddressType: true, AddressType: 1}).Marshal(), nil, v(1, 2, 3, 4),
			func(x []byte) (any, error) { var m BluetoothDeviceRequest; err := m.Unmarshal(x); return m, err },
		},
		{
			"BluetoothDeviceConnectionResponse", (&BluetoothDeviceConnectionResponse{Address: 3, Connected: true, MTU: 247, Error: 1}).Marshal(), nil, v(1, 2, 3, 4),
			func(x []byte) (any, error) {
				var m BluetoothDeviceConnectionResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"BluetoothConnectionsFreeResponse", (&BluetoothConnectionsFreeResponse{Free: 1, Limit: 3, Allocated: []uint64{5}}).Marshal(), nil, v(1, 2),
			func(x []byte) (any, error) {
				var m BluetoothConnectionsFreeResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"BluetoothGATTGetServicesResponse", (&BluetoothGATTGetServicesResponse{Address: 1, Services: []GATTService{svc}}).Marshal(), nil, fs(v(1), b(2)),
			func(x []byte) (any, error) {
				var m BluetoothGATTGetServicesResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"GATTService", svc.marshal(), func(in []byte) []byte { return appendBytesForce(nil, 2, in) }, fs(v(2, 4), b(3)),
			func(x []byte) (any, error) {
				var m BluetoothGATTGetServicesResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"GATTCharacteristic", svc.Characteristics[0].marshal(),
			func(in []byte) []byte { return appendBytesForce(nil, 2, appendBytesForce(nil, 3, in)) }, fs(v(2, 3, 5), b(4)),
			func(x []byte) (any, error) {
				var m BluetoothGATTGetServicesResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
		{
			"GATTDescriptor", svc.Characteristics[0].Descriptors[0].marshal(),
			func(in []byte) []byte {
				return appendBytesForce(nil, 2, appendBytesForce(nil, 3, appendBytesForce(nil, 4, in)))
			}, v(2, 3),
			func(x []byte) (any, error) {
				var m BluetoothGATTGetServicesResponse
				err := m.Unmarshal(x)
				return m, err
			},
		},
	}
	check := func(c wireCase) {
		t.Helper()
		wrap := c.wrap
		if wrap == nil {
			wrap = func(in []byte) []byte { return in }
		}
		want, err := c.decode(wrap(c.good))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, f := range c.fields {
			got, err := c.decode(wrap(cat(c.good, wrongType(f.num, f.varint))))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("%s field %d in the wrong wire type: %+v, %v; want %+v", c.name, f.num, got, err, want)
			}
		}
	}
	for _, c := range cases {
		check(c)
	}
	// AddressHandle: field 3's type is the message type's.
	for _, typ := range []struct {
		name   string
		t      uint16
		fields []fieldType
	}{
		{"BluetoothGATTWriteRequest", TypeBluetoothGATTWriteRequest, fs(v(1, 2, 3), b(4))},
		{"BluetoothGATTNotifyRequest", TypeBluetoothGATTNotifyRequest, v(1, 2, 3)},
		{"BluetoothGATTErrorResponse", TypeBluetoothGATTErrorResponse, v(1, 2, 3)},
		{"BluetoothGATTNotifyDataResponse", TypeBluetoothGATTNotifyDataResponse, fs(v(1, 2), b(3))},
	} {
		good := (&AddressHandle{MessageType: typ.t, Address: 1, Handle: 2, Flag: true, Error: 3, Data: []byte{4}}).Marshal()
		check(wireCase{typ.name, good, nil, typ.fields, func(x []byte) (any, error) {
			var m AddressHandle
			err := m.Unmarshal(typ.t, x)
			return m, err
		}})
	}
}

// TestDecodersGoThroughTheirShape is the guard of R2-P6-wire-2054's class:
// in proto.go, fields is called by decode alone, so every decoder states
// the wire type of each field it takes.
func TestDecodersGoThroughTheirShape(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "proto.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name.Name == "decode" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "fields" {
					t.Errorf("%s calls fields: a decoder goes through decode and its shape", fn.Name.Name)
				}
			}
			return true
		})
	}
}
