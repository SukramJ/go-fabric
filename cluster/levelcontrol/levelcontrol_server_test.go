// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/tlv"
)

// fakeHost is the host port under test. It records which method the
// server called so a command can be shown to have reached the host —
// and, for the four coupled commands, to have reached the *coupled*
// method rather than its plain twin.
type fakeHost struct {
	level      uint8
	levelKnown bool
	options    uint8
	onLevel    uint8
	onKnown    bool

	optionSets  []uint8
	onLevelSets []*uint8

	// calls records the port method names in call order.
	calls []string
	// payloads records the request each command method received.
	payloads []any

	// refuse is returned by every command method when non-nil.
	refuse error
	// setErr is returned by both attribute writers when non-nil.
	setErr error

	// notify holds the callback the server registered, so a test can
	// simulate a device-side change.
	notify func()
}

func (h *fakeHost) CurrentLevel() (uint8, bool) { return h.level, h.levelKnown }
func (h *fakeHost) Options() uint8              { return h.options }
func (h *fakeHost) OnLevel() (uint8, bool)      { return h.onLevel, h.onKnown }

func (h *fakeHost) SetOptions(_ context.Context, options uint8) error {
	if h.setErr != nil {
		return h.setErr
	}
	h.optionSets = append(h.optionSets, options)
	return nil
}

func (h *fakeHost) SetOnLevel(_ context.Context, level *uint8) error {
	if h.setErr != nil {
		return h.setErr
	}
	h.onLevelSets = append(h.onLevelSets, level)
	return nil
}

// record is the single body every command method shares.
func (h *fakeHost) record(name string, req any) error {
	if h.refuse != nil {
		return h.refuse
	}
	h.calls = append(h.calls, name)
	h.payloads = append(h.payloads, req)
	return nil
}

func (h *fakeHost) MoveToLevel(_ context.Context, req levelcontrol.MoveToLevelRequest) error {
	return h.record("MoveToLevel", req)
}

func (h *fakeHost) MoveToLevelWithOnOff(_ context.Context, req levelcontrol.MoveToLevelRequest) error {
	return h.record("MoveToLevelWithOnOff", req)
}

func (h *fakeHost) Move(_ context.Context, req levelcontrol.MoveRequest) error {
	return h.record("Move", req)
}

func (h *fakeHost) MoveWithOnOff(_ context.Context, req levelcontrol.MoveRequest) error {
	return h.record("MoveWithOnOff", req)
}

func (h *fakeHost) Step(_ context.Context, req levelcontrol.StepRequest) error {
	return h.record("Step", req)
}

func (h *fakeHost) StepWithOnOff(_ context.Context, req levelcontrol.StepRequest) error {
	return h.record("StepWithOnOff", req)
}

func (h *fakeHost) Stop(_ context.Context, req levelcontrol.StopRequest) error {
	return h.record("Stop", req)
}

func (h *fakeHost) StopWithOnOff(_ context.Context, req levelcontrol.StopRequest) error {
	return h.record("StopWithOnOff", req)
}

// OnMatterValueChanged makes the fake a [contract.ChangeNotifier], which
// is the hop a device-side level change travels.
func (h *fakeHost) OnMatterValueChanged(cb func()) func() {
	h.notify = cb
	return func() { h.notify = nil }
}

// observedHost is a host that has heard from its device.
func observedHost() *fakeHost {
	return &fakeHost{
		level:      128,
		levelKnown: true,
		options:    levelcontrol.OptionExecuteIfOff,
		onLevel:    200,
		onKnown:    true,
	}
}

// buildPayload encodes a context-tagged command struct, which is the raw
// shape a host holding the wire payload hands to the server.
func buildPayload(t *testing.T, fields func(e *tlv.Encoder)) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	fields(e)
	if err := e.EndContainer(); err != nil {
		t.Fatalf("buildPayload EndContainer: %v", err)
	}
	b, err := e.Bytes()
	if err != nil {
		t.Fatalf("buildPayload Bytes: %v", err)
	}
	return b
}

