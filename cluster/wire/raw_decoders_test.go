// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// rawPayload is a structure with a uint 1 under each of tags, plus
// whatever extra writes after them.
func rawPayload(t *testing.T, tags []uint8, extra func(*tlv.Encoder)) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	for _, tag := range tags {
		enc.PutUint(tlv.ContextTag(tag), 1)
	}
	if extra != nil {
		extra(enc)
	}
	if err := enc.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rawDecoders are the hand-written raw-payload decoders beside the
// generated command they decode, and the command's context tags.
var rawDecoders = []struct {
	name    string
	tags    []uint8
	raw     func([]byte) error
	def     *spec.Cluster
	command uint32
}{
	{"MoveToLevel", []uint8{0, 1, 2, 3}, func(b []byte) error { _, err := wire.DecodeMoveToLevel(b); return err }, lvldef.Definition, lvldef.CmdMoveToLevel},
	{"Move", []uint8{0, 1, 2, 3}, func(b []byte) error { _, err := wire.DecodeMove(b); return err }, lvldef.Definition, lvldef.CmdMove},
	{"Step", []uint8{0, 1, 2, 3, 4}, func(b []byte) error { _, err := wire.DecodeStep(b); return err }, lvldef.Definition, lvldef.CmdStep},
	{"Stop", []uint8{0, 1}, func(b []byte) error { _, err := wire.DecodeStop(b); return err }, lvldef.Definition, lvldef.CmdStop},
	{"MoveToLevelWithOnOff", []uint8{0, 1, 2, 3}, func(b []byte) error { _, err := wire.DecodeMoveToLevel(b); return err }, lvldef.Definition, lvldef.CmdMoveToLevelWithOnOff},
	{"MoveWithOnOff", []uint8{0, 1, 2, 3}, func(b []byte) error { _, err := wire.DecodeMove(b); return err }, lvldef.Definition, lvldef.CmdMoveWithOnOff},
	{"StepWithOnOff", []uint8{0, 1, 2, 3, 4}, func(b []byte) error { _, err := wire.DecodeStep(b); return err }, lvldef.Definition, lvldef.CmdStepWithOnOff},
	{"StopWithOnOff", []uint8{0, 1}, func(b []byte) error { _, err := wire.DecodeStop(b); return err }, lvldef.Definition, lvldef.CmdStopWithOnOff},
	{"OffWithEffect", []uint8{0, 1}, func(b []byte) error { _, err := wire.DecodeOffWithEffect(b); return err }, onoffdef.Definition, onoffdef.CmdOffWithEffect},
	{"OnWithTimedOff", []uint8{0, 1, 2}, func(b []byte) error { _, err := wire.DecodeOnWithTimedOff(b); return err }, onoffdef.Definition, onoffdef.CmdOnWithTimedOff},
}

// TestRawDecodersRefuseWhatTheGeneratedOnesRefuse (review II,
// R2-P6-wire-1209): each raw decoder refuses a payload that lacks a field
// exactly when the generated decoder of its command does — a mandatory
// field — and answers the generated decoders' INVALID_COMMAND. The
// standing guard of the class: a raw decoder that drifts from the
// generated definition's mandatory fields fails here.
func TestRawDecodersRefuseWhatTheGeneratedOnesRefuse(t *testing.T) {
	t.Parallel()
	for _, d := range rawDecoders {
		cmd := d.def.Command(d.command, spec.Request)
		check := func(label string, tags []uint8) {
			b := rawPayload(t, tags, nil)
			_, genErr := cmd.Decode(spectest.Decode(t, b))
			rawErr := d.raw(b)
			if (genErr == nil) != (rawErr == nil) {
				t.Errorf("%s %s: raw %v, generated %v", d.name, label, rawErr, genErr)
				return
			}
			var se im.StatusCodeError
			if rawErr != nil && (!errors.As(rawErr, &se) || se.MatterStatusCode() != im.StatusInvalidCommand) {
				t.Errorf("%s %s: raw %v, want INVALID_COMMAND", d.name, label, rawErr)
			}
		}
		check("whole", d.tags)
		check("empty", nil)
		for i, tag := range d.tags {
			rest := append(append([]uint8{}, d.tags[:i]...), d.tags[i+1:]...)
			check("without "+string(rune('0'+tag)), rest)
		}
	}
}

// TestWalkContextDrainsAnIgnoredContainer (review II, R2-P6-wire-1208): a
// container under a context tag the decoder does not read is skipped
// whole — its children are not taken for the command's fields, and the
// fields after it are read.
func TestWalkContextDrainsAnIgnoredContainer(t *testing.T) {
	t.Parallel()
	nested := func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.ContextTag(9))
		enc.PutUint(tlv.ContextTag(0), 200)
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(7), 7)
	}
	b := rawPayload(t, nil, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 10)
		enc.PutNull(tlv.ContextTag(1))
		nested(enc)
		enc.PutUint(tlv.ContextTag(2), 7)
		enc.PutUint(tlv.ContextTag(3), 0)
	})
	req, err := wire.DecodeMoveToLevel(b)
	if err != nil {
		t.Fatal(err)
	}
	if req.Level != 10 || req.OptionsMask != 7 {
		t.Errorf("MoveToLevel %+v, want Level 10 and OptionsMask 7", req)
	}
	pin := rawPayload(t, nil, func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.ContextTag(5))
		enc.PutOctets(tlv.ContextTag(0), []byte("1234"))
		_ = enc.EndContainer()
	})
	lock, err := wire.DecodeDoorLockRequest(pin)
	if err != nil {
		t.Fatal(err)
	}
	if lock.PinCode != nil {
		t.Errorf("PinCode %q taken from a nested container", lock.PinCode)
	}
}
