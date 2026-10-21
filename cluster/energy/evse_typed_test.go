// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	evsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
)

// TestTypedEvseServer drives the self-held EnergyEvse server through its
// typed server: Set stores the host's attributes (the charge limit recomputed),
// the state machine's attributes are refused, a host handler replaces
// Disable, an event outside the selection is refused, and OnWrite vetoes
// a controller's write.
func TestTypedEvseServer(t *testing.T) {
	t.Parallel()
	srv, _, _ := newEvse(t, evsedef.FeatureChargingPreferences|evsedef.FeatureSoCReporting)
	basicFunctionality(t, srv)
	s := evsedef.Wrap(srv)
	changes := 0
	defer s.OnChanged(func(evsedef.State, []uint32) { changes++ })()

	if err := s.Set(evsedef.Patch{CircuitCapacity: typedPtr(int64(16000)), StateOfCharge: typedPtr(uint8(40))}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.CircuitCapacity != 16000 || st.StateOfCharge == nil || *st.StateOfCharge != 40 || changes == 0 {
		t.Errorf("state %+v, %d changes", st, changes)
	}
	if err := s.Set(evsedef.Patch{MaximumChargeCurrent: typedPtr(int64(1))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("MaximumChargeCurrent: %v", err)
	}

	disabled := 0
	if err := s.HandleDisable(func(context.Context, evsedef.DisableRequest) (struct{}, error) {
		disabled++
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), evsedef.CmdDisable, nil); err != nil || disabled != 1 {
		t.Errorf("host Disable: %v, %d", err, disabled)
	}
	if err := s.EmitRfid(2, evsedef.RfidEvent{Uid: []byte{1}}); err == nil {
		t.Error("an Rfid event without the RFID feature went out")
	}

	refusal := errors.New("not now")
	if err := s.OnWrite(func(context.Context, evsedef.State, uint32) error { return refusal }); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(context.Background(), evsedef.AttrUserMaximumChargeCurrent, int64(8000)); !errors.Is(err, refusal) {
		t.Errorf("a vetoed write: %v", err)
	}
}
