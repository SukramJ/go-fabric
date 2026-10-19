// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock

import (
	"context"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// LocalInstant is the current instant expressed the way the schedules
// read it: matter.js LockSchedule.LocalInstant (LockSchedule.ts:86-95).
// YearDay's LocalStartTime/LocalEndTime are epoch seconds "with local time
// offset" — the local wall-clock reading, encoded as if it were UTC — and
// WeekDay's Start/EndHour and Minute are the plain local wall-clock time.
type LocalInstant struct {
	EpochS      int64
	MinuteOfDay int
	DayOfWeek   time.Weekday
}

// LocalInstantOf mirrors matter.js LockSchedule.localInstant
// (LockSchedule.ts:97-112) for a time already in the lock's zone.
func LocalInstantOf(t time.Time) LocalInstant {
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	return LocalInstant{
		EpochS:      time.Date(year, month, day, hour, minute, second, 0, time.UTC).Unix(),
		MinuteOfDay: hour*60 + minute,
		DayOfWeek:   t.Weekday(),
	}
}

// lastMinuteOfDay is LAST_MINUTE_OF_DAY (LockSchedule.ts:84).
const lastMinuteOfDay = 23*60 + 59

// matchesWeekDay mirrors matter.js LockSchedule.ts:114-123: the start
// minute is in, the end minute out, but an end of 23:59 runs to the end of
// that minute.
func matchesWeekDay(w store.DoorLockWeekDaySchedule, now LocalInstant) bool {
	if w.DaysMask&(1<<uint(now.DayOfWeek)) == 0 {
		return false
	}
	start := int(w.StartHour)*60 + int(w.StartMinute)
	end := int(w.EndHour)*60 + int(w.EndMinute)
	return now.MinuteOfDay >= start && (now.MinuteOfDay < end || end == lastMinuteOfDay)
}

// matchesYearDay mirrors matter.js LockSchedule.ts:125-127.
func matchesYearDay(y store.DoorLockYearDaySchedule, now LocalInstant) bool {
	return now.EpochS >= int64(y.LocalStartTime) && now.EpochS <= int64(y.LocalEndTime)
}

// IsAccessGranted reports whether the schedules of a user grant access
// now; a user type that is not schedule-restricted is always granted.
// Mirrors matter.js LockSchedule.isAccessGranted (LockSchedule.ts:129-167).
func IsAccessGranted(userType lockdef.UserTypeEnum, userIndex uint16, weekDay []store.DoorLockWeekDaySchedule,
	yearDay []store.DoorLockYearDaySchedule, now LocalInstant,
) bool {
	var weekly []store.DoorLockWeekDaySchedule
	for _, w := range weekDay {
		if w.UserIndex == userIndex {
			weekly = append(weekly, w)
		}
	}
	var yearly []store.DoorLockYearDaySchedule
	for _, y := range yearDay {
		if y.UserIndex == userIndex {
			yearly = append(yearly, y)
		}
	}
	anyWeekly := slices.ContainsFunc(weekly, func(w store.DoorLockWeekDaySchedule) bool { return matchesWeekDay(w, now) })
	anyYearly := slices.ContainsFunc(yearly, func(y store.DoorLockYearDaySchedule) bool { return matchesYearDay(y, now) })
	switch userType {
	case lockdef.UserTypeWeekDayScheduleUser:
		return anyWeekly
	case lockdef.UserTypeYearDayScheduleUser:
		return anyYearly
	case lockdef.UserTypeScheduleRestrictedUser:
		switch {
		case len(weekly) == 0 && len(yearly) == 0:
			return false
		case len(yearly) == 0:
			return anyWeekly
		case len(weekly) == 0:
			return anyYearly
		}
		return anyWeekly && anyYearly
	default:
		return true
	}
}

// requireValidUserLocked mirrors matter.js DoorLockServer.ts:1365-1373.
func (s *DoorLockServer) requireValidUserLocked(userIndex uint16) error {
	maxUsers, _ := s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported)
	if userIndex < 1 || userIndex > maxUsers {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid user index %d", userIndex)
	}
	if s.findUser(userIndex) == nil {
		return spec.Errorf(im.StatusFailure, "doorlock: user %d not found", userIndex)
	}
	return nil
}

