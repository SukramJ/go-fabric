// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cameratest_test

import (
	"context"
	"slices"
	"testing"
	"time"

	clustercamera "thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/controller/camera/cameratest"
	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/secure/setup"
)

// TestPortReOfferKeepsTheSessionsStreams (review II, R2-P6-test-2205,
// carried cameratest Port.session): a re-offer in the revision-2 form —
// VideoStreams, no deprecated id, which a re-offer's OfferArgs do not
// carry — answers with the session's id and the session keeps its
// streams; the deprecated form still works. The port starts a re-offer
// from the provider's CurrentSessions entry, as the reference daemon's
// port does (77bbff6b). Before, the port returned a session naming no
// stream and the provider answered FAILURE.
func TestPortReOfferKeepsTheSessionsStreams(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true})
	cam, err := cameratest.New(cameratest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{cam.Device()}, Configure: func(cfg *node.Config) {
		cfg.Network.TCP = node.TCPConfig{Incoming: true}
	}})
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	cfg.Camera = &controller.CameraConfig{}
	cfg.TCP = controller.TCPConfig{Outgoing: true}
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
	t.Cleanup(func() { run(c.Close) })
	var n *client.Node
	run(func(ctx context.Context) error {
		code, err := setup.ManualCode(dev.Discriminator(), dev.Passcode())
		if err != nil {
			return err
		}
		n, err = c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	ep, _ := dev.EndpointOf("camera")
	usage := wpdef.StreamUsageLiveView
	origin := controller.CameraControllerEndpoint
	sdp := "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H264/90000\r\na=recvonly\r\n"
	// offer sends a ProvideOffer and returns the id of the session the
	// port answered.
	offer := func(req wpdef.ProvideOfferRequest) (uint16, error) {
		req.Sdp, req.StreamUsage, req.OriginatingEndpointId = sdp, &usage, &origin
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := site.Run(ctx, func(ctx context.Context) error {
			_, err := n.Invoke(ctx, client.CommandPath{Endpoint: ep, Cluster: wpdef.ClusterID, Command: wpdef.CmdProvideOffer}, req, client.InvokeOptions{})
			return err
		})
		return cam.Port().LastOffer().SessionID, err
	}
	streams := []uint16{cameratest.AllocatedVideoStream}
	id, err := offer(wpdef.ProvideOfferRequest{WebRtcSessionId: spec.NullOf[uint16](), VideoStreams: &streams})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	provider := cam.Device().WebRtcTransportProvider().Underlying().(*clustercamera.Provider)
	sessionOf := func() wpdef.WebRTCSessionStruct {
		i := slices.IndexFunc(provider.Sessions(), func(s wpdef.WebRTCSessionStruct) bool { return s.Id == id })
		if i < 0 {
			t.Fatalf("session %d is not in CurrentSessions", id)
		}
		return provider.Sessions()[i]
	}
	if re, err := offer(wpdef.ProvideOfferRequest{WebRtcSessionId: spec.ValueOf(id), VideoStreams: &streams}); err != nil || re != id {
		t.Fatalf("re-offer of %d with VideoStreams = %d, %v; want its id", id, re, err)
	}
	if s := sessionOf(); s.VideoStreams == nil || !slices.Equal(*s.VideoStreams, streams) {
		t.Errorf("session %d after the re-offer names video streams %v, want %v", id, s.VideoStreams, streams)
	}
	videoID := spec.ValueOf(cameratest.AllocatedVideoStream)
	if re, err := offer(wpdef.ProvideOfferRequest{WebRtcSessionId: spec.ValueOf(id), VideoStreamId: &videoID}); err != nil || re != id {
		t.Fatalf("re-offer of %d with VideoStreamId = %d, %v; want its id", id, re, err)
	}
	if got := len(cam.Port().Offers()); got != 3 {
		t.Errorf("the port answered %d offers, want 3", got)
	}
}
