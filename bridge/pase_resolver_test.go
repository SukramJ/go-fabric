// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import "testing"

// TestOperationalSessionLookupPASEResolver covers the optional
// [SessionPASEResolver] side of the adapter: unwired it must answer
// (false, false) so a request is never handed Administer by accident,
// and wired it must forward the session id to the closure.
func TestOperationalSessionLookupPASEResolver(t *testing.T) {
	t.Parallel()

	bare := NewOperationalSessionLookup(nil)
	if pase, ok := bare.IsPASE(7); pase || ok {
		t.Fatalf("unwired IsPASE(7) = (%v, %v), want (false, false)", pase, ok)
	}

	var seen uint16
	wired := NewOperationalSessionLookup(nil).WithPASEResolver(func(id uint16) (bool, bool) {
		seen = id
		return id == 7, true
	})
	if pase, ok := wired.IsPASE(7); !pase || !ok {
		t.Fatalf("wired IsPASE(7) = (%v, %v), want (true, true)", pase, ok)
	}
	if seen != 7 {
		t.Fatalf("closure saw session %d, want 7", seen)
	}
	if pase, _ := wired.IsPASE(8); pase {
		t.Fatal("wired IsPASE(8) = true, want false")
	}

	var _ SessionPASEResolver = (*OperationalSessionLookup)(nil)
}
