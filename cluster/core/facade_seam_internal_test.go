// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/internal/facadeseam"
)

// TestFacadeSlotsRunBeforeTheHosts (R2-P5-apinode-07): the facade's slot of
// a fabric hook and the host's are two slots — setting one leaves the
// other — and the facade's runs first.
func TestFacadeSlotsRunBeforeTheHosts(t *testing.T) {
	t.Parallel()
	var got []string
	o := &OperationalCredentials{}
	if !facadeseam.SetOnFabricUpdated(o, func(context.Context, uint8) { got = append(got, "facade") }) {
		t.Fatal("the seam refused an OperationalCredentials")
	}
	o.SetOnFabricUpdated(func(context.Context, uint8) { got = append(got, "host") })
	chainFabricHooks(o.facadeOnFabricUpdated, o.onFabricUpdated)(context.Background(), 1)
	if !slices.Equal(got, []string{"facade", "host"}) {
		t.Fatalf("hooks ran %v, want [facade host]", got)
	}
	if facadeseam.SetOnFabricUpdated(&GeneralCommissioning{}, nil) || facadeseam.SetOnCommissioningComplete(o, nil) {
		t.Fatal("a seam setter took a server it does not serve")
	}
	if chainFabricHooks(nil, nil) != nil {
		t.Fatal("two empty slots chained into a hook")
	}
}
