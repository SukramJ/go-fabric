// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	avdef "thingfabric.dev/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	chimedef "thingfabric.dev/go-fabric/cluster/spec/chime"
	padef "thingfabric.dev/go-fabric/cluster/spec/pushavstreamtransport"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	zmdef "thingfabric.dev/go-fabric/cluster/spec/zonemanagement"
	"thingfabric.dev/go-fabric/im"
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

func (fakeZones) CreateTwoDCartesianZone(context.Context, uint16, zmdef.TwoDCartesianZoneStruct) error {
	return nil
}

func (fakeZones) UpdateTwoDCartesianZone(context.Context, uint16, zmdef.TwoDCartesianZoneStruct) error {
	return nil
}

func (fakeZones) RemoveZone(context.Context, uint16) error { return nil }

func (fakeZones) CreateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error { return nil }

func (fakeZones) UpdateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error { return nil }

func (fakeZones) RemoveTrigger(context.Context, uint16) error { return nil }

// keptZones is a ZonePort that reports the last zone ID it was handed
// (camera.LastZoneIDSource), as a host that persisted it would after a
// restart.
type keptZones struct {
	fakeZones
	last uint16
}

func (k keptZones) LastZoneID() (uint16, bool) { return k.last, true }

// TestZonesThroughTheFacadeContinueAfterThePortsLastID: a camera built
// through the device facade, restarted with its zones 0 and 1 and a port
// that reports 2 as the last ID it was handed (zone 2 was removed), gives
// the next zone 3 — matter.js "does not reuse the ID of a removed zone
// after a restart" (ZoneManagementServerTest.ts:272-292 at b25ed960);
// before owner decision O-7 the facade passed no last ID and the zone got 2.
func TestZonesThroughTheFacadeContinueAfterThePortsLastID(t *testing.T) {
	square := func(offset uint16) zmdef.TwoDCartesianZoneStruct {
		return zmdef.TwoDCartesianZoneStruct{Name: "zone", Use: zmdef.ZoneUsePrivacy, Vertices: []zmdef.TwoDCartesianVertexStruct{
			{X: 10 + offset, Y: 10}, {X: 20 + offset, Y: 10}, {X: 20 + offset, Y: 20}, {X: 10 + offset, Y: 20},
		}}
	}
	zone := func(id uint16) zmdef.ZoneInformationStruct {
		z := square(id)
		return zmdef.ZoneInformationStruct{ZoneId: id, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceUser, TwoDCartesianZone: &z}
	}
	maxUser, maxZones, sensMax := uint8(5), uint8(6), uint8(5)
	twoDMax := zmdef.TwoDCartesianVertexStruct{X: 1919, Y: 1079}
	kept := []zmdef.ZoneInformationStruct{zone(0), zone(1)}
	d, err := newWith(CameraType(), testOptions("zones"), zmdef.Declare{
		Features: zmdef.FeatureTwoDimensionalCartesianZone | zmdef.FeatureUserDefined,
		Initial:  zmdef.Patch{MaxUserDefinedZones: &maxUser, MaxZones: &maxZones, SensitivityMax: &sensMax, TwoDCartesianMax: &twoDMax, Zones: &kept},
		ZonePort: keptZones{last: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	z, ok := d.Server(zmdef.ClusterID).(*camera.Zones)
	if !ok {
		t.Fatalf("ZoneManagement is %T", d.Server(zmdef.ClusterID))
	}
	resp, err := z.MatterInvoke(context.Background(), zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: square(3)})
	if r, _ := resp.(zmdef.CreateTwoDCartesianZoneResponse); err != nil || r.ZoneId != 3 {
		t.Fatalf("CreateTwoDCartesianZone: %+v, %v; want zone 3", resp, err)
	}
}

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
		{wrdef.ClusterID, func() (*Device, error) { return New(CameraControllerType(), testOptions("cc")) }, (*camera.Requestor)(nil)},
		{chimedef.ClusterID, func() (*Device, error) {
			return New(ChimeType(), testOptions("chime"), chimedef.Declare{ChimePort: fakeChime{}})
		}, (*camera.Chime)(nil)},
		{avdef.ClusterID, func() (*Device, error) {
			return newWith(CameraType(), testOptions("ptz"), avdef.Declare{Features: avdef.FeatureMechanicalPan, MechanicalPTZ: fakePTZ{}})
		}, (*camera.AvSettings)(nil)},
		{csdef.ClusterID, func() (*Device, error) {
			return newWith(IntercomType(), testOptions("intercom"), csdef.Declare{AudioStreamPort: fakeAudio{}})
		}, (*camera.AvStream)(nil)},
		{zmdef.ClusterID, func() (*Device, error) {
			maxZones, sensMax := uint8(4), uint8(5)
			return newWith(CameraType(), testOptions("zones"), zmdef.Declare{
				Initial: zmdef.Patch{MaxZones: &maxZones, SensitivityMax: &sensMax}, ZonePort: fakeZones{},
			})
		}, (*camera.Zones)(nil)},
		{wpdef.ClusterID, func() (*Device, error) {
			return newWith(CameraType(), testOptions("provider"), wpdef.Declare{ProviderPort: fakeProvider{}})
		}, (*camera.Provider)(nil)},
		{padef.ClusterID, func() (*Device, error) {
			return newWith(CameraType(), testOptions("push"), padef.Declare{PushTransportPort: fakePush{}})
		}, (*camera.PushTransport)(nil)},
	}
}

