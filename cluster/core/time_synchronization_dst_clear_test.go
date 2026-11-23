// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	tsdef "thingfabric.dev/go-fabric/cluster/spec/timesynchronization"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// refusingSettings is a settings store whose writes fail while fail is set.
type refusingSettings struct {
	store.Settings
	fail bool
	vals map[string]string
}

func (s *refusingSettings) GetSetting(_ context.Context, k string) (string, bool, error) {
	v, ok := s.vals[k]
	return v, ok, nil
}

func (s *refusingSettings) SetSetting(_ context.Context, k, v string) error {
	if s.fail {
		return errors.New("store down")
	}
	s.vals[k] = v
	return nil
}

// TestSetDSTOffsetEmptyStoreFailureIsConstraintError (R3-store-3151):
// SetDSTOffset with an empty list whose store write fails answers
// CONSTRAINT_ERROR, as its non-empty branch does, and reports nothing;
// the list is emptied in memory before the store, as chip's ClearDSTOffset
// (by_design BD-chip-TimeSync-StateBeforePersist).
func TestSetDSTOffsetEmptyStoreFailureIsConstraintError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &refusingSettings{vals: map[string]string{}}
	srv, err := core.NewTimeSynchronizationWith(core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone, Store: st})
	if err != nil {
		t.Fatal(err)
	}
	var changes [][]uint32
	srv.OnAttributeChange(func(c contract.Change) { changes = append(changes, c.Attributes) })
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetDstOffset, tsdef.SetDstOffsetRequest{DstOffset: []tsdef.DSTOffsetStruct{{Offset: 3600, ValidUntil: spec.NullOf[uint64]()}}}); err != nil {
		t.Fatal(err)
	}
	changes = nil
	st.fail = true
	_, err = srv.MatterInvoke(ctx, tsdef.CmdSetDstOffset, tsdef.SetDstOffsetRequest{})
	if got := spec.StatusOf(err); got != im.StatusConstraintError {
		t.Errorf("SetDSTOffset([]) with the store down = %v (status %v), want CONSTRAINT_ERROR", err, got)
	}
	if len(changes) != 0 {
		t.Errorf("SetDSTOffset([]) with the store down reported %v", changes)
	}
	st.fail = false
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetDstOffset, tsdef.SetDstOffsetRequest{}); err != nil {
		t.Errorf("SetDSTOffset([]) with the store up = %v, want SUCCESS", err)
	}
}
