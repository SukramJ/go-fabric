// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// MatterScheduleConfiguration, the write: a port of the write and atomic
// cases of connectedhomeip
// src/app/clusters/thermostat-server/tests/TestThermostatClusterSchedules.cpp
// at 645b267a, over the wire: chip's ClusterTester Invoke and
// WriteAttribute are a fabrictest pair's AtomicRequest and write, chip's
// delegate the SchedulePort, mPendingSchedules what the writing peer reads
// back, mSchedules the server's Schedules(). The fixture is mschConfig
// (schedules_chip_test.go).
//
// The Schedules attribute's schema is checked before chip's rules, as for
// every write (the definition, matter.js ValueValidator): an oversized
// handle, name or preset handle, an empty transition list, a weekday
// outside the bitmap, a TransitionTime beyond 1439 and an unknown
// SystemMode are CONSTRAINT_ERROR there, the status chip answers too.
//
// Not ported, each for a reason:
//   - AppendPendingScheduleRejectsTooManyTransitionsHardCap (:646) — chip's
//     owned-storage capacity (kScheduleTransitionsMax 16), not a rule of
//     the specification; here NumberOfScheduleTransitions bounds a schedule.
//   - the delegate-error cases — PrecommitFailsWhenGetScheduleAtIndexErrors
//     (:838), PrecommitFailsWhenMatchingPendingScheduleLookupErrors (:853),
//     AppendPendingScheduleFailsWhenScheduleTypeLookupErrors (:872),
//     AppendPendingScheduleReportsDelegateErrorFromNameSupportLookup (:891),
//     AppendPendingSchedulePropagatesPresetLookupError (:1026),
//     PrecommitPresetsFailsWhenScheduleInUseLookupErrors (:1067): the
//     schedules, the pending list and the presets are the server's state,
//     read without a port that could fail.
package thermo_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

var schedulesOnly = []uint32{tdef.AttrSchedules}

// newSchedule is chip's MakeSchedule (:52-63) with a null handle and
// BuiltIn: a new Heat schedule with one transition.
func newSchedule(transitions ...thermo.ScheduleTransition) thermo.Schedule {
	if len(transitions) == 0 {
		transitions = []thermo.ScheduleTransition{heatTransition(0, tdef.ScheduleDayOfWeekMonday)}
	}
	return thermo.Schedule{
		ScheduleHandle: spec.NullOf[[]byte](), SystemMode: tdef.SystemModeHeat,
		Transitions: transitions, BuiltIn: spec.NullOf[bool](),
	}
}

// schedulesThermostat commissions the chip fixture with cfg's edits.
func schedulesThermostat(t *testing.T, edit ...func(*thermo.ThermostatConfig)) *thermostatPair {
	t.Helper()
	return commissionedThermostat(t, func(*fabrictest.Site) thermo.ThermostatConfig { return mschConfig(edit...) })
}

// beginSchedules opens an atomic write of attrs (Schedules by default).
func (p *thermostatPair) beginSchedules(t *testing.T, q *fabrictest.Pair, attrs ...uint32) {
	t.Helper()
	if len(attrs) == 0 {
		attrs = schedulesOnly
	}
	r, err := p.atomicRequest(parityCtx(t), q, beginWrite, attrs, timeoutMs(3000))
	if err != nil {
		t.Fatal(err)
	}
	if resp := decodeAtomicResponse(t, r); resp.StatusCode != tdef.StatusSuccess {
		t.Fatalf("BeginWrite answered %+v", resp)
	}
}

// endSchedules sends a CommitWrite or RollbackWrite of attrs (Schedules by
// default) and returns the AtomicResponse.
func (p *thermostatPair) endSchedules(t *testing.T, requestType uint8, attrs ...uint32) tdef.AtomicResponse {
	t.Helper()
	if len(attrs) == 0 {
		attrs = schedulesOnly
	}
	r, err := p.atomicRequest(parityCtx(t), p.pair, requestType, attrs, nil)
	if err != nil {
		t.Fatal(err)
	}
	return decodeAtomicResponse(t, r)
}

