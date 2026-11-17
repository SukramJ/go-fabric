// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

// droppingSchedulePort stands for a device whose program changes while it
// switches: SetActiveSchedule stores a list without the handle through the
// host's SetSchedules.
type droppingSchedulePort struct {
	srv  *thermo.ThermostatServer
	keep []thermo.Schedule
}

func (d *droppingSchedulePort) SetActiveSchedule(context.Context, []byte) error {
	return d.srv.SetSchedules(d.keep)
}

func (d *droppingSchedulePort) CommitSchedules(context.Context, []thermo.Schedule) error { return nil }

// TestSetActiveScheduleRequestDoesNotStoreAHandleDroppedMeanwhile
// (R2-P6-conc-1510): a schedule the host's SetSchedules dropped while the
// port switched the device is not stored as the active one; the command
// answers INVALID_COMMAND, as it does for a handle unknown at the start.
func TestSetActiveScheduleRequestDoesNotStoreAHandleDroppedMeanwhile(t *testing.T) {
	t.Parallel()
	port := &droppingSchedulePort{keep: []thermo.Schedule{builtInSchedule(2)}}
	cfg := mschConfig(withSchedules(builtInSchedule(1), builtInSchedule(2)))
	cfg.SchedulePort = port
	srv := newMSCH(t, cfg)
	port.srv = srv
	err := invokeSetActiveSchedule(srv, []byte{1})
	if err == nil {
		t.Fatal("the request was accepted for a schedule the device dropped")
	}
	if got := statusOfErr(t, err); got != im.StatusInvalidCommand {
		t.Errorf("status %v, want INVALID_COMMAND", got)
	}
	if h := srv.ActiveScheduleHandle(); h != nil {
		t.Errorf("ActiveScheduleHandle %x names a schedule the list no longer holds", h)
	}
}
