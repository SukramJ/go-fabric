// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import "fmt"

// Code classifies an [Error], matter.js's OtbrRestErrorCode
// (packages/thread-br-client/src/otbr-rest/OtbrRestError.ts:9-15 at
// 862ad75f).
type Code string

// The error codes.
const (
	// CodeUnreachable is a network failure or a timeout.
	CodeUnreachable Code = "rest_unreachable"
	// CodeProtocol is a response that violates the expected shape, or a
	// non-2xx status without a code of its own.
	CodeProtocol Code = "rest_protocol"
	// CodeDisabled is the REST surface switched off.
	CodeDisabled Code = "rest_disabled"
	// CodeUnsupported is 404: an endpoint this firmware lacks.
	CodeUnsupported Code = "rest_unsupported"
	// CodeConflict is 409: the operation conflicts with the node's state
	// (a dataset set while Thread is active).
	CodeConflict Code = "rest_conflict"
	// CodeNotAllowed is 405: the firmware does not allow the operation.
	CodeNotAllowed Code = "rest_not_allowed"
)

// Error is a REST failure: matter.js's OtbrRestError (OtbrRestError.ts:
// 22-34) — its code, the HTTP status when a response carried one, and the
// cause. The message names the method and the path, never a dataset.
type Error struct {
	Code       Code
	Message    string
	HTTPStatus int
	Cause      error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("otbr: %s (%s): %v", e.Message, e.Code, e.Cause)
	}
	return fmt.Sprintf("otbr: %s (%s)", e.Message, e.Code)
}

func (e *Error) Unwrap() error { return e.Cause }

// protocolError is a [CodeProtocol] error.
func protocolError(format string, args ...any) *Error {
	return &Error{Code: CodeProtocol, Message: fmt.Sprintf(format, args...)}
}

// codeForStatus maps a non-2xx status as python-otbr-api does
// (OtbrRestClient.ts:63-80, errorCodeForStatus): 404 unsupported, 405 not
// allowed, 409 conflict, anything else protocol.
func codeForStatus(status int) Code {
	switch status {
	case 404:
		return CodeUnsupported
	case 405:
		return CodeNotAllowed
	case 409:
		return CodeConflict
	}
	return CodeProtocol
}