// scheduleChangeLocked emits the LockUserChange of a schedule command; the
// accessing fabric is read here, where matter.js reads #fabricIndex.
func (s *DoorLockServer) scheduleChangeLocked(ctx context.Context, dataType lockdef.LockDataTypeEnum, op lockdef.DataOperationTypeEnum, userIndex *uint16, index uint8) error {
	if _, err := requestFabric(ctx); err != nil {
		return err
	}
	dataIndex := uint16(index)
	s.emitLockUserChangeLocked(ctx, dataType, op, userIndex, &dataIndex)
	return nil
}

// mutateSchedule runs a schedule command under op as one transaction.
func (s *DoorLockServer) mutateSchedule(ctx context.Context, fn func() error) (any, error) {
	s.op.Lock()
	defer s.op.Unlock()
	return nil, s.commit(ctx, fn)
}

// getLocked runs a Get…Schedule command under op and mu.
func (s *DoorLockServer) getLocked(fn func() any) any {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}

// ── Week day schedules (WDSCH) ──────────────────────────────────────────

func (s *DoorLockServer) handleSetWeekDaySchedule(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.SetWeekDayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	return s.mutateSchedule(ctx, func() error { return s.setWeekDayScheduleLocked(ctx, req) })
}

// setWeekDayScheduleLocked mirrors matter.js DoorLockServer.ts:653-692.
func (s *DoorLockServer) setWeekDayScheduleLocked(ctx context.Context, req lockdef.SetWeekDayScheduleRequest) error {
	maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfWeekDaySchedulesSupportedPerUser)
	if req.WeekDayIndex < 1 || req.WeekDayIndex > maxSchedules {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid schedule index %d", req.WeekDayIndex)
	}
	if err := s.requireValidUserLocked(req.UserIndex); err != nil {
		return err
	}
	if req.EndHour < req.StartHour {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: end hour must be >= start hour")
	}
	if req.EndHour == req.StartHour && req.EndMinute <= req.StartMinute {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: end time must be after start time")
	}
	s.data.WeekDaySchedules = append(slices.DeleteFunc(s.data.WeekDaySchedules, func(w store.DoorLockWeekDaySchedule) bool {
		return w.WeekDayIndex == req.WeekDayIndex && w.UserIndex == req.UserIndex
	}), store.DoorLockWeekDaySchedule{
		WeekDayIndex: req.WeekDayIndex, UserIndex: req.UserIndex, DaysMask: uint8(req.DaysMask),
		StartHour: req.StartHour, StartMinute: req.StartMinute, EndHour: req.EndHour, EndMinute: req.EndMinute,
	})
	s.dirty = true
	userIndex := req.UserIndex
	return s.scheduleChangeLocked(ctx, lockdef.LockDataTypeWeekDaySchedule, lockdef.DataOperationTypeAdd, &userIndex, req.WeekDayIndex)
}

func (s *DoorLockServer) handleGetWeekDaySchedule(_ context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.GetWeekDayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	return s.getLocked(func() any { return s.getWeekDayScheduleLocked(req) }), nil
}

