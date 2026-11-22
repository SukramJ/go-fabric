// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/lock"
	"thingfabric.dev/go-fabric/cluster/spec"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/im"
)

// A local MatterInvoke passes no request gate; the server refuses a value
// the enum does not define where it stores the record, at every store
// site the parity port (DoorLockServerTest.ts:823-845) does not reach:
// SetUser Add and Modify, and the credential's own type.

// wantUndefinedEnum asserts err is the store's refusal of value for enum.
func wantUndefinedEnum(t *testing.T, err error, value, enum string) {
	t.Helper()
	want := "Matter does not define the enum value " + value + " for " + enum
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%v, want %q", err, want)
	}
	var se *spec.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusConstraintError || !errors.Is(err, spec.ErrUnknownEnumValue) {
		t.Errorf("%v, want CONSTRAINT_ERROR wrapping ErrUnknownEnumValue", err)
	}
}

func localUser(t *testing.T, srv *lock.DoorLockServer, index uint16) lockdef.GetUserResponse {
	t.Helper()
	res, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdGetUser, lockdef.GetUserRequest{UserIndex: index})
	if err != nil {
		t.Fatal(err)
	}
	return res.(lockdef.GetUserResponse)
}

func TestLocalStoreRefusesUndefinedEnumValues(t *testing.T) {
	t.Parallel()

	t.Run("SetUser Add with an undefined UserStatus", func(t *testing.T) {
		t.Parallel()
		srv, em := newLocal(t, nil)
		req := setUserReq(lockdef.DataOperationTypeAdd, 2)
		req.UserStatus = spec.ValueOf(lockdef.UserStatusEnum(99))
		_, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req)
		wantUndefinedEnum(t, err, "99", "UserStatusEnum")
		if got := localUser(t, srv, 2); !got.UserStatus.Null {
			t.Errorf("user 2 stored with status %d", got.UserStatus.Value)
		}
		if n := len(em.calls); n != 0 {
			t.Errorf("%d events, want none", n)
		}
	})

	t.Run("SetUser Add with an undefined CredentialRule", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLocal(t, nil)
		req := setUserReq(lockdef.DataOperationTypeAdd, 2)
		req.CredentialRule = spec.ValueOf(lockdef.CredentialRuleEnum(7))
		_, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req)
		wantUndefinedEnum(t, err, "7", "CredentialRuleEnum")
		if got := localUser(t, srv, 2); !got.UserStatus.Null {
			t.Errorf("user 2 stored with status %d", got.UserStatus.Value)
		}
	})

	t.Run("SetUser Modify with an undefined UserType changes nothing", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLocal(t, nil)
		req := setUserReq(lockdef.DataOperationTypeModify, 1)
		req.UserName = spec.ValueOf("renamed")
		req.UserType = spec.ValueOf(lockdef.UserTypeEnum(99))
		_, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req)
		wantUndefinedEnum(t, err, "99", "UserTypeEnum")
		got := localUser(t, srv, 1)
		if got.UserType.Value != lockdef.UserTypeUnrestrictedUser || got.UserName.Value != "" {
			t.Errorf("user 1 changed: type %d, name %q", got.UserType.Value, got.UserName.Value)
		}
	})

	t.Run("a refused Modify leaves the expiry timer running", func(t *testing.T) {
		t.Parallel()
		// by_design BD-Matter-DoorLock-RefusedModifyKeepsExpiryTimer:
		// matter.js stops the timer before it refuses
		// (DoorLockServer.ts:312-326 at b25ed960); here the refused
		// command changes nothing, so the user is disabled at the deadline.
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.advance(30 * time.Second)
		req := setUserReq(lockdef.DataOperationTypeModify, 1)
		req.UserType = spec.ValueOf(lockdef.UserTypeEnum(99))
		_, err := f.srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req)
		wantUndefinedEnum(t, err, "99", "UserTypeEnum")
		f.advance(40 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("status %d after the deadline, want OccupiedDisabled (timer kept)", got)
		}
	})

	t.Run("SetCredential Add of an undefined CredentialType", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLocal(t, nil)
		req := addPin(2, "5678", noUser)
		req.Credential.CredentialType = lockdef.CredentialTypeEnum(99)
		_, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetCredential, req)
		wantUndefinedEnum(t, err, "99", "CredentialTypeEnum")
		if got := localUser(t, srv, 2); !got.UserStatus.Null {
			t.Errorf("user 2 stored with status %d", got.UserStatus.Value)
		}
	})

	t.Run("a defined value is stored", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLocal(t, nil)
		req := setUserReq(lockdef.DataOperationTypeAdd, 2)
		req.UserStatus = spec.ValueOf(lockdef.UserStatusOccupiedDisabled)
		req.CredentialRule = spec.ValueOf(lockdef.CredentialRuleDual)
		if _, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req); err != nil {
			t.Fatal(err)
		}
		if got := localUser(t, srv, 2); got.UserStatus.Value != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("user 2 status %v", got.UserStatus)
		}
	})
}

