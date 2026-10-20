// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// clearAll is the index that names every entry of ClearUser and
// ClearCredential (0xFFFE), clearAllSchedules that of the Clear…Schedule
// commands (0xFE).
const (
	clearAll          uint16 = 0xFFFE
	clearAllSchedules uint8  = 0xFE
)

// maxExpiryDelay is the longest single wait of an expiry timer; the
// deadline is wall-clock time, so a longer wait re-arms and picks up clock
// corrections (DoorLockServer.ts:48-49).
const maxExpiryDelay = 24 * time.Hour

// occupiedError is DoorLock.OccupiedError: FAILURE with the cluster
// status Occupied (matter.js ClusterType.ts:283-293 builds every cluster
// status error on Status.Failure).
type occupiedError struct{ msg string }

func (e occupiedError) Error() string                 { return e.msg }
func (occupiedError) MatterStatusCode() im.StatusCode { return im.StatusFailure }
func (occupiedError) MatterClusterStatus() uint8      { return uint8(lockdef.StatusCodeOccupied) }

var _ im.MatterClusterStatusError = occupiedError{}

// requestAs reads a command's decoded fields as T.
func requestAs[T any](fields any) (T, error) {
	switch v := fields.(type) {
	case *T:
		if v != nil {
			return *v, nil
		}
	case T:
		return v, nil
	}
	var zero T
	return zero, spec.Errorf(im.StatusInvalidCommand, "doorlock: command fields %T, want %T", fields, zero)
}

// requestFabric is matter.js #fabricIndex (DoorLockServer.ts:935-941):
// the accessing fabric, UNSUPPORTED_ACCESS without one.
func requestFabric(ctx context.Context) (uint8, error) {
	if _, fabricIndex := im.FabricFilterFromContext(ctx); fabricIndex != 0 {
		return fabricIndex, nil
	}
	return 0, spec.Errorf(im.StatusUnsupportedAccess, "doorlock: fabric required")
}

// invalidField is the INVALID_COMMAND matter.js answers a request field
// its constraint refuses for the commands DoorLockServer.ts:1494-1511
// remaps (Supervision onValidationError = throwInvalidCommand).
func invalidField(field string, v uint64) error {
	return spec.Errorf(im.StatusInvalidCommand, "doorlock: %s: the cluster does not define the enum value %d", field, v)
}

// checkEnum holds an enum field to the values its constraint lists, or —
// for an empty list — to the values the cluster defines for the feature
// selection.
func (s *DoorLockServer) checkEnum(field string, def *spec.Enum, v uint64, allowed ...uint64) error {
	if !s.EnumSupported(def, v) {
		return invalidField(field, v)
	}
	if len(allowed) > 0 && !slices.Contains(allowed, v) {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: %s %d is outside the command's constraint", field, v)
	}
	return nil
}

// The constraints of SetUser and SetCredential (door-lock-cluster.element.ts):
// OperationType "add, modify", UserStatus "occupiedEnabled,
// occupiedDisabled", and UserType without the schedule users and, for
// SetUser, the programming user.
var (
	dataOperationAddModify = []uint64{uint64(lockdef.DataOperationTypeAdd), uint64(lockdef.DataOperationTypeModify)}
	requestUserStatuses    = []uint64{uint64(lockdef.UserStatusOccupiedEnabled), uint64(lockdef.UserStatusOccupiedDisabled)}
	setUserUserTypes       = []uint64{
		uint64(lockdef.UserTypeUnrestrictedUser), uint64(lockdef.UserTypeNonAccessUser), uint64(lockdef.UserTypeForcedUser),
		uint64(lockdef.UserTypeDisposableUser), uint64(lockdef.UserTypeExpiringUser), uint64(lockdef.UserTypeScheduleRestrictedUser),
		uint64(lockdef.UserTypeRemoteOnlyUser),
	}
	setCredentialUserTypes = []uint64{
		uint64(lockdef.UserTypeUnrestrictedUser), uint64(lockdef.UserTypeProgrammingUser), uint64(lockdef.UserTypeNonAccessUser),
		uint64(lockdef.UserTypeForcedUser), uint64(lockdef.UserTypeDisposableUser), uint64(lockdef.UserTypeExpiringUser),
		uint64(lockdef.UserTypeRemoteOnlyUser),
	}
)

