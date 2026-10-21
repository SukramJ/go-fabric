// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// eventLog is an EventEmitter that keeps every event it is handed.
type eventLog struct {
	mu     sync.Mutex
	events []loggedEvent
}

type loggedEvent struct {
	endpoint uint16
	cluster  uint32
	event    uint32
	data     any
	priority contract.EventPriority
}

func (l *eventLog) MatterEmitEvent(endpoint uint16, clusterID, event uint32, data any, priority contract.EventPriority) {
	l.mu.Lock()
	l.events = append(l.events, loggedEvent{endpoint, clusterID, event, data, priority})
	l.mu.Unlock()
}

func (l *eventLog) all() []loggedEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.events)
}

// refusingDefaultHost is a host whose DefaultOpenDuration write fails.
type refusingDefaultHost struct {
	*fakeHost
	err error
}

func (h *refusingDefaultHost) SetDefaultOpenDuration(context.Context, *uint32) error { return h.err }

// TestSetAttributesRefusesEveryValue: the state is the StateSource's.
func TestSetAttributesRefusesEveryValue(t *testing.T) {
	t.Parallel()

	srv := valve.NewServer(valve.Config{Source: fullyObservedHost()})
	if err := srv.SetAttributes(nil); err != nil {
		t.Errorf("SetAttributes(nil) = %v, want nil", err)
	}
	err := srv.SetAttributes(map[uint32]any{valve.AttrCurrentState: uint8(valve.StateOpen)})
	if !errors.Is(err, spec.ErrNotHeld) {
		t.Fatalf("SetAttributes = %v, want ErrNotHeld", err)
	}
	if !strings.Contains(err.Error(), "0x") {
		t.Errorf("error %q does not name the attribute", err)
	}
}

// TestHandleReplacesTheCommands: a host handler takes Open, nil hands it
// back, and a command the cluster does not accept is refused.
func TestHandleReplacesTheCommands(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})
	calls := 0
	if err := srv.Handle(valve.CmdClose, func(context.Context, any) (any, error) {
		calls++
		return nil, errors.New("handler refused")
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, valve.CmdClose, nil); err == nil || calls != 1 || host.closeCalls != 0 {
		t.Errorf("host handler: err %v, calls %d, port closes %d; want the handler's error only", err, calls, host.closeCalls)
	}
	if err := srv.Handle(valve.CmdClose, nil); err != nil {
		t.Fatalf("Handle(nil): %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, valve.CmdClose, nil); err != nil || host.closeCalls != 1 {
		t.Errorf("after nil: err %v, port closes %d; want the server's own handling", err, host.closeCalls)
	}
	if err := srv.Handle(0x42, func(context.Context, any) (any, error) { return nil, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle(0x42) = %v, want ErrNotAccepted", err)
	}
}

// TestEmitReachesTheEmitterOfADeclaredEvent: an event Config.Events
// declares goes out at the definition's priority, one it does not is
// refused, and an id outside the definition declares nothing.
func TestEmitReachesTheEmitterOfADeclaredEvent(t *testing.T) {
	t.Parallel()

	srv := valve.NewServer(valve.Config{Source: fullyObservedHost(), Events: []uint32{valvedef.EventValveFault}})
	if got := srv.MatterEvents(); !slices.Equal(got, []uint32{valvedef.EventValveFault}) {
		t.Fatalf("MatterEvents = %v, want [ValveFault]", got)
	}
	// Without an emitter a declared event is dropped without an error.
	if err := srv.Emit(1, valvedef.EventValveFault, nil); err != nil {
		t.Errorf("Emit without an emitter = %v", err)
	}
	log := &eventLog{}
	srv.SetMatterEventEmitter(log)
	data := valvedef.ValveFaultEvent{}
	if err := srv.Emit(4, valvedef.EventValveFault, data); err != nil {
		t.Fatalf("Emit(ValveFault) = %v", err)
	}
	got := log.all()
	if len(got) != 1 || got[0].endpoint != 4 || got[0].event != valvedef.EventValveFault || got[0].cluster != valve.ClusterID {
		t.Errorf("emitted %+v, want one ValveFault of the valve cluster on endpoint 4", got)
	}
	if err := srv.Emit(4, valvedef.EventValveStateChanged, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit(ValveStateChanged) = %v, want ErrNotEmitted", err)
	}

	bad := valve.NewServer(valve.Config{Source: fullyObservedHost(), Events: []uint32{0x99}})
	if got := bad.MatterEvents(); len(got) != 0 {
		t.Errorf("an unknown event id declared %v", got)
	}
}

// TestSetBeforeWriteVetoesTheDefaultOpenDurationWrite: the veto refuses a
// controller's write before the source takes it, and nil removes it.
func TestSetBeforeWriteVetoesTheDefaultOpenDurationWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})
	veto := errors.New("vetoed")
	var seen uint32
	srv.SetBeforeWrite(func(_ context.Context, attrID uint32, _ any) error {
		seen = attrID
		return veto
	})
	if err := srv.MatterWrite(ctx, valve.AttrDefaultOpenDuration, uint64(90)); !errors.Is(err, veto) {
		t.Fatalf("MatterWrite = %v, want the veto", err)
	}
	if seen != valve.AttrDefaultOpenDuration || len(host.defaultSets) != 0 {
		t.Errorf("veto saw 0x%04X, port saw %d writes; want the veto only", seen, len(host.defaultSets))
	}
	srv.SetBeforeWrite(nil)
	if err := srv.MatterWrite(ctx, valve.AttrDefaultOpenDuration, uint64(90)); err != nil || len(host.defaultSets) != 1 {
		t.Errorf("after removing the veto: err %v, port writes %d", err, len(host.defaultSets))
	}
}