// boundStreams is a camera's four stream ports for the session-bound
// test.
type boundStreams struct{ fakeAudio }

func (boundStreams) VideoStreamAllocate(context.Context, csdef.VideoStreamStruct) (uint16, error) {
	return 1, nil
}

func (boundStreams) VideoStreamAllocated(context.Context, csdef.VideoStreamStruct, csdef.StreamAllocationAction) {
}
func (boundStreams) VideoStreamModify(context.Context, uint16, *bool, *bool) error { return nil }
func (boundStreams) VideoStreamDeallocate(context.Context, uint16) error           { return nil }
func (boundStreams) SnapshotStreamAllocate(context.Context, csdef.SnapshotStreamStruct) (uint16, error) {
	return 1, nil
}
func (boundStreams) SnapshotStreamModify(context.Context, uint16, *bool, *bool) error { return nil }
func (boundStreams) SnapshotStreamDeallocate(context.Context, uint16) error           { return nil }
func (boundStreams) CaptureSnapshot(context.Context, *uint16, csdef.VideoResolutionStruct) (csdef.CaptureSnapshotResponse, error) {
	return csdef.CaptureSnapshotResponse{}, nil
}

// boundProvider is a ProviderPort that returns the session it is offered.
type boundProvider struct{ fakeProvider }

func (boundProvider) ProvideOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return wpdef.WebRTCSessionStruct{
		Id: a.SessionID, PeerNodeId: a.PeerNodeID, PeerEndpointId: a.OriginatingEndpointID,
		StreamUsage: a.StreamUsage, FabricIndex: a.FabricIndex, VideoStreams: a.VideoStreams,
	}, nil
}