// validateSetUser is the request validation matter.js runs before setUser,
// remapped to INVALID_COMMAND (DoorLockServer.ts:1494-1511).
func (s *DoorLockServer) validateSetUser(req lockdef.SetUserRequest) error {
	if err := s.checkEnum("OperationType", lockdef.DataOperationTypeEnumDef, uint64(req.OperationType), dataOperationAddModify...); err != nil {
		return err
	}
	if !req.UserStatus.Null {
		if err := s.checkEnum("UserStatus", lockdef.UserStatusEnumDef, uint64(req.UserStatus.Value), requestUserStatuses...); err != nil {
			return err
		}
	}
	if !req.UserType.Null {
		if err := s.checkEnum("UserType", lockdef.UserTypeEnumDef, uint64(req.UserType.Value), setUserUserTypes...); err != nil {
			return err
		}
	}
	if !req.CredentialRule.Null {
		if err := s.checkEnum("CredentialRule", lockdef.CredentialRuleEnumDef, uint64(req.CredentialRule.Value)); err != nil {
			return err
		}
	}
	return nil
}

// ── User database (USR) ─────────────────────────────────────────────────

func (s *DoorLockServer) handleSetUser(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.SetUserRequest](fields)
	if err != nil {
		return nil, err
	}
	if err := s.validateSetUser(req); err != nil {
		return nil, err
	}
	s.op.Lock()
	defer s.op.Unlock()
	return nil, s.commit(ctx, func() error { return s.setUserLocked(ctx, req) })
}

// setUserLocked mirrors matter.js DoorLockServer.ts:261-339 (setUser).
func (s *DoorLockServer) setUserLocked(ctx context.Context, req lockdef.SetUserRequest) error {
	userIndex := req.UserIndex
	maxUsers, _ := s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported)
	fabricIndex, err := requestFabric(ctx)
	if err != nil {
		return err
	}
	if userIndex < 1 || userIndex > maxUsers {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid user index %d", userIndex)
	}
	existing := s.findUser(userIndex)
	switch req.OperationType {
	case lockdef.DataOperationTypeAdd:
		if existing != nil {
			return occupiedError{fmt.Sprintf("doorlock: user slot %d is occupied", userIndex)}
		}
		user := store.DoorLockUser{
			UserIndex:               userIndex,
			UserStatus:              uint8(lockdef.UserStatusOccupiedEnabled),
			UserType:                uint8(lockdef.UserTypeUnrestrictedUser),
			CredentialRule:          uint8(lockdef.CredentialRuleSingle),
			Credentials:             []store.DoorLockCredentialRef{},
			CreatorFabricIndex:      fabricIndex,
			LastModifiedFabricIndex: fabricIndex,
		}
		if !req.UserName.Null {
			user.UserName = req.UserName.Value
		}
		if !req.UserUniqueId.Null {
			id := req.UserUniqueId.Value
			user.UserUniqueID = &id
		}
		if !req.UserStatus.Null {
			user.UserStatus = uint8(req.UserStatus.Value)
		}
		if !req.UserType.Null {
			user.UserType = uint8(req.UserType.Value)
		}
		if !req.CredentialRule.Null {
			user.CredentialRule = uint8(req.CredentialRule.Value)
		}
		s.addUserLocked(user)
		s.emitLockUserChangeLocked(ctx, lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeAdd, &userIndex, &userIndex)
	case lockdef.DataOperationTypeModify:
		if existing == nil {
			return spec.Errorf(im.StatusInvalidCommand, "doorlock: user slot %d is available", userIndex)
		}
		if !req.UserName.Null && fabricIndex != existing.CreatorFabricIndex {
			return spec.Errorf(im.StatusInvalidCommand, "doorlock: cannot modify userName from a different fabric")
		}
		if !req.UserUniqueId.Null && fabricIndex != existing.CreatorFabricIndex {
			return spec.Errorf(im.StatusInvalidCommand, "doorlock: cannot modify userUniqueId from a different fabric")
		}
		status, userType := existing.UserStatus, existing.UserType
		if !req.UserStatus.Null {
			status = uint8(req.UserStatus.Value)
		}
		if !req.UserType.Null {
			userType = uint8(req.UserType.Value)
		}
		// A status or type change starts a new ExpiringUser cycle; the
		// timeout arms again on the next first use (:311-315).
		restartsExpiry := status != existing.UserStatus || userType != existing.UserType
		if restartsExpiry {
			s.stopExpiryTimerLocked(userIndex)
			existing.ExpiringUserExpiresAt = nil
		}
		if !req.UserName.Null {
			existing.UserName = req.UserName.Value
		}
		if !req.UserUniqueId.Null {
			id := req.UserUniqueId.Value
			existing.UserUniqueID = &id
		}
		existing.UserStatus, existing.UserType = status, userType
		if !req.CredentialRule.Null {
			existing.CredentialRule = uint8(req.CredentialRule.Value)
		}
		existing.LastModifiedFabricIndex = fabricIndex
		s.dirty = true
		s.emitLockUserChangeLocked(ctx, lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeModify, &userIndex, &userIndex)
	default:
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid operation type %d", req.OperationType)
	}
	return nil
}

