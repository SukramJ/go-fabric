// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// TestControllerServesTheCameraRequestor is the one-identity flow of
// issue 410: the controller invokes ProvideOffer on a camera's
// WebRtcTransportProvider, and the camera's Answer comes back to the
// controller's own WebRtcTransportRequestor over the session the
// controller opened — RootNodeID the camera's PeerNodeID, as matter.js's
// one-node controller receives it (matterjs-server ControllerNode.ts:147,
// ControllerCommandHandler.ts:768-771 at 0bd06f2) and as chip's camera-app
// sends it on the newest live session with the peer
// (OperationalSessionSetup.cpp:75-96 at 6170af84).
func TestControllerServesTheCameraRequestor(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true, Logger: testLogger(t)})
	port := &testCameraPort{offered: make(chan wpdef.OfferArgs, 1)}
	cam := newTestCamera(t, port)
	// The SDP travels over TCP (a large-message command requires it,
	// ClientInteraction.ts:1235), as matterjs-server runs its controller
	// with TCP (ControllerNode.ts:113-114 at 0bd06f2): the camera accepts
	// TCP, the controller opens it.
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{cam}, Configure: func(cfg *node.Config) {
		cfg.Network.TCP = node.TCPConfig{Incoming: true}
	}})
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	cfg.Camera = &controller.CameraConfig{}
	cfg.TCP = controller.TCPConfig{Outgoing: true}
	cfg.Advertiser = mdns.NewNoop()
	run := func(fn func(ctx context.Context) error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := site.Run(ctx, fn); err != nil {
			t.Fatal(err)
		}
	}
	var c *controller.Controller
	run(func(ctx context.Context) (err error) { c, err = controller.New(ctx, cfg); return err })
	defer run(c.Close)
	if c.CameraRequestor() == nil {
		t.Fatal("no requestor served")
	}

	signals := make(chan controller.CameraSignal, 4)
	defer c.OnCameraSignal(func(s controller.CameraSignal) { signals <- s })()

	var n *client.Node
	run(func(ctx context.Context) (err error) {
		n, err = c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	ep, ok := dev.EndpointOf("camera")
	if !ok {
		t.Fatal("the camera endpoint is not on the device")
	}
	var cameraNodeID uint64
	run(func(ctx context.Context) error {
		infos, err := dev.Node().FabricInfos(ctx)
		if err != nil || len(infos) != 1 {
			return errors.New("the camera holds no fabric")
		}
		cameraNodeID = infos[0].NodeID
		return nil
	})

	// ProvideOffer: the controller is the invoker, so the camera stores
	// RootNodeID as the session's peer and the controller's endpoint as
	// its PeerEndpointId.
	usage := wpdef.StreamUsageLiveView
	origin := controller.CameraControllerEndpoint
	var session uint16
	run(func(ctx context.Context) error {
		res, err := n.Invoke(ctx, client.CommandPath{Endpoint: ep, Cluster: wpdef.ClusterID, Command: wpdef.CmdProvideOffer},
			wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}, Sdp: "v=0 offer", StreamUsage: &usage, OriginatingEndpointId: &origin, VideoStreamId: &spec.Nullable[uint16]{Null: true}},
			client.InvokeOptions{})
		if err != nil {
			return err
		}
		var resp wpdef.ProvideOfferResponse
		if err := resp.DecodeTLV(res.Fields); err != nil {
			return err
		}
		session = resp.WebRtcSessionId
		return nil
	})
	offered := <-port.offered
	if offered.PeerNodeID != c.Fabric().RootNodeID {
		t.Fatalf("the camera stored peer 0x%X, want RootNodeID 0x%X", offered.PeerNodeID, c.Fabric().RootNodeID)
	}
	if offered.OriginatingEndpointID != controller.CameraControllerEndpoint {
		t.Fatalf("originating endpoint %d", offered.OriginatingEndpointID)
	}

	// The camera service (wave C) keeps the requestor's table; here the
	// test registers the session the camera will answer on.
	if err := c.CameraRequestor().UpsertSession(camera.WebRTCSession{
		Id: session, PeerNodeId: cameraNodeID, PeerEndpointId: ep, StreamUsage: wrdef.StreamUsageEnum(usage), FabricIndex: c.Fabric().FabricIndex,
	}); err != nil {
		t.Fatal(err)
	}

	// The camera answers: its provider invokes Answer on the controller's
	// requestor over the existing session.
	provider, ok := cam.WebRtcTransportProvider().Underlying().(*camera.Provider)
	if !ok {
		t.Fatal("the device serves no camera.Provider")
	}
	run(func(ctx context.Context) error { return provider.Answer(ctx, session, "v=0 answer") })
	select {
	case sig := <-signals:
		if sig.Kind != controller.CameraSignalAnswer || sig.SDP != "v=0 answer" || sig.Session.Id != session {
			t.Fatalf("signal %+v", sig)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the camera's Answer did not reach the controller's requestor")
	}

	// End, the same way.
	run(func(ctx context.Context) error { return provider.End(ctx, session, 1) })
	select {
	case sig := <-signals:
		if sig.Kind != controller.CameraSignalEnd || sig.Reason != 1 {
			t.Fatalf("signal %+v", sig)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the camera's End did not reach the controller's requestor")
	}
}

// newTestCamera is a camera device with one allocated live-view video
// stream, so that a ProvideOffer without stream ids selects it.
func newTestCamera(t *testing.T, port *testCameraPort) *device.Camera {
	t.Helper()
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView}
	off := false
	streams := []csdef.VideoStreamStruct{{
		VideoStreamId: 1, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264,
		MinFrameRate: 15, MaxFrameRate: 30,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720},
		MinBitRate: 100_000, MaxBitRate: 2_000_000, KeyFrameInterval: 4000, WatermarkEnabled: &off, OsdEnabled: &off,
	}}
	cam, err := device.NewCamera(device.Options{StableKey: device.StringKey("camera"), Name: "Camera"},
		csdef.Declare{
			Features:        csdef.FeatureVideo | csdef.FeatureAudio | csdef.FeatureSnapshot,
			Initial:         csdef.Patch{SupportedStreamUsages: &usages, StreamUsagePriorities: &usages, AllocatedVideoStreams: &streams},
			VideoStreamPort: port, AudioStreamPort: port, SnapshotStreamPort: port, SnapshotCapturePort: port,
		},
		wpdef.Declare{ProviderPort: port},
	)
	if err != nil {
		t.Fatal(err)
	}
	return cam
}

