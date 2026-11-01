// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
)

// TestParityMatterJS_StartUpCurrentLevel: the held server with Lighting
// applies StartUpCurrentLevel at start-up — 0 is MinLevel, null keeps
// CurrentLevel, any other value is the level — except after a software
// update and on a bridged endpoint, one row per branch of matter.js
// packages/node/src/behaviors/level-control/LevelControlServer.ts:222-245
// at 862ad75f (initializeLighting; W3-clua2-02). matter.js's tests do not
// cover it; the rows follow the code.
func TestParityMatterJS_StartUpCurrentLevel(t *testing.T) {
	t.Parallel()
	u8 := func(v uint8) *uint8 { return &v }
	softwareUpdate := func() uint8 { return 5 } // BootReasonEnum.SoftwareUpdateCompleted
	powerOn := func() uint8 { return 1 }
	for _, c := range []struct {
		name     string
		features uint32
		current  *uint8
		startUp  *uint8
		bridged  bool
		boot     func() uint8
		want     any
	}{
		{"0 is MinLevel", levelcontrol.FeatureLighting, u8(200), u8(0), false, nil, uint8(1)},
		{"null keeps CurrentLevel", levelcontrol.FeatureLighting, u8(200), nil, false, nil, uint8(200)},
		{"a value is the level", levelcontrol.FeatureLighting, u8(200), u8(77), false, powerOn, uint8(77)},
		{"a value over a null level", levelcontrol.FeatureLighting, nil, u8(77), false, nil, uint8(77)},
		{"not after a software update", levelcontrol.FeatureLighting, u8(200), u8(77), false, softwareUpdate, uint8(200)},
		{"not under an Aggregator", levelcontrol.FeatureLighting, u8(200), u8(77), true, nil, uint8(200)},
		{"not without Lighting", 0, u8(200), u8(77), false, nil, uint8(200)},
	} {
		srv, err := levelcontrol.New(levelcontrol.HeldConfig{
			Features: c.features, CurrentLevel: c.current, StartUpCurrentLevel: c.startUp, Bridged: c.bridged, BootReason: c.boot,
		})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if v, _ := srv.MatterRead(levelcontrol.AttrCurrentLevel); v != c.want {
			t.Errorf("%s: CurrentLevel %v, want %v", c.name, v, c.want)
		}
		if c.features&levelcontrol.FeatureLighting != 0 {
			want := any(nil)
			if c.startUp != nil {
				want = *c.startUp
			}
			if v, _ := srv.MatterRead(levelcontrol.AttrStartUpCurrentLevel); v != want {
				t.Errorf("%s: StartUpCurrentLevel %v, want %v", c.name, v, want)
			}
		}
	}
}