func (s *DoorLockServer) handleGetUser(_ context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.GetUserRequest](fields)
	if err != nil {
		return nil, err
	}
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getUserLocked(req)
}

// getUserLocked mirrors matter.js DoorLockServer.ts:341-379 (getUser).
func (s *DoorLockServer) getUserLocked(req lockdef.GetUserRequest) (any, error) {
	maxUsers, _ := s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported)
	if req.UserIndex < 1 || req.UserIndex > maxUsers {
		return nil, spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid user index %d", req.UserIndex)
	}
	user := s.findUser(req.UserIndex)
	resp := lockdef.GetUserResponse{
		UserIndex:               req.UserIndex,
		UserName:                spec.NullOf[string](),
		UserUniqueId:            spec.NullOf[uint32](),
		UserStatus:              spec.NullOf[lockdef.UserStatusEnum](),
		UserType:                spec.NullOf[lockdef.UserTypeEnum](),
		CredentialRule:          spec.NullOf[lockdef.CredentialRuleEnum](),
		Credentials:             spec.NullOf[[]lockdef.CredentialStruct](),
		CreatorFabricIndex:      spec.NullOf[uint8](),
		LastModifiedFabricIndex: spec.NullOf[uint8](),
		NextUserIndex:           nullable(s.findNextOccupiedUserIndex(req.UserIndex)),
	}
	if user == nil {
		return resp, nil
	}
	resp.UserName = spec.ValueOf(user.UserName)
	resp.UserUniqueId = nullable(user.UserUniqueID)
	resp.UserStatus = spec.ValueOf(lockdef.UserStatusEnum(user.UserStatus))
	resp.UserType = spec.ValueOf(lockdef.UserTypeEnum(user.UserType))
	resp.CredentialRule = spec.ValueOf(lockdef.CredentialRuleEnum(user.CredentialRule))
	creds := make([]lockdef.CredentialStruct, 0, len(user.Credentials))
	for _, c := range user.Credentials {
		creds = append(creds, lockdef.CredentialStruct{CredentialType: lockdef.CredentialTypeEnum(c.Type), CredentialIndex: c.Index})
	}
	resp.Credentials = spec.ValueOf(creds)
	resp.CreatorFabricIndex = spec.ValueOf(user.CreatorFabricIndex)
	resp.LastModifiedFabricIndex = spec.ValueOf(user.LastModifiedFabricIndex)
	return resp, nil
}

func (s *DoorLockServer) handleClearUser(ctx context.Context, fields any) (any, error) {
	req, err := requestAs[lockdef.ClearUserRequest](fields)
	if err != nil {
		return nil, err
	}
	s.op.Lock()
	defer s.op.Unlock()
	return nil, s.commit(ctx, func() error { return s.clearUserLocked(ctx, req.UserIndex) })
}

// clearUserLocked mirrors matter.js DoorLockServer.ts:381-415 (clearUser).
func (s *DoorLockServer) clearUserLocked(ctx context.Context, userIndex uint16) error {
	maxUsers, _ := s.uint16Attr(lockdef.AttrNumberOfTotalUsersSupported)
	if _, err := requestFabric(ctx); err != nil {
		return err
	}
	if userIndex == clearAll {
		for _, u := range s.data.Users {
			s.clearCredentialsForUserLocked(u.UserIndex)
			s.clearSchedulesForUserLocked(u.UserIndex)
			s.stopExpiryTimerLocked(u.UserIndex)
		}
		s.data.Users = nil
		s.dirty = true
		idx := clearAll
		s.emitLockUserChangeLocked(ctx, lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeClear, &idx, &idx)
		return nil
	}
	if userIndex < 1 || userIndex > maxUsers {
		return spec.Errorf(im.StatusInvalidCommand, "doorlock: invalid user index %d", userIndex)
	}
	if s.findUser(userIndex) == nil {
		// CHIP clears a user that does not exist silently (:404-407).
		return nil
	}
	s.clearCredentialsForUserLocked(userIndex)
	s.clearSchedulesForUserLocked(userIndex)
	s.stopExpiryTimerLocked(userIndex)
	s.removeUserLocked(userIndex)
	s.emitLockUserChangeLocked(ctx, lockdef.LockDataTypeUserIndex, lockdef.DataOperationTypeClear, &userIndex, &userIndex)
	return nil
}

