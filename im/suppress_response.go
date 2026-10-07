// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"github.com/SukramJ/go-fabric/tlv"
)

// SuppressResponseOf reads only the SuppressResponse field (context tag 0)
// of an InvokeRequestMessage or WriteRequestMessage whose full decode
// failed, so the error a failed request raises can still be suppressed.
// It mirrors matter.js InteractionMessenger.ts suppressResponseOf
// (TlvObject({ suppressResponse: TlvOptionalField(0, TlvBoolean) })): the
// rest of the structure may break the request's schema, but malformed TLV
// anywhere in it, or a tag 0 that is not a boolean, reads as "not
// suppressed" — unknown means the action is answered.
func SuppressResponseOf(payload []byte) bool {
	dec := tlv.NewDecoder(payload)
	open, err := dec.Next()
	if err != nil || !open.IsContainer || open.Type != tlv.TypeStructure {
		return false
	}
	suppress := false
	depth := 0
	for {
		el, err := dec.Next()
		if err != nil {
			return false
		}
		if el.IsEndContainer {
			if depth == 0 {
				return suppress
			}
			depth--
			continue
		}
		if depth == 0 && el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == uint32(tagInvokeReqSuppressResponse) {
			if el.Type != tlv.TypeBoolTrue && el.Type != tlv.TypeBoolFalse {
				return false
			}
			suppress = el.Bool
		}
		if el.IsContainer {
			depth++
		}
	}
}
