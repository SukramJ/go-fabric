// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// decodeDoorLockFields decodes a DoorLock request through its generated
// definition with the two request-validation rules matter.js's
// DoorLockServer installs. Mirrors matter.js
// packages/node/src/behaviors/door-lock/DoorLockServer.ts:1491-1521 at
// 862ad75f:
//
//   - for SetUser, GetUser, ClearUser, SetCredential and the Set and Clear
//     commands of the three schedule kinds a payload the request's schema
//     refuses answers INVALID_COMMAND (Supervision onValidationError =
//     throwInvalidCommand, :1494-1511) — the schema's own status is
//     CONSTRAINT_ERROR for a value outside its constraint;
//   - GetWeekDaySchedule, GetYearDaySchedule and GetHolidaySchedule check
//     their index fields themselves and answer an index out of range in
//     their response (:1513-1521, constraint = false), so the constraint
//     of those fields is not applied here; the type's width still is.
func decodeDoorLockFields(cmdID uint32, dec *tlv.Decoder, el tlv.Element) (any, error) {
	cmd := lockdef.Definition.Command(cmdID, spec.Request)
	if cmd == nil || cmd.Decode == nil {
		return decodeGenericTagMap(dec)
	}
	n, err := spec.ReadContainer(dec, el)
	if err != nil {
		return nil, fmt.Errorf("DoorLock.%s: %w", cmd.Name, err)
	}
	fields, err := cmd.Decode(n)
	var fe *spec.FieldError
	if err == nil || !errors.As(err, &fe) {
		return fields, err
	}
	switch cmdID {
	case lockdef.CmdGetWeekDaySchedule, lockdef.CmdGetYearDaySchedule, lockdef.CmdGetHolidaySchedule:
		if lenient, ok := decodeScheduleQuery(cmdID, n); ok {
			return lenient, nil
		}
	case lockdef.CmdSetUser, lockdef.CmdGetUser, lockdef.CmdClearUser, lockdef.CmdSetCredential,
		lockdef.CmdSetWeekDaySchedule, lockdef.CmdClearWeekDaySchedule,
		lockdef.CmdSetYearDaySchedule, lockdef.CmdClearYearDaySchedule,
		lockdef.CmdSetHolidaySchedule, lockdef.CmdClearHolidaySchedule:
		return nil, &spec.FieldError{Status: im.StatusInvalidCommand, Msg: fe.Msg}
	}
	return nil, err
}

// decodeScheduleQuery reads a Get…Schedule request without its index
// constraints; ok is false when a field is missing or wider than its type.
func decodeScheduleQuery(cmdID uint32, n spec.Node) (any, bool) {
	vals := map[uint64]uint64{}
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && !c.El.IsContainer && !c.El.IsNull {
			vals[uint64(c.El.Tag.Number)] = c.El.Uint
		}
	}
	u8 := func(tag uint64) (uint8, bool) {
		v, ok := vals[tag]
		return uint8(v), ok && v <= 0xFF //nolint:gosec // bounded by the check
	}
	u16 := func(tag uint64) (uint16, bool) {
		v, ok := vals[tag]
		return uint16(v), ok && v <= 0xFFFF //nolint:gosec // bounded by the check
	}
	switch cmdID {
	case lockdef.CmdGetWeekDaySchedule:
		idx, ok1 := u8(0)
		user, ok2 := u16(1)
		return lockdef.GetWeekDayScheduleRequest{WeekDayIndex: idx, UserIndex: user}, ok1 && ok2
	case lockdef.CmdGetYearDaySchedule:
		idx, ok1 := u8(0)
		user, ok2 := u16(1)
		return lockdef.GetYearDayScheduleRequest{YearDayIndex: idx, UserIndex: user}, ok1 && ok2
	default:
		idx, ok := u8(0)
		return lockdef.GetHolidayScheduleRequest{HolidayIndex: idx}, ok
	}
}
