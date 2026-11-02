// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package eligibility

import "testing"

// TestDeviceTypeNamedRefusesAnUnknownName: a name the schema does not list
// is a mistake of compat.go, caught at init by a panic.
func TestDeviceTypeNamedRefusesAnUnknownName(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("deviceTypeNamed accepted a name the schema does not list")
		}
	}()
	_ = deviceTypeNamed("NoSuchDeviceType")
}
