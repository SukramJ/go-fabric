// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/valve"
)

// TestValveOrigins is the origin audit of the pull Valve server. The
// state lives in the host's StateSource, so the server holds nothing of
// its own and reports no attribute change itself (it is no
// contract.AttributeChangeNotifier, which spectest.CheckOrigins needs; the
// source's ChangeNotifier is the route a change takes): the host's Set is
// refused (ErrNotHeld) and changes nothing, a controller's write of the
// writable DefaultOpenDuration and the Open command reach the source and
// the read-only attributes refuse a write.
func TestValveOrigins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})

	seconds := uint32(60)
	if err := valvedef.Wrap(srv).Set(valvedef.Patch{DefaultOpenDuration: &seconds}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("host set: %v, want ErrNotHeld", err)
	}
	if _, err := srv.MatterInvoke(ctx, valvedef.CmdOpen, valvedef.OpenRequest{}); err != nil || len(host.openCalls) != 1 {
		t.Errorf("command: %v, the source saw %d Open calls, want one", err, len(host.openCalls))
	}
	if err := srv.MatterWrite(ctx, valvedef.AttrDefaultOpenDuration, uint64(900)); err != nil || len(host.defaultSets) != 1 {
		t.Errorf("controller write: %v, the source saw %d DefaultOpenDuration sets, want one", err, len(host.defaultSets))
	}
	if err := srv.MatterWrite(ctx, valvedef.AttrCurrentState, uint8(1)); err == nil {
		t.Error("a write of the read-only CurrentState succeeded")
	}
}
