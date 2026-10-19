// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"reflect"
	"testing"

	store "github.com/SukramJ/go-fabric/store"
)

func TestDoorLockAuthRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := store.New(openTestDB(t))

	if _, saved, err := s.LoadDoorLockAuth(ctx, "lock:1"); err != nil || saved {
		t.Fatalf("LoadDoorLockAuth before any save = saved %v, %v; want not saved", saved, err)
	}

	unique := uint32(77)
	expires := int64(1_800_000_000_000)
	want := store.DoorLockAuth{
		Users: []store.DoorLockUser{
			{
				UserIndex: 1, UserName: "front", UserStatus: 1, UserType: 0, CredentialRule: 0,
				Credentials:        []store.DoorLockCredentialRef{{Type: 1, Index: 1}, {Type: 2, Index: 3}},
				CreatorFabricIndex: 1, LastModifiedFabricIndex: 2,
			},
			{
				UserIndex: 4, UserUniqueID: &unique, UserStatus: 3, UserType: 5, CredentialRule: 1,
				Credentials:        []store.DoorLockCredentialRef{},
				CreatorFabricIndex: 2, LastModifiedFabricIndex: 2, ExpiringUserExpiresAt: &expires,
			},
		},
		Credentials: []store.DoorLockCredential{
			{Type: 0, Index: 0, Data: []byte{9, 9}, CreatorFabricIndex: 1, LastModifiedFabricIndex: 1},
			{Type: 1, Index: 1, Data: []byte{1, 2, 3}, CreatorFabricIndex: 1, LastModifiedFabricIndex: 2},
		},
		WeekDaySchedules: []store.DoorLockWeekDaySchedule{{WeekDayIndex: 2, UserIndex: 1, DaysMask: 0x02, StartHour: 8, EndHour: 17, EndMinute: 30}},
		YearDaySchedules: []store.DoorLockYearDaySchedule{{YearDayIndex: 1, UserIndex: 4, LocalStartTime: 100, LocalEndTime: 200}},
		HolidaySchedules: []store.DoorLockHolidaySchedule{{HolidayIndex: 3, LocalStartTime: 300, LocalEndTime: 400, OperatingMode: 1}},
	}
	if err := s.SaveDoorLockAuth(ctx, "lock:1", want); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDoorLockAuth(ctx, "lock:2", store.DoorLockAuth{Users: want.Users[:1]}); err != nil {
		t.Fatal(err)
	}
	got, saved, err := s.LoadDoorLockAuth(ctx, "lock:1")
	if err != nil || !saved {
		t.Fatalf("LoadDoorLockAuth = saved %v, %v", saved, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip\n got %+v\nwant %+v", got, want)
	}

	// A save replaces the endpoint's records; an empty save is still saved.
	if err := s.SaveDoorLockAuth(ctx, "lock:1", store.DoorLockAuth{}); err != nil {
		t.Fatal(err)
	}
	got, saved, err = s.LoadDoorLockAuth(ctx, "lock:1")
	if err != nil || !saved || !reflect.DeepEqual(got, store.DoorLockAuth{}) {
		t.Errorf("after an empty save = %+v, saved %v, %v", got, saved, err)
	}
	other, _, err := s.LoadDoorLockAuth(ctx, "lock:2")
	if err != nil || len(other.Users) != 1 {
		t.Errorf("the other endpoint = %+v, %v; want its one user", other, err)
	}
}
