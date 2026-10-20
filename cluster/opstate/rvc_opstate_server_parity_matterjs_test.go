// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/rvc-operational-state/RvcOperationalStateServerTest.ts
// at 862ad75f. matter.js adds a RoboticVacuumCleanerDevice whose
// RvcOperationalState lists only Error and is in Error (createNode
// :17-50); the server implements none of the base commands, so pause()
// throws NotImplementedError. The Go server implements the commands its
// Config names: one built with none answers Pause with the IM status of a
// command the device does not implement, UNSUPPORTED_COMMAND, and leaves
// it out of the AcceptedCommandList. "knows methods are methods" is a
// TypeScript type assertion with no runtime (the register names it).
package opstate_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/im"
)

// TestRvcOperationalStateServerParityMatterJS ports the describe
// "RvcOperationalState" (RvcOperationalStateServerTest.ts:53-73).
func TestRvcOperationalStateServerParityMatterJS(t *testing.T) {
	t.Parallel()

	// :54-61.
	t.Run("supports base commands", func(t *testing.T) {
		t.Parallel()
		srv, err := opstate.NewRvcServer(opstate.Config{
			States: []opstate.StateEntry{{ID: opstate.StateError}}, State: opstate.StateError,
		})
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(srv.MatterAcceptedCommands(), opstate.CmdPause) {
			t.Errorf("AcceptedCommandList %v holds Pause the device does not implement", srv.MatterAcceptedCommands())
		}
		_, err = srv.MatterInvoke(context.Background(), opstate.CmdPause, nil)
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedCommand {
			t.Fatalf("Pause: %v, want UNSUPPORTED_COMMAND", err)
		}
	})
}
