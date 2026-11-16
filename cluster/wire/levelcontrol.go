// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// LevelControl command IDs per Matter §1.6.7.
const (
	LevelCtrlCmdMoveToLevel            uint32 = 0x00
	LevelCtrlCmdMove                   uint32 = 0x01
	LevelCtrlCmdStep                   uint32 = 0x02
	LevelCtrlCmdStop                   uint32 = 0x03
	LevelCtrlCmdMoveToLevelWithOnOff   uint32 = 0x04
	LevelCtrlCmdMoveWithOnOff          uint32 = 0x05
	LevelCtrlCmdStepWithOnOff          uint32 = 0x06
	LevelCtrlCmdStopWithOnOff          uint32 = 0x07
	LevelCtrlCmdMoveToClosestFrequency uint32 = 0x08
)

// MoveMode values per Matter §1.6.7.2.
const (
	LevelMoveModeUp   uint8 = 0
	LevelMoveModeDown uint8 = 1
)

// StepMode values per Matter §1.6.7.3.
const (
	LevelStepModeUp   uint8 = 0
	LevelStepModeDown uint8 = 1
)

// ErrLevelControlMalformed is returned for malformed LevelControl
// command payloads.
var ErrLevelControlMalformed = errors.New("wire: LevelControl command malformed")

// MoveToLevelRequest mirrors Matter §1.6.7.1.
type MoveToLevelRequest struct {
	Level           uint8
	TransitionTime  *uint16 // nullable
	OptionsMask     uint8
	OptionsOverride uint8
}

// MoveRequest mirrors Matter §1.6.7.2.
type MoveRequest struct {
	MoveMode        uint8
	Rate            *uint8 // nullable
	OptionsMask     uint8
	OptionsOverride uint8
}

// StepRequest mirrors Matter §1.6.7.3.
type StepRequest struct {
	StepMode        uint8
	StepSize        uint8
	TransitionTime  *uint16 // nullable
	OptionsMask     uint8
	OptionsOverride uint8
}

// StopRequest mirrors Matter §1.6.7.4.
type StopRequest struct {
	OptionsMask     uint8
	OptionsOverride uint8
}

// DecodeMoveToLevel parses MoveToLevel / MoveToLevelWithOnOff payloads.
// A field of the wrong type or outside its width is refused ([fieldUint]);
// a payload without one of its four fields — TransitionTime included, a
// mandatory nullable field — is refused INVALID_COMMAND. Mirrors matter.js:
// decode leaves the member unset (TlvObject.ts:205-251), then
// requestTlv.validate (CommandInvokeResponse.ts:486) throws
// ValidationMandatoryFieldMissingError (TlvObject.ts:257-261), answered
// INVALID_COMMAND (CommandInvokeResponse.ts:529-535 at db021da0); review
// II, R2-P6-wire-1209.
func DecodeMoveToLevel(payload []byte) (MoveToLevelRequest, error) {
	var req MoveToLevelRequest
	err := walkContext(payload, ErrLevelControlMalformed, []uint32{0, 1, 2, 3}, func(tag uint32, el tlv.Element) (err error) {
		s := ErrLevelControlMalformed
		switch tag {
		case 0:
			req.Level, err = fieldUint8(el, s)
		case 1:
			req.TransitionTime, err = nullableUint16(el, s)
		case 2:
			req.OptionsMask, err = fieldUint8(el, s)
		case 3:
			req.OptionsOverride, err = fieldUint8(el, s)
		}
		return err
	})
	return req, err
}

// DecodeMove parses Move / MoveWithOnOff payloads.
func DecodeMove(payload []byte) (MoveRequest, error) {
	var req MoveRequest
	err := walkContext(payload, ErrLevelControlMalformed, []uint32{0, 1, 2, 3}, func(tag uint32, el tlv.Element) (err error) {
		s := ErrLevelControlMalformed
		switch tag {
		case 0:
			req.MoveMode, err = fieldUint8(el, s)
		case 1:
			if el.IsNull {
				return nil
			}
			var v uint8
			v, err = fieldUint8(el, s)
			req.Rate = &v
		case 2:
			req.OptionsMask, err = fieldUint8(el, s)
		case 3:
			req.OptionsOverride, err = fieldUint8(el, s)
		}
		return err
	})
	return req, err
}

