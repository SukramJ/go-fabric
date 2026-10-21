// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	"github.com/SukramJ/go-fabric/contract"
)

func hepaInstance(t *testing.T) *spec.Instance {
	t.Helper()
	inst, err := spec.New(hepa.Definition, spec.Options{
		Features: uint32(hepa.FeatureCondition),
		Commands: []uint32{hepa.CmdResetCondition},
	})
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// TestHostHandlers: a registered handler answers its command, nil hands it
// back, and a command the instance does not accept is refused.
func TestHostHandlers(t *testing.T) {
	t.Parallel()
	inst := hepaInstance(t)
	var h spec.HostHandlers
	ctx := context.Background()
	if _, handled, _ := h.Invoke(ctx, hepa.CmdResetCondition, nil); handled {
		t.Fatal("handled without a handler")
	}
	if err := h.Register(inst, 0x7F, func(context.Context, any) (any, error) { return nil, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("an unaccepted command: %v", err)
	}
	refusal := errors.New("busy")
	if err := h.Register(inst, hepa.CmdResetCondition, func(_ context.Context, fields any) (any, error) { return fields, refusal }); err != nil {
		t.Fatal(err)
	}
	resp, handled, err := h.Invoke(ctx, hepa.CmdResetCondition, "payload")
	if !handled || resp != "payload" || !errors.Is(err, refusal) {
		t.Errorf("Invoke = %v, %t, %v", resp, handled, err)
	}
	if err := h.Register(inst, hepa.CmdResetCondition, nil); err != nil {
		t.Fatal(err)
	}
	if _, handled, _ := h.Invoke(ctx, hepa.CmdResetCondition, nil); handled {
		t.Error("handled after nil handed the command back")
	}
}

// TestEvents: an event of the EventList reaches the emitter with the
// definition's priority; one outside it is refused; without an emitter the
// event is dropped.
func TestEvents(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(smokecoalarm.Definition, spec.Options{Features: uint32(smokecoalarm.FeatureSmokeAlarm)})
	if err != nil {
		t.Fatal(err)
	}
	var ev spec.Events
	if err := ev.Emit(inst, 1, smokecoalarm.EventLowBattery, nil); err != nil {
		t.Errorf("no emitter: %v", err)
	}
	em := &emitter{}
	ev.SetEmitter(em)
	if err := ev.Emit(inst, 3, smokecoalarm.EventSmokeAlarm, "data"); err != nil {
		t.Fatal(err)
	}
	if err := ev.Emit(inst, 3, smokecoalarm.EventCoAlarm, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event without its feature: %v", err)
	}
	if len(em.events) != 1 || em.events[0].endpoint != 3 || em.events[0].cluster != smokecoalarm.ClusterID ||
		em.events[0].event != smokecoalarm.EventSmokeAlarm || em.events[0].data != "data" ||
		em.events[0].priority != inst.EventPriority(smokecoalarm.EventSmokeAlarm) {
		t.Errorf("emitted %+v", em.events)
	}
}

// TestWriteVeto: no veto admits; a veto sees the write and its error is
// returned as given; nil removes it.
func TestWriteVeto(t *testing.T) {
	t.Parallel()
	var w spec.WriteVeto
	ctx := context.Background()
	if err := w.Check(ctx, 1, uint8(2)); err != nil {
		t.Errorf("no veto: %v", err)
	}
	refusal := errors.New("no")
	var seen []any
	w.Set(func(_ context.Context, attrID uint32, value any) error {
		seen = append(seen, attrID, value)
		return refusal
	})
	if err := w.Check(ctx, 7, uint8(9)); err != refusal { //nolint:errorlint // returned as given
		t.Errorf("veto = %v", err)
	}
	if len(seen) != 2 || seen[0] != uint32(7) || seen[1] != uint8(9) {
		t.Errorf("the veto saw %v", seen)
	}
	w.Set(nil)
	if err := w.Check(ctx, 7, uint8(9)); err != nil {
		t.Errorf("removed veto: %v", err)
	}
}

// heldServer is a self-held server over a hepa store, its own
// ResetCondition setting Condition to 100.
type heldServer struct {
	spec.SelfHeld
	srv *spec.Server
}

func newHeldServer(t *testing.T) *heldServer {
	t.Helper()
	s := &heldServer{}
	s.srv = hepaServer(t, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{
		hepa.CmdResetCondition: func(context.Context, any) (any, error) {
			return nil, s.srv.Set(hepa.AttrCondition, uint8(100))
		},
	}})
	s.SelfHeld = spec.NewSelfHeld(s.srv)
	return s
}

// TestSelfHeld: the four interfaces over the store — Set stores, a host
// handler replaces the server's command and nil hands it back
// (InvokeHeld), Emit reaches the store's emitter, SetBeforeWrite vetoes a
// controller's write.
func TestSelfHeld(t *testing.T) {
	t.Parallel()
	s := newHeldServer(t)
	ctx := context.Background()
	var _ spec.AttributeSetter = &s.SelfHeld
	var _ spec.CommandRegistrar = &s.SelfHeld
	var _ spec.EventEmitting = &s.SelfHeld
	var _ spec.BeforeWriteSetter = &s.SelfHeld
	var _ contract.EventReceiver = &s.SelfHeld

	if err := s.SetAttributes(map[uint32]any{hepa.AttrCondition: uint8(10)}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.srv.Value(hepa.AttrCondition); v != uint8(10) {
		t.Errorf("Condition %v after Set", v)
	}

	// The server's own command, then the host's in its place, then back.
	if _, err := spec.InvokeHeld(ctx, &s.SelfHeld, hepa.CmdResetCondition, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.srv.Value(hepa.AttrCondition); v != uint8(100) {
		t.Errorf("Condition %v after the server's ResetCondition", v)
	}
	_ = s.SetAttributes(map[uint32]any{hepa.AttrCondition: uint8(10)})
	called := false
	if err := s.Handle(hepa.CmdResetCondition, func(context.Context, any) (any, error) { called = true; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := spec.InvokeHeld(ctx, &s.SelfHeld, hepa.CmdResetCondition, nil); err != nil || !called {
		t.Fatalf("host handler: %v, called %t", err, called)
	}
	if v, _ := s.srv.Value(hepa.AttrCondition); v != uint8(10) {
		t.Errorf("the server's own ResetCondition ran beside the host's: Condition %v", v)
	}
	if err := s.Handle(hepa.CmdResetCondition, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := spec.InvokeHeld(ctx, &s.SelfHeld, hepa.CmdResetCondition, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.srv.Value(hepa.AttrCondition); v != uint8(100) {
		t.Errorf("handed back: Condition %v", v)
	}
	if err := s.Handle(0x7F, nil); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("an unaccepted command: %v", err)
	}

	// Hepa defines no event: Emit refuses through the store.
	s.SetMatterEventEmitter(&emitter{})
	if err := s.Emit(1, 0x00, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit: %v", err)
	}

	refusal := errors.New("locked")
	s.SetBeforeWrite(func(context.Context, uint32, any) error { return refusal })
	if err := s.srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(5)); !errors.Is(err, refusal) {
		t.Errorf("vetoed write: %v", err)
	}
}

// TestRequestOf: a generated request by value or by non-nil pointer; a
// nil pointer or another type is no request.
func TestRequestOf(t *testing.T) {
	t.Parallel()
	type req struct{ A uint8 }
	if r, ok := spec.RequestOf[req](req{A: 1}); !ok || r.A != 1 {
		t.Errorf("value: %v, %t", r, ok)
	}
	if r, ok := spec.RequestOf[req](&req{A: 2}); !ok || r.A != 2 {
		t.Errorf("pointer: %v, %t", r, ok)
	}
	if _, ok := spec.RequestOf[req]((*req)(nil)); ok {
		t.Error("a nil pointer is a request")
	}
	if _, ok := spec.RequestOf[req]("other"); ok {
		t.Error("another type is a request")
	}
	if _, ok := spec.RequestOf[req](nil); ok {
		t.Error("nil is a request")
	}
}

// TestServerHandleRefusesAndRestores: Handle refuses a command the
// instance does not accept; nil restores the handler construction gave.
func TestServerHandleRefusesAndRestores(t *testing.T) {
	t.Parallel()
	s := newHeldServer(t)
	ctx := context.Background()
	if err := s.srv.Handle(0x7F, nil); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("unaccepted: %v", err)
	}
	if err := s.srv.Handle(hepa.CmdResetCondition, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.srv.MatterInvoke(ctx, hepa.CmdResetCondition, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.srv.Value(hepa.AttrCondition); v != uint8(40) {
		t.Errorf("the replaced handler ran: Condition %v", v)
	}
	if err := s.srv.Handle(hepa.CmdResetCondition, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.srv.MatterInvoke(ctx, hepa.CmdResetCondition, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.srv.Value(hepa.AttrCondition); v != uint8(100) {
		t.Errorf("the restored handler did not run: Condition %v", v)
	}
}

// TestCheckSet: a value for an unserved attribute or outside the
// constraint is refused; an accepted one comes back in its stored form.
func TestCheckSet(t *testing.T) {
	t.Parallel()
	inst := hepaInstance(t)
	if _, err := inst.CheckSet(hepa.AttrReplacementProductList, nil, nil); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("unserved: %v", err)
	}
	if _, err := inst.CheckSet(hepa.AttrCondition, uint8(101), nil); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("out of constraint: %v", err)
	}
	if v, err := inst.CheckSet(hepa.AttrCondition, uint64(50), nil); err != nil || v != uint8(50) {
		t.Errorf("accepted: %v (%T), %v", v, v, err)
	}
}

// TestSetPatchRefusesAServerWithoutSetter: SetPatch on a server that is no
// AttributeSetter.
func TestSetPatchRefusesAServerWithoutSetter(t *testing.T) {
	t.Parallel()
	srv := struct{ contract.ClusterServer }{hepaServer(t, spec.ServerConfig{})}
	if err := spec.SetPatch(srv, hepa.Patch{}); !errors.Is(err, spec.ErrNotSettable) {
		t.Errorf("SetPatch: %v", err)
	}
}
