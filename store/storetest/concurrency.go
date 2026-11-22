// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//nolint:thelper // each case is a test body that Run runs as a subtest
package storetest

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/store"
)

// fabricsAddBesideWriter (R2-P6-conc-0602): AddFabric with an allocated
// index succeeds while another caller writes the store — a setting, as a
// subscription save or a counter does during an AddNOC. Every call
// succeeds; a backend that reads before it writes fails some of them on
// the lock upgrade (SQLite's SQLITE_BUSY, which skips the busy timeout).
func fabricsAddBesideWriter(ctx context.Context, t *testing.T, p store.Port) {
	stop := make(chan struct{})
	var writes atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := p.SetSetting(ctx, "writer", strconv.Itoa(i)); err == nil {
				writes.Add(1)
			}
			// A pause, so the writer does not starve the AddFabric calls
			// past their busy timeout: the case is about the lock upgrade.
			if !clock.Sleep(clock.System{}, 200*time.Microsecond, stop) {
				return
			}
		}
	}()
	failed := 0
	var first error
	for i := range 50 {
		idx, err := p.AddFabric(ctx, fabric(byte(i%200+1)))
		if err != nil {
			failed++
			if first == nil {
				first = err
			}
			continue
		}
		if err := p.RemoveFabric(ctx, idx); err != nil {
			t.Errorf("RemoveFabric(%d): %v", idx, err)
		}
	}
	close(stop)
	wg.Wait()
	if failed > 0 {
		t.Errorf("AddFabric beside a writer failed %d of 50 times (first: %v); the writer wrote %d times", failed, first, writes.Load())
	}
}

// doorLocksConcurrent (R2-P6-store-0607): LoadDoorLockAuth beside a saver
// that alternates two record sets of one endpoint reads one of them whole
// — never the users of one save with the credentials of the other (the
// port's "a concurrent reader sees the state before it or after it").
func doorLocksConcurrent(ctx context.Context, t *testing.T, p store.Port) {
	set := func(n byte) store.DoorLockAuth {
		return store.DoorLockAuth{
			Users:            []store.DoorLockUser{{UserIndex: uint16(n), UserName: strconv.Itoa(int(n)), UserStatus: 1, Credentials: []store.DoorLockCredentialRef{{Type: 1, Index: uint16(n)}}}},
			Credentials:      []store.DoorLockCredential{{Type: 1, Index: uint16(n), Data: []byte{n}}},
			WeekDaySchedules: []store.DoorLockWeekDaySchedule{{WeekDayIndex: 1, UserIndex: uint16(n), DaysMask: n}},
			YearDaySchedules: []store.DoorLockYearDaySchedule{{YearDayIndex: 1, UserIndex: uint16(n), LocalStartTime: uint32(n), LocalEndTime: uint32(n) + 1}},
			HolidaySchedules: []store.DoorLockHolidaySchedule{{HolidayIndex: 1, LocalStartTime: uint32(n), LocalEndTime: uint32(n) + 1}},
		}
	}
	a, b := set(1), set(2)
	must(t, p.SaveDoorLockAuth(ctx, "lock", a))
	wantA, _, err := p.LoadDoorLockAuth(ctx, "lock")
	must(t, err)
	must(t, p.SaveDoorLockAuth(ctx, "lock", b))
	wantB, _, err := p.LoadDoorLockAuth(ctx, "lock")
	must(t, err)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			next := a
			if i%2 == 1 {
				next = b
			}
			_ = p.SaveDoorLockAuth(ctx, "lock", next)
		}
	}()
	mixed := 0
	for range 300 {
		got, _, err := p.LoadDoorLockAuth(ctx, "lock")
		if err != nil {
			continue
		}
		if !reflect.DeepEqual(got, wantA) && !reflect.DeepEqual(got, wantB) {
			mixed++
		}
	}
	close(stop)
	wg.Wait()
	if mixed > 0 {
		t.Errorf("LoadDoorLockAuth beside a saver read a mix of two saves %d of 300 times", mixed)
	}
}
