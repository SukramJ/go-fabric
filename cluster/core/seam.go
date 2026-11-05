// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "github.com/SukramJ/go-fabric/internal/coreseam"

// The one-way seams that set the SpecificationVersion a BasicInformation
// server reports and hook an ICDManagement server's registration
// commands; see the coreseam package doc for why they are not fields of
// Config.
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
	coreseam.ObserveICDRegistrations = func(server any, hook func(register bool) error) bool {
		m, ok := server.(*ICDManagement)
		if !ok || m == nil {
			return false
		}
		m.mu.Lock()
		m.registrationHook = hook
		m.mu.Unlock()
		return true
	}
}
