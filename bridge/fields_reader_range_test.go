// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for the width checks in the command-field decoders: a
// wire value that does not fit the field's schema type is rejected, not
// truncated into a different, unrequested value. Lives in package
// bridge to reach the decoders directly.

import (
	"errors"
	"testing"

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
		{"MoveToColorTemperature.ColorTemperatureMireds", 0, 0x10099, func(d *tlv.Decoder) error { _, err := decodeMoveToColorTemperatureFields(d); return err }},
		{"MoveToColorTemperature.TransitionTime", 1, 0x10000, func(d *tlv.Decoder) error { _, err := decodeMoveToColorTemperatureFields(d); return err }},
		{"MoveToHue.Hue", 0, 0x100, func(d *tlv.Decoder) error { _, err := decodeMoveToHueFields(d); return err }},
		{"MoveToHue.Direction", 1, 0x100, func(d *tlv.Decoder) error { _, err := decodeMoveToHueFields(d); return err }},
		{"MoveToHue.TransitionTime", 2, 0x10000, func(d *tlv.Decoder) error { _, err := decodeMoveToHueFields(d); return err }},
		{"MoveToSaturation.Saturation", 0, 0x100, func(d *tlv.Decoder) error { _, err := decodeMoveToSaturationFields(d); return err }},
		{"MoveToSaturation.TransitionTime", 1, 0x10000, func(d *tlv.Decoder) error { _, err := decodeMoveToSaturationFields(d); return err }},
		{"MoveToHueAndSaturation.Hue", 0, 0x100, func(d *tlv.Decoder) error { _, err := decodeMoveToHueAndSaturationFields(d); return err }},
		{"MoveToHueAndSaturation.Saturation", 1, 0x100, func(d *tlv.Decoder) error { _, err := decodeMoveToHueAndSaturationFields(d); return err }},
		{"MoveToHueAndSaturation.TransitionTime", 2, 0x10000, func(d *tlv.Decoder) error { _, err := decodeMoveToHueAndSaturationFields(d); return err }},
		{"MoveToLevel.TransitionTime", 1, 0x10000, func(d *tlv.Decoder) error { _, err := decodeMoveToLevelRequest(d); return err }},
		{"MoveToLevel.OptionsMask", 2, 0x100, func(d *tlv.Decoder) error { _, err := decodeMoveToLevelRequest(d); return err }},
		{"MoveToLevel.OptionsOverride", 3, 0x100, func(d *tlv.Decoder) error { _, err := decodeMoveToLevelRequest(d); return err }},
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

// TestDecodeMoveToColorTemperatureFields_InRangeStillDecodes is the
// negative control for the width check: a value that fits still lands.
func TestDecodeMoveToColorTemperatureFields_InRangeStillDecodes(t *testing.T) {
	t.Parallel()
	dec := buildDecoderAfterStructOpen(func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0xFFFF)
		enc.PutUint(tlv.ContextTag(1), 0xFFFF)
	})
	req, err := decodeMoveToColorTemperatureFields(dec)
	if err != nil {
		t.Fatalf("decodeMoveToColorTemperatureFields: %v", err)
	}
	if req.ColorTemperatureMireds != 0xFFFF || req.TransitionTime != 0xFFFF {
		t.Errorf("decoded %+v, want both fields 0xFFFF", req)
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