// wantStatus asserts that err carries the named Matter status. A host
// refusal or a rejected payload must not reach a controller as Success,
// and must not reach it as the wrong status either.
func wantStatus(t *testing.T, err error, want im.StatusCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error carrying status 0x%02X, got nil", uint8(want))
	}
	var sc im.StatusCodeError
	if !errors.As(err, &sc) {
		t.Fatalf("error %v carries no Matter status, want 0x%02X", err, uint8(want))
	}
	if got := sc.MatterStatusCode(); got != want {
		t.Errorf("status = 0x%02X, want 0x%02X (%v)", uint8(got), uint8(want), err)
	}
}

// TestAttributesReadBackWhatThePortReports pins the three
// conformance-M attributes plus the two universal globals.
func TestAttributesReadBackWhatThePortReports(t *testing.T) {
	t.Parallel()

	srv := levelcontrol.NewServer(levelcontrol.Config{Source: observedHost()})

	tests := []struct {
		name   string
		attrID uint32
		want   any
	}{
		{name: "CurrentLevel is the port's level", attrID: levelcontrol.AttrCurrentLevel, want: uint8(128)},
		{
			name:   "Options is the port's bitmap",
			attrID: levelcontrol.AttrOptions,
			want:   levelcontrol.OptionExecuteIfOff,
		},
		{name: "OnLevel is the port's on-level", attrID: levelcontrol.AttrOnLevel, want: uint8(200)},
		{
			name:   "FeatureMap advertises OO alone",
			attrID: cluster.AttrGlobalFeatureMap,
			want:   levelcontrol.FeatureOnOff,
		},
		{
			name:   "ClusterRevision comes from the generated snapshot",
			attrID: cluster.AttrGlobalClusterRevision,
			want:   schema.ClusterRevisions[levelcontrol.ClusterID],
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := srv.MatterRead(tc.attrID)
			if !ok {
				t.Fatalf("MatterRead(0x%04X) returned ok=false", tc.attrID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("MatterRead(0x%04X) = %#v, want %#v", tc.attrID, got, tc.want)
			}
		})
	}
}

// TestUnobservedQualityXAttributesReadAsNull pins the X quality on
// CurrentLevel and OnLevel: a device that has not reported reads as TLV
// null, not as level 0 — which is a level a controller would act on.
func TestUnobservedQualityXAttributesReadAsNull(t *testing.T) {
	t.Parallel()

	srv := levelcontrol.NewServer(levelcontrol.Config{Source: &fakeHost{}})
	for _, attrID := range []uint32{levelcontrol.AttrCurrentLevel, levelcontrol.AttrOnLevel} {
		got, ok := srv.MatterRead(attrID)
		if !ok {
			t.Fatalf("MatterRead(0x%04X) returned ok=false", attrID)
		}
		if got != nil {
			t.Errorf("MatterRead(0x%04X) = %#v, want nil (null) while the device has not reported", attrID, got)
		}
	}
}

// TestUnservedAttributesAreNotResolved pins that the feature-gated and
// optional attributes are absent rather than answered with a zero.
func TestUnservedAttributesAreNotResolved(t *testing.T) {
	t.Parallel()

	srv := levelcontrol.NewServer(levelcontrol.Config{Source: observedHost()})
	// RemainingTime (LT), MinLevel/MaxLevel (O), CurrentFrequency (FQ),
	// OnOffTransitionTime (O), DefaultMoveRate (O), StartUpCurrentLevel (LT).
	for _, attrID := range []uint32{0x0001, 0x0002, 0x0003, 0x0004, 0x0010, 0x0014, 0x4000} {
		if _, ok := srv.MatterRead(attrID); ok {
			t.Errorf("MatterRead(0x%04X) resolved, but the attribute is not conformance M here", attrID)
		}
	}
}

