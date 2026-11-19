// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	evsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// decodeRequest is req as the definition's request check answers it:
// encoded, then decoded through spec.DecodeRequest.
func decodeRequest(t *testing.T, clusterID, cmdID uint32, req spec.Encodable) (any, error) {
	t.Helper()
	enc := tlv.NewEncoder()
	req.EncodeTLV(enc, tlv.AnonymousTag())
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(raw)
	open, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	fields, ok, err := spec.DecodeRequest(clusterID, cmdID, dec, open)
	if !ok {
		t.Fatalf("cluster 0x%04X command 0x%02X has no request decoder", clusterID, cmdID)
	}
	return fields, err
}

// TestRequestCheckParityMatterJS holds the request check a definition
// makes before the server runs to matter.js's: the request schema refuses
// a field outside its constraint with CONSTRAINT_ERROR
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:474-488
// at b25ed960), and a server that departs from it says so in the
// relaxation table (script/clustergen/relaxed.go) — DoorLock answers
// INVALID_COMMAND for fifteen commands (onValidationError,
// DoorLockServer.ts:1494-1516 at b25ed960) and leaves the get*Schedule
// indexes to the handler (constraint = false, :1518-1526); its other
// commands are held to the schema as every command is.
func TestRequestCheckParityMatterJS(t *testing.T) {
	t.Parallel()
	status := spec.StatusOf

	// A server held to the model: OnOff's OnTime "max 65534".
	if _, err := decodeRequest(t, onoffdef.ClusterID, onoffdef.CmdOnWithTimedOff, onoffdef.OnWithTimedOffRequest{OnTime: 65535}); status(err) != im.StatusConstraintError {
		t.Errorf("OnWithTimedOff OnTime 65535: %v, want CONSTRAINT_ERROR", err)
	}
	// A list over its count: EnergyEvse SetTargets' ChargingTargetSchedules
	// "max 7".
	if _, err := decodeRequest(t, evsedef.ClusterID, evsedef.CmdSetTargets, evsedef.SetTargetsRequest{ChargingTargetSchedules: make([]evsedef.ChargingTargetScheduleStruct, 8)}); status(err) != im.StatusConstraintError {
		t.Errorf("SetTargets with 8 schedules: %v, want CONSTRAINT_ERROR", err)
	}
	// DoorLock, a refusal command: a UserName over "max 10" is
	// INVALID_COMMAND.
	long := spec.ValueOf("elevenchars")
	if _, err := decodeRequest(t, lockdef.ClusterID, lockdef.CmdSetUser, lockdef.SetUserRequest{OperationType: lockdef.DataOperationTypeAdd, UserIndex: 1, UserName: long}); status(err) != im.StatusInvalidCommand {
		t.Errorf("SetUser with an 11-character UserName: %v, want INVALID_COMMAND", err)
	}
	// DoorLock, a command without configuration: the schema holds it, a
	// SigningKey of 31 bytes against "32" is CONSTRAINT_ERROR (the
	// cluster-wide skip of db021da0 is gone at b25ed960).
	if _, err := decodeRequest(t, lockdef.ClusterID, lockdef.CmdSetAliroReaderConfig, lockdef.SetAliroReaderConfigRequest{
		SigningKey: make([]byte, 31), VerificationKey: make([]byte, 65), GroupIdentifier: make([]byte, 16),
	}); status(err) != im.StatusConstraintError {
		t.Errorf("SetAliroReaderConfig with a 31-byte SigningKey: %v, want CONSTRAINT_ERROR", err)
	}
	// DoorLock, a get*Schedule index: 0 reaches the server, which answers
	// it in its response (DoorLockServer.ts:1518-1526 at b25ed960); the
	// type's width still holds.
	if _, err := decodeRequest(t, lockdef.ClusterID, lockdef.CmdGetHolidaySchedule, lockdef.GetHolidayScheduleRequest{HolidayIndex: 0}); err != nil {
		t.Errorf("GetHolidaySchedule index 0: %v, want it decoded", err)
	}
	refusal := map[uint32]bool{
		lockdef.CmdSetUser: true, lockdef.CmdGetUser: true, lockdef.CmdClearUser: true, lockdef.CmdSetCredential: true,
		lockdef.CmdSetWeekDaySchedule: true, lockdef.CmdClearWeekDaySchedule: true, lockdef.CmdSetYearDaySchedule: true,
		lockdef.CmdClearYearDaySchedule: true, lockdef.CmdSetHolidaySchedule: true, lockdef.CmdClearHolidaySchedule: true,
		lockdef.CmdGetWeekDaySchedule: true, lockdef.CmdGetYearDaySchedule: true, lockdef.CmdGetHolidaySchedule: true,
		lockdef.CmdClearCredential: true, lockdef.CmdGetCredentialStatus: true,
	}
	for ci := range lockdef.Definition.Commands {
		cmd := &lockdef.Definition.Commands[ci]
		if cmd.Direction != spec.Request {
			continue
		}
		want := spec.RequestCheck{}
		if refusal[cmd.ID] {
			want = spec.RequestCheck{Refusal: im.StatusInvalidCommand, Ref: cmd.Check.Ref}
		}
		if cmd.Check != want || refusal[cmd.ID] && cmd.Check.Ref == "" {
			t.Errorf("DoorLock %s check %+v, want %+v with its reference", cmd.Name, cmd.Check, want)
		}
	}
	relaxed := map[string]bool{
		"GetWeekDaySchedule.WeekDayIndex": true, "GetWeekDaySchedule.UserIndex": true,
		"GetYearDaySchedule.YearDayIndex": true, "GetYearDaySchedule.UserIndex": true,
		"GetHolidaySchedule.HolidayIndex": true,
	}
	for ci := range lockdef.Definition.Commands {
		cmd := &lockdef.Definition.Commands[ci]
		if cmd.Direction != spec.Request {
			continue
		}
		for fi := range cmd.Fields {
			if f := &cmd.Fields[fi]; (f.Relaxed != "") != relaxed[cmd.Name+"."+f.Name] {
				t.Errorf("DoorLock %s.%s relaxed %q", cmd.Name, f.Name, f.Relaxed)
			}
		}
	}
}

