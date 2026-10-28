// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/camera"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
)

type fakeChime struct{}

func (fakeChime) InstalledChimeSounds() ([]chimedef.ChimeSoundStruct, error) {
	return []chimedef.ChimeSoundStruct{{ChimeId: 1, Name: "Ding"}}, nil
}
func (fakeChime) PlayChimeSound(context.Context, uint8) error { return nil }

type fakePTZ struct{}

func (fakePTZ) MptzSetPosition(context.Context, avdef.MPTZStruct) error  { return nil }
func (fakePTZ) MptzRelativeMove(context.Context, avdef.MPTZStruct) error { return nil }

type fakeAudio struct{}

func (fakeAudio) AudioStreamAllocate(context.Context, csdef.AudioStreamStruct) (uint16, error) {
	return 1, nil
}

func (fakeAudio) AudioStreamDeallocate(context.Context, uint16) error { return nil }

type fakeZones struct{}

func (fakeZones) CreateTwoDCartesianZone(context.Context, zmdef.TwoDCartesianZoneStruct) (uint16, error) {
	return 1, nil
}

func (fakeZones) UpdateTwoDCartesianZone(context.Context, uint16, zmdef.TwoDCartesianZoneStruct) error {
	return nil
}

func (fakeZones) RemoveZone(context.Context, uint16) error { return nil }

func (fakeZones) CreateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error { return nil }

func (fakeZones) UpdateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error { return nil }

func (fakeZones) RemoveTrigger(context.Context, uint16) error { return nil }

// cameraCases are the camera servers of TestSelfHeldDefaults
// (defaults_camera.go): the requestor without a port, Chime and
// CameraAvSettingsUserLevelManagement on theirs; CameraAvStreamManagement
// and ZoneManagement on theirs (brief 26-A2).
type fakeProvider struct{}

func (fakeProvider) SolicitOffer(context.Context, wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return wpdef.WebRTCSessionStruct{}, false, nil
}

func (fakeProvider) ProvideOffer(context.Context, wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return wpdef.WebRTCSessionStruct{}, nil
}
func (fakeProvider) ProvideAnswer(context.Context, uint16, string) error { return nil }
func (fakeProvider) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}
func (fakeProvider) EndSession(context.Context, uint16, uint8) error { return nil }

type fakePush struct{}

func (fakePush) AllocatePushTransport(context.Context, uint16, uint8, padef.TransportOptionsStruct, padef.TLSCredentials) error {
	return nil
}
func (fakePush) DeallocatePushTransport(context.Context, uint16) error { return nil }
func (fakePush) ModifyPushTransport(context.Context, uint16, padef.TransportOptionsStruct) error {
	return nil
}

func (fakePush) SetTransportStatus(context.Context, []uint16, padef.TransportStatusEnum) error {
	return nil
}

func (fakePush) ManuallyTriggerTransport(context.Context, uint16, padef.TriggerActivationReasonEnum, *padef.TransportMotionTriggerTimeControlStruct) error {
	return nil
}
func (fakePush) TransportBusy(uint16) bool { return false }
func (fakePush) ValidateBandwidthLimit(context.Context, padef.StreamUsageEnum, *uint16, *uint16) error {
	return nil
}
func (fakePush) CMAFSessionNumber(uint16) (uint64, bool) { return 0, false }

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
		{csdef.ClusterID, func() (*Device, error) {
			return New(&IntercomType, testOptions("intercom"), csdef.Declare{AudioStreamPort: fakeAudio{}})
		}, (*camera.AvStream)(nil)},
		{zmdef.ClusterID, func() (*Device, error) {
			maxZones, sensMax := uint8(4), uint8(5)
			return New(&CameraType, testOptions("zones"), zmdef.Declare{
				Initial: zmdef.Patch{MaxZones: &maxZones, SensitivityMax: &sensMax}, ZonePort: fakeZones{},
			})
		}, (*camera.Zones)(nil)},
		{wpdef.ClusterID, func() (*Device, error) {
			return New(&CameraType, testOptions("provider"), wpdef.Declare{ProviderPort: fakeProvider{}})
		}, (*camera.Provider)(nil)},
		{padef.ClusterID, func() (*Device, error) {
			return New(&CameraType, testOptions("push"), padef.Declare{PushTransportPort: fakePush{}})
		}, (*camera.PushTransport)(nil)},
	}
}
