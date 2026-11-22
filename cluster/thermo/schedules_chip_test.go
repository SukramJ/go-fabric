// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// MatterScheduleConfiguration, the cases of connectedhomeip
// src/app/clusters/thermostat-server/tests/TestThermostatClusterSchedules.cpp
// at 645b267a that matter.js's three Schedules suites at b25ed960 have no
// counterpart for (schedules_*_parity_matterjs_test.go hold the rules
// since matter.js implements the feature behind its forward feature
// thermostat-msch, ThermostatServer.ts:267-271): the attributes and
// commands served, the counts and ScheduleTypes read, the SchedulePort's
// answers, the pending list a peer reads during its write, the field
// sizes, and a second peer's write. The chip line numbers are this port's
// earlier reading at 645b267a, not re-read at the pin. chip's fixture
// (:37-98) is a HEAT+MSCH thermostat with NumberOfSchedules 2,
// NumberOfScheduleTransitions 4, NumberOfScheduleTransitionPerDay 2 and
// one schedule type, Heat, of two schedules supporting names and
// setpoints; its delegate is the SchedulePort, its SeedBuiltInSchedule a
// configured schedule — with one transition, which the Schedules
// attribute's "1 to numberOfScheduleTransitions" requires of a schedule
// the device supplies.
//
// Not ported: SetActiveScheduleRequestPropagatesDelegateGetError (:796) —
// the active handle is the server's state, not read through the port —
// and AppendPendingScheduleRejectsTooManyTransitionsHardCap (:646), chip's
// owned-storage capacity rather than a rule.
package thermo_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/fabrictestseam"
)

// mschConfig is chip's ThermostatSchedulesTestFixture (:84-98) on the
// module's default limits.
func mschConfig(edit ...func(*thermo.ThermostatConfig)) thermo.ThermostatConfig {
	cfg := thermo.DefaultThermostatConfig()
	cfg.Features = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureMSCH
	perDay := uint8(2)
	cfg.ScheduleTypes = []thermo.ScheduleType{{
		SystemMode: tdef.SystemModeHeat, NumberOfSchedules: 2,
		ScheduleTypeFeatures: tdef.ScheduleTypeFeaturesSupportsNames | tdef.ScheduleTypeFeaturesSupportsSetpoints,
	}}
	cfg.NumberOfSchedules, cfg.NumberOfScheduleTransitions, cfg.NumberOfScheduleTransitionPerDay = 2, 4, &perDay
	for _, e := range edit {
		e(&cfg)
	}
	return cfg
}

// heatTransition is chip's MakeScheduleTransition (:43-50): heating to
// 20.00 °C at minute on day.
func heatTransition(minute uint16, day tdef.ScheduleDayOfWeekBitmap) thermo.ScheduleTransition {
	heat := int16(2000)
	return thermo.ScheduleTransition{DayOfWeek: day, TransitionTime: minute, HeatingSetpoint: &heat}
}

// builtInSchedule is chip's SeedBuiltInSchedule (ThermostatTestCommon.h:712-722)
// with handle {b}, and one transition.
func builtInSchedule(b byte) thermo.Schedule {
	return thermo.Schedule{
		ScheduleHandle: spec.ValueOf([]byte{b}), SystemMode: tdef.SystemModeHeat,
		Transitions: []thermo.ScheduleTransition{heatTransition(0, tdef.ScheduleDayOfWeekMonday)},
		BuiltIn:     spec.ValueOf(true),
	}
}

// withSchedules seeds the configured schedules.
func withSchedules(list ...thermo.Schedule) func(*thermo.ThermostatConfig) {
	return func(cfg *thermo.ThermostatConfig) { cfg.Schedules = list }
}

