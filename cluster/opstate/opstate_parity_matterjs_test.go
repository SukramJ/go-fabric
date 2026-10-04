// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityMatterJS_OperationalStateRevisionIDsAndAccess pins both
// clusters' revisions, element ids, access and qualities against the
// matter.js HEAD snapshot.
func TestParityMatterJS_OperationalStateRevisionIDsAndAccess(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		id  uint32
		rev uint16
	}{{opstate.ClusterIDOperationalState, opstate.Revision()}, {opstate.ClusterIDRvcOperationalState, opstate.RvcRevision()}} {
		js := paritytest.ClusterSnapshot(t, c.id)
		if c.rev != js.Revision {
			t.Errorf("0x%04X revision %d, matter.js %d", c.id, c.rev, js.Revision)
		}
		if len(js.Features) != 0 {
			t.Errorf("0x%04X: matter.js defines features %v; the server advertises none", c.id, js.Features)
		}
		attrs := map[string]uint32{
			"PhaseList": opstate.AttrPhaseList, "CurrentPhase": opstate.AttrCurrentPhase,
			"CountdownTime": opstate.AttrCountdownTime, "OperationalStateList": opstate.AttrOperationalStateList,
			"OperationalState": opstate.AttrOperationalState, "OperationalError": opstate.AttrOperationalError,
		}
		for _, a := range js.Attributes {
			if a.ID >= 0xFFF0 {
				continue
			}
			if id, ok := attrs[a.Name]; !ok || id != a.ID {
				t.Errorf("0x%04X attribute %s (0x%04X) has constant 0x%04X (known %v)", c.id, a.Name, a.ID, id, ok)
			}
			if a.Access != "R V" {
				t.Errorf("0x%04X %s access %q; the server serves it read-only", c.id, a.Name, a.Access)
			}
		}
		if q := js.Attribute(t, "CountdownTime").Quality; q != "X Q" {
			t.Errorf("CountdownTime quality %q; the server reports it as a quieter attribute", q)
		}
		cmds := map[string]uint32{
			"Pause": opstate.CmdPause, "Stop": opstate.CmdStop, "Start": opstate.CmdStart, "Resume": opstate.CmdResume,
			"OperationalCommandResponse": opstate.CmdOperationalCommandResponse, "GoHome": opstate.CmdGoHome,
		}
		for _, cmd := range js.Commands {
			if id, ok := cmds[cmd.Name]; !ok || id != cmd.ID {
				t.Errorf("0x%04X command %s (0x%02X) has constant 0x%02X (known %v)", c.id, cmd.Name, cmd.ID, id, ok)
			}
		}
		for _, e := range js.Events {
			want := map[string]uint32{"OperationalError": opstate.EventOperationalError, "OperationCompletion": opstate.EventOperationCompletion}[e.Name]
			if want != e.ID {
				t.Errorf("0x%04X event %s id 0x%02X", c.id, e.Name, e.ID)
			}
		}
	}
	// The derivation inherits Pause / Resume and their response.
	rvc := paritytest.ClusterSnapshot(t, opstate.ClusterIDRvcOperationalState)
	for name, want := range map[string]string{"Stop": "X", "Start": "X", "GoHome": "O"} {
		if got := rvc.Command(t, name).Conformance; got != want {
			t.Errorf("RvcOperationalState %s conformance %q, want %q", name, got, want)
		}
	}
}

// commandNames maps the server's command set to the names matter.js's
// command conformance refers to.
func commandNames(c opstate.Command) map[string]bool {
	return map[string]bool{
		"Pause": c&opstate.CommandPause != 0, "Stop": c&opstate.CommandStop != 0,
		"Start": c&opstate.CommandStart != 0, "Resume": c&opstate.CommandResume != 0,
		"GoHome": c&opstate.CommandGoHome != 0,
	}
}

