// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// SettingDoorLockCredentialKey holds the node's DoorLock credential key:
// the random 16-byte key, hex-encoded, the DoorLock server encrypts every
// stored credential with. The server generates it once, on first use, and
// reads it back on every start — matter.js keeps the same key as
// nonvolatile state (DoorLockServer.ts:153-164 initialize, :1569-1573
// State.credentialKey). One key serves every lock endpoint of the node.
const SettingDoorLockCredentialKey = "door_lock.credential_key"

// settingDoorLockSaved prefixes the matter_settings key that records that
// a lock endpoint's records were saved at least once, so a lock whose
// users were all cleared is told apart from one that never saved.
const settingDoorLockSaved = "door_lock.saved:"

// DoorLockCredentialRef names one credential of a user: matter.js
// LockAuth.CredentialRef (LockAuth.ts:33-39), the DoorLock CredentialStruct.
type DoorLockCredentialRef struct {
	Type  uint8  `json:"type"`
	Index uint16 `json:"index"`
}

// DoorLockUser is one entry of a lock's user database: matter.js
// LockAuth.User (LockAuth.ts:44-82).
type DoorLockUser struct {
	UserIndex      uint16
	UserName       string
	UserUniqueID   *uint32 // nil is null
	UserStatus     uint8
	UserType       uint8
	CredentialRule uint8
	Credentials    []DoorLockCredentialRef
	// CreatorFabricIndex and LastModifiedFabricIndex are the fabrics that
	// created and last changed the user.
	CreatorFabricIndex      uint8
	LastModifiedFabricIndex uint8
	// ExpiringUserExpiresAt is the wall-clock deadline of an ExpiringUser
	// in Unix milliseconds, set on its first use; nil until then.
	ExpiringUserExpiresAt *int64
}

// DoorLockCredential is one stored credential: matter.js
// LockAuth.Credential (LockAuth.ts:87-102). Data is the credential data as
// the DoorLock server encrypted it; this package never sees it in clear.
type DoorLockCredential struct {
	Type                    uint8
	Index                   uint16
	Data                    []byte
	CreatorFabricIndex      uint8
	LastModifiedFabricIndex uint8
}

// DoorLockWeekDaySchedule is matter.js LockSchedule.WeekDay
// (LockSchedule.ts:17-38).
type DoorLockWeekDaySchedule struct {
	WeekDayIndex uint8
	UserIndex    uint16
	DaysMask     uint8
	StartHour    uint8
	StartMinute  uint8
	EndHour      uint8
	EndMinute    uint8
}

// DoorLockYearDaySchedule is matter.js LockSchedule.YearDay
// (LockSchedule.ts:43-55): the window in local epoch seconds.
type DoorLockYearDaySchedule struct {
	YearDayIndex   uint8
	UserIndex      uint16
	LocalStartTime uint32
	LocalEndTime   uint32
}

// DoorLockHolidaySchedule is matter.js LockSchedule.Holiday
// (LockSchedule.ts:60-72).
type DoorLockHolidaySchedule struct {
	HolidayIndex   uint8
	LocalStartTime uint32
	LocalEndTime   uint32
	OperatingMode  uint8
}

// DoorLockAuth is what a lock endpoint keeps across restarts: the five
// lists of matter.js's DoorLockServer State (DoorLockServer.ts:1538-1574)
// but the credential key, which is [SettingDoorLockCredentialKey].
type DoorLockAuth struct {
	Users            []DoorLockUser
	Credentials      []DoorLockCredential
	WeekDaySchedules []DoorLockWeekDaySchedule
	YearDaySchedules []DoorLockYearDaySchedule
	HolidaySchedules []DoorLockHolidaySchedule
}

// Schedule kinds of matter_doorlock_schedules.
const (
	doorLockWeekDay = "week_day"
	doorLockYearDay = "year_day"
	doorLockHoliday = "holiday"
)

