// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"

	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// decodeChangeToModeRequest reads a ModeBase ChangeToMode
// (mode-base.element.ts:44-50): [0] NewMode uint8, mandatory. A missing
// NewMode or one that is not an unsigned integer is InvalidCommand, as
// matter.js answers a payload its request schema rejects; a value wider
// than uint8 is ConstraintError (TlvUInt8). Whether the mode is supported
// is the server's to answer, in the response.
func decodeChangeToModeRequest(dec *tlv.Decoder) (clusterwire.ChangeToModeRequest, error) {
	const cmd = "ChangeToMode"
	var req clusterwire.ChangeToModeRequest
	have := false
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			if !have {
				return req, fieldInvalidCommandError{msg: cmd + ": mandatory field NewMode missing", consumed: true}
			}
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number != uint32(clusterwire.ChangeToModeFieldNewMode) {
			if err := skipValue(dec, el); err != nil {
				return req, fmt.Errorf("%s: %w", cmd, err)
			}
			continue
		}
		if !isUnsignedInt(el) {
			_ = skipValue(dec, el)
			return req, fieldInvalidCommandError{msg: fmt.Sprintf("%s: NewMode is not a uint8 (type=0x%02X)", cmd, el.Type)}
		}
		if req.NewMode, err = fieldUint8(cmd, "NewMode", el); err != nil {
			return req, err
		}
		have = true
	}
}
