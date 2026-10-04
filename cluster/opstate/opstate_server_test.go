// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// device is a host appliance: it records the commands it receives and
// answers with reply; follow moves the server's state as the device
// would.
type device struct {
	mu     sync.Mutex
	cmds   []opstate.Command
	reply  opstate.ErrorState
	err    error
	srv    *opstate.Server
	follow map[opstate.Command]opstate.State
}

func (d *device) HandleOperationalCommand(_ context.Context, cmd opstate.Command) (opstate.ErrorState, error) {
	d.mu.Lock()
	d.cmds = append(d.cmds, cmd)
	reply, err, srv, next := d.reply, d.err, d.srv, d.follow[cmd]
	d.mu.Unlock()
	if err == nil && reply.ID == opstate.ErrorNoError && srv != nil && next != 0 {
		_ = srv.SetOperationalState(next)
	}
	return reply, err
}

func (d *device) commands() []opstate.Command {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.cmds)
}

// recorder is an EventEmitter that keeps every event.
type recorder struct {
	mu     sync.Mutex
	events []emitted
}

type emitted struct {
	endpoint uint16
	cluster  uint32
	event    uint32
	data     any
	priority contract.EventPriority
}

func (r *recorder) MatterEmitEvent(endpoint uint16, clusterID, event uint32, data any, priority contract.EventPriority) {
	r.mu.Lock()
	r.events = append(r.events, emitted{endpoint, clusterID, event, data, priority})
	r.mu.Unlock()
}

func (r *recorder) all() []emitted {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// changes records every attribute-change notification.
type changes struct {
	mu   sync.Mutex
	seen [][]uint32
}

func (c *changes) record(ids []uint32) {
	c.mu.Lock()
	c.seen = append(c.seen, ids)
	c.mu.Unlock()
}

func (c *changes) take() [][]uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.seen
	c.seen = nil
	return out
}

var washerStates = []opstate.StateEntry{
	{ID: opstate.StateStopped},
	{ID: opstate.StateRunning},
	{ID: opstate.StatePaused},
	{ID: opstate.StateError},
	{ID: 0x80, Label: "Pre-soak"},
}

var rvcStates = []opstate.StateEntry{
	{ID: opstate.StateStopped},
	{ID: opstate.StateRunning},
	{ID: opstate.StatePaused},
	{ID: opstate.StateError},
	{ID: opstate.StateSeekingCharger},
	{ID: opstate.StateCharging},
	{ID: opstate.StateDocked},
}

const allBase = opstate.CommandPause | opstate.CommandStop | opstate.CommandStart | opstate.CommandResume

func newWasher(t *testing.T, d *device, mutate func(*opstate.Config)) *opstate.Server {
	t.Helper()
	cfg := opstate.Config{Handler: d, Commands: allBase, States: washerStates, State: opstate.StateStopped}
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := opstate.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if d != nil {
		d.srv = srv
	}
	return srv
}

func newRvc(t *testing.T, d *device, st opstate.State) *opstate.Server {
	t.Helper()
	srv, err := opstate.NewRvcServer(opstate.Config{
		Handler: d, Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandGoHome,
		States: rvcStates, State: st, DeviceType: opstate.DeviceTypeRoboticVacuumCleaner,
	})
	if err != nil {
		t.Fatal(err)
	}
	d.srv = srv
	return srv
}

func invoke(t *testing.T, srv *opstate.Server, cmd uint32) opstate.ErrorID {
	t.Helper()
	resp, err := srv.MatterInvoke(context.Background(), cmd, nil)
	if err != nil {
		t.Fatalf("invoke 0x%02X: %v", cmd, err)
	}
	r, ok := resp.(clusterwire.OperationalCommandResponse)
	if !ok {
		t.Fatalf("invoke 0x%02X answered %T", cmd, resp)
	}
	return opstate.ErrorID(r.CommandResponseState.ErrorStateID)
}

func state(t *testing.T, srv *opstate.Server) opstate.State {
	t.Helper()
	v, _ := srv.MatterRead(opstate.AttrOperationalState)
	return opstate.State(v.(uint8))
}

