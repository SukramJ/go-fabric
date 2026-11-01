// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"errors"

	"github.com/SukramJ/go-fabric/tlv"
)

// OnOff command IDs per Matter §1.5.6.
const (
	OnOffCmdOff                     uint32 = 0x00
	OnOffCmdOn                      uint32 = 0x01
	OnOffCmdToggle                  uint32 = 0x02
	OnOffCmdOffWithEffect           uint32 = 0x40
	OnOffCmdOnWithRecallGlobalScene uint32 = 0x41
	OnOffCmdOnWithTimedOff          uint32 = 0x42
)

// Errors.
var (
	// ErrOnOffMalformed is returned for malformed OnOff command payloads.
	ErrOnOffMalformed = errors.New("wire: OnOff command malformed")
)

// OffWithEffectRequest mirrors Matter §1.5.7.4.
type OffWithEffectRequest struct {
	EffectIdentifier uint8
	EffectVariant    uint8
}

// OnWithTimedOffRequest mirrors Matter §1.5.7.6.
type OnWithTimedOffRequest struct {
	OnOffControl uint8  // bit 0 = AcceptOnlyWhenOn
	OnTime       uint16 // 1/10s units
	OffWaitTime  uint16 // 1/10s units
}

// DecodeOffWithEffect parses the OffWithEffect command payload. A field
// of the wrong type or outside its width is refused ([fieldUint]).
func DecodeOffWithEffect(payload []byte) (OffWithEffectRequest, error) {
	var req OffWithEffectRequest
	err := walkContext(payload, ErrOnOffMalformed, func(tag uint32, el tlv.Element) (err error) {
		switch tag {
		case 0:
			req.EffectIdentifier, err = fieldUint8(el, ErrOnOffMalformed)
		case 1:
			req.EffectVariant, err = fieldUint8(el, ErrOnOffMalformed)
		}
		return err
	})
	return req, err
}

// DecodeOnWithTimedOff parses the OnWithTimedOff command payload.
func DecodeOnWithTimedOff(payload []byte) (OnWithTimedOffRequest, error) {
	var req OnWithTimedOffRequest
	err := walkContext(payload, ErrOnOffMalformed, func(tag uint32, el tlv.Element) (err error) {
		switch tag {
		case 0:
			req.OnOffControl, err = fieldUint8(el, ErrOnOffMalformed)
		case 1:
			req.OnTime, err = fieldUint16(el, ErrOnOffMalformed)
		case 2:
			req.OffWaitTime, err = fieldUint16(el, ErrOnOffMalformed)
		}
		return err
	})
	return req, err
}
