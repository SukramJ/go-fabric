// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: the `it` cases directly in describe("DoorLockServer")
// (:311-816) — SetCredential's statuses, the next credential index, the
// use cases of § 5.2.10.20 and the programming PIN — on createLock
// (DoorLockServerTest.ts:92-96). matter.js invokes on the lock's local
// agent of MockServerNode.online (:362-363); the port invokes over the
// wire of a commissioned pair (doorlockserver_harness_test.go).

package lock_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
)

const (
	statusSuccess        = lockdef.StatusSuccess
	statusInvalidCommand = lockdef.StatusInvalidCommand
	statusDuplicate      = lockdef.Status(lockdef.StatusCodeDuplicate)
	statusOccupied       = lockdef.Status(lockdef.StatusCodeOccupied)
)

func TestDoorLockServerParity(t *testing.T) {
	t.Run("refuses SetCredential naming an OperationType its constraint omits", func(t *testing.T) {
		f := createLock(t, nil)
		// The request creates its own user, so it does not depend on a
		// seeded record any particular fabric owns (:314).
		req := addPin(2, "9876", noUser)

		req.OperationType = lockdef.DataOperationTypeClear
		if se := f.refused(lockdef.CmdSetCredential, req); se.Status != im.StatusInvalidCommand {
			t.Errorf("Clear: interaction status 0x%02X, want INVALID_COMMAND and no payload", se.Status)
		}

		req.OperationType = lockdef.DataOperationTypeAdd
		if got := f.setCredential(req); got.Status != statusSuccess {
			t.Errorf("Add: payload status %d, want Success", got.Status)
		}
	})

	t.Run("reports DUPLICATE when CredentialData duplicates another credential of the same CredentialType", func(t *testing.T) {
		f := createLock(t, nil)
		if got := f.setCredential(addPin(1, "1234", user1)); got.Status != statusSuccess {
			t.Fatalf("first %d, want Success", got.Status)
		}
		if got := f.setCredential(addPin(2, "1234", user1)); got.Status != statusDuplicate {
			t.Errorf("duplicate %d, want DUPLICATE", got.Status)
		}
	})

	t.Run("reports OCCUPIED when an Add operation targets an occupied CredentialIndex", func(t *testing.T) {
		f := createLock(t, nil)
		if got := f.setCredential(addPin(1, "1234", user1)); got.Status != statusSuccess {
			t.Fatalf("first %d, want Success", got.Status)
		}
		if got := f.setCredential(addPin(1, "5678", user1)); got.Status != statusOccupied {
			t.Errorf("occupied %d, want OCCUPIED", got.Status)
		}
	})

	t.Run("creates the user alongside the credential when no user index is given", func(t *testing.T) {
		f := createLock(t, nil)
		added := f.setCredential(addPin(1, "1234", noUser))
		if added.Status != statusSuccess || added.UserIndex.Null || added.UserIndex.Value != 2 {
			t.Fatalf("added %+v, want Success for user 2", added)
		}
		user := f.getUser(2)
		if !user.UserUniqueId.Null {
			t.Errorf("userUniqueId %+v, want null", user.UserUniqueId)
		}
		if user.UserStatus.Null || user.UserStatus.Value != lockdef.UserStatusOccupiedEnabled {
			t.Errorf("userStatus %+v, want OccupiedEnabled", user.UserStatus)
		}
		if user.UserType.Null || user.UserType.Value != lockdef.UserTypeUnrestrictedUser {
			t.Errorf("userType %+v, want UnrestrictedUser", user.UserType)
		}
		want := []lockdef.CredentialStruct{credential(lockdef.CredentialTypePin, 1)}
		if user.Credentials.Null || !equalCredentials(user.Credentials.Value, want) {
			t.Errorf("credentials %+v, want %+v", user.Credentials, want)
		}
	})

	t.Run("reports OCCUPIED when no user slot remains for the new credential", func(t *testing.T) {
		f := createLock(t, func(c *lock.DoorLockConfig) { c.NumberOfTotalUsersSupported = 1 })
		if got := f.setCredential(addPin(1, "1234", noUser)); got.Status != statusOccupied {
			t.Errorf("exhausted %d, want OCCUPIED", got.Status)
		}
		status := response[lockdef.GetCredentialStatusResponse](t, f.must(lockdef.CmdGetCredentialStatus,
			lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypePin, 1)}))
		if status.CredentialExists {
			t.Error("the credential exists, want it removed again")
		}
	})

	t.Run("refuses to create a programming user alongside a credential", func(t *testing.T) {
		f := createLock(t, nil)
		req := addPin(1, "1234", noUser)
		req.UserType = spec.ValueOf(lockdef.UserTypeProgrammingUser)
		if got := f.setCredential(req); got.Status != statusInvalidCommand {
			t.Errorf("refused %d, want INVALID_COMMAND", got.Status)
		}
	})

	t.Run("reports the next available credential index whatever the status", func(t *testing.T) {
		f := createLock(t, nil)
		f.setCredential(addPin(1, "1234", user1))
		duplicate := f.setCredential(addPin(2, "1234", user1))
		if duplicate.Status != statusDuplicate || !isIndex(duplicate.NextCredentialIndex, 3) {
			t.Errorf("duplicate %d next %v, want DUPLICATE next 3", duplicate.Status, nextIndex(duplicate.NextCredentialIndex))
		}
		tooShort := f.setCredential(addPin(2, "1", user1))
		if tooShort.Status != statusInvalidCommand || !isIndex(tooShort.NextCredentialIndex, 3) {
			t.Errorf("too short %d next %v, want INVALID_COMMAND next 3", tooShort.Status, nextIndex(tooShort.NextCredentialIndex))
		}
	})

	t.Run("refuses a credential index beyond the supported count", func(t *testing.T) {
		f := createLock(t, nil)
		refused := f.setCredential(addPin(11, "1234", user1))
		if refused.Status != statusInvalidCommand {
			t.Errorf("refused %d, want INVALID_COMMAND", refused.Status)
		}
		// Nothing follows the last supported index, so there is no next
		// index to report (:534-535).
		if n := nextIndex(refused.NextCredentialIndex); n != nil || refused.NextCredentialIndex == nil {
			t.Errorf("next %v (present %v), want null", n, refused.NextCredentialIndex != nil)
		}
	})

	t.Run("reports the next available credential index when modifying a credential", func(t *testing.T) {
		f := createLock(t, nil)
		f.setCredential(addPin(1, "1234", user1))
		req := addPin(1, "5678", user1)
		req.OperationType = lockdef.DataOperationTypeModify
		modified := f.setCredential(req)
		if modified.Status != statusSuccess || !isIndex(modified.NextCredentialIndex, 2) {
			t.Errorf("modified %d next %v, want Success next 2", modified.Status, nextIndex(modified.NextCredentialIndex))
		}
	})

	t.Run("refuses user fields when the credential joins an existing user", func(t *testing.T) {
		f := createLock(t, nil)
		req := addPin(1, "1234", user1)
		req.UserStatus = spec.ValueOf(lockdef.UserStatusOccupiedEnabled)
		if got := f.setCredential(req); got.Status != statusInvalidCommand {
			t.Errorf("refused %d, want INVALID_COMMAND", got.Status)
		}
	})

	t.Run("refuses user fields when modifying a credential", func(t *testing.T) {
		f := createLock(t, nil)
		f.setCredential(addPin(1, "1234", user1))
		withStatus := addPin(1, "5678", user1)
		withStatus.OperationType = lockdef.DataOperationTypeModify
		withStatus.UserStatus = spec.ValueOf(lockdef.UserStatusOccupiedEnabled)
		if got := f.setCredential(withStatus); got.Status != statusInvalidCommand {
			t.Errorf("with userStatus %d, want INVALID_COMMAND", got.Status)
		}
		withType := addPin(1, "5678", user1)
		withType.OperationType = lockdef.DataOperationTypeModify
		withType.UserType = spec.ValueOf(lockdef.UserTypeUnrestrictedUser)
		if got := f.setCredential(withType); got.Status != statusInvalidCommand {
			t.Errorf("with userType %d, want INVALID_COMMAND", got.Status)
		}
	})

	t.Run("stores the user status and type the request carries", func(t *testing.T) {
		f := createLock(t, nil)
		req := addPin(1, "1234", noUser)
		req.UserStatus = spec.ValueOf(lockdef.UserStatusOccupiedDisabled)
		req.UserType = spec.ValueOf(lockdef.UserTypeNonAccessUser)
		if got := f.setCredential(req); got.Status != statusSuccess {
			t.Fatalf("added %d, want Success", got.Status)
		}
		user := f.getUser(2)
		if user.UserStatus.Value != lockdef.UserStatusOccupiedDisabled || user.UserType.Value != lockdef.UserTypeNonAccessUser {
			t.Errorf("user status %+v type %+v, want OccupiedDisabled NonAccessUser", user.UserStatus, user.UserType)
		}
	})

	t.Run("modifies the programming PIN only as the specification states that use case", func(t *testing.T) {
		f := createLock(t, nil)
		programming := func(op lockdef.DataOperationTypeEnum, code string, userIndex spec.Nullable[uint16], userType spec.Nullable[lockdef.UserTypeEnum]) lockdef.SetCredentialRequest {
			req := addPin(0, code, userIndex)
			req.OperationType = op
			req.Credential = credential(lockdef.CredentialTypeProgrammingPin, 0)
			req.UserType = userType
			return req
		}
		none := spec.NullOf[lockdef.UserTypeEnum]()
		programmingUser := spec.ValueOf(lockdef.UserTypeProgrammingUser)
		if got := f.setCredential(programming(lockdef.DataOperationTypeAdd, "1234", user1, none)); got.Status != statusSuccess {
			t.Fatalf("seeded %d, want Success", got.Status)
		}
		if got := f.setCredential(programming(lockdef.DataOperationTypeModify, "5678", noUser, programmingUser)); got.Status != statusSuccess {
			t.Errorf("modified %d, want Success", got.Status)
		}
		if got := f.setCredential(programming(lockdef.DataOperationTypeModify, "9012", noUser, none)); got.Status != statusInvalidCommand {
			t.Errorf("without type %d, want INVALID_COMMAND", got.Status)
		}
		if got := f.setCredential(addPin(1, "3456", user1)); got.Status != statusSuccess {
			t.Fatalf("seeded PIN %d, want Success", got.Status)
		}
		wrongType := addPin(1, "7890", noUser)
		wrongType.OperationType = lockdef.DataOperationTypeModify
		wrongType.UserType = programmingUser
		if got := f.setCredential(wrongType); got.Status != statusInvalidCommand {
			t.Errorf("wrong credential type %d, want INVALID_COMMAND", got.Status)
		}
	})

	t.Run("reserves credential index 0 for the programming PIN", func(t *testing.T) {
		f := createLock(t, nil)
		if got := f.setCredential(addPin(0, "1234", user1)); got.Status != statusInvalidCommand {
			t.Errorf("PIN at 0 %d, want INVALID_COMMAND", got.Status)
		}
		if got := f.setCredential(addPin(10, "5678", user1)); got.Status != statusSuccess {
			t.Errorf("PIN at max %d, want Success", got.Status)
		}
		if got := f.setCredential(addPin(11, "6789", user1)); got.Status != statusInvalidCommand {
			t.Errorf("PIN past max %d, want INVALID_COMMAND", got.Status)
		}
		elsewhere := addPin(1, "1234", user1)
		elsewhere.Credential = credential(lockdef.CredentialTypeProgrammingPin, 1)
		if got := f.setCredential(elsewhere); got.Status != statusInvalidCommand {
			t.Errorf("programming PIN at 1 %d, want INVALID_COMMAND", got.Status)
		}
	})

	t.Run("reports no next index for the programming PIN", func(t *testing.T) {
		f := createLock(t, nil)
		req := addPin(0, "1234", user1)
		req.Credential = credential(lockdef.CredentialTypeProgrammingPin, 0)
		added := f.setCredential(req)
		if added.Status != statusSuccess {
			t.Fatalf("added %d, want Success", added.Status)
		}
		if n := nextIndex(added.NextCredentialIndex); n != nil || added.NextCredentialIndex == nil {
			t.Errorf("next %v (present %v), want null", n, added.NextCredentialIndex != nil)
		}
	})

	t.Run("reports DUPLICATE ahead of a malformed user field", func(t *testing.T) {
		f := createLock(t, nil)
		f.setCredential(addPin(1, "1234", user1))
		// The CHIP SDK scans for duplicates before it validates the user
		// fields (:782).
		both := addPin(2, "1234", user1)
		both.UserStatus = spec.ValueOf(lockdef.UserStatusOccupiedEnabled)
		if got := f.setCredential(both); got.Status != statusDuplicate {
			t.Errorf("both %d, want DUPLICATE", got.Status)
		}
	})

	t.Run("refuses to store a user type Matter does not define", func(t *testing.T) {
		f := createLock(t, nil)
		// matter.js calls the local agent, whose request skips the command's
		// constraint and the state's enum validation refuses the user (:800).
		// The Go server validates the field itself, so the port asks over the
		// wire and the local call alike: INVALID_COMMAND naming the value.
		req := addPin(1, "1234", noUser)
		req.UserType = spec.ValueOf(lockdef.UserTypeEnum(99))
		se := f.refused(lockdef.CmdSetCredential, req)
		if se.Status != im.StatusInvalidCommand {
			t.Errorf("over the wire 0x%02X, want INVALID_COMMAND", se.Status)
		}
		_, err := f.srv.MatterInvoke(withPairFabric(), lockdef.CmdSetCredential, req)
		if err == nil || !strings.Contains(err.Error(), "does not define the enum value 99") {
			t.Errorf("local call %v, want the enum refusal", err)
		}
	})
}

func equalCredentials(a, b []lockdef.CredentialStruct) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isIndex(n *spec.Nullable[uint16], want uint16) bool {
	v := nextIndex(n)
	return v != nil && *v == want
}
