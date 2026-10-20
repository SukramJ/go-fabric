// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/energy-evse-mode/EnergyEvsModeServerTest.ts
// at 862ad75f. matter.js adds OnOffPlugInUnitDevice.with(EnergyEvseModeServer)
// to a MockServerNode with the modes Manual (tag Manual) and Quick (tag
// TimeOfUse), current mode 0 (createNode :16-50); the Go form is the
// server built with those modes. "supports extension of inherited
// commands" is a TypeScript type assertion with no runtime (the register
// names it).
package modebase_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/modebase"
)

// evseTestModes are createNode's supportedModes (:23-44).
var evseTestModes = []modebase.ModeOption{
	{Label: "Manual", Mode: 0, Tags: tags(modebase.EvseTagManual)},
	{Label: "Quick", Mode: 1, Tags: tags(modebase.EvseTagTimeOfUse)},
}

// TestEnergyEvseModeServerParityMatterJS ports the describe
// "EnergyEvseModeServer" (EnergyEvsModeServerTest.ts:53-79).
func TestEnergyEvseModeServerParityMatterJS(t *testing.T) {
	t.Parallel()

	// :54-58. The node starts and closes with the local mode tags: the
	// server builds with them and serves them.
	t.Run("accepts local mode tags", func(t *testing.T) {
		t.Parallel()
		srv, err := modebase.NewEnergyEvseMode(modebase.Config{Changer: &changer{}, SupportedModes: evseTestModes, CurrentMode: 0})
		if err != nil {
			t.Fatalf("building the server: %v", err)
		}
		if current(srv) != 0 {
			t.Errorf("CurrentMode %d, want 0", current(srv))
		}
	})

	// :60-67. `typeof changeToMode === "function"`: the inherited ModeBase
	// command is accepted and changes the mode.
	t.Run("supports inherited commands", func(t *testing.T) {
		t.Parallel()
		c := &changer{}
		srv, err := modebase.NewEnergyEvseMode(modebase.Config{Changer: c, SupportedModes: evseTestModes, CurrentMode: 0})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(srv.MatterAcceptedCommands(), modebase.CmdChangeToMode) {
			t.Fatalf("AcceptedCommandList %v lacks ChangeToMode", srv.MatterAcceptedCommands())
		}
		if resp := change(t, srv, 1); resp.Status != 0 || current(srv) != 1 {
			t.Errorf("ChangeToMode 1: status %d, CurrentMode %d", resp.Status, current(srv))
		}
	})
}
