// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// emptyACLLister lists nothing. What matters is that it is non-nil: a host
// that attaches an ACL source is asking for access control, and CheckACL
// fails closed without one for its own reasons.
type emptyACLLister struct{}

func (emptyACLLister) ListACL(context.Context, uint8) ([]store.ACLEntry, error) {
	return nil, nil
}

// TestUnresolvableFabricIsDenied is the guard on a silent authorisation
// bypass.
//
// CheckACL answers Success for fabric index 0 because that means "PASE, no
// fabric yet" — correct while commissioning. The bridge used to hand it that
// same 0 when it could not name a session's fabric at all, so a host with an
// ACL and a session lookup that could not resolve had every CASE session
// waved through with the access check never applied. Nothing failed and
// nothing logged.
//
// The two are distinct values now: 0 is "legitimately no fabric",
// FabricIndexUnresolvable is "the bridge does not know". An unknown fabric
// cannot be checked against anything, so it is denied.
func TestUnresolvableFabricIsDenied(t *testing.T) {
	t.Parallel()

	d := endpoint.NewTopologyDispatcher(nil)
	d.SetACLLister(emptyACLLister{})

	got := d.CheckACL(context.Background(), endpoint.FabricIndexUnresolvable, 1, nil, 1, 0x0006, 1)
	if got.IsSuccess() {
		t.Fatal("an unresolvable fabric was granted access — it is indistinguishable from a CASE " +
			"session whose fabric the bridge could not name, which is the bypass this guards")
	}
	if got != im.StatusUnsupportedAccess {
		t.Errorf("CheckACL(unresolvable) = %v, want UnsupportedAccess", got)
	}
}

// TestPASEStillPassesTheAccessCheck is the negative control, and the reason
// the fix is a second value rather than "deny index 0": commissioning
// genuinely has no fabric, and denying it would stop a device being paired at
// all.
func TestPASEStillPassesTheAccessCheck(t *testing.T) {
	t.Parallel()

	d := endpoint.NewTopologyDispatcher(nil)
	d.SetACLLister(emptyACLLister{})

	if got := d.CheckACL(context.Background(), 0, 1, nil, 1, 0x0006, 1); !got.IsSuccess() {
		t.Fatalf("a PASE session (fabric 0) was denied: %v — commissioning has no fabric to check "+
			"against", got)
	}
}

// TestUnresolvableIsOutsideTheValidFabricRange pins the choice of value
// against the specification rather than against convenience. Matter
// constrains a fabric index to "1 to 254" (matter.js
// packages/model/src/standard/elements/operational-credentials.element.ts:129,
// and three further declarations), so 255 cannot collide with a real fabric.
// If that ever changes, this fails here rather than by granting access to
// whatever fabric 255 became.
func TestUnresolvableIsOutsideTheValidFabricRange(t *testing.T) {
	t.Parallel()

	if endpoint.FabricIndexUnresolvable >= 1 && endpoint.FabricIndexUnresolvable <= 254 {
		t.Fatalf("FabricIndexUnresolvable = %d, inside Matter's valid range of 1 to 254 — it would "+
			"collide with a real fabric", endpoint.FabricIndexUnresolvable)
	}
	if endpoint.FabricIndexUnresolvable == 0 {
		t.Fatal("FabricIndexUnresolvable is 0, which already means \"PASE, no fabric yet\" — the " +
			"whole point is that the two are distinguishable")
	}
}
