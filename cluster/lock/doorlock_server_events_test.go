// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// eventTestSource is a StateSource whose LockInvoke outcome is
// configurable, so tests can exercise both the success path (event
// emitted) and the failure path (no event, no DataVersion bump).
type eventTestSource struct {
	locked    bool
	observed  bool
	invokeErr error
	invoked   []uint32
}

func (s *eventTestSource) IsJammed() bool                    { return false }
func (s *eventTestSource) IsLocked() (locked, observed bool) { return s.locked, s.observed }

func (s *eventTestSource) LockInvoke(_ context.Context, cmdID uint32) error {
	s.invoked = append(s.invoked, cmdID)
	return s.invokeErr
}

// emitCall captures a single MatterEmitEvent invocation.
type emitCall struct {
	endpoint uint16
	cluster  uint32
	event    uint32
	data     any
	priority contract.EventPriority
}

// fakeEventEmitter implements [contract.EventEmitter] and
// records every call for assertion.
type fakeEventEmitter struct {
	calls []emitCall
}

func (f *fakeEventEmitter) MatterEmitEvent(endpoint uint16, clusterID, eventID uint32, data any, priority contract.EventPriority) {
	f.calls = append(f.calls, emitCall{endpoint: endpoint, cluster: clusterID, event: eventID, data: data, priority: priority})
}

var errInvokeFailed = errors.New("doorlock: invoke failed")

// newWiredServer returns a DoorLockServer with an emitter and endpoint
// already wired, plus the source and emitter for assertions.
func newWiredServer(t *testing.T, src *eventTestSource, endpoint uint16) (*lock.DoorLockServer, *fakeEventEmitter) {
	t.Helper()
	srv := lock.NewDoorLockServer(lock.DoorLockConfig{Source: src})
	emitter := &fakeEventEmitter{}
	srv.SetMatterEventEmitter(emitter)
	srv.SetEndpoint(endpoint)
	return srv, emitter
}

// TestDoorLockServer_EmitLockOperation_LockDoor verifies that a
// successful LockDoor invoke fires exactly one LockOperation event
// (0x02) with LockOperationType=Lock(0), OperationSource=Remote(7),
// and FabricIndex/SourceNode lifted from the invoking context. Mirrors
// matter.js 0ceb202d DoorLockServer.ts:1041 (#actuate ends in
// #completeOperation → #emitLockOperation) and :1289-1301 (payload
// assembly).
func TestDoorLockServer_EmitLockOperation_LockDoor(t *testing.T) {
	t.Parallel()
	src := &eventTestSource{observed: true}
	srv, emitter := newWiredServer(t, src, 7)

	ctx := im.WithFabricFilter(context.Background(), false, 2)
	ctx = im.WithSubject(ctx, 0xAABB, nil)

	if _, err := srv.MatterInvoke(ctx, wire.DoorLockCmdLockDoor, nil); err != nil {
		t.Fatalf("MatterInvoke(LockDoor): %v", err)
	}

	if len(emitter.calls) != 1 {
		t.Fatalf("emitter.calls = %d, want exactly 1", len(emitter.calls))
	}
	c := emitter.calls[0]
	if c.endpoint != 7 {
		t.Errorf("endpoint = %d, want 7", c.endpoint)
	}
	if c.cluster != wire.DoorLockClusterID {
		t.Errorf("cluster = 0x%04X, want 0x%04X", c.cluster, wire.DoorLockClusterID)
	}
	if c.event != wire.DoorLockEventLockOperation {
		t.Errorf("event = 0x%02X, want 0x%02X", c.event, wire.DoorLockEventLockOperation)
	}
	if c.priority != contract.EventPriorityCritical {
		t.Errorf("priority = %v, want MatterEventPriorityCritical", c.priority)
	}

	payload, ok := c.data.(lock.LockOperationEvent)
	if !ok {
		t.Fatalf("data type = %T, want lock.LockOperationEvent", c.data)
	}
	if payload.LockOperationType != 0 {
		t.Errorf("LockOperationType = %d, want 0 (Lock)", payload.LockOperationType)
	}
	if payload.OperationSource != 7 {
		t.Errorf("OperationSource = %d, want 7 (Remote)", payload.OperationSource)
	}
	if payload.UserIndex != nil {
		t.Errorf("UserIndex = %v, want nil", *payload.UserIndex)
	}
	if payload.FabricIndex == nil {
		t.Error("FabricIndex = nil, want non-nil (2)")
	} else if *payload.FabricIndex != 2 {
		t.Errorf("FabricIndex = %d, want 2", *payload.FabricIndex)
	}
	if payload.SourceNode == nil {
		t.Error("SourceNode = nil, want non-nil (0xAABB)")
	} else if *payload.SourceNode != 0xAABB {
		t.Errorf("SourceNode = 0x%X, want 0xAABB", *payload.SourceNode)
	}
}

