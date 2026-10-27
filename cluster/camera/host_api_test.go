// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestAvSettingsHostAPI covers the host's side of the camera settings: the
// limit setters (chip :339-407), the stream bookkeeping (:469-580), the
// typed Set, and the data version a change moves.
func TestAvSettingsHostAPI(t *testing.T) { //nolint:funlen // one subtest per host call
	t.Run("limits", func(t *testing.T) {
		a := newAvSettings(t)
		v := a.MatterDataVersion()
		if err := a.SetPanMin(-90); err != nil {
			t.Fatal(err)
		}
		if err := a.SetPanMax(90); err != nil {
			t.Fatal(err)
		}
		if err := a.SetTiltMin(-45); err != nil {
			t.Fatal(err)
		}
		if err := a.SetTiltMax(45); err != nil {
			t.Fatal(err)
		}
		if err := a.SetZoomMax(10); err != nil {
			t.Fatal(err)
		}
		if a.MatterDataVersion() == v {
			t.Fatal("the data version did not move")
		}
		if err := a.SetPanMin(1); !errors.Is(err, spec.ErrInvalidValue) {
			t.Fatalf("PanMin 1: %v", err)
		}
		if err := a.SetZoomMax(1); !errors.Is(err, spec.ErrInvalidValue) {
			t.Fatalf("ZoomMax 1: %v", err)
		}
		far := int16(91)
		if got := statusOf(invokeAv(a, avdef.CmdMptzSetPosition, avdef.MptzSetPositionRequest{Pan: &far})); got != im.StatusConstraintError {
			t.Fatalf("Pan 91 beyond PanMax 90: %s", got)
		}
	})
	t.Run("a limit without its feature", func(t *testing.T) {
		a := newAvSettings(t, func(cfg *camera.AvSettingsConfig) { cfg.Features = camera.FeatureDigitalPTZ })
		if err := a.SetTiltMax(10); !errors.Is(err, spec.ErrNotServed) {
			t.Fatalf("TiltMax without MTILT: %v", err)
		}
	})
	t.Run("streams", func(t *testing.T) {
		a := newAvSettings(t)
		hd := camera.Viewport{X2: 1280, Y2: 720}
		a.AddMoveCapableVideoStream(1, fullHD)
		a.AddMoveCapableVideoStream(2, fullHD)
		a.UpdateMoveCapableVideoStream(2, hd)
		a.UpdateMoveCapableVideoStream(9, hd)
		if got := a.Streams(); got[0].Viewport != fullHD || got[1].Viewport != hd {
			t.Fatalf("streams %+v", got)
		}
		a.UpdateMoveCapableVideoStreams(hd)
		a.RemoveMoveCapableVideoStream(1)
		a.RemoveMoveCapableVideoStream(9)
		if got := streamsOf(t, a); !slices.Equal(got, []camera.DPTZStream{{VideoStreamId: 2, Viewport: hd}}) {
			t.Fatalf("DPTZStreams %+v", got)
		}
	})
	t.Run("typed Set", func(t *testing.T) {
		a := newAvSettings(t)
		changes := 0
		unsubscribe := a.OnAttributeChange(func(contract.Change) { changes++ })
		defer unsubscribe()
		w := avdef.Wrap(a)
		zoomMax := uint8(50)
		presets := []avdef.MPTZPresetStruct{{PresetId: 1, Name: "door", Settings: camera.MPTZ{Pan: ptr(int16(0)), Tilt: ptr(int16(0)), Zoom: ptr(uint8(1))}}}
		streams := []avdef.DPTZStruct{{VideoStreamId: 4, Viewport: fullHD}}
		pos := camera.MPTZ{Pan: ptr(int16(30)), Tilt: ptr(int16(-10)), Zoom: ptr(uint8(5))}
		if err := w.Set(avdef.Patch{ZoomMax: &zoomMax, MptzPresets: &presets, DptzStreams: &streams, MptzPosition: &pos}); err != nil {
			t.Fatal(err)
		}
		st := w.Get()
		if st.ZoomMax != 50 || len(st.MptzPresets) != 1 || len(st.DptzStreams) != 1 || *st.MptzPosition.Pan != 30 {
			t.Fatalf("state %+v", st)
		}
		if a.NextPresetID() != 2 || changes == 0 {
			t.Fatalf("next preset id %d, %d changes", a.NextPresetID(), changes)
		}
		nine := uint8(9)
		if err := w.Set(avdef.Patch{MaxPresets: &nine}); !errors.Is(err, spec.ErrNotHeld) {
			t.Fatalf("MaxPresets: %v", err)
		}
		if err := a.MatterWrite(context.Background(), avdef.AttrPanMax, int16(10)); statusOf(err) == im.StatusSuccess {
			t.Fatal("a write of PanMax succeeded")
		}
	})
}

