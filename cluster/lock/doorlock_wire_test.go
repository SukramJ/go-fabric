// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"testing"

	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// TestRequestValidationOverTheWire holds the bridge's DoorLock request
// decoding to matter.js DoorLockServer.ts:1491-1521 at 862ad75f: a
// request the schema refuses answers INVALID_COMMAND for the commands
// matter.js remaps, and the Get…Schedule commands answer an index out of
// range in their response (TC-DRLK-2.7 step 5, TC-DRLK-2.8 step 6c).
func TestRequestValidationOverTheWire(t *testing.T) {
	f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))

	if se := f.refused(lockdef.CmdGetUser, lockdef.GetUserRequest{UserIndex: 0}); se.Status != im.StatusInvalidCommand {
		t.Errorf("GetUser 0: 0x%02X, want INVALID_COMMAND", se.Status)
	}
	if se := f.refused(lockdef.CmdSetYearDaySchedule, lockdef.SetYearDayScheduleRequest{YearDayIndex: 0, UserIndex: 15, LocalStartTime: 1020, LocalEndTime: 2040}); se.Status != im.StatusInvalidCommand {
		t.Errorf("SetYearDaySchedule 0: 0x%02X, want INVALID_COMMAND", se.Status)
	}
	if se := f.refused(lockdef.CmdSetWeekDaySchedule, lockdef.SetWeekDayScheduleRequest{WeekDayIndex: 1, UserIndex: 1, DaysMask: 1, StartHour: 24}); se.Status != im.StatusInvalidCommand {
		t.Errorf("SetWeekDaySchedule hour 24: 0x%02X, want INVALID_COMMAND", se.Status)
	}
	// The responses echo the index out of range, which their own schema
	// refuses, as matter.js's do (:704-707); the status field is read raw.
	if st := statusField(f.must(lockdef.CmdGetWeekDaySchedule, lockdef.GetWeekDayScheduleRequest{WeekDayIndex: 0, UserIndex: 1}), 2); st != uint64(lockdef.StatusInvalidCommand) {
		t.Errorf("GetWeekDaySchedule 0: status %d, want INVALID_COMMAND in the response", st)
	}
	if st := statusField(f.must(lockdef.CmdGetYearDaySchedule, lockdef.GetYearDayScheduleRequest{YearDayIndex: 1, UserIndex: 0}), 2); st != uint64(lockdef.StatusInvalidCommand) {
		t.Errorf("GetYearDaySchedule user 0: status %d, want INVALID_COMMAND in the response", st)
	}
	if st := statusField(f.must(lockdef.CmdGetHolidaySchedule, lockdef.GetHolidayScheduleRequest{HolidayIndex: 0}), 1); st != uint64(lockdef.StatusInvalidCommand) {
		t.Errorf("GetHolidaySchedule 0: status %d, want INVALID_COMMAND in the response", st)
	}
}

// statusField reads the context-tagged field tag of a response, raw.
func statusField(res interaction.Result, tag uint32) uint64 {
	for _, c := range res.Fields.Children {
		if c.El.Tag.Number == tag {
			return c.El.Uint
		}
	}
	return 0xFFFF
}
