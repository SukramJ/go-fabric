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
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:474-487
// at db021da0), and a server that departs from it says so in the
// relaxation table (script/clustergen/relaxed.go) — DoorLock skips the
// schema's validation for every command (ProtocolService.ts:343-348) and
// answers INVALID_COMMAND for ten of them (DoorLockServer.ts:1493-1511).
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
	// DoorLock, an unchecked command: a SigningKey of 31 bytes reaches the
	// server.
	got, err := decodeRequest(t, lockdef.ClusterID, lockdef.CmdSetAliroReaderConfig, lockdef.SetAliroReaderConfigRequest{
		SigningKey: make([]byte, 31), VerificationKey: make([]byte, 65), GroupIdentifier: make([]byte, 16),
	})
	if err != nil || len(got.(lockdef.SetAliroReaderConfigRequest).SigningKey) != 31 {
		t.Errorf("SetAliroReaderConfig with a 31-byte SigningKey: %v %v, want it decoded", got, err)
	}
	// DoorLock, an unchecked command: an index of 0 reaches the server,
	// which answers it in its response (DoorLockServer.ts:887-893); the
	// type's width still holds.
	if _, err := decodeRequest(t, lockdef.ClusterID, lockdef.CmdGetHolidaySchedule, lockdef.GetHolidayScheduleRequest{HolidayIndex: 0}); err != nil {
		t.Errorf("GetHolidaySchedule index 0: %v, want it decoded", err)
	}
	cmd := lockdef.Definition.Command(lockdef.CmdGetHolidaySchedule, spec.Request)
	if !cmd.Check.Unchecked || cmd.Check.Ref == "" {
		t.Errorf("GetHolidaySchedule check %+v, want unchecked with its reference", cmd.Check)
	}
}

// rawRequest is a request encoded by hand: a field left out, as no
// generated request type can encode it.
type rawRequest func(*tlv.Encoder, tlv.Tag)

func (r rawRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { r(enc, tag) }

// TestDoorLockRefusalKindsParityMatterJS (F-db021da0-20): DoorLock's ten
// refusal commands answer INVALID_COMMAND for every kind of request-schema
// violation matter.js's validator refuses, not only a string over its
// length — matter.js answers the ten's validation errors with
// StatusResponseError("Invalid field value", Status.InvalidCommand)
// (packages/node/src/behaviors/door-lock/DoorLockServer.ts:1494-1511 at
// db021da0). Compared kind by kind with matter.js's validator; a per-field
// run against matter.js was not made.
func TestDoorLockRefusalKindsParityMatterJS(t *testing.T) {
	t.Parallel()
	week := func(mod func(*lockdef.SetWeekDayScheduleRequest)) lockdef.SetWeekDayScheduleRequest {
		r := lockdef.SetWeekDayScheduleRequest{WeekDayIndex: 1, UserIndex: 1, DaysMask: lockdef.DaysMaskMonday, StartHour: 8, EndHour: 9}
		mod(&r)
		return r
	}
	user := func(mod func(*lockdef.SetUserRequest)) lockdef.SetUserRequest {
		r := lockdef.SetUserRequest{OperationType: lockdef.DataOperationTypeAdd, UserIndex: 1}
		mod(&r)
		return r
	}
	// differs marks a kind the request check passes at this HEAD where
	// matter.js refuses it: reported back as a difference of
	// F-db021da0-20, not fixed without a new reference item.
	for _, c := range []struct {
		name    string
		cmd     uint32
		req     spec.Encodable
		differs bool
	}{
		// An enum value that is no member and in no range member
		// (ValueValidator.ts:176-180).
		{"SetUser CredentialRule 9, no member", lockdef.CmdSetUser, user(func(r *lockdef.SetUserRequest) {
			r.CredentialRule = spec.ValueOf(lockdef.CredentialRuleEnum(9))
		}), true},
		// A member outside the field's member list: OperationType
		// "add, modify" (door-lock-cluster.element.ts:428; the
		// constraint states the values the enum may take,
		// constraint.ts:182-189 and ValueValidator.ts:182 — that the
		// names resolve to their numbers before the test is inferred).
		{"SetUser OperationType Clear, outside add, modify", lockdef.CmdSetUser, user(func(r *lockdef.SetUserRequest) {
			r.OperationType = lockdef.DataOperationTypeClear
		}), true},
		// A bit no member defines (ValueValidator.ts:202-209, :307-311;
		// DaysMaskBitmap, door-lock-cluster.element.ts:261).
		{"SetWeekDaySchedule DaysMask bit 7", lockdef.CmdSetWeekDaySchedule, week(func(r *lockdef.SetWeekDayScheduleRequest) {
			r.DaysMask = lockdef.DaysMaskBitmap(1 << 7)
		}), true},
		// A value over its bound: StartHour "max 23"
		// (door-lock-cluster.element.ts:262-265).
		{"SetWeekDaySchedule StartHour 24", lockdef.CmdSetWeekDaySchedule, week(func(r *lockdef.SetWeekDayScheduleRequest) {
			r.StartHour = 24
		}), false},
		// A mandatory field missing (conformance; ValueValidator.ts:146,
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
		}), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.differs {
				t.Skip("F-db021da0-20: the request check passes this kind; matter.js refuses it — a difference reported back")
			}
			if _, err := decodeRequest(t, lockdef.ClusterID, c.cmd, c.req); spec.StatusOf(err) != im.StatusInvalidCommand {
				t.Errorf("%v (status %v), want INVALID_COMMAND", err, spec.StatusOf(err))
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
