// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/boolcfg"
	def "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
)

// TestPublishedAlarmsFollowTheServersState (R2-C2-conc-1551): a command
// and a host setter that store AlarmsActive in one order and publish it in
// the other leave the served value at the state's, not at the older one.
// Before the fix EnableDisableAlarm stored Visual, the host stored and
// published 0, and the command then published Visual: a read served an
// alarm the device had cleared.
func TestPublishedAlarmsFollowTheServersState(t *testing.T) {
	t.Parallel()
	s, err := boolcfg.New(boolcfg.Config{
		Features: boolcfg.FeatureVisual | boolcfg.FeatureAudible, Optional: boolcfg.OptionalAlarmsEnabled,
		AlarmsSupported: boolcfg.AlarmVisual | boolcfg.AlarmAudible, AlarmsEnabled: boolcfg.AlarmVisual | boolcfg.AlarmAudible,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAlarmsActive(boolcfg.AlarmVisual | boolcfg.AlarmAudible); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	// The command's publish of AlarmsActive waits; its first publish,
	// AlarmsEnabled, does not.
	boolcfg.HoldNextPublish(s, def.AttrAlarmsActive, entered, release)
	done := make(chan error, 1)
	go func() {
		// Disabling Audible clears it from AlarmsActive: Visual stored.
		_, err := s.MatterInvoke(context.Background(), def.CmdEnableDisableAlarm,
			def.EnableDisableAlarmRequest{AlarmsToEnableDisable: boolcfg.AlarmVisual})
		done <- err
	}()
	<-entered
	// The device clears every alarm meanwhile: stored and published 0.
	if err := s.SetAlarmsActive(0); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state := s.State().AlarmsActive
	served, _ := s.MatterRead(def.AttrAlarmsActive)
	if served != any(uint8(state)) && served != any(state) {
		t.Errorf("AlarmsActive serves %v while the server's state is 0x%02X", served, state)
	}
}
