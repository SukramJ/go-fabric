// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/device"
)

type chimePort struct{}

func (chimePort) InstalledChimeSounds() ([]chimedef.ChimeSoundStruct, error) {
	return []chimedef.ChimeSoundStruct{{ChimeId: 1, Name: "Ding Dong"}}, nil
}
func (chimePort) PlayChimeSound(context.Context, uint8) error { return nil }

type ptzPort struct{}

func (ptzPort) MptzSetPosition(context.Context, avdef.MPTZStruct) error  { return nil }
func (ptzPort) MptzRelativeMove(context.Context, avdef.MPTZStruct) error { return nil }

// TestCameraControllerServesTheKitRequestor: the CameraController's
// WebRtcTransportRequestor is the kit's server (matter.js
// CameraControllerDevice.with(WebRtcTransportRequestorServer)), with or
// without a RequestorPort.
func TestCameraControllerServesTheKitRequestor(t *testing.T) {
	t.Parallel()
	d, err := device.NewCameraController(device.Options{StableKey: device.StringKey("cam"), Name: "cam"})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := d.WebRtcTransportRequestor().Underlying().(*camera.Requestor)
	if !ok {
		t.Fatalf("WebRtcTransportRequestor is %T", d.WebRtcTransportRequestor().Underlying())
	}
	video := []uint16{1}
	if err := r.UpsertSession(wrdef.WebRTCSessionStruct{Id: 1, PeerNodeId: 2, PeerEndpointId: 1, StreamUsage: wrdef.StreamUsageLiveView, VideoStreams: &video, FabricIndex: 1}); err != nil {
		t.Fatal(err)
	}
	if st := d.WebRtcTransportRequestor().Get(); len(st.CurrentSessions) != 1 {
		t.Fatalf("CurrentSessions %+v", st.CurrentSessions)
	}
}

// TestCameraServesTheKitAvSettings: a Camera's optional
// CameraAvSettingsUserLevelManagement declared with a MechanicalPTZ is the
// kit's server, its limits from Initial.
func TestCameraServesTheKitAvSettings(t *testing.T) {
	t.Parallel()
	panMax := int16(90)
	d, err := device.NewCamera(device.Options{StableKey: device.StringKey("ptz"), Name: "ptz"},
		avdef.Declare{Features: avdef.FeatureMechanicalPan, Initial: avdef.Patch{PanMax: &panMax}, MechanicalPTZ: ptzPort{}})
	if err != nil {
		t.Fatal(err)
	}
	av, ok := d.Server(avdef.ClusterID).(*camera.AvSettings)
	if !ok {
		t.Fatalf("CameraAvSettingsUserLevelManagement is %T", d.Server(avdef.ClusterID))
	}
	if v, _ := av.MatterRead(avdef.AttrPanMax); v != int16(90) {
		t.Fatalf("PanMax %v, want the declared 90", v)
	}
}

// TestChimeServesTheKitChimeWithItsPort: a Chime declared with a ChimePort
// is the kit's server, SelectedChime from Initial; without the port the
// cluster keeps the generated server.
func TestChimeServesTheKitChimeWithItsPort(t *testing.T) {
	t.Parallel()
	one := uint8(1)
	d, err := device.NewChime(device.Options{StableKey: device.StringKey("chime"), Name: "chime"},
		chimedef.Declare{Initial: chimedef.Patch{SelectedChime: &one}, ChimePort: chimePort{}})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := d.Chime().Underlying().(*camera.Chime)
	if !ok {
		t.Fatalf("Chime is %T", d.Chime().Underlying())
	}
	if c.SelectedChime() != 1 {
		t.Fatalf("SelectedChime %d", c.SelectedChime())
	}
	plain, err := device.NewChime(device.Options{StableKey: device.StringKey("chime-plain"), Name: "chime"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.Chime().Underlying().(*spec.Server); !ok {
		t.Fatalf("Chime without a port is %T", plain.Chime().Underlying())
	}
}
