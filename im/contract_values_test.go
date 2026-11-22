// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// TestContractStatusesAreTheEngines (R2-P5-apidev-15): the statuses and
// privileges contract names for a host are the engine's and the store's
// values.
func TestContractStatusesAreTheEngines(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		contract uint8
		engine   im.StatusCode
	}{
		{"Failure", contract.StatusFailure, im.StatusFailure},
		{"InvalidCommand", contract.StatusInvalidCommand, im.StatusInvalidCommand},
		{"ConstraintError", contract.StatusConstraintError, im.StatusConstraintError},
		{"UnsupportedWrite", contract.StatusUnsupportedWrite, im.StatusUnsupportedWrite},
		{"ResourceExhausted", contract.StatusResourceExhausted, im.StatusResourceExhausted},
		{"NotFound", contract.StatusNotFound, im.StatusNotFound},
		{"Busy", contract.StatusBusy, im.StatusBusy},
		{"InvalidInState", contract.StatusInvalidInState, im.StatusInvalidInState},
		{"Timeout", contract.StatusTimeout, im.StatusTimeout},
	} {
		if im.StatusCode(c.contract) != c.engine {
			t.Errorf("contract.Status%s = %#x, im's is %#x", c.name, c.contract, uint8(c.engine))
		}
	}
	for _, c := range []struct {
		name     string
		contract uint8
		store    uint8
	}{
		{"View", contract.PrivilegeView, uint8(store.PrivilegeView)},
		{"Operate", contract.PrivilegeOperate, uint8(store.PrivilegeOperate)},
		{"Manage", contract.PrivilegeManage, uint8(store.PrivilegeManage)},
		{"Administer", contract.PrivilegeAdminister, uint8(store.PrivilegeAdminister)},
	} {
		if c.contract != c.store {
			t.Errorf("contract.Privilege%s = %d, the store's is %d", c.name, c.contract, c.store)
		}
	}
}

// TestFabricFilterIsContractsAccessingFabric (R2-P5-apidev-14): what the
// engine stamps is what contract.AccessingFabric reads, and the reverse.
func TestFabricFilterIsContractsAccessingFabric(t *testing.T) {
	t.Parallel()
	if _, _, ok := contract.AccessingFabric(context.Background()); ok {
		t.Fatal("AccessingFabric found a request behind a bare context")
	}
	idx, filtered, ok := contract.AccessingFabric(im.WithFabricFilter(context.Background(), true, 3))
	if !ok || idx != 3 || !filtered {
		t.Fatalf("AccessingFabric of the engine's stamp = (%d, %v, %v), want (3, true, true)", idx, filtered, ok)
	}
	f, i, ok := im.LookupFabricFilter(contract.WithAccessingFabric(context.Background(), 0, false))
	if !ok || i != 0 || f {
		t.Fatalf("LookupFabricFilter of contract's stamp = (%v, %d, %v), want (false, 0, true)", f, i, ok)
	}
}