// getWeekDayScheduleLocked mirrors matter.js DoorLockServer.ts:694-732:
// an index out of range is answered in the response, not as an error.
func (s *DoorLockServer) getWeekDayScheduleLocked(req lockdef.GetWeekDayScheduleRequest) lockdef.GetWeekDayScheduleResponse {
	resp := lockdef.GetWeekDayScheduleResponse{WeekDayIndex: req.WeekDayIndex, UserIndex: req.UserIndex}
	maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfWeekDaySchedulesSupportedPerUser)
	maxUsers, _ := s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported)
	if req.WeekDayIndex < 1 || req.WeekDayIndex > maxSchedules || req.UserIndex < 1 || req.UserIndex > maxUsers {
		resp.Status = lockdef.StatusInvalidCommand
		return resp
	}
	if s.findUser(req.UserIndex) == nil {
		resp.Status = lockdef.StatusNotFound
		return resp
	}
	i := slices.IndexFunc(s.data.WeekDaySchedules, func(w store.DoorLockWeekDaySchedule) bool {
		return w.WeekDayIndex == req.WeekDayIndex && w.UserIndex == req.UserIndex
	})
	if i < 0 {
		resp.Status = lockdef.StatusNotFound
		return resp
	}
	w := s.data.WeekDaySchedules[i]
	days := lockdef.DaysMaskBitmap(w.DaysMask)
	resp.Status = lockdef.StatusSuccess
	resp.DaysMask, resp.StartHour, resp.StartMinute, resp.EndHour, resp.EndMinute = &days, &w.StartHour, &w.StartMinute, &w.EndHour, &w.EndMinute
	return resp
}

func (s *DoorLockServer) handleClearWeekDaySchedule(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.ClearWeekDayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	return s.mutateSchedule(ctx, func() error { return s.clearWeekDayScheduleLocked(ctx, req) })
}

// clearWeekDayScheduleLocked mirrors matter.js DoorLockServer.ts:734-757.
func (s *DoorLockServer) clearWeekDayScheduleLocked(ctx context.Context, req lockdef.ClearWeekDayScheduleRequest) error {
	if err := s.requireValidUserLocked(req.UserIndex); err != nil {
		return err
	}
	if req.WeekDayIndex != clearAllSchedules {
		maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfWeekDaySchedulesSupportedPerUser)
		if req.WeekDayIndex < 1 || req.WeekDayIndex > maxSchedules {
			return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid schedule index %d", req.WeekDayIndex)
		}
	}
	s.data.WeekDaySchedules = slices.DeleteFunc(s.data.WeekDaySchedules, func(w store.DoorLockWeekDaySchedule) bool {
		return w.UserIndex == req.UserIndex && (req.WeekDayIndex == clearAllSchedules || w.WeekDayIndex == req.WeekDayIndex)
	})
	s.dirty = true
	userIndex := req.UserIndex
	return s.scheduleChangeLocked(ctx, lockdef.LockDataTypeWeekDaySchedule, lockdef.DataOperationTypeClear, &userIndex, req.WeekDayIndex)
}

// ── Year day schedules (YDSCH) ──────────────────────────────────────────

func (s *DoorLockServer) handleSetYearDaySchedule(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.SetYearDayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	return s.mutateSchedule(ctx, func() error { return s.setYearDayScheduleLocked(ctx, req) })
}

// setYearDayScheduleLocked mirrors matter.js DoorLockServer.ts:759-792.
func (s *DoorLockServer) setYearDayScheduleLocked(ctx context.Context, req lockdef.SetYearDayScheduleRequest) error {
	maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfYearDaySchedulesSupportedPerUser)
	if req.YearDayIndex < 1 || req.YearDayIndex > maxSchedules {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid schedule index %d", req.YearDayIndex)
	}
	if err := s.requireValidUserLocked(req.UserIndex); err != nil {
		return err
	}
	if req.LocalEndTime <= req.LocalStartTime {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: end time must be after start time")
	}
	s.data.YearDaySchedules = append(slices.DeleteFunc(s.data.YearDaySchedules, func(y store.DoorLockYearDaySchedule) bool {
		return y.YearDayIndex == req.YearDayIndex && y.UserIndex == req.UserIndex
	}), store.DoorLockYearDaySchedule{
		YearDayIndex: req.YearDayIndex, UserIndex: req.UserIndex, LocalStartTime: req.LocalStartTime, LocalEndTime: req.LocalEndTime,
	})
	s.dirty = true
	userIndex := req.UserIndex
	return s.scheduleChangeLocked(ctx, lockdef.LockDataTypeYearDaySchedule, lockdef.DataOperationTypeAdd, &userIndex, req.YearDayIndex)
}

