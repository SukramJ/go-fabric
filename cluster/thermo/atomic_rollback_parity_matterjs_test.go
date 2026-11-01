// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"errors"
	"sync"
	"testing"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// refusingPorts is a PresetStore and a SchedulePort; each refuses its
// commit when told to, and records what it was handed.
type refusingPorts struct {
	mu              sync.Mutex
	refusePresets   bool
	refuseSchedules bool
	savedPresets    [][]byte
	committed       [][]Schedule
}

func (p *refusingPorts) LoadPresets(context.Context) ([]byte, []byte, bool, error) {
	return nil, nil, false, nil
}

func (p *refusingPorts) SavePresets(_ context.Context, presets, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.refusePresets {
		return contract.StatusError{Status: uint8(im.StatusResourceExhausted)}
	}
	p.savedPresets = append(p.savedPresets, presets)
	return nil
}

func (p *refusingPorts) SetActiveSchedule(context.Context, []byte) error { return nil }

func (p *refusingPorts) CommitSchedules(_ context.Context, schedules []Schedule) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.refuseSchedules {
		return errors.New("the device refuses")
	}
	p.committed = append(p.committed, schedules)
	return nil
}

// TestParityMatterJS_AtomicCommitIsOneUnit: CommitWrite of Presets and
// Schedules is one unit even when a host port refuses the later
// attribute: the attribute already committed is restored — its previous
// value stored and handed to its port again — and the refusal's status
// stands for every attribute. Mirrors matter.js
// packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:314-326 at
// 862ad75f and AtomicWriteHandlerTest.ts:491-517 ("discards a valid
// Presets write when the Schedules write fails"), the failure here
// raised by this module's host ports (W3-clua1-02).
func TestParityMatterJS_AtomicCommitIsOneUnit(t *testing.T) {
	presetsAttr, schedulesAttr := tdef.AttrPresets, tdef.AttrSchedules
	status := func(attr uint32, code tdef.Status) tdef.AtomicResponseAttributeStatusEntry {
		return tdef.AtomicResponseAttributeStatusEntry{AttributeId: attr, StatusCode: code}
	}

	t.Run("a refused Schedules commit restores the committed Presets", func(t *testing.T) {
		ports := &refusingPorts{refuseSchedules: true}
		s := presetsAndSchedulesServer(t, func(c *ThermostatConfig) { c.PresetStore, c.SchedulePort = ports, ports })
		before := s.Presets()
		ports.mu.Lock()
		base := len(ports.savedPresets) // the presets New saved
		ports.mu.Unlock()
		both := []uint32{presetsAttr, schedulesAttr}
		atomicAs(t, s, atomicBeginWrite, both)
		stage(t, s, presetsAttr, append(s.Presets(), newUnoccupiedPreset()))
		stage(t, s, schedulesAttr, []Schedule{scheduleAt(360)})
		wantStatuses(t, atomicAs(t, s, atomicCommitWrite, both), tdef.StatusFailure,
			status(presetsAttr, tdef.StatusFailure), status(schedulesAttr, tdef.StatusFailure))
		if got := s.Presets(); len(got) != len(before) {
			t.Errorf("presets %d after the refused commit, want the %d before it", len(got), len(before))
		}
		if len(s.Schedules()) != 0 {
			t.Errorf("schedules %d after the refused commit, want 0", len(s.Schedules()))
		}
		ports.mu.Lock()
		defer ports.mu.Unlock()
		if n := len(ports.savedPresets) - base; n != 2 || string(ports.savedPresets[len(ports.savedPresets)-1]) != string(EncodePresets(before)) {
			t.Errorf("the PresetStore saw %d saves, want the commit and the restore of the previous presets", n)
		}
	})

	t.Run("a refused Presets commit restores the committed Schedules", func(t *testing.T) {
		ports := &refusingPorts{}
		s := presetsAndSchedulesServer(t, func(c *ThermostatConfig) { c.PresetStore, c.SchedulePort = ports, ports })
		ports.mu.Lock()
		ports.refusePresets = true
		ports.mu.Unlock()
		order := []uint32{schedulesAttr, presetsAttr}
		atomicAs(t, s, atomicBeginWrite, order)
		stage(t, s, schedulesAttr, []Schedule{scheduleAt(360)})
		stage(t, s, presetsAttr, append(s.Presets(), newUnoccupiedPreset()))
		wantStatuses(t, atomicAs(t, s, atomicCommitWrite, order), tdef.StatusFailure,
			status(schedulesAttr, tdef.StatusResourceExhausted), status(presetsAttr, tdef.StatusResourceExhausted))
		if len(s.Schedules()) != 0 || len(s.Presets()) != 1 {
			t.Errorf("schedules %d presets %d after the refused commit, want 0 and 1", len(s.Schedules()), len(s.Presets()))
		}
		ports.mu.Lock()
		defer ports.mu.Unlock()
		if n := len(ports.committed); n != 2 || len(ports.committed[1]) != 0 {
			t.Errorf("the SchedulePort saw %d commits, want the commit and the restore of none", n)
		}
	})
}
