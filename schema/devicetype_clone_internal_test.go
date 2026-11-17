// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

import "testing"

// TestDeviceTypeDefinitionCloneOfNil: the deep copy the device-type
// accessors return keeps a nil definition nil rather than handing out a
// zero one (review II, wave 1's accessor copies).
func TestDeviceTypeDefinitionCloneOfNil(t *testing.T) {
	t.Parallel()
	if c := (*DeviceTypeDefinition)(nil).clone(); c != nil {
		t.Errorf("clone of a nil definition = %#v, want nil", c)
	}
}
