// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for the width checks in the command-field decoders: a
// wire value that does not fit the field's schema type is rejected, not
// truncated into a different, unrequested value. Lives in package
// bridge to reach the decoders directly.

import (
	"errors"
	"testing"

	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestCommandFieldDecoders_RejectOverWideIntegers pins that every
// narrow integer field is validated against its schema width. matter.js
// validates each command against its TlvSchema before the invoker runs
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:446-448
// requestTlv.validate; TlvNumber.ts:151-156 validateBoundaries throws
// ValidationOutOfBoundsError, a ValidationError carrying
// Status.ConstraintError). Truncating instead executes a value the
// controller never asked for — 0x10099 mireds became 153, and an
// ArmFailSafe of 0x10000 s became a DISARM.
func TestCommandFieldDecoders_RejectOverWideIntegers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		tag    uint8
		value  uint64
		decode func(*tlv.Decoder) error
	}{
		{"MoveToColorTemperature.ColorTemperatureMireds", 0, 0x10099, viaDefinition(0x0300, 0x0A)},
		{"MoveToColorTemperature.TransitionTime", 1, 0x10000, viaDefinition(0x0300, 0x0A)},
		{"MoveToColorTemperature.OptionsMask", 2, 0x100, viaDefinition(0x0300, 0x0A)},
		{"MoveToColorTemperature.OptionsOverride", 3, 0x100, viaDefinition(0x0300, 0x0A)},
		{"MoveToHue.Hue", 0, 0x100, viaDefinition(0x0300, 0x00)},
		{"MoveToHue.Direction", 1, 0x100, viaDefinition(0x0300, 0x00)},
		{"MoveToHue.TransitionTime", 2, 0x10000, viaDefinition(0x0300, 0x00)},
		{"MoveToSaturation.Saturation", 0, 0x100, viaDefinition(0x0300, 0x03)},
		{"MoveToSaturation.TransitionTime", 1, 0x10000, viaDefinition(0x0300, 0x03)},
		{"MoveToHueAndSaturation.Hue", 0, 0x100, viaDefinition(0x0300, 0x06)},
		{"MoveToHueAndSaturation.Saturation", 1, 0x100, viaDefinition(0x0300, 0x06)},
		{"MoveToHueAndSaturation.TransitionTime", 2, 0x10000, viaDefinition(0x0300, 0x06)},
		{"MoveToLevel.TransitionTime", 1, 0x10000, viaDefinition(0x0008, 0x00)},
		{"MoveToLevel.OptionsMask", 2, 0x100, viaDefinition(0x0008, 0x00)},
		{"MoveToLevel.OptionsOverride", 3, 0x100, viaDefinition(0x0008, 0x00)},
		{"ArmFailSafe.ExpiryLengthSeconds", 0, 0x10000, func(d *tlv.Decoder) error { _, err := decodeArmFailSafeRequest(d); return err }},
		{"SetRegulatoryConfig.NewRegulatoryConfig", 0, 0x100, func(d *tlv.Decoder) error { _, err := decodeSetRegulatoryConfigRequest(d); return err }},
		{"CertificateChainRequest.CertificateType", 0, 0x100, func(d *tlv.Decoder) error { _, err := decodeCertificateChainRequest(d); return err }},
		{"AddNOC.AdminVendorID", 4, 0x10000, func(d *tlv.Decoder) error { _, err := decodeAddNOCRequest(d); return err }},
		{"RemoveFabric.FabricIndex", 0, 0x100, func(d *tlv.Decoder) error { _, err := decodeRemoveFabricRequest(d); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dec := buildDecoderAfterStructOpen(func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(tc.tag), tc.value)
			})
			err := tc.decode(dec)
			if err == nil {
				t.Fatalf("value %#x accepted; want a ConstraintError reject", tc.value)
			}
			var sce im.StatusCodeError
			if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusConstraintError {
				t.Errorf("err = %v; want an error carrying StatusConstraintError", err)
			}
		})
	}
}

