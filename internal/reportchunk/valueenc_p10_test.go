// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package reportchunk_test

import (
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/reportchunk"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// unhashableP10 is a comparable type whose value can be unhashable.
type unhashableP10 struct{ V any }

// TestSplitUnhashablePanicCostsOnePathP10 (P10-err-6401): the writer
// panics for attribute 9 with a value of a comparable type that is
// unhashable. #625's promise holds for it: FAILURE for 9, attribute 1 goes
// out. Red at d1ab4e970: the panic left Split and no chunk was built.
func TestSplitUnhashablePanicCostsOnePathP10(t *testing.T) {
	before, _ := valueenc.UnexpectedPanics()
	splitWithPanicP10(t, unhashableP10{V: []int{1}})
	n, _ := valueenc.UnexpectedPanics()
	if n <= before {
		t.Fatalf("undeclared panics %d, want more than %d: an unhashable value cannot have been declared", n, before)
	}
	valueenc.ForgetUnexpectedPanics(n - before) // provoked on purpose
}

// TestSplitHashablePanicControlP10: the same report, the panic value
// hashable and declared.
func TestSplitHashablePanicControlP10(t *testing.T) {
	valueenc.ExpectPanic(unhashableP10{V: 1})
	splitWithPanicP10(t, unhashableP10{V: 1})
}

func splitWithPanicP10(t *testing.T, pv any) {
	t.Helper()
	w := func(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
		if s, ok := v.Value.(string); ok && s == "boom" {
			panic(pv)
		}
		enc.PutUTF8(tag, v.Value.(string))
	}
	reps := []im.AttributeReport{attr(1, "ok"), attr(9, "boom")}
	var escaped any
	var chunks []im.ReportData
	var err error
	func() {
		defer func() { escaped = recover() }()
		chunks, err = reportchunk.Split(im.ReportData{Reports: reps}, 4000, 1280, w, nil)
	}()
	if escaped != nil {
		t.Fatalf("panic left reportchunk.Split, no chunk at all: %T %v", escaped, escaped)
	}
	if err != nil || len(chunks) != 1 {
		t.Fatalf("chunks %d err %v", len(chunks), err)
	}
	var sawOne, sawNine bool
	for _, r := range chunks[0].Reports {
		switch r.Path.Attribute {
		case 1:
			sawOne = !r.IsStatus
		case 9:
			sawNine = true
			if !r.IsStatus || r.Status.Status != im.StatusFailure {
				t.Fatalf("attribute 9: %+v, want FAILURE", r)
			}
		}
	}
	if !sawOne || !sawNine {
		t.Fatalf("reports %+v: want attribute 1 as data and attribute 9 as FAILURE", chunks[0].Reports)
	}
}
