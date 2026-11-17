// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "errors"

// laterErrors keeps the first error of a request decode that matter.js
// finds only once the request is decoded: a value outside its type's
// bounds (a [StatusCodeError] of ConstraintError) or a mandatory field
// missing ([ErrMissingField]). matter.js decodes a request first and
// validates it afterwards (packages/types/src/schema/Schema.ts:18-24 at
// db021da0): every type error is thrown while it decodes, bounds and
// mandatory fields only when it validates, so a request with an
// out-of-range integer ahead of a mistyped field answers the type error's
// FAILURE, not CONSTRAINT_ERROR (review II, 5a-2 A1-d).
//
// Every request decoder loop passes its errors through [laterErrors.keep]
// and goes on to the end of its container; there it returns the kept
// error, ahead of its own checks, when nothing else failed. A decoder
// therefore returns a kept error only with its container consumed, and
// the loop around it goes on as well.
type laterErrors struct{ first error }

// keep returns err when it ends the decode now, and nil when it is kept
// for the container's end (the first one only).
func (l *laterErrors) keep(err error) error {
	if err == nil || !validatedLater(err) {
		return err
	}
	if l.first == nil {
		l.first = err
	}
	return nil
}

// validatedLater reports whether err is one matter.js finds when it
// validates a decoded request rather than while it decodes it.
func validatedLater(err error) bool {
	if errors.Is(err, ErrMissingField) {
		return true
	}
	sce, ok := errors.AsType[StatusCodeError](err)
	return ok && sce.MatterStatusCode() == StatusConstraintError
}
