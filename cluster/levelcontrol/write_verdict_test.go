// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/levelcontrol"
	lvldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
)

// laterHost is a host that applies an Options or OnLevel write later: its
// state does not show the write when the setter returns. calls counts the
// setter calls.
type laterHost struct {
	*fakeHost
	calls int
}

func (l *laterHost) SetOptions(context.Context, lvldef.OptionsBitmap) error {
	l.calls++
	return nil
}

func (l *laterHost) SetOnLevel(context.Context, *uint8) error {
	l.calls++
	return nil
}

// TestLevelWriteVerdictIsTheWrittenValueAgainstTheHeldOne pins that an
// Options or OnLevel write's verdict compares the written value with the
// one the host holds, before the host is called: the held value reaches
// no setter and answers unchanged, another reaches the setter and answers
// changed — whatever the host's state reads when the setter returns.
// matter.js notes a change, and runs a reactor, only for a value that
// differs from the held one (packages/node/src/behavior/state/managed/
// Datasource.ts:1476-1492 at db021da0, #noteIfChanged); its
// LevelControlServer reacts to OnOff's onOff$Changed alone
// (packages/node/src/behaviors/level-control/LevelControlServer.ts:253-255),
// not to a write of Options or OnLevel.
func TestLevelWriteVerdictIsTheWrittenValueAgainstTheHeldOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name          string
		attr          uint32
		held, written any
	}{
		{"Options", levelcontrol.AttrOptions, uint64(0), uint64(1)},
		{"OnLevel", levelcontrol.AttrOnLevel, uint64(100), uint64(120)},
		{"OnLevel null", levelcontrol.AttrOnLevel, uint64(100), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := &laterHost{fakeHost: &fakeHost{level: 50, levelKnown: true, onLevel: 100, onKnown: true}}
			srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
			v0 := srv.MatterDataVersion()
			if changed, err := srv.MatterWriteChanged(ctx, tc.attr, tc.held); err != nil || changed || host.calls != 0 || srv.MatterDataVersion() != v0 {
				t.Errorf("the held %v: changed %v, %v, setter calls %d, DataVersion %d → %d; want unchanged, no call, no advance",
					tc.held, changed, err, host.calls, v0, srv.MatterDataVersion())
			}
			if changed, err := srv.MatterWriteChanged(ctx, tc.attr, tc.written); err != nil || !changed || host.calls != 1 || srv.MatterDataVersion() != v0+1 {
				t.Errorf("%v over the held %v: changed %v, %v, setter calls %d, DataVersion %d → %d; want changed, one call, one advance",
					tc.written, tc.held, changed, err, host.calls, v0, srv.MatterDataVersion())
			}
		})
	}
}
