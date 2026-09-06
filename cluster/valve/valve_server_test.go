// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package valve_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/im"
)

// fakeHost is the host port under test. It records what the server sent
// so a command can be shown to have reached the host rather than to have
// been absorbed by the cluster server.
type fakeHost struct {
	currentState    valve.State
	currentKnown    bool
	targetState     valve.State
	targetKnown     bool
	openDuration    uint32
	openKnown       bool
	remaining       uint32
	remainingKnown  bool
	defaultDuration uint32
	defaultKnown    bool

	openCalls   []valve.OpenRequest
	closeCalls  int
	defaultSets []*uint32

	openErr  error
	closeErr error
}

func (h *fakeHost) CurrentState() (valve.State, bool) { return h.currentState, h.currentKnown }
func (h *fakeHost) TargetState() (valve.State, bool)  { return h.targetState, h.targetKnown }
func (h *fakeHost) OpenDuration() (uint32, bool)      { return h.openDuration, h.openKnown }
func (h *fakeHost) RemainingDuration() (uint32, bool) { return h.remaining, h.remainingKnown }
func (h *fakeHost) DefaultOpenDuration() (uint32, bool) {
	return h.defaultDuration, h.defaultKnown
}

func (h *fakeHost) SetDefaultOpenDuration(_ context.Context, seconds *uint32) error {
	h.defaultSets = append(h.defaultSets, seconds)
	return nil
}

func (h *fakeHost) Open(_ context.Context, req valve.OpenRequest) error {
	if h.openErr != nil {
		return h.openErr
	}
	h.openCalls = append(h.openCalls, req)
	return nil
}

func (h *fakeHost) Close(_ context.Context) error {
	if h.closeErr != nil {
		return h.closeErr
	}
	h.closeCalls++
	return nil
}

func fullyObservedHost() *fakeHost {
	return &fakeHost{
		currentState:    valve.StateOpen,
		currentKnown:    true,
		targetState:     valve.StateClosed,
		targetKnown:     true,
		openDuration:    600,
		openKnown:       true,
		remaining:       120,
		remainingKnown:  true,
		defaultDuration: 300,
		defaultKnown:    true,
	}
}

func TestServer_ClusterIdentity(t *testing.T) {
	t.Parallel()
	srv := valve.NewServer(valve.Config{Source: fullyObservedHost()})
	if got := srv.MatterClusterID(); got != 0x0081 {
		t.Fatalf("MatterClusterID = 0x%04X, want 0x0081", got)
	}
	rev, ok := srv.MatterRead(cluster.AttrGlobalClusterRevision)
	if !ok {
		t.Fatal("ClusterRevision: ok=false")
	}
	if rev != valve.Revision() {
		t.Fatalf("ClusterRevision = %v, want %v", rev, valve.Revision())
	}
	fm, ok := srv.MatterRead(cluster.AttrGlobalFeatureMap)
	if !ok {
		t.Fatal("FeatureMap: ok=false")
	}
	// No optional feature is advertised, so neither level attribute may
	// be claimed by the FeatureMap.
	if fm != uint32(0) {
		t.Fatalf("FeatureMap = %v, want 0", fm)
	}
}

// TestServer_MandatoryAttributesReadBackFromPort measures that each of
// the five conformance-M attributes carries the host's own value.
func TestServer_MandatoryAttributesReadBackFromPort(t *testing.T) {
	t.Parallel()
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})

	cases := []struct {
		name   string
		attrID uint32
		want   any
	}{
		{"OpenDuration", valve.AttrOpenDuration, uint32(600)},
		{"DefaultOpenDuration", valve.AttrDefaultOpenDuration, uint32(300)},
		{"RemainingDuration", valve.AttrRemainingDuration, uint32(120)},
		{"CurrentState", valve.AttrCurrentState, uint8(valve.StateOpen)},
		{"TargetState", valve.AttrTargetState, uint8(valve.StateClosed)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := srv.MatterRead(tc.attrID)
			if !ok {
				t.Fatalf("MatterRead(0x%04X): ok=false", tc.attrID)
			}
			if got != tc.want {
				t.Fatalf("MatterRead(0x%04X) = %#v, want %#v", tc.attrID, got, tc.want)
			}
		})
	}
}

