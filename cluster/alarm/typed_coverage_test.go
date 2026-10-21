// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarm_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/spec"
	smokedef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	"github.com/SukramJ/go-fabric/im"
)

// notifyingDevice is a host alarm whose source pushes its changes.
type notifyingDevice struct {
	*device
	cbs []func()
}

func (n *notifyingDevice) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	n.cbs = append(n.cbs, cb)
	return func() { n.cbs = nil }
}

func (n *notifyingDevice) fire() {
	for _, cb := range slices.Clone(n.cbs) {
		cb()
	}
}

// TestOptionalForNamesTheOptionalBitsOfTheAttributes: each optional
// attribute sets its own bit, every other id is ignored.
func TestOptionalForNamesTheOptionalBitsOfTheAttributes(t *testing.T) {
	t.Parallel()

	if got := alarm.OptionalFor(nil); got != 0 {
		t.Errorf("OptionalFor(nil) = %v, want 0", got)
	}
	got := alarm.OptionalFor([]uint32{
		alarm.AttrExpressedState, // mandatory: ignored
		alarm.AttrDeviceMuted, alarm.AttrInterconnectSmokeAlarm, alarm.AttrInterconnectCOAlarm,
		alarm.AttrContaminationState, alarm.AttrSmokeSensitivityLevel, alarm.AttrExpiryDate, alarm.AttrUnmounted,
	})
	if got != everyOptional {
		t.Errorf("OptionalFor(all) = %v, want %v", got, everyOptional)
	}
	if got := alarm.OptionalFor([]uint32{alarm.AttrExpiryDate, 0xFFFF}); got != alarm.OptionalExpiryDate {
		t.Errorf("OptionalFor(ExpiryDate, unknown) = %v, want ExpiryDate", got)
	}
}

// TestHostSetRefusesEveryAttribute: the state is the source's.
func TestHostSetRefusesEveryAttribute(t *testing.T) {
	t.Parallel()

	srv := newFull(t, &device{})
	if err := srv.SetAttributes(nil); err != nil {
		t.Errorf("SetAttributes(nil) = %v, want nil", err)
	}
	err := srv.SetAttributes(map[uint32]any{alarm.AttrDeviceMuted: uint8(1)})
	if !errors.Is(err, spec.ErrNotHeld) {
		t.Fatalf("SetAttributes = %v, want ErrNotHeld", err)
	}
	if !strings.Contains(err.Error(), "0x") {
		t.Errorf("error %q does not name the attribute", err)
	}
}

