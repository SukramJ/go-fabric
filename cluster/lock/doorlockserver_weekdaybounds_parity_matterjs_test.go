// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("week day window bounds") (:937-968) inside
// "schedule-restricted access" — LockSchedule.isAccessGranted on one
// Monday schedule, called directly as matter.js calls it
// ([lock.IsAccessGranted], LockSchedule.ts:129-167).

package lock_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/lock"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/store"
)

// weekDayAccess is DoorLockServerTest.ts:938-956.
func weekDayAccess(start, end, at [2]int) bool {
	return lock.IsAccessGranted(lockdef.UserTypeWeekDayScheduleUser, 1,
		[]store.DoorLockWeekDaySchedule{{
			WeekDayIndex: 1, UserIndex: 1, DaysMask: uint8(lockdef.DaysMaskMonday),
			StartHour: uint8(start[0]), StartMinute: uint8(start[1]), EndHour: uint8(end[0]), EndMinute: uint8(end[1]),
		}},
		nil,
		lock.LocalInstant{EpochS: 0, MinuteOfDay: at[0]*60 + at[1], DayOfWeek: time.Monday},
	)
}

func TestDoorLockServerWeekDayWindowBoundsParity(t *testing.T) {
	t.Parallel()

	t.Run("includes the start minute and excludes the end minute", func(t *testing.T) {
		t.Parallel()
		for _, c := range []struct {
			at   [2]int
			want bool
		}{{[2]int{7, 59}, false}, {[2]int{8, 0}, true}, {[2]int{17, 29}, true}, {[2]int{17, 30}, false}} {
			if got := weekDayAccess([2]int{8, 0}, [2]int{17, 30}, c.at); got != c.want {
				t.Errorf("08:00-17:30 at %02d:%02d = %v, want %v", c.at[0], c.at[1], got, c.want)
			}
		}
	})

	t.Run("includes the whole of 23:59 when the window ends there", func(t *testing.T) {
		t.Parallel()
		if !weekDayAccess([2]int{8, 0}, [2]int{23, 59}, [2]int{23, 59}) {
			t.Error("08:00-23:59 at 23:59 refused")
		}
	})
}