// TestParityMatterJS_OperationalStateCommandConformance builds every
// command set and checks that NewServer accepts exactly the sets matter.js's
// conformance allows ("Resume, O", "Start, O", "Pause, O", "O"; the
// derivation's "X" for Start / Stop) and that AcceptedCommandList /
// GeneratedCommandList follow ("Pause | Stop | Start | Resume").
func TestParityMatterJS_OperationalStateCommandConformance(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		id     uint32
		build  func(opstate.Config) (*opstate.Server, error)
		states []opstate.StateEntry
	}{
		{opstate.ClusterIDOperationalState, opstate.NewServer, washerStates},
		{opstate.ClusterIDRvcOperationalState, opstate.NewRvcServer, rvcStates},
	} {
		js := paritytest.ClusterSnapshot(t, c.id)
		base := paritytest.ClusterSnapshot(t, opstate.ClusterIDOperationalState)
		conformanceOf := func(name string) string {
			// A derived command without its own conformance inherits the
			// base cluster's.
			for _, cmd := range js.Commands {
				if cmd.Name == name && cmd.Conformance != "" {
					return cmd.Conformance
				}
			}
			for _, cmd := range base.Commands {
				if cmd.Name == name {
					return cmd.Conformance
				}
			}
			return "X"
		}
		for set := range opstate.Command(1 << 5) {
			names := commandNames(set)
			legal := true
			for _, name := range []string{"Pause", "Stop", "Start", "Resume", "GoHome"} {
				_, allowed := paritytest.Conformance(conformanceOf(name), names)
				required, _ := paritytest.Conformance(conformanceOf(name), names)
				if names[name] && !allowed || !names[name] && required {
					legal = false
				}
			}
			srv, err := c.build(opstate.Config{Handler: &device{}, Commands: set, States: c.states})
			if (err == nil) != legal {
				t.Errorf("0x%04X commands %05b: NewServer err=%v, matter.js conformance legal=%v", c.id, set, err, legal)
				continue
			}
			if err != nil {
				continue
			}
			var accepted []uint32
			for _, cmd := range js.Commands {
				if names[cmd.Name] {
					accepted = append(accepted, cmd.ID)
				}
			}
			slices.Sort(accepted)
			if got := srv.MatterAcceptedCommands(); !slices.Equal(got, accepted) && len(got)+len(accepted) > 0 {
				t.Errorf("0x%04X commands %05b: AcceptedCommandList %v, want %v", c.id, set, got, accepted)
			}
			response := conformanceOf("OperationalCommandResponse")
			required, _ := paritytest.Conformance(response, names)
			if got := len(srv.MatterGeneratedCommands()) == 1; got != (required || set&opstate.CommandGoHome != 0) {
				t.Errorf("0x%04X commands %05b: GeneratedCommandList %v (%q)", c.id, set, srv.MatterGeneratedCommands(), response)
			}
		}
	}
}

// TestParityMatterJS_OperationalStateAttributeConformance checks the
// attribute list against each attribute's conformance, with and without
// the optional CountdownTime.
func TestParityMatterJS_OperationalStateAttributeConformance(t *testing.T) {
	t.Parallel()
	js := paritytest.ClusterSnapshot(t, opstate.ClusterIDOperationalState)
	for _, countdown := range []bool{false, true} {
		srv, err := opstate.NewServer(opstate.Config{States: washerStates, CountdownTime: countdown})
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range js.Attributes {
			if a.ID >= 0xFFF0 {
				continue
			}
			required, allowed := paritytest.Conformance(a.Conformance, nil)
			has := slices.Contains(srv.MatterAttributes(), a.ID)
			if required && !has || has && !allowed {
				t.Errorf("countdown=%v: %s (%q) served=%v", countdown, a.Name, a.Conformance, has)
			}
		}
	}
}

// TestParityMatterJS_OperationalStateEventsAndPriorities checks EventList
// and each emitted priority against the snapshot.
func TestParityMatterJS_OperationalStateEventsAndPriorities(t *testing.T) {
	t.Parallel()
	js := paritytest.ClusterSnapshot(t, opstate.ClusterIDOperationalState)
	srv := newWasher(t, &device{}, func(c *opstate.Config) { c.OperationCompletion = true; c.State = opstate.StateRunning })
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	if err := srv.SetOperationalError(opstate.ErrorState{ID: opstate.ErrorUnableToCompleteOperation}); err != nil {
		t.Fatal(err)
	}
	if err := srv.EmitOperationCompletion(opstate.OperationCompletion{}); err != nil {
		t.Fatal(err)
	}
	var ids []uint32
	for _, e := range js.Events {
		ids = append(ids, e.ID)
		want := contract.EventPriorityInfo
		if e.Priority == "critical" {
			want = contract.EventPriorityCritical
		}
		for _, got := range rec.all() {
			if got.event == e.ID && got.priority != want {
				t.Errorf("%s priority %d, matter.js %q", e.Name, got.priority, e.Priority)
			}
		}
		if required, _ := paritytest.Conformance(e.Conformance, nil); required && !slices.Contains(newWasher(t, &device{}, nil).MatterEvents(), e.ID) {
			t.Errorf("mandatory event %s missing from EventList", e.Name)
		}
	}
	if got := srv.MatterEvents(); !slices.Equal(got, ids) {
		t.Errorf("EventList %v, want %v", got, ids)
	}
}

