// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package snapshot_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
)

// TestVendors: the embedded snapshot names the ledger's vendors offline
// over matter.js's static vendors, in id order; a vendor list with an
// entry of no name stops there, keeping the entries before it.
func TestVendors(t *testing.T) {
	t.Parallel()
	s, err := snapshot.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	vs := s.Vendors()
	if len(vs) < 100 {
		t.Fatalf("%d vendors embedded", len(vs))
	}
	for i := 1; i < len(vs); i++ {
		if vs[i-1].ID >= vs[i].ID {
			t.Fatalf("vendors out of order at %d: %d, %d", i, vs[i-1].ID, vs[i].ID)
		}
	}
	if v, ok := s.Vendor(0xFFF1); !ok || v.Name != "Test Vendor" {
		t.Fatalf("0xFFF1: %+v (%v)", v, ok)
	}
	if v, ok := s.Vendor(1); !ok || v.Name == "" {
		t.Fatalf("vendor 1: %+v (%v)", v, ok)
	}
	broken := fstest.MapFS{snapshot.VendorsFile: {Data: []byte(`{"expectedCount":2,"entries":[
		{"vendorId":7,"vendorName":"Seven","kind":"production"},
		{"vendorId":8,"vendorName":"","kind":"production"}]}`)}}
	s, err = snapshot.Load(broken)
	if !errors.Is(err, snapshot.ErrMalformed) {
		t.Fatalf("a nameless vendor: %v, want ErrMalformed", err)
	}
	if _, ok := s.Vendor(7); !ok {
		t.Fatal("the entry before the malformed one was dropped")
	}
	if _, ok := s.Vendor(8); ok {
		t.Fatal("the malformed entry was kept")
	}
	empty, err := snapshot.Load(fstest.MapFS{})
	if err != nil || len(empty.Vendors()) != 5 {
		t.Fatalf("no vendor list: %d vendors (%v), want the 5 static ones", len(empty.Vendors()), err)
	}
}
