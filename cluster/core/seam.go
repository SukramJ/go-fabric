// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "github.com/SukramJ/go-fabric/internal/coreseam"

// The one-way seam that sets the SpecificationVersion a BasicInformation
// server reports; see the coreseam package doc for why it is not a field
// of Config.
func init() {
	coreseam.SetSpecificationVersion = func(server any, version uint32) bool {
		b, ok := server.(*BasicInformation)
		if !ok || b == nil || version == 0 {
			return false
		}
		b.mu.Lock()
		b.specVersion = version
		b.mu.Unlock()
		return true
	}
}
