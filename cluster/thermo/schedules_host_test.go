// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestSchedulesFromTheDevice: the device's schedules — configured or
// stored by SetSchedules — are the host's authoritative replace (owner
// decision O-8, BD-Thermostat-HostSchedules): held to matter.js's content
// rules (ThermostatServer.ts:2693-3003 at b25ed960) and normalised — a
// null handle gets one, a null BuiltIn takes the stored schedule's or
// false (:3028-3048) — while matter.js's lifecycle rules of a Matter write
// do not apply: a handle the device never issued is a new schedule, a
// schedule may arrive built in, a built-in schedule may be dropped, and a
// dropped active schedule is cleared (as the start clears a dangling
// handle, :353-366). A repeated handle and a configuration without a
// schedule type are refused; the setters do not call the port and report
// as the host's change.
func TestSchedulesFromTheDevice(t *testing.T) {
	t.Parallel()
	noHandle := builtInSchedule(0x01)
	noHandle.ScheduleHandle, noHandle.BuiltIn = spec.NullOf[[]byte](), spec.NullOf[bool]()
	srv := newMSCH(t, mschConfig(withSchedules(noHandle)))
	if got := srv.Schedules(); len(got) != 1 || len(got[0].ScheduleHandle.Value) != 16 || got[0].BuiltIn.Null || got[0].BuiltIn.Value {
		t.Errorf("schedules %+v, want a generated handle and BuiltIn false", got)
	}
	newBuiltIn := builtInSchedule(0x01)
	newBuiltIn.ScheduleHandle = spec.NullOf[[]byte]()
	if got := newMSCH(t, mschConfig(withSchedules(newBuiltIn))).Schedules(); len(got) != 1 || !got[0].BuiltIn.Value {
		t.Errorf("schedules %+v, want the configured built-in schedule", got)
	}
	if _, err := thermo.New(mschConfig(withSchedules(builtInSchedule(0x01), builtInSchedule(0x01)))); !errors.Is(err, thermo.ErrSchedules) {
		t.Errorf("a repeated handle: %v, want ErrSchedules", err)
	}
	if _, err := thermo.New(mschConfig(func(cfg *thermo.ThermostatConfig) { cfg.ScheduleTypes = nil })); !errors.Is(err, thermo.ErrSchedules) {
		t.Errorf("no schedule type: %v, want ErrSchedules (ScheduleTypes is 1 to 3)", err)
	}

	port := &recordingSchedulePort{setErr: errors.New("must not be called"), commitErr: errors.New("must not be called")}
	removable := builtInSchedule(0x02)
	removable.BuiltIn = spec.NullOf[bool]()
	srv = newMSCH(t, mschConfig(withSchedules(builtInSchedule(0x01), removable), func(cfg *thermo.ThermostatConfig) {
		cfg.SchedulePort, cfg.ActiveScheduleHandle = port, []byte{0x09}
	}))
	if h := srv.ActiveScheduleHandle(); h != nil {
		t.Errorf("an active handle naming no schedule was kept: %x", h)
	}
	if got := srv.Schedules(); len(got) != 2 || got[1].BuiltIn.Null || got[1].BuiltIn.Value {
		t.Errorf("schedules %+v, want the null BuiltIn taken as false", got)
	}
	rec := recordHoldChanges(t, srv)
	if err := srv.SetActiveScheduleHandle([]byte{0x09}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("SetActiveScheduleHandle(09): %v, want spec.ErrInvalidValue", err)
	}
	if err := srv.SetActiveScheduleHandle([]byte{0x02}); err != nil {
		t.Fatal(err)
	}
	// Dropping the active schedule and the built-in one, adding a schedule
	// with a handle of the host's own: accepted, the active handle cleared.
	if err := srv.SetSchedules([]thermo.Schedule{builtInSchedule(0x03)}); err != nil {
		t.Fatal(err)
	}
	if h := srv.ActiveScheduleHandle(); h != nil {
		t.Errorf("the dropped active schedule %x is still active", h)
	}
	if got := srv.Schedules(); len(got) != 1 || !bytes.Equal(got[0].ScheduleHandle.Value, []byte{0x03}) || !got[0].BuiltIn.Value {
		t.Errorf("schedules %+v, want the host's built-in schedule 03", got)
	}
	want := []uint32{tdef.AttrActiveScheduleHandle, tdef.AttrSchedules, tdef.AttrActiveScheduleHandle}
	if got := rec.attrs(contract.OriginHostSet); !slices.Equal(got, want) {
		t.Errorf("reported %v, want %v", got, want)
	}
	if n, m := len(port.activeCalls()), len(port.commitCalls()); n != 0 || m != 0 {
		t.Errorf("the setters called the port %d and %d times", n, m)
	}
	plain := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
	if err := plain.SetSchedules(nil); err == nil {
		t.Error("SetSchedules without MSCH succeeded")
	}
}

