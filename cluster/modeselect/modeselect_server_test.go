// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package modeselect_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/tlv"
)

// fakeSource is a [modeselect.ModeSource] whose every answer is set by
// the test, so a read that does not come from the port is visible.
type fakeSource struct {
	description  string
	namespace    uint8
	hasNamespace bool
	modes        []modeselect.ModeOptionStruct
	current      uint8

	changed   []uint8
	changeErr error
}

func (f *fakeSource) ModeDescription() string { return f.description }

func (f *fakeSource) ModeNamespace() (uint8, bool) { return f.namespace, f.hasNamespace }

func (f *fakeSource) SupportedModes() []modeselect.ModeOptionStruct { return f.modes }

func (f *fakeSource) CurrentMode() uint8 { return f.current }

func (f *fakeSource) ChangeToMode(_ context.Context, newMode uint8) error {
	if f.changeErr != nil {
		return f.changeErr
	}
	f.changed = append(f.changed, newMode)
	f.current = newMode
	return nil
}

// coffeeSource is the mode list the read tests project. Milk / Sugar is
// the example the cluster's own spec text uses for two ModeSelect
// instances on one appliance.
func coffeeSource() *fakeSource {
	return &fakeSource{
		description:  "Milk",
		namespace:    7,
		hasNamespace: true,
		current:      2,
		modes: []modeselect.ModeOptionStruct{
			{Label: "None", Mode: 0},
			{Label: "Some", Mode: 1, SemanticTags: []modeselect.SemanticTagStruct{{MfgCode: 0x1234, Value: 9}}},
			{Label: "Lots", Mode: 2},
		},
	}
}

