// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// TestOriginTravelsWithTheDelivery: the origin DeliverChange states is read
// with the delivered slice inside the listener, and nowhere else.
func TestOriginTravelsWithTheDelivery(t *testing.T) {
	t.Parallel()
	ids := []uint32{1, 2}
	var inside contract.Origin
	contract.DeliverChange(contract.OriginCommand, ids, func(got []uint32) { inside = contract.OriginOf(got) })
	if inside != contract.OriginCommand {
		t.Errorf("inside the listener %v, want command", inside)
	}
	if o := contract.OriginOf(ids); o != contract.OriginUnknown {
		t.Errorf("after the listener %v, want unknown", o)
	}
	if o := contract.OriginOf(nil); o != contract.OriginUnknown {
		t.Errorf("empty slice %v, want unknown", o)
	}
	called := false
	contract.DeliverChange(contract.OriginHostSet, nil, func([]uint32) { called = true })
	if !called {
		t.Error("an empty delivery skipped the listener")
	}
}

// TestOriginOnTheContext: a request's context carries its origin; one
// without is the host's.
func TestOriginOnTheContext(t *testing.T) {
	t.Parallel()
	ctx := contract.WithOrigin(context.Background(), contract.OriginControllerWrite)
	if o := contract.OriginFrom(ctx); o != contract.OriginControllerWrite {
		t.Errorf("stamped %v, want controller write", o)
	}
	if o := contract.OriginFrom(context.Background()); o != contract.OriginHostSet {
		t.Errorf("unstamped %v, want host set", o)
	}
	var none context.Context
	if o := contract.OriginFrom(none); o != contract.OriginHostSet {
		t.Errorf("nil context %v, want host set", o)
	}
	for o, want := range map[contract.Origin]string{
		contract.OriginUnknown: "unknown", contract.OriginHostSet: "host set",
		contract.OriginControllerWrite: "controller write", contract.OriginCommand: "command",
	} {
		if o.String() != want {
			t.Errorf("%d.String() = %q, want %q", o, o.String(), want)
		}
	}
}