// testCameraPort is the camera's WebRTC stack: every offer becomes a
// session; the test answers it through the provider.
type testCameraPort struct {
	offered chan wpdef.OfferArgs
}

func (p *testCameraPort) SolicitOffer(context.Context, wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return wpdef.WebRTCSessionStruct{}, false, errors.New("not offered")
}

func (p *testCameraPort) ProvideOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	p.offered <- args
	return wpdef.WebRTCSessionStruct{
		Id: args.SessionID, PeerNodeId: args.PeerNodeID, PeerEndpointId: args.OriginatingEndpointID,
		StreamUsage: args.StreamUsage, VideoStreams: args.VideoStreams, AudioStreams: args.AudioStreams, FabricIndex: args.FabricIndex,
	}, nil
}

func (p *testCameraPort) ProvideAnswer(context.Context, uint16, string) error { return nil }

func (p *testCameraPort) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}

func (p *testCameraPort) EndSession(context.Context, uint16, uint8) error { return nil }

func (p *testCameraPort) VideoStreamAllocate(_ context.Context, args csdef.VideoStreamStruct) (uint16, error) {
	return args.VideoStreamId, nil
}

func (p *testCameraPort) VideoStreamAllocated(context.Context, csdef.VideoStreamStruct, csdef.StreamAllocationAction) {
}

func (p *testCameraPort) VideoStreamModify(context.Context, uint16, *bool, *bool) error { return nil }

func (p *testCameraPort) VideoStreamDeallocate(context.Context, uint16) error { return nil }

func (p *testCameraPort) AudioStreamAllocate(_ context.Context, args csdef.AudioStreamStruct) (uint16, error) {
	return args.AudioStreamId, nil
}

func (p *testCameraPort) AudioStreamDeallocate(context.Context, uint16) error { return nil }

func (p *testCameraPort) SnapshotStreamAllocate(_ context.Context, args csdef.SnapshotStreamStruct) (uint16, error) {
	return args.SnapshotStreamId, nil
}

func (p *testCameraPort) SnapshotStreamModify(context.Context, uint16, *bool, *bool) error {
	return nil
}

func (p *testCameraPort) SnapshotStreamDeallocate(context.Context, uint16) error { return nil }

func (p *testCameraPort) CaptureSnapshot(context.Context, *uint16, csdef.VideoResolutionStruct) (csdef.CaptureSnapshotResponse, error) {
	return csdef.CaptureSnapshotResponse{}, errors.New("no snapshot")
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	if os.Getenv("GOFABRIC_TEST_LOG") == "" {
		return nil
	}
	f, err := os.Create(os.Getenv("GOFABRIC_TEST_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
