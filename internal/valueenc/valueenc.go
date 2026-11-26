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
	"runtime/debug"

	"thingfabric.dev/go-fabric/tlv"
)

// ErrNotOneElement reports an encoder that returned without panicking and
// with its containers balanced but did not write exactly one anonymously
// tagged element: none, two or more, or one under another tag.
var ErrNotOneElement = errors.New("valueenc: the encoder did not write exactly one anonymous element")

// ErrPanic reports an encoder that panicked; the error carries the panic
// value and the stack.
var ErrPanic = errors.New("valueenc: the encoder panicked")

// Encode runs write on a fresh encoder and returns the one anonymous TLV
// element it wrote. write must write its value under the tag it is handed
// ([tlv.AnonymousTag]). It fails when write panics (the panic is
// recovered; [ErrPanic], with the stack), when it leaves a container open
// ([tlv.ErrUnbalancedContainer]), and when what it wrote is not exactly
// one complete, anonymously tagged element ([ErrNotOneElement]). A surplus
// EndContainer writes nothing on an encoder of its own ([tlv.Encoder]
// refuses it), so it cannot close a container of the message.
//
// The element is the caller's to keep; [tlv.Encoder.PutRawElement] writes
// it back under the tag the message needs.
func Encode(write func(enc *tlv.Encoder, tag tlv.Tag)) (element []byte, err error) {
	enc := tlv.NewEncoder()
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
			err = fmt.Errorf("%w: %v\n%s", ErrPanic, r, debug.Stack())
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