// ── ExpiringUser timeout (§ 5.2.6.18.8) ─────────────────────────────────
//
// The persisted deadline is the source of truth; the timer only triggers a
// re-check against it. The deadline is wall-clock time because it has to
// survive a reboot (DoorLockServer.ts:1203-1206).

// hasExpired mirrors matter.js DoorLockServer.ts:1595-1598.
func (s *DoorLockServer) hasExpired(user *store.DoorLockUser) bool {
	return lockdef.UserTypeEnum(user.UserType) == lockdef.UserTypeExpiringUser &&
		user.ExpiringUserExpiresAt != nil && *user.ExpiringUserExpiresAt <= s.clock.Now().UnixMilli()
}

// armExpiringUserOnFirstUseLocked mirrors matter.js DoorLockServer.ts:1208-1221.
func (s *DoorLockServer) armExpiringUserOnFirstUseLocked(user store.DoorLockUser) {
	if lockdef.UserTypeEnum(user.UserType) != lockdef.UserTypeExpiringUser || user.ExpiringUserExpiresAt != nil {
		return
	}
	minutes, ok := s.uint16Attr(lockdef.AttrExpiringUserTimeout)
	if !ok {
		return
	}
	expiresAt := s.clock.Now().UnixMilli() + int64(minutes)*int64(time.Minute/time.Millisecond)
	if live := s.findUser(user.UserIndex); live != nil {
		live.ExpiringUserExpiresAt = &expiresAt
		s.dirty = true
	}
	s.armExpiryTimer(user.UserIndex, expiresAt)
}

// armExpiryTimer mirrors matter.js DoorLockServer.ts:1223-1231: the timer
// waits for the remaining time, at most [maxExpiryDelay], at once for a
// deadline that has passed. The caller holds mu (or is New).
func (s *DoorLockServer) armExpiryTimer(userIndex uint16, expiresAt int64) {
	s.stopExpiryTimerLocked(userIndex)
	remaining := time.Duration(expiresAt-s.clock.Now().UnixMilli()) * time.Millisecond
	delay := min(max(remaining, 0), maxExpiryDelay)
	t := &serverTimer{}
	// The action takes op: it runs in a goroutine of its own, as matter.js's
	// callback waits for the lock (:166, `{ lock: true }`).
	t.t = s.clock.AfterFunc(delay, func() { s.goTimerAction(t, func() { s.runExpireUser(userIndex, t) }) })
	s.expiryTimers[userIndex] = t
}

// stopExpiryTimerLocked mirrors matter.js DoorLockServer.ts:1241-1244.
func (s *DoorLockServer) stopExpiryTimerLocked(userIndex uint16) {
	if t, ok := s.expiryTimers[userIndex]; ok {
		t.stop()
		delete(s.expiryTimers, userIndex)
	}
}

// runExpireUser mirrors matter.js DoorLockServer.ts:1246-1268 (#expireUser):
// the deadline is checked again; one not reached yet re-arms the timer.
func (s *DoorLockServer) runExpireUser(userIndex uint16, t *serverTimer) {
	s.op.Lock()
	defer s.op.Unlock()
	_ = s.commit(s.base, func() error {
		if s.expiryTimers[userIndex] == t {
			delete(s.expiryTimers, userIndex)
		}
		user := s.findUser(userIndex)
		if user == nil || lockdef.UserTypeEnum(user.UserType) != lockdef.UserTypeExpiringUser ||
			lockdef.UserStatusEnum(user.UserStatus) == lockdef.UserStatusOccupiedDisabled || user.ExpiringUserExpiresAt == nil {
			return nil
		}
		if !s.hasExpired(user) {
			s.armExpiryTimer(userIndex, *user.ExpiringUserExpiresAt)
			return nil
		}
		s.disableUserLocked(userIndex)
		return nil
	})
}

