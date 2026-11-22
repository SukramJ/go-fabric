// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/lock"
	"thingfabric.dev/go-fabric/cluster/spec"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// removedFabric is the fabric the cases remove; pairFabric stays.
const removedFabric = 2

// lockUser is an occupied user a Matter command made, of the given creator
// and last modifier.
func lockUser(index uint16, creator, modifier uint8, creds ...store.DoorLockCredentialRef) store.DoorLockUser {
	return store.DoorLockUser{
		UserIndex:               index,
		UserStatus:              uint8(lockdef.UserStatusOccupiedEnabled),
		UserType:                uint8(lockdef.UserTypeUnrestrictedUser),
		CredentialRule:          uint8(lockdef.CredentialRuleSingle),
		Credentials:             append([]store.DoorLockCredentialRef{}, creds...),
		CreatorFabricIndex:      creator,
		LastModifiedFabricIndex: modifier,
		CreatedThroughMatter:    true,
		ModifiedThroughMatter:   true,
	}
}

// fabricRecords is a lock whose users and credentials were created and
// changed by the pair's fabric and by removedFabric, persisted in memory.
func fabricRecords(auth *memoryAuth) func(*lock.DoorLockConfig) {
	return func(c *lock.DoorLockConfig) {
		pin2 := store.DoorLockCredentialRef{Type: uint8(lockdef.CredentialTypePin), Index: 2}
		available := lockUser(4, removedFabric, removedFabric)
		available.UserStatus = uint8(lockdef.UserStatusAvailable)
		c.Initial.Users = append(c.Initial.Users,
			lockUser(2, removedFabric, removedFabric, pin2),
			lockUser(3, pairFabric, removedFabric),
			available,
		)
		c.Initial.Credentials = append(c.Initial.Credentials, store.DoorLockCredential{
			Type: uint8(lockdef.CredentialTypePin), Index: 2, Data: pin("5678"),
			CreatorFabricIndex: removedFabric, LastModifiedFabricIndex: removedFabric,
			CreatedThroughMatter: true, ModifiedThroughMatter: true,
		})
		c.Initial.WeekDaySchedules = []store.DoorLockWeekDaySchedule{{
			WeekDayIndex: 1, UserIndex: 2, DaysMask: 1, StartHour: 8, EndHour: 18,
		}}
		c.Auth, c.AuthKey = auth, "lock"
	}
}

