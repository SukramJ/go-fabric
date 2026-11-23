// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	avdef "thingfabric.dev/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/im"
)

// TestAvSettingsPresetsHeldToMaxPresets: a host's MPTZPresets longer than
// MaxPresets is refused, and the server's presets and the attribute stay
// as they were. Before, SetAttributes answered nil, Presets() held the
// three presets and the attribute kept its old value — the publication's
// error was dropped (review II, R2-P6-store-1608, R2-P6-err-1611).
func TestAvSettingsPresetsHeldToMaxPresets(t *testing.T) {
	t.Parallel()
	a := newAvSettings(t, func(c *camera.AvSettingsConfig) { c.MaxPresets = 2 })
	at := camera.MPTZ{Pan: ptr(int16(0)), Tilt: ptr(int16(0)), Zoom: ptr(uint8(1))}
	three := []camera.MPTZPreset{{PresetId: 1, Name: "a", Settings: at}, {PresetId: 2, Name: "b", Settings: at}, {PresetId: 3, Name: "c", Settings: at}}
	if err := a.SetAttributes(map[uint32]any{avdef.AttrMptzPresets: three}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("3 presets with MaxPresets 2: %v, want ErrInvalidValue", err)
	}
	if n := len(a.Presets()); n != 0 {
		t.Errorf("Presets() holds %d after the refusal", n)
	}
	if l := readAv[spec.List[camera.MPTZPreset]](t, a, avdef.AttrMptzPresets); len(l) != 0 {
		t.Errorf("MPTZPresets = %v after the refusal", l)
	}
	// Full is full: with MaxPresets presets held a save under a new id
	// is RESOURCE_EXHAUSTED.
	if err := a.SetAttributes(map[uint32]any{avdef.AttrMptzPresets: three[:2]}); err != nil {
		t.Fatal(err)
	}
	if err := invokeAv(a, avdef.CmdMptzSavePreset, avdef.MptzSavePresetRequest{Name: "c"}); statusOf(err) != im.StatusResourceExhausted {
		t.Errorf("save with every preset taken: %v, want RESOURCE_EXHAUSTED", err)
	}
}

// TestCameraTablesAreHandedOutAsDeepCopies is the package's guard of
// decision 142: a caller that edits through a pointer or a list of an
// entry a getter returned changes nothing the server serves. Before, the
// getters returned slices.Clone of the tables, whose entries shared their
// lists and optional fields with the server's (review II, R2-P6-err-1460's
// class).
func TestCameraTablesAreHandedOutAsDeepCopies(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t, nil)
	video := []uint16{1}
	if err := p.UpsertSession(camera.ProviderSession{Id: 1, FabricIndex: 1, VideoStreams: &video}); err != nil {
		t.Fatal(err)
	}
	video[0] = 9 // the list handed in
	(*p.Sessions()[0].VideoStreams)[0] = 8
	if got := (*p.Sessions()[0].VideoStreams)[0]; got != 1 {
		t.Errorf("Provider session stream %d, want 1", got)
	}

	push, _ := newPush(t)
	allocatePush(t, push, motionOptions())
	push.Connections()[0].TransportOptions.Url = "https://changed.example/"
	if got := push.Connections()[0].TransportOptions.Url; got == "https://changed.example/" {
		t.Error("PushTransport connection changed through Connections()")
	}

	a := newAvSettings(t)
	pan := int16(5)
	if err := a.SetAttributes(map[uint32]any{avdef.AttrMptzPresets: []camera.MPTZPreset{{PresetId: 1, Name: "a", Settings: camera.MPTZ{Pan: &pan, Tilt: ptr(int16(0)), Zoom: ptr(uint8(1))}}}}); err != nil {
		t.Fatal(err)
	}
	pan = 6
	*a.Presets()[0].Settings.Pan = 7
	*a.Position().Pan = 7
	if got := *a.Presets()[0].Settings.Pan; got != 5 {
		t.Errorf("preset Pan %d, want 5", got)
	}
	if got := a.Position().Pan; got != nil && *got == 7 {
		t.Error("MPTZPosition changed through Position()")
	}
}