// TestEveryCommandReachesThePort pins all eight conformance-M commands,
// each through the payload shape it actually arrives in, and pins that
// the four coupled variants land on their own port method rather than on
// the plain twin.
func TestEveryCommandReachesThePort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cmdID  uint32
		fields any
		want   string
	}{
		{
			name:   "MoveToLevel via the bridge's typed decoder",
			cmdID:  levelcontrol.CmdMoveToLevel,
			fields: levelcontrol.MoveToLevelRequest{Level: 40},
			want:   "MoveToLevel",
		},
		{
			name:   "MoveToLevelWithOnOff via the bridge's typed decoder",
			cmdID:  levelcontrol.CmdMoveToLevelWithOnOff,
			fields: levelcontrol.MoveToLevelRequest{Level: 40},
			want:   "MoveToLevelWithOnOff",
		},
		{
			name:   "Move via the generic tag map",
			cmdID:  levelcontrol.CmdMove,
			fields: map[uint8]any{0: uint64(levelcontrol.MoveModeUp), 1: uint64(20)},
			want:   "Move",
		},
		{
			name:   "MoveWithOnOff via the generic tag map",
			cmdID:  levelcontrol.CmdMoveWithOnOff,
			fields: map[uint8]any{0: uint64(levelcontrol.MoveModeDown), 1: nil},
			want:   "MoveWithOnOff",
		},
		{
			name:   "Step via the generic tag map",
			cmdID:  levelcontrol.CmdStep,
			fields: map[uint8]any{0: uint64(levelcontrol.StepModeUp), 1: uint64(5), 2: uint64(10)},
			want:   "Step",
		},
		{
			name:   "StepWithOnOff via the typed request",
			cmdID:  levelcontrol.CmdStepWithOnOff,
			fields: levelcontrol.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 5},
			want:   "StepWithOnOff",
		},
		{
			name:   "Stop via the generic tag map",
			cmdID:  levelcontrol.CmdStop,
			fields: map[uint8]any{0: uint64(0), 1: uint64(0)},
			want:   "Stop",
		},
		{
			name:   "StopWithOnOff via the typed request",
			cmdID:  levelcontrol.CmdStopWithOnOff,
			fields: levelcontrol.StopRequest{},
			want:   "StopWithOnOff",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := observedHost()
			srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
			before := srv.MatterDataVersion()

			resp, err := srv.MatterInvoke(context.Background(), tc.cmdID, tc.fields)
			if err != nil {
				t.Fatalf("MatterInvoke(0x%02X): %v", tc.cmdID, err)
			}
			if resp != nil {
				t.Errorf("response = %#v, want nil — every LevelControl command is status-only", resp)
			}
			if !reflect.DeepEqual(host.calls, []string{tc.want}) {
				t.Errorf("port saw %v, want [%s]", host.calls, tc.want)
			}
			if got := srv.MatterDataVersion(); got == before {
				t.Errorf("DataVersion stayed %d across an accepted %s", got, tc.want)
			}
		})
	}
}

