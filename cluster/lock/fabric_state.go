// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock

import (
	"context"
	"slices"

	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/contract"
)

// undefinedFabricIndex is the index a removed fabric's creator and
// last-modified fields take: connectedhomeip kUndefinedFabricIndex
// (src/lib/core/DataModelTypes.h:69 at 6170af84).
const undefinedFabricIndex uint8 = 0

// The lock's users and credentials name the fabric that created and last
// changed them, so the server takes part in the removal contract.
var _ contract.FabricScopedState = (*DoorLockServer)(nil)

// MatterDropFabric implements [contract.FabricScopedState]: the removed
// fabric's index leaves the creator and last-modified fields of every
// occupied user and of every credential, which take the undefined index 0,
// and the records are saved. Nothing else changes — no user, credential or
// schedule is removed, no LockUserChange event is emitted — so a later
// fabric given the same index is no creator of them. Mirrors
// connectedhomeip src/app/clusters/door-lock-server/CodegenIntegration.cpp
// at 6170af84: DoorLockServer::OnFabricRemoved (:1487-1516) runs
// clearFabricFromUsers (:2180-2225; an Available slot is skipped,
// :2198-2202) and clearFabricFromCredentials (:3296-3389; a
// credential this server stores is occupied). matter.js
// keeps the index: DoorLockServer.ts reacts to no fabric's deletion at
// db021da0 (BD-Matter-DoorLock-CreatorFabricIndexCleared). The error is the
// AuthStore's; the records then keep the index until the next reconcile.
func (s *DoorLockServer) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	if fabricIndex == undefinedFabricIndex {
		return nil
	}
	s.op.Lock()
	defer s.op.Unlock()
	return s.commit(ctx, func() error {
		strip := func(creator, modifier *uint8) {
			if *creator == fabricIndex {
				*creator, s.dirty = undefinedFabricIndex, true
			}
			if *modifier == fabricIndex {
				*modifier, s.dirty = undefinedFabricIndex, true
			}
		}
		for i := range s.data.Users {
			if u := &s.data.Users[i]; userOccupied(u.UserStatus) {
				strip(&u.CreatorFabricIndex, &u.LastModifiedFabricIndex)
			}
		}
		for i := range s.data.Credentials {
			c := &s.data.Credentials[i]
			strip(&c.CreatorFabricIndex, &c.LastModifiedFabricIndex)
		}
		return nil
	})
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// named as creator or last modifier of an occupied user or of a
// credential, the undefined index 0 left out.
func (s *DoorLockServer) MatterHeldFabrics() []uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uint8
	add := func(fabrics ...uint8) {
		for _, f := range fabrics {
			if f != undefinedFabricIndex && !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	for _, u := range s.data.Users {
		if userOccupied(u.UserStatus) {
			add(u.CreatorFabricIndex, u.LastModifiedFabricIndex)
		}
	}
	for _, c := range s.data.Credentials {
		add(c.CreatorFabricIndex, c.LastModifiedFabricIndex)
	}
	return out
}

// userOccupied reports a user slot that is not Available, the users
// connectedhomeip's clearFabricFromUsers clears
// (CodegenIntegration.cpp:2198-2202 at 6170af84).
func userOccupied(status uint8) bool {
	return status != uint8(lockdef.UserStatusAvailable)
}
