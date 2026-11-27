// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package valueenc runs one value encoder in isolation: on an encoder of
// its own, under a recover, and judged by what it wrote. It is the
// containment of #625 (owner decisions 695–701): an attribute value, an
// event payload or a command response written by an encoder this module
// does not control — a host's EncodeTLV behind contract/ — costs at most
// its own path, never the message around it.
//
// The scratch encoder is the checkpoint of connectedhomeip's report
// engine, which takes one before each attribute, rolls the partial
// AttributeReportIB back when the encode fails and writes a status for the
// path instead (src/app/reporting/Engine.cpp:449-450, :487, :498 at
// 6170af84): nothing a failing encoder wrote can reach the message.
package valueenc

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"

	"thingfabric.dev/go-fabric/tlv"
)

// ErrNotOneElement reports an encoder that returned without panicking and
// with its containers balanced but did not write exactly one anonymously
// tagged element: none, two or more, or one under another tag.
var ErrNotOneElement = errors.New("valueenc: the encoder did not write exactly one anonymous element")

// ErrPanic reports an encoder that panicked. The error is a
// [*PanicError]: its message names the panic value's type and, for a
// runtime error, the runtime's message; any other panic value is withheld
// ("(redacted)"), because a host encoder's panic may carry the value it
// was writing. The stack is a field, for a log at DEBUG.
var ErrPanic = errors.New("valueenc: the encoder panicked")

// PanicError is the [ErrPanic] of one recovered panic.
type PanicError struct {
	// Type is the panic value's Go type.
	Type string
	// Message is a runtime error's message, at most [maxPanicMessage]
	// bytes; "(redacted)" for any other panic value.
	Message string
	// Stack is the goroutine's stack where the panic was recovered.
	Stack []byte
}

// maxPanicMessage bounds [PanicError.Message].
const maxPanicMessage = 160

// Error names the type and the bounded message, never the stack.
func (e *PanicError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrPanic, e.Type, e.Message)
}

// Is makes a PanicError match [ErrPanic].
func (e *PanicError) Is(target error) bool { return target == ErrPanic }

// PanicStack is err's stack when err is a [*PanicError], else nil.
func PanicStack(err error) []byte {
	var pe *PanicError
	if errors.As(err, &pe) {
		return pe.Stack
	}
	return nil
}

// newPanicError is the [*PanicError] of the recovered value r.
func newPanicError(r any) *PanicError {
	msg := "(redacted)"
	if re, ok := r.(runtime.Error); ok {
		msg = re.Error()
		if len(msg) > maxPanicMessage {
			msg = msg[:maxPanicMessage]
		}
	}
	return &PanicError{Type: fmt.Sprintf("%T", r), Message: msg, Stack: debug.Stack()}
}

// Encode runs write on a fresh encoder and returns the one anonymous TLV
// element it wrote. write must write its value under the tag it is handed
// ([tlv.AnonymousTag]). It fails when write panics (the panic is
// recovered; [ErrPanic], a [*PanicError]), when it leaves a container open
// ([tlv.ErrUnbalancedContainer]), and when what it wrote is not exactly
// one complete, anonymously tagged element ([ErrNotOneElement]). A surplus
// EndContainer writes nothing on an encoder of its own ([tlv.Encoder]
// refuses it), so it cannot close a container of the message.
//
// The element is the caller's to keep; [tlv.Encoder.PutRawElement] writes
// it back under the tag the message needs.
func Encode(write func(enc *tlv.Encoder, tag tlv.Tag)) (element []byte, err error) {
	return encodeOn(tlv.NewEncoder(), write)
}

// Scratch is [Encode] on one encoder reused from value to value, for a
// caller that encodes many values in a row — the report chunker's
// resolver, one per chunker. It holds Encode's guarantees for every value:
// the encoder is [tlv.Encoder.Reset] before each, so a predecessor that
// panicked, left a container open or wrote two elements leaves nothing in
// the next value; and after a value that failed, the encoder is dropped
// and the next value gets a fresh one, so a state the failed write left
// behind is never written on again. Each element returned is a copy the
// caller keeps.
//
// The reuse has a cost Encode has not: a write function that retains the
// *tlv.Encoder past its return and writes on it later writes into a later
// value of the same Scratch, not into a dead encoder (see
// [tlv.Encoder.Reset]). The contract forbids it (contract.ClusterServer,
// MatterRead: an EncodeTLV writes exactly one element under the tag it is
// handed, deterministic and free of side effects); a synchronous
// misbehaviour is still contained to its own value. An asynchronous write
// is a data race with the value being encoded: one that lands inside a
// container that value has open passes the one-element check, and that
// path goes out with foreign bytes and no FAILURE. Elements already
// returned never change (each is a copy), and fabric masking still
// applies to what goes out.
//
// The zero Scratch is ready to use. A Scratch is not safe for concurrent
// use: one goroutine encodes on it at a time.
type Scratch struct {
	enc *tlv.Encoder
}

// Encode runs write on the scratch encoder, as the package [Encode] does
// on a fresh one, and returns the one anonymous element it wrote.
func (s *Scratch) Encode(write func(enc *tlv.Encoder, tag tlv.Tag)) (element []byte, err error) {
	if s.enc == nil {
		s.enc = tlv.NewEncoder()
	} else {
		s.enc.Reset()
	}
	element, err = encodeOn(s.enc, write)
	if err != nil {
		s.enc = nil
	}
	return element, err
}

// encodeOn runs write on enc, empty, and returns the one anonymous element
// it wrote, a copy.
func encodeOn(enc *tlv.Encoder, write func(enc *tlv.Encoder, tag tlv.Tag)) (element []byte, err error) {
	if err := run(write, enc); err != nil {
		return nil, err
	}
	out, err := enc.Bytes()
	if err != nil {
		return nil, err
	}
	if err := oneElement(out); err != nil {
		return nil, err
	}
	return out, nil
}

// run calls write, turning a panic into [ErrPanic].
func run(write func(enc *tlv.Encoder, tag tlv.Tag), enc *tlv.Encoder) (err error) {
	defer func() {
		if r := recover(); r != nil {
			pe := newPanicError(r)
			notePanic(r, pe)
			err = pe
		}
	}()
	write(enc, tlv.AnonymousTag())
	return nil
}

// oneElement checks that b is exactly one complete element under an
// anonymous tag.
func oneElement(b []byte) error {
	dec := tlv.NewDecoder(b)
	el, err := dec.Next()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: none", ErrNotOneElement)
		}
		return fmt.Errorf("%w: %w", ErrNotOneElement, err)
	}
	if el.IsEndContainer || el.Tag.Kind != tlv.TagKindAnonymous {
		return fmt.Errorf("%w: first element tag kind %d", ErrNotOneElement, el.Tag.Kind)
	}
	if el.IsContainer {
		for depth := 1; depth > 0; {
			inner, err := dec.Next()
			if err != nil {
				return fmt.Errorf("%w: %w", ErrNotOneElement, err)
			}
			switch {
			case inner.IsEndContainer:
				depth--
			case inner.IsContainer:
				depth++
			}
		}
	}
	if n := dec.Remaining(); n != 0 {
		return fmt.Errorf("%w: %d bytes after the first element", ErrNotOneElement, n)
	}
	return nil
}
