// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valueenc

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

// The test tripwire. [Encode] contains every panic, also one of this
// module's own encoders — a coding mistake that used to panic a test
// binary now answers FAILURE for one path, which a test that does not
// read that path's bytes would pass. So every contained panic whose value
// no test declared with [ExpectPanic] is counted, and the TestMain of each
// package whose tests encode through Encode fails the run when the count
// is not zero ([UnexpectedPanics]). Production code never calls either;
// nothing here changes what Encode returns.
//
// Declaring by value rather than by count: the bridge also encodes a
// value for change detection and for logging, so how often a test's
// failing value is encoded is not a number the test controls.

var (
	expectedPanics   sync.Map // panic value -> struct{}
	unexpectedPanics atomic.Int64
	lastUnexpected   atomic.Pointer[PanicError]
)

// ExpectPanic declares v, a comparable panic value a test's encoder
// panics with on purpose: a contained panic with that value is not
// counted by [UnexpectedPanics]. A test seam; production code does not
// call it.
func ExpectPanic(v any) {
	expectedPanics.Store(v, struct{}{})
}

// UnexpectedPanics is the number of contained panics whose value no test
// declared, and a description of the last one (its type and stack). A
// test seam for TestMain.
func UnexpectedPanics() (n int64, last string) {
	if pe := lastUnexpected.Load(); pe != nil {
		last = fmt.Sprintf("%s\n%s", pe.Error(), pe.Stack)
	}
	return unexpectedPanics.Load(), last
}

// notePanic counts r, recovered as pe, unless a test declared it.
func notePanic(r any, pe *PanicError) {
	if t := reflect.TypeOf(r); t != nil && t.Comparable() {
		if _, ok := expectedPanics.Load(r); ok {
			return
		}
	}
	unexpectedPanics.Add(1)
	lastUnexpected.Store(pe)
}
