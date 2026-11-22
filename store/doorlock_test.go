// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"reflect"
	"testing"

	store "thingfabric.dev/go-fabric/store"
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

// TestMigrationV10DoorLockRecordsKeepTheirSource (R2-P5-port-08): a user
// and a credential a v9 store saved read back after Apply as made through
// Matter — the answer a v9 node gave for every record — while a record
// saved with the flags false keeps them false.
func TestMigrationV10DoorLockRecordsKeepTheirSource(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openEmptyDB(t)
	applyUp(t, db, "v9")
	for _, stmt := range []string{
		`INSERT INTO matter_settings (key, value) VALUES ('door_lock.saved:lock', '1')`,
		`INSERT INTO matter_doorlock_users (endpoint_key, user_index, user_name, user_status, user_type, credential_rule,
    credentials_json, creator_fabric_index, last_modified_fabric_index) VALUES ('lock', 1, 'a', 1, 0, 0, '[]', 1, 2)`,
		`INSERT INTO matter_doorlock_credentials (endpoint_key, credential_type, credential_index, credential_data,
    creator_fabric_index, last_modified_fabric_index) VALUES ('lock', 1, 1, X'07', 1, 1)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	auth, saved, err := s.LoadDoorLockAuth(ctx, "lock")
	if err != nil || !saved || len(auth.Users) != 1 || len(auth.Credentials) != 1 {
		t.Fatalf("LoadDoorLockAuth after Apply = %+v, %v, %v", auth, saved, err)
	}
	if u := auth.Users[0]; !u.CreatedThroughMatter || !u.ModifiedThroughMatter {
		t.Errorf("the v9 user's source = %v/%v, want made through Matter", u.CreatedThroughMatter, u.ModifiedThroughMatter)
	}
	if c := auth.Credentials[0]; !c.CreatedThroughMatter || !c.ModifiedThroughMatter {
		t.Errorf("the v9 credential's source = %v/%v, want made through Matter", c.CreatedThroughMatter, c.ModifiedThroughMatter)
	}
	auth.Users[0].CreatedThroughMatter, auth.Credentials[0].ModifiedThroughMatter = false, false
	if err := s.SaveDoorLockAuth(ctx, "lock", auth); err != nil {
		t.Fatal(err)
	}
	if got, _, err := s.LoadDoorLockAuth(ctx, "lock"); err != nil || got.Users[0].CreatedThroughMatter || got.Credentials[0].ModifiedThroughMatter ||
		!got.Users[0].ModifiedThroughMatter || !got.Credentials[0].CreatedThroughMatter {
		t.Errorf("the flags after a save = %+v / %+v, %v", got.Users[0], got.Credentials[0], err)
	}
}