// TestChimeHostAPI covers the chime's typed Set, SoundsChanged and the
// data version.
func TestChimeHostAPI(t *testing.T) {
	m := newMockChime()
	c := newChime(t, m)
	w := chimedef.Wrap(c)
	v := c.MatterDataVersion()
	two, off := uint8(2), false
	if err := w.Set(chimedef.Patch{SelectedChime: &two, Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if c.SelectedChime() != 2 || c.Enabled() || c.MatterDataVersion() == v {
		t.Fatalf("SelectedChime %d, Enabled %v", c.SelectedChime(), c.Enabled())
	}
	nine := uint8(9)
	if err := w.Set(chimedef.Patch{SelectedChime: &nine}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Fatalf("SelectedChime 9: %v", err)
	}
	sounds := []chimedef.ChimeSoundStruct{}
	if err := w.Set(chimedef.Patch{InstalledChimeSounds: &sounds}); !errors.Is(err, spec.ErrNotHeld) {
		t.Fatalf("InstalledChimeSounds: %v", err)
	}
	v = c.MatterDataVersion()
	if err := c.SoundsChanged(); err != nil || c.MatterDataVersion() == v {
		t.Fatalf("SoundsChanged: %v", err)
	}
	if _, err := camera.NewChime(camera.ChimeConfig{}); !errors.Is(err, camera.ErrNoChimePort) {
		t.Fatalf("a Chime without a port: %v", err)
	}
}

// TestRequestorHostAPI covers the typed Set of CurrentSessions, the local
// read that sees every fabric, the refusal of a write and of a command
// without a peer's node session.
func TestRequestorHostAPI(t *testing.T) {
	r := newRequestor(t, nil)
	w := wrdef.Wrap(r)
	v := r.MatterDataVersion()
	list := []wrdef.WebRTCSessionStruct{makeSession(withFabric(1)), makeSession(withFabric(2))}
	if err := w.Set(wrdef.Patch{CurrentSessions: &list}); err != nil {
		t.Fatal(err)
	}
	if r.MatterDataVersion() == v {
		t.Fatal("the data version did not move")
	}
	got := w.Get().CurrentSessions
	if len(got) != 2 || got[1].VideoStreamId == nil || got[1].VideoStreamId.Value != 10 {
		t.Fatalf("CurrentSessions %+v", got)
	}
	filtered, _ := r.MatterReadFiltered(im.WithFabricFilter(context.Background(), true, 2), wrdef.AttrCurrentSessions)
	if l := filtered.(spec.List[camera.WebRTCSession]); len(l) != 1 || l[0].FabricIndex != 2 {
		t.Fatalf("fabric 2 reads %+v", l)
	}
	if err := r.UpsertSession(makeSession(withFabric(0))); err == nil {
		t.Fatal("a session of fabric 0 was stored")
	}
	if err := r.MatterWrite(context.Background(), wrdef.AttrCurrentSessions, nil); statusOf(err) == im.StatusSuccess {
		t.Fatal("a write of CurrentSessions succeeded")
	}
	if _, err := r.MatterInvoke(context.Background(), wrdef.CmdEnd, wrdef.EndRequest{WebRtcSessionId: 1}); statusOf(err) != im.StatusFailure {
		t.Fatalf("End without a peer: %v", err)
	}
}
