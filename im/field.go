// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"fmt"
	"math"

	"github.com/SukramJ/go-fabric/tlv"
)

// FieldError is a request-decode error that names the IM status it
// answers: Failure for an integer field of the wrong TLV type,
// InvalidAction for a boolean field of the wrong TLV type on the response
// side ([boolField]), ConstraintError for an integer outside its type's
// range. It is an [StatusCodeError], so the bridge's decode-error reply
// answers its status.
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
// InvalidAction. matter.js packages/types/src/tlv/TlvObject.ts:261
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
// [FieldError] otherwise (chip src/app/MessageDef/Parser.h:113-123).
func boolField(el tlv.Element) (bool, error) {
	if el.Type != tlv.TypeBoolFalse && el.Type != tlv.TypeBoolTrue {
		return false, &FieldError{Status: StatusInvalidAction, Tag: fieldTag(el), Reason: fmt.Sprintf("TLV type 0x%02X, want a boolean", uint8(el.Type))}
	}
	return el.Bool, nil
}

// requestBoolField returns el's value when el is a boolean. A request
// field of another TLV type fails the decode with a plain error, which
// carries no status, so the bridge answers it FAILURE
// (bridge.replyDecodeError). Mirrors matter.js at db021da0:
// packages/types/src/tlv/TlvBoolean.ts:22-24 throws UnexpectedDataError for
// any element that is not a boolean, TlvObject.ts:222 decodes every known
// field through it, and InteractionMessenger.handleRequest
// (packages/protocol/src/interaction/InteractionMessenger.ts:344) answers
// an error that carries no status with Status.Failure (:425-460). The
// thrown error is a plain MatterError (packages/general/src/MatterError.ts:188).
// The fields:
// TlvReadRequest.ts:30, TlvSubscribeRequest.ts:21, :39,
// TlvInvokeRequest.ts:19, :22, TlvWriteRequest.ts:18, :21, :25 (under
// packages/types/src/protocol/messages/), TlvAttributePath.ts:21 and
// TlvEventPath.ts:22 (under packages/types/src/protocol/types/). The
// response decoders keep [boolField], except the event path, which
// readEventPathFields shares with the request side: the client's
// DataReport decoding reads a received EventPathIB's IsUrgent here too.
func requestBoolField(el tlv.Element, name string) (bool, error) {
	if el.Type != tlv.TypeBoolFalse && el.Type != tlv.TypeBoolTrue {
		return false, fmt.Errorf("im: %s (tag %d) not a boolean: TLV type 0x%02X", name, fieldTag(el), uint8(el.Type))
	}
	return el.Bool, nil
}

// uintField returns el's value when el is an unsigned integer of at most
// maxValue. A path or header integer is never masked into range: an
// out-of-range value would become a different, valid path.
//
// Mirrors matter.js at db021da0, which decodes a request with
// schema.decode and no relaxed number checks
// (packages/protocol/src/interaction/InteractionMessenger.ts:360,
// packages/types/src/tlv/TlvSchema.ts:49). A wrong TLV type — a signed
// integer included — throws UnexpectedDataError
// (packages/types/src/tlv/TlvNumber.ts:124-131), which is no
// StatusResponseError (packages/general/src/MatterError.ts:188), so
// handleRequest answers Failure (InteractionMessenger.ts:425-460, :431).
// A value out of bounds throws ValidationOutOfBoundsError
// (TlvNumber.ts:145-155), a ValidationError whose code is ConstraintError
// (packages/types/src/common/ValidationError.ts:16-29).
func uintField(el tlv.Element, maxValue uint64) (uint64, error) {
	switch el.Type {
	case tlv.TypeUnsignedInt1, tlv.TypeUnsignedInt2, tlv.TypeUnsignedInt4, tlv.TypeUnsignedInt8:
	default:
		return 0, &FieldError{Status: StatusFailure, Tag: fieldTag(el), Reason: fmt.Sprintf("TLV type 0x%02X, want an unsigned integer", uint8(el.Type))}
	}
	if el.Uint > maxValue {
		return 0, &FieldError{Status: StatusConstraintError, Tag: fieldTag(el), Reason: fmt.Sprintf("%d exceeds %d", el.Uint, maxValue)}
	}
	return el.Uint, nil
}

// uint8Field is [uintField] bounded by the uint8 range.
func uint8Field(el tlv.Element) (uint8, error) {
	v, err := uintField(el, math.MaxUint8)
	if err != nil || v > math.MaxUint8 {
		return 0, err
	}
	return uint8(v), nil
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

// errNonContextMember refuses a structure member that carries no context
// tag ([contextMember]).
var errNonContextMember = errors.New("im: structure member without a context tag")

// contextMember is the rule every request decoder of this package, the
// path lists, StatusIB and StatusResponse apply to a member before their
// switch (the client's report decoders keep skipping a non-context
// member): a member with a context tag is the
// decoder's to read — one whose tag it does not know is skipped whole by
// the switch's default arm ([skipField]) — and a member with any other tag
// refuses the structure. matter.js TlvObject refuses a non-context tag and
// skips an unknown context tag as a whole element
// (packages/types/src/tlv/TlvObject.ts:212-219 at db021da0); a nested
// container read field by field as the enclosing structure's used to end
// that structure early (review II, R2-P6-wire-0504). The refusal is a plain
// error: the request is answered FAILURE.
func contextMember(el tlv.Element) error {
	if el.Tag.Kind == tlv.TagKindContext {
		return nil
	}
	return fmt.Errorf("%w (tag kind %d)", errNonContextMember, el.Tag.Kind)
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