// TestAvSettingsHostChangesAreAllOrNothing: a host change the store
// refuses after CheckSet accepted it — a preset without the Tilt MTILT
// makes mandatory, a field conformance CheckSet does not judge — leaves
// every attribute of the change and the server's own state as they were.
// Before, the state took the change and the publication's error was
// dropped (review II, R2-P6-err-1611, with R2-P6-par-1304's check).
func TestAvSettingsHostChangesAreAllOrNothing(t *testing.T) {
	t.Parallel()
	a := newAvSettings(t)
	panMin := readAv[int16](t, a, avdef.AttrPanMin)
	bad := []camera.MPTZPreset{{PresetId: 1, Name: "door", Settings: camera.MPTZ{Pan: ptr(int16(5))}}}
	if err := a.SetAttributes(map[uint32]any{avdef.AttrPanMin: int16(-10), avdef.AttrMptzPresets: bad}); err == nil {
		t.Fatal("a preset without Tilt stored under MTILT")
	}
	if got := readAv[int16](t, a, avdef.AttrPanMin); got != panMin {
		t.Errorf("PanMin %d after a refused SetAttributes, want %d", got, panMin)
	}
	if len(a.Presets()) != 0 {
		t.Errorf("Presets() = %+v after a refused SetAttributes", a.Presets())
	}
	if id := a.NextPresetID(); id != 1 {
		t.Errorf("NextPresetID %d after a refused SetAttributes, want 1", id)
	}
}

// streamlessProvider is a provider port whose session names no stream.
type streamlessProvider struct{ *mockProvider }

func (s streamlessProvider) SolicitOffer(ctx context.Context, args camera.OfferArgs) (session camera.ProviderSession, deferred bool, err error) {
	session, deferred, err = s.mockProvider.SolicitOffer(ctx, args)
	session.VideoStreams, session.AudioStreams = nil, nil
	return session, deferred, err
}

func (s streamlessProvider) ProvideOffer(ctx context.Context, args camera.OfferArgs) (camera.ProviderSession, error) {
	session, err := s.mockProvider.ProvideOffer(ctx, args)
	session.VideoStreams, session.AudioStreams = nil, nil
	return session, err
}

// TestProviderStoresNoStreamlessSession: a session the port returns
// without VideoStreams and AudioStreams fails CurrentSessions' "O.a+" and
// is not stored — the command answers FAILURE and the table stays empty.
// A request naming no stream is INVALID_COMMAND before the port runs
// (TestSolicitOfferRules, "no stream field"), so the server builds no such
// session itself (review II, R2-P6-par-1304 row 8, R2-P6-err-1611).
func TestProviderStoresNoStreamlessSession(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t, cameraStreams(), func(c *camera.ProviderConfig) {
		c.Port = streamlessProvider{&mockProvider{}}
	})
	_, err := p.MatterInvoke(peer(1, 0x55), wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageLiveView))
	if statusOf(err) != im.StatusFailure {
		t.Fatalf("SolicitOffer with a streamless session: %v, want FAILURE", err)
	}
	if n := len(p.Sessions()); n != 0 {
		t.Errorf("%d sessions stored", n)
	}
	if l := readList[camera.ProviderSession](t, p, wpdef.AttrCurrentSessions); len(l) != 0 {
		t.Errorf("CurrentSessions = %+v", l)
	}
}

// TestProviderEndsTheSessionItCouldNotStore (R2-C2-err-1602): the port
// created the session, the table refused it, so the server ends it at the
// port before it answers FAILURE — no controller can end a session the
// table does not hold.
func TestProviderEndsTheSessionItCouldNotStore(t *testing.T) {
	t.Parallel()
	u, ep := wpdef.StreamUsageLiveView, uint16(1)
	for name, call := range map[string]struct {
		cmd    uint32
		fields any
	}{
		"SolicitOffer": {wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageLiveView)},
		"ProvideOffer": {wpdef.CmdProvideOffer, wpdef.ProvideOfferRequest{
			WebRtcSessionId: spec.NullOf[uint16](), Sdp: "v=0", StreamUsage: &u, OriginatingEndpointId: &ep, VideoStreamId: nnull(),
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := &mockProvider{}
			p, _ := newProvider(t, cameraStreams(), func(c *camera.ProviderConfig) {
				c.Port = streamlessProvider{m}
			})
			if _, err := p.MatterInvoke(peer(1, 0x55), call.cmd, call.fields); statusOf(err) != im.StatusFailure {
				t.Fatalf("%s with a streamless session: %v, want FAILURE", name, err)
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if len(m.offers) != 1 || !slices.Equal(m.ended, []uint16{m.offers[0].SessionID}) {
				t.Errorf("offers %d, ended %v; want the offered session ended", len(m.offers), m.ended)
			}
		})
	}
}

func readList[T spec.Encodable](t *testing.T, srv interface{ MatterRead(uint32) (any, bool) }, attrID uint32) spec.List[T] {
	t.Helper()
	v, ok := srv.MatterRead(attrID)
	if !ok {
		t.Fatalf("attribute 0x%04X not readable", attrID)
	}
	l, _ := v.(spec.List[T])
	return l
}
