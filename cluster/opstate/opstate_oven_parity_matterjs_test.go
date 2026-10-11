// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/opstate"
	ovendef "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

var ovenStates = []opstate.StateEntry{
	{ID: opstate.StateStopped}, {ID: opstate.StateRunning}, {ID: opstate.StateError},
}

// TestParityMatterJS_OvenCavityServer holds OvenCavityOperationalState,
// bare and with every optional element, against matter.js
// oven-cavity-operational-state.element.ts (spectest.CheckServer).
func TestParityMatterJS_OvenCavityServer(t *testing.T) {
	t.Parallel()
	for _, cfg := range []opstate.Config{
		{States: ovenStates[2:], State: opstate.StateError},
		{
			Handler: &device{}, Commands: opstate.CommandStart | opstate.CommandStop,
			States: ovenStates, State: opstate.StateStopped, CountdownTime: true,
			DeviceType: opstate.DeviceTypeTemperatureControlledCabinet,
		},
	} {
		srv, err := opstate.NewOvenCavityServer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		spectest.CheckServer(t, srv, ovendef.Definition, 0)
		if srv.MatterClusterID() != opstate.ClusterIDOvenCavityOperationalState || opstate.OvenCavityRevision() != ovendef.Revision {
			t.Errorf("cluster 0x%04X revision %d", srv.MatterClusterID(), opstate.OvenCavityRevision())
		}
	}
}

// TestParityMatterJS_OvenCavitySnapshot pins the snapshot facts the
// derivation rests on: it derives from OperationalState, Pause and Resume
// are "X", Start "O" and Stop "Start, O"; the TemperatureControlledCabinet
// offers it ("[Heater]") with OperationCompletion mandatory, and the
// MicrowaveOven mandates OperationalState with CountdownTime.
func TestParityMatterJS_OvenCavitySnapshot(t *testing.T) {
	t.Parallel()
	js := paritytest.ClusterSnapshot(t, opstate.ClusterIDOvenCavityOperationalState)
	if js.Base != "OperationalState" {
		t.Errorf("base %q", js.Base)
	}
	for name, want := range map[string]string{"Pause": "X", "Resume": "X"} {
		if c := js.Command(t, name); c.Conformance != want {
			t.Errorf("%s %+v", name, c)
		}
	}
	for _, c := range ovendef.Definition.Commands {
		switch c.Name {
		case "Start":
			if c.Conformance.Text != "O" {
				t.Errorf("Start %q", c.Conformance.Text)
			}
		case "Stop":
			if c.Conformance.Text != "Start, O" {
				t.Errorf("Stop %q", c.Conformance.Text)
			}
		}
	}
	if allowed, known := schema.DeviceTypeAllowsServerCluster(uint32(opstate.DeviceTypeTemperatureControlledCabinet), opstate.ClusterIDOvenCavityOperationalState); !allowed || !known {
		t.Error("TemperatureControlledCabinet does not offer OvenCavityOperationalState")
	}
	if !schema.DeviceTypeRequiresServerCluster(uint32(opstate.DeviceTypeMicrowaveOven), opstate.ClusterIDOperationalState) {
		t.Error("MicrowaveOven does not mandate OperationalState")
	}
	srv, err := opstate.NewServer(opstate.Config{States: washerStates, DeviceType: opstate.DeviceTypeMicrowaveOven})
	if err != nil {
		t.Fatal(err)
	}
	// microwave-oven.element.ts: CountdownTime and OperationCompletion "M".
	if !slices.Contains(srv.MatterAttributes(), opstate.AttrCountdownTime) || !slices.Contains(srv.MatterEvents(), opstate.EventOperationCompletion) {
		t.Errorf("microwave OperationalState attributes %v events %v", srv.MatterAttributes(), srv.MatterEvents())
	}
	cab, err := opstate.NewOvenCavityServer(opstate.Config{States: ovenStates, DeviceType: opstate.DeviceTypeTemperatureControlledCabinet})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cab.MatterEvents(), opstate.EventOperationCompletion) {
		t.Error("OperationCompletion is mandatory on the cabinet")
	}
	if _, err := opstate.NewOvenCavityServer(opstate.Config{States: ovenStates, DeviceType: opstate.DeviceTypeLaundryWasher}); !errors.Is(err, opstate.ErrDeviceType) {
		t.Errorf("oven cavity on a washer: %v", err)
	}
}

// TestOvenCavityPhaseList: #assertPhaseList admits only "pre-heating",
// "pre-heated" and "cooling down", at initialization
// (OvenCavityOperationalStateServer.ts:24, :35-46).
func TestOvenCavityPhaseList(t *testing.T) {
	t.Parallel()
	zero := uint8(0)
	if _, err := opstate.NewOvenCavityServer(opstate.Config{States: ovenStates, Phases: []string{"pre-heating", "baking"}, CurrentPhase: &zero}); !errors.Is(err, opstate.ErrPhaseNotAllowed) {
		t.Errorf("phase baking: %v", err)
	}
	srv, err := opstate.NewOvenCavityServer(opstate.Config{States: ovenStates, Phases: opstate.OvenCavityPhases, CurrentPhase: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(opstate.AttrPhaseList); !slices.Equal(v.([]string), []string{"pre-heating", "pre-heated", "cooling down"}) {
		t.Errorf("PhaseList %v", v)
	}
	// The base cluster has no such rule.
	if _, err := opstate.NewServer(opstate.Config{States: washerStates, Phases: []string{"baking"}, CurrentPhase: &zero}); err != nil {
		t.Errorf("base PhaseList: %v", err)
	}
}

// TestOvenCavityCommands: Pause and Resume are refused at construction;
// Start and Stop run through the base checks and the error reactors hold.
func TestOvenCavityCommands(t *testing.T) {
	t.Parallel()
	if _, err := opstate.NewOvenCavityServer(opstate.Config{Handler: &device{}, Commands: opstate.CommandPause | opstate.CommandResume, States: washerStates}); !errors.Is(err, opstate.ErrCommandNotAllowed) {
		t.Errorf("Pause/Resume: %v", err)
	}
	d := &device{follow: map[opstate.Command]opstate.State{opstate.CommandStart: opstate.StateRunning, opstate.CommandStop: opstate.StateStopped}}
	srv, err := opstate.NewOvenCavityServer(opstate.Config{Handler: d, Commands: opstate.CommandStart | opstate.CommandStop, States: ovenStates, State: opstate.StateStopped})
	if err != nil {
		t.Fatal(err)
	}
	d.srv = srv
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	resp, err := srv.MatterInvoke(context.Background(), opstate.CmdStart, nil)
	if err != nil || resp.(clusterwire.OperationalCommandResponse).CommandResponseState.ErrorStateID != 0 {
		t.Fatalf("Start: %v %v", resp, err)
	}
	if v, _ := srv.MatterRead(opstate.AttrOperationalState); v != uint8(opstate.StateRunning) {
		t.Errorf("state %v", v)
	}
	if _, err := srv.MatterInvoke(context.Background(), opstate.CmdPause, nil); err == nil {
		t.Error("Pause accepted")
	}
	if err := srv.SetOperationalError(opstate.ErrorState{ID: opstate.ErrorUnableToCompleteOperation}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(opstate.AttrOperationalState); v != uint8(opstate.StateError) || len(rec.all()) != 1 {
		t.Errorf("state %v, events %v", v, rec.all())
	}
	if err := srv.SetOperationalState(opstate.StateStopped); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(opstate.AttrOperationalError); v.(clusterwire.ErrorStateStruct).ErrorStateID != 0 {
		t.Errorf("error %v not cleared", v)
	}
}