// wantDisallowedEnum asserts err is the store's refusal of a defined
// member whose conformance the lock's features do not meet: the message
// of matter.js's EnumValueConformanceError (conformance-compiler.ts:734-746
// at b25ed960), CONSTRAINT_ERROR wrapping ErrEnumValueConformance.
func wantDisallowedEnum(t *testing.T, err error, member string) {
	t.Helper()
	want := "Matter does not allow enum value " + member + " here"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%v, want %q", err, want)
	}
	var se *spec.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusConstraintError || !errors.Is(err, spec.ErrEnumValueConformance) {
		t.Errorf("%v, want CONSTRAINT_ERROR wrapping ErrEnumValueConformance", err)
	}
}

// A defined value whose member conformance the lock's features do not
// meet is refused at the store as well, following matter.js's store
// validation (addEnumMemberValidation, conformance-compiler.ts:762-862 at
// b25ed960; derived, not run); the request gate does the same for a peer.
func TestLocalStoreRefusesFeatureDisallowedEnumValues(t *testing.T) {
	t.Parallel()
	noCOTA := func(c *lock.DoorLockConfig) { c.Features &^= lock.FeatureCOTA }

	t.Run("SetUser Add of RemoteOnlyUser without COTA", func(t *testing.T) {
		t.Parallel()
		srv, em := newLocal(t, noCOTA)
		req := setUserReq(lockdef.DataOperationTypeAdd, 2)
		req.UserType = spec.ValueOf(lockdef.UserTypeRemoteOnlyUser)
		_, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req)
		wantDisallowedEnum(t, err, "RemoteOnlyUser (ID 9)")
		if got := localUser(t, srv, 2); !got.UserStatus.Null {
			t.Errorf("user 2 stored with status %d", got.UserStatus.Value)
		}
		if n := len(em.calls); n != 0 {
			t.Errorf("%d events, want none", n)
		}
	})

	t.Run("SetUser Modify to RemoteOnlyUser without COTA changes nothing", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLocal(t, noCOTA)
		req := setUserReq(lockdef.DataOperationTypeModify, 1)
		req.UserName = spec.ValueOf("renamed")
		req.UserType = spec.ValueOf(lockdef.UserTypeRemoteOnlyUser)
		_, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req)
		wantDisallowedEnum(t, err, "RemoteOnlyUser (ID 9)")
		got := localUser(t, srv, 1)
		if got.UserType.Value != lockdef.UserTypeUnrestrictedUser || got.UserName.Value != "" {
			t.Errorf("user 1 changed: type %d, name %q", got.UserType.Value, got.UserName.Value)
		}
	})

	t.Run("SetCredential Add of a Fingerprint without FGP", func(t *testing.T) {
		t.Parallel()
		srv, em := newLocal(t, nil)
		req := addPin(1, "5678", noUser)
		req.Credential = credential(lockdef.CredentialTypeFingerprint, 1)
		_, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetCredential, req)
		wantDisallowedEnum(t, err, "Fingerprint (ID 3)")
		res, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdGetCredentialStatus,
			lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypeFingerprint, 1)})
		if err != nil {
			t.Fatal(err)
		}
		if res.(lockdef.GetCredentialStatusResponse).CredentialExists {
			t.Error("fingerprint 1 stored")
		}
		if got := localUser(t, srv, 2); !got.UserStatus.Null {
			t.Errorf("user 2 stored with status %d", got.UserStatus.Value)
		}
		if n := len(em.calls); n != 0 {
			t.Errorf("%d events, want none", n)
		}
	})

	t.Run("SetUser Add of ScheduleRestrictedUser without WDSCH and YDSCH", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLocal(t, func(c *lock.DoorLockConfig) { c.Features &^= lock.FeatureWDSCH | lock.FeatureYDSCH })
		req := setUserReq(lockdef.DataOperationTypeAdd, 2)
		req.UserType = spec.ValueOf(lockdef.UserTypeScheduleRestrictedUser)
		_, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req)
		wantDisallowedEnum(t, err, "ScheduleRestrictedUser (ID 8)")
	})

	t.Run("the member is stored when the features allow it", func(t *testing.T) {
		t.Parallel()
		srv, _ := newLocal(t, nil)
		for idx, typ := range map[uint16]lockdef.UserTypeEnum{2: lockdef.UserTypeRemoteOnlyUser, 3: lockdef.UserTypeExpiringUser} {
			req := setUserReq(lockdef.DataOperationTypeAdd, idx)
			req.UserType = spec.ValueOf(typ)
			if _, err := srv.MatterInvoke(withPairFabric(), lockdef.CmdSetUser, req); err != nil {
				t.Fatalf("user type %d: %v", typ, err)
			}
			if got := localUser(t, srv, idx); got.UserType.Value != typ {
				t.Errorf("user %d type %v, want %d", idx, got.UserType, typ)
			}
		}
		fgp, _ := newLocal(t, func(c *lock.DoorLockConfig) { c.Features |= lock.FeatureFGP })
		req := addPin(1, "5678", noUser)
		req.Credential = credential(lockdef.CredentialTypeFingerprint, 1)
		res, err := fgp.MatterInvoke(withPairFabric(), lockdef.CmdSetCredential, req)
		if err != nil {
			t.Fatal(err)
		}
		if st := res.(lockdef.SetCredentialResponse).Status; st != lockdef.StatusSuccess {
			t.Errorf("fingerprint status %d, want SUCCESS", st)
		}
	})
}