// disableUserLocked mirrors matter.js DoorLockServer.ts:1270-1285: the
// user is OccupiedDisabled, reported as a change of no fabric and no
// source node.
func (s *DoorLockServer) disableUserLocked(userIndex uint16) {
	user := s.findUser(userIndex)
	if user == nil {
		return
	}
	user.UserStatus = uint8(lockdef.UserStatusOccupiedDisabled)
	s.dirty = true
	s.queue = append(s.queue, queuedEvent{lockdef.EventLockUserChange, lockdef.LockUserChangeEvent{
		LockDataType:      lockdef.LockDataTypeUserIndex,
		DataOperationType: lockdef.DataOperationTypeModify,
		OperationSource:   lockdef.OperationSourceUnspecified,
		UserIndex:         spec.ValueOf(userIndex),
		FabricIndex:       spec.NullOf[uint8](),
		SourceNode:        spec.NullOf[uint64](),
		DataIndex:         spec.ValueOf(userIndex),
	}})
}

// ── The user and credential store (LockAuth.Store, LockAuth.ts:129-278) ──

// findUser is LockAuth.Store.findUser (:140-142): the live record.
func (s *DoorLockServer) findUser(userIndex uint16) *store.DoorLockUser {
	for i := range s.data.Users {
		if s.data.Users[i].UserIndex == userIndex {
			return &s.data.Users[i]
		}
	}
	return nil
}

// findNextOccupiedUserIndex is LockAuth.Store :144-152.
func (s *DoorLockServer) findNextOccupiedUserIndex(after uint16) *uint16 {
	var next *uint16
	for _, u := range s.data.Users {
		if u.UserIndex > after && (next == nil || u.UserIndex < *next) {
			idx := u.UserIndex
			next = &idx
		}
	}
	return next
}

// findAvailableUserIndex is LockAuth.Store :154-160.
func (s *DoorLockServer) findAvailableUserIndex(maxUsers uint16) (uint16, bool) {
	for i := uint16(1); i <= maxUsers && i != 0; i++ {
		if s.findUser(i) == nil {
			return i, true
		}
	}
	return 0, false
}

// findUserIndexForCredential is LockAuth.Store :162-169.
func (s *DoorLockServer) findUserIndexForCredential(credType uint8, index uint16) (uint16, bool) {
	for _, u := range s.data.Users {
		for _, c := range u.Credentials {
			if c.Type == credType && c.Index == index {
				return u.UserIndex, true
			}
		}
	}
	return 0, false
}

func (s *DoorLockServer) addUserLocked(u store.DoorLockUser) {
	s.data.Users = append(s.data.Users, u)
	s.dirty = true
}

func (s *DoorLockServer) removeUserLocked(userIndex uint16) {
	s.data.Users = slices.DeleteFunc(s.data.Users, func(u store.DoorLockUser) bool { return u.UserIndex == userIndex })
	s.dirty = true
}

// clearSchedulesForUserLocked mirrors matter.js DoorLockServer.ts:1485-1488.
func (s *DoorLockServer) clearSchedulesForUserLocked(userIndex uint16) {
	s.data.WeekDaySchedules = slices.DeleteFunc(s.data.WeekDaySchedules, func(w store.DoorLockWeekDaySchedule) bool { return w.UserIndex == userIndex })
	s.data.YearDaySchedules = slices.DeleteFunc(s.data.YearDaySchedules, func(y store.DoorLockYearDaySchedule) bool { return y.UserIndex == userIndex })
	s.dirty = true
}

// cloneUser deep-copies a user.
func cloneUser(u store.DoorLockUser) store.DoorLockUser {
	out := u
	out.Credentials = slices.Clone(u.Credentials)
	if u.UserUniqueID != nil {
		v := *u.UserUniqueID
		out.UserUniqueID = &v
	}
	if u.ExpiringUserExpiresAt != nil {
		v := *u.ExpiringUserExpiresAt
		out.ExpiringUserExpiresAt = &v
	}
	return out
}

// cloneAuth deep-copies the records.
func cloneAuth(a store.DoorLockAuth) store.DoorLockAuth {
	out := store.DoorLockAuth{
		WeekDaySchedules: slices.Clone(a.WeekDaySchedules),
		YearDaySchedules: slices.Clone(a.YearDaySchedules),
		HolidaySchedules: slices.Clone(a.HolidaySchedules),
	}
	if a.Users != nil {
		out.Users = make([]store.DoorLockUser, len(a.Users))
		for i, u := range a.Users {
			out.Users[i] = cloneUser(u)
		}
	}
	if a.Credentials != nil {
		out.Credentials = make([]store.DoorLockCredential, len(a.Credentials))
		for i, c := range a.Credentials {
			out.Credentials[i] = c
			out.Credentials[i].Data = slices.Clone(c.Data)
		}
	}
	return out
}