// TestRawPayloadGoesThroughTheWireDecoders pins the raw-bytes shape for
// all four payload types: the server hands them to cluster/wire rather
// than parsing them a second time.
func TestRawPayloadGoesThroughTheWireDecoders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cmdID  uint32
		build  func(e *tlv.Encoder)
		verify func(t *testing.T, got any)
	}{
		{
			name:  "MoveToLevel",
			cmdID: levelcontrol.CmdMoveToLevel,
			build: func(e *tlv.Encoder) {
				e.PutUint(tlv.ContextTag(0), 77)
				e.PutUint(tlv.ContextTag(1), 10)
			},
			verify: func(t *testing.T, got any) {
				t.Helper()
				req, ok := got.(levelcontrol.MoveToLevelRequest)
				if !ok {
					t.Fatalf("port saw %T, want MoveToLevelRequest", got)
				}
				if req.Level != 77 || req.TransitionTime == nil || *req.TransitionTime != 10 {
					t.Errorf("port saw %#v, want Level 77 and TransitionTime 10", req)
				}
			},
		},
		{
			name:  "Move",
			cmdID: levelcontrol.CmdMoveWithOnOff,
			build: func(e *tlv.Encoder) {
				e.PutUint(tlv.ContextTag(0), uint64(levelcontrol.MoveModeDown))
				e.PutNull(tlv.ContextTag(1))
			},
			verify: func(t *testing.T, got any) {
				t.Helper()
				req, ok := got.(levelcontrol.MoveRequest)
				if !ok {
					t.Fatalf("port saw %T, want MoveRequest", got)
				}
				if req.MoveMode != levelcontrol.MoveModeDown || req.Rate != nil {
					t.Errorf("port saw %#v, want MoveMode Down and a null Rate", req)
				}
			},
		},
		{
			name:  "Step",
			cmdID: levelcontrol.CmdStep,
			build: func(e *tlv.Encoder) {
				e.PutUint(tlv.ContextTag(0), uint64(levelcontrol.StepModeUp))
				e.PutUint(tlv.ContextTag(1), 3)
				e.PutUint(tlv.ContextTag(2), 25)
			},
			verify: func(t *testing.T, got any) {
				t.Helper()
				req, ok := got.(levelcontrol.StepRequest)
				if !ok {
					t.Fatalf("port saw %T, want StepRequest", got)
				}
				if req.StepSize != 3 || req.TransitionTime == nil || *req.TransitionTime != 25 {
					t.Errorf("port saw %#v, want StepSize 3 and TransitionTime 25", req)
				}
			},
		},
		{
			name:  "Stop",
			cmdID: levelcontrol.CmdStopWithOnOff,
			build: func(e *tlv.Encoder) {
				e.PutUint(tlv.ContextTag(0), uint64(levelcontrol.OptionExecuteIfOff))
				e.PutUint(tlv.ContextTag(1), uint64(levelcontrol.OptionExecuteIfOff))
			},
			verify: func(t *testing.T, got any) {
				t.Helper()
				req, ok := got.(levelcontrol.StopRequest)
				if !ok {
					t.Fatalf("port saw %T, want StopRequest", got)
				}
				if req.OptionsMask != levelcontrol.OptionExecuteIfOff {
					t.Errorf("port saw %#v, want OptionsMask ExecuteIfOff", req)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := observedHost()
			srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
			if _, err := srv.MatterInvoke(context.Background(), tc.cmdID, buildPayload(t, tc.build)); err != nil {
				t.Fatalf("MatterInvoke: %v", err)
			}
			if len(host.payloads) != 1 {
				t.Fatalf("port saw %d payloads, want 1", len(host.payloads))
			}
			tc.verify(t, host.payloads[0])
		})
	}
}

// TestHostRefusalIsNotSuccess pins the rule the whole port exists for:
// a host that refuses must not have its refusal reported as Success, and
// must not move the DataVersion either.
func TestHostRefusalIsNotSuccess(t *testing.T) {
	t.Parallel()

	refusal := errors.New("device unreachable")

	tests := []struct {
		name   string
		cmdID  uint32
		fields any
	}{
		{name: "MoveToLevel", cmdID: levelcontrol.CmdMoveToLevel, fields: levelcontrol.MoveToLevelRequest{Level: 1}},
		{name: "Move", cmdID: levelcontrol.CmdMove, fields: levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeUp}},
		{name: "Step", cmdID: levelcontrol.CmdStep, fields: levelcontrol.StepRequest{StepSize: 1}},
		{name: "Stop", cmdID: levelcontrol.CmdStop, fields: levelcontrol.StopRequest{}},
		{
			name:   "MoveToLevelWithOnOff",
			cmdID:  levelcontrol.CmdMoveToLevelWithOnOff,
			fields: levelcontrol.MoveToLevelRequest{Level: 1},
		},
		{
			name:   "MoveWithOnOff",
			cmdID:  levelcontrol.CmdMoveWithOnOff,
			fields: levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeUp},
		},
		{name: "StepWithOnOff", cmdID: levelcontrol.CmdStepWithOnOff, fields: levelcontrol.StepRequest{StepSize: 1}},
		{name: "StopWithOnOff", cmdID: levelcontrol.CmdStopWithOnOff, fields: levelcontrol.StopRequest{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := observedHost()
			host.refuse = refusal
			srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
			before := srv.MatterDataVersion()

			_, err := srv.MatterInvoke(context.Background(), tc.cmdID, tc.fields)
			if err == nil {
				t.Fatalf("%s reported Success although the host refused", tc.name)
			}
			if !errors.Is(err, refusal) {
				t.Errorf("error %v does not wrap the host's refusal", err)
			}
			if got := srv.MatterDataVersion(); got != before {
				t.Errorf("DataVersion moved to %d on a refused command", got)
			}
		})
	}
}