// TestParityMatterJS_OperationalStateDeviceTypes pins the four device
// types: each mandates its OperationalState cluster in the snapshot, and
// the server accepts exactly the device types that use its cluster.
func TestParityMatterJS_OperationalStateDeviceTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		dt      uint16
		name    string
		cluster uint32
	}{
		{opstate.DeviceTypeLaundryWasher, "LaundryWasher", opstate.ClusterIDOperationalState},
		{opstate.DeviceTypeDishwasher, "Dishwasher", opstate.ClusterIDOperationalState},
		{opstate.DeviceTypeLaundryDryer, "LaundryDryer", opstate.ClusterIDOperationalState},
		{opstate.DeviceTypeRoboticVacuumCleaner, "RoboticVacuumCleaner", opstate.ClusterIDRvcOperationalState},
	}
	for _, tc := range cases {
		if name, _ := schema.DeviceTypeName(uint32(tc.dt)); name != tc.name {
			t.Errorf("device type 0x%04X is %q, want %q", tc.dt, name, tc.name)
		}
		if !schema.DeviceTypeRequiresServerCluster(uint32(tc.dt), tc.cluster) {
			t.Errorf("%s does not mandate 0x%04X in the snapshot", tc.name, tc.cluster)
		}
		build := opstate.NewServer
		states := washerStates
		if tc.cluster == opstate.ClusterIDRvcOperationalState {
			build, states = opstate.NewRvcServer, rvcStates
		}
		srv, err := build(opstate.Config{States: states, DeviceType: tc.dt})
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		// <device>.element.ts: Requirement OperationCompletion "M".
		if !slices.Contains(srv.MatterEvents(), opstate.EventOperationCompletion) {
			t.Errorf("%s: OperationCompletion is mandatory for the device type", tc.name)
		}
	}
}

// TestParityMatterJS_OperationalStateEnumValues pins the enum values
// against operational-state.element.ts and rvc-operational-state.element.ts.
func TestParityMatterJS_OperationalStateEnumValues(t *testing.T) {
	t.Parallel()
	states := []opstate.State{
		opstate.StateStopped, opstate.StateRunning, opstate.StatePaused, opstate.StateError,
		opstate.StateSeekingCharger, opstate.StateCharging, opstate.StateDocked, opstate.StateEmptyingDustBin,
		opstate.StateCleaningMop, opstate.StateFillingWaterTank, opstate.StateUpdatingMaps,
	}
	if got := fmt.Sprint(states); got != "[0 1 2 3 64 65 66 67 68 69 70]" {
		t.Errorf("OperationalStateEnum %s", got)
	}
	errs := []opstate.ErrorID{
		opstate.ErrorNoError, opstate.ErrorUnableToStartOrResume, opstate.ErrorUnableToCompleteOperation,
		opstate.ErrorCommandInvalidInState, opstate.ErrorFailedToFindChargingDock, opstate.ErrorStuck,
		opstate.ErrorDustBinMissing, opstate.ErrorDustBinFull, opstate.ErrorWaterTankEmpty, opstate.ErrorWaterTankMissing,
		opstate.ErrorWaterTankLidOpen, opstate.ErrorMopCleaningPadMissing, opstate.ErrorLowBattery,
		opstate.ErrorCannotReachTargetArea, opstate.ErrorDirtyWaterTankFull, opstate.ErrorDirtyWaterTankMissing,
		opstate.ErrorWheelsJammed, opstate.ErrorBrushJammed, opstate.ErrorNavigationSensorObscured,
	}
	if got := fmt.Sprint(errs); got != "[0 1 2 3 64 65 66 67 68 69 70 71 72 73 74 75 76 77 78]" {
		t.Errorf("ErrorStateEnum %s", got)
	}
}
