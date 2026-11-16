// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	def "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	"github.com/SukramJ/go-fabric/im"
)

// clearingDelegate stands for a device whose alarm clears while it takes
// a controller's change: the named port call clears every alarm through
// the host's setter before it returns.
type clearingDelegate struct {
	srv *boolcfg.Server
	on  string
}

func (clearingDelegate) CurrentSensitivityLevelChanged(context.Context, uint8) error { return nil }

func (d clearingDelegate) AlarmsEnabledChanged(context.Context, def.AlarmModeBitmap) error {
	return d.clear("enabled")
}

func (d clearingDelegate) AlarmsActiveChanged(context.Context, def.AlarmModeBitmap) error {
	return d.clear("active")
}

func (d clearingDelegate) AlarmsSuppressedChanged(context.Context, def.AlarmModeBitmap) error {
	return d.clear("suppressed")
}

func (d clearingDelegate) clear(port string) error {
	if port == d.on {
		return d.srv.SetAlarmsActive(0)
	}
	return nil
}

func overlapServer(t *testing.T, on string) *boolcfg.Server {
	t.Helper()
	d := &clearingDelegate{on: on}
	srv, err := boolcfg.New(boolcfg.Config{
		Features:        boolcfg.FeatureVisual | boolcfg.FeatureAudible | boolcfg.FeatureAlarmSuppress,
		AlarmsSupported: boolcfg.AlarmVisual | boolcfg.AlarmAudible,
		AlarmsEnabled:   boolcfg.AlarmVisual | boolcfg.AlarmAudible,
		Delegate:        d,
	})
	if err != nil {
		t.Fatal(err)
	}
	d.srv = srv
	if err := srv.SetAlarmsActive(boolcfg.AlarmVisual | boolcfg.AlarmAudible); err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestACommandKeepsAHostChangeMadeWhileItsPortRan (R2-P6-conc-1510): a
// command stores its change on the state as it is after the port call,
// not on the snapshot it took before — an alarm the host cleared
// meanwhile stays cleared, and one that is no longer active is not
// suppressed.
func TestACommandKeepsAHostChangeMadeWhileItsPortRan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("EnableDisableAlarm", func(t *testing.T) {
		t.Parallel()
		srv := overlapServer(t, "enabled")
		if _, err := srv.MatterInvoke(ctx, def.CmdEnableDisableAlarm, def.EnableDisableAlarmRequest{AlarmsToEnableDisable: boolcfg.AlarmVisual}); err != nil {
			t.Fatal(err)
		}
		if got := srv.State().AlarmsActive; got != 0 {
			t.Errorf("AlarmsActive 0x%02X: the alarm the host cleared during the command came back", got)
		}
	})

	t.Run("SuppressAlarm", func(t *testing.T) {
		t.Parallel()
		srv := overlapServer(t, "suppressed")
		_, err := srv.MatterInvoke(ctx, def.CmdSuppressAlarm, def.SuppressAlarmRequest{AlarmsToSuppress: boolcfg.AlarmVisual})
		var sc im.StatusCodeError
		if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusInvalidInState {
			t.Errorf("SuppressAlarm of an alarm cleared meanwhile: %v, want INVALID_IN_STATE", err)
		}
		if got := srv.State().AlarmsSuppressed; got != 0 {
			t.Errorf("AlarmsSuppressed 0x%02X names an alarm that is no longer active", got)
		}
	})
}