// TestNoHostPortIsNotSuccess pins the same rule for a server with no
// port at all: it never claims a device moved.
func TestNoHostPortIsNotSuccess(t *testing.T) {
	t.Parallel()

	srv := levelcontrol.NewServer(levelcontrol.Config{})
	if _, err := srv.MatterInvoke(
		context.Background(), levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 1},
	); err == nil {
		t.Error("MoveToLevel reported Success with no host port")
	}
	if err := srv.MatterWrite(context.Background(), levelcontrol.AttrOnLevel, uint64(10)); err == nil {
		t.Error("OnLevel write reported Success with no host port")
	}
}

// TestDeviceSideChangeBumpsTheVersion pins the ChangeNotifier hop: a
// level the device changed on its own bumps the DataVersion, which is
// what makes a subscribed controller see the new value before its next
// read.
func TestDeviceSideChangeBumpsTheVersion(t *testing.T) {
	t.Parallel()

	host := observedHost()
	tracker := &cluster.DataVersionTracker{}
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: host, DataVersion: tracker})

	var fired int
	unsubscribe := srv.OnMatterValueChanged(func() { fired++ })
	if host.notify == nil {
		t.Fatal("the server did not subscribe to the host port's notifier")
	}
	before := srv.MatterDataVersion()

	host.level = 200
	host.notify()

	if fired != 1 {
		t.Errorf("the bridge callback fired %d times, want 1", fired)
	}
	after := srv.MatterDataVersion()
	if after == before {
		t.Errorf("DataVersion stayed %d across a device-side level change", after)
	}
	if got := tracker.Current(); got != after {
		t.Errorf("the host's tracker reads %d but the server reports %d", got, after)
	}

	unsubscribe()
	if host.notify != nil {
		t.Error("unsubscribe did not release the host port's notifier")
	}
}

// TestChangeNotifierIsOptional pins that a port which cannot notify
// still yields a usable, releasable subscription.
func TestChangeNotifierIsOptional(t *testing.T) {
	t.Parallel()

	srv := levelcontrol.NewServer(levelcontrol.Config{Source: silentHost{observedHost()}})
	unsubscribe := srv.OnMatterValueChanged(func() { t.Error("a silent port fired a change") })
	if unsubscribe == nil {
		t.Fatal("OnMatterValueChanged returned a nil unsubscribe")
	}
	unsubscribe()
}

// silentHost is a port with no notifier — every host that only answers
// reads and takes commands. Embedding the interface rather than the fake
// is what hides the fake's own notifier from the type assertion.
type silentHost struct{ levelcontrol.LevelSource }

// TestWritesReachThePort pins the two "RW VO" attributes, including the
// null write that clears OnLevel.
func TestWritesReachThePort(t *testing.T) {
	t.Parallel()

	t.Run("Options", func(t *testing.T) {
		t.Parallel()
		host := observedHost()
		srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
		before := srv.MatterDataVersion()
		if err := srv.MatterWrite(
			context.Background(), levelcontrol.AttrOptions, uint64(levelcontrol.OptionExecuteIfOff),
		); err != nil {
			t.Fatalf("MatterWrite(Options): %v", err)
		}
		if !reflect.DeepEqual(host.optionSets, []uint8{levelcontrol.OptionExecuteIfOff}) {
			t.Errorf("port saw %v, want [%d]", host.optionSets, levelcontrol.OptionExecuteIfOff)
		}
		if srv.MatterDataVersion() == before {
			t.Error("DataVersion stayed put across an accepted Options write")
		}
	})

	t.Run("OnLevel", func(t *testing.T) {
		t.Parallel()
		host := observedHost()
		srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
		if err := srv.MatterWrite(context.Background(), levelcontrol.AttrOnLevel, uint64(64)); err != nil {
			t.Fatalf("MatterWrite(OnLevel): %v", err)
		}
		if err := srv.MatterWrite(context.Background(), levelcontrol.AttrOnLevel, nil); err != nil {
			t.Fatalf("MatterWrite(OnLevel, null): %v", err)
		}
		if len(host.onLevelSets) != 2 {
			t.Fatalf("port saw %d OnLevel writes, want 2", len(host.onLevelSets))
		}
		if host.onLevelSets[0] == nil || *host.onLevelSets[0] != 64 {
			t.Errorf("first write = %v, want a pointer to 64", host.onLevelSets[0])
		}
		if host.onLevelSets[1] != nil {
			t.Errorf("second write = %v, want nil — a null OnLevel means it has no effect", host.onLevelSets[1])
		}
	})
}