func newMSCH(t *testing.T, cfg thermo.ThermostatConfig) *thermo.ThermostatServer {
	t.Helper()
	srv, err := thermo.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// recordingSchedulePort records the port's calls and refuses each with
// setErr / commitErr.
type recordingSchedulePort struct {
	mu        sync.Mutex
	active    [][]byte
	commits   [][]thermo.Schedule
	setErr    error
	commitErr error
}

func (r *recordingSchedulePort) SetActiveSchedule(_ context.Context, handle []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = append(r.active, handle)
	return r.setErr
}

func (r *recordingSchedulePort) CommitSchedules(_ context.Context, schedules []thermo.Schedule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commits = append(r.commits, schedules)
	return r.commitErr
}

func (r *recordingSchedulePort) activeCalls() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.active)
}

func (r *recordingSchedulePort) commitCalls() [][]thermo.Schedule {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.commits)
}

// eventRecorder is a contract.EventEmitter that keeps every event.
type eventRecorder struct {
	mu     sync.Mutex
	events []uint32
}

func (e *eventRecorder) MatterEmitEvent(_ uint16, _, event uint32, _ any, _ contract.EventPriority) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func invokeSetActiveSchedule(srv *thermo.ThermostatServer, handle []byte) error {
	ctx := im.WithCommandPath(context.Background(), im.ConcreteCommandPath{
		Endpoint: 1, Cluster: tdef.ClusterID, Command: tdef.CmdSetActiveScheduleRequest, HasEndpoint: true,
	})
	_, err := srv.MatterInvoke(ctx, tdef.CmdSetActiveScheduleRequest, tdef.SetActiveScheduleRequestRequest{ScheduleHandle: handle})
	return err
}

func statusOfErr(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sc im.StatusCodeError
	if !errors.As(err, &sc) {
		t.Fatalf("error %v carries no IM status", err)
	}
	return sc.MatterStatusCode()
}

// TestChipSchedulesAttributePresentWhenFeatureEnabled ports
// SchedulesAttributePresentWhenFeatureEnabled (:101-114), with the rest of
// the MSCH surface: the six attributes and SetActiveScheduleRequest and
// AtomicRequest accepted.
func TestChipSchedulesAttributePresentWhenFeatureEnabled(t *testing.T) {
	t.Parallel()
	srv := newMSCH(t, mschConfig())
	attrs := srv.MatterAttributes()
	for _, id := range []uint32{
		tdef.AttrScheduleTypes, tdef.AttrNumberOfSchedules, tdef.AttrNumberOfScheduleTransitions,
		tdef.AttrNumberOfScheduleTransitionPerDay, tdef.AttrActiveScheduleHandle, tdef.AttrSchedules,
	} {
		if !slices.Contains(attrs, id) {
			t.Errorf("attribute 0x%04X not in AttributeList", id)
		}
	}
	accepted := srv.MatterAcceptedCommands()
	for _, id := range []uint32{tdef.CmdSetActiveScheduleRequest, tdef.CmdAtomicRequest} {
		if !slices.Contains(accepted, id) {
			t.Errorf("command 0x%02X not accepted: %v", id, accepted)
		}
	}
}

// TestChipReadNumberOfSchedulesAndTransitions ports
// ReadNumberOfSchedulesAndTransitions (:116-137); a null per-day limit
// reads null.
func TestChipReadNumberOfSchedulesAndTransitions(t *testing.T) {
	t.Parallel()
	srv := newMSCH(t, mschConfig())
	for id, want := range map[uint32]any{
		tdef.AttrNumberOfSchedules: uint8(2), tdef.AttrNumberOfScheduleTransitions: uint8(4), tdef.AttrNumberOfScheduleTransitionPerDay: uint8(2),
	} {
		if v, _ := srv.MatterRead(id); v != want {
			t.Errorf("attribute 0x%04X = %v, want %v", id, v, want)
		}
	}
	unlimited := newMSCH(t, mschConfig(func(cfg *thermo.ThermostatConfig) { cfg.NumberOfScheduleTransitionPerDay = nil }))
	if v, ok := unlimited.MatterRead(tdef.AttrNumberOfScheduleTransitionPerDay); !ok || v != nil {
		t.Errorf("NumberOfScheduleTransitionPerDay = %v, %v; want null", v, ok)
	}
}

