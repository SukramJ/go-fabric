// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tls

// idAllocator hands out the node-wide ids of a table: the next id after
// the last one handed out that no entry holds, wrapping to 0 at maxID, so
// maxID itself is never handed out. Mirrors connectedhomeip
// IncrementingIdHelper::GetNextId and ConsumeId
// (tls-certificate-management-server/IncrementingIdHelper.h:164-200):
// mNextId starts at 0 (:40) and survives a removal.
type idAllocator struct {
	next  uint16
	maxID uint16
}

// allocate returns the next free id; false when every id below maxID is
// taken (chip's CHIP_ERROR_ENDPOINT_POOL_FULL, :175). It looks at each of
// the maxID ids once, so it returns from any position — a position past
// maxID, which only a damaged record holds, starts again at 0
// (R2-P6-store-1119).
func (a *idAllocator) allocate(taken func(id uint16) bool) (uint16, bool) {
	for range int(a.maxID) {
		if a.next >= a.maxID {
			a.next = 0
		}
		id := a.next
		a.next++
		if !taken(id) {
			return id, true
		}
	}
	return 0, false
}
