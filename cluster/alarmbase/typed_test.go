// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarmbase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dwa "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
)

func alarm[T any](v T) *T { return &v }

// TestTypedAlarmBaseServer drives the self-held DishwasherAlarm server
// through its typed server: Set follows chip's SetMask / SetState and emits
// Notify, Supported is fixed, a host handler replaces Reset, an event the
// cluster defines goes out, and every attribute is read-only.
func TestTypedAlarmBaseServer(t *testing.T) {
	t.Parallel()
	srv, err := alarmbase.NewDishwasherAlarm(alarmbase.Config{
		Features: uint32(dwa.FeatureReset), Supported: 0x3, Latch: 0x1, ModifyEnabledAlarms: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	s := dwa.Wrap(srv)
	var changed [][]uint32
	defer s.OnChanged(func(_ dwa.State, ids []uint32) { changed = append(changed, ids) })()

	if err := s.Set(dwa.Patch{Mask: alarm(dwa.AlarmBitmap(0x3)), State: alarm(dwa.AlarmBitmap(0x1))}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.Mask != 0x3 || st.State != 0x1 || st.Supported != 0x3 {
		t.Errorf("state %+v", st)
	}
	if len(changed) == 0 || len(em.events) != 1 || em.events[0].event != dwa.EventNotify {
		t.Errorf("changes %v, events %+v", changed, em.events)
	}
	// All or nothing: a State outside the Mask leaves the Mask.
	if err := s.Set(dwa.Patch{Mask: alarm(dwa.AlarmBitmap(0x1)), State: alarm(dwa.AlarmBitmap(0x2))}); !errors.Is(err, alarmbase.ErrNotSupported) || s.Get().Mask != 0x3 {
		t.Errorf("a refused patch: %v, Mask %v", err, s.Get().Mask)
	}
	if err := s.Set(dwa.Patch{Supported: alarm(dwa.AlarmBitmap(0x7))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Supported: %v", err)
	}

	var resets []dwa.ResetRequest
	if err := s.HandleReset(func(_ context.Context, req dwa.ResetRequest) (struct{}, error) {
		resets = append(resets, req)
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), dwa.CmdReset, &dwa.ResetRequest{Alarms: 0x1}); err != nil {
		t.Fatal(err)
	}
	if len(resets) != 1 || s.Get().State != 0x1 {
		t.Errorf("host Reset saw %v, State %v (the server's own Reset ran)", resets, s.Get().State)
	}
	if err := s.HandleReset(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), dwa.CmdReset, dwa.ResetRequest{Alarms: 0x1}); err != nil || s.Get().State != 0 {
		t.Errorf("handed back: %v, State %v", err, s.Get().State)
	}

	if err := s.EmitNotify(7, dwa.NotifyEvent{Active: 0x1}); err != nil {
		t.Fatal(err)
	}
	if last := em.events[len(em.events)-1]; last.endpoint != 7 || last.event != dwa.EventNotify {
		t.Errorf("event %+v", last)
	}
	if err := s.OnWrite(func(context.Context, dwa.State, uint32) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(context.Background(), dwa.AttrMask, uint64(1)); err == nil {
		t.Error("a write of the read-only Mask succeeded")
	}
}
