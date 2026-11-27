// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestAbsentCommandFieldsDecodeAsEmpty (decision 334): an InvokeRequest
// whose CommandDataIB carries no CommandFields decodes through the
// bridge's reader exactly as one carrying an empty structure: a command
// with a mandatory field answers INVALID_COMMAND (a definition-decoded and
// a hand-read cluster), a command whose fields are all optional gets its
// decoded empty request, and a command without fields — by definition or
// by hand reader — hands no payload on (matter.js TlvNoArguments decodes to
// nothing, packages/types/src/tlv/TlvVoid.ts:20-22 at db91886c).
func TestAbsentCommandFieldsDecodeAsEmpty(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name         string
		cluster, cmd uint32
		status       im.StatusCode
		nilFields    bool
	}{
		{"Identify.Identify, IdentifyTime mandatory", 0x0003, 0x00, im.StatusInvalidCommand, true},
		{"OnOff.OffWithEffect, mandatory fields", 0x0006, 0x40, im.StatusInvalidCommand, true},
		{"GeneralCommissioning.ArmFailSafe (hand read)", 0x0030, 0x00, im.StatusInvalidCommand, true},
		{"DoorLock.LockDoor, PINCode optional", 0x0101, 0x00, im.StatusSuccess, false},
		{"OnOff.Off, no fields", 0x0006, 0x00, im.StatusSuccess, true},
		{"AdministratorCommissioning.RevokeCommissioning, no fields", 0x003C, 0x02, im.StatusSuccess, true},
		{"GeneralCommissioning.CommissioningComplete (hand read), no fields", 0x0030, 0x04, im.StatusSuccess, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := im.ConcreteCommandPath{Endpoint: 1, HasEndpoint: true, Cluster: c.cluster, Command: c.cmd}
			decode := func(fields bool) im.CommandInvocation {
				enc := tlv.NewEncoder()
				im.MarshalInvokeRequestTLV(enc, im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: path}}}, func(enc *tlv.Encoder, tag tlv.Tag, _ any) {
					if fields {
						enc.StartStruct(tag)
						_ = enc.EndContainer()
					}
				})
				wire, err := enc.Bytes()
				if err != nil {
					t.Fatal(err)
				}
				req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(wire), commandFieldsReader)
				if err != nil || len(req.Invokes) != 1 {
					t.Fatalf("unmarshal: %v", err)
				}
				return req.Invokes[0]
			}
			absent, empty := decode(false), decode(true)
			if absent.DecodeStatus != c.status || (absent.Fields == nil) != c.nilFields {
				t.Errorf("absent: status %v, fields %#v; want %v, nil %v", absent.DecodeStatus, absent.Fields, c.status, c.nilFields)
			}
			if absent.DecodeStatus != empty.DecodeStatus || (absent.Fields == nil) != (empty.Fields == nil) {
				t.Errorf("absent %+v and empty %+v decode differently", absent, empty)
			}
		})
	}
}
