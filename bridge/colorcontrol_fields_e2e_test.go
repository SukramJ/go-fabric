// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"testing"

	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// readColorFields runs commandFieldsReader on a ColorControl payload built
// by fields.
func readColorFields(t *testing.T, command uint32, fields func(*tlv.Encoder)) (any, error) {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	fields(enc)
	_ = enc.EndContainer()
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	dec := tlv.NewDecoder(raw)
	opener, err := dec.Next()
	if err != nil {
		t.Fatalf("dec.Next (opener): %v", err)
	}
	return commandFieldsReader(im.ConcreteCommandPath{Cluster: ccdef.ClusterID, Command: command}, dec, opener)
}

// TestCommandFieldsReader_ColorControl_MoveToHueAndSaturation proves that a
// real TLV-encoded MoveToHueAndSaturation payload decodes through the
// generated definition into the generated request the ColorControl server
// takes (one decoder, gate 1 G1-2), and that a payload missing a mandatory
// field is INVALID_COMMAND, as matter.js's request schema answers it
// (CommandInvokeResponse.ts, ValidationMandatoryFieldMissingError) — the
// hand-written decoder read it as 0.
func TestCommandFieldsReader_ColorControl_MoveToHueAndSaturation(t *testing.T) {
	t.Parallel()
	got, err := readColorFields(t, ccdef.CmdMoveToHueAndSaturation, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 127) // Hue
		enc.PutUint(tlv.ContextTag(1), 200) // Saturation
		enc.PutUint(tlv.ContextTag(2), 10)  // TransitionTime
		enc.PutUint(tlv.ContextTag(3), 0)   // OptionsMask
		enc.PutUint(tlv.ContextTag(4), 0)   // OptionsOverride
	})
	if err != nil {
		t.Fatalf("commandFieldsReader: %v", err)
	}
	req, ok := got.(ccdef.MoveToHueAndSaturationRequest)
	if !ok || req.Hue != 127 || req.Saturation != 200 || req.TransitionTime != 10 {
		t.Fatalf("decoded %#v, want the generated request 127 / 200 / 10", got)
	}

	_, err = readColorFields(t, ccdef.CmdMoveToHueAndSaturation, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 127)
		enc.PutUint(tlv.ContextTag(1), 200)
		enc.PutUint(tlv.ContextTag(2), 10)
	})
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("without OptionsMask/OptionsOverride: %v, want INVALID_COMMAND", err)
	}
}

// TestCommandFieldsReader_ColorControl_MoveToColorTemperature proves that a
// real TLV-encoded MoveToColorTemperature payload decodes into the
// generated request.
func TestCommandFieldsReader_ColorControl_MoveToColorTemperature(t *testing.T) {
	t.Parallel()
	got, err := readColorFields(t, ccdef.CmdMoveToColorTemperature, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 370) // ColorTemperatureMireds
		enc.PutUint(tlv.ContextTag(1), 0)   // TransitionTime
		enc.PutUint(tlv.ContextTag(2), 0)   // OptionsMask
		enc.PutUint(tlv.ContextTag(3), 0)   // OptionsOverride
	})
	if err != nil {
		t.Fatalf("commandFieldsReader: %v", err)
	}
	if req, ok := got.(ccdef.MoveToColorTemperatureRequest); !ok || req.ColorTemperatureMireds != 370 {
		t.Fatalf("decoded %#v, want the generated request with 370 mireds", got)
	}
}
