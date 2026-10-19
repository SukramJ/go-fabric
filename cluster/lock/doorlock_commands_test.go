// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
)

// The commands' remaining statuses, invoked on the server directly (the
// matter.js sources each case mirrors are cited on the server's code).

func newLocal(t *testing.T, edit func(*lock.DoorLockConfig)) (*lock.DoorLockServer, *fakeEventEmitter) {
	t.Helper()
	cfg := scheduledLockState(scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
	cfg.Features |= lock.FeatureDPS
	if edit != nil {
		edit(&cfg)
	}
	srv, err := lock.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	em := &fakeEventEmitter{}
	srv.SetMatterEventEmitter(em)
	srv.SetEndpoint(3)
	return srv, em
}

func invokeStatus(t *testing.T, srv *lock.DoorLockServer, ctx context.Context, cmd uint32, req any) (any, im.StatusCode) {
	t.Helper()
	res, err := srv.MatterInvoke(ctx, cmd, req)
	if err == nil {
		return res, im.StatusSuccess
	}
	var sc im.StatusCodeError
	if !errors.As(err, &sc) {
		t.Fatalf("command 0x%02X: %v carries no status", cmd, err)
	}
	return res, sc.MatterStatusCode()
}

func setUserReq(op lockdef.DataOperationTypeEnum, index uint16) lockdef.SetUserRequest {
	return lockdef.SetUserRequest{
		OperationType: op, UserIndex: index,
		UserName: spec.NullOf[string](), UserUniqueId: spec.NullOf[uint32](),
		UserStatus: spec.NullOf[lockdef.UserStatusEnum](), UserType: spec.NullOf[lockdef.UserTypeEnum](),
		CredentialRule: spec.NullOf[lockdef.CredentialRuleEnum](),
	}
}

func TestUserCommandStatuses(t *testing.T) {
	t.Parallel()
	srv, _ := newLocal(t, nil)
	ctx := withPairFabric()
	other := im.WithFabricFilter(context.Background(), false, 2)

	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetUser, setUserReq(lockdef.DataOperationTypeAdd, 1)); st != im.StatusFailure {
		t.Errorf("Add on an occupied slot: 0x%02X, want FAILURE (cluster status Occupied)", st)
	}
	_, err := srv.MatterInvoke(ctx, lockdef.CmdSetUser, setUserReq(lockdef.DataOperationTypeAdd, 1))
	var cs im.MatterClusterStatusError
	if !errors.As(err, &cs) || cs.MatterClusterStatus() != uint8(lockdef.StatusCodeOccupied) {
		t.Errorf("Add on an occupied slot: %v, want cluster status Occupied", err)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetUser, setUserReq(lockdef.DataOperationTypeModify, 5)); st != im.StatusInvalidCommand {
		t.Errorf("Modify of an available slot: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetUser, setUserReq(lockdef.DataOperationTypeAdd, 11)); st != im.StatusInvalidCommand {
		t.Errorf("Add past NumberOfTotalUsersSupported: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, context.Background(), lockdef.CmdSetUser, setUserReq(lockdef.DataOperationTypeAdd, 2)); st != im.StatusUnsupportedAccess {
		t.Errorf("Add without a fabric: 0x%02X", st)
	}
	named := setUserReq(lockdef.DataOperationTypeModify, 1)
	named.UserName = spec.ValueOf("x")
	if _, st := invokeStatus(t, srv, other, lockdef.CmdSetUser, named); st != im.StatusInvalidCommand {
		t.Errorf("rename from another fabric: 0x%02X", st)
	}
	unique := setUserReq(lockdef.DataOperationTypeModify, 1)
	unique.UserUniqueId = spec.ValueOf[uint32](7)
	if _, st := invokeStatus(t, srv, other, lockdef.CmdSetUser, unique); st != im.StatusInvalidCommand {
		t.Errorf("unique id from another fabric: 0x%02X", st)
	}
	for _, bad := range []lockdef.SetUserRequest{
		func() lockdef.SetUserRequest { r := setUserReq(lockdef.DataOperationTypeClear, 2); return r }(),
		func() lockdef.SetUserRequest {
			r := setUserReq(lockdef.DataOperationTypeAdd, 2)
			r.UserStatus = spec.ValueOf(lockdef.UserStatusAvailable)
			return r
		}(),
		func() lockdef.SetUserRequest {
			r := setUserReq(lockdef.DataOperationTypeAdd, 2)
			r.UserType = spec.ValueOf(lockdef.UserTypeProgrammingUser)
			return r
		}(),
		func() lockdef.SetUserRequest {
			r := setUserReq(lockdef.DataOperationTypeAdd, 2)
			r.CredentialRule = spec.ValueOf(lockdef.CredentialRuleEnum(9))
			return r
		}(),
	} {
		if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetUser, bad); st != im.StatusInvalidCommand {
			t.Errorf("SetUser %+v: 0x%02X, want INVALID_COMMAND", bad, st)
		}
	}
	full := setUserReq(lockdef.DataOperationTypeAdd, 3)
	full.UserName, full.UserUniqueId = spec.ValueOf("guest"), spec.ValueOf[uint32](42)
	full.UserStatus, full.UserType = spec.ValueOf(lockdef.UserStatusOccupiedDisabled), spec.ValueOf(lockdef.UserTypeForcedUser)
	full.CredentialRule = spec.ValueOf(lockdef.CredentialRuleDual)
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetUser, full); st != im.StatusSuccess {
		t.Fatalf("Add user 3: 0x%02X", st)
	}
	res, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetUser, lockdef.GetUserRequest{UserIndex: 1})
	if got := res.(lockdef.GetUserResponse); got.NextUserIndex.Null || got.NextUserIndex.Value != 3 {
		t.Errorf("GetUser 1 next %+v, want 3", got.NextUserIndex)
	}
	res, _ = invokeStatus(t, srv, ctx, lockdef.CmdGetUser, lockdef.GetUserRequest{UserIndex: 2})
	if got := res.(lockdef.GetUserResponse); !got.UserStatus.Null || got.NextUserIndex.Value != 3 {
		t.Errorf("GetUser 2 = %+v, want an empty slot before 3", got)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdGetUser, lockdef.GetUserRequest{UserIndex: 0}); st != im.StatusInvalidCommand {
		t.Errorf("GetUser 0: 0x%02X", st)
	}
	if u, ok := srv.User(3); !ok || u.UserName != "guest" || *u.UserUniqueID != 42 {
		t.Errorf("user 3 %+v", u)
	}
	if users := srv.Users(); len(users) != 2 {
		t.Errorf("users %+v, want two", users)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearUser, lockdef.ClearUserRequest{UserIndex: 7}); st != im.StatusSuccess {
		t.Errorf("ClearUser of an empty slot: 0x%02X, want silent success", st)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearUser, lockdef.ClearUserRequest{UserIndex: 11}); st != im.StatusInvalidCommand {
		t.Errorf("ClearUser 11: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearUser, lockdef.ClearUserRequest{UserIndex: 1}); st != im.StatusSuccess {
		t.Errorf("ClearUser 1: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdGetUser, "not a request"); st != im.StatusInvalidCommand {
		t.Errorf("GetUser with foreign fields: 0x%02X", st)
	}
}

func TestCredentialCommandStatuses(t *testing.T) {
	t.Parallel()
	srv, _ := newLocal(t, func(c *lock.DoorLockConfig) {
		c.Features |= lock.FeatureRID
		c.NumberOfRfidUsersSupported, c.MinRfidCodeLength, c.MaxRfidCodeLength = 4, 2, 8
	})
	ctx := withPairFabric()
	other := im.WithFabricFilter(context.Background(), false, 2)
	status := func(res any) lockdef.Status { return res.(lockdef.SetCredentialResponse).Status }

	res, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetCredentialStatus, lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypePin, 1)})
	got := res.(lockdef.GetCredentialStatusResponse)
	if !got.CredentialExists || got.UserIndex.Value != 1 || got.CreatorFabricIndex.Value != pairFabric {
		t.Errorf("GetCredentialStatus PIN 1 = %+v", got)
	}
	rfid := addPin(1, "abcd", user1)
	rfid.Credential = credential(lockdef.CredentialTypeRfid, 1)
	if res, _ := invokeStatus(t, srv, ctx, lockdef.CmdSetCredential, rfid); status(res) != lockdef.StatusSuccess {
		t.Errorf("RFID add %+v", res)
	}
	for _, c := range []struct {
		name string
		ctx  context.Context
		req  lockdef.SetCredentialRequest
		want lockdef.Status
	}{
		{"another fabric's user", other, addPin(2, "2222", user1), lockdef.StatusInvalidCommand},
		{"a user that does not exist", ctx, addPin(2, "2222", spec.ValueOf[uint16](4)), lockdef.StatusInvalidCommand},
		{"an RFID too long", ctx, func() lockdef.SetCredentialRequest {
			r := addPin(2, "123456789", user1)
			r.Credential = credential(lockdef.CredentialTypeRfid, 2)
			return r
		}(), lockdef.StatusInvalidCommand},
		{"a modify of a missing credential", ctx, func() lockdef.SetCredentialRequest {
			r := addPin(5, "5555", user1)
			r.OperationType = lockdef.DataOperationTypeModify
			return r
		}(), lockdef.StatusInvalidCommand},
		{"a modify from another fabric", other, func() lockdef.SetCredentialRequest {
			r := addPin(1, "5555", noUser)
			r.OperationType = lockdef.DataOperationTypeModify
			return r
		}(), lockdef.StatusInvalidCommand},
		{"a modify naming a user without it", ctx, func() lockdef.SetCredentialRequest {
			r := addPin(1, "5555", spec.ValueOf[uint16](4))
			r.OperationType = lockdef.DataOperationTypeModify
			return r
		}(), lockdef.StatusInvalidCommand},
	} {
		if res, _ := invokeStatus(t, srv, c.ctx, lockdef.CmdSetCredential, c.req); status(res) != c.want {
			t.Errorf("%s: %+v, want %d", c.name, res, c.want)
		}
	}
	// The per-user credential limit (NumberOfCredentialsSupportedPerUser 5;
	// user 1 holds PIN 1 and RFID 1).
	for i := uint16(2); i <= 4; i++ {
		if res, _ := invokeStatus(t, srv, ctx, lockdef.CmdSetCredential, addPin(i, "100"+string(rune('0'+i)), user1)); status(res) != lockdef.StatusSuccess {
			t.Fatalf("PIN %d: %+v", i, res)
		}
	}
	if res, _ := invokeStatus(t, srv, ctx, lockdef.CmdSetCredential, addPin(5, "2005", user1)); status(res) != lockdef.StatusResourceExhausted {
		t.Errorf("a sixth credential: %+v, want RESOURCE_EXHAUSTED", res)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetCredential, func() lockdef.SetCredentialRequest {
		r := addPin(7, "7777", spec.ValueOf[uint16](11))
		return r
	}()); st != im.StatusInvalidCommand {
		t.Errorf("a user index past the total: 0x%02X", st)
	}
	res, _ = invokeStatus(t, srv, ctx, lockdef.CmdGetCredentialStatus, lockdef.GetCredentialStatusRequest{Credential: credential(lockdef.CredentialTypePin, 7)})
	if got := res.(lockdef.GetCredentialStatusResponse); got.CredentialExists || got.NextCredentialIndex == nil || !got.NextCredentialIndex.Null {
		t.Errorf("GetCredentialStatus PIN 7 = %+v", got)
	}

	for _, c := range []struct {
		name string
		ctx  context.Context
		req  lockdef.ClearCredentialRequest
		want im.StatusCode
	}{
		{"the programming PIN", ctx, lockdef.ClearCredentialRequest{Credential: spec.ValueOf(credential(lockdef.CredentialTypeProgrammingPin, 0))}, im.StatusInvalidCommand},
		{"a missing credential", ctx, lockdef.ClearCredentialRequest{Credential: spec.ValueOf(credential(lockdef.CredentialTypePin, 9))}, im.StatusInvalidCommand},
		{"without a fabric", context.Background(), lockdef.ClearCredentialRequest{Credential: spec.NullOf[lockdef.CredentialStruct]()}, im.StatusUnsupportedAccess},
		{"every credential", ctx, lockdef.ClearCredentialRequest{Credential: spec.NullOf[lockdef.CredentialStruct]()}, im.StatusSuccess},
	} {
		if _, st := invokeStatus(t, srv, c.ctx, lockdef.CmdClearCredential, c.req); st != c.want {
			t.Errorf("ClearCredential %s: 0x%02X, want 0x%02X", c.name, st, c.want)
		}
	}
	if u, _ := srv.User(1); len(u.Credentials) != 0 {
		t.Errorf("user 1 credentials after clearing all: %+v", u.Credentials)
	}
}

