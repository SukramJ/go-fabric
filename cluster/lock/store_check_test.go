// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
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