// viaDefinition decodes the fields after the struct opener through the
// cluster's registered definition, as commandFieldsReader does.
func viaDefinition(cluster, command uint32) func(*tlv.Decoder) error {
	return func(d *tlv.Decoder) error {
		path := im.ConcreteCommandPath{Cluster: cluster, Command: command}
		_, ok, err := decodeByDefinition(path, d, structOpener)
		if !ok {
			return errors.New("no definition decodes the command")
		}
		return err
	}
}

// structOpener is the fields container's opening element.
var structOpener = tlv.Element{Tag: tlv.AnonymousTag(), Type: tlv.TypeStructure, IsContainer: true}

// TestDecodeMoveToColorTemperature_InRangeStillDecodes is the negative
// control for the width check: a value that fits still lands, as the
// generated request the ColorControl server takes.
func TestDecodeMoveToColorTemperature_InRangeStillDecodes(t *testing.T) {
	t.Parallel()
	dec := buildDecoderAfterStructOpen(func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0xFEFF)
		enc.PutUint(tlv.ContextTag(1), 0xFFFE)
		enc.PutUint(tlv.ContextTag(2), 0x01)
		enc.PutUint(tlv.ContextTag(3), 0x01)
	})
	fields, ok, err := decodeByDefinition(im.ConcreteCommandPath{Cluster: 0x0300, Command: 0x0A}, dec, structOpener)
	if !ok || err != nil {
		t.Fatalf("decodeByDefinition: ok %t, %v", ok, err)
	}
	req, isReq := fields.(ccdef.MoveToColorTemperatureRequest)
	if !isReq || req.ColorTemperatureMireds != 0xFEFF || req.TransitionTime != 0xFFFE || req.OptionsMask != 0x01 || req.OptionsOverride != 0x01 {
		t.Errorf("decoded %#v, want the generated request with 0xFEFF / 0xFFFE and the options 0x01", fields)
	}
}

// TestInvokeRequestWithOverWideFieldCarriesConstraintErrorStatus pins
// the end-to-end shape: an InvokeRequest whose command carries an
// out-of-range field still decodes as a request, with that command
// marked ConstraintError and no fields — the dispatcher then answers
// the command with a CommandStatusIB instead of executing it, and the
// rest of the batch is unaffected.
func TestInvokeRequestWithOverWideFieldCarriesConstraintErrorStatus(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false) // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), false) // TimedRequest
	enc.StartArray(tlv.ContextTag(2))     // InvokeRequests
	for _, mireds := range []uint64{0x10099, 153} {
		enc.StartStruct(tlv.AnonymousTag()) // CommandDataIB
		enc.StartList(tlv.ContextTag(0))    // CommandPathIB
		enc.PutUint(tlv.ContextTag(0), 1)   // Endpoint
		enc.PutUint(tlv.ContextTag(1), 0x0300)
		enc.PutUint(tlv.ContextTag(2), 0x0A) // MoveToColorTemperature
		_ = enc.EndContainer()
		enc.StartStruct(tlv.ContextTag(1)) // CommandFields
		enc.PutUint(tlv.ContextTag(0), mireds)
		enc.PutUint(tlv.ContextTag(1), 10)
		enc.PutUint(tlv.ContextTag(2), 0) // OptionsMask
		enc.PutUint(tlv.ContextTag(3), 0) // OptionsOverride
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(wire), commandFieldsReader)
	if err != nil {
		t.Fatalf("UnmarshalInvokeRequestTLV: %v (an out-of-range field must yield a per-command status, not a dropped request)", err)
	}
	if len(req.Invokes) != 2 {
		t.Fatalf("decoded %d invokes, want 2", len(req.Invokes))
	}
	if req.Invokes[0].DecodeStatus != im.StatusConstraintError || req.Invokes[0].Fields != nil {
		t.Errorf("invoke 0: DecodeStatus=%v Fields=%v, want ConstraintError and nil fields", req.Invokes[0].DecodeStatus, req.Invokes[0].Fields)
	}
	if req.Invokes[1].DecodeStatus != im.StatusSuccess || req.Invokes[1].Fields == nil {
		t.Errorf("invoke 1: DecodeStatus=%v Fields=%v, want Success with decoded fields", req.Invokes[1].DecodeStatus, req.Invokes[1].Fields)
	}
}