// TestProviderSessionBoundsThroughTheFacade (issue #590, decision 619):
// wpdef.Declare's MaxSessions and MaxSessionsPerFabric reach the kit's
// WebRtcTransportProvider — a device declared with a bound of one refuses
// the second ProvideOffer with RESOURCE_EXHAUSTED — and a negative bound
// is refused when the device is built. Before the fields, the facade
// built every provider unbounded.
func TestProviderSessionBoundsThroughTheFacade(t *testing.T) {
	t.Parallel()
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView}
	res := csdef.VideoResolutionStruct{Width: 640, Height: 480}
	videos := []csdef.VideoStreamStruct{{
		VideoStreamId: 1, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264,
		MinFrameRate: 30, MaxFrameRate: 30, MinResolution: res, MaxResolution: res,
		MinBitRate: 10000, MaxBitRate: 10000, KeyFrameInterval: 4000,
	}}
	streams := func() csdef.Declare {
		return csdef.Declare{
			Initial:         csdef.Patch{SupportedStreamUsages: &usages, StreamUsagePriorities: &usages, AllocatedVideoStreams: &videos},
			VideoStreamPort: boundStreams{}, AudioStreamPort: boundStreams{}, SnapshotStreamPort: boundStreams{}, SnapshotCapturePort: boundStreams{},
		}
	}
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 0x77, nil)
	offer := func(p *camera.Provider) error {
		u, ep, null := wpdef.StreamUsageLiveView, uint16(1), spec.NullOf[uint16]()
		_, err := p.MatterInvoke(ctx, wpdef.CmdProvideOffer, wpdef.ProvideOfferRequest{
			WebRtcSessionId: spec.NullOf[uint16](), Sdp: "v=0", StreamUsage: &u, OriginatingEndpointId: &ep, VideoStreamId: &null,
		})
		return err
	}
	for _, tc := range []struct {
		name string
		decl wpdef.Declare
	}{
		{"MaxSessions", wpdef.Declare{ProviderPort: boundProvider{}, MaxSessions: 1}},
		{"MaxSessionsPerFabric", wpdef.Declare{ProviderPort: boundProvider{}, MaxSessionsPerFabric: 1}},
	} {
		d, err := newWith(CameraType(), testOptions("bounded-"+tc.name), streams(), tc.decl)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		p, ok := d.Server(wpdef.ClusterID).(*camera.Provider)
		if !ok {
			t.Fatalf("%s: WebRtcTransportProvider is %T", tc.name, d.Server(wpdef.ClusterID))
		}
		if err := offer(p); err != nil {
			t.Fatalf("%s: the first ProvideOffer: %v", tc.name, err)
		}
		var sc im.StatusCodeError
		if err := offer(p); !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusResourceExhausted {
			t.Fatalf("%s: the second ProvideOffer = %v, want RESOURCE_EXHAUSTED", tc.name, err)
		}
	}
	for _, decl := range []wpdef.Declare{
		{ProviderPort: boundProvider{}, MaxSessions: -1},
		{ProviderPort: boundProvider{}, MaxSessionsPerFabric: -1},
	} {
		if _, err := newWith(CameraType(), testOptions("negative"), streams(), decl); !errors.Is(err, camera.ErrNegativeMaxSessions) {
			t.Fatalf("%+v: %v, want ErrNegativeMaxSessions", decl, err)
		}
	}
	// Issue #634 (decisions 702, 711): EvictIdle and EvictAcrossFabrics
	// reach the kit — with a bound of one and an EvictIdle of a
	// nanosecond, the second ProvideOffer evicts the first session
	// instead of being refused — and a negative EvictIdle is refused.
	d, err := newWith(CameraType(), testOptions("evicting"), streams(), wpdef.Declare{
		ProviderPort: boundProvider{}, MaxSessions: 1, EvictIdle: time.Nanosecond, EvictAcrossFabrics: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	p := d.Server(wpdef.ClusterID).(*camera.Provider)
	for i := range 2 {
		if err := offer(p); err != nil {
			t.Fatalf("ProvideOffer %d with EvictIdle: %v", i+1, err)
		}
	}
	if ids := p.Sessions(); len(ids) != 1 || ids[0].Id != 1 {
		t.Fatalf("CurrentSessions %+v, want session 1 alone", ids)
	}
	if _, err := newWith(CameraType(), testOptions("negative-evict"), streams(), wpdef.Declare{ProviderPort: boundProvider{}, EvictIdle: -time.Second}); !errors.Is(err, camera.ErrNegativeEvictIdle) {
		t.Fatalf("negative EvictIdle: %v, want ErrNegativeEvictIdle", err)
	}
}
