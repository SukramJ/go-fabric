// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
)

// blockingCommitPresetStore lets New seed it, then blocks the first
// commit's SavePresets until release closes.
type blockingCommitPresetStore struct {
	calls   int
	entered chan struct{}
	release chan struct{}
}

func (*blockingCommitPresetStore) LoadPresets(context.Context) (presets, active []byte, ok bool, err error) {
	return nil, nil, false, nil
}

func (p *blockingCommitPresetStore) SavePresets(context.Context, []byte, []byte) error {
	p.calls++
	if p.calls != 2 { // 1: the seed at New; 2: the commit under test
		return nil
	}
	close(p.entered)
	<-p.release
	return nil
}

// A value the peer stages while its CommitWrite runs is not what the
// commit stores: the commit judges and stores the values staged when it
// began (R2-C2-conc-1501). Before the fix, a Schedules list staged while
// commitAll saved the Presets replaced the one the checks had passed, and
// the built-in schedule 01 was removed with SUCCESS.
func TestCommitWriteStoresTheValuesStagedWhenItBegan(t *testing.T) {
	store := &blockingCommitPresetStore{entered: make(chan struct{}), release: make(chan struct{})}
	s := presetsAndSchedulesServer(t, func(cfg *ThermostatConfig) {
		sc := scheduleWithHandle1(360)
		sc.BuiltIn = spec.ValueOf(true)
		cfg.Schedules = []Schedule{sc}
		cfg.PresetStore = store
	})
	both := []uint32{tdef.AttrPresets, tdef.AttrSchedules}
	atomicAs(t, s, atomicBeginWrite, both)
	stage(t, s, tdef.AttrPresets, s.Presets())
	stage(t, s, tdef.AttrSchedules, s.Schedules()) // keeps built-in 01
	done := make(chan tdef.AtomicResponse, 1)
	go func() { done <- atomicAs(t, s, atomicCommitWrite, both) }()
	<-store.entered
	// The same peer, while the commit saves the Presets: a list that
	// drops the built-in schedule.
	_ = s.MatterWrite(peerContext(), tdef.AttrSchedules, []Schedule{scheduleAt(480)})
	close(store.release)
	resp := <-done
	if resp.StatusCode != tdef.StatusSuccess {
		t.Fatalf("commit status = %v, want SUCCESS (the values staged before it are valid)", resp.StatusCode)
	}
	got := s.Schedules()
	if len(got) != 1 || !got[0].BuiltIn.Value || got[0].ScheduleHandle.Value == nil || got[0].ScheduleHandle.Value[0] != 0x01 {
		t.Fatalf("stored schedules = %+v, want the built-in schedule 01 the commit was judged on", got)
	}
}
