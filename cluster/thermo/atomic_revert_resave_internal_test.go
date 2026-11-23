// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
)

// revertRefusingPorts accepts the presets an atomic commit saves, refuses
// the save that restores them once, and refuses every schedule commit.
type revertRefusingPorts struct {
	mu          sync.Mutex
	armed       bool // the next accepted save is the commit's; the one after it is refused
	commitSeen  bool
	lastPresets []byte
}

func (p *revertRefusingPorts) LoadPresets(context.Context) (presets, activeHandle []byte, ok bool, err error) {
	return nil, nil, false, nil
}

func (p *revertRefusingPorts) SavePresets(_ context.Context, presets, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.armed {
		if p.commitSeen {
			p.armed = false
			return errors.New("store unavailable")
		}
		p.commitSeen = true
	}
	p.lastPresets = presets
	return nil
}

func (p *revertRefusingPorts) SetActiveSchedule(context.Context, []byte) error { return nil }

func (p *revertRefusingPorts) CommitSchedules(context.Context, []Schedule) error {
	return errors.New("the device refuses")
}

// TestRevertedPresetsReachTheStoreAfterAFailedRestore (R3-store-3253
// class, decision 570): a refused Schedules commit reverts the committed
// Presets; when the PresetStore refuses that restore, memory reads the
// previous presets while the store keeps the committed ones. The pair is
// marked behind, so the next save of it — here a direct setpoint change
// that ends nothing — brings the store back to memory. Before the fix the
// store kept the presets the controller was told were discarded.
func TestRevertedPresetsReachTheStoreAfterAFailedRestore(t *testing.T) {
	ports := &revertRefusingPorts{}
	s := presetsAndSchedulesServer(t, func(c *ThermostatConfig) { c.PresetStore, c.SchedulePort = ports, ports })
	before := s.Presets()
	ports.mu.Lock()
	ports.armed = true
	ports.mu.Unlock()
	both := []uint32{tdef.AttrPresets, tdef.AttrSchedules}
	atomicAs(t, s, atomicBeginWrite, both)
	stage(t, s, tdef.AttrPresets, append(s.Presets(), newUnoccupiedPreset()))
	stage(t, s, tdef.AttrSchedules, []Schedule{scheduleAt(360)})
	atomicAs(t, s, atomicCommitWrite, both)
	if got := s.Presets(); len(got) != len(before) {
		t.Fatalf("presets %d after the refused commit, want the %d before it", len(got), len(before))
	}
	s.SetOccupiedHeatingSetpoint(1900) // ends nothing: no preset is active
	ports.mu.Lock()
	defer ports.mu.Unlock()
	if !bytes.Equal(ports.lastPresets, EncodePresets(before)) {
		t.Errorf("the store holds the reverted presets %x, memory %x", ports.lastPresets, EncodePresets(before))
	}
}