// fabricCtx is the context of a CASE requester on fabric 1 without a
// node subject.
func fabricCtx() context.Context { return im.WithFabricFilter(context.Background(), false, 1) }

// TestDoorLockServer_EmitLockOperation_OperationTypeMapping verifies
// the LockOperationType wire value per command: UnlockDoor→Unlock(1),
// UnboltDoor→Unlock(1). Unbolting retracts the bolt without pulling the
// latch, so the lock ends up Unlocked and reports an Unlock — matter.js
// 0ceb202d DoorLockServer.ts:208-217 and :1024-1025.
func TestDoorLockServer_EmitLockOperation_OperationTypeMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cmdID  uint32
		wantOp uint8
	}{
		{"UnlockDoor", wire.DoorLockCmdUnlockDoor, 1},
		{"UnboltDoor", wire.DoorLockCmdUnboltDoor, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := &eventTestSource{observed: true}
			srv, emitter := newWiredServer(t, src, 1)

			if _, err := srv.MatterInvoke(fabricCtx(), tc.cmdID, nil); err != nil {
				t.Fatalf("MatterInvoke(0x%02X): %v", tc.cmdID, err)
			}
			if len(emitter.calls) != 1 {
				t.Fatalf("emitter.calls = %d, want exactly 1", len(emitter.calls))
			}
			payload, ok := emitter.calls[0].data.(lock.LockOperationEvent)
			if !ok {
				t.Fatalf("data type = %T, want lock.LockOperationEvent", emitter.calls[0].data)
			}
			if payload.LockOperationType != tc.wantOp {
				t.Errorf("LockOperationType = %d, want %d", payload.LockOperationType, tc.wantOp)
			}
		})
	}
}

// TestDoorLockServer_NoFabricIsUnsupportedAccess verifies that a lock
// command without an accessing fabric (a PASE session before AddNOC) is
// refused with UNSUPPORTED_ACCESS before the source is called, and emits
// no event. Mirrors matter.js 0ceb202d DoorLockServer.ts:1017-1022.
func TestDoorLockServer_NoFabricIsUnsupportedAccess(t *testing.T) {
	t.Parallel()
	for _, cmdID := range []uint32{wire.DoorLockCmdLockDoor, wire.DoorLockCmdUnlockDoor, wire.DoorLockCmdUnboltDoor} {
		src := &eventTestSource{observed: true}
		srv, emitter := newWiredServer(t, src, 3)
		_, err := srv.MatterInvoke(context.Background(), cmdID, nil)
		if got := statusOf(err); got != im.StatusUnsupportedAccess {
			t.Errorf("0x%02X: status = %v (%v), want UnsupportedAccess", cmdID, got, err)
		}
		if len(src.invoked) != 0 || len(emitter.calls) != 0 {
			t.Errorf("0x%02X: LockInvoke %v, events %d — want neither", cmdID, src.invoked, len(emitter.calls))
		}
	}
}

