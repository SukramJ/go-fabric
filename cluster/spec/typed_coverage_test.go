// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/tlv"
)

// inlineQueue is a Queue that keeps what it is handed and runs it on
// demand.
type inlineQueue struct{ jobs []func() }

func (q *inlineQueue) Enqueue(fn func()) { q.jobs = append(q.jobs, fn) }

func (q *inlineQueue) drain() {
	for _, fn := range q.jobs {
		fn()
	}
	q.jobs = nil
}

// emitted is an EventEmitter that counts its events.
type countingEmitter struct{ events []uint32 }

func (c *countingEmitter) MatterEmitEvent(_ uint16, _, event uint32, _ any, _ contract.EventPriority) {
	c.events = append(c.events, event)
}

// TestHooksOverAnInstance: the hooks a typed server embeds hand commands
// to the host, refuse the events and the commands the instance does not
// have, and run the write veto after the server's own checks.
func TestHooksOverAnInstance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var h spec.Hooks
	h.Init(hepaInstance(t))

	if _, handled, _ := h.Invoke(ctx, hepa.CmdResetCondition, nil); handled {
		t.Fatal("handled without a handler")
	}
	if err := h.Handle(0x7F, func(context.Context, any) (any, error) { return nil, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle(0x7F) = %v, want ErrNotAccepted", err)
	}
	if err := h.Handle(hepa.CmdResetCondition, func(_ context.Context, fields any) (any, error) { return fields, nil }); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resp, handled, err := h.Invoke(ctx, hepa.CmdResetCondition, "x"); !handled || resp != "x" || err != nil {
		t.Errorf("Invoke = %v, %v, %v", resp, handled, err)
	}

	// The instance has no event: the emit is refused, with or without an
	// emitter.
	if err := h.Emit(1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit = %v, want ErrNotEmitted", err)
	}
	h.SetMatterEventEmitter(&countingEmitter{})
	if err := h.Emit(1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit with an emitter = %v, want ErrNotEmitted", err)
	}

	if err := h.CheckWrite(ctx, hepa.AttrCondition, uint8(1)); err != nil {
		t.Errorf("CheckWrite without a veto = %v", err)
	}
	veto := errors.New("vetoed")
	h.SetBeforeWrite(func(_ context.Context, attrID uint32, value any) error {
		if attrID != hepa.AttrCondition || value != uint8(1) {
			t.Errorf("the veto saw 0x%04X %v", attrID, value)
		}
		return veto
	})
	if err := h.CheckWrite(ctx, hepa.AttrCondition, uint8(1)); !errors.Is(err, veto) {
		t.Errorf("CheckWrite = %v, want the veto", err)
	}
	h.SetBeforeWrite(nil)
	if err := h.CheckWrite(ctx, hepa.AttrCondition, uint8(1)); err != nil {
		t.Errorf("CheckWrite after removing the veto = %v", err)
	}
}

// TestHookErrorNamesTheClusterAndTheHook: nil stays nil.
func TestHookErrorNamesTheClusterAndTheHook(t *testing.T) {
	t.Parallel()

	if err := spec.HookError("OnOff", "OnWrite", nil); err != nil {
		t.Errorf("HookError(nil) = %v", err)
	}
	cause := errors.New("not accepted")
	err := spec.HookError("OnOff", "OnWrite", cause)
	if !errors.Is(err, cause) || err.Error() != "OnOff OnWrite: not accepted" {
		t.Errorf("HookError = %v", err)
	}
}