// TestRejectedPayloadsCarryTheRightStatus pins the validation the server
// applies before anything reaches the host. Each row would otherwise
// forward a value the cluster has no meaning for.
func TestRejectedPayloadsCarryTheRightStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cmdID  uint32
		fields any
		want   im.StatusCode
	}{
		{
			name:   "Level above the max-254 constraint",
			cmdID:  levelcontrol.CmdMoveToLevel,
			fields: levelcontrol.MoveToLevelRequest{Level: 255},
			want:   im.StatusConstraintError,
		},
		{
			name:   "MoveMode outside MoveModeEnum",
			cmdID:  levelcontrol.CmdMove,
			fields: levelcontrol.MoveRequest{MoveMode: 7},
			want:   im.StatusConstraintError,
		},
		{
			name:   "StepMode outside StepModeEnum",
			cmdID:  levelcontrol.CmdStepWithOnOff,
			fields: levelcontrol.StepRequest{StepMode: 9},
			want:   im.StatusConstraintError,
		},
		{
			name:   "a move rate of zero",
			cmdID:  levelcontrol.CmdMove,
			fields: map[uint8]any{0: uint64(levelcontrol.MoveModeUp), 1: uint64(0)},
			want:   im.StatusInvalidCommand,
		},
		{
			name:   "MoveToLevel with no fields at all",
			cmdID:  levelcontrol.CmdMoveToLevel,
			fields: nil,
			want:   im.StatusInvalidCommand,
		},
		{
			name:   "Step missing its mandatory StepSize",
			cmdID:  levelcontrol.CmdStep,
			fields: map[uint8]any{0: uint64(levelcontrol.StepModeUp)},
			want:   im.StatusInvalidCommand,
		},
		{
			name:   "a payload shape the server cannot read",
			cmdID:  levelcontrol.CmdStop,
			fields: "not a command",
			want:   im.StatusInvalidCommand,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := observedHost()
			srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
			_, err := srv.MatterInvoke(context.Background(), tc.cmdID, tc.fields)
			wantStatus(t, err, tc.want)
			if len(host.calls) != 0 {
				t.Errorf("the port saw %v, but the command should not have reached it", host.calls)
			}
		})
	}
}

// TestWriteStatuses pins the attribute-write refusals: a read-only
// attribute, an attribute this server does not carry, and an Options bit
// whose feature is not advertised.
func TestWriteStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		attrID uint32
		value  any
		want   im.StatusCode
	}{
		{
			name:   "CurrentLevel is R V",
			attrID: levelcontrol.AttrCurrentLevel,
			value:  uint64(10),
			want:   im.StatusUnsupportedWrite,
		},
		{
			name:   "OnOffTransitionTime is not served here",
			attrID: 0x0010,
			value:  uint64(10),
			want:   im.StatusUnsupportedAttribute,
		},
		{
			name:   "CoupleColorTempToLevel needs the LT feature",
			attrID: levelcontrol.AttrOptions,
			value:  uint64(levelcontrol.OptionCoupleColorTempToLevel),
			want:   im.StatusConstraintError,
		},
		{
			name:   "Options is not nullable",
			attrID: levelcontrol.AttrOptions,
			value:  nil,
			want:   im.StatusConstraintError,
		},
		{
			name:   "OnLevel above maxLevel",
			attrID: levelcontrol.AttrOnLevel,
			value:  uint64(255),
			want:   im.StatusConstraintError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := observedHost()
			srv := levelcontrol.NewServer(levelcontrol.Config{Source: host})
			wantStatus(t, srv.MatterWrite(context.Background(), tc.attrID, tc.value), tc.want)
			if len(host.optionSets) != 0 || len(host.onLevelSets) != 0 {
				t.Error("a rejected write still reached the port")
			}
		})
	}
}

