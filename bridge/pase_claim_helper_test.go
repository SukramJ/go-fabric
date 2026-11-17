// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// claimPaseInFlight is [Bridge.claimPase] for the tests that only ask
// whether key holds the slot afterwards: taken now, or held by key already.
func (b *Bridge) claimPaseInFlight(key HandshakeKey) bool {
	return b.claimPase(key) != paseClaimBusy
}
