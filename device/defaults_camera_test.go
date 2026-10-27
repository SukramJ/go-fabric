// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/camera"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
)

type fakeChime struct{}

func (fakeChime) InstalledChimeSounds() ([]chimedef.ChimeSoundStruct, error) {
	return []chimedef.ChimeSoundStruct{{ChimeId: 1, Name: "Ding"}}, nil
}
func (fakeChime) PlayChimeSound(context.Context, uint8) error { return nil }

type fakePTZ struct{}

func (fakePTZ) MptzSetPosition(context.Context, avdef.MPTZStruct) error  { return nil }
func (fakePTZ) MptzRelativeMove(context.Context, avdef.MPTZStruct) error { return nil }

// cameraCases are the camera servers of TestSelfHeldDefaults
// (defaults_camera.go): the requestor without a port, Chime and
// CameraAvSettingsUserLevelManagement on theirs.
func cameraCases() []struct {
	cluster uint32
	build   func() (*Device, error)
	want    any
} {
	return []struct {
		cluster uint32
		build   func() (*Device, error)
		want    any
	}{
		{wrdef.ClusterID, func() (*Device, error) { return New(&CameraControllerType, testOptions("cc")) }, (*camera.Requestor)(nil)},
		{chimedef.ClusterID, func() (*Device, error) {
			return New(&ChimeType, testOptions("chime"), chimedef.Declare{ChimePort: fakeChime{}})
		}, (*camera.Chime)(nil)},
		{avdef.ClusterID, func() (*Device, error) {
			return New(&CameraType, testOptions("ptz"), avdef.Declare{Features: avdef.FeatureMechanicalPan, MechanicalPTZ: fakePTZ{}})
		}, (*camera.AvSettings)(nil)},
	}
}