// TestWriteNeedsAWorkingPort: without a port, or with one that refuses,
// the write fails and the data version stays.
func TestWriteNeedsAWorkingPort(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	none := valve.NewServer(valve.Config{})
	before := none.MatterDataVersion()
	if err := none.MatterWrite(ctx, valve.AttrDefaultOpenDuration, uint64(90)); err == nil || !strings.Contains(err.Error(), "no host port") {
		t.Errorf("write without a port = %v, want a missing-port error", err)
	}
	refused := errors.New("device offline")
	srv := valve.NewServer(valve.Config{Source: &refusingDefaultHost{fakeHost: fullyObservedHost(), err: refused}})
	srvBefore := srv.MatterDataVersion()
	if err := srv.MatterWrite(ctx, valve.AttrDefaultOpenDuration, uint64(90)); !errors.Is(err, refused) {
		t.Errorf("write to a refusing port = %v, want the port's error", err)
	}
	if none.MatterDataVersion() != before || srv.MatterDataVersion() != srvBefore {
		t.Error("a failed write moved the data version")
	}
}

// TestCommandsNeedAPort: Close and Open without a port are refused.
func TestCommandsNeedAPort(t *testing.T) {
	t.Parallel()

	srv := valve.NewServer(valve.Config{})
	for _, cmd := range []uint32{valve.CmdOpen, valve.CmdClose} {
		if _, err := srv.MatterInvoke(context.Background(), cmd, nil); err == nil || !strings.Contains(err.Error(), "no host port") {
			t.Errorf("command 0x%02X without a port = %v", cmd, err)
		}
	}
}

// TestUnobservedPortReadsNull: without a port every attribute reads null.
func TestNoPortReadsNull(t *testing.T) {
	t.Parallel()

	srv := valve.NewServer(valve.Config{})
	for _, id := range []uint32{
		valve.AttrOpenDuration, valve.AttrDefaultOpenDuration, valve.AttrRemainingDuration,
		valve.AttrCurrentState, valve.AttrTargetState,
	} {
		if v, ok := srv.MatterRead(id); !ok || v != nil {
			t.Errorf("MatterRead(0x%04X) = %v, %v; want null", id, v, ok)
		}
	}
}

