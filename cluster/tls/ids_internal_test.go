// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tls

import "testing"

// TestAllocatorPastItsBound (R2-P6-store-1119 a): an allocator whose
// position is past its bound hands out no id above it and returns when
// every id is taken.
func TestAllocatorPastItsBound(t *testing.T) {
	a := idAllocator{next: 65535, maxID: MaxCertificateID}
	if id, ok := a.allocate(func(uint16) bool { return false }); !ok || id >= MaxCertificateID {
		t.Fatalf("allocate = %d, %v", id, ok)
	}
	a = idAllocator{next: 65535, maxID: MaxCertificateID}
	if _, ok := a.allocate(func(uint16) bool { return true }); ok {
		t.Fatal("an id handed out with every id taken")
	}
}
