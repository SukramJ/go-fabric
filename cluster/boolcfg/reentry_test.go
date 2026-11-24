// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/cluster/boolcfg"
	"thingfabric.dev/go-fabric/cluster/spec"
	def "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// setterDelegate calls a device-side setter from AlarmsEnabledChanged, as
// the Delegate doc allows, and holds AlarmsSuppressedChanged until release
// closes when release is set.
type setterDelegate struct {
	setter  func() error
	entered chan struct{}
	release chan struct{}
}

func (*setterDelegate) CurrentSensitivityLevelChanged(context.Context, uint8) error { return nil }

func (d *setterDelegate) AlarmsEnabledChanged(context.Context, def.AlarmModeBitmap) error {
	if d.setter != nil {
		return d.setter()
	}
	return nil
}

func (*setterDelegate) AlarmsActiveChanged(context.Context, def.AlarmModeBitmap) error { return nil }

func (d *setterDelegate) AlarmsSuppressedChanged(context.Context, def.AlarmModeBitmap) error {
	if d.release != nil {
		close(d.entered)
		<-d.release
	}
	return nil
}

func newSetterServer(t *testing.T, d *setterDelegate) *boolcfg.Server {
	t.Helper()
	srv, err := boolcfg.New(boolcfg.Config{
		Features: boolcfg.FeatureVisual | boolcfg.FeatureAudible | boolcfg.FeatureAlarmSuppress |
			boolcfg.FeatureSensitivityLevel,
		Optional:                   boolcfg.OptionalAlarmsEnabled | boolcfg.OptionalSensorFault,
		SupportedSensitivityLevels: 3,
		AlarmsSupported:            boolcfg.AlarmVisual | boolcfg.AlarmAudible,
		AlarmsEnabled:              boolcfg.AlarmVisual | boolcfg.AlarmAudible,
		Delegate:                   d,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestDelegateSetterListenerCommandCompletes (R5-conc-5501): a device-side
// setter the Delegate calls inside a command publishes once the command
// released cmd, so a change listener that invokes a command on the
// goroutine that tells it is not held by the outer command. Before the fix
// the setter published on the spot, the listener's command waited for cmd
// until its context ended, and the outer command waited on the listener.
func TestDelegateSetterListenerCommandCompletes(t *testing.T) {
	t.Parallel()
	setters := map[string]func(*boolcfg.Server) error{
		"SetAlarmsActive": func(s *boolcfg.Server) error { return s.SetAlarmsActive(boolcfg.AlarmVisual) },
		"ClearAllAlarms":  func(s *boolcfg.Server) error { s.ClearAllAlarms(); return nil },
		"SetSensorFault":  func(s *boolcfg.Server) error { s.SetSensorFault(boolcfg.SensorFaultGeneral); return nil },
		"SetCurrentSensitivityLevel": func(s *boolcfg.Server) error {
			return s.SetCurrentSensitivityLevel(2)
		},
	}
	for name, setter := range setters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				d := &setterDelegate{}
				srv := newSetterServer(t, d)
				if name == "ClearAllAlarms" {
					if err := srv.SetAlarmsActive(boolcfg.AlarmVisual); err != nil {
						t.Fatal(err)
					}
				}
				d.setter = func() error { return setter(srv) }
				fired := false
				var inner error
				srv.OnAttributeChange(func(contract.Change) {
					if fired {
						return
					}
					fired = true
					ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
					defer cancel()
					_, inner = srv.MatterInvoke(ctx, def.CmdEnableDisableAlarm,
						def.EnableDisableAlarmRequest{AlarmsToEnableDisable: boolcfg.AlarmVisual | boolcfg.AlarmAudible})
				})
				start := time.Now()
				_, err := srv.MatterInvoke(context.Background(), def.CmdEnableDisableAlarm,
					def.EnableDisableAlarmRequest{AlarmsToEnableDisable: boolcfg.AlarmVisual})
				if err != nil {
					t.Fatalf("outer EnableDisableAlarm: %v", err)
				}
				if !fired {
					t.Fatal("no change listener ran")
				}
				if inner != nil {
					t.Errorf("listener's EnableDisableAlarm: %v", inner)
				}
				if waited := time.Since(start); waited != 0 {
					t.Errorf("outer EnableDisableAlarm returned after %v (fake clock), want at once", waited)
				}
				if got := srv.State().AlarmsEnabled; got != boolcfg.AlarmVisual|boolcfg.AlarmAudible {
					t.Errorf("AlarmsEnabled 0x%02X after the listener's EnableDisableAlarm(0x03)", got)
				}
			})
		})
	}
}

// TestSuppressAlarmOutsideSupportedAnswersWhileACommandRuns (R5-err-5507):
// a SuppressAlarm naming a mode outside AlarmsSupported answers
// CONSTRAINT_ERROR at once, as EnableDisableAlarm does, also while another
// command holds cmd; before the fix it waited for cmd and answered with its
// context's end.
func TestSuppressAlarmOutsideSupportedAnswersWhileACommandRuns(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		d := &setterDelegate{entered: make(chan struct{}), release: make(chan struct{})}
		srv := newSetterServer(t, d)
		if err := srv.SetAlarmsActive(boolcfg.AlarmVisual); err != nil {
			t.Fatal(err)
		}
		doneA := make(chan error, 1)
		go func() {
			_, err := srv.MatterInvoke(context.Background(), def.CmdSuppressAlarm,
				def.SuppressAlarmRequest{AlarmsToSuppress: boolcfg.AlarmVisual})
			doneA <- err
		}()
		<-d.entered
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, err := srv.MatterInvoke(ctx, def.CmdSuppressAlarm, def.SuppressAlarmRequest{AlarmsToSuppress: 0x04})
		if spec.StatusOf(err) != im.StatusConstraintError {
			t.Errorf("SuppressAlarm(0x04) while a command runs: %v, want CONSTRAINT_ERROR", err)
		}
		close(d.release)
		if err := <-doneA; err != nil {
			t.Fatal(err)
		}
	})
}
