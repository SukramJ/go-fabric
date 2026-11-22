// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/im"
)

// TestRefusedRequestNeverReachesTheHostHandler: SetUser is one of the ten
// commands whose definition states INVALID_COMMAND as its refusal; a
// request with a UserType the cluster does not define is refused with it
// before the host's handler runs, and so is a SetHolidaySchedule with an
// undefined OperatingMode — the server's own handlers no longer judge the
// enums themselves (R2-P5-apispec-09, F-db021da0-20).
func TestRefusedRequestNeverReachesTheHostHandler(t *testing.T) {
	srv, _ := newLocal(t, nil)
	calls := 0
	host := func(context.Context, any) (any, error) { calls++; return nil, nil }
	for _, cmd := range []uint32{lockdef.CmdSetUser, lockdef.CmdSetHolidaySchedule} {
		if err := srv.Handle(cmd, host); err != nil {
			t.Fatal(err)
		}
	}
	setUser := lockdef.SetUserRequest{
		OperationType: lockdef.DataOperationTypeAdd, UserIndex: 1,
		UserName: spec.NullOf[string](), UserUniqueId: spec.NullOf[uint32](),
		UserStatus: spec.NullOf[lockdef.UserStatusEnum](), UserType: spec.ValueOf(lockdef.UserTypeEnum(99)),
		CredentialRule: spec.NullOf[lockdef.CredentialRuleEnum](),
	}
	holiday := lockdef.SetHolidayScheduleRequest{HolidayIndex: 1, OperatingMode: lockdef.OperatingModeEnum(42)}
	clearOp := setUser // OperationType outside the constraint's "add, modify"
	clearOp.OperationType, clearOp.UserType = lockdef.DataOperationTypeClear, spec.NullOf[lockdef.UserTypeEnum]()
	for name, c := range map[string]struct {
		cmd uint32
		req any
	}{
		"SetUser UserType 99":         {lockdef.CmdSetUser, setUser},
		"SetUser OperationType clear": {lockdef.CmdSetUser, clearOp},
		"SetHolidaySchedule mode 42":  {lockdef.CmdSetHolidaySchedule, holiday},
	} {
		if _, st := invokeStatus(t, srv, withPairFabric(), c.cmd, c.req); st != im.StatusInvalidCommand {
			t.Errorf("%s: %v, want INVALID_COMMAND", name, st)
		}
	}
	if calls != 0 {
		t.Errorf("the host's handler ran %d times for a refused request", calls)
	}
	setUser.UserType = spec.ValueOf(lockdef.UserTypeUnrestrictedUser)
	if _, st := invokeStatus(t, srv, withPairFabric(), lockdef.CmdSetUser, setUser); st != im.StatusSuccess || calls != 1 {
		t.Errorf("a request that fits: %v, %d handler calls", st, calls)
	}
}

// TestRefusedRequestOfTheFiveNewRefusalCommandsNeverReachesTheHostHandler:
// at b25ed960 GetWeekDaySchedule, GetYearDaySchedule, GetHolidaySchedule,
// ClearCredential and GetCredentialStatus answer a validation error with
// INVALID_COMMAND too (DoorLockServer.ts:1509-1513), where db021da0 left
// them unchecked: a CredentialType the enum does not define and a request
// without fields (validated as {}, ProtocolService.ts:624-625) are refused
// before the host's handler runs.
func TestRefusedRequestOfTheFiveNewRefusalCommandsNeverReachesTheHostHandler(t *testing.T) {
	srv, _ := newLocal(t, nil)
	calls := 0
	host := func(context.Context, any) (any, error) { calls++; return nil, nil }
	for _, cmd := range []uint32{lockdef.CmdClearCredential, lockdef.CmdGetCredentialStatus, lockdef.CmdGetWeekDaySchedule} {
		if err := srv.Handle(cmd, host); err != nil {
			t.Fatal(err)
		}
	}
	undefined := lockdef.CredentialStruct{CredentialType: lockdef.CredentialTypeEnum(9), CredentialIndex: 1}
	for name, c := range map[string]struct {
		cmd uint32
		req any
	}{
		"ClearCredential CredentialType 9":     {lockdef.CmdClearCredential, lockdef.ClearCredentialRequest{Credential: spec.ValueOf(undefined)}},
		"GetCredentialStatus CredentialType 9": {lockdef.CmdGetCredentialStatus, lockdef.GetCredentialStatusRequest{Credential: undefined}},
		"GetWeekDaySchedule without fields":    {lockdef.CmdGetWeekDaySchedule, nil},
	} {
		if _, st := invokeStatus(t, srv, withPairFabric(), c.cmd, c.req); st != im.StatusInvalidCommand {
			t.Errorf("%s: %v, want INVALID_COMMAND", name, st)
		}
	}
	if calls != 0 {
		t.Errorf("the host's handler ran %d times for a refused request", calls)
	}
}