// TestServer_UnobservedAttributesReadNull pins the quality-X behaviour:
// a host that has not observed a value yields TLV null (value nil with
// ok=true), not a fabricated Closed / zero duration.
func TestServer_UnobservedAttributesReadNull(t *testing.T) {
	t.Parallel()
	srv := valve.NewServer(valve.Config{Source: &fakeHost{}})

	for _, attrID := range []uint32{
		valve.AttrOpenDuration,
		valve.AttrDefaultOpenDuration,
		valve.AttrRemainingDuration,
		valve.AttrCurrentState,
		valve.AttrTargetState,
	} {
		got, ok := srv.MatterRead(attrID)
		if !ok {
			t.Fatalf("MatterRead(0x%04X): ok=false, want ok=true with a null value", attrID)
		}
		if got != nil {
			t.Fatalf("MatterRead(0x%04X) = %#v, want nil (null)", attrID, got)
		}
	}
}

// TestServer_UnknownAttributeMatchesSiblingServers measures the unknown-
// attribute result against two sibling cluster servers rather than
// against a convention: the dispatcher turns (nil, false) into
// UnsupportedAttribute, so all three must agree on that shape.
func TestServer_UnknownAttributeMatchesSiblingServers(t *testing.T) {
	t.Parallel()
	const unknown uint32 = 0x1234

	servers := map[string]interface {
		MatterRead(uint32) (any, bool)
	}{
		"valve":          valve.NewServer(valve.Config{Source: fullyObservedHost()}),
		"closurecontrol": closure.NewControlServer(closure.Config{}),
		"doorlock":       lock.NewDoorLockServer(lock.DoorLockConfig{}),
	}
	for name, srv := range servers {
		value, ok := srv.MatterRead(unknown)
		if ok || value != nil {
			t.Fatalf("%s.MatterRead(0x%04X) = (%#v, %v), want (nil, false)", name, unknown, value, ok)
		}
	}
}

// TestServer_OpenReachesPort pins the point of the value-port pattern:
// Open is not answered by the cluster server, it is forwarded.
func TestServer_OpenReachesPort(t *testing.T) {
	t.Parallel()
	sixty := uint32(60)

	cases := []struct {
		name   string
		fields any
		want   valve.OpenRequest
	}{
		{"no fields", nil, valve.OpenRequest{}},
		{
			"OpenDuration from the wire",
			map[uint8]any{0: uint64(60)},
			valve.OpenRequest{HasOpenDuration: true, OpenDuration: &sixty},
		},
		{
			"explicit null OpenDuration stays distinct from absence",
			map[uint8]any{0: nil},
			valve.OpenRequest{HasOpenDuration: true},
		},
		{
			"typed request from a host-side decoder",
			valve.OpenRequest{HasOpenDuration: true, OpenDuration: &sixty},
			valve.OpenRequest{HasOpenDuration: true, OpenDuration: &sixty},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := fullyObservedHost()
			srv := valve.NewServer(valve.Config{Source: host})

			resp, err := srv.MatterInvoke(context.Background(), valve.CmdOpen, tc.fields)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if resp != nil {
				t.Fatalf("Open response = %#v, want nil (status-only command)", resp)
			}
			if len(host.openCalls) != 1 {
				t.Fatalf("host saw %d Open calls, want 1", len(host.openCalls))
			}
			got := host.openCalls[0]
			if got.HasOpenDuration != tc.want.HasOpenDuration {
				t.Fatalf("HasOpenDuration = %v, want %v", got.HasOpenDuration, tc.want.HasOpenDuration)
			}
			switch {
			case tc.want.OpenDuration == nil && got.OpenDuration != nil:
				t.Fatalf("OpenDuration = %d, want nil", *got.OpenDuration)
			case tc.want.OpenDuration != nil && got.OpenDuration == nil:
				t.Fatalf("OpenDuration = nil, want %d", *tc.want.OpenDuration)
			case tc.want.OpenDuration != nil && *got.OpenDuration != *tc.want.OpenDuration:
				t.Fatalf("OpenDuration = %d, want %d", *got.OpenDuration, *tc.want.OpenDuration)
			}
		})
	}
}