// LoadDoorLockAuth returns the records of the lock endpoint endpointKey.
// saved is false when [Store.SaveDoorLockAuth] never ran for it; the lock
// then starts from its configured initial records.
func (s *Store) LoadDoorLockAuth(ctx context.Context, endpointKey string) (auth DoorLockAuth, saved bool, err error) {
	if _, saved, err = s.GetSetting(ctx, settingDoorLockSaved+endpointKey); err != nil || !saved {
		return DoorLockAuth{}, false, err
	}
	if auth.Users, err = s.loadDoorLockUsers(ctx, endpointKey); err != nil {
		return DoorLockAuth{}, false, err
	}
	if auth.Credentials, err = s.loadDoorLockCredentials(ctx, endpointKey); err != nil {
		return DoorLockAuth{}, false, err
	}
	if err = s.loadDoorLockSchedules(ctx, endpointKey, &auth); err != nil {
		return DoorLockAuth{}, false, err
	}
	return auth, true, nil
}

func (s *Store) loadDoorLockUsers(ctx context.Context, endpointKey string) ([]DoorLockUser, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT user_index, user_name, user_unique_id, user_status, user_type, credential_rule,
       credentials_json, creator_fabric_index, last_modified_fabric_index, expiring_user_expires_at
FROM matter_doorlock_users WHERE endpoint_key = ? ORDER BY user_index`, endpointKey)
	if err != nil {
		return nil, fmt.Errorf("matter store: load door lock users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []DoorLockUser
	for rows.Next() {
		var (
			u       DoorLockUser
			unique  sql.NullInt64
			expires sql.NullInt64
			creds   string
		)
		if err := rows.Scan(&u.UserIndex, &u.UserName, &unique, &u.UserStatus, &u.UserType, &u.CredentialRule,
			&creds, &u.CreatorFabricIndex, &u.LastModifiedFabricIndex, &expires); err != nil {
			return nil, fmt.Errorf("matter store: load door lock users: %w", err)
		}
		if unique.Valid {
			v := uint32(unique.Int64) //nolint:gosec // written from a uint32
			u.UserUniqueID = &v
		}
		if expires.Valid {
			v := expires.Int64
			u.ExpiringUserExpiresAt = &v
		}
		if err := json.Unmarshal([]byte(creds), &u.Credentials); err != nil {
			return nil, fmt.Errorf("matter store: door lock user %d credentials: %w", u.UserIndex, err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matter store: load door lock users: %w", err)
	}
	return out, nil
}

func (s *Store) loadDoorLockCredentials(ctx context.Context, endpointKey string) ([]DoorLockCredential, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT credential_type, credential_index, credential_data, creator_fabric_index, last_modified_fabric_index
FROM matter_doorlock_credentials WHERE endpoint_key = ? ORDER BY credential_type, credential_index`, endpointKey)
	if err != nil {
		return nil, fmt.Errorf("matter store: load door lock credentials: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []DoorLockCredential
	for rows.Next() {
		var c DoorLockCredential
		if err := rows.Scan(&c.Type, &c.Index, &c.Data, &c.CreatorFabricIndex, &c.LastModifiedFabricIndex); err != nil {
			return nil, fmt.Errorf("matter store: load door lock credentials: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matter store: load door lock credentials: %w", err)
	}
	return out, nil
}

func (s *Store) loadDoorLockSchedules(ctx context.Context, endpointKey string, auth *DoorLockAuth) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT kind, schedule_index, user_index, days_mask, start_hour, start_minute, end_hour, end_minute,
       local_start_time, local_end_time, operating_mode
FROM matter_doorlock_schedules WHERE endpoint_key = ? ORDER BY kind, user_index, schedule_index`, endpointKey)
	if err != nil {
		return fmt.Errorf("matter store: load door lock schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			kind                                  string
			index, daysMask, sh, sm, eh, em, mode uint8
			user                                  uint16
			start, end                            uint32
		)
		if err := rows.Scan(&kind, &index, &user, &daysMask, &sh, &sm, &eh, &em, &start, &end, &mode); err != nil {
			return fmt.Errorf("matter store: load door lock schedules: %w", err)
		}
		switch kind {
		case doorLockWeekDay:
			auth.WeekDaySchedules = append(auth.WeekDaySchedules, DoorLockWeekDaySchedule{
				WeekDayIndex: index, UserIndex: user, DaysMask: daysMask,
				StartHour: sh, StartMinute: sm, EndHour: eh, EndMinute: em,
			})
		case doorLockYearDay:
			auth.YearDaySchedules = append(auth.YearDaySchedules, DoorLockYearDaySchedule{
				YearDayIndex: index, UserIndex: user, LocalStartTime: start, LocalEndTime: end,
			})
		case doorLockHoliday:
			auth.HolidaySchedules = append(auth.HolidaySchedules, DoorLockHolidaySchedule{
				HolidayIndex: index, LocalStartTime: start, LocalEndTime: end, OperatingMode: mode,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("matter store: load door lock schedules: %w", err)
	}
	return nil
}

// SaveDoorLockAuth replaces the records of the lock endpoint endpointKey
// with auth, in one transaction.
func (s *Store) SaveDoorLockAuth(ctx context.Context, endpointKey string, auth DoorLockAuth) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("matter store: save door lock: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"matter_doorlock_users", "matter_doorlock_credentials", "matter_doorlock_schedules"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE endpoint_key = ?`, endpointKey); err != nil {
			return fmt.Errorf("matter store: save door lock: clear %s: %w", table, err)
		}
	}
	for _, u := range auth.Users {
		creds, err := json.Marshal(nonNilRefs(u.Credentials))
		if err != nil {
			return fmt.Errorf("matter store: save door lock user %d: %w", u.UserIndex, err)
		}
		var unique, expires any
		if u.UserUniqueID != nil {
			unique = int64(*u.UserUniqueID)
		}
		if u.ExpiringUserExpiresAt != nil {
			expires = *u.ExpiringUserExpiresAt
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO matter_doorlock_users
    (endpoint_key, user_index, user_name, user_unique_id, user_status, user_type, credential_rule,
     credentials_json, creator_fabric_index, last_modified_fabric_index, expiring_user_expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			endpointKey, u.UserIndex, u.UserName, unique, u.UserStatus, u.UserType, u.CredentialRule,
			string(creds), u.CreatorFabricIndex, u.LastModifiedFabricIndex, expires); err != nil {
			return fmt.Errorf("matter store: save door lock user %d: %w", u.UserIndex, err)
		}
	}
	for _, c := range auth.Credentials {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO matter_doorlock_credentials
    (endpoint_key, credential_type, credential_index, credential_data, creator_fabric_index, last_modified_fabric_index)
VALUES (?, ?, ?, ?, ?, ?)`,
			endpointKey, c.Type, c.Index, c.Data, c.CreatorFabricIndex, c.LastModifiedFabricIndex); err != nil {
			return fmt.Errorf("matter store: save door lock credential %d/%d: %w", c.Type, c.Index, err)
		}
	}
	const insertSchedule = `
INSERT INTO matter_doorlock_schedules
    (endpoint_key, kind, schedule_index, user_index, days_mask, start_hour, start_minute, end_hour, end_minute,
     local_start_time, local_end_time, operating_mode)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, w := range auth.WeekDaySchedules {
		if _, err := tx.ExecContext(ctx, insertSchedule, endpointKey, doorLockWeekDay, w.WeekDayIndex, w.UserIndex,
			w.DaysMask, w.StartHour, w.StartMinute, w.EndHour, w.EndMinute, 0, 0, 0); err != nil {
			return fmt.Errorf("matter store: save door lock week day schedule %d: %w", w.WeekDayIndex, err)
		}
	}
	for _, y := range auth.YearDaySchedules {
		if _, err := tx.ExecContext(ctx, insertSchedule, endpointKey, doorLockYearDay, y.YearDayIndex, y.UserIndex,
			0, 0, 0, 0, 0, y.LocalStartTime, y.LocalEndTime, 0); err != nil {
			return fmt.Errorf("matter store: save door lock year day schedule %d: %w", y.YearDayIndex, err)
		}
	}
	for _, h := range auth.HolidaySchedules {
		if _, err := tx.ExecContext(ctx, insertSchedule, endpointKey, doorLockHoliday, h.HolidayIndex, 0,
			0, 0, 0, 0, 0, h.LocalStartTime, h.LocalEndTime, h.OperatingMode); err != nil {
			return fmt.Errorf("matter store: save door lock holiday schedule %d: %w", h.HolidayIndex, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO matter_settings (key, value) VALUES (?, '1')
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingDoorLockSaved+endpointKey); err != nil {
		return fmt.Errorf("matter store: save door lock: mark saved: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("matter store: save door lock: commit: %w", err)
	}
	return nil
}

// nonNilRefs keeps a user without credentials a JSON [] rather than null.
func nonNilRefs(refs []DoorLockCredentialRef) []DoorLockCredentialRef {
	if refs == nil {
		return []DoorLockCredentialRef{}
	}
	return refs
}
