// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("LockUserChange") (:1704-1945) — the event every
// user, credential and schedule command reports, on setUpScheduledLock
// (doorlockserver_harness_test.go).

package lock_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
)

// change is the summary of a LockUserChange (:1705-1712).
type change struct {
	lockDataType      lockdef.LockDataTypeEnum
	dataOperationType lockdef.DataOperationTypeEnum
	userIndex         spec.Nullable[uint16]
	dataIndex         spec.Nullable[uint16]
}

func (f *lockFixture) changeSummary(all bool) []change {
	f.t.Helper()
	ev := f.events()
	if all {
		ev = f.allEvents()
	}
	out := []change{}
	for _, c := range ev.userChanges {
		out = append(out, change{c.LockDataType, c.DataOperationType, c.UserIndex, c.DataIndex})
	}
	return out
}

func checkChanges(t *testing.T, got, want []change) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("user changes %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("user change %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func idx(v uint16) spec.Nullable[uint16] { return spec.ValueOf(v) }

func TestDoorLockServerLockUserChangeParity(t *testing.T) {
	null := spec.NullOf[uint16]()

	t.Run("reports schedule changes", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeWeekDayScheduleUser, nil))
		_, _, epochS := f.currentLocal()
		start, end := uint32(epochS), uint32(epochS+60)
		f.must(lockdef.CmdSetWeekDaySchedule, lockdef.SetWeekDayScheduleRequest{
			WeekDayIndex: 2, UserIndex: 1, DaysMask: lockdef.DaysMaskMonday, StartHour: 8, StartMinute: 0, EndHour: 9, EndMinute: 0,
		})
		f.must(lockdef.CmdClearWeekDaySchedule, lockdef.ClearWeekDayScheduleRequest{WeekDayIndex: 2, UserIndex: 1})
		f.must(lockdef.CmdClearWeekDaySchedule, lockdef.ClearWeekDayScheduleRequest{WeekDayIndex: 0xFE, UserIndex: 1})
		f.must(lockdef.CmdSetYearDaySchedule, lockdef.SetYearDayScheduleRequest{YearDayIndex: 3, UserIndex: 1, LocalStartTime: start, LocalEndTime: end})
		f.must(lockdef.CmdClearYearDaySchedule, lockdef.ClearYearDayScheduleRequest{YearDayIndex: 3, UserIndex: 1})
		f.must(lockdef.CmdSetHolidaySchedule, lockdef.SetHolidayScheduleRequest{
			HolidayIndex: 4, LocalStartTime: start, LocalEndTime: end, OperatingMode: lockdef.OperatingModeNormal,
		})
		f.must(lockdef.CmdClearHolidaySchedule, lockdef.ClearHolidayScheduleRequest{HolidayIndex: 4})

		add, clear := lockdef.DataOperationTypeAdd, lockdef.DataOperationTypeClear
		checkChanges(t, f.changeSummary(true), []change{
			{lockdef.LockDataTypeWeekDaySchedule, add, idx(1), idx(2)},
			{lockdef.LockDataTypeWeekDaySchedule, clear, idx(1), idx(2)},
			{lockdef.LockDataTypeWeekDaySchedule, clear, idx(1), idx(0xFE)},
			{lockdef.LockDataTypeYearDaySchedule, add, idx(1), idx(3)},
			{lockdef.LockDataTypeYearDaySchedule, clear, idx(1), idx(3)},
			{lockdef.LockDataTypeHolidaySchedule, add, null, idx(4)},
			{lockdef.LockDataTypeHolidaySchedule, clear, null, idx(4)},
		})
	})

	t.Run("reports the user index as DataIndex for user changes", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		f.must(lockdef.CmdSetUser, lockdef.SetUserRequest{
			OperationType:  lockdef.DataOperationTypeAdd,
			UserIndex:      2,
			UserName:       spec.NullOf[string](),
			UserUniqueId:   spec.NullOf[uint32](),
			UserStatus:     spec.NullOf[lockdef.UserStatusEnum](),
			UserType:       spec.NullOf[lockdef.UserTypeEnum](),
			CredentialRule: spec.NullOf[lockdef.CredentialRuleEnum](),
		})
		name := "x"
		f.modifyUser(&name, nil)
		f.must(lockdef.CmdClearUser, lockdef.ClearUserRequest{UserIndex: 2})
		f.must(lockdef.CmdClearUser, lockdef.ClearUserRequest{UserIndex: 0xFFFE})

		checkChanges(t, f.changeSummary(true), []change{
			{lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeAdd, idx(2), idx(2)},
			{lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeModify, idx(1), idx(1)},
			{lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeClear, idx(2), idx(2)},
			{lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeClear, idx(0xFFFE), idx(0xFFFE)},
		})
	})

	t.Run("reports the owning user when the programming PIN is modified", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		programmingPin := credential(lockdef.CredentialTypeProgrammingPin, 0)
		addReq := addPin(0, "5678", user1)
		addReq.Credential = programmingPin
		if added := f.setCredential(addReq); added.Status != statusSuccess {
			t.Fatalf("added %d, want Success", added.Status)
		}
		f.markEvents()
		modReq := addPin(0, "8765", noUser)
		modReq.OperationType = lockdef.DataOperationTypeModify
		modReq.Credential = programmingPin
		modReq.UserType = spec.ValueOf(lockdef.UserTypeProgrammingUser)
		if modified := f.setCredential(modReq); modified.Status != statusSuccess {
			t.Fatalf("modified %d, want Success", modified.Status)
		}
		checkChanges(t, f.changeSummary(false), []change{
			{lockdef.LockDataTypeProgrammingCode, lockdef.DataOperationTypeModify, idx(1), idx(0)},
		})
	})

	t.Run("reports the owning user for credential changes", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		f.setCredential(addPin(3, "5678", noUser))
		modify := addPin(1, "4321", user1)
		modify.OperationType = lockdef.DataOperationTypeModify
		f.setCredential(modify)
		f.must(lockdef.CmdClearCredential, lockdef.ClearCredentialRequest{Credential: spec.ValueOf(credential(lockdef.CredentialTypePin, 1))})
		f.must(lockdef.CmdClearCredential, lockdef.ClearCredentialRequest{Credential: spec.ValueOf(credential(lockdef.CredentialTypePin, 0xFFFE))})

		checkChanges(t, f.changeSummary(true), []change{
			{lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeAdd, idx(2), idx(2)},
			{lockdef.LockDataTypePin, lockdef.DataOperationTypeAdd, idx(2), idx(3)},
			{lockdef.LockDataTypePin, lockdef.DataOperationTypeModify, idx(1), idx(1)},
			{lockdef.LockDataTypePin, lockdef.DataOperationTypeClear, idx(1), idx(1)},
			{lockdef.LockDataTypePin, lockdef.DataOperationTypeClear, idx(0xFFFE), idx(0xFFFE)},
		})
	})
}
