// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"fmt"
	"math"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// fieldError is a command field the hand-written decoders refuse: it
// wraps the package's sentinel (errors.Is still finds it) and carries the
// status the field earns ([im.StatusCodeError]).
type fieldError struct {
	sentinel error
	status   im.StatusCode
	reason   string
}

func (e *fieldError) Error() string { return fmt.Sprintf("%v: %s", e.sentinel, e.reason) }

func (e *fieldError) Unwrap() error { return e.sentinel }

// MatterStatusCode implements [im.StatusCodeError].
func (e *fieldError) MatterStatusCode() im.StatusCode { return e.status }

// fieldTypeError refuses a field whose TLV type is not the one the
// command states: INVALID_ACTION, matter.js's
// ValidationDatatypeMismatchError
// (packages/types/src/common/ValidationError.ts:35-38 at 862ad75f).
func fieldTypeError(el tlv.Element, sentinel error, want string) error {
	return &fieldError{sentinel: sentinel, status: im.StatusInvalidAction, reason: fmt.Sprintf("field %d has TLV type 0x%02X, want %s", el.Tag.Number, uint8(el.Type), want)}
}

// fieldUint is an unsigned integer field of at most maxValue. A value
// above it is refused with CONSTRAINT_ERROR rather than masked into a
// different, valid value — matter.js packages/types/src/tlv/TlvNumber.ts:145-156
// at 862ad75f (validate: a non-number is ValidationDatatypeMismatchError,
// one out of bounds ValidationOutOfBoundsError, a ValidationError =
// CONSTRAINT_ERROR); the same rule the Interaction Model applies to its
// path fields (W3-clucore-08).
func fieldUint(el tlv.Element, maxValue uint64, sentinel error) (uint64, error) {
	switch el.Type {
	case tlv.TypeUnsignedInt1, tlv.TypeUnsignedInt2, tlv.TypeUnsignedInt4, tlv.TypeUnsignedInt8:
	default:
		return 0, fieldTypeError(el, sentinel, "an unsigned integer")
	}
	if el.Uint > maxValue {
		return 0, &fieldError{sentinel: sentinel, status: im.StatusConstraintError, reason: fmt.Sprintf("field %d is %d, above %d", el.Tag.Number, el.Uint, maxValue)}
	}
	return el.Uint, nil
}

// fieldUint8 is [fieldUint] bounded by the uint8 range.
func fieldUint8(el tlv.Element, sentinel error) (uint8, error) {
	v, err := fieldUint(el, math.MaxUint8, sentinel)
	if err != nil || v > math.MaxUint8 {
		return 0, err
	}
	return uint8(v), nil
}

// fieldUint16 is [fieldUint] bounded by the uint16 range.
func fieldUint16(el tlv.Element, sentinel error) (uint16, error) {
	v, err := fieldUint(el, math.MaxUint16, sentinel)
	if err != nil || v > math.MaxUint16 {
		return 0, err
	}
	return uint16(v), nil
}

// nullableUint16 is a nullable [fieldUint16]: nil for null.
func nullableUint16(el tlv.Element, sentinel error) (*uint16, error) {
	if el.IsNull {
		return nil, nil
	}
	v, err := fieldUint16(el, sentinel)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