func (s *DoorLockServer) handleGetYearDaySchedule(_ context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.GetYearDayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	return s.getLocked(func() any { return s.getYearDayScheduleLocked(req) }), nil
}

// getYearDayScheduleLocked mirrors matter.js DoorLockServer.ts:794-829.
func (s *DoorLockServer) getYearDayScheduleLocked(req lockdef.GetYearDayScheduleRequest) lockdef.GetYearDayScheduleResponse {
	resp := lockdef.GetYearDayScheduleResponse{YearDayIndex: req.YearDayIndex, UserIndex: req.UserIndex}
	maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfYearDaySchedulesSupportedPerUser)
	maxUsers, _ := s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported)
	if req.YearDayIndex < 1 || req.YearDayIndex > maxSchedules || req.UserIndex < 1 || req.UserIndex > maxUsers {
		resp.Status = lockdef.StatusInvalidCommand
		return resp
	}
	if s.findUser(req.UserIndex) == nil {
		resp.Status = lockdef.StatusNotFound
		return resp
	}
	i := slices.IndexFunc(s.data.YearDaySchedules, func(y store.DoorLockYearDaySchedule) bool {
		return y.YearDayIndex == req.YearDayIndex && y.UserIndex == req.UserIndex
	})
	if i < 0 {
		resp.Status = lockdef.StatusNotFound
		return resp
	}
	y := s.data.YearDaySchedules[i]
	resp.Status = lockdef.StatusSuccess
	resp.LocalStartTime, resp.LocalEndTime = &y.LocalStartTime, &y.LocalEndTime
	return resp
}

func (s *DoorLockServer) handleClearYearDaySchedule(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.ClearYearDayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	return s.mutateSchedule(ctx, func() error { return s.clearYearDayScheduleLocked(ctx, req) })
}

// clearYearDayScheduleLocked mirrors matter.js DoorLockServer.ts:831-854.
func (s *DoorLockServer) clearYearDayScheduleLocked(ctx context.Context, req lockdef.ClearYearDayScheduleRequest) error {
	if err := s.requireValidUserLocked(req.UserIndex); err != nil {
		return err
	}
	if req.YearDayIndex != clearAllSchedules {
		maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfYearDaySchedulesSupportedPerUser)
		if req.YearDayIndex < 1 || req.YearDayIndex > maxSchedules {
			return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid schedule index %d", req.YearDayIndex)
		}
	}
	s.data.YearDaySchedules = slices.DeleteFunc(s.data.YearDaySchedules, func(y store.DoorLockYearDaySchedule) bool {
		return y.UserIndex == req.UserIndex && (req.YearDayIndex == clearAllSchedules || y.YearDayIndex == req.YearDayIndex)
	})
	s.dirty = true
	userIndex := req.UserIndex
	return s.scheduleChangeLocked(ctx, lockdef.LockDataTypeYearDaySchedule, lockdef.DataOperationTypeClear, &userIndex, req.YearDayIndex)
}

// ── Holiday schedules (HDSCH) ───────────────────────────────────────────

func (s *DoorLockServer) handleSetHolidaySchedule(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.SetHolidayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	// The OperatingMode constraint, remapped to INVALID_COMMAND
	// (DoorLockServer.ts:1494-1511).
	if err := s.checkEnum("OperatingMode", lockdef.OperatingModeEnumDef, uint64(req.OperatingMode)); err != nil {
		return nil, err
	}
	return s.mutateSchedule(ctx, func() error { return s.setHolidayScheduleLocked(ctx, req) })
}

