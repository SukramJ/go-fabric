// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/lock"
	"thingfabric.dev/go-fabric/cluster/spec"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/im"
)

// TestParity_DoorLock_RecordSource (R2-P5-port-08, F-db021da0-33):
// GetUser and GetCredentialStatus answer a record's creator and
// last-modifier fabric only for what a Matter command made. A user and a
// credential the host seeded (DoorLockConfig.Initial) answer both null; a
// Matter change to the seeded user answers its last modifier and keeps the
// creator null; a user and a credential a Matter command added answer
// both. Each index follows its own source: connectedhomeip sets the
// creator only when the creation, and the last modifier only when the last
// modification, came through Matter IM (src/app/clusters/door-lock-server/
// CodegenIntegration.cpp:570-578, :967-974 at 6170af84).
func TestParity_DoorLock_RecordSource(t *testing.T) {
	t.Parallel()
	srv, _ := newLocal(t, func(c *lock.DoorLockConfig) {
		for i := range c.Initial.Users {
			c.Initial.Users[i].CreatedThroughMatter, c.Initial.Users[i].ModifiedThroughMatter = false, false
		}
		for i := range c.Initial.Credentials {
			c.Initial.Credentials[i].CreatedThroughMatter, c.Initial.Credentials[i].ModifiedThroughMatter = false, false
		}
	})
	ctx := withPairFabric()
	null, pair := spec.NullOf[uint8](), spec.ValueOf[uint8](pairFabric)

	getUser := func(index uint16) lockdef.GetUserResponse {
		t.Helper()
		res, st := invokeStatus(t, srv, ctx, lockdef.CmdGetUser, lockdef.GetUserRequest{UserIndex: index})
		if st != im.StatusSuccess {
			t.Fatalf("GetUser %d: 0x%02X", index, st)
		}
		return res.(lockdef.GetUserResponse)
	}
	credStatus := func(index uint16) lockdef.GetCredentialStatusResponse {
		t.Helper()
		res, st := invokeStatus(t, srv, ctx, lockdef.CmdGetCredentialStatus,
			lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypePin, index)})
		if st != im.StatusSuccess {
			t.Fatalf("GetCredentialStatus PIN %d: 0x%02X", index, st)
		}
		return res.(lockdef.GetCredentialStatusResponse)
	}

	if u := getUser(1); u.CreatorFabricIndex != null || u.LastModifiedFabricIndex != null {
		t.Errorf("seeded user: creator %+v, last modified %+v; want both null", u.CreatorFabricIndex, u.LastModifiedFabricIndex)
	}
	if c := credStatus(1); !c.CredentialExists || c.CreatorFabricIndex != null || c.LastModifiedFabricIndex != null {
		t.Errorf("seeded credential: %+v; want it with both indexes null", c)
	}

	modify := setUserReq(lockdef.DataOperationTypeModify, 1)
	modify.UserStatus = spec.ValueOf(lockdef.UserStatusOccupiedDisabled)
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetUser, modify); st != im.StatusSuccess {
		t.Fatalf("SetUser Modify 1: 0x%02X", st)
	}
	if u := getUser(1); u.CreatorFabricIndex != null || u.LastModifiedFabricIndex != pair {
		t.Errorf("seeded user a Matter command changed: creator %+v, last modified %+v; want null and %d", u.CreatorFabricIndex, u.LastModifiedFabricIndex, pairFabric)
	}

	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetUser, setUserReq(lockdef.DataOperationTypeAdd, 2)); st != im.StatusSuccess {
		t.Fatalf("SetUser Add 2: 0x%02X", st)
	}
	if u := getUser(2); u.CreatorFabricIndex != pair || u.LastModifiedFabricIndex != pair {
		t.Errorf("Matter-made user: creator %+v, last modified %+v; want %d", u.CreatorFabricIndex, u.LastModifiedFabricIndex, pairFabric)
	}
	res, st := invokeStatus(t, srv, ctx, lockdef.CmdSetCredential, addPin(2, "4321", spec.ValueOf[uint16](2)))
	if st != im.StatusSuccess || res.(lockdef.SetCredentialResponse).Status != lockdef.StatusSuccess {
		t.Fatalf("SetCredential Add PIN 2: 0x%02X %+v", st, res)
	}
	if c := credStatus(2); c.CreatorFabricIndex != pair || c.LastModifiedFabricIndex != pair {
		t.Errorf("Matter-made credential: %+v; want both %d", c, pairFabric)
	}
	for _, u := range srv.Users() {
		if made := u.UserIndex == 2; u.CreatedThroughMatter != made || !u.ModifiedThroughMatter {
			t.Errorf("user %d source = %v/%v", u.UserIndex, u.CreatedThroughMatter, u.ModifiedThroughMatter)
		}
	}
}
