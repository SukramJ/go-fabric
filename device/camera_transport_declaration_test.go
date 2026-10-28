// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
)

type transportPorts struct{}

func (transportPorts) SolicitOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return wpdef.WebRTCSessionStruct{Id: a.SessionID, FabricIndex: a.FabricIndex, PeerNodeId: a.PeerNodeID}, false, nil
}

func (transportPorts) ProvideOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return wpdef.WebRTCSessionStruct{Id: a.SessionID}, nil
}
func (transportPorts) ProvideAnswer(context.Context, uint16, string) error { return nil }
func (transportPorts) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}
func (transportPorts) EndSession(context.Context, uint16, uint8) error { return nil }

func (transportPorts) AllocatePushTransport(context.Context, uint16, uint8, padef.TransportOptionsStruct, padef.TLSCredentials) error {
	return nil
}
func (transportPorts) DeallocatePushTransport(context.Context, uint16) error { return nil }
func (transportPorts) ModifyPushTransport(context.Context, uint16, padef.TransportOptionsStruct) error {
	return nil
}

func (transportPorts) SetTransportStatus(context.Context, []uint16, padef.TransportStatusEnum) error {
	return nil
}

func (transportPorts) ManuallyTriggerTransport(context.Context, uint16, padef.TriggerActivationReasonEnum, *padef.TransportMotionTriggerTimeControlStruct) error {
	return nil
}
func (transportPorts) TransportBusy(uint16) bool { return false }
func (transportPorts) ValidateBandwidthLimit(context.Context, padef.StreamUsageEnum, *uint16, *uint16) error {
	return nil
}
func (transportPorts) CMAFSessionNumber(uint16) (uint64, bool) { return 0, false }

func (transportPorts) CreateTwoDCartesianZone(context.Context, zmdef.TwoDCartesianZoneStruct) (uint16, error) {
	return 1, nil
}

func (transportPorts) UpdateTwoDCartesianZone(context.Context, uint16, zmdef.TwoDCartesianZoneStruct) error {
	return nil
}

func (transportPorts) RemoveZone(context.Context, uint16) error { return nil }

func (transportPorts) CreateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error {
	return nil
}

func (transportPorts) UpdateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error {
	return nil
}

func (transportPorts) RemoveTrigger(context.Context, uint16) error { return nil }

func peerContext() context.Context {
	return im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 0x77, nil)
}

func codeOf(err error) im.StatusCode {
	if sc, ok := err.(im.StatusCodeError); ok {
		return sc.MatterStatusCode()
	}
	if err == nil {
		return im.StatusSuccess
	}
	return im.StatusFailure
}

// TestCameraServesTheKitTransports: a Camera declared with its transport
// ports is the kit's WebRtcTransportProvider and PushAvStreamTransport
// servers, wired to the device's CameraAvStreamManagement and
// ZoneManagement servers; without the ports both keep the generated
// server.
func TestCameraServesTheKitTransports(t *testing.T) {
	t.Parallel()
	p := streamPorts{}
	tp := transportPorts{}
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView}
	maxZones, sensMax := uint8(4), uint8(5)
	d, err := device.NewCamera(device.Options{StableKey: device.StringKey("cam-transports"), Name: "cam"},
		csdef.Declare{
			Initial:         csdef.Patch{SupportedStreamUsages: &usages},
			VideoStreamPort: p, AudioStreamPort: p, SnapshotStreamPort: p, SnapshotCapturePort: p,
		},
		zmdef.Declare{Initial: zmdef.Patch{MaxZones: &maxZones, SensitivityMax: &sensMax}, ZonePort: tp},
		wpdef.Declare{ProviderPort: tp},
		padef.Declare{PushTransportPort: tp},
	)
	if err != nil {
		t.Fatal(err)
	}
	prov, ok := d.Server(wpdef.ClusterID).(*camera.Provider)
	if !ok {
		t.Fatalf("WebRtcTransportProvider is %T", d.Server(wpdef.ClusterID))
	}
	// No video stream is allocated on the device's AvStream: a null
	// VideoStreamID is INVALID_IN_STATE (WebRTCTransportProviderCluster.cpp:517).
	null := spec.NullOf[uint16]()
	_, err = prov.MatterInvoke(peerContext(), wpdef.CmdSolicitOffer, wpdef.SolicitOfferRequest{StreamUsage: wpdef.StreamUsageLiveView, VideoStreamId: &null})
	if got := codeOf(err); got != im.StatusInvalidInState {
		t.Fatalf("SolicitOffer without streams: %s", got)
	}
	if _, ok := d.Server(padef.ClusterID).(*camera.PushTransport); !ok {
		t.Fatalf("PushAvStreamTransport is %T", d.Server(padef.ClusterID))
	}

	bare, err := device.NewCamera(device.Options{StableKey: device.StringKey("cam-bare"), Name: "cam"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.Server(wpdef.ClusterID).(*camera.Provider); ok {
		t.Fatal("a provider without its port")
	}
}
