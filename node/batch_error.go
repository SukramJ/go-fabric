// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"thingfabric.dev/go-fabric/device"
)

// BatchOp is the kind of a [Batch] call a [BatchCallError] names.
type BatchOp uint8

// The [Batch] calls.
const (
	// BatchAdd is [Batch.Add].
	BatchAdd BatchOp = iota + 1
	// BatchReplace is [Batch.Replace].
	BatchReplace
	// BatchRemove is [Batch.Remove].
	BatchRemove
	// BatchForget is [Batch.Forget].
	BatchForget
)

// String is the call's method name: "Add", "Replace", "Remove" or
// "Forget".
func (o BatchOp) String() string {
	switch o {
	case BatchAdd:
		return "Add"
	case BatchReplace:
		return "Replace"
	case BatchRemove:
		return "Remove"
	case BatchForget:
		return "Forget"
	}
	return fmt.Sprintf("BatchOp(%d)", uint8(o))
}

// BatchError is what [Partition.Batch] returns when anything failed after
// fn returned: fn's own error beside the errors of the calls that answered
// nil and failed later, the store errors no call owns, and the reassembly's.
// A call that returned nil succeeded unless Calls names it. It unwraps to
// each non-nil part, so errors.Is finds every sentinel a part carries
// ([ErrDuplicateKey], [ErrUnknownKey], a store's error, …).
type BatchError struct {
	// Fn is what fn returned.
	Fn error
	// Calls are the calls of the batch that failed after fn returned —
	// a held call refused or failed at its judgement, an applied one
	// undone, or the store work of a call — one entry per call, in call
	// order.
	Calls []*BatchCallError
	// Store are the store errors no call of the batch owns: a forget in
	// progress that could not be cancelled, or the erasure a batch whose
	// reassembly failed left to this one (see Reassembly).
	Store error
	// Reassembly, when set, is the reassembly's error: the set holds the
	// batch's changes but the node still serves the topology before.
	// The devices the batch removed or replaced stay bound and the
	// erasures of its Forgets wait until a reassembly succeeds; the next
	// Batch, an empty one included, reassembles and runs them, and
	// [Aggregator.RetirePartition] does too.
	Reassembly error
}

// Error joins the parts' texts, one per line, in the order of Unwrap.
func (e *BatchError) Error() string {
	parts := e.Unwrap()
	s := make([]string, len(parts))
	for i, p := range parts {
		s[i] = p.Error()
	}
	return strings.Join(s, "\n")
}

// Unwrap is the non-nil parts: Fn, each of Calls, Store, Reassembly.
func (e *BatchError) Unwrap() []error {
	out := make([]error, 0, len(e.Calls)+3)
	if e.Fn != nil {
		out = append(out, e.Fn)
	}
	for _, c := range e.Calls {
		out = append(out, c)
	}
	if e.Store != nil {
		out = append(out, e.Store)
	}
	if e.Reassembly != nil {
		out = append(out, e.Reassembly)
	}
	return out
}

// BatchCallError is one call of a [Batch] that answered nil — or whose
// store work ran after fn — and failed after fn returned.
type BatchCallError struct {
	// Op is the call.
	Op BatchOp
	// Key is the call's own key, as the host passed it: the endpoint's
	// StableKey for an Add, the key argument otherwise. A collision with
	// a part key names that key in Err's text.
	Key device.SourceKey
	// Applied reports that the set took the call's change and Err is
	// what failed after it — the store's reservation or erasure work
	// (an Add stays in the set, a Forget's device out of it, and the
	// Forget can be made again). False: the call changed nothing — it
	// was refused at its judgement or undone.
	Applied bool
	// Err is what failed, the call's errors joined.
	Err error
}

// Error names the call and its error.
func (e *BatchCallError) Error() string {
	return fmt.Sprintf("node: batch %s %s: %v", e.Op, e.Key, e.Err)
}

// Unwrap is Err.
func (e *BatchCallError) Unwrap() error { return e.Err }

// callOrigin is the [Batch] call a held call, a take or a reservation op
// comes from: its kind, its own key and its place among the batch's
// calls (order, from 1; 0 is no call).
type callOrigin struct {
	op    BatchOp
	key   device.SourceKey
	order int
}

// callErrors collects the [BatchCallError]s of one batch by call.
type callErrors struct {
	byOrder map[int]*BatchCallError
}

// add records err for every call of origins: joined into the call's
// entry, which is applied only while every error recorded for it is of
// an applied change. An error of no call is returned for the caller to
// keep.
func (c *callErrors) add(origins []callOrigin, applied bool, err error) error {
	if err == nil {
		return nil
	}
	var owned bool
	for _, o := range origins {
		if o.order == 0 {
			continue
		}
		owned = true
		if c.byOrder == nil {
			c.byOrder = map[int]*BatchCallError{}
		}
		e := c.byOrder[o.order]
		if e == nil {
			c.byOrder[o.order] = &BatchCallError{Op: o.op, Key: o.key, Applied: applied, Err: err}
			continue
		}
		e.Applied = e.Applied && applied
		e.Err = errors.Join(e.Err, err)
	}
	if owned {
		return nil
	}
	return err
}

// list is the entries in call order.
func (c *callErrors) list() []*BatchCallError {
	if len(c.byOrder) == 0 {
		return nil
	}
	out := make([]*BatchCallError, 0, len(c.byOrder))
	for _, o := range slices.Sorted(maps.Keys(c.byOrder)) {
		out = append(out, c.byOrder[o])
	}
	return out
}