// TestChipActiveScheduleHandleDefaultsToNull ports
// ActiveScheduleHandleDefaultsToNull (:139-151).
func TestChipActiveScheduleHandleDefaultsToNull(t *testing.T) {
	t.Parallel()
	srv := newMSCH(t, mschConfig())
	if v, ok := srv.MatterRead(tdef.AttrActiveScheduleHandle); !ok || v != nil {
		t.Errorf("ActiveScheduleHandle = %v, %v; want null", v, ok)
	}
}

// TestChipSetActiveScheduleRequestSucceedsAndGeneratesEvent ports
// SetActiveScheduleRequestSucceedsAndGeneratesEvent (:170-197): the port
// switches the device, the handle is stored and reported (chip's
// IsAttributeDirty). The test enables Events (TEVT), which this server
// does not serve: without it chip generates no event either
// (ThermostatClusterEvents.cpp:189-195), so ActiveScheduleChange stays out
// of the EventList and nothing is emitted.
func TestChipSetActiveScheduleRequestSucceedsAndGeneratesEvent(t *testing.T) {
	t.Parallel()
	port := &recordingSchedulePort{}
	srv := newMSCH(t, mschConfig(withSchedules(builtInSchedule(0x01)), func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port }))
	events := &eventRecorder{}
	srv.SetMatterEventEmitter(events)
	rec := recordHoldChanges(t, srv)
	if err := invokeSetActiveSchedule(srv, []byte{0x01}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(tdef.AttrActiveScheduleHandle); !bytes.Equal(v.([]byte), []byte{0x01}) {
		t.Errorf("ActiveScheduleHandle = %v, want 01", v)
	}
	if got := port.activeCalls(); len(got) != 1 || !bytes.Equal(got[0], []byte{0x01}) {
		t.Errorf("port calls %v, want [01]", got)
	}
	if got := rec.attrs(contract.OriginHostSet); !slices.Contains(got, tdef.AttrActiveScheduleHandle) {
		t.Errorf("ActiveScheduleHandle not reported: %v", got)
	}
	events.mu.Lock()
	n := len(events.events)
	events.mu.Unlock()
	if n != 0 {
		t.Errorf("%d events emitted without TEVT", n)
	}
}

// TestChipSetActiveScheduleRequestPropagatesDelegateSetError ports
// SetActiveScheduleRequestPropagatesDelegateSetError (:776-794): the port's
// refusal answers the command (its status, FAILURE without one) and the
// handle stays.
func TestChipSetActiveScheduleRequestPropagatesDelegateSetError(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		err  error
		want im.StatusCode
	}{
		{"plain error is FAILURE", errors.New("radio timeout"), im.StatusFailure},
		{"status passes through", spec.Errorf(im.StatusBusy, "device busy"), im.StatusBusy},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			port := &recordingSchedulePort{setErr: c.err}
			srv := newMSCH(t, mschConfig(withSchedules(builtInSchedule(0x01)), func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port }))
			if got := statusOfErr(t, invokeSetActiveSchedule(srv, []byte{0x01})); got != c.want {
				t.Errorf("status %s, want %s", got, c.want)
			}
			if h := srv.ActiveScheduleHandle(); h != nil {
				t.Errorf("ActiveScheduleHandle = %x after a refusal, want null", h)
			}
		})
	}
}

// TestChipReadScheduleTypesReturnsConfiguredTypes ports
// ReadScheduleTypesReturnsConfiguredTypes (:515-531).
func TestChipReadScheduleTypesReturnsConfiguredTypes(t *testing.T) {
	t.Parallel()
	srv := newMSCH(t, mschConfig())
	v, _ := srv.MatterRead(tdef.AttrScheduleTypes)
	types, _ := v.(spec.List[thermo.ScheduleType])
	if len(types) != 1 || types[0].SystemMode != tdef.SystemModeHeat {
		t.Errorf("ScheduleTypes = %+v, want one Heat type", v)
	}
}

