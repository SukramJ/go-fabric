// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valueenc_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/reportchunk"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// panickingRuntimeErrorP10 is a host panic value that implements
// runtime.Error and whose Error panics.
type panickingRuntimeErrorP10 struct{}

func (panickingRuntimeErrorP10) RuntimeError() {}

func (panickingRuntimeErrorP10) Error() string { panic("Error panics") }

// TestPanickingRuntimeErrorIsContainedP10 (P10F-err-6404): a host encoder
// panics with a runtime.Error whose Error panics. Encode and
// Scratch.Encode answer ErrPanic, and reportchunk.Split answers FAILURE
// for that path only. Red at b728687e2: the panic of Error, called in
// run's deferred func, left both entry points and Split.
func TestPanickingRuntimeErrorIsContainedP10(t *testing.T) {
	valueenc.ExpectPanic(panickingRuntimeErrorP10{})
	var s valueenc.Scratch
	for name, encode := range map[string]func(func(*tlv.Encoder, tlv.Tag)) ([]byte, error){
		"Encode":         valueenc.Encode,
		"Scratch.Encode": s.Encode,
	} {
		escaped, err := encodeEscapes(func() error {
			_, err := encode(func(*tlv.Encoder, tlv.Tag) { panic(panickingRuntimeErrorP10{}) })
			return err
		})
		var pe *valueenc.PanicError
		if escaped != nil || !errors.Is(err, valueenc.ErrPanic) || !errors.As(err, &pe) ||
			pe.Type != "valueenc_test.panickingRuntimeErrorP10" {
			t.Fatalf("%s: escaped %v, err %v; want ErrPanic naming the type", name, escaped, err)
		}
	}

	w := func(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
		if v.Value == "boom" {
			panic(panickingRuntimeErrorP10{})
		}
		enc.PutUTF8(tag, v.Value.(string))
	}
	path := func(id uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: id, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	reps := []im.AttributeReport{
		{Path: path(1), Value: im.AttributeValue{Value: "ok"}},
		{Path: path(9), Value: im.AttributeValue{Value: "boom"}},
	}
	var chunks []im.ReportData
	escaped, err := encodeEscapes(func() (err error) {
		chunks, err = reportchunk.Split(im.ReportData{Reports: reps}, 4000, 1280, w, nil)
		return err
	})
	if escaped != nil || err != nil || len(chunks) != 1 {
		t.Fatalf("Split: escaped %v, err %v, chunks %d; want one chunk", escaped, err, len(chunks))
	}
	var sawOne, failedNine bool
	for _, r := range chunks[0].Reports {
		switch r.Path.Attribute {
		case 1:
			sawOne = !r.IsStatus
		case 9:
			failedNine = r.IsStatus && r.Status.Status == im.StatusFailure
		}
	}
	if !sawOne || !failedNine {
		t.Fatalf("reports %+v: want attribute 1 as data and attribute 9 as FAILURE", chunks[0].Reports)
	}
}