// TestUnknownCommandIsUnsupported pins that an id outside the eight is
// refused rather than absorbed.
func TestUnknownCommandIsUnsupported(t *testing.T) {
	t.Parallel()

	srv := levelcontrol.NewServer(levelcontrol.Config{Source: observedHost()})
	// 0x8 is MoveToClosestFrequency, which carries conformance "FQ".
	_, err := srv.MatterInvoke(context.Background(), 0x08, nil)
	if err == nil {
		t.Fatal("MoveToClosestFrequency was accepted although FQ is not advertised")
	}
}

// TestEffectiveOptions pins the temporary-bitmap rule: a set mask bit
// takes the bit from the override, a clear mask bit keeps the
// attribute's own bit.
func TestEffectiveOptions(t *testing.T) {
	t.Parallel()

	const on = levelcontrol.OptionExecuteIfOff

	tests := []struct {
		name                       string
		attr, mask, override, want uint8
	}{
		{name: "no mask keeps the attribute", attr: on, mask: 0, override: 0, want: on},
		{name: "mask takes the override on", attr: 0, mask: on, override: on, want: on},
		{name: "mask takes the override off", attr: on, mask: on, override: 0, want: 0},
		{
			name:     "an unadvertised bit never survives",
			attr:     levelcontrol.OptionCoupleColorTempToLevel,
			mask:     levelcontrol.OptionCoupleColorTempToLevel,
			override: levelcontrol.OptionCoupleColorTempToLevel,
			want:     0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := levelcontrol.EffectiveOptions(tc.attr, tc.mask, tc.override); got != tc.want {
				t.Errorf("EffectiveOptions(%d, %d, %d) = %d, want %d", tc.attr, tc.mask, tc.override, got, tc.want)
			}
		})
	}
}

// TestSurfaceLists pins the three global lists the dispatcher builds
// from this server.
func TestSurfaceLists(t *testing.T) {
	t.Parallel()

	srv := levelcontrol.NewServer(levelcontrol.Config{Source: observedHost()})

	wantAttrs := []uint32{levelcontrol.AttrCurrentLevel, levelcontrol.AttrOptions, levelcontrol.AttrOnLevel}
	if got := srv.MatterAttributes(); !reflect.DeepEqual(got, wantAttrs) {
		t.Errorf("MatterAttributes() = %v, want %v", got, wantAttrs)
	}

	wantCmds := []uint32{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	if got := srv.MatterAcceptedCommands(); !reflect.DeepEqual(got, wantCmds) {
		t.Errorf("MatterAcceptedCommands() = %v, want %v", got, wantCmds)
	}
	if got := srv.MatterGeneratedCommands(); got != nil {
		t.Errorf("MatterGeneratedCommands() = %v, want nil — every command is status-only", got)
	}
	if got := srv.MatterReportable(); !reflect.DeepEqual(got, []uint32{levelcontrol.AttrCurrentLevel}) {
		t.Errorf("MatterReportable() = %v, want [CurrentLevel]", got)
	}
	if got := srv.MatterClusterID(); got != 0x0008 {
		t.Errorf("MatterClusterID() = 0x%04X, want 0x0008", got)
	}
	if got := levelcontrol.Revision(); got != schema.ClusterRevisions[levelcontrol.ClusterID] {
		t.Errorf("Revision() = %d, want the snapshot's %d", got, schema.ClusterRevisions[levelcontrol.ClusterID])
	}
}