// chipSchedule is chip's MakeSchedule (:52-63) with a null handle and
// BuiltIn: a new Heat schedule with one transition.
func chipSchedule(transitions ...thermo.ScheduleTransition) thermo.Schedule {
	if len(transitions) == 0 {
		transitions = []thermo.ScheduleTransition{heatTransition(0, tdef.ScheduleDayOfWeekMonday)}
	}
	return thermo.Schedule{
		ScheduleHandle: spec.NullOf[[]byte](), SystemMode: tdef.SystemModeHeat,
		Transitions: transitions, BuiltIn: spec.NullOf[bool](),
	}
}

// chipThermostat commissions the chip fixture with cfg's edits.
func chipThermostat(t *testing.T, edit ...func(*thermo.ThermostatConfig)) *thermostatPair {
	t.Helper()
	return commissionedThermostat(t, func(*fabrictest.Site) thermo.ThermostatConfig { return mschConfig(edit...) })
}

// chipWrite writes Schedules from q and returns the first status other
// than SUCCESS, SUCCESS when every chunk succeeded.
func (p *thermostatPair) chipWrite(t *testing.T, q *fabrictest.Pair, list []thermo.Schedule) im.StatusCode {
	t.Helper()
	ctx := parityCtx(t)
	ia, err := fabrictestseam.Connect(ctx, q)
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

// TestChipThermostatSchedulesWrite holds the write cases of chip's suite
// that matter.js's suites do not name.
func TestChipThermostatSchedulesWrite(t *testing.T) {
	t.Run("BeginWriteSeedsPendingSchedulesFromExistingList (:214)", func(t *testing.T) {
		// The peer holding the write reads the stored list until it writes
		// (matter.js's schedules getter falls back to the stored value
		// without a pending one, ThermostatServer.ts:3249-3261).
		p := chipThermostat(t, withSchedules(builtInSchedule(0x01)))
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		pending := p.readSchedules(t, p.pair)
		if len(pending) != 1 || pending[0].SystemMode != tdef.SystemModeHeat || !bytes.Equal(pending[0].ScheduleHandle.Value, []byte{0x01}) {
			t.Errorf("pending %+v, want the built-in schedule 01", pending)
		}
	})

	t.Run("PrecommitSucceedsWhenBuiltInScheduleIsPreserved (:254)", func(t *testing.T) {
		p := chipThermostat(t, withSchedules(builtInSchedule(0x01)))
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		wantCommit(t, p.atomic(t, p.pair, commitWrite, schedulesAttribute), tdef.StatusSuccess, tdef.StatusSuccess)
		if got := p.srv.Schedules(); len(got) != 1 {
			t.Errorf("schedules %+v, want the built-in one", got)
		}
	})

	for _, c := range []struct {
		name string
		edit func(*thermo.Schedule)
	}{
		{"AppendPendingScheduleRejectsOversizedHandle (:293)", func(s *thermo.Schedule) { s.ScheduleHandle = spec.ValueOf(make([]byte, 17)) }},
		{"AppendPendingScheduleRejectsOversizedPresetHandle (:603)", func(s *thermo.Schedule) { h := make([]byte, 17); s.PresetHandle = &h }},
		{"AppendPendingScheduleRejectsOversizedTransitionPresetHandle (:624)", func(s *thermo.Schedule) {
			h := make([]byte, 17)
			s.Transitions[0].PresetHandle = &h
		}},
		{"AppendPendingScheduleRejectsInvalidSystemMode (:672)", func(s *thermo.Schedule) { s.SystemMode = 0xFF }},
		{"AppendPendingScheduleRejectsMissingOrOutOfRangeSetpoint (:998) missing", func(s *thermo.Schedule) { s.Transitions[0].HeatingSetpoint = nil }},
		{"AppendPendingScheduleRejectsMissingOrOutOfRangeSetpoint (:998) out of range", func(s *thermo.Schedule) {
			v := int16(5000)
			s.Transitions[0].HeatingSetpoint = &v
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := chipThermostat(t)
			p.atomic(t, p.pair, beginWrite, schedulesAttribute)
			sc := chipSchedule()
			c.edit(&sc)
			wantWrite(t, p.chipWrite(t, p.pair, []thermo.Schedule{sc}), im.StatusConstraintError)
		})
	}

	t.Run("FullAtomicWriteRoundTripAppendsAndCommitsNewSchedule (:471)", func(t *testing.T) {
		port := &recordingSchedulePort{}
		p := chipThermostat(t, func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port })
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		wantWrite(t, p.chipWrite(t, p.pair, []thermo.Schedule{chipSchedule()}), im.StatusSuccess)
		wantCommit(t, p.atomic(t, p.pair, commitWrite, schedulesAttribute), tdef.StatusSuccess, tdef.StatusSuccess)
		got := p.srv.Schedules()
		if len(got) != 1 || len(got[0].ScheduleHandle.Value) != 16 {
			t.Fatalf("schedules %+v, want one with an issued 16-byte handle", got)
		}
		if commits := port.commitCalls(); len(commits) != 1 || !bytes.Equal(commits[0][0].ScheduleHandle.Value, got[0].ScheduleHandle.Value) {
			t.Errorf("port commits %+v, want the stored list", commits)
		}
	})

	t.Run("RollbackWriteClearsPendingScheduleList (:495)", func(t *testing.T) {
		port := &recordingSchedulePort{}
		p := chipThermostat(t, withSchedules(builtInSchedule(0x01)), func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port })
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		wantWrite(t, p.chipWrite(t, p.pair, []thermo.Schedule{builtInSchedule(0x01), chipSchedule()}), im.StatusSuccess)
		if r := p.atomic(t, p.pair, rollbackWrite, schedulesAttribute); r.StatusCode != tdef.StatusSuccess {
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
		p := chipThermostat(t)
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		wantWrite(t, p.chipWrite(t, p.pair, []thermo.Schedule{chipSchedule()}), im.StatusSuccess)
		if pending := p.readSchedules(t, p.pair); len(pending) != 1 {
			t.Errorf("pending %+v, want one schedule", pending)
		}
		if got := p.srv.Schedules(); len(got) != 0 {
			t.Errorf("stored schedules %+v during the write, want none", got)
		}
	})

	t.Run("WriteToSchedulesFromDifferentNodeDuringAtomicWriteIsInvalidInState (:558)", func(t *testing.T) {
		// matter.js answers another peer's Schedules write INVALID_IN_STATE
		// (AtomicWriteHandler.ts:54-56, :474-479).
		p := chipThermostat(t)
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		q := p.secondPeer(t)
		wantWrite(t, p.chipWrite(t, q, []thermo.Schedule{chipSchedule()}), im.StatusInvalidInState)
	})

	t.Run("AppendItemListOperationAppendsSingleSchedule (:581)", func(t *testing.T) {
		p := chipThermostat(t)
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		wantWrite(t, p.chipWrite(t, p.pair, []thermo.Schedule{}), im.StatusSuccess)
		wantWrite(t, p.chipWrite(t, p.pair, []thermo.Schedule{chipSchedule(), chipSchedule()}), im.StatusSuccess)
		if pending := p.readSchedules(t, p.pair); len(pending) != 2 {
			t.Errorf("pending %+v, want two schedules", pending)
		}
	})

	t.Run("CommitFailsWhenDelegateCommitReturnsError (:821)", func(t *testing.T) {
		port := &recordingSchedulePort{commitErr: errors.New("device storage full")}
		p := chipThermostat(t, func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port })
		changes := recordThermostatChanges(p.srv)
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		wantWrite(t, p.chipWrite(t, p.pair, []thermo.Schedule{chipSchedule()}), im.StatusSuccess)
		wantCommit(t, p.atomic(t, p.pair, commitWrite, schedulesAttribute), tdef.StatusFailure, tdef.StatusFailure)
		if got := p.srv.Schedules(); len(got) != 0 {
			t.Errorf("schedules %+v after a refused commit", got)
		}
		if changes.count(tdef.AttrSchedules) != 0 {
			t.Error("Schedules reported after a refused commit")
		}
	})
}