func TestNewServerRefusesInvalidConfigurations(t *testing.T) {
	t.Parallel()
	d := &device{}
	noErr := []opstate.StateEntry{{ID: opstate.StateStopped}}
	cases := []struct {
		name string
		rvc  bool
		cfg  opstate.Config
		want error
	}{
		{"GoHome on OperationalState", false, opstate.Config{Handler: d, Commands: opstate.CommandGoHome, States: washerStates}, opstate.ErrCommandNotAllowed},
		{"Start on RvcOperationalState", true, opstate.Config{Handler: d, Commands: opstate.CommandStart | opstate.CommandStop, States: rvcStates}, opstate.ErrCommandNotAllowed},
		{"commands without handler", false, opstate.Config{Commands: opstate.CommandStop, States: washerStates}, opstate.ErrNoHandler},
		{"Pause without Resume", false, opstate.Config{Handler: d, Commands: opstate.CommandPause, States: washerStates}, opstate.ErrPauseResume},
		{"Resume without Pause", false, opstate.Config{Handler: d, Commands: opstate.CommandResume, States: washerStates}, opstate.ErrPauseResume},
		{"Start without Stop", false, opstate.Config{Handler: d, Commands: opstate.CommandStart, States: washerStates}, opstate.ErrStartNeedsStop},
		{"no Error state", false, opstate.Config{States: noErr}, opstate.ErrNoErrorState},
		{"RVC state on OperationalState", false, opstate.Config{States: append(slices.Clone(washerStates), opstate.StateEntry{ID: opstate.StateDocked})}, opstate.ErrUnknownState},
		{"reserved state", true, opstate.Config{States: append(slices.Clone(rvcStates), opstate.StateEntry{ID: 0x47})}, opstate.ErrUnknownState},
		{"duplicate state", false, opstate.Config{States: append(slices.Clone(washerStates), opstate.StateEntry{ID: opstate.StateRunning})}, opstate.ErrDuplicateState},
		{"long label", false, opstate.Config{States: append(slices.Clone(washerStates), opstate.StateEntry{ID: 0x81, Label: string(make([]byte, 65))})}, opstate.ErrTooLong},
		{"Stop without Stopped", false, opstate.Config{Handler: d, Commands: opstate.CommandStop, States: []opstate.StateEntry{{ID: opstate.StateError}}, State: opstate.StateError}, opstate.ErrStateMissing},
		{"GoHome without SeekingCharger", true, opstate.Config{Handler: d, Commands: opstate.CommandGoHome, States: washerStates[:4]}, opstate.ErrStateMissing},
		{"initial state not listed", false, opstate.Config{States: washerStates, State: opstate.StateError + 1}, opstate.ErrStateNotListed},
		{"too many phases", false, opstate.Config{States: washerStates, Phases: make([]string, 33), CurrentPhase: new(uint8)}, opstate.ErrPhaseList},
		{"long phase", false, opstate.Config{States: washerStates, Phases: []string{string(make([]byte, 65))}, CurrentPhase: new(uint8)}, opstate.ErrTooLong},
		{"phases without current", false, opstate.Config{States: washerStates, Phases: []string{"wash"}}, opstate.ErrCurrentPhase},
		{"current beyond phases", false, opstate.Config{States: washerStates, Phases: []string{"wash"}, CurrentPhase: ptr(uint8(1))}, opstate.ErrCurrentPhase},
		{"RVC device type on OperationalState", false, opstate.Config{States: washerStates, DeviceType: opstate.DeviceTypeRoboticVacuumCleaner}, opstate.ErrDeviceType},
		{"washer device type on RVC", true, opstate.Config{States: rvcStates, DeviceType: opstate.DeviceTypeLaundryWasher}, opstate.ErrDeviceType},
	}
	for _, tc := range cases {
		build := opstate.NewServer
		if tc.rvc {
			build = opstate.NewRvcServer
		}
		if _, err := build(tc.cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestAttributesListsAndGlobals(t *testing.T) {
	t.Parallel()
	srv := newWasher(t, &device{}, nil)
	if got := srv.MatterAttributes(); !slices.Equal(got, []uint32{0, 1, 3, 4, 5}) {
		t.Errorf("AttributeList without CountdownTime = %v", got)
	}
	if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0, 1, 2, 3}) {
		t.Errorf("AcceptedCommandList = %v", got)
	}
	if got := srv.MatterGeneratedCommands(); !slices.Equal(got, []uint32{4}) {
		t.Errorf("GeneratedCommandList = %v", got)
	}
	if got := srv.MatterEvents(); !slices.Equal(got, []uint32{0}) {
		t.Errorf("EventList = %v", got)
	}
	if got := srv.MatterReportable(); slices.Contains(got, opstate.AttrCountdownTime) {
		t.Errorf("CountdownTime is reportable: %v", got)
	}
	if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(0) {
		t.Errorf("FeatureMap = %v", fm)
	}
	if rev, _ := srv.MatterRead(cluster.AttrGlobalClusterRevision); rev != opstate.Revision() || opstate.Revision() == 0 {
		t.Errorf("ClusterRevision = %v", rev)
	}
	if v, ok := srv.MatterRead(opstate.AttrCountdownTime); ok || v != nil {
		t.Errorf("CountdownTime read without the attribute: %v, %v", v, ok)
	}
	if _, ok := srv.MatterRead(0x0F00); ok {
		t.Error("unknown attribute read")
	}
	if v, _ := srv.MatterRead(opstate.AttrPhaseList); v != nil {
		t.Errorf("PhaseList = %v, want null", v)
	}
	if v, _ := srv.MatterRead(opstate.AttrCurrentPhase); v != nil {
		t.Errorf("CurrentPhase = %v, want null", v)
	}
	v, _ := srv.MatterRead(opstate.AttrOperationalStateList)
	list := v.([]clusterwire.OperationalStateStruct)
	if len(list) != 5 || list[4] != (clusterwire.OperationalStateStruct{OperationalStateID: 0x80, OperationalStateLabel: "Pre-soak"}) {
		t.Errorf("OperationalStateList = %+v", list)
	}
	if v, _ := srv.MatterRead(opstate.AttrOperationalError); v != (clusterwire.ErrorStateStruct{}) {
		t.Errorf("OperationalError = %+v, want NoError", v)
	}

	// A status-only cluster without commands, with CountdownTime, a
	// washer device type that makes OperationCompletion mandatory.
	quiet, err := opstate.NewServer(opstate.Config{
		States: washerStates, CountdownTime: true, DeviceType: opstate.DeviceTypeLaundryWasher,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := quiet.MatterAttributes(); !slices.Equal(got, []uint32{0, 1, 2, 3, 4, 5}) {
		t.Errorf("AttributeList with CountdownTime = %v", got)
	}
	if got := quiet.MatterAcceptedCommands(); len(got) != 0 {
		t.Errorf("AcceptedCommandList = %v", got)
	}
	if got := quiet.MatterGeneratedCommands(); len(got) != 0 {
		t.Errorf("GeneratedCommandList = %v", got)
	}
	if got := quiet.MatterEvents(); !slices.Equal(got, []uint32{0, 1}) {
		t.Errorf("EventList for a LaundryWasher = %v", got)
	}
	if v, ok := quiet.MatterRead(opstate.AttrCountdownTime); !ok || v != nil {
		t.Errorf("CountdownTime = %v, %v, want null", v, ok)
	}
	if quiet.MatterClusterID() != opstate.ClusterIDOperationalState || quiet.MatterDataVersion() == 0 {
		t.Error("cluster id or data version")
	}
}

func TestWritesAreRefused(t *testing.T) {
	t.Parallel()
	srv := newWasher(t, &device{}, nil)
	var sce im.StatusCodeError
	if err := srv.MatterWrite(context.Background(), opstate.AttrOperationalState, uint64(1)); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedWrite {
		t.Errorf("OperationalState write: %v", err)
	}
	if err := srv.MatterWrite(context.Background(), opstate.AttrCountdownTime, uint64(1)); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedAttribute {
		t.Errorf("CountdownTime write without the attribute: %v", err)
	}
}

// TestCommandsFollowOperationalStateUtils walks matter.js's
// OperationalStateUtils.assertPause / assertResume and the
// specification's "already in that state" rule.
func TestCommandsFollowOperationalStateUtils(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from     opstate.State
		cmd      uint32
		want     opstate.ErrorID
		toDevice bool
	}{
		{opstate.StateStopped, opstate.CmdPause, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateError, opstate.CmdPause, opstate.ErrorCommandInvalidInState, false},
		{opstate.StatePaused, opstate.CmdPause, opstate.ErrorNoError, false},
		{opstate.StateRunning, opstate.CmdPause, opstate.ErrorNoError, true},
		{0x80, opstate.CmdPause, opstate.ErrorNoError, true},
		{opstate.StateStopped, opstate.CmdResume, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateError, opstate.CmdResume, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateRunning, opstate.CmdResume, opstate.ErrorNoError, false},
		{opstate.StatePaused, opstate.CmdResume, opstate.ErrorNoError, true},
		{opstate.StateStopped, opstate.CmdStop, opstate.ErrorNoError, false},
		{opstate.StateRunning, opstate.CmdStop, opstate.ErrorNoError, true},
		{opstate.StateError, opstate.CmdStop, opstate.ErrorNoError, true},
		{opstate.StateRunning, opstate.CmdStart, opstate.ErrorNoError, false},
		{opstate.StateStopped, opstate.CmdStart, opstate.ErrorNoError, true},
	}
	for _, tc := range cases {
		d := &device{}
		srv := newWasher(t, d, func(c *opstate.Config) { c.State = tc.from })
		if got := invoke(t, srv, tc.cmd); got != tc.want {
			t.Errorf("command 0x%02X in state 0x%02X answered 0x%02X, want 0x%02X", tc.cmd, uint8(tc.from), uint8(got), uint8(tc.want))
		}
		if reached := len(d.commands()) == 1; reached != tc.toDevice {
			t.Errorf("command 0x%02X in state 0x%02X reached the device: %v, want %v", tc.cmd, uint8(tc.from), reached, tc.toDevice)
		}
	}
}

// TestRvcCommandsFollowOperationalStateUtils covers assertRvcPause,
// assertRvcResume and assertRvcGoHome.
func TestRvcCommandsFollowOperationalStateUtils(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from     opstate.State
		cmd      uint32
		want     opstate.ErrorID
		toDevice bool
	}{
		{opstate.StateCharging, opstate.CmdPause, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateDocked, opstate.CmdPause, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateStopped, opstate.CmdPause, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateSeekingCharger, opstate.CmdPause, opstate.ErrorNoError, true},
		{opstate.StateSeekingCharger, opstate.CmdResume, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateError, opstate.CmdResume, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateCharging, opstate.CmdResume, opstate.ErrorNoError, true},
		{opstate.StateDocked, opstate.CmdGoHome, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateCharging, opstate.CmdGoHome, opstate.ErrorCommandInvalidInState, false},
		{opstate.StateSeekingCharger, opstate.CmdGoHome, opstate.ErrorNoError, false},
		{opstate.StateRunning, opstate.CmdGoHome, opstate.ErrorNoError, true},
	}
	for _, tc := range cases {
		d := &device{}
		srv := newRvc(t, d, tc.from)
		if got := invoke(t, srv, tc.cmd); got != tc.want {
			t.Errorf("RVC command 0x%02X in state 0x%02X answered 0x%02X, want 0x%02X", tc.cmd, uint8(tc.from), uint8(got), uint8(tc.want))
		}
		if reached := len(d.commands()) == 1; reached != tc.toDevice {
			t.Errorf("RVC command 0x%02X in state 0x%02X reached the device: %v, want %v", tc.cmd, uint8(tc.from), reached, tc.toDevice)
		}
	}
	srv := newRvc(t, &device{}, opstate.StateRunning)
	if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0x00, 0x03, 0x80}) {
		t.Errorf("RVC AcceptedCommandList = %v", got)
	}
	if got := srv.MatterEvents(); !slices.Equal(got, []uint32{0, 1}) {
		t.Errorf("RVC EventList = %v, want OperationCompletion mandatory", got)
	}
	if srv.MatterClusterID() != opstate.ClusterIDRvcOperationalState {
		t.Error("RVC cluster id")
	}
	if rev, _ := srv.MatterRead(cluster.AttrGlobalClusterRevision); rev != opstate.RvcRevision() {
		t.Errorf("RVC revision %v", rev)
	}
}

