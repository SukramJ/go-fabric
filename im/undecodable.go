// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// UndecodableValue is the [AttributeValue.Value] of a write whose data the
// attribute's type cannot decode: a structure or a list written to a scalar
// attribute, an element of another type, a list element that is not a
// structure where the list holds structures. An [AttributeValueReader]
// answers it instead of an error so the write reaches its place in the
// request.
//
// [HandleWriteRequest] and [HandleGroupWrite] end the interaction where its
// value would be written — after the existence, writability, access, timed
// and fabric checks of its path, through the same gate a numeric ListIndex
// takes — and the server answers a StatusResponse FAILURE in place of the
// WriteResponse; the attribute data before it in the request were written.
// A dispatcher refuses it for every server: no cluster server receives it.
//
// Mirrors matter.js AttributeWriteResponse.ts:429, :452-459 and
// InteractionMessenger.ts:425-459: a value the attribute's schema cannot
// decode (TlvNumber.ts:124-131, TlvObject.ts:205-207) is not a per-path
// status; the write interaction is answered StatusResponse FAILURE.
type UndecodableValue struct {
	// Err says what did not decode; it is for logs, never sent.
	Err error
}

// Error implements error.
func (v UndecodableValue) Error() string {
	if v.Err == nil {
		return "im: undecodable write value"
	}
	return "im: undecodable write value: " + v.Err.Error()
}

// IsUndecodable reports whether v is an [UndecodableValue].
func IsUndecodable(v AttributeValue) bool {
	_, ok := v.Value.(UndecodableValue)
	return ok
}
