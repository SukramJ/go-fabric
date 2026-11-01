// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"errors"
	"fmt"

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
// A field of the wrong type or outside its width is refused ([fieldUint]).
func DecodeMoveToLevel(payload []byte) (MoveToLevelRequest, error) {
	var req MoveToLevelRequest
	err := walkContext(payload, ErrLevelControlMalformed, func(tag uint32, el tlv.Element) (err error) {
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
	err := walkContext(payload, ErrLevelControlMalformed, func(tag uint32, el tlv.Element) (err error) {
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

// DecodeStep parses Step / StepWithOnOff payloads.
func DecodeStep(payload []byte) (StepRequest, error) {
	var req StepRequest
	err := walkContext(payload, ErrLevelControlMalformed, func(tag uint32, el tlv.Element) (err error) {
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
	err := walkContext(payload, ErrLevelControlMalformed, func(tag uint32, el tlv.Element) (err error) {
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
// refuses.
func walkContext(payload []byte, sentinel error, visit func(tag uint32, el tlv.Element) error) error {
	d := tlv.NewDecoder(payload)
	open, err := d.Next()
	if err != nil {
		return fmt.Errorf("%w: top: %w", sentinel, err)
	}
	if open.Type != tlv.TypeStructure {
		return fmt.Errorf("%w: top must be Structure", sentinel)
	}
	for {
		el, err := d.Next()
		if err != nil {
			return fmt.Errorf("%w: %w", sentinel, err)
		}
		if el.IsEndContainer {
			return nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if err := visit(el.Tag.Number, el); err != nil {
			return err
		}
	}
}