func TestDeviceRepliesAndFailures(t *testing.T) {
	t.Parallel()
	d := &device{follow: map[opstate.Command]opstate.State{opstate.CommandStart: opstate.StateRunning}}
	srv := newWasher(t, d, nil)
	if got := invoke(t, srv, opstate.CmdStart); got != opstate.ErrorNoError || state(t, srv) != opstate.StateRunning {
		t.Fatalf("Start = 0x%02X, state 0x%02X", uint8(got), uint8(state(t, srv)))
	}
	// The device refuses: its ErrorState goes out as the response.
	d.reply = opstate.ErrorState{ID: opstate.ErrorUnableToStartOrResume, Details: "door open"}
	if err := srv.SetOperationalState(opstate.StateStopped); err != nil {
		t.Fatal(err)
	}
	resp, err := srv.MatterInvoke(context.Background(), opstate.CmdStart, nil)
	if err != nil || resp.(clusterwire.OperationalCommandResponse).CommandResponseState !=
		(clusterwire.ErrorStateStruct{ErrorStateID: 1, ErrorStateDetails: "door open"}) {
		t.Fatalf("refused Start = %+v, %v", resp, err)
	}
	// An undefined error state is the host's bug: FAILURE.
	d.reply = opstate.ErrorState{ID: opstate.ErrorStuck}
	var sce im.StatusCodeError
	if _, err := srv.MatterInvoke(context.Background(), opstate.CmdStart, nil); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusFailure {
		t.Errorf("RVC error from a washer: %v", err)
	}
	// A device error fails the invoke.
	d.reply, d.err = opstate.ErrorState{}, errors.New("offline")
	if _, err := srv.MatterInvoke(context.Background(), opstate.CmdStart, nil); err == nil {
		t.Error("a device error answered")
	}
	// Unknown and unsupported commands.
	for _, cmd := range []uint32{opstate.CmdGoHome, opstate.CmdOperationalCommandResponse, 0x42} {
		if _, err := srv.MatterInvoke(context.Background(), cmd, nil); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedCommand {
			t.Errorf("command 0x%02X: %v", cmd, err)
		}
	}
	stopOnly, err := opstate.NewServer(opstate.Config{Handler: d, Commands: opstate.CommandStop, States: washerStates})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stopOnly.MatterInvoke(context.Background(), opstate.CmdStart, nil); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedCommand {
		t.Errorf("Start on a Stop-only server: %v", err)
	}
}

