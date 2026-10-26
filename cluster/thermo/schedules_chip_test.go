// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// MatterScheduleConfiguration, read and switch: a port of the reading and
// switching cases of connectedhomeip
// src/app/clusters/thermostat-server/tests/TestThermostatClusterSchedules.cpp
// at 645b267a (the harness image's CHIP commit 6170af84 has no schedules
// server). matter.js does not implement the feature
// (ThermostatServer.ts:82 at 862ad75f), so chip is the behavioural
// authority (CLAUDE.md workflow step 5). chip's fixture (:37-98) is a
// HEAT+MSCH thermostat with NumberOfSchedules 2, NumberOfScheduleTransitions
// 4, NumberOfScheduleTransitionPerDay 2 and one schedule type, Heat, of two
// schedules supporting names and setpoints; its delegate is the
// SchedulePort, its SeedBuiltInSchedule a configured schedule — with one
// transition, which the Schedules attribute's "1 to
// numberOfScheduleTransitions" requires of a schedule the device supplies.
//
// Not ported: SetActiveScheduleRequestPropagatesDelegateGetError (:796) —
// the active handle is the server's state, not read through the port.
package thermo_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
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

// TestChipSetActiveScheduleRequestWithUnknownHandleIsInvalidCommand ports
// SetActiveScheduleRequestWithUnknownHandleIsInvalidCommand (:153-168):
// chip SetActiveSchedule (ThermostatClusterSchedules.cpp:743-758). The port
// is not called.
func TestChipSetActiveScheduleRequestWithUnknownHandleIsInvalidCommand(t *testing.T) {
	t.Parallel()
	port := &recordingSchedulePort{}
	srv := newMSCH(t, mschConfig(func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port }))
	if got := statusOfErr(t, invokeSetActiveSchedule(srv, []byte{0x42})); got != im.StatusInvalidCommand {
		t.Errorf("status %s, want INVALID_COMMAND", got)
	}
	if n := len(port.activeCalls()); n != 0 {
		t.Errorf("port called %d times for an unknown handle", n)
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

// TestSchedulesFromTheDevice: the device's schedules each need a handle of
// their own, a null BuiltIn is built in, an active handle that names no
// schedule is cleared at construction and refused by the setter, and a
// schedule list that drops the active schedule clears it; the setters do
// not call the port and report as the host's change.
func TestSchedulesFromTheDevice(t *testing.T) {
	t.Parallel()
	noHandle := builtInSchedule(0x01)
	noHandle.ScheduleHandle = spec.NullOf[[]byte]()
	if _, err := thermo.New(mschConfig(withSchedules(noHandle))); !errors.Is(err, thermo.ErrSchedules) {
		t.Errorf("a schedule without a handle: %v, want ErrSchedules", err)
	}
	if _, err := thermo.New(mschConfig(withSchedules(builtInSchedule(0x01), builtInSchedule(0x01)))); !errors.Is(err, thermo.ErrSchedules) {
		t.Errorf("a repeated handle: %v, want ErrSchedules", err)
	}
	if _, err := thermo.New(mschConfig(func(cfg *thermo.ThermostatConfig) { cfg.ScheduleTypes = nil })); !errors.Is(err, thermo.ErrSchedules) {
		t.Errorf("no schedule type: %v, want ErrSchedules (ScheduleTypes is 1 to 3)", err)
	}

	port := &recordingSchedulePort{setErr: errors.New("must not be called")}
	nullBuiltIn := builtInSchedule(0x02)
	nullBuiltIn.BuiltIn = spec.NullOf[bool]()
	srv := newMSCH(t, mschConfig(withSchedules(builtInSchedule(0x01), nullBuiltIn), func(cfg *thermo.ThermostatConfig) {
		cfg.SchedulePort, cfg.ActiveScheduleHandle = port, []byte{0x09}
	}))
	if h := srv.ActiveScheduleHandle(); h != nil {
		t.Errorf("an active handle naming no schedule was kept: %x", h)
	}
	if got := srv.Schedules(); len(got) != 2 || got[1].BuiltIn.Null || !got[1].BuiltIn.Value {
		t.Errorf("schedules %+v, want the null BuiltIn taken as built in", got)
	}
	rec := recordHoldChanges(t, srv)
	if err := srv.SetActiveScheduleHandle([]byte{0x09}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("SetActiveScheduleHandle(09): %v, want spec.ErrInvalidValue", err)
	}
	if err := srv.SetActiveScheduleHandle([]byte{0x02}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetSchedules([]thermo.Schedule{builtInSchedule(0x01)}); err != nil {
		t.Fatal(err)
	}
	if h := srv.ActiveScheduleHandle(); h != nil {
		t.Errorf("the dropped active schedule %x is still active", h)
	}
	want := []uint32{tdef.AttrActiveScheduleHandle, tdef.AttrSchedules, tdef.AttrActiveScheduleHandle}
	if got := rec.attrs(contract.OriginHostSet); !slices.Equal(got, want) {
		t.Errorf("reported %v, want %v", got, want)
	}
	if n := len(port.activeCalls()); n != 0 {
		t.Errorf("the setters called the port %d times", n)
	}
	plain := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
	if err := plain.SetSchedules(nil); err == nil {
		t.Error("SetSchedules without MSCH succeeded")
	}
}

// TestTypedSetOfSchedules: the typed server's Set takes Schedules and
// ActiveScheduleHandle through the setters; the schedule types and counts
// are the configuration's.
func TestTypedSetOfSchedules(t *testing.T) {
	t.Parallel()
	typed := tdef.Wrap(newMSCH(t, mschConfig()))
	list := []tdef.ScheduleStruct{builtInSchedule(0x07)}
	active := []byte{0x07}
	if err := typed.Set(tdef.Patch{Schedules: &list, ActiveScheduleHandle: &active}); err != nil {
		t.Fatal(err)
	}
	st := typed.Get()
	if len(st.Schedules) != 1 || st.ActiveScheduleHandle == nil || !bytes.Equal(*st.ActiveScheduleHandle, active) {
		t.Errorf("state schedules %+v active %v", st.Schedules, st.ActiveScheduleHandle)
	}
	n := uint8(3)
	if err := typed.Set(tdef.Patch{NumberOfSchedules: &n}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set NumberOfSchedules: %v, want spec.ErrNotHeld", err)
	}
}
