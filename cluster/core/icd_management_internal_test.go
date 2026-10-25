// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "testing"

// mustICD builds an ICD Management server for the definition checks.
func mustICD(t *testing.T, cfg ICDManagementConfig) *ICDManagement {
	t.Helper()
	s, err := NewICDManagement(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