// writeSchedules writes Schedules from q and returns the first status
// other than SUCCESS, SUCCESS when every chunk succeeded.
func (p *thermostatPair) writeSchedules(t *testing.T, q *fabrictest.Pair, list []thermo.Schedule) im.StatusCode {
	t.Helper()
	ctx := parityCtx(t)
	ia, err := fabrictest.Connect(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	var statuses interaction.WriteStatuses
	err = p.site.Run(ctx, func(ctx context.Context) error {
		var werr error
		statuses, werr = ia.Write(ctx, []interaction.WriteRequest{{Path: p.attrPath(tdef.AttrSchedules), Value: spec.List[thermo.Schedule](list)}}, interaction.WriteOptions{})
		return werr
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statuses {
		if s.Status != im.StatusSuccess {
			return s.Status
		}
	}
	return im.StatusSuccess
}

// readSchedules reads Schedules as q.
func (p *thermostatPair) readSchedules(t *testing.T, q *fabrictest.Pair) []thermo.Schedule {
	t.Helper()
	d, err := fabrictest.Read(parityCtx(t), q, p.attrPath(tdef.AttrSchedules))
	if err != nil {
		t.Fatal(err)
	}
	v, err := tdef.Definition.Attribute(tdef.AttrSchedules).Decode(d.Value)
	if err != nil {
		t.Fatal(err)
	}
	return []thermo.Schedule(v.(spec.List[thermo.Schedule]))
}

func wantWrite(t *testing.T, got, want im.StatusCode) {
	t.Helper()
	if got != want {
		t.Errorf("write status %s, want %s", got, want)
	}
}

func wantCommit(t *testing.T, got tdef.AtomicResponse, command, attr tdef.Status) {
	t.Helper()
	if got.StatusCode != command || len(got.AttributeStatus) != 1 || got.AttributeStatus[0].StatusCode != attr {
		t.Errorf("AtomicResponse %+v, want command %d, Schedules %d", got, command, attr)
	}
}

func TestChipThermostatSchedulesWrite(t *testing.T) {
	t.Run("WriteToSchedulesOutsideAtomicWriteIsRejected (:199)", func(t *testing.T) {
		p := schedulesThermostat(t)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusInvalidInState)
	})

	t.Run("BeginWriteSeedsPendingSchedulesFromExistingList (:214)", func(t *testing.T) {
		p := schedulesThermostat(t, withSchedules(builtInSchedule(0x01)))
		p.beginSchedules(t, p.pair)
		pending := p.readSchedules(t, p.pair)
		if len(pending) != 1 || pending[0].SystemMode != tdef.SystemModeHeat || !bytes.Equal(pending[0].ScheduleHandle.Value, []byte{0x01}) {
			t.Errorf("pending %+v, want the built-in schedule 01", pending)
		}
	})

	t.Run("PrecommitFailsWhenBuiltInScheduleIsRemoved (:236)", func(t *testing.T) {
		p := schedulesThermostat(t, withSchedules(builtInSchedule(0x01)))
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{}), im.StatusSuccess)
		wantCommit(t, p.endSchedules(t, commitWrite), tdef.StatusFailure, tdef.StatusConstraintError)
		if got := p.srv.Schedules(); len(got) != 1 {
			t.Errorf("schedules %+v after a refused commit, want the built-in one", got)
		}
	})

	t.Run("PrecommitSucceedsWhenBuiltInScheduleIsPreserved (:254)", func(t *testing.T) {
		p := schedulesThermostat(t, withSchedules(builtInSchedule(0x01)))
		p.beginSchedules(t, p.pair)
		wantCommit(t, p.endSchedules(t, commitWrite), tdef.StatusSuccess, tdef.StatusSuccess)
	})

	t.Run("AppendPendingScheduleSucceedsAndAddsToPendingList (:271)", func(t *testing.T) {
		// chip's test delegate issues the handle on append
		// (ThermostatTestCommon.h:682-691); the server issues it on commit
		// (issueScheduleHandles), so the pending entry keeps its null handle.
		p := schedulesThermostat(t)
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusSuccess)
		if pending := p.readSchedules(t, p.pair); len(pending) != 1 || pending[0].BuiltIn.Null || pending[0].BuiltIn.Value {
			t.Errorf("pending %+v, want one schedule, BuiltIn false", pending)
		}
	})

	for _, c := range []struct {
		name string
		edit func(*thermo.Schedule)
	}{
		{"AppendPendingScheduleRejectsOversizedHandle (:293)", func(s *thermo.Schedule) { s.ScheduleHandle = spec.ValueOf(make([]byte, 17)) }},
		{"AppendPendingScheduleRejectsOversizedName (:312)", func(s *thermo.Schedule) { n := strings.Repeat("a", 65); s.Name = &n }},
		{"AppendPendingScheduleRejectsOversizedPresetHandle (:603)", func(s *thermo.Schedule) { h := make([]byte, 17); s.PresetHandle = &h }},
		{"AppendPendingScheduleRejectsOversizedTransitionPresetHandle (:624)", func(s *thermo.Schedule) {
			h := make([]byte, 17)
			s.Transitions[0].PresetHandle = &h
		}},
		{"AppendPendingScheduleRejectsInvalidSystemMode (:672)", func(s *thermo.Schedule) { s.SystemMode = 0xFF }},
		{"AppendPendingScheduleRejectsBuiltInOnNewSchedule (:692)", func(s *thermo.Schedule) { s.BuiltIn = spec.ValueOf(true) }},
		{"AppendPendingScheduleRejectsUnsupportedSystemMode (:711)", func(s *thermo.Schedule) { s.SystemMode = tdef.SystemModeCool }},
		{"AppendPendingScheduleRejectsEmptyTransitions (:915)", func(s *thermo.Schedule) { s.Transitions = []thermo.ScheduleTransition{} }},
		{"AppendPendingScheduleRejectsInvalidDayOfWeek (:935)", func(s *thermo.Schedule) { s.Transitions[0].DayOfWeek |= tdef.ScheduleDayOfWeekAway }},
		{"AppendPendingScheduleRejectsInvalidTransitionTime (:956)", func(s *thermo.Schedule) { s.Transitions[0].TransitionTime = 1440 }},
		{"AppendPendingScheduleRejectsDuplicateTransitions (:975)", func(s *thermo.Schedule) {
			t1 := heatTransition(480, tdef.ScheduleDayOfWeekMonday|tdef.ScheduleDayOfWeekTuesday)
			t2 := heatTransition(480, tdef.ScheduleDayOfWeekTuesday|tdef.ScheduleDayOfWeekWednesday)
			s.Transitions = []thermo.ScheduleTransition{t1, t2}
		}},
		{"AppendPendingScheduleRejectsMissingOrOutOfRangeSetpoint (:998) missing", func(s *thermo.Schedule) { s.Transitions[0].HeatingSetpoint = nil }},
		{"AppendPendingScheduleRejectsMissingOrOutOfRangeSetpoint (:998) out of range", func(s *thermo.Schedule) {
			v := int16(5000)
			s.Transitions[0].HeatingSetpoint = &v
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := schedulesThermostat(t)
			p.beginSchedules(t, p.pair)
			sc := newSchedule()
			c.edit(&sc)
			wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{sc}), im.StatusConstraintError)
		})
	}

	t.Run("AppendPendingScheduleRejectsWhenTotalTransitionsExceedMaximum (:333)", func(t *testing.T) {
		// Five transitions exceed NumberOfScheduleTransitions 4, in one
		// schedule as across two: RESOURCE_EXHAUSTED (:986-991). The schema's
		// "1 to numberOfScheduleTransitions" names an attribute the
		// definition does not resolve, so chip's count answers.
		p := schedulesThermostat(t, func(cfg *thermo.ThermostatConfig) { cfg.NumberOfScheduleTransitionPerDay = nil })
		p.beginSchedules(t, p.pair)
		days := []tdef.ScheduleDayOfWeekBitmap{
			tdef.ScheduleDayOfWeekSunday, tdef.ScheduleDayOfWeekMonday, tdef.ScheduleDayOfWeekTuesday,
			tdef.ScheduleDayOfWeekWednesday, tdef.ScheduleDayOfWeekThursday,
		}
		var five []thermo.ScheduleTransition
		for _, d := range days {
			five = append(five, heatTransition(0, d))
		}
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule(five...)}), im.StatusResourceExhausted)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule(five[:3]...), newSchedule(five[3:]...)}), im.StatusResourceExhausted)
	})

	t.Run("AppendPendingScheduleRejectsWhenPerDayTransitionLimitExceeded (:360)", func(t *testing.T) {
		p := schedulesThermostat(t)
		p.beginSchedules(t, p.pair)
		three := newSchedule(heatTransition(0, tdef.ScheduleDayOfWeekMonday), heatTransition(100, tdef.ScheduleDayOfWeekMonday), heatTransition(200, tdef.ScheduleDayOfWeekMonday))
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{three}), im.StatusResourceExhausted)
	})

	t.Run("ValidateTransitionsPerDaySkippedWhenNoLimitConfigured (:732)", func(t *testing.T) {
		p := schedulesThermostat(t, func(cfg *thermo.ThermostatConfig) { cfg.NumberOfScheduleTransitionPerDay = nil })
		p.beginSchedules(t, p.pair)
		three := newSchedule(heatTransition(0, tdef.ScheduleDayOfWeekMonday), heatTransition(100, tdef.ScheduleDayOfWeekMonday), heatTransition(200, tdef.ScheduleDayOfWeekMonday))
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{three}), im.StatusSuccess)
	})

	t.Run("AppendPendingScheduleRejectsUnknownScheduleHandle (:384)", func(t *testing.T) {
		p := schedulesThermostat(t)
		p.beginSchedules(t, p.pair)
		sc := newSchedule()
		sc.ScheduleHandle = spec.ValueOf([]byte{0xFF})
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{sc}), im.StatusNotFound)
	})

	t.Run("AppendPendingScheduleRejectsBuiltInMismatch (:404)", func(t *testing.T) {
		p := schedulesThermostat(t, withSchedules(builtInSchedule(0x01)))
		p.beginSchedules(t, p.pair)
		sc := newSchedule()
		sc.ScheduleHandle, sc.BuiltIn = spec.ValueOf([]byte{0x01}), spec.ValueOf(false)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{sc}), im.StatusConstraintError)
	})

	t.Run("AppendPendingScheduleRejectsDuplicatePendingHandle (:426)", func(t *testing.T) {
		p := schedulesThermostat(t, withSchedules(builtInSchedule(0x01)))
		p.beginSchedules(t, p.pair)
		sc := newSchedule()
		sc.ScheduleHandle = spec.ValueOf([]byte{0x01})
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{sc, sc}), im.StatusConstraintError)
	})

	t.Run("AppendPendingScheduleRejectsWhenScheduleCountExceedsMaximum (:451)", func(t *testing.T) {
		p := schedulesThermostat(t)
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule(), newSchedule(), newSchedule()}), im.StatusResourceExhausted)
	})

	t.Run("FullAtomicWriteRoundTripAppendsAndCommitsNewSchedule (:471)", func(t *testing.T) {
		port := &recordingSchedulePort{}
		p := schedulesThermostat(t, func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port })
		changes := recordThermostatChanges(p.srv)
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusSuccess)
		wantCommit(t, p.endSchedules(t, commitWrite), tdef.StatusSuccess, tdef.StatusSuccess)
		got := p.srv.Schedules()
		if len(got) != 1 || got[0].ScheduleHandle.Null || len(got[0].ScheduleHandle.Value) != 16 {
			t.Fatalf("schedules %+v, want one with an issued 16-byte handle", got)
		}
		if commits := port.commitCalls(); len(commits) != 1 || !bytes.Equal(commits[0][0].ScheduleHandle.Value, got[0].ScheduleHandle.Value) {
			t.Errorf("port commits %+v, want the stored list", commits)
		}
		if changes.count(tdef.AttrSchedules) == 0 {
			t.Error("Schedules not reported after the commit")
		}
	})

	t.Run("RollbackWriteClearsPendingScheduleList (:495)", func(t *testing.T) {
		port := &recordingSchedulePort{}
		p := schedulesThermostat(t, withSchedules(builtInSchedule(0x01)), func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port })
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{builtInSchedule(0x01), newSchedule()}), im.StatusSuccess)
		if r := p.endSchedules(t, rollbackWrite); r.StatusCode != tdef.StatusSuccess {
			t.Fatalf("RollbackWrite answered %+v", r)
		}
		if got := p.srv.Schedules(); len(got) != 1 {
			t.Errorf("schedules %+v after the rollback, want the stored one", got)
		}
		if n := len(port.commitCalls()); n != 0 {
			t.Errorf("the rollback reached the port %d times", n)
		}
	})

	t.Run("ReadSchedulesDuringAtomicWriteReturnsPendingList (:533)", func(t *testing.T) {
		p := schedulesThermostat(t)
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusSuccess)
		if pending := p.readSchedules(t, p.pair); len(pending) != 1 {
			t.Errorf("pending %+v, want one schedule", pending)
		}
		if got := p.srv.Schedules(); len(got) != 0 {
			t.Errorf("stored schedules %+v during the write, want none", got)
		}
	})

	t.Run("WriteToSchedulesFromDifferentNodeDuringAtomicWriteIsInvalidInState (:558)", func(t *testing.T) {
		p := schedulesThermostat(t)
		p.beginSchedules(t, p.pair)
		q := p.secondPeer(t)
		wantWrite(t, p.writeSchedules(t, q, []thermo.Schedule{newSchedule()}), im.StatusInvalidInState)
	})

	t.Run("AppendItemListOperationAppendsSingleSchedule (:581)", func(t *testing.T) {
		p := schedulesThermostat(t)
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{}), im.StatusSuccess)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule(), newSchedule()}), im.StatusSuccess)
		if pending := p.readSchedules(t, p.pair); len(pending) != 2 {
			t.Errorf("pending %+v, want two schedules", pending)
		}
	})

	t.Run("PrecommitFailsWhenActiveScheduleHandleNotInPendingList (:758)", func(t *testing.T) {
		// chip sets an active handle that names no schedule; the server keeps
		// none such, so the active schedule is a stored one the write drops.
		removable := builtInSchedule(0x05)
		removable.BuiltIn = spec.ValueOf(false)
		p := schedulesThermostat(t, withSchedules(removable), func(cfg *thermo.ThermostatConfig) { cfg.ActiveScheduleHandle = []byte{0x05} })
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{}), im.StatusSuccess)
		wantCommit(t, p.endSchedules(t, commitWrite), tdef.StatusFailure, tdef.StatusInvalidInState)
	})

	t.Run("CommitFailsWhenDelegateCommitReturnsError (:821)", func(t *testing.T) {
		port := &recordingSchedulePort{commitErr: errors.New("device storage full")}
		p := schedulesThermostat(t, func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port })
		changes := recordThermostatChanges(p.srv)
		p.beginSchedules(t, p.pair)
		wantWrite(t, p.writeSchedules(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusSuccess)
		wantCommit(t, p.endSchedules(t, commitWrite), tdef.StatusFailure, tdef.StatusFailure)
		if got := p.srv.Schedules(); len(got) != 0 {
			t.Errorf("schedules %+v after a refused commit", got)
		}
		if changes.count(tdef.AttrSchedules) != 0 {
			t.Error("Schedules reported after a refused commit (chip IsAttributeDirty false)")
		}
	})
}
