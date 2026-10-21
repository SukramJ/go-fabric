// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package onoff_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
)

type nopEmitter struct{ n int }

func (e *nopEmitter) MatterEmitEvent(uint16, uint32, uint32, any, contract.EventPriority) { e.n++ }

func lightingServer(t *testing.T, sink onoff.Sink) *onoff.Server {
	t.Helper()
	srv, err := onoff.New(onoff.Config{Features: onoffdef.FeatureLighting, Sink: sink, TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestTypedOnOffServer is the typed server's one shape over onoff.Server:
// Set stores and reports without the sink, a host handler replaces the
// command (the server's state does not move, the sink is not told) and nil
// hands it back, OnOff defines no event, OnWrite vetoes a controller's
// write before it is stored.
func TestTypedOnOffServer(t *testing.T) {
	t.Parallel()
	sink := &recSink{}
	srv := lightingServer(t, sink)
	s := onoffdef.Wrap(srv)
	ctx := context.Background()

	var changed [][]uint32
	defer s.OnChanged(func(_ onoffdef.State, ids []uint32) { changed = append(changed, ids) })()
	on := true
	if err := s.Set(onoffdef.Patch{OnOff: &on, OnTime: new(uint16(5))}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); !st.OnOff || st.OnTime != 5 {
		t.Errorf("state %+v", st)
	}
	if len(changed) != 1 || !slices.Contains(changed[0], onoffdef.AttrOnOff) || len(sink.calls) != 0 {
		t.Errorf("changes %v, sink %v", changed, sink.calls)
	}
	if err := s.Set(onoffdef.Patch{OffWaitTime: new(uint16(1)), StartUpOnOff: new(onoffdef.StartUpOnOffEnum(9))}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("an out-of-range StartUpOnOff: %v", err)
	}
	if st := s.Get(); st.OffWaitTime != 0 {
		t.Errorf("a refused patch stored OffWaitTime %d", st.OffWaitTime)
	}

	toggled := 0
	if err := s.HandleToggle(func(context.Context, onoffdef.ToggleRequest) (struct{}, error) { toggled++; return struct{}{}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, onoff.CmdToggle, nil); err != nil {
		t.Fatal(err)
	}
	if toggled != 1 || !s.Get().OnOff || len(sink.calls) != 0 {
		t.Errorf("host Toggle: called %d, OnOff %t, sink %v", toggled, s.Get().OnOff, sink.calls)
	}
	if err := s.HandleToggle(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, onoff.CmdToggle, nil); err != nil {
		t.Fatal(err)
	}
	if s.Get().OnOff {
		t.Error("the server's own Toggle did not run after nil")
	}

	em := &nopEmitter{}
	srv.SetMatterEventEmitter(em)
	if err := spec.EmitOn(srv, 1, 0x00, nil); !errors.Is(err, spec.ErrNotEmitted) || em.n != 0 {
		t.Errorf("Emit: %v, emitted %d", err, em.n)
	}

	refusal := errors.New("not now")
	if err := s.OnWrite(func(_ context.Context, next onoffdef.State, attrID uint32) error {
		if attrID == onoffdef.AttrOnTime && next.OnTime > 100 {
			return refusal
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := s.Get().OnTime
	if err := srv.MatterWrite(ctx, onoffdef.AttrOnTime, uint64(200)); !errors.Is(err, refusal) {
		t.Errorf("vetoed write: %v", err)
	}
	if s.Get().OnTime != before {
		t.Errorf("a vetoed write stored OnTime %d", s.Get().OnTime)
	}
}

// TestHandleRefusesAnUnacceptedCommand: Toggle with OFFONLY is not
// accepted, so its handler is refused at registration.
func TestHandleRefusesAnUnacceptedCommand(t *testing.T) {
	t.Parallel()
	srv, err := onoff.New(onoff.Config{Features: onoffdef.FeatureOffOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := onoffdef.Wrap(srv).HandleToggle(func(context.Context, onoffdef.ToggleRequest) (struct{}, error) { return struct{}{}, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Toggle with OFFONLY: %v", err)
	}
}

// TestOnWithTimedOffTakesAPointer: the generated request by pointer is the
// request; a nil pointer is INVALID_COMMAND.
func TestOnWithTimedOffTakesAPointer(t *testing.T) {
	t.Parallel()
	srv := lightingServer(t, nil)
	ctx := context.Background()
	if _, err := srv.MatterInvoke(ctx, onoff.CmdOnWithTimedOff, &onoffdef.OnWithTimedOffRequest{OnTime: 3}); err != nil {
		t.Fatal(err)
	}
	if !srv.OnOff() {
		t.Error("OnWithTimedOff by pointer left the light off")
	}
	if _, err := srv.MatterInvoke(ctx, onoff.CmdOnWithTimedOff, (*onoffdef.OnWithTimedOffRequest)(nil)); err == nil {
		t.Error("a nil request accepted")
	}
}