// TestStateAndErrorReactors mirrors OperationalStateServer's reactors.
func TestStateAndErrorReactors(t *testing.T) {
	t.Parallel()
	srv := newWasher(t, &device{}, func(c *opstate.Config) { c.State = opstate.StateRunning })
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	srv.SetEndpoint(7)
	ch := &changes{}
	srv.OnMatterAttributesChanged(ch.record)

	if err := srv.SetOperationalState(opstate.StateDocked); !errors.Is(err, opstate.ErrStateNotListed) {
		t.Errorf("unlisted state: %v", err)
	}
	if err := srv.SetOperationalState(opstate.StateRunning); err != nil || len(ch.take()) != 0 {
		t.Errorf("setting the same state: %v", err)
	}
	if err := srv.SetOperationalError(opstate.ErrorState{ID: opstate.ErrorStuck}); !errors.Is(err, opstate.ErrUnknownError) {
		t.Errorf("RVC error on a washer: %v", err)
	}
	if err := srv.SetOperationalError(opstate.ErrorState{ID: 1, Details: string(make([]byte, 65))}); !errors.Is(err, opstate.ErrTooLong) {
		t.Errorf("long details: %v", err)
	}

	// An error moves the state to Error and emits the event, once.
	jam := opstate.ErrorState{ID: opstate.ErrorUnableToCompleteOperation, Details: "drain blocked"}
	if err := srv.SetOperationalError(jam); err != nil {
		t.Fatal(err)
	}
	if state(t, srv) != opstate.StateError {
		t.Errorf("state after an error = 0x%02X, want Error", uint8(state(t, srv)))
	}
	if got := ch.take(); len(got) != 1 || !slices.Equal(got[0], []uint32{opstate.AttrOperationalError, opstate.AttrOperationalState}) {
		t.Errorf("changes after an error = %v", got)
	}
	if err := srv.SetOperationalError(jam); err != nil {
		t.Fatal(err)
	}
	evs := rec.all()
	if len(evs) != 1 || evs[0].event != opstate.EventOperationalError || evs[0].priority != contract.EventPriorityCritical ||
		evs[0].endpoint != 7 || evs[0].cluster != opstate.ClusterIDOperationalState ||
		evs[0].data != (clusterwire.OperationalErrorEvent{ErrorState: clusterwire.ErrorStateStruct{ErrorStateID: 2, ErrorStateDetails: "drain blocked"}}) {
		t.Errorf("events = %+v, want one critical OperationalError", evs)
	}
	// Leaving Error clears the error.
	if err := srv.SetOperationalState(opstate.StateStopped); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(opstate.AttrOperationalError); v != (clusterwire.ErrorStateStruct{}) {
		t.Errorf("OperationalError after leaving Error = %+v", v)
	}
	if got := ch.take(); len(got) != 1 || !slices.Equal(got[0], []uint32{opstate.AttrOperationalState, opstate.AttrOperationalError}) {
		t.Errorf("changes after leaving Error = %v", got)
	}
	// Setting NoError does not move the state and emits nothing.
	if err := srv.SetOperationalError(opstate.ErrorState{ID: 0x80, Label: "Lid"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetOperationalError(opstate.ErrorState{}); err != nil {
		t.Fatal(err)
	}
	if state(t, srv) != opstate.StateError || len(rec.all()) != 2 {
		t.Errorf("state 0x%02X, %d events", uint8(state(t, srv)), len(rec.all()))
	}
	// An Error state entered with NoError, then left: nothing to clear.
	if err := srv.SetOperationalState(opstate.StateRunning); err != nil {
		t.Fatal(err)
	}
	if got := ch.take(); !slices.Equal(got[len(got)-1], []uint32{opstate.AttrOperationalState}) {
		t.Errorf("last change = %v", got)
	}
}

func TestPhases(t *testing.T) {
	t.Parallel()
	srv := newWasher(t, &device{}, func(c *opstate.Config) {
		c.Phases, c.CurrentPhase = []string{"pre-soak", "rinse"}, ptr(uint8(1))
	})
	ch := &changes{}
	srv.OnMatterAttributesChanged(ch.record)
	if v, _ := srv.MatterRead(opstate.AttrPhaseList); !slices.Equal(v.([]string), []string{"pre-soak", "rinse"}) {
		t.Errorf("PhaseList = %v", v)
	}
	if v, _ := srv.MatterRead(opstate.AttrCurrentPhase); v != uint8(1) {
		t.Errorf("CurrentPhase = %v", v)
	}
	if err := srv.SetCurrentPhase(ptr(uint8(2))); !errors.Is(err, opstate.ErrCurrentPhase) {
		t.Errorf("out-of-bounds phase: %v", err)
	}
	if err := srv.SetCurrentPhase(nil); !errors.Is(err, opstate.ErrCurrentPhase) {
		t.Errorf("null phase with a list: %v", err)
	}
	if err := srv.SetCurrentPhase(ptr(uint8(1))); err != nil || len(ch.take()) != 0 {
		t.Errorf("same phase: %v", err)
	}
	if err := srv.SetCurrentPhase(ptr(uint8(0))); err != nil {
		t.Fatal(err)
	}
	if got := ch.take(); len(got) != 1 || !slices.Equal(got[0], []uint32{opstate.AttrCurrentPhase}) {
		t.Errorf("changes = %v", got)
	}
	// A new list and phase together.
	if err := srv.SetPhaseList([]string{"wash", "rinse", "spin"}, ptr(uint8(2))); err != nil {
		t.Fatal(err)
	}
	if got := ch.take(); len(got) != 1 || !slices.Equal(got[0], []uint32{opstate.AttrPhaseList, opstate.AttrCurrentPhase}) {
		t.Errorf("changes = %v", got)
	}
	if err := srv.SetPhaseList([]string{"wash"}, ptr(uint8(1))); !errors.Is(err, opstate.ErrCurrentPhase) {
		t.Errorf("phase beyond the new list: %v", err)
	}
	// An empty list nulls CurrentPhase, whatever was asked.
	if err := srv.SetPhaseList([]string{}, ptr(uint8(0))); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(opstate.AttrCurrentPhase); v != nil {
		t.Errorf("CurrentPhase with an empty list = %v", v)
	}
	if v, _ := srv.MatterRead(opstate.AttrPhaseList); v == nil || len(v.([]string)) != 0 {
		t.Errorf("PhaseList = %#v, want an empty list", v)
	}
	if err := srv.SetCurrentPhase(ptr(uint8(0))); !errors.Is(err, opstate.ErrCurrentPhase) {
		t.Errorf("phase with an empty list: %v", err)
	}
	// Empty to null: the list changes, the phase does not.
	if err := srv.SetPhaseList(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := ch.take(); len(got) != 2 || !slices.Equal(got[1], []uint32{opstate.AttrPhaseList}) {
		t.Errorf("changes = %v", got)
	}
	if err := srv.SetPhaseList(nil, nil); err != nil || len(ch.take()) != 0 {
		t.Errorf("same null list: %v", err)
	}
	if err := srv.SetPhaseList(make([]string, 33), nil); !errors.Is(err, opstate.ErrPhaseList) {
		t.Errorf("33 phases: %v", err)
	}
}

func TestOperationCompletion(t *testing.T) {
	t.Parallel()
	srv := newWasher(t, &device{}, nil)
	if err := srv.EmitOperationCompletion(opstate.OperationCompletion{}); !errors.Is(err, opstate.ErrEventNotDeclared) {
		t.Errorf("undeclared event: %v", err)
	}
	srv = newWasher(t, &device{}, func(c *opstate.Config) { c.OperationCompletion = true })
	if err := srv.EmitOperationCompletion(opstate.OperationCompletion{}); err != nil {
		t.Errorf("without an emitter: %v", err)
	}
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	if err := srv.EmitOperationCompletion(opstate.OperationCompletion{Code: opstate.ErrorStuck}); !errors.Is(err, opstate.ErrUnknownError) {
		t.Errorf("RVC code on a washer: %v", err)
	}
	total := &clusterwire.ElapsedS{Seconds: 3600}
	if err := srv.EmitOperationCompletion(opstate.OperationCompletion{Code: opstate.ErrorNoError, TotalOperationalTime: total}); err != nil {
		t.Fatal(err)
	}
	evs := rec.all()
	if len(evs) != 1 || evs[0].event != opstate.EventOperationCompletion || evs[0].priority != contract.EventPriorityInfo ||
		evs[0].data.(clusterwire.OperationCompletionEvent).TotalOperationalTime != total {
		t.Errorf("events = %+v", evs)
	}
}

func TestCountdownTimeLimits(t *testing.T) {
	t.Parallel()
	srv := newWasher(t, &device{}, nil)
	if err := srv.SetCountdownTime(ptr(uint32(1))); !errors.Is(err, opstate.ErrCountdownTime) {
		t.Errorf("CountdownTime not served: %v", err)
	}
	srv = newWasher(t, &device{}, func(c *opstate.Config) { c.CountdownTime = true })
	if err := srv.SetCountdownTime(ptr(opstate.CountdownTimeMax + 1)); !errors.Is(err, opstate.ErrCountdownTime) {
		t.Errorf("above 259200: %v", err)
	}
	if err := srv.SetCountdownTime(ptr(opstate.CountdownTimeMax)); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(opstate.AttrCountdownTime); v != opstate.CountdownTimeMax {
		t.Errorf("CountdownTime = %v", v)
	}
}