// rawRequest is a request encoded by hand: a field left out, as no
// generated request type can encode it.
type rawRequest func(*tlv.Encoder, tlv.Tag)

func (r rawRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { r(enc, tag) }

// refusalKind is one kind of request-schema violation sent to a DoorLock
// refusal command.
type refusalKind struct {
	name string
	cmd  uint32
	req  spec.Encodable
}

// assertInvalidCommand holds each kind to INVALID_COMMAND, the status
// matter.js answers the ten refusal commands' validation errors with.
func assertInvalidCommand(t *testing.T, kinds []refusalKind) {
	t.Helper()
	for _, c := range kinds {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeRequest(t, lockdef.ClusterID, c.cmd, c.req); spec.StatusOf(err) != im.StatusInvalidCommand {
				t.Errorf("%v (status %v), want INVALID_COMMAND", err, spec.StatusOf(err))
			}
		})
	}
}

func weekDayRequest(mod func(*lockdef.SetWeekDayScheduleRequest)) lockdef.SetWeekDayScheduleRequest {
	r := lockdef.SetWeekDayScheduleRequest{WeekDayIndex: 1, UserIndex: 1, DaysMask: lockdef.DaysMaskMonday, StartHour: 8, EndHour: 9}
	mod(&r)
	return r
}

func userRequest(mod func(*lockdef.SetUserRequest)) lockdef.SetUserRequest {
	r := lockdef.SetUserRequest{OperationType: lockdef.DataOperationTypeAdd, UserIndex: 1}
	mod(&r)
	return r
}

// TestDoorLockRefusalKindsParityMatterJS (F-db021da0-20): DoorLock's
// refusal commands answer INVALID_COMMAND for a value over its bound and a
// missing mandatory field, as matter.js answers the ten's validation
// errors with StatusResponseError("Invalid field value",
// Status.InvalidCommand)
// (packages/node/src/behaviors/door-lock/DoorLockServer.ts:1494-1511 at
// db021da0). Compared kind by kind with matter.js's validator; a
// per-field run against matter.js was not made.
func TestDoorLockRefusalKindsParityMatterJS(t *testing.T) {
	t.Parallel()
	assertInvalidCommand(t, []refusalKind{
		// A value over its bound: StartHour "max 23"
		// (door-lock-cluster.element.ts:262-265).
		{"SetWeekDaySchedule StartHour 24", lockdef.CmdSetWeekDaySchedule, weekDayRequest(func(r *lockdef.SetWeekDayScheduleRequest) {
			r.StartHour = 24
		})},
		// A mandatory field missing (conformance;
		// packages/node/src/behavior/state/validation/ValueValidator.ts:146,
		// read by 6a's reference read, not re-read).
		{"SetWeekDaySchedule without EndMinute", lockdef.CmdSetWeekDaySchedule, rawRequest(func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.StartStruct(tag)
			spec.PutUint[uint8](enc, tlv.ContextTag(0), 1)
			spec.PutUint[uint16](enc, tlv.ContextTag(1), 1)
			spec.PutUint[uint8](enc, tlv.ContextTag(2), uint8(lockdef.DaysMaskMonday))
			spec.PutUint[uint8](enc, tlv.ContextTag(3), 8)
			spec.PutUint[uint8](enc, tlv.ContextTag(4), 0)
			spec.PutUint[uint8](enc, tlv.ContextTag(5), 9)
			_ = enc.EndContainer()
		})},
	})
}

