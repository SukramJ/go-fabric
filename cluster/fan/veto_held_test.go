// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fan_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/fan"
)

// TestVetoHearsAChangeOnly (R2-P5-apispec-02): a write of every writable
// attribute's held value reaches no veto; a change of each reaches it and
// is refused by it.
func TestVetoHearsAChangeOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := newFull(t, &device{})
	refusal := errors.New("the host says no")
	calls := 0
	srv.SetBeforeWrite(func(context.Context, uint32, any) error { calls++; return refusal })
	for _, id := range []uint32{fan.AttrFanMode, fan.AttrPercentSetting, fan.AttrSpeedSetting, fan.AttrRockSetting, fan.AttrWindSetting, fan.AttrAirflowDirection} {
		v, ok := srv.MatterRead(id)
		if !ok {
			t.Fatalf("0x%04X not served", id)
		}
		var w any
		switch x := v.(type) {
		case uint8:
			w = uint64(x)
		case nil:
			continue
		default:
			t.Fatalf("0x%04X reads %T", id, v)
		}
		if err := srv.MatterWrite(ctx, id, w); err != nil {
			t.Errorf("held 0x%04X: %v", id, err)
		}
	}
	if calls != 0 {
		t.Fatalf("held writes reached the veto %d times", calls)
	}
	for _, c := range []struct {
		id uint32
		v  uint64
	}{
		{fan.AttrFanMode, uint64(fan.FanModeHigh)},
		{fan.AttrPercentSetting, 55},
		{fan.AttrSpeedSetting, 7},
		{fan.AttrRockSetting, uint64(fan.RockUpDown)},
		{fan.AttrWindSetting, uint64(fan.WindNatural)},
		{fan.AttrAirflowDirection, 1},
	} {
		if err := srv.MatterWrite(ctx, c.id, c.v); !errors.Is(err, refusal) {
			t.Errorf("change of 0x%04X: %v, want the veto", c.id, err)
		}
	}
}