// TestDoorLockServer_EmitLockOperation_NoSubjectNode verifies that a
// requester with a fabric but no node subject yields SourceNode=nil —
// matter.js 0ceb202d DoorLockServer.ts:943-949 (#sourceNode).
func TestDoorLockServer_EmitLockOperation_NoSubjectNode(t *testing.T) {
	t.Parallel()
	src := &eventTestSource{observed: true}
	srv, emitter := newWiredServer(t, src, 3)

	if _, err := srv.MatterInvoke(fabricCtx(), wire.DoorLockCmdLockDoor, nil); err != nil {
		t.Fatalf("MatterInvoke: %v", err)
	}
	if len(emitter.calls) != 1 {
		t.Fatalf("emitter.calls = %d, want exactly 1", len(emitter.calls))
	}
	payload, ok := emitter.calls[0].data.(lock.LockOperationEvent)
	if !ok {
		t.Fatalf("data type = %T, want lock.LockOperationEvent", emitter.calls[0].data)
	}
	if payload.FabricIndex == nil || *payload.FabricIndex != 1 {
		t.Errorf("FabricIndex = %v, want 1", payload.FabricIndex)
	}
	if payload.SourceNode != nil {
		t.Errorf("SourceNode = 0x%X, want nil", *payload.SourceNode)
	}
}

// lockOperationError returns the single LockOperationError payload the
// emitter recorded.
func lockOperationError(t *testing.T, emitter *fakeEventEmitter) lockdef.LockOperationErrorEvent {
	t.Helper()
	if len(emitter.calls) != 1 {
		t.Fatalf("emitter.calls = %d, want exactly 1", len(emitter.calls))
	}
	c := emitter.calls[0]
	if c.event != wire.DoorLockEventLockOperationError || c.priority != contract.EventPriorityCritical {
		t.Fatalf("event 0x%02X priority %v, want LockOperationError (0x03), critical", c.event, c.priority)
	}
	payload, ok := c.data.(lockdef.LockOperationErrorEvent)
	if !ok {
		t.Fatalf("data type = %T, want lockdef.LockOperationErrorEvent", c.data)
	}
	return payload
}

func statusOf(err error) im.StatusCode {
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return sc.MatterStatusCode()
	}
	return 0
}

// TestDoorLockServer_InvokeErrorEmitsLockOperationError verifies that a
// failing LockInvoke emits LockOperationError (OperationError Unspecified,
// the requester's fabric and node) and answers FAILURE — or the error's
// own IM status when it carries one — without a DataVersion bump. Mirrors
// matter.js 0ceb202d DoorLockServer.ts:1033-1038 (hardwareFailureReason
// :1605-1624 picks Unspecified for an error that is not a
// LockOperationFailedError); matter.js answers FAILURE in both cases.
func TestDoorLockServer_InvokeErrorEmitsLockOperationError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want im.StatusCode
	}{
		{errInvokeFailed, im.StatusFailure},
		{spec.Errorf(im.StatusBusy, "busy"), im.StatusBusy},
	} {
		src := &eventTestSource{observed: true, invokeErr: tc.err}
		srv, emitter := newWiredServer(t, src, 4)
		dvBefore := srv.MatterDataVersion()

		ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, 2), 0xAABB, nil)
		_, err := srv.MatterInvoke(ctx, wire.DoorLockCmdUnboltDoor, nil)
		if got := statusOf(err); got != tc.want {
			t.Errorf("%v: status = %v (%v), want %v", tc.err, got, err, tc.want)
		}
		ev := lockOperationError(t, emitter)
		if ev.LockOperationType != lockdef.LockOperationTypeUnlock || ev.OperationSource != lockdef.OperationSourceRemote ||
			ev.OperationError != lockdef.OperationErrorUnspecified {
			t.Errorf("payload = %+v, want Unlock / Remote / Unspecified", ev)
		}
		if !ev.UserIndex.Null || ev.FabricIndex != spec.ValueOf(uint8(2)) || ev.SourceNode != spec.ValueOf(uint64(0xAABB)) {
			t.Errorf("payload = %+v, want UserIndex null, FabricIndex 2, SourceNode 0xAABB", ev)
		}
		if ev.Credentials != nil {
			t.Error("Credentials present, want absent (no USR)")
		}
		if srv.MatterDataVersion() != dvBefore {
			t.Error("DataVersion bumped despite failed invoke")
		}
	}
}

