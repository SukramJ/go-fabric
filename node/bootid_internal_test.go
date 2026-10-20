// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// Config.BootID.RotateUniqueIDs enables the rotation in New; the zero
// value leaves it off. The spy keeps the process-wide rotation off for the
// rest of the package's tests, so the node still pins its UniqueID.
func TestBootIDRotation(t *testing.T) {
	orig := enableBootIDRotation
	t.Cleanup(func() { enableBootIDRotation = orig })
	for _, rotate := range []bool{false, true} {
		calls := 0
		enableBootIDRotation = func() { calls++ }
		cfg := mdnsTestConfig(t)
		cfg.Network.MDNS.Disabled = true
		cfg.BootID.RotateUniqueIDs = rotate
		n := newMDNSTestNode(t, cfg)
		want := 0
		if rotate {
			want = 1
		}
		if calls != want {
			t.Fatalf("RotateUniqueIDs %v: EnableRotation called %d times, want %d", rotate, calls, want)
		}
		if _, found, err := n.st.GetSetting(context.Background(), store.SettingUniqueID); err != nil || !found {
			t.Fatalf("UniqueID not pinned: %v %v", found, err)
		}
	}
}
