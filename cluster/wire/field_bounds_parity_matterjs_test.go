// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestParityMatterJS_DecoderFieldBounds: the hand-written command decoders
// refuse a field above its width with CONSTRAINT_ERROR and one of the
// wrong TLV type with INVALID_ACTION, where they used to mask the value
// into a different, valid one — matter.js
// packages/types/src/tlv/TlvNumber.ts:145-156 and
// packages/types/src/common/ValidationError.ts:35-38 at 862ad75f
// (W3-clucore-08). The sentinel stays reachable through errors.Is.
func TestParityMatterJS_DecoderFieldBounds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		build    func(*tlv.Encoder)
		decode   func([]byte) error
		sentinel error
		want     im.StatusCode
	}{
		{
			"MoveToLevel Level 300", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 300) },
			func(p []byte) error { _, err := wire.DecodeMoveToLevel(p); return err }, wire.ErrLevelControlMalformed, im.StatusConstraintError,
		},
		{
			"MoveToLevel TransitionTime 70000", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 1); e.PutUint(tlv.ContextTag(1), 70000) },
			func(p []byte) error { _, err := wire.DecodeMoveToLevel(p); return err }, wire.ErrLevelControlMalformed, im.StatusConstraintError,
		},
		{
			"MoveToLevel Level as a string", func(e *tlv.Encoder) { e.PutUTF8(tlv.ContextTag(0), "x") },
			func(p []byte) error { _, err := wire.DecodeMoveToLevel(p); return err }, wire.ErrLevelControlMalformed, im.StatusInvalidAction,
		},
		{
			"Step StepSize 256", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 0); e.PutUint(tlv.ContextTag(1), 256) },
			func(p []byte) error { _, err := wire.DecodeStep(p); return err }, wire.ErrLevelControlMalformed, im.StatusConstraintError,
		},
		{
			"OnWithTimedOff OnTime 65536", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 0); e.PutUint(tlv.ContextTag(1), 65536) },
			func(p []byte) error { _, err := wire.DecodeOnWithTimedOff(p); return err }, wire.ErrOnOffMalformed, im.StatusConstraintError,
		},
		{
			"OffWithEffect EffectVariant 256", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 0); e.PutUint(tlv.ContextTag(1), 256) },
			func(p []byte) error { _, err := wire.DecodeOffWithEffect(p); return err }, wire.ErrOnOffMalformed, im.StatusConstraintError,
		},
		{
			"GoToLiftValue 65536", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 65536) },
			func(p []byte) error { _, err := wire.DecodeGoToLiftValue(p); return err }, wire.ErrWindowCoveringMalformed, im.StatusConstraintError,
		},
		{
			"LockDoor PINCode as an integer", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 1234) },
			func(p []byte) error { _, err := wire.DecodeDoorLockRequest(p); return err }, wire.ErrDoorLockMalformed, im.StatusInvalidAction,
		},
	} {
		err := c.decode(buildPayload(t, c.build))
		if !errors.Is(err, c.sentinel) {
			t.Errorf("%s: %v, want %v", c.name, err, c.sentinel)
		}
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != c.want {
			t.Errorf("%s: %v, want status 0x%02X", c.name, err, c.want)
		}
	}
	// The widest value still decodes.
	got, err := wire.DecodeMoveToLevel(buildPayload(t, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 254); e.PutUint(tlv.ContextTag(1), 65535) }))
	if err != nil || got.Level != 254 || got.TransitionTime == nil || *got.TransitionTime != 65535 {
		t.Errorf("MoveToLevel 254 / 65535: %+v, %v", got, err)
	}
}