// TestServer_CloseReachesPort is the Close half of the same claim.
func TestServer_CloseReachesPort(t *testing.T) {
	t.Parallel()
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})

	if _, err := srv.MatterInvoke(context.Background(), valve.CmdClose, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if host.closeCalls != 1 {
		t.Fatalf("host saw %d Close calls, want 1", host.closeCalls)
	}
	if len(host.openCalls) != 0 {
		t.Fatalf("Close reached the port as %d Open call(s)", len(host.openCalls))
	}
}

// TestServer_HostRefusalIsNotSuccess covers the failure direction: a
// port that refuses must not produce a Success InvokeResult.
func TestServer_HostRefusalIsNotSuccess(t *testing.T) {
	t.Parallel()
	want := errors.New("device unreachable")

	t.Run("Open", func(t *testing.T) {
		t.Parallel()
		host := fullyObservedHost()
		host.openErr = want
		srv := valve.NewServer(valve.Config{Source: host})
		if _, err := srv.MatterInvoke(context.Background(), valve.CmdOpen, nil); !errors.Is(err, want) {
			t.Fatalf("Open error = %v, want wrapped %v", err, want)
		}
	})
	t.Run("Close", func(t *testing.T) {
		t.Parallel()
		host := fullyObservedHost()
		host.closeErr = want
		srv := valve.NewServer(valve.Config{Source: host})
		if _, err := srv.MatterInvoke(context.Background(), valve.CmdClose, nil); !errors.Is(err, want) {
			t.Fatalf("Close error = %v, want wrapped %v", err, want)
		}
	})
	t.Run("no host port", func(t *testing.T) {
		t.Parallel()
		srv := valve.NewServer(valve.Config{})
		if _, err := srv.MatterInvoke(context.Background(), valve.CmdOpen, nil); err == nil {
			t.Fatal("Open without a host port returned success")
		}
	})
}

// TestServer_OpenRejectsFieldsTheFeatureSetCannotHonour keeps a
// controller from being told a level was applied.
func TestServer_OpenRejectsFieldsTheFeatureSetCannotHonour(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		fields any
	}{
		{"TargetLevel needs the LVL feature", map[uint8]any{1: uint64(50)}},
		{"OpenDuration violates min 1", map[uint8]any{0: uint64(0)}},
		{"OpenDuration exceeds uint32", map[uint8]any{0: uint64(1) << 40}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := fullyObservedHost()
			srv := valve.NewServer(valve.Config{Source: host})
			_, err := srv.MatterInvoke(context.Background(), valve.CmdOpen, tc.fields)
			if err == nil {
				t.Fatal("Open accepted a request it cannot honour")
			}
			if got := statusOf(t, err); got != im.StatusConstraintError {
				t.Fatalf("status = %v, want ConstraintError", got)
			}
			if len(host.openCalls) != 0 {
				t.Fatalf("rejected Open still reached the port %d time(s)", len(host.openCalls))
			}
		})
	}
}

func TestServer_UnknownCommand(t *testing.T) {
	t.Parallel()
	srv := valve.NewServer(valve.Config{Source: fullyObservedHost()})
	_, err := srv.MatterInvoke(context.Background(), 0x07, nil)
	if err == nil {
		t.Fatal("unknown command returned success")
	}
	if !errors.Is(err, im.ErrUnsupportedCommand) {
		t.Fatalf("error = %v, want im.ErrUnsupportedCommand", err)
	}
	if got := statusOf(t, err); got != im.StatusUnsupportedCommand {
		t.Fatalf("status = %v, want UnsupportedCommand", got)
	}
}

