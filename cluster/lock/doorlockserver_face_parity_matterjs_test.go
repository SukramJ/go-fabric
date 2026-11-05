// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The face credentials (FACE) on the port of matter.js
// packages/node/test/behaviors/door-lock/DoorLockServerTest.ts at 862ad75f.
// DoorLockServerTest.ts names no face — nor fingerprint — credential: every
// credential case there is a PIN on DoorLockServer.with("User",
// "PinCredential") (:62). matter.js serves FACE through its base server
// (DoorLockServer.ts:86-98, "FaceCredentials" :90), and a face credential
// takes the default branch of the type switches a PIN does not:
// NumberOfTotalUsersSupported bounds its index (#maxCredentialIndexForType
// :1415-1427) and any data length is accepted (#validateCredentialDataLength
// :1429-1445). So the SetCredential cases of describe("DoorLockServer")
// (:359-437, :518-537) run once more with the Face type on createLock with
// FACE added, each named for it.

package lock_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
)

// createFaceLock is createLock (DoorLockServerTest.ts:92-96) with FACE.
func createFaceLock(t *testing.T) *lockFixture {
	t.Helper()
	return createLock(t, func(c *lock.DoorLockConfig) { c.Features |= lock.FeatureFACE })
}

// addFace is SetCredential Add of a face credential; the data is an opaque
// template, any length.
func addFace(index uint16, template string, userIndex spec.Nullable[uint16]) lockdef.SetCredentialRequest {
	req := addPin(index, template, userIndex)
	req.Credential = credential(lockdef.CredentialTypeFace, index)
	return req
}

func TestDoorLockServerFaceCredentialParity(t *testing.T) {
	t.Run("Face: reports DUPLICATE when CredentialData duplicates another face credential", func(t *testing.T) {
		f := createFaceLock(t)
		if got := f.setCredential(addFace(1, "face-template-a", user1)); got.Status != statusSuccess {
			t.Fatalf("first %d, want Success", got.Status)
		}
		if got := f.setCredential(addFace(2, "face-template-a", user1)); got.Status != statusDuplicate {
			t.Errorf("duplicate %d, want DUPLICATE", got.Status)
		}
	})

	t.Run("Face: reports OCCUPIED when an Add operation targets an occupied face CredentialIndex", func(t *testing.T) {
		f := createFaceLock(t)
		if got := f.setCredential(addFace(1, "face-template-a", user1)); got.Status != statusSuccess {
			t.Fatalf("first %d, want Success", got.Status)
		}
		if got := f.setCredential(addFace(1, "face-template-b", user1)); got.Status != statusOccupied {
			t.Errorf("occupied %d, want OCCUPIED", got.Status)
		}
	})

	t.Run("Face: creates the user alongside the face credential when no user index is given", func(t *testing.T) {
		f := createFaceLock(t)
		added := f.setCredential(addFace(1, "f", noUser))
		if added.Status != statusSuccess || added.UserIndex.Null || added.UserIndex.Value != 2 {
			t.Fatalf("added %+v, want Success for user 2", added)
		}
		user := f.getUser(2)
		if user.UserStatus.Null || user.UserStatus.Value != lockdef.UserStatusOccupiedEnabled {
			t.Errorf("userStatus %+v, want OccupiedEnabled", user.UserStatus)
		}
		if user.UserType.Null || user.UserType.Value != lockdef.UserTypeUnrestrictedUser {
			t.Errorf("userType %+v, want UnrestrictedUser", user.UserType)
		}
		if user.Credentials.Null || !equalCredentials(user.Credentials.Value, []lockdef.CredentialStruct{credential(lockdef.CredentialTypeFace, 1)}) {
			t.Errorf("credentials %+v, want [Face 1]", user.Credentials)
		}
	})

	t.Run("Face: refuses a face credential index beyond NumberOfTotalUsersSupported", func(t *testing.T) {
		f := createFaceLock(t)
		refused := f.setCredential(addFace(11, "face-template-a", user1))
		if refused.Status != statusInvalidCommand {
			t.Errorf("status %d, want INVALID_COMMAND", refused.Status)
		}
		if nextIndex(refused.NextCredentialIndex) != nil {
			t.Errorf("nextCredentialIndex %v, want null", nextIndex(refused.NextCredentialIndex))
		}
	})
}
