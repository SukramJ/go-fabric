// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// TestInvokeAccessSpotChecks pins the command access tables without a
// matter.js checkout, so their coverage and behaviour do not depend on one
// (TestFabricScopedInvokeMatchesMatterJS and
// TestInvokePrivilegeMatchesMatterJS hold the whole tables to matter.js's
// element files where the checkout exists). The expectations are the
// access strings of those element files.
func TestInvokeAccessSpotChecks(t *testing.T) {
	t.Parallel()
	scoped := []struct {
		cluster, command uint32
		want             bool
	}{
		{0x003E, 0x07, true},  // OperationalCredentials UpdateFabricLabel "F A"
		{0x003E, 0x0A, false}, // RemoveFabric "A", not fabric-scoped
		{0x0004, 0x00, true},  // Groups AddGroup "F M"
		{0x0062, 0x40, true},  // ScenesManagement CopyScene "F M"
		{0x0006, 0x00, false}, // OnOff Off — no fabric quality
		{0x9999, 0x00, false}, // an unknown cluster
	}
	for _, c := range scoped {
		if got := schema.IsFabricScopedInvoke(c.cluster, c.command); got != c.want {
			t.Errorf("IsFabricScopedInvoke(0x%04X, 0x%02X) = %v, want %v", c.cluster, c.command, got, c.want)
		}
	}
	privilege := []struct {
		cluster, command uint32
		want             uint8
	}{
		{0x003F, 0x00, schema.PrivilegeAdminister}, // KeySetWrite "F A"
		{0x0062, 0x00, schema.PrivilegeManage},     // AddScene "F M"
		{0x0101, 0x1A, schema.PrivilegeAdminister}, // DoorLock SetUser "A T"
		{0x0006, 0x02, schema.PrivilegeOperate},    // OnOff Toggle: the default
		{0x9999, 0x00, schema.PrivilegeOperate},    // an unknown cluster
	}
	for _, c := range privilege {
		if got := schema.InvokePrivilege(c.cluster, c.command); got != c.want {
			t.Errorf("InvokePrivilege(0x%04X, 0x%02X) = %d, want %d", c.cluster, c.command, got, c.want)
		}
	}
}