func TestServer_WriteDefaultOpenDurationReachesPort(t *testing.T) {
	t.Parallel()
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})

	// Unsigned TLV writes arrive as uint64.
	if err := srv.MatterWrite(context.Background(), valve.AttrDefaultOpenDuration, uint64(900)); err != nil {
		t.Fatalf("write DefaultOpenDuration: %v", err)
	}
	// A null write clears the default.
	if err := srv.MatterWrite(context.Background(), valve.AttrDefaultOpenDuration, nil); err != nil {
		t.Fatalf("write null DefaultOpenDuration: %v", err)
	}
	if len(host.defaultSets) != 2 {
		t.Fatalf("host saw %d SetDefaultOpenDuration calls, want 2", len(host.defaultSets))
	}
	if host.defaultSets[0] == nil || *host.defaultSets[0] != 900 {
		t.Fatalf("first write = %v, want 900", host.defaultSets[0])
	}
	if host.defaultSets[1] != nil {
		t.Fatalf("second write = %d, want nil (null)", *host.defaultSets[1])
	}
}

func TestServer_WriteStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		attrID uint32
		value  any
		want   im.StatusCode
	}{
		{"read-only OpenDuration", valve.AttrOpenDuration, uint64(10), im.StatusUnsupportedWrite},
		{"read-only CurrentState", valve.AttrCurrentState, uint64(1), im.StatusUnsupportedWrite},
		{"unknown attribute", 0x1234, uint64(1), im.StatusUnsupportedAttribute},
		{"DefaultOpenDuration min 1", valve.AttrDefaultOpenDuration, uint64(0), im.StatusConstraintError},
		{"DefaultOpenDuration not numeric", valve.AttrDefaultOpenDuration, "600", im.StatusConstraintError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := fullyObservedHost()
			srv := valve.NewServer(valve.Config{Source: host})
			err := srv.MatterWrite(context.Background(), tc.attrID, tc.value)
			if err == nil {
				t.Fatal("write was accepted")
			}
			if got := statusOf(t, err); got != tc.want {
				t.Fatalf("status = %v, want %v", got, tc.want)
			}
			if len(host.defaultSets) != 0 {
				t.Fatalf("rejected write still reached the port %d time(s)", len(host.defaultSets))
			}
		})
	}
}

func TestServer_AdvertisedSurface(t *testing.T) {
	t.Parallel()
	srv := valve.NewServer(valve.Config{Source: fullyObservedHost()})

	wantAttrs := []uint32{
		valve.AttrOpenDuration,
		valve.AttrDefaultOpenDuration,
		valve.AttrRemainingDuration,
		valve.AttrCurrentState,
		valve.AttrTargetState,
	}
	gotAttrs := srv.MatterAttributes()
	if len(gotAttrs) != len(wantAttrs) {
		t.Fatalf("MatterAttributes = %v, want %v", gotAttrs, wantAttrs)
	}
	for i, want := range wantAttrs {
		if gotAttrs[i] != want {
			t.Fatalf("MatterAttributes[%d] = 0x%04X, want 0x%04X", i, gotAttrs[i], want)
		}
	}
	// Every advertised attribute must be answerable, or a wildcard read
	// reports UnsupportedAttribute for something the server claims.
	for _, attrID := range gotAttrs {
		if _, ok := srv.MatterRead(attrID); !ok {
			t.Fatalf("advertised attribute 0x%04X is not readable", attrID)
		}
	}
	gotCmds := srv.MatterAcceptedCommands()
	if len(gotCmds) != 2 || gotCmds[0] != valve.CmdOpen || gotCmds[1] != valve.CmdClose {
		t.Fatalf("MatterAcceptedCommands = %v, want [Open Close]", gotCmds)
	}
	if got := srv.MatterGeneratedCommands(); len(got) != 0 {
		t.Fatalf("MatterGeneratedCommands = %v, want none", got)
	}
}

func TestServer_DataVersionAdvancesOnCommand(t *testing.T) {
	t.Parallel()
	srv := valve.NewServer(valve.Config{Source: fullyObservedHost()})
	before := srv.MatterDataVersion()
	if _, err := srv.MatterInvoke(context.Background(), valve.CmdOpen, nil); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if after := srv.MatterDataVersion(); after == before {
		t.Fatalf("DataVersion stayed at %d after a successful Open", after)
	}
}

// statusOf extracts the Matter status an error carries, so a test
// asserts the code that reaches the wire rather than the message.
func statusOf(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		t.Fatalf("error %v carries no im.StatusCodeError", err)
	}
	return sce.MatterStatusCode()
}
