// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestATypedPatchListIsStoredAsItsList: a list of a structure a typed
// Patch sets is stored as its spec.List (the generated
// Patch.AttributeValues), so the attribute writer encodes it byte for byte
// as the list, not as null.
func TestATypedPatchListIsStoredAsItsList(t *testing.T) {
	srv, err := spec.NewServer(epmdef.Definition, spec.Options{Features: uint32(epmdef.FeatureAlternatingCurrent)}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	fixed := uint64(1)
	accuracy := []epmdef.MeasurementAccuracyStruct{{
		MeasurementType: epmdef.MeasurementTypeActivePower, Measured: true, MinMeasuredValue: -10, MaxMeasuredValue: 10,
		AccuracyRanges: []epmdef.MeasurementAccuracyRangeStruct{{RangeMin: -10, RangeMax: 10, FixedMax: &fixed}},
	}}
	if err := epmdef.Wrap(srv).Set(epmdef.Patch{Accuracy: &accuracy}); err != nil {
		t.Fatal(err)
	}
	v, _ := srv.MatterRead(epmdef.AttrAccuracy)
	list, ok := v.(spec.List[epmdef.MeasurementAccuracyStruct])
	if !ok {
		t.Fatalf("Accuracy is stored as %T, want its spec.List", v)
	}
	got, err := callAttributeWriter(im.AttributeValue{Value: v}).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	enc := tlv.NewEncoder()
	list.EncodeTLV(enc, tlv.AnonymousTag())
	want, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("written % X, want the list's % X", got, want)
	}
	if st := epmdef.Wrap(srv).Get(); len(st.Accuracy) != 1 || st.Accuracy[0].AccuracyRanges[0].RangeMax != 10 {
		t.Errorf("Get reads Accuracy back as %+v", st.Accuracy)
	}
}

// TestAPlainEnumListIsAnArray: a list a server returns as a plain slice
// of an enum (CameraAvStreamManagement.StreamUsagePriorities,
// []StreamUsageEnum) is a TLV array of its values, not null
// (TC-WEBRTCP-2.8 read it as ValueDecodeFailure).
func TestAPlainEnumListIsAnArray(t *testing.T) {
	type usage uint8
	type flag bool
	got, err := callAttributeWriter(im.AttributeValue{Value: []usage{3, 1}}).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	want := tlv.NewEncoder()
	want.StartArray(tlv.AnonymousTag())
	want.PutUint(tlv.AnonymousTag(), 3)
	want.PutUint(tlv.AnonymousTag(), 1)
	_ = want.EndContainer()
	wantBytes, _ := want.Bytes()
	if !bytes.Equal(got, wantBytes) {
		t.Fatalf("[]usage = %x, want %x", got, wantBytes)
	}
	if got, _ := callAttributeWriter(im.AttributeValue{Value: []flag{true}}).Bytes(); len(got) < 2 || got[0] != 0x16 {
		t.Fatalf("[]flag = %x, want an array", got)
	}
	if got, _ := callAttributeWriter(im.AttributeValue{Value: []struct{}{{}}}).Bytes(); len(got) != 1 || got[0] != 0x14 {
		t.Fatalf("[]struct{} = %x, want null (no rule)", got)
	}
}
