// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("schedule-restricted access (spec § 5.2.6.18.2, .3,
// .9)") (:818-969, without its nested "week day window bounds") — users
// whose WeekDay and YearDay schedules decide an unlock, on
// setUpScheduledLock (doorlockserver_harness_test.go). The schedules are
// read in time.Local, the zone the lock reads them in.

package lock_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/lock"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

func TestDoorLockServerScheduleRestrictedAccessParity(t *testing.T) {
	// `denies a ${UserType[userType]} with no schedules configured` (:853-856 at b25ed960).
	for _, userType := range []lockdef.UserTypeEnum{
		lockdef.UserTypeWeekDayScheduleUser, lockdef.UserTypeYearDayScheduleUser, lockdef.UserTypeScheduleRestrictedUser,
	} {
		t.Run("denies a ${UserType[userType]} with no schedules configured/"+userTypeName(userType), func(t *testing.T) {
			f := setUpScheduledLock(t, nil, scheduledUser(userType, nil))
			f.expectScheduledDenial(lockdef.OperationErrorRestricted)
		})
	}

	t.Run("grants a WeekDayScheduleUser inside a matching schedule and denies outside it", func(t *testing.T) {
		day, hour, _ := currentLocal()
		other := (day + 1) % 7
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
			c.Initial.WeekDaySchedules = []store.DoorLockWeekDaySchedule{{
				WeekDayIndex: 1, UserIndex: 1, DaysMask: 1 << uint(other), StartHour: 0, StartMinute: 0, EndHour: 23, EndMinute: 59,
			}}
		}, scheduledUser(lockdef.UserTypeWeekDayScheduleUser, nil))
		f.expectScheduledDenial(lockdef.OperationErrorRestricted)

		f.must(lockdef.CmdSetWeekDaySchedule, lockdef.SetWeekDayScheduleRequest{
			WeekDayIndex: 1, UserIndex: 1, DaysMask: lockdef.DaysMaskBitmap(1 << uint(day)),
			StartHour: uint8(hour), StartMinute: 0, EndHour: 23, EndMinute: 59,
		})
		if err := f.unlockScheduled(); err != nil {
			t.Fatalf("unlock inside the schedule: %v", err)
		}
	})

	t.Run("grants a YearDayScheduleUser inside a matching time window and denies outside it", func(t *testing.T) {
		_, _, epochS := currentLocal()
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
			c.Initial.YearDaySchedules = []store.DoorLockYearDaySchedule{{
				YearDayIndex: 1, UserIndex: 1, LocalStartTime: uint32(epochS - 7200), LocalEndTime: uint32(epochS - 3600),
			}}
		}, scheduledUser(lockdef.UserTypeYearDayScheduleUser, nil))
		f.expectScheduledDenial(lockdef.OperationErrorRestricted)

		f.must(lockdef.CmdSetYearDaySchedule, lockdef.SetYearDayScheduleRequest{
			YearDayIndex: 1, UserIndex: 1, LocalStartTime: uint32(epochS - 60), LocalEndTime: uint32(epochS + 3600),
		})
		if err := f.unlockScheduled(); err != nil {
			t.Fatalf("unlock inside the window: %v", err)
		}
	})

	t.Run("requires both WeekDay AND YearDay schedules to match for a ScheduleRestrictedUser once both are set", func(t *testing.T) {
		day, hour, epochS := currentLocal()
		other := (day + 1) % 7
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
			c.Initial.WeekDaySchedules = []store.DoorLockWeekDaySchedule{{
				WeekDayIndex: 1, UserIndex: 1, DaysMask: 1 << uint(day), StartHour: uint8(hour), StartMinute: 0, EndHour: 23, EndMinute: 59,
			}}
			c.Initial.YearDaySchedules = []store.DoorLockYearDaySchedule{{
				YearDayIndex: 1, UserIndex: 1, LocalStartTime: uint32(epochS - 7200), LocalEndTime: uint32(epochS - 3600),
			}}
		}, scheduledUser(lockdef.UserTypeScheduleRestrictedUser, nil))
		f.expectScheduledDenial(lockdef.OperationErrorRestricted)

		f.must(lockdef.CmdSetYearDaySchedule, lockdef.SetYearDayScheduleRequest{
			YearDayIndex: 1, UserIndex: 1, LocalStartTime: uint32(epochS - 60), LocalEndTime: uint32(epochS + 3600),
		})
		if err := f.unlockScheduled(); err != nil {
			t.Fatalf("unlock with both matching: %v", err)
		}

		f.must(lockdef.CmdSetWeekDaySchedule, lockdef.SetWeekDayScheduleRequest{
			WeekDayIndex: 1, UserIndex: 1, DaysMask: lockdef.DaysMaskBitmap(1 << uint(other)),
			StartHour: 0, StartMinute: 0, EndHour: 23, EndMinute: 59,
		})
		f.expectScheduledDenial(lockdef.OperationErrorRestricted)
	})

	// :859-894 at b25ed960: GetWeekDaySchedule's index fields are not held
	// to their constraint (constraint = false,
	// DoorLockServer.ts:1520-1522), so the handler answers index 0 in its
	// response; the response echoes the index, so its Status is read raw,
	// as matter.js decodes it without validating.
	t.Run("answers a GetWeekDaySchedule index outside the supported range in the response", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeWeekDayScheduleUser, nil))
		res := f.must(lockdef.CmdGetWeekDaySchedule, lockdef.GetWeekDayScheduleRequest{WeekDayIndex: 0, UserIndex: 1})
		if st := statusField(res, 2); st != uint64(lockdef.StatusInvalidCommand) {
			t.Errorf("payload status %d, want INVALID_COMMAND", st)
		}
	})

	// :896-919 at b25ed960: a request without command fields is validated
	// as {} (ProtocolService.ts:624-625), its mandatory fields missing, and
	// the command's onValidationError answers INVALID_COMMAND
	// (DoorLockServer.ts:1509).
	t.Run("answers a GetWeekDaySchedule without command fields with INVALID_COMMAND", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeWeekDayScheduleUser, nil))
		if se := f.refused(lockdef.CmdGetWeekDaySchedule, nil); se.Status != im.StatusInvalidCommand {
			t.Errorf("interaction status 0x%02X, want INVALID_COMMAND", se.Status)
		}
	})
}

func userTypeName(u lockdef.UserTypeEnum) string {
	for i := range lockdef.UserTypeEnumDef.Values {
		if v := &lockdef.UserTypeEnumDef.Values[i]; v.Value == uint64(u) {
			return v.Name
		}
	}
	return "unknown"
}
