// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/cluster/spec"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	rvcdef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
)

// TestServerMatchesTheGeneratedDefinition holds both derivations, bare
// and with every optional element, against matter.js
// operational-state.element.ts and rvc-operational-state.element.ts
// (spectest.CheckServer).
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	all := []opstate.StateEntry{
		{ID: opstate.StateStopped}, {ID: opstate.StateRunning}, {ID: opstate.StatePaused}, {ID: opstate.StateError},
	}
	rvcAll := append([]opstate.StateEntry{{ID: opstate.StateSeekingCharger}}, all...)
	for _, cfg := range []opstate.Config{
		{States: all[3:], State: opstate.StateError},
		{
			Handler: &device{}, Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandStop | opstate.CommandStart,
			States: all, State: opstate.StateStopped, CountdownTime: true, OperationCompletion: true,
		},
	} {
		srv, err := opstate.NewServer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		spectest.CheckServer(t, srv, opdef.Definition, 0)
	}
	for _, cfg := range []opstate.Config{
		{States: all[3:], State: opstate.StateError},
		{
			Handler: &device{}, Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandGoHome,
			States: rvcAll, State: opstate.StateStopped, CountdownTime: true, OperationCompletion: true,
		},
	} {
		srv, err := opstate.NewRvcServer(cfg)
		if err != nil {
			t.Fatal(err)
		}
		spectest.CheckServer(t, srv, rvcdef.Definition, 0)
	}
}

// TestLimitsMatchTheDefinition pins the exported limits against the
// element's constraints.
func TestLimitsMatchTheDefinition(t *testing.T) {
	t.Parallel()
	phases := opdef.Definition.Attribute(opstate.AttrPhaseList).Constraint
	countdown := opdef.Definition.Attribute(opstate.AttrCountdownTime).Constraint
	label := opdef.ErrorStateStructDef.Fields[1].Constraint
	for _, c := range []struct {
		name string
		b    *spec.Bound
		want int64
	}{
		{"PhaseList entries", phases.Max, opstate.PhaseListMaxEntries},
		{"PhaseList entry bytes", phases.Entry.Max, opstate.PhaseMaxBytes},
		{"CountdownTime", countdown.Max, int64(opstate.CountdownTimeMax)},
		{"ErrorStateLabel", label.Max, opstate.LabelMaxBytes},
	} {
		if c.b == nil || c.b.Int != c.want {
			t.Errorf("%s: definition %+v, exported %d", c.name, c.b, c.want)
		}
	}
}