// TestHostReplaceOfSchedules (O-8): the host's replace takes a new
// schedule with a fixed handle of its own, refuses one naming a preset no
// preset carries (a content rule, ThermostatServer.ts:2775-2788 at
// b25ed960) and leaves the stored list as it was, and clears the active
// handle when it drops the active schedule.
func TestHostReplaceOfSchedules(t *testing.T) {
	t.Parallel()
	srv := newSchedulesServer(t, schedulesConfig(5, nil))
	fixed := newSchedule(scHandle([]byte("P1")), scBuiltIn(true))
	if err := srv.SetSchedules([]thermo.Schedule{fixed}); err != nil {
		t.Fatalf("a new schedule with a fixed handle: %v", err)
	}
	if got := srv.Schedules(); len(got) != 1 || !bytes.Equal(got[0].ScheduleHandle.Value, []byte("P1")) || !got[0].BuiltIn.Value {
		t.Fatalf("schedules %+v, want P1, built in", got)
	}
	wantStatusCode(t, srv.SetSchedules([]thermo.Schedule{fixed, newSchedule(scHandle([]byte("P2")), scPreset(filled(16, 9)))}), im.StatusConstraintError, "names no preset")
	if got := srv.Schedules(); len(got) != 1 {
		t.Errorf("schedules %+v after a refused replace, want P1 alone", got)
	}
	if err := srv.SetActiveScheduleHandle([]byte("P1")); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetSchedules([]thermo.Schedule{newSchedule(scHandle([]byte("P2")))}); err != nil {
		t.Fatalf("a replace dropping the active schedule: %v", err)
	}
	if h := srv.ActiveScheduleHandle(); h != nil {
		t.Errorf("ActiveScheduleHandle %x, want null once its schedule is gone", h)
	}
}

// confirmingSchedulePort stands for a device that stores the list a
// commit hands it and confirms it through the host's SetSchedules.
type confirmingSchedulePort struct {
	srv *thermo.ThermostatServer
	err error
}

func (*confirmingSchedulePort) SetActiveSchedule(context.Context, []byte) error { return nil }

func (c *confirmingSchedulePort) CommitSchedules(_ context.Context, list []thermo.Schedule) error {
	c.err = c.srv.SetSchedules(list)
	return c.err
}

// TestSchedulePortConfirmsACommit: the SchedulePort may confirm the list
// a commit hands it through SetSchedules, the host's replace, new
// schedules with the handles the commit issued.
func TestSchedulePortConfirmsACommit(t *testing.T) {
	port := &confirmingSchedulePort{}
	p := chipThermostat(t, func(cfg *thermo.ThermostatConfig) { cfg.SchedulePort = port })
	port.srv = p.srv
	p.atomic(t, p.pair, beginWrite, schedulesAttribute)
	wantWrite(t, p.chipWrite(t, p.pair, []thermo.Schedule{chipSchedule()}), im.StatusSuccess)
	wantCommit(t, p.atomic(t, p.pair, commitWrite, schedulesAttribute), tdef.StatusSuccess, tdef.StatusSuccess)
	if port.err != nil {
		t.Errorf("the port's SetSchedules: %v", port.err)
	}
	if got := p.srv.Schedules(); len(got) != 1 || len(got[0].ScheduleHandle.Value) != 16 {
		t.Errorf("schedules %+v, want the committed one", got)
	}
}

// TestTypedSetOfSchedules: the typed server's Set takes Schedules and
// ActiveScheduleHandle through the setters; the schedule types and counts
// are the configuration's.
func TestTypedSetOfSchedules(t *testing.T) {
	t.Parallel()
	typed := tdef.Wrap(newMSCH(t, mschConfig(withSchedules(builtInSchedule(0x07)))))
	list := []tdef.ScheduleStruct{builtInSchedule(0x07)}
	list[0].Transitions[0].TransitionTime = 60
	active := []byte{0x07}
	if err := typed.Set(tdef.Patch{Schedules: &list, ActiveScheduleHandle: &active}); err != nil {
		t.Fatal(err)
	}
	st := typed.Get()
	if len(st.Schedules) != 1 || st.Schedules[0].Transitions[0].TransitionTime != 60 || st.ActiveScheduleHandle == nil || !bytes.Equal(*st.ActiveScheduleHandle, active) {
		t.Errorf("state schedules %+v active %v", st.Schedules, st.ActiveScheduleHandle)
	}
	n := uint8(3)
	if err := typed.Set(tdef.Patch{NumberOfSchedules: &n}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set NumberOfSchedules: %v, want spec.ErrNotHeld", err)
	}
}
