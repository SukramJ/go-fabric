// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestAPlainGeneratedListEncodesAsItsList: a plain slice of a generated
// struct — what a generated server stores for a list a typed Patch sets —
// goes on the wire byte for byte as its spec.List, not as null.
func TestAPlainGeneratedListEncodesAsItsList(t *testing.T) {
	entries := []ct.DayEntryStruct{{DayEntryId: 1, StartTime: 0}, {DayEntryId: 2, StartTime: 720}}
	plain, err := callAttributeWriter(im.AttributeValue{Value: entries}).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	enc := tlv.NewEncoder()
	spec.List[ct.DayEntryStruct](entries).EncodeTLV(enc, tlv.AnonymousTag())
	want, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, want) {
		t.Errorf("plain slice % X, want its spec.List % X", plain, want)
	}
	if EncodesAttributeValue([]struct{ X int }{{1}}) {
		t.Error("a slice of a struct the writer does not know encodes")
	}
}
