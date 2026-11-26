// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

// SetProviderCommitRefusalW2B6 is [SetAvStreamCommitRefusal] for p's
// session table.
func SetProviderCommitRefusalW2B6(p *Provider, refuse func() error) {
	p.refuseForTests = refuse
}
