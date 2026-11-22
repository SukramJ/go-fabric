// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarm_test

import (
	"context"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/alarm"
	smokedef "thingfabric.dev/go-fabric/cluster/spec/smokecoalarm"
)

// laterAlarm is a host that applies a write later: its state does not
// show the write when the setter returns. calls counts the setter calls.
type laterAlarm struct {
	mu    sync.Mutex
	st    smokedef.State
	calls int
}

func (l *laterAlarm) SmokeCOState() smokedef.State {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.st
}

func (l *laterAlarm) SetSmokeSensitivityLevel(context.Context, alarm.Sensitivity) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return nil
}

// TestSmokeSensitivityWriteVerdictIsTheWrittenValueAgainstTheHeldOne
// pins that a SmokeSensitivityLevel write's verdict compares the written
// level with the one the host holds, before the host is called: the held
// level reaches no setter and answers unchanged, another reaches the
// setter and answers changed — whatever the host's state reads when the
// setter returns. matter.js notes a change, and runs a reactor, only for
// a value that differs from the held one (packages/node/src/behavior/
// state/managed/Datasource.ts:1476-1492 at db021da0, #noteIfChanged), and
// its SmokeCoAlarmServer adds no behaviour to a write
// (packages/node/src/behaviors/smoke-co-alarm/SmokeCoAlarmServer.ts:18-42).
func TestSmokeSensitivityWriteVerdictIsTheWrittenValueAgainstTheHeldOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	host := &laterAlarm{st: smokedef.State{SmokeSensitivityLevel: alarm.SensitivityStandard}}
	srv, err := alarm.NewServer(alarm.Config{
		Source: host, Features: alarm.FeatureSmokeAlarm, Optional: alarm.OptionalSmokeSensitivityLevel,
	})
	if err != nil {
		t.Fatal(err)
	}
	v0 := srv.MatterDataVersion()
	if changed, err := srv.MatterWriteChanged(ctx, alarm.AttrSmokeSensitivityLevel, uint64(alarm.SensitivityStandard)); err != nil || changed || host.calls != 0 || srv.MatterDataVersion() != v0 {
		t.Errorf("the held level: changed %v, %v, setter calls %d, DataVersion %d → %d; want unchanged, no call, no advance",
			changed, err, host.calls, v0, srv.MatterDataVersion())
	}
	if changed, err := srv.MatterWriteChanged(ctx, alarm.AttrSmokeSensitivityLevel, uint64(alarm.SensitivityLow)); err != nil || !changed || host.calls != 1 || srv.MatterDataVersion() != v0+1 {
		t.Errorf("Low over Standard: changed %v, %v, setter calls %d, DataVersion %d → %d; want changed, one call, one advance",
			changed, err, host.calls, v0, srv.MatterDataVersion())
	}
}