// TestOpenReadsTheTagMapsDurations: an in-process caller's tag-keyed map
// carries the duration in whatever integer width it has; a value that is
// no uint32 is a ConstraintError.
func TestOpenReadsTheTagMapsDurations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name string
		in   any
		want uint32
	}{
		{"uint8", uint8(5), 5},
		{"uint16", uint16(500), 500},
		{"uint32", uint32(70000), 70000},
		{"uint64", uint64(80000), 80000},
		{"int", 90, 90},
		{"int8", int8(100), 100},
		{"int16", int16(1000), 1000},
		{"int32", int32(100000), 100000},
		{"int64", int64(1) << 31, 1 << 31},
	} {
		host := fullyObservedHost()
		srv := valve.NewServer(valve.Config{Source: host})
		if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, map[uint8]any{0: tc.in}); err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if len(host.openCalls) != 1 || host.openCalls[0].OpenDuration == nil || host.openCalls[0].OpenDuration.Value != tc.want {
			t.Errorf("%s: the port saw %+v, want OpenDuration %d", tc.name, host.openCalls, tc.want)
		}
	}

	for _, tc := range []struct {
		name string
		in   any
	}{
		{"negative", int8(-1)},
		{"beyond uint32 as signed", int64(1) << 40},
		{"a string", "ten"},
		{"a float", 1.5},
	} {
		srv := valve.NewServer(valve.Config{Source: fullyObservedHost()})
		_, err := srv.MatterInvoke(ctx, valve.CmdOpen, map[uint8]any{0: tc.in})
		if err == nil || statusOf(t, err) != im.StatusConstraintError || err.Error() == "" {
			t.Errorf("%s: %v, want a ConstraintError", tc.name, err)
		}
	}

	// A map without the duration is the default opening; a payload of
	// another type is refused.
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})
	if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, map[uint8]any{}); err != nil || len(host.openCalls) != 1 || host.openCalls[0].OpenDuration != nil {
		t.Errorf("an empty map: err %v, port saw %+v", err, host.openCalls)
	}
	if _, err := srv.MatterInvoke(ctx, valve.CmdOpen, "fast"); err == nil || !strings.Contains(err.Error(), "string") {
		t.Errorf("a string payload: %v, want an error naming the type", err)
	}
}

// TestHostOwnedDataVersion: a host's tracker is the one the server
// advances.
func TestHostOwnedDataVersion(t *testing.T) {
	t.Parallel()

	dv := &cluster.DataVersionTracker{}
	srv := valve.NewServer(valve.Config{Source: fullyObservedHost(), DataVersion: dv})
	before := dv.Current()
	if _, err := srv.MatterInvoke(context.Background(), valve.CmdClose, nil); err != nil {
		t.Fatal(err)
	}
	if dv.Current() == before || srv.MatterDataVersion() != dv.Current() {
		t.Errorf("host tracker %d (was %d), server %d", dv.Current(), before, srv.MatterDataVersion())
	}
}

// TestReportableAttributes: the four that move while the valve runs.
func TestReportableAttributes(t *testing.T) {
	t.Parallel()

	got := valve.NewServer(valve.Config{}).MatterReportable()
	want := []uint32{valve.AttrOpenDuration, valve.AttrRemainingDuration, valve.AttrCurrentState, valve.AttrTargetState}
	if !slices.Equal(got, want) {
		t.Errorf("MatterReportable = %v, want %v", got, want)
	}
}

// TestChangeNotifierWithoutASource: a source that notifies nothing, or no
// source, gives a no-op unsubscribe and never calls back; a nil callback
// still moves the data version.
func TestChangeNotifierWithoutASource(t *testing.T) {
	t.Parallel()

	for _, srv := range []*valve.Server{
		valve.NewServer(valve.Config{}),
		valve.NewServer(valve.Config{Source: fullyObservedHost()}),
	} {
		called := false
		unsub := srv.OnMatterValueChanged(func() { called = true })
		if unsub == nil {
			t.Fatal("nil unsubscribe")
		}
		unsub()
		if called {
			t.Error("callback ran for a source without a notifier")
		}
	}

	src := &notifyingHost{fakeHost: fullyObservedHost()}
	srv := valve.NewServer(valve.Config{Source: src})
	before := srv.MatterDataVersion()
	srv.OnMatterValueChanged(nil)
	src.cb()
	if srv.MatterDataVersion() == before {
		t.Error("a change with no callback did not move the data version")
	}
}
