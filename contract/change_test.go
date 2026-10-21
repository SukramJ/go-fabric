// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

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