// TestHandleReplacesSelfTestRequest: a host handler replaces the server's
// busy check and the source's test, nil hands the command back, and a
// command the cluster does not accept is refused.
func TestHandleReplacesSelfTestRequest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d)
	d.set(func(s *smokedef.State) { s.SmokeState = alarm.AlarmCritical })

	calls := 0
	if err := srv.Handle(alarm.CmdSelfTestRequest, func(context.Context, any) (any, error) {
		calls++
		return nil, nil
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Alarming would be BUSY on the server's own logic.
	if _, err := srv.MatterInvoke(ctx, alarm.CmdSelfTestRequest, nil); err != nil || calls != 1 || d.selfTests != 0 {
		t.Errorf("host handler: err %v, calls %d, self-tests %d; want the handler only", err, calls, d.selfTests)
	}
	if err := srv.Handle(alarm.CmdSelfTestRequest, nil); err != nil {
		t.Fatalf("Handle(nil): %v", err)
	}
	_, err := srv.MatterInvoke(ctx, alarm.CmdSelfTestRequest, nil)
	if statusOf(t, err) != im.StatusBusy {
		t.Errorf("after nil: %v, want Busy", err)
	}
	if !strings.Contains(err.Error(), "while alarming or testing") {
		t.Errorf("busy message = %q", err.Error())
	}
	if err := srv.Handle(0x42, func(context.Context, any) (any, error) { return nil, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle(0x42) = %v, want ErrNotAccepted", err)
	}
}

// TestEmitServedAndUnservedEvents: an event of the EventList reaches the
// emitter, one outside it is ErrNotEmitted.
func TestEmitServedAndUnservedEvents(t *testing.T) {
	t.Parallel()

	srv, err := alarm.NewServer(alarm.Config{Source: &device{}, Features: alarm.FeatureCOAlarm})
	if err != nil {
		t.Fatal(err)
	}
	// Without an emitter a served event is dropped without an error.
	if err := srv.Emit(1, alarm.EventLowBattery, nil); err != nil {
		t.Errorf("Emit without an emitter = %v", err)
	}
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	if err := srv.Emit(3, alarm.EventLowBattery, "data"); err != nil {
		t.Fatalf("Emit(LowBattery) = %v", err)
	}
	got := rec.take()
	if len(got) != 1 || got[0].endpoint != 3 || got[0].event != alarm.EventLowBattery || got[0].data != "data" {
		t.Errorf("emitted %+v, want one LowBattery on endpoint 3", got)
	}
	// SmokeAlarm needs the SMOKE feature, which this server lacks.
	if err := srv.Emit(3, alarm.EventSmokeAlarm, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit(SmokeAlarm) = %v, want ErrNotEmitted", err)
	}
	if got := rec.take(); len(got) != 0 {
		t.Errorf("an unserved event was emitted: %v", ids(got))
	}
}

// TestSetBeforeWriteVetoesTheSensitivityWrite: the veto refuses a
// controller's write before the source takes it, and nil removes it.
func TestSetBeforeWriteVetoesTheSensitivityWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d)
	veto := errors.New("vetoed")
	var seenAttr uint32
	srv.SetBeforeWrite(func(_ context.Context, attrID uint32, _ any) error {
		seenAttr = attrID
		return veto
	})
	before := srv.MatterDataVersion()
	if err := srv.MatterWrite(ctx, alarm.AttrSmokeSensitivityLevel, uint64(alarm.SensitivityHigh)); !errors.Is(err, veto) {
		t.Fatalf("MatterWrite = %v, want the veto", err)
	}
	if seenAttr != alarm.AttrSmokeSensitivityLevel || len(d.written) != 0 || srv.MatterDataVersion() != before {
		t.Errorf("attr 0x%X, source saw %v, version moved %v; want a refusal with no effect",
			seenAttr, d.written, srv.MatterDataVersion() != before)
	}
	srv.SetBeforeWrite(nil)
	if err := srv.MatterWrite(ctx, alarm.AttrSmokeSensitivityLevel, uint64(alarm.SensitivityHigh)); err != nil {
		t.Errorf("MatterWrite after removing the veto = %v", err)
	}
}

// TestOnMatterValueChangedRefreshesThenNotifies: a source that pushes
// changes makes the server emit the events of the change before the
// subscriber's callback runs; a source that does not, or a nil callback,
// gives a no-op unsubscribe.
func TestOnMatterValueChangedRefreshesThenNotifies(t *testing.T) {
	t.Parallel()

	nd := &notifyingDevice{device: &device{}}
	srv, err := alarm.NewServer(alarm.Config{
		Source:   nd,
		Features: alarm.FeatureSmokeAlarm | alarm.FeatureCOAlarm,
		Optional: alarm.OptionalDeviceMuted,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)

	var atCallback []uint32
	notified := 0
	unsub := srv.OnMatterValueChanged(func() {
		notified++
		atCallback = ids(rec.take())
	})
	nd.set(func(s *smokedef.State) { s.SmokeState = alarm.AlarmCritical })
	nd.fire()
	if notified != 1 {
		t.Fatalf("callback ran %d times, want 1", notified)
	}
	if !slices.Contains(atCallback, alarm.EventSmokeAlarm) {
		t.Errorf("events at the callback = %v, want SmokeAlarm already emitted", atCallback)
	}
	unsub()
	nd.fire()
	if notified != 1 {
		t.Errorf("callback ran after unsubscribe")
	}

	if srv.OnMatterValueChanged(nil) == nil {
		t.Error("nil callback returned a nil unsubscribe")
	}
	srv.OnMatterValueChanged(nil)()

	// A source that pushes nothing never calls back.
	plain := newFull(t, &device{})
	called := false
	plain.OnMatterValueChanged(func() { called = true })()
	if called {
		t.Error("callback ran for a source without a notifier")
	}
}