// setHolidayScheduleLocked mirrors matter.js DoorLockServer.ts:856-885.
func (s *DoorLockServer) setHolidayScheduleLocked(ctx context.Context, req lockdef.SetHolidayScheduleRequest) error {
	maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfHolidaySchedulesSupported)
	if req.HolidayIndex < 1 || req.HolidayIndex > maxSchedules {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid schedule index %d", req.HolidayIndex)
	}
	if req.LocalEndTime <= req.LocalStartTime {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: end time must be after start time")
	}
	s.data.HolidaySchedules = append(slices.DeleteFunc(s.data.HolidaySchedules, func(h store.DoorLockHolidaySchedule) bool {
		return h.HolidayIndex == req.HolidayIndex
	}), store.DoorLockHolidaySchedule{
		HolidayIndex: req.HolidayIndex, LocalStartTime: req.LocalStartTime, LocalEndTime: req.LocalEndTime, OperatingMode: uint8(req.OperatingMode),
	})
	s.dirty = true
	return s.scheduleChangeLocked(ctx, lockdef.LockDataTypeHolidaySchedule, lockdef.DataOperationTypeAdd, nil, req.HolidayIndex)
}

func (s *DoorLockServer) handleGetHolidaySchedule(_ context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.GetHolidayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	return s.getLocked(func() any { return s.getHolidayScheduleLocked(req) }), nil
}

// getHolidayScheduleLocked mirrors matter.js DoorLockServer.ts:887-910.
func (s *DoorLockServer) getHolidayScheduleLocked(req lockdef.GetHolidayScheduleRequest) lockdef.GetHolidayScheduleResponse {
	resp := lockdef.GetHolidayScheduleResponse{HolidayIndex: req.HolidayIndex}
	maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfHolidaySchedulesSupported)
	if req.HolidayIndex < 1 || req.HolidayIndex > maxSchedules {
		resp.Status = lockdef.StatusInvalidCommand
		return resp
	}
	i := slices.IndexFunc(s.data.HolidaySchedules, func(h store.DoorLockHolidaySchedule) bool { return h.HolidayIndex == req.HolidayIndex })
	if i < 0 {
		resp.Status = lockdef.StatusNotFound
		return resp
	}
	h := s.data.HolidaySchedules[i]
	start, end := spec.ValueOf(h.LocalStartTime), spec.ValueOf(h.LocalEndTime)
	mode := spec.ValueOf(lockdef.OperatingModeEnum(h.OperatingMode))
	resp.Status = lockdef.StatusSuccess
	resp.LocalStartTime, resp.LocalEndTime, resp.OperatingMode = &start, &end, &mode
	return resp
}

func (s *DoorLockServer) handleClearHolidaySchedule(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.ClearHolidayScheduleRequest](fields)
	if err != nil {
		return nil, err
	}
	return s.mutateSchedule(ctx, func() error { return s.clearHolidayScheduleLocked(ctx, req) })
}

// clearHolidayScheduleLocked mirrors matter.js DoorLockServer.ts:912-931.
func (s *DoorLockServer) clearHolidayScheduleLocked(ctx context.Context, req lockdef.ClearHolidayScheduleRequest) error {
	if req.HolidayIndex != clearAllSchedules {
		maxSchedules, _ := s.uint8Attr(lockdef.AttrNumberOfHolidaySchedulesSupported)
		if req.HolidayIndex < 1 || req.HolidayIndex > maxSchedules {
			return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid schedule index %d", req.HolidayIndex)
		}
	}
	s.data.HolidaySchedules = slices.DeleteFunc(s.data.HolidaySchedules, func(h store.DoorLockHolidaySchedule) bool {
		return req.HolidayIndex == clearAllSchedules || h.HolidayIndex == req.HolidayIndex
	})
	s.dirty = true
	return s.scheduleChangeLocked(ctx, lockdef.LockDataTypeHolidaySchedule, lockdef.DataOperationTypeClear, nil, req.HolidayIndex)
}
