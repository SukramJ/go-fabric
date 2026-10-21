// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/spec"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
)

// TestPumpOrigins is the origin audit of the pull PumpConfigurationAndControl
// server. The state lives in the host's StateSource, so the server holds
// nothing of its own and reports no attribute change itself (it is no
// contract.AttributeChangeNotifier, which spectest.CheckOrigins needs; the
// source's ChangeNotifier is the route a change takes): the host's Set is
// refused (ErrNotHeld), a controller's write of the writable OperationMode
// reaches the source and a read-only attribute refuses a write. No command
// row: the cluster defines no command.
func TestPumpOrigins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{st: pump.State{OperationMode: pump.OperationNormal}}
	srv := newFull(t, d)

	mode := pumpdef.OperationModeMaximum
	if err := pumpdef.Wrap(srv).Set(pumpdef.Patch{OperationMode: &mode}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("host set: %v, want ErrNotHeld", err)
	}
	if err := srv.MatterWrite(ctx, pump.AttrOperationMode, uint64(pump.OperationMaximum)); err != nil || d.st.OperationMode != pump.OperationMaximum {
		t.Errorf("controller write: %v, the source holds mode %d, want Maximum", err, d.st.OperationMode)
	}
	if err := srv.MatterWrite(ctx, pump.AttrCapacity, uint64(1)); err == nil {
		t.Error("a write of the read-only Capacity succeeded")
	}
}