// DecodeStep parses Step / StepWithOnOff payloads. A payload without one
// of its five fields — TransitionTime included — is refused
// INVALID_COMMAND, as [DecodeMoveToLevel] (R2-P6-wire-1209).
func DecodeStep(payload []byte) (StepRequest, error) {
	var req StepRequest
	err := walkContext(payload, ErrLevelControlMalformed, []uint32{0, 1, 2, 3, 4}, func(tag uint32, el tlv.Element) (err error) {
		s := ErrLevelControlMalformed
		switch tag {
		case 0:
			req.StepMode, err = fieldUint8(el, s)
		case 1:
			req.StepSize, err = fieldUint8(el, s)
		case 2:
			req.TransitionTime, err = nullableUint16(el, s)
		case 3:
			req.OptionsMask, err = fieldUint8(el, s)
		case 4:
			req.OptionsOverride, err = fieldUint8(el, s)
		}
		return err
	})
	return req, err
}

// DecodeStop parses Stop / StopWithOnOff payloads.
func DecodeStop(payload []byte) (StopRequest, error) {
	var req StopRequest
	err := walkContext(payload, ErrLevelControlMalformed, []uint32{0, 1}, func(tag uint32, el tlv.Element) (err error) {
		s := ErrLevelControlMalformed
		switch tag {
		case 0:
			req.OptionsMask, err = fieldUint8(el, s)
		case 1:
			req.OptionsOverride, err = fieldUint8(el, s)
		}
		return err
	})
	return req, err
}

// walkContext is the common Structure-walker for context-tagged
// payloads. It opens the top struct, calls visit(tag, element) for
// each child, and stops at EndContainer or at the first field visit
// refuses. A container child — one visit did not refuse, as no visitor
// reads into one — is skipped whole, so its members are never taken for
// the command's fields (review II, R2-P6-wire-1208). A payload without
// one of the mandatory tags is refused INVALID_COMMAND, as the generated
// decoders refuse it (cluster/spec/codec.go, decodeFields; review II,
// R2-P6-wire-1209).
func walkContext(payload []byte, sentinel error, mandatory []uint32, visit func(tag uint32, el tlv.Element) error) error {
	d := tlv.NewDecoder(payload)
	open, err := d.Next()
	if err != nil {
		return fmt.Errorf("%w: top: %w", sentinel, err)
	}
	if open.Type != tlv.TypeStructure {
		return fmt.Errorf("%w: top must be Structure", sentinel)
	}
	var seen []uint32
	for {
		el, err := d.Next()
		if err != nil {
			return fmt.Errorf("%w: %w", sentinel, err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind == tlv.TagKindContext {
			if err := visit(el.Tag.Number, el); err != nil {
				return err
			}
			seen = append(seen, el.Tag.Number)
		}
		if el.IsContainer {
			if err := skipMembers(d); err != nil {
				return fmt.Errorf("%w: %w", sentinel, err)
			}
		}
	}
	for _, tag := range mandatory {
		if !slices.Contains(seen, tag) {
			return &fieldError{sentinel: sentinel, status: im.StatusInvalidCommand, reason: fmt.Sprintf("mandatory field %d missing", tag)}
		}
	}
	return nil
}

// skipMembers consumes the members of the container whose opener d just
// read, through its end marker.
func skipMembers(d *tlv.Decoder) error {
	for depth := 1; depth > 0; {
		el, err := d.Next()
		if err != nil {
			return err
		}
		switch {
		case el.IsEndContainer:
			depth--
		case el.IsContainer:
			depth++
		}
	}
	return nil
}
