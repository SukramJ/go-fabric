// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
)

// TestScenesForgetFabricOnAnUnloadedTable (review II, R4-store-4154, the
// run-4 probe): a fabric forgotten while the scene table does not load is
// gone once it loads — its scenes and its scene info entry — instead of
// the pending removal creating the info entry again.
func TestScenesForgetFabricOnAnUnloadedTable(t *testing.T) {
	t.Parallel()
	refuse := true
	stored := []byte(`[{"f":3,"g":257,"s":1}]`)
	st := core.NewPersistedScenesState(
		func(context.Context) ([]byte, error) {
			if refuse {
				return nil, errors.New("the store refuses")
			}
			return stored, nil
		},
		func(_ context.Context, b []byte) error { stored = b; return nil })
	st.ForgetFabric(3)
	refuse = false
	if got := st.Fabrics(); slices.Contains(got, 3) {
		t.Fatalf("Fabrics after ForgetFabric(3) and the load = %v, want without 3", got)
	}
}