func TestScheduleCommandStatuses(t *testing.T) {
	t.Parallel()
	srv, _ := newLocal(t, nil)
	ctx := withPairFabric()

	week := lockdef.SetWeekDayScheduleRequest{WeekDayIndex: 1, UserIndex: 1, DaysMask: lockdef.DaysMaskMonday, StartHour: 8, EndHour: 9}
	for _, c := range []struct {
		name string
		req  lockdef.SetWeekDayScheduleRequest
		want im.StatusCode
	}{
		{"index past the count", func() lockdef.SetWeekDayScheduleRequest { r := week; r.WeekDayIndex = 11; return r }(), im.StatusInvalidCommand},
		{"a user past the total", func() lockdef.SetWeekDayScheduleRequest { r := week; r.UserIndex = 11; return r }(), im.StatusInvalidCommand},
		{"a user that does not exist", func() lockdef.SetWeekDayScheduleRequest { r := week; r.UserIndex = 2; return r }(), im.StatusFailure},
		{"an end before the start", func() lockdef.SetWeekDayScheduleRequest { r := week; r.EndHour = 7; return r }(), im.StatusInvalidCommand},
		{"an end at the start", func() lockdef.SetWeekDayScheduleRequest { r := week; r.EndHour = 8; return r }(), im.StatusInvalidCommand},
		{"a valid schedule", week, im.StatusSuccess},
	} {
		if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetWeekDaySchedule, c.req); st != c.want {
			t.Errorf("SetWeekDaySchedule %s: 0x%02X, want 0x%02X", c.name, st, c.want)
		}
	}
	res, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetWeekDaySchedule, lockdef.GetWeekDayScheduleRequest{WeekDayIndex: 1, UserIndex: 1})
	if got := res.(lockdef.GetWeekDayScheduleResponse); got.Status != lockdef.StatusSuccess || *got.StartHour != 8 || *got.EndHour != 9 {
		t.Errorf("GetWeekDaySchedule 1 = %+v", got)
	}
	for _, c := range []struct {
		req  lockdef.GetWeekDayScheduleRequest
		want lockdef.Status
	}{
		{lockdef.GetWeekDayScheduleRequest{WeekDayIndex: 0, UserIndex: 1}, lockdef.StatusInvalidCommand},
		{lockdef.GetWeekDayScheduleRequest{WeekDayIndex: 1, UserIndex: 2}, lockdef.StatusNotFound},
		{lockdef.GetWeekDayScheduleRequest{WeekDayIndex: 2, UserIndex: 1}, lockdef.StatusNotFound},
	} {
		res, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetWeekDaySchedule, c.req)
		if got := res.(lockdef.GetWeekDayScheduleResponse); got.Status != c.want {
			t.Errorf("GetWeekDaySchedule %+v = %d, want %d", c.req, got.Status, c.want)
		}
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearWeekDaySchedule, lockdef.ClearWeekDayScheduleRequest{WeekDayIndex: 11, UserIndex: 1}); st != im.StatusInvalidCommand {
		t.Errorf("ClearWeekDaySchedule 11: 0x%02X", st)
	}

	year := lockdef.SetYearDayScheduleRequest{YearDayIndex: 1, UserIndex: 1, LocalStartTime: 100, LocalEndTime: 200}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetYearDaySchedule, func() lockdef.SetYearDayScheduleRequest { r := year; r.LocalEndTime = 100; return r }()); st != im.StatusInvalidCommand {
		t.Errorf("SetYearDaySchedule with an empty window: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetYearDaySchedule, func() lockdef.SetYearDayScheduleRequest { r := year; r.YearDayIndex = 0; return r }()); st != im.StatusInvalidCommand {
		t.Errorf("SetYearDaySchedule 0: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetYearDaySchedule, year); st != im.StatusSuccess {
		t.Fatalf("SetYearDaySchedule: 0x%02X", st)
	}
	res, _ = invokeStatus(t, srv, ctx, lockdef.CmdGetYearDaySchedule, lockdef.GetYearDayScheduleRequest{YearDayIndex: 1, UserIndex: 1})
	if got := res.(lockdef.GetYearDayScheduleResponse); got.Status != lockdef.StatusSuccess || *got.LocalEndTime != 200 {
		t.Errorf("GetYearDaySchedule 1 = %+v", got)
	}
	for _, c := range []struct {
		req  lockdef.GetYearDayScheduleRequest
		want lockdef.Status
	}{
		{lockdef.GetYearDayScheduleRequest{YearDayIndex: 11, UserIndex: 1}, lockdef.StatusInvalidCommand},
		{lockdef.GetYearDayScheduleRequest{YearDayIndex: 1, UserIndex: 2}, lockdef.StatusNotFound},
		{lockdef.GetYearDayScheduleRequest{YearDayIndex: 2, UserIndex: 1}, lockdef.StatusNotFound},
	} {
		res, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetYearDaySchedule, c.req)
		if got := res.(lockdef.GetYearDayScheduleResponse); got.Status != c.want {
			t.Errorf("GetYearDaySchedule %+v = %d, want %d", c.req, got.Status, c.want)
		}
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearYearDaySchedule, lockdef.ClearYearDayScheduleRequest{YearDayIndex: 0xFE, UserIndex: 1}); st != im.StatusSuccess {
		t.Errorf("ClearYearDaySchedule all: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearYearDaySchedule, lockdef.ClearYearDayScheduleRequest{YearDayIndex: 11, UserIndex: 1}); st != im.StatusInvalidCommand {
		t.Errorf("ClearYearDaySchedule 11: 0x%02X", st)
	}

	holiday := lockdef.SetHolidayScheduleRequest{HolidayIndex: 1, LocalStartTime: 100, LocalEndTime: 200, OperatingMode: lockdef.OperatingModeVacation}
	for _, c := range []struct {
		name string
		req  lockdef.SetHolidayScheduleRequest
		want im.StatusCode
	}{
		{"index 0", func() lockdef.SetHolidayScheduleRequest { r := holiday; r.HolidayIndex = 0; return r }(), im.StatusInvalidCommand},
		{"an empty window", func() lockdef.SetHolidayScheduleRequest { r := holiday; r.LocalEndTime = 50; return r }(), im.StatusInvalidCommand},
		{"an operating mode the cluster does not define", func() lockdef.SetHolidayScheduleRequest {
			r := holiday
			r.OperatingMode = 9
			return r
		}(), im.StatusInvalidCommand},
		{"a valid schedule", holiday, im.StatusSuccess},
	} {
		if _, st := invokeStatus(t, srv, ctx, lockdef.CmdSetHolidaySchedule, c.req); st != c.want {
			t.Errorf("SetHolidaySchedule %s: 0x%02X, want 0x%02X", c.name, st, c.want)
		}
	}
	res, _ = invokeStatus(t, srv, ctx, lockdef.CmdGetHolidaySchedule, lockdef.GetHolidayScheduleRequest{HolidayIndex: 1})
	if got := res.(lockdef.GetHolidayScheduleResponse); got.Status != lockdef.StatusSuccess || got.OperatingMode.Value != lockdef.OperatingModeVacation {
		t.Errorf("GetHolidaySchedule 1 = %+v", got)
	}
	for _, c := range []struct {
		idx  uint8
		want lockdef.Status
	}{{0, lockdef.StatusInvalidCommand}, {2, lockdef.StatusNotFound}} {
		res, _ := invokeStatus(t, srv, ctx, lockdef.CmdGetHolidaySchedule, lockdef.GetHolidayScheduleRequest{HolidayIndex: c.idx})
		if got := res.(lockdef.GetHolidayScheduleResponse); got.Status != c.want {
			t.Errorf("GetHolidaySchedule %d = %d, want %d", c.idx, got.Status, c.want)
		}
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearHolidaySchedule, lockdef.ClearHolidayScheduleRequest{HolidayIndex: 11}); st != im.StatusInvalidCommand {
		t.Errorf("ClearHolidaySchedule 11: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdClearHolidaySchedule, lockdef.ClearHolidayScheduleRequest{HolidayIndex: 0xFE}); st != im.StatusSuccess {
		t.Errorf("ClearHolidaySchedule all: 0x%02X", st)
	}
	if _, st := invokeStatus(t, srv, context.Background(), lockdef.CmdClearHolidaySchedule, lockdef.ClearHolidayScheduleRequest{HolidayIndex: 0xFE}); st != im.StatusUnsupportedAccess {
		t.Errorf("ClearHolidaySchedule without a fabric: 0x%02X", st)
	}
}

func TestDoorStateAndAlarms(t *testing.T) {
	t.Parallel()
	srv, em := newLocal(t, nil)
	if err := srv.SetDoorState(lockdef.DoorStateDoorOpen); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetDoorState(lockdef.DoorStateDoorJammed); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(lockdef.AttrDoorState); v != uint8(lockdef.DoorStateDoorJammed) {
		t.Errorf("DoorState %v", v)
	}
	srv.ReportAlarm(lockdef.AlarmCodeLockJammed)
	if err := srv.SetLockState(lockdef.LockStateNotFullyLocked); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(lockdef.AttrLockState); v != uint8(lockdef.LockStateNotFullyLocked) {
		t.Errorf("LockState %v", v)
	}
	var ids []uint32
	for _, c := range em.calls {
		ids = append(ids, c.event)
	}
	want := []uint32{lockdef.EventDoorStateChange, lockdef.EventDoorStateChange, lockdef.EventDoorLockAlarm}
	if len(ids) != len(want) || ids[0] != want[0] || ids[2] != want[2] || em.calls[0].endpoint != 3 {
		t.Errorf("events %v on %d, want %v on 3", ids, em.calls[0].endpoint, want)
	}
	srv.MatterQuiesce()
}

func TestLockCommandFields(t *testing.T) {
	t.Parallel()
	srv, _ := newLocal(t, nil)
	ctx := withPairFabric()
	code := pin("1234")
	for _, c := range []struct {
		cmd uint32
		req any
	}{
		{lockdef.CmdUnlockDoor, &lockdef.UnlockDoorRequest{PinCode: &code}},
		{lockdef.CmdLockDoor, &lockdef.LockDoorRequest{PinCode: &code}},
		{lockdef.CmdUnboltDoor, &lockdef.UnboltDoorRequest{PinCode: &code}},
		{lockdef.CmdUnlockWithTimeout, &lockdef.UnlockWithTimeoutRequest{Timeout: 1, PinCode: &code}},
		{lockdef.CmdLockDoor, nil},
	} {
		if _, st := invokeStatus(t, srv, ctx, c.cmd, c.req); st != im.StatusSuccess {
			t.Errorf("command 0x%02X with %T: 0x%02X", c.cmd, c.req, st)
		}
	}
	if _, st := invokeStatus(t, srv, ctx, lockdef.CmdLockDoor, 42); st != im.StatusInvalidCommand {
		t.Errorf("LockDoor with foreign fields: 0x%02X", st)
	}
	if _, err := srv.MatterInvoke(ctx, lockdef.CmdSetAliroReaderConfig, nil); err == nil {
		t.Error("an ALIRO command was accepted")
	}
	if lock.ActuationUnbolt.String() != "unbolt" || lock.Actuation(9).String() == "" {
		t.Error("Actuation.String")
	}
	if (&lock.LockOperationFailedError{Message: "m"}).Error() != "m" || (&lock.LockOperationFailedError{}).Error() == "" {
		t.Error("LockOperationFailedError.Error")
	}
}
