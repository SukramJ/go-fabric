// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"reflect"
	"testing"

	"thingfabric.dev/go-fabric/client/discovery"
)

// TestDescriptor_CloneCopiesTheDiscoveryDataFields: the copy a peer hands
// out ([Peer.Descriptor]) and the one it keeps share no TXT field, so a
// caller writing through a returned descriptor's *DN leaves the peer's
// alone — every pointer field of [discovery.TXTData], so a field added
// later is held too (review II, R2-C2-api-2157).
func TestDescriptor_CloneCopiesTheDiscoveryDataFields(t *testing.T) {
	t.Parallel()
	var txt discovery.TXTData
	v := reflect.ValueOf(&txt).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		if f.Kind() != reflect.Pointer {
			t.Fatalf("TXTData.%s is %s, not a pointer: extend the test and clone", v.Type().Field(i).Name, f.Kind())
		}
		f.Set(reflect.New(f.Type().Elem()))
	}
	name := "Kitchen"
	txt.DN = &name
	d := Descriptor{DiscoveryData: &txt}
	c := d.clone()
	*c.DiscoveryData.DN = "Hall"
	if *d.DiscoveryData.DN != "Kitchen" {
		t.Fatalf("writing the copy's DN changed the original's to %q", *d.DiscoveryData.DN)
	}
	cv := reflect.ValueOf(c.DiscoveryData).Elem()
	for i := range v.NumField() {
		if cv.Field(i).IsNil() {
			t.Errorf("TXTData.%s is not copied", v.Type().Field(i).Name)
			continue
		}
		if v.Field(i).Pointer() == cv.Field(i).Pointer() {
			t.Errorf("TXTData.%s is shared between the descriptor and its copy", v.Type().Field(i).Name)
		}
	}
}