// TestWrapperOnAQueueRunsTheAsynchronousCallbacks: the callbacks run on the
// queue the wrapper holds, one change one job; without a queue the
// subscription is refused, and a nil callback subscribes to nothing.
func TestWrapperOnAQueueRunsTheAsynchronousCallbacks(t *testing.T) {
	t.Parallel()

	srv := hepaServer(t, spec.ServerConfig{})
	var states []hepaState

	if _, err := spec.OnChangedAsyncOf(spec.NewWrapper(srv), func(hepaState, contract.Change) {}); !errors.Is(err, spec.ErrNoQueue) {
		t.Errorf("without a queue: %v, want ErrNoQueue", err)
	}
	q := &inlineQueue{}
	w := spec.NewWrapperOn(srv, q)
	if w.Underlying() != contract.ClusterServer(srv) {
		t.Error("the wrapper does not hold its server")
	}
	unsub, err := spec.OnChangedAsyncOf[hepaState](w, nil)
	if err != nil || unsub == nil {
		t.Fatalf("nil callback: unsubscribe %v, err %v", unsub != nil, err)
	}
	unsub()

	unsub, err = spec.OnChangedAsyncOf(w, func(s hepaState, _ contract.Change) { states = append(states, s) })
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Set(hepa.AttrCondition, uint8(9)); err != nil {
		t.Fatal(err)
	}
	if len(states) != 0 || len(q.jobs) != 1 {
		t.Fatalf("the callback ran on the reporting goroutine: %d states, %d jobs", len(states), len(q.jobs))
	}
	q.drain()
	if len(states) != 1 || states[0].Condition != 9 {
		t.Errorf("states %+v, want one with Condition 9", states)
	}
	unsub()
	if err := srv.Set(hepa.AttrCondition, uint8(10)); err != nil {
		t.Fatal(err)
	}
	if len(q.jobs) != 0 {
		t.Error("a change after the unsubscribe was queued")
	}
}

// TestSetPatchOnAServerWithASetter: the patch's values are set as a whole.
func TestSetPatchOnAServerWithASetter(t *testing.T) {
	t.Parallel()

	srv := hepaServer(t, spec.ServerConfig{})
	if err := spec.SetPatch(srv, hepaPatch{Condition: ptr(uint8(12))}); err != nil {
		t.Fatalf("SetPatch: %v", err)
	}
	if got := spec.StateOf[hepaState](srv).Condition; got != 12 {
		t.Errorf("Condition %d, want 12", got)
	}
}

// TestConvertReadsAStoredValueAsTheGeneratedType: the conversion the typed
// form reads every field through.
func TestConvertReadsAStoredValueAsTheGeneratedType(t *testing.T) {
	t.Parallel()

	v, ok := spec.Convert(uint8(2), reflect.TypeFor[hepa.ChangeIndicationEnum]())
	if !ok || v.Interface() != hepa.ChangeIndicationEnum(2) {
		t.Errorf("Convert = %v, %v", v, ok)
	}
	if _, ok := spec.Convert("two", reflect.TypeFor[hepa.ChangeIndicationEnum]()); ok {
		t.Error("a string was converted to an enum")
	}
}

// TestBoundIsInt: a bound is a number when it names nothing and states no
// expression; nil is not.
func TestBoundIsInt(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		b    *spec.Bound
		want bool
	}{
		{"nil", nil, false},
		{"number", &spec.Bound{Int: 3}, true},
		{"reference", &spec.Bound{Ref: "maxMeasuredValue"}, false},
		{"expression", &spec.Bound{Expr: "a - 1"}, false},
	} {
		if got := c.b.IsInt(); got != c.want {
			t.Errorf("%s: IsInt = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestInstanceIsTimedCommand: the timed gate reads the definition.
func TestInstanceIsTimedCommand(t *testing.T) {
	t.Parallel()

	inst := hepaInstance(t)
	if got := inst.IsTimedCommand(hepa.CmdResetCondition); got != inst.IsTimed(hepa.CmdResetCondition) {
		t.Errorf("IsTimedCommand = %v, IsTimed = %v", got, inst.IsTimed(hepa.CmdResetCondition))
	}
}

// TestPutOptionalLeavesOutANilField: a present value is encoded, a nil one
// is not.
func TestPutOptionalLeavesOutANilField(t *testing.T) {
	t.Parallel()

	encode := func(v *uint8) []byte {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		spec.PutOptional(enc, tlv.ContextTag(1), v, spec.PutUint[uint8])
		_ = enc.EndContainer()
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	absent, present := encode(nil), encode(ptr(uint8(5)))
	if len(present) <= len(absent) {
		t.Errorf("present %x is not longer than absent %x", present, absent)
	}
}
