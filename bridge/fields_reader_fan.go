// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"

	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// decodeFanStepRequest reads FanControl Step (fan-control.element.ts:69-74):
// [0] StepDirectionEnum Direction (mandatory), [1] bool Wrap (optional,
// default false), [2] bool LowestOff (optional, default true). A missing
// Direction or a field of the wrong TLV type is InvalidCommand, as
// matter.js answers a payload its request schema rejects; the enum's
// membership is the server's to check.
func decodeFanStepRequest(dec *tlv.Decoder) (clusterwire.FanStepRequest, error) {
	const cmd = "Step"
	req := clusterwire.FanStepRequest{LowestOff: true}
	haveDirection := false
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			if !haveDirection {
				return req, fieldInvalidCommandError{msg: cmd + ": mandatory field Direction missing", consumed: true}
			}
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if err := skipValue(dec, el); err != nil {
				return req, fmt.Errorf("%s: %w", cmd, err)
			}
			continue
		}
		switch el.Tag.Number {
		case uint32(clusterwire.FanStepFieldDirection):
			if !isUnsignedInt(el) {
				_ = skipValue(dec, el)
				return req, fieldInvalidCommandError{msg: fmt.Sprintf("%s: Direction is not an enum (type=0x%02X)", cmd, el.Type)}
			}
			if req.Direction, err = fieldUint8(cmd, "Direction", el); err != nil {
				return req, err
			}
			haveDirection = true
		case uint32(clusterwire.FanStepFieldWrap), uint32(clusterwire.FanStepFieldLowestOff):
			if el.Type != tlv.TypeBoolFalse && el.Type != tlv.TypeBoolTrue {
				_ = skipValue(dec, el)
				return req, fieldInvalidCommandError{msg: fmt.Sprintf("%s: field %d is not a bool (type=0x%02X)", cmd, el.Tag.Number, el.Type)}
			}
			if el.Tag.Number == uint32(clusterwire.FanStepFieldWrap) {
				req.Wrap = el.Bool
			} else {
				req.LowestOff = el.Bool
			}
		default:
			if err := skipValue(dec, el); err != nil {
				return req, fmt.Errorf("%s: %w", cmd, err)
			}
		}
	}
}