// TestDropFabricClearsTheRemovedFabricFromUsersAndCredentials: a removed
// fabric's index leaves the creator and last-modified fields of every
// occupied user and credential and becomes the undefined index 0; the
// users, their credentials and schedules stay, another fabric's fields
// stay, and the records are persisted. A later fabric given the same
// index is no user's creator. Mirrors connectedhomeip
// src/app/clusters/door-lock-server/CodegenIntegration.cpp:1487-1516 at
// 6170af84 (DoorLockServer::OnFabricRemoved), :2198-2212
// (clearFabricFromUsers: occupied users only) and :3333-3347
// (clearFabricFromCredentials), with kUndefinedFabricIndex 0
// (src/lib/core/DataModelTypes.h:69); GetUser and GetCredentialStatus
// answer the cleared index as a value (:573, :577, :969, :973). matter.js
// keeps the index (BD-Matter-DoorLock-CreatorFabricIndexCleared).
func TestDropFabricClearsTheRemovedFabricFromUsersAndCredentials(t *testing.T) {
	t.Parallel()
	auth := newMemoryAuth()
	srv, _ := newLocal(t, fabricRecords(auth))
	ctx := withPairFabric()
	removed := im.WithFabricFilter(context.Background(), false, removedFabric)

	fs, ok := any(srv).(contract.FabricScopedState)
	if !ok {
		t.Fatal("DoorLockServer does not implement contract.FabricScopedState: a removed fabric's index stays in its users and credentials")
	}
	if got := fs.MatterHeldFabrics(); !slices.Equal(sorted(got), []uint8{pairFabric, removedFabric}) {
		t.Fatalf("held %v, want [1 2]", got)
	}
	if err := fs.MatterDropFabric(context.Background(), removedFabric); err != nil {
		t.Fatal(err)
	}
	if got := fs.MatterHeldFabrics(); !slices.Equal(got, []uint8{pairFabric}) {
		t.Errorf("held after the drop %v, want [1]", got)
	}

	want := map[uint16][2]uint8{1: {pairFabric, pairFabric}, 2: {0, 0}, 3: {pairFabric, 0}, 4: {removedFabric, removedFabric}}
	users := srv.Users()
	if len(users) != len(want) {
		t.Fatalf("users %+v, want %d: a removal deletes no user", users, len(want))
	}
	for _, u := range users {
		if got := [2]uint8{u.CreatorFabricIndex, u.LastModifiedFabricIndex}; got != want[u.UserIndex] {
			t.Errorf("user %d creator/last modified %v, want %v", u.UserIndex, got, want[u.UserIndex])
		}
	}
	if u, _ := srv.User(2); len(u.Credentials) != 1 {
		t.Errorf("user 2 credentials %+v, want its PIN kept", u.Credentials)
	}

	res, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetUser, lockdef.GetUserRequest{UserIndex: 2})
	got := res.(lockdef.GetUserResponse)
	if got.CreatorFabricIndex != spec.ValueOf[uint8](0) || got.LastModifiedFabricIndex != spec.ValueOf[uint8](0) {
		t.Errorf("GetUser 2 creator %+v last modified %+v, want the value 0", got.CreatorFabricIndex, got.LastModifiedFabricIndex)
	}
	cres, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetCredentialStatus, lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypePin, 2)})
	cs := cres.(lockdef.GetCredentialStatusResponse)
	if !cs.CredentialExists || cs.CreatorFabricIndex != spec.ValueOf[uint8](0) || cs.LastModifiedFabricIndex != spec.ValueOf[uint8](0) {
		t.Errorf("GetCredentialStatus PIN 2 = %+v, want the credential with creator and last modified 0", cs)
	}
	cres, _ = invokeStatus(t, srv, ctx, lockdef.CmdGetCredentialStatus, lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypePin, 1)})
	if cs := cres.(lockdef.GetCredentialStatusResponse); cs.CreatorFabricIndex != spec.ValueOf[uint8](pairFabric) {
		t.Errorf("GetCredentialStatus PIN 1 creator %+v, want the pair's fabric kept", cs.CreatorFabricIndex)
	}

	// A later fabric with the removed fabric's index is no creator.
	rename := setUserReq(lockdef.DataOperationTypeModify, 2)
	rename.UserName = spec.ValueOf("taken over")
	if _, st := invokeStatus(t, srv, removed, lockdef.CmdSetUser, rename); st != im.StatusInvalidCommand {
		t.Errorf("rename of user 2 by a fabric of the removed index: 0x%02X, want INVALID_COMMAND", st)
	}

	saved, ok := auth.saved["lock"]
	if !ok {
		t.Fatal("the cleared records were not persisted")
	}
	for _, u := range saved.Users {
		if got := [2]uint8{u.CreatorFabricIndex, u.LastModifiedFabricIndex}; got != want[u.UserIndex] {
			t.Errorf("persisted user %d creator/last modified %v, want %v", u.UserIndex, got, want[u.UserIndex])
		}
	}
	for _, c := range saved.Credentials {
		if c.Index == 2 && (c.CreatorFabricIndex != 0 || c.LastModifiedFabricIndex != 0) {
			t.Errorf("persisted PIN 2 creator/last modified %d/%d, want 0/0", c.CreatorFabricIndex, c.LastModifiedFabricIndex)
		}
	}
	if len(saved.WeekDaySchedules) != 1 {
		t.Errorf("persisted week-day schedules %+v, want user 2's kept", saved.WeekDaySchedules)
	}
}

// TestDropFabricHoldingNothingChangesNothing: a fabric no user or
// credential names leaves the records as they are and saves nothing; the
// undefined index 0 is no fabric the lock holds.
func TestDropFabricHoldingNothingChangesNothing(t *testing.T) {
	t.Parallel()
	auth := newMemoryAuth()
	srv, _ := newLocal(t, fabricRecords(auth))
	fs, ok := any(srv).(contract.FabricScopedState)
	if !ok {
		t.Fatal("DoorLockServer does not implement contract.FabricScopedState")
	}
	before := srv.Users()
	for _, f := range []uint8{0, 3} {
		if err := fs.MatterDropFabric(context.Background(), f); err != nil {
			t.Fatal(err)
		}
	}
	if _, saved := auth.saved["lock"]; saved {
		t.Error("a drop that cleared nothing saved the records")
	}
	if after := srv.Users(); !slices.EqualFunc(before, after, func(a, b store.DoorLockUser) bool {
		return a.CreatorFabricIndex == b.CreatorFabricIndex && a.LastModifiedFabricIndex == b.LastModifiedFabricIndex
	}) {
		t.Errorf("users %+v after a drop of no held fabric, want %+v", after, before)
	}
	if err := fs.MatterDropFabric(context.Background(), removedFabric); err != nil {
		t.Fatal(err)
	}
	if err := fs.MatterDropFabric(context.Background(), pairFabric); err != nil {
		t.Fatal(err)
	}
	// User 4 is Available: its fields are not cleared, and it holds nothing.
	if got := fs.MatterHeldFabrics(); len(got) != 0 {
		t.Errorf("held %v after both fabrics left, want none (0 is no fabric, an Available user holds nothing)", got)
	}
}

func sorted(s []uint8) []uint8 {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