// TestDoorLockRequestGateRefusalKindsParityMatterJS (F-db021da0-20): the
// three kinds matter.js's model validator refuses before the handler — an
// enum value that is no member
// (packages/node/src/behavior/state/validation/ValueValidator.ts:176-180),
// a member outside the field's member list (OperationType "add, modify",
// door-lock-cluster.element.ts:428; the names become member ids,
// EncodedConstraint.ts:205-212, :236-241, and a list constraint fails when
// every part fails, Constraint.ts:172-191, :207-209), and a bit no member
// defines
// (packages/node/src/behavior/state/validation/ValueValidator.ts:209-225, :307-311;
// DaysMaskBitmap, door-lock-cluster.element.ts:581-588) — answer INVALID_COMMAND for the
// ten refusal commands (DoorLockServer.ts:1494-1511 at db021da0). The
// decoder passes them (decision 64's split: types and constraints at the
// decoder, conformance and enums at the gate); the server's request gate
// (Instance.CheckRequest, answered with the command's stated Refusal)
// refuses them before any handler. A valid request of each command passes.
func TestDoorLockRequestGateRefusalKindsParityMatterJS(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(lockdef.Definition, spec.Options{
		Features: uint32(lockdef.FeaturePinCredential | lockdef.FeatureUser | lockdef.FeatureWeekDayAccessSchedules),
	})
	if err != nil {
		t.Fatal(err)
	}
	// gate is the server's request gate (admitRequest): the check, and a
	// refusal answered with the command's stated Refusal.
	gate := func(t *testing.T, cmd uint32, req spec.Encodable) im.StatusCode {
		t.Helper()
		fields, err := decodeRequest(t, lockdef.ClusterID, cmd, req)
		if err != nil {
			t.Fatalf("the decoder refused the payload: %v", err)
		}
		if err := inst.CheckRequest(cmd, fields, nil); err != nil {
			return lockdef.Definition.Command(cmd, spec.Request).Check.Refusal
		}
		return im.StatusSuccess
	}
	// A valid UserStatus (null): a zero UserStatus is refused on its own
	// ("occupiedEnabled, occupiedDisabled"), which would hide the kind.
	user := func(mod func(*lockdef.SetUserRequest)) lockdef.SetUserRequest {
		return userRequest(func(r *lockdef.SetUserRequest) {
			r.UserStatus = spec.Nullable[lockdef.UserStatusEnum]{Null: true}
			mod(r)
		})
	}
	for _, c := range []struct {
		name  string
		cmd   uint32
		req   spec.Encodable
		valid bool
	}{
		{"SetUser valid", lockdef.CmdSetUser, user(func(*lockdef.SetUserRequest) {}), true},
		{"SetUser CredentialRule 9, no member", lockdef.CmdSetUser, user(func(r *lockdef.SetUserRequest) {
			r.CredentialRule = spec.ValueOf(lockdef.CredentialRuleEnum(9))
		}), false},
		{"SetUser OperationType Clear, outside add, modify", lockdef.CmdSetUser, user(func(r *lockdef.SetUserRequest) {
			r.OperationType = lockdef.DataOperationTypeClear
		}), false},
		{"SetWeekDaySchedule valid", lockdef.CmdSetWeekDaySchedule, weekDayRequest(func(*lockdef.SetWeekDayScheduleRequest) {}), true},
		{"SetWeekDaySchedule DaysMask bit 7", lockdef.CmdSetWeekDaySchedule, weekDayRequest(func(r *lockdef.SetWeekDayScheduleRequest) {
			r.DaysMask = lockdef.DaysMaskBitmap(1 << 7)
		}), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := gate(t, c.cmd, c.req)
			switch {
			case c.valid && got == im.StatusInvalidCommand:
				t.Errorf("a valid request was refused at the gate (INVALID_COMMAND)")
			case !c.valid && got != im.StatusInvalidCommand:
				t.Errorf("status %v, want INVALID_COMMAND", got)
			}
		})
	}
}

// TestRelaxedAttributeIsHeldToItsWidth: a written value of an attribute
// the relaxation table relaxes is held to its type's width only — the
// server judges the rest (LaundryWasherControls NumberOfRinses, owner
// decision 69).
func TestRelaxedAttributeIsHeldToItsWidth(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(lwc.Definition, spec.Options{Features: uint32(lwc.FeatureRinse)})
	if err != nil {
		t.Fatal(err)
	}
	if a := lwc.Definition.Attribute(lwc.AttrNumberOfRinses); a.Relaxed == "" {
		t.Fatal("NumberOfRinses carries no relaxation")
	}
	if _, err := inst.ValidateWrite(lwc.AttrNumberOfRinses, uint64(4), nil); err != nil {
		t.Errorf("NumberOfRinses 4: %v, want it to reach the server", err)
	}
	if _, err := inst.ValidateWrite(lwc.AttrNumberOfRinses, uint64(256), nil); spec.StatusOf(err) != im.StatusConstraintError {
		t.Errorf("NumberOfRinses 256: %v, want CONSTRAINT_ERROR", err)
	}
}
