// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"fmt"
	"math"

	"github.com/SukramJ/go-fabric/tlv"
)

// FieldError is a request-decode error that names the IM status it
// answers: InvalidAction for a field of the wrong TLV type or a missing
// mandatory field, ConstraintError for an integer outside its type's
// range. It is an [StatusCodeError], so the bridge's decode-error reply
// answers its status rather than Failure.
type FieldError struct {
	// Status is the IM status the request is answered with.
	Status StatusCode
	// Tag is the context tag of the field.
	Tag uint8
	// Reason says what is wrong with the field.
	Reason string
}

// Error implements error.
func (e *FieldError) Error() string {
	return fmt.Sprintf("im: field %d: %s", e.Tag, e.Reason)
}

// MatterStatusCode implements [StatusCodeError].
func (e *FieldError) MatterStatusCode() StatusCode { return e.Status }

// ErrMissingField is the error a request decoder returns, wrapped with
// the field's name, when a mandatory field is absent; it answers
// InvalidAction. matter.js packages/types/src/tlv/TlvObject.ts:258
// throws ValidationMandatoryFieldMissingError (InvalidAction,
// packages/types/src/common/ValidationError.ts:45-48); chip's parsers
// return CHIP_END_OF_TLV, which src/app/ReadHandler.cpp:208-214 answers
// InvalidAction.
var ErrMissingField StatusCodeError = missingFieldError{}

type missingFieldError struct{}

func (missingFieldError) Error() string                { return "im: mandatory field missing" }
func (missingFieldError) MatterStatusCode() StatusCode { return StatusInvalidAction }

// Is lets errors.Is match [ErrMissingField] by value.
func (missingFieldError) Is(target error) bool {
	_, ok := target.(missingFieldError)
	return ok
}

// missingField wraps [ErrMissingField] with the field's name and tag.
func missingField(tag uint8, name string) error {
	return fmt.Errorf("%w: %s (tag %d)", ErrMissingField, name, tag)
}

// boolField returns el's value when el is a boolean, and an InvalidAction
// [FieldError] otherwise (chip src/app/MessageDef/Parser.h:113-123, as
// for [uintField]).
func boolField(el tlv.Element) (bool, error) {
	if el.Type != tlv.TypeBoolFalse && el.Type != tlv.TypeBoolTrue {
		return false, &FieldError{Status: StatusInvalidAction, Tag: fieldTag(el), Reason: fmt.Sprintf("TLV type 0x%02X, want a boolean", uint8(el.Type))}
	}
	return el.Bool, nil
}

// uintField returns el's value when el is an unsigned integer of at most
// maxValue. A path or header integer is never masked into range: an
// out-of-range value would become a different, valid path.
//
// The range check mirrors matter.js
// packages/types/src/tlv/TlvNumber.ts:151-155 (out of bounds →
// ValidationOutOfBoundsError, a ValidationError = ConstraintError); the
// type check mirrors chip src/app/MessageDef/Parser.h:113-123
// (GetSimpleValue → CHIP_ERROR_WRONG_TLV_TYPE) as
// src/app/MessageDef/AttributePathIB.cpp:220-275 turns it into
// InvalidAction.
func uintField(el tlv.Element, maxValue uint64) (uint64, error) {
	switch el.Type {
	case tlv.TypeUnsignedInt1, tlv.TypeUnsignedInt2, tlv.TypeUnsignedInt4, tlv.TypeUnsignedInt8:
	default:
		return 0, &FieldError{Status: StatusInvalidAction, Tag: fieldTag(el), Reason: fmt.Sprintf("TLV type 0x%02X, want an unsigned integer", uint8(el.Type))}
	}
	if el.Uint > maxValue {
		return 0, &FieldError{Status: StatusConstraintError, Tag: fieldTag(el), Reason: fmt.Sprintf("%d exceeds %d", el.Uint, maxValue)}
	}
	return el.Uint, nil
}

// uint16Field is [uintField] bounded by the uint16 range.
func uint16Field(el tlv.Element) (uint16, error) {
	v, err := uintField(el, math.MaxUint16)
	if err != nil || v > math.MaxUint16 {
		return 0, err
	}
	return uint16(v), nil
}

// uint32Field is [uintField] bounded by the uint32 range.
func uint32Field(el tlv.Element) (uint32, error) {
	v, err := uintField(el, math.MaxUint32)
	if err != nil || v > math.MaxUint32 {
		return 0, err
	}
	return uint32(v), nil
}

// uint64Field is [uintField] over the whole uint64 range: the type check
// alone.
func uint64Field(el tlv.Element) (uint64, error) {
	return uintField(el, math.MaxUint64)
}

// skipField drains el when it opens a container, so an ignored field
// never desynchronises the decoder.
func skipField(dec *tlv.Decoder, el tlv.Element) error {
	if el.IsContainer {
		return skipContainer(dec)
	}
	return nil
}

func fieldTag(el tlv.Element) uint8 {
	return uint8(el.Tag.Number & 0xFF) //nolint:gosec // context tags are one byte
}