// TestMatterReadProjectsThePort pins that every mandatory attribute is
// answered from the host port rather than from state this server keeps
// of its own.
func TestMatterReadProjectsThePort(t *testing.T) {
	t.Parallel()

	src := coffeeSource()
	srv := modeselect.NewServer(modeselect.Config{Source: src})

	tests := []struct {
		name   string
		attrID uint32
		want   any
	}{
		{
			name:   "Description carries the spec's max-64 bound",
			attrID: modeselect.AttrDescription,
			want:   tlv.BoundedString{Value: "Milk", MaxBytes: modeselect.DescriptionMaxBytes},
		},
		{
			name:   "StandardNamespace is the port's enum8",
			attrID: modeselect.AttrStandardNamespace,
			want:   uint8(7),
		},
		{
			name:   "SupportedModes is the port's list",
			attrID: modeselect.AttrSupportedModes,
			want:   src.modes,
		},
		{
			name:   "CurrentMode is the port's mode",
			attrID: modeselect.AttrCurrentMode,
			want:   uint8(2),
		},
		{
			name:   "FeatureMap advertises nothing, so OnMode stays absent",
			attrID: cluster.AttrGlobalFeatureMap,
			want:   uint32(0),
		},
		{
			name:   "ClusterRevision comes from the generated snapshot",
			attrID: cluster.AttrGlobalClusterRevision,
			want:   schema.ClusterRevisions[modeselect.ClusterID],
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

// TestStandardNamespaceAbsentReadsAsNull pins the "X F" quality: a host
// with no standard namespace produces TLV null, not namespace 0, which
// is a real namespace value.
func TestStandardNamespaceAbsentReadsAsNull(t *testing.T) {
	t.Parallel()

	srv := modeselect.NewServer(modeselect.Config{Source: &fakeSource{}})
	got, ok := srv.MatterRead(modeselect.AttrStandardNamespace)
	if !ok {
		t.Fatal("MatterRead(StandardNamespace) returned ok=false")
	}
	if got != nil {
		t.Errorf("StandardNamespace = %#v, want nil (null) when the host has no namespace", got)
	}
}

// TestSupportedModesReadIsACopy pins that a caller cannot reach into the
// host's mode list through the value it read.
func TestSupportedModesReadIsACopy(t *testing.T) {
	t.Parallel()

	src := coffeeSource()
	srv := modeselect.NewServer(modeselect.Config{Source: src})
	got, _ := srv.MatterRead(modeselect.AttrSupportedModes)
	modes, ok := got.([]modeselect.ModeOptionStruct)
	if !ok {
		t.Fatalf("SupportedModes = %T, want []ModeOptionStruct", got)
	}
	modes[0].Label = "clobbered"
	modes[1].SemanticTags[0].Value = 0xFFFF
	if src.modes[0].Label != "None" {
		t.Errorf("host Label = %q, want %q — the read handed out the host's own slice", src.modes[0].Label, "None")
	}
	if src.modes[1].SemanticTags[0].Value != 9 {
		t.Errorf("host SemanticTag Value = %d, want 9 — the read shared the host's tag slice",
			src.modes[1].SemanticTags[0].Value)
	}
}

// TestChangeToModeReachesThePort pins the command path for the two
// payload shapes the server accepts: the typed request, and the tag map
// the bridge's generic fields reader produces for a cluster with no
// typed decoder.
func TestChangeToModeReachesThePort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields any
	}{
		{name: "typed request", fields: modeselect.ChangeToModeRequest{NewMode: 1}},
		{name: "pointer to typed request", fields: &modeselect.ChangeToModeRequest{NewMode: 1}},
		{name: "generic tag map from the bridge", fields: map[uint8]any{modeselect.ChangeToModeFieldNewMode: uint64(1)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := coffeeSource()
			srv := modeselect.NewServer(modeselect.Config{Source: src})
			before := srv.MatterDataVersion()

			resp, err := srv.MatterInvoke(context.Background(), modeselect.CmdChangeToMode, tc.fields)
			if err != nil {
				t.Fatalf("MatterInvoke: %v", err)
			}
			if resp != nil {
				t.Errorf("response = %#v, want nil — ChangeToMode is status-only", resp)
			}
			if !reflect.DeepEqual(src.changed, []uint8{1}) {
				t.Errorf("port saw %v, want [1]", src.changed)
			}
			if got := srv.MatterDataVersion(); got == before {
				t.Errorf("DataVersion stayed %d across a successful ChangeToMode", got)
			}
		})
	}
}

// TestChangeToModeUnsupportedModeIsInvalidCommand pins the status the
// spec names for a mode outside SupportedModes: INVALID_COMMAND (0x85),
// not ConstraintError. matter.js raises it from
// ModeSelectServer.ts:88-95.
func TestChangeToModeUnsupportedModeIsInvalidCommand(t *testing.T) {
	t.Parallel()

	src := coffeeSource()
	srv := modeselect.NewServer(modeselect.Config{Source: src})

	_, err := srv.MatterInvoke(context.Background(), modeselect.CmdChangeToMode,
		modeselect.ChangeToModeRequest{NewMode: 42})
	if err == nil {
		t.Fatal("MatterInvoke accepted mode 42, which is not in SupportedModes")
	}
	var sc im.StatusCodeError
	if !errors.As(err, &sc) {
		t.Fatalf("err %v does not carry a Matter status code", err)
	}
	if got := sc.MatterStatusCode(); got != im.StatusInvalidCommand {
		t.Errorf("MatterStatusCode() = %v, want InvalidCommand (0x85)", got)
	}
	if len(src.changed) != 0 {
		t.Errorf("port saw %v, want no call — an unsupported mode must not reach the device", src.changed)
	}
}

// TestUnknownCommandIsUnsupportedCommand pins that probing a command
// this cluster does not define reports 0x81 rather than a device fault.
func TestUnknownCommandIsUnsupportedCommand(t *testing.T) {
	t.Parallel()

	srv := modeselect.NewServer(modeselect.Config{Source: coffeeSource()})
	_, err := srv.MatterInvoke(context.Background(), 0x07, nil)
	if !errors.Is(err, im.ErrUnsupportedCommand) {
		t.Fatalf("err = %v, want an UnsupportedCommand-typed error", err)
	}
}

// TestMatterWriteRefusesEveryAttribute pins the access column: the four
// implemented attributes are "R V" and report UnsupportedWrite, while
// the two writable ones this server does not implement report
// UnsupportedAttribute — absent, not read-only.
func TestMatterWriteRefusesEveryAttribute(t *testing.T) {
	t.Parallel()

	srv := modeselect.NewServer(modeselect.Config{Source: coffeeSource()})

	tests := []struct {
		name   string
		attrID uint32
		want   im.StatusCode
	}{
		{name: "Description is R V", attrID: modeselect.AttrDescription, want: im.StatusUnsupportedWrite},
		{name: "StandardNamespace is R V", attrID: modeselect.AttrStandardNamespace, want: im.StatusUnsupportedWrite},
		{name: "SupportedModes is R V", attrID: modeselect.AttrSupportedModes, want: im.StatusUnsupportedWrite},
		{name: "CurrentMode is R V", attrID: modeselect.AttrCurrentMode, want: im.StatusUnsupportedWrite},
		{name: "StartUpMode is not implemented", attrID: modeselect.AttrStartUpMode, want: im.StatusUnsupportedAttribute},
		{name: "OnMode needs DEPONOFF", attrID: modeselect.AttrOnMode, want: im.StatusUnsupportedAttribute},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := srv.MatterWrite(context.Background(), tc.attrID, uint8(1))
			var sc im.StatusCodeError
			if !errors.As(err, &sc) {
				t.Fatalf("err %v does not carry a Matter status code", err)
			}
			if got := sc.MatterStatusCode(); got != tc.want {
				t.Errorf("MatterStatusCode() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAdvertisedSurface pins the cluster identity and the lists the IM
// dispatcher synthesises the global attributes from.
func TestAdvertisedSurface(t *testing.T) {
	t.Parallel()

	srv := modeselect.NewServer(modeselect.Config{Source: coffeeSource()})
	if got := srv.MatterClusterID(); got != 0x0050 {
		t.Errorf("MatterClusterID() = %#04x, want 0x0050", got)
	}
	wantAttrs := []uint32{0x0000, 0x0001, 0x0002, 0x0003}
	if got := srv.MatterAttributes(); !reflect.DeepEqual(got, wantAttrs) {
		t.Errorf("MatterAttributes() = %#v, want %#v", got, wantAttrs)
	}
	if got := srv.MatterAcceptedCommands(); !reflect.DeepEqual(got, []uint32{0x00}) {
		t.Errorf("MatterAcceptedCommands() = %#v, want [0x00]", got)
	}
	if got := srv.MatterGeneratedCommands(); got != nil {
		t.Errorf("MatterGeneratedCommands() = %#v, want nil — ChangeToMode responds with a status", got)
	}
	// Only CurrentMode carries quality N; the other three are quality F
	// and never report.
	if got := srv.MatterReportable(); !reflect.DeepEqual(got, []uint32{modeselect.AttrCurrentMode}) {
		t.Errorf("MatterReportable() = %#v, want [CurrentMode]", got)
	}
}

// notifyingSource is a [modeselect.ModeSource] that also implements
// [contract.ChangeNotifier], the way a host whose device pushes mode
// changes does.
type notifyingSource struct {
	*fakeSource
	cb    func()
	unsub int
}

func (n *notifyingSource) OnMatterValueChanged(cb func()) func() {
	n.cb = cb
	return func() { n.unsub++ }
}

// TestDeviceSideChangeReachesSubscribers pins the change hop: a mode the
// device changed on its own reaches a subscriber. CurrentMode is
// quality N and is listed as reportable, so without a push path from
// the port the report would never fire and the DataVersion would never
// move outside a command.
func TestDeviceSideChangeReachesSubscribers(t *testing.T) {
	t.Parallel()

	src := &notifyingSource{fakeSource: coffeeSource()}
	srv := modeselect.NewServer(modeselect.Config{Source: src})
	before := srv.MatterDataVersion()

	fired := 0
	unsub := srv.OnMatterValueChanged(func() { fired++ })
	if src.cb == nil {
		t.Fatal("the server did not subscribe to the port's notifier")
	}

	src.current = 1
	src.cb()
	if fired != 1 {
		t.Errorf("subscriber fired %d times, want 1", fired)
	}
	if got := srv.MatterDataVersion(); got == before {
		t.Errorf("DataVersion stayed %d across a device-side mode change", got)
	}
	unsub()
	if src.unsub != 1 {
		t.Errorf("port saw %d unsubscribes, want 1", src.unsub)
	}
}

// TestChangeNotifierIsANoOpWithoutAPortThatPushes pins that a port
// which cannot notify yields a usable no-op rather than a nil closure
// the bridge would call.
func TestChangeNotifierIsANoOpWithoutAPortThatPushes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		src  modeselect.ModeSource
	}{
		{name: "port without a notifier", src: coffeeSource()},
		{name: "no port at all", src: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := modeselect.NewServer(modeselect.Config{Source: tc.src})
			unsub := srv.OnMatterValueChanged(func() { t.Error("callback fired without a notifying port") })
			if unsub == nil {
				t.Fatal("OnMatterValueChanged returned a nil unsubscribe")
			}
			unsub()
		})
	}
}

// TestHostSuppliedDataVersionIsTheOneReported pins that a host which
// owns the counter — so the version survives server reconstruction and
// a device-side change it observes elsewhere still bumps it — sees its
// own tracker used, not a second one hidden in the server.
func TestHostSuppliedDataVersionIsTheOneReported(t *testing.T) {
	t.Parallel()

	var host cluster.DataVersionTracker
	srv := modeselect.NewServer(modeselect.Config{Source: coffeeSource(), DataVersion: &host})

	if got, want := srv.MatterDataVersion(), host.Current(); got != want {
		t.Fatalf("MatterDataVersion() = %d, want the host's %d", got, want)
	}
	want := host.Bump()
	if got := srv.MatterDataVersion(); got != want {
		t.Errorf("MatterDataVersion() = %d after a host-side bump, want %d", got, want)
	}
	if _, err := srv.MatterInvoke(context.Background(), modeselect.CmdChangeToMode,
		modeselect.ChangeToModeRequest{NewMode: 1}); err != nil {
		t.Fatalf("MatterInvoke: %v", err)
	}
	if got := host.Current(); got == want {
		t.Errorf("the host's tracker stayed %d across a ChangeToMode", got)
	}
}

// TestNilSourceReadsNullAndRefusesCommands pins the missing-port
// behaviour: every projected attribute reads as TLV null and
// ChangeToMode fails, rather than the server dereferencing a nil port
// or answering with a mode nothing reported.
func TestNilSourceReadsNullAndRefusesCommands(t *testing.T) {
	t.Parallel()

	srv := modeselect.NewServer(modeselect.Config{})
	for _, attrID := range srv.MatterAttributes() {
		got, ok := srv.MatterRead(attrID)
		if !ok {
			t.Errorf("MatterRead(0x%04X) returned ok=false, want a null value", attrID)
		}
		if got != nil {
			t.Errorf("MatterRead(0x%04X) = %#v, want nil (TLV null) without a port", attrID, got)
		}
	}
	if _, err := srv.MatterInvoke(context.Background(), modeselect.CmdChangeToMode,
		modeselect.ChangeToModeRequest{NewMode: 1}); err == nil {
		t.Error("ChangeToMode succeeded without a host port")
	}
}

// TestRevisionComesFromTheGeneratedSnapshot pins that the revision is
// read from the generated matter.js schema rather than restated, so a
// regeneration that moves it carries this server along.
func TestRevisionComesFromTheGeneratedSnapshot(t *testing.T) {
	t.Parallel()

	want, ok := schema.ClusterRevisions[modeselect.ClusterID]
	if !ok {
		t.Fatalf("cluster %#04x missing from the generated schema", modeselect.ClusterID)
	}
	if got := modeselect.Revision(); got != want {
		t.Errorf("Revision() = %d, want %d from the snapshot", got, want)
	}
}
