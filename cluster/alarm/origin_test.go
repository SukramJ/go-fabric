// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarm_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/spec"
	smokedef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
)

// TestSmokeCoAlarmOrigins is the origin audit of the pull SmokeCoAlarm
// server. The state lives in the host's StateSource, so the server holds
// nothing of its own and reports no attribute change itself (it is no
// contract.AttributeChangeNotifier, which spectest.CheckOrigins needs; the
// source's ChangeNotifier is the route a change takes): the host's Set is
// refused (ErrNotHeld), SelfTestRequest and a controller's write of the
// writable SmokeSensitivityLevel reach the source, and the read-only
// attributes refuse a write.
func TestSmokeCoAlarmOrigins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d)

	muted := smokedef.MuteStateMuted
	if err := smokedef.Wrap(srv).Set(smokedef.Patch{DeviceMuted: &muted}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("host set: %v, want ErrNotHeld", err)
	}
	if _, err := srv.MatterInvoke(ctx, smokedef.CmdSelfTestRequest, nil); err != nil || d.selfTests != 1 {
		t.Errorf("command: %v, the source saw %d self-tests, want one", err, d.selfTests)
	}
	if err := srv.MatterWrite(ctx, smokedef.AttrSmokeSensitivityLevel, uint64(alarm.SensitivityHigh)); err != nil ||
		!slices.Equal(d.written, []alarm.Sensitivity{alarm.SensitivityHigh}) {
		t.Errorf("controller write: %v, the source saw %v, want High", err, d.written)
	}
	if err := srv.MatterWrite(ctx, smokedef.AttrExpressedState, uint64(0)); err == nil {
		t.Error("a write of the read-only ExpressedState succeeded")
	}
}
