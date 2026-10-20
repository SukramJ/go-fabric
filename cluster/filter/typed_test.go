// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package filter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
)

func ptrOf[T any](v T) *T { return &v }

// TestTypedFilterServer drives the self-held filter server through its
// typed server: Set stores and reports the state, the configuration's
// attributes are fixed, a host handler replaces ResetCondition, the
// cluster defines no event, and OnWrite vetoes a LastChangedTime write
// before the writer sees it.
func TestTypedFilterServer(t *testing.T) {
	t.Parallel()
	h := &host{}
	srv := full(t, h)
	s := hepa.Wrap(srv)
	var changed [][]uint32
	defer s.OnChanged(func(_ hepa.State, ids []uint32) { changed = append(changed, ids) })()

	if err := s.Set(hepa.Patch{Condition: ptrOf(uint8(20)), ChangeIndication: ptrOf(hepa.ChangeIndicationCritical)}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.Condition != 20 || st.ChangeIndication != hepa.ChangeIndicationCritical || srv.State().Condition != 20 {
		t.Errorf("state %+v", st)
	}
	if len(changed) != 1 {
		t.Errorf("changes %v", changed)
	}
	if err := s.Set(hepa.Patch{DegradationDirection: ptrOf(hepa.DegradationDirectionUp)}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("DegradationDirection: %v", err)
	}

	host := 0
	if err := s.HandleResetCondition(func(context.Context, hepa.ResetConditionRequest) (struct{}, error) {
		host++
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), hepa.CmdResetCondition, nil); err != nil || host != 1 || h.resets != 0 {
		t.Errorf("host ResetCondition: %v, host %d, resetter %d", err, host, h.resets)
	}
	if err := spec.EmitOn(srv, 1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event the cluster does not define: %v", err)
	}

	refusal := errors.New("not now")
	if err := s.OnWrite(func(context.Context, hepa.State, uint32) error { return refusal }); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(context.Background(), hepa.AttrLastChangedTime, uint64(1000)); !errors.Is(err, refusal) || len(h.written) != 0 {
		t.Errorf("a vetoed write: %v, writer saw %v", err, h.written)
	}
}