// TestDoorLockServer_NoRemoteLockUnlockRefusesRemoteOperations verifies
// that with OperatingMode NoRemoteLockUnlock (3) every lock command emits
// LockOperationError (Unspecified) and answers FAILURE without calling
// the source. Mirrors matter.js 0ceb202d DoorLockServer.ts:1050-1056
// (#authorize: Privacy or NoRemoteLockUnlock); Privacy is not a supported
// mode here (SupportedOperatingModes 0xFFF6), so it cannot be written.
func TestDoorLockServer_NoRemoteLockUnlockRefusesRemoteOperations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cmdID  uint32
		wantOp lockdef.LockOperationTypeEnum
	}{
		{wire.DoorLockCmdLockDoor, lockdef.LockOperationTypeLock},
		{wire.DoorLockCmdUnlockDoor, lockdef.LockOperationTypeUnlock},
		{wire.DoorLockCmdUnboltDoor, lockdef.LockOperationTypeUnlock},
	} {
		src := &eventTestSource{observed: true}
		srv, emitter := newWiredServer(t, src, 5)
		if err := srv.MatterWrite(context.Background(), wire.DoorLockAttrOperatingMode, uint8(3)); err != nil {
			t.Fatalf("write OperatingMode=3: %v", err)
		}
		_, err := srv.MatterInvoke(fabricCtx(), tc.cmdID, nil)
		if got := statusOf(err); got != im.StatusFailure {
			t.Errorf("0x%02X: status = %v (%v), want Failure", tc.cmdID, got, err)
		}
		if len(src.invoked) != 0 {
			t.Errorf("0x%02X: LockInvoke called %v, want not called", tc.cmdID, src.invoked)
		}
		ev := lockOperationError(t, emitter)
		if ev.LockOperationType != tc.wantOp || ev.OperationError != lockdef.OperationErrorUnspecified ||
			ev.FabricIndex != spec.ValueOf(uint8(1)) || !ev.SourceNode.Null {
			t.Errorf("0x%02X: payload = %+v, want type %d, Unspecified, fabric 1, no node", tc.cmdID, ev, tc.wantOp)
		}
	}
}

// TestDoorLockServer_EmitLockOperation_NoEmitterWired verifies that
// MatterInvoke succeeds without panicking when no emitter has been
// wired via [lock.DoorLockServer.SetMatterEventEmitter] — the
// no-op default before the bridge assembles the topology.
func TestDoorLockServer_EmitLockOperation_NoEmitterWired(t *testing.T) {
	t.Parallel()
	src := &eventTestSource{observed: true}
	srv := lock.NewDoorLockServer(lock.DoorLockConfig{Source: src})

	if _, err := srv.MatterInvoke(fabricCtx(), wire.DoorLockCmdLockDoor, nil); err != nil {
		t.Fatalf("MatterInvoke without emitter: %v", err)
	}
}

// TestDoorLockServer_MatterEvents_AdvertisesThreeEvents verifies that
// MatterEvents() returns exactly the three conformance-M DoorLock
// event IDs (DoorLockAlarm 0x00, LockOperation 0x02,
// LockOperationError 0x03).
func TestDoorLockServer_MatterEvents_AdvertisesThreeEvents(t *testing.T) {
	t.Parallel()
	srv := lock.NewDoorLockServer(lock.DoorLockConfig{Source: &eventTestSource{observed: true}})

	got := srv.MatterEvents()
	want := map[uint32]bool{
		wire.DoorLockEventDoorLockAlarm:      true,
		wire.DoorLockEventLockOperation:      true,
		wire.DoorLockEventLockOperationError: true,
	}
	if len(got) != len(want) {
		t.Fatalf("MatterEvents() = %v (len %d), want %d distinct ids", got, len(got), len(want))
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("MatterEvents() contains unexpected id 0x%02X", id)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Errorf("MatterEvents() missing ids: %v", want)
	}
}
