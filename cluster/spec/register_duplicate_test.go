// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/rvcrunmode"
)

// TestRegisterPanicsOnADuplicate holds Register to database/sql.Register's
// rule: a second, different definition under a cluster id already
// registered is a programming error and panics, so a vendor definition
// can no longer replace a generated one for the whole process unnoticed.
// Registering the definition already held is a no-op. Before, the later
// registration silently replaced the earlier one (review II,
// R2-P6-api-1311).
func TestRegisterPanicsOnADuplicate(t *testing.T) {
	t.Parallel()
	spec.Register(rvcrunmode.Definition) // the one its init registered
	if spec.Lookup(rvcrunmode.ClusterID) != rvcrunmode.Definition {
		t.Fatal("re-registering the held definition replaced it")
	}
	vendor := *rvcrunmode.Definition
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "registered twice") {
			t.Errorf("Register of another definition under 0x%04X: recovered %v, want a panic naming the duplicate", rvcrunmode.ClusterID, r)
		}
		if spec.Lookup(rvcrunmode.ClusterID) != rvcrunmode.Definition {
			t.Error("the refused registration replaced the held definition")
		}
	}()
	spec.Register(&vendor)
}
