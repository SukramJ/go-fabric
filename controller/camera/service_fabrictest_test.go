// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	clustercamera "github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/camera"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// The offer of the acceptance: one H.264 video section the host only
// receives on.
const acceptanceOffer = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H264/90000\r\na=recvonly\r\n"

// TestServiceOnTheFabric is the acceptance of issue 410 on fabrictest: a
// controller with the camera service and outgoing TCP, a camera device
// whose cluster/camera.Provider runs on a test port, the camera reached
// over TCP. The sweep ends the session an earlier run left; StartStream
// with an offer allocates or reuses, the camera's PeerNodeID is
// RootNodeID; Answer, ICECandidates and End arrive on Session.Signals over
// the controller's session; Stop ends; a second start reuses; a codec the
// camera lacks is *IncompatibleError{Reason: codec}; a SolicitOffer's
// answer goes over TCP; after a camera restart the next signal reaches
// the service over the camera's own CASE session.
func TestServiceOnTheFabric(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true})
	port := newCameraPort()
	cam := newAcceptanceCamera(t, port)
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{cam}, Configure: func(cfg *node.Config) {
		cfg.Network.TCP = node.TCPConfig{Incoming: true}
	}})
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	cfg.Camera = &controller.CameraConfig{}
	cfg.TCP = controller.TCPConfig{Outgoing: true}
	var reports camera.Reports
	reports.Install(&cfg)
	run := func(fn func(ctx context.Context) error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := site.Run(ctx, fn); err != nil {
			t.Fatal(err)
		}
	}
	eventually := func(what string, cond func() bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := site.Eventually(ctx, what, cond); err != nil {
			t.Fatal(err)
		}
	}
	var c *controller.Controller
	run(func(ctx context.Context) (err error) { c, err = controller.New(ctx, cfg); return err })
	defer run(c.Close)

	// Without outgoing TCP the service refuses; without Reports too.
	if _, err := camera.New(context.Background(), c, camera.Config{}); !errors.Is(err, camera.ErrReportsNotInstalled) {
		t.Fatalf("New without Reports: %v", err)
	}

	var n *client.Node
	run(func(ctx context.Context) (err error) {
		code, err := setup.ManualCode(dev.Discriminator(), dev.Passcode())
		if err != nil {
			return err
		}
		n, err = c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	ep, ok := dev.EndpointOf("camera")
	if !ok {
		t.Fatal("the camera endpoint is not on the device")
	}
	root := c.Fabric().RootNodeID

	// A session an earlier run of the controller left: opened as
	// RootNodeID, held by no requestor.
	usage := wpdef.StreamUsageLiveView
	origin := controller.CameraControllerEndpoint
	run(func(ctx context.Context) error {
		_, err := n.Invoke(ctx, client.CommandPath{Endpoint: ep, Cluster: wpdef.ClusterID, Command: wpdef.CmdProvideOffer},
			wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}, Sdp: acceptanceOffer, StreamUsage: &usage, OriginatingEndpointId: &origin, VideoStreamId: &spec.Nullable[uint16]{Value: 1}},
			client.InvokeOptions{})
		return err
	})
	leftover := port.lastOffer().SessionID
	eventually("the leftover in the controller's image", func() bool { return sessionListed(n, ep, leftover) })

	var (
		mu      sync.Mutex
		endings []camera.SessionEnd
	)
	svc, err := camera.New(context.Background(), c, camera.Config{Reports: &reports, Observer: camera.Observer{
		SessionEnded: func(e camera.SessionEnd) { mu.Lock(); endings = append(endings, e); mu.Unlock() },
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer run(svc.Close)

	// The sweep: the camera's next report ends the leftover with
	// UnknownReason.
	eventually("the sweep's EndSession", func() bool { return port.ended(leftover) == 12 })

	// A start with an offer allocates or reuses, as RootNodeID.
	var first *camera.Session
	run(func(ctx context.Context) (err error) {
		offer := acceptanceOffer
		first, err = svc.StartStream(ctx, camera.StartRequest{Node: n, Endpoint: ep, Usage: csdef.StreamUsageLiveView, Offer: &offer, Owner: "host", Key: "first"})
		return err
	})
	if first.Mode != camera.ModeProvideOffer || len(first.Streams) != 1 || first.Streams[0].Kind != camera.StreamVideo {
		t.Fatalf("first session %+v", first)
	}
	if got := port.lastOffer(); got.PeerNodeID != root || got.OriginatingEndpointID != controller.CameraControllerEndpoint {
		t.Fatalf("the camera stored peer 0x%X endpoint %d, want RootNodeID 0x%X endpoint %d", got.PeerNodeID, got.OriginatingEndpointID, root, controller.CameraControllerEndpoint)
	}
	if !requestorHolds(c, first.ID) {
		t.Fatal("the requestor does not hold the session")
	}
	video := first.Streams[0]

	// The camera answers on the controller's session: the answer is
	// narrowed to the offer's directions.
	provider := providerOf(t, cam)
	run(func(ctx context.Context) error {
		return provider.Answer(ctx, first.ID, "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=sendrecv\r\n")
	})
	if sig := next(t, first.Signals()); sig.Kind != camera.SignalAnswer || !strings.Contains(sig.SDP, "a=sendonly") {
		t.Fatalf("answer %+v", sig)
	}
	run(func(ctx context.Context) error {
		return provider.IceCandidates(ctx, first.ID, []clustercamera.ProviderICECandidate{{Candidate: "candidate:1 1 UDP 1 192.0.2.1 9 typ host", SdpMid: spec.Nullable[string]{Value: "0"}}})
	})
	if sig := next(t, first.Signals()); sig.Kind != camera.SignalICECandidates || len(sig.Candidates) != 1 {
		t.Fatalf("candidates %+v", sig)
	}
	run(func(ctx context.Context) error {
		return first.ProvideICECandidates(ctx, []wpdef.ICECandidateStruct{{Candidate: "candidate:2 1 UDP 1 192.0.2.2 9 typ host", SdpMid: spec.Nullable[string]{Value: "0"}}})
	})
	if port.candidates(first.ID) != 1 {
		t.Fatal("the host's candidates did not reach the camera")
	}

	// A second start reuses the first's video stream.
	var second *camera.Session
	run(func(ctx context.Context) (err error) {
		offer := acceptanceOffer
		second, err = svc.StartStream(ctx, camera.StartRequest{Node: n, Endpoint: ep, Usage: csdef.StreamUsageLiveView, Offer: &offer, Owner: "host"})
		return err
	})
	if v := second.Streams[0]; v.ID != video.ID || v.Provenance == camera.ProvenanceAllocated {
		t.Fatalf("second video %+v, first %+v", v, video)
	}

	// Stop ends the first: EndSession with UserHangup, the requestor
	// drops it, its Signals close.
	run(func(ctx context.Context) error {
		ended, err := first.Stop(ctx)
		if err == nil && !ended {
			err = errors.New("not ended")
		}
		return err
	})
	if port.ended(first.ID) != 2 || requestorHolds(c, first.ID) {
		t.Fatal("Stop did not end the session on both sides")
	}
	if _, open := <-first.Signals(); open {
		t.Fatal("the stopped session's Signals stay open")
	}

	// The camera ends the second: End arrives, then Signals close and the
	// observer hears the peer's end.
	run(func(ctx context.Context) error { return provider.End(ctx, second.ID, 3) })
	if sig := next(t, second.Signals()); sig.Kind != camera.SignalEnd || sig.Reason != 3 {
		t.Fatalf("end %+v", sig)
	}
	if _, open := <-second.Signals(); open {
		t.Fatal("the ended session's Signals stay open")
	}
	mu.Lock()
	heard := len(endings) > 0 && endings[len(endings)-1].Cause == camera.EndPeer && endings[len(endings)-1].SessionID == second.ID
	mu.Unlock()
	if !heard {
		t.Fatalf("endings %+v", endings)
	}

	// A codec the camera lacks.
	run(func(ctx context.Context) error {
		offer := acceptanceOffer
		_, err := svc.StartStream(ctx, camera.StartRequest{Node: n, Endpoint: ep, Usage: csdef.StreamUsageLiveView, Offer: &offer, Video: &camera.VideoBounds{Codecs: []string{"AV1"}}})
		var inc *camera.IncompatibleError
		if !errors.As(err, &inc) || inc.Reason != camera.ReasonCodec {
			return errors.New("want IncompatibleError codec, got " + errString(err))
		}
		return nil
	})

	// SolicitOffer: the camera offers, the host's answer goes over TCP.
	var solicited *camera.Session
	run(func(ctx context.Context) (err error) {
		solicited, err = svc.StartStream(ctx, camera.StartRequest{Node: n, Endpoint: ep, Usage: csdef.StreamUsageLiveView})
		return err
	})
	if solicited.Mode != camera.ModeSolicitOffer {
		t.Fatalf("mode %s", solicited.Mode)
	}
	run(func(ctx context.Context) error { return provider.Offer(ctx, solicited.ID, "v=0 camera offer") })
	if sig := next(t, solicited.Signals()); sig.Kind != camera.SignalOffer {
		t.Fatalf("offer %+v", sig)
	}
	run(func(ctx context.Context) error { return solicited.ProvideAnswer(ctx, "v=0 host answer") })
	if port.answer(solicited.ID) != "v=0 host answer" {
		t.Fatal("the host's answer did not reach the camera")
	}

	// The rest of the facade over the same camera: capabilities, a
	// snapshot, the stops by key, by id, by owner and by a host's own
	// EndSession, and a stream release.
	run(func(ctx context.Context) error {
		caps, err := svc.Capabilities(ctx, n, ep)
		if err != nil {
			return err
		}
		if len(caps.Video.Codecs) != 1 || caps.Video.Codecs[0] != csdef.VideoCodecH264 || len(caps.Allocated.Video) == 0 {
			return errors.New("capabilities")
		}
		snap, err := svc.Snapshot(ctx, camera.SnapshotRequest{Node: n, Endpoint: ep})
		if err != nil {
			return err
		}
		if len(snap.Data) == 0 || snap.Provenance != camera.ProvenanceAllocated {
			return errors.New("snapshot")
		}
		offer := acceptanceOffer
		keyed, err := svc.StartStream(ctx, camera.StartRequest{Node: n, Endpoint: ep, Usage: csdef.StreamUsageLiveView, Offer: &offer, Owner: "host", Key: "keyed"})
		if err != nil {
			return err
		}
		if ended, err := svc.StopByKey(ctx, n, ep, "host", "keyed"); err != nil || !ended || port.ended(keyed.ID) != 2 {
			return errors.New("StopByKey")
		}
		byID, err := svc.StartStream(ctx, camera.StartRequest{Node: n, Endpoint: ep, Usage: csdef.StreamUsageLiveView, Offer: &offer})
		if err != nil {
			return err
		}
		if ended, err := svc.StopSession(ctx, n, ep, byID.ID, "other"); err != nil || !ended {
			return errors.New("StopSession")
		}
		owned, err := svc.StartStream(ctx, camera.StartRequest{Node: n, Endpoint: ep, Usage: csdef.StreamUsageLiveView, Offer: &offer, Owner: "leaving"})
		if err != nil {
			return err
		}
		svc.Release(ctx, "leaving")
		if port.ended(owned.ID) != 2 {
			return errors.New("Release")
		}
		raw, err := svc.StartStream(ctx, camera.StartRequest{Node: n, Endpoint: ep, Usage: csdef.StreamUsageLiveView, Offer: &offer})
		if err != nil {
			return err
		}
		p, err := svc.Provider(n, ep)
		if err != nil {
			return err
		}
		if err := p.EndSession(ctx, wpdef.EndSessionRequest{WebRtcSessionId: raw.ID, Reason: 4}, "host"); err != nil || port.ended(raw.ID) != 4 {
			return errors.New("Provider.EndSession")
		}
		return svc.ReleaseStream(ctx, n, ep, camera.StreamSnapshot, snap.StreamID)
	})

	// After a camera restart — the node disabled so the registry does not
	// reopen a session — the camera's next signal opens CASE to RootNodeID
	// and reaches the session.
	run(n.Disable)
	run(dev.Restart)
	run(func(ctx context.Context) error {
		return provider.IceCandidates(ctx, solicited.ID, []clustercamera.ProviderICECandidate{{Candidate: "candidate:3 1 UDP 1 192.0.2.3 9 typ host", SdpMid: spec.Nullable[string]{Value: "0"}}})
	})
	if sig := next(t, solicited.Signals()); sig.Kind != camera.SignalICECandidates {
		t.Fatalf("after the restart %+v", sig)
	}
	inbound := false
	for _, p := range c.Peers().Peers() {
		for _, s := range p.Sessions() {
			inbound = inbound || !c.Peers().Initiated(s)
		}
	}
	if !inbound {
		t.Fatal("the camera's signal did not come over a session the camera opened")
	}
}

// TestServiceRefusesWithoutTCP: the SDP needs TCP, so a controller
// without outgoing TCP gets no service.
func TestServiceRefusesWithoutTCP(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	cfg.Camera = &controller.CameraConfig{}
	var reports camera.Reports
	reports.Install(&cfg)
	ctx := context.Background()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) { c, err = controller.New(ctx, cfg); return err }); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = site.Run(ctx, c.Close) }()
	if _, err := camera.New(ctx, c, camera.Config{Reports: &reports}); !errors.Is(err, camera.ErrTCPRequired) {
		t.Fatalf("New: %v", err)
	}
}

// TestServiceRefusesWithoutRequestor: a controller that serves no camera
// requestor (no Config.Camera) gets no service, typed.
func TestServiceRefusesWithoutRequestor(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	cfg.TCP = controller.TCPConfig{Outgoing: true}
	var reports camera.Reports
	reports.Install(&cfg)
	ctx := context.Background()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) { c, err = controller.New(ctx, cfg); return err }); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = site.Run(ctx, c.Close) }()
	if _, err := camera.New(ctx, c, camera.Config{Reports: &reports}); !errors.Is(err, camera.ErrNoRequestor) {
		t.Fatalf("New: %v", err)
	}
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func next(t *testing.T, signals <-chan camera.Signal) camera.Signal {
	t.Helper()
	select {
	case sig, ok := <-signals:
		if !ok {
			t.Fatal("Signals closed")
		}
		return sig
	case <-time.After(30 * time.Second):
		t.Fatal("no signal")
		return camera.Signal{}
	}
}

func providerOf(t *testing.T, cam *device.Camera) *clustercamera.Provider {
	t.Helper()
	p, ok := cam.WebRtcTransportProvider().Underlying().(*clustercamera.Provider)
	if !ok {
		t.Fatal("the device serves no camera.Provider")
	}
	return p
}

func requestorHolds(c *controller.Controller, id uint16) bool {
	for _, s := range c.CameraRequestor().Sessions() {
		if s.Id == id {
			return true
		}
	}
	return false
}

func sessionListed(n *client.Node, ep, id uint16) bool {
	e := n.Endpoint(ep)
	if e == nil {
		return false
	}
	st, _ := wpdef.StateFrom(e.Cluster(wpdef.ClusterID))
	for _, s := range st.CurrentSessions {
		if s.Id == id {
			return true
		}
	}
	return false
}

// newAcceptanceCamera is a camera with H.264 up to 1920x1080 at 30 fps,
// one allocated live-view stream, and the Provider on port.
func newAcceptanceCamera(t *testing.T, port *cameraPort) *device.Camera {
	t.Helper()
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView, csdef.StreamUsageRecording}
	off := false
	streams := []csdef.VideoStreamStruct{{
		VideoStreamId: 1, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264,
		MinFrameRate: 15, MaxFrameRate: 30,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720},
		MinBitRate: 100_000, MaxBitRate: 2_000_000, KeyFrameInterval: 4000, WatermarkEnabled: &off, OsdEnabled: &off,
	}}
	points := []csdef.RateDistortionTradeOffPointsStruct{{Codec: csdef.VideoCodecH264, Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MinBitRate: 100_000}}
	sensor := csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30}
	minView := csdef.VideoResolutionStruct{Width: 640, Height: 480}
	snapshots := []csdef.SnapshotCapabilitiesStruct{{Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxFrameRate: 1, ImageCodec: csdef.ImageCodecJpeg}}
	encoders := uint8(4)
	pixelRate := uint32(1920 * 1080 * 30 * 4)
	bandwidth := uint32(20_000_000)
	cam, err := device.NewCamera(device.Options{StableKey: device.StringKey("camera"), Name: "Camera"},
		csdef.Declare{
			Features: csdef.FeatureVideo | csdef.FeatureAudio | csdef.FeatureSnapshot,
			Initial: csdef.Patch{
				SupportedStreamUsages: &usages, StreamUsagePriorities: &usages, AllocatedVideoStreams: &streams,
				RateDistortionTradeOffPoints: &points, VideoSensorParams: &sensor, MinViewportResolution: &minView, SnapshotCapabilities: &snapshots,
				MaxConcurrentEncoders: &encoders, MaxEncodedPixelRate: &pixelRate, MaxNetworkBandwidth: &bandwidth,
			},
			VideoStreamPort: port, AudioStreamPort: port, SnapshotStreamPort: port, SnapshotCapturePort: port,
		},
		wpdef.Declare{ProviderPort: port},
	)
	if err != nil {
		t.Fatal(err)
	}
	return cam
}

// cameraPort is the camera's WebRTC stack and encoders: every offer
// becomes a session, every allocate a new stream id.
type cameraPort struct {
	mu      sync.Mutex
	offers  []wpdef.OfferArgs
	nextID  uint16
	ends    map[uint16]uint8
	cands   map[uint16]int
	answers map[uint16]string
	nextVid uint16
}

func newCameraPort() *cameraPort {
	return &cameraPort{nextID: 1, nextVid: 10, ends: map[uint16]uint8{}, cands: map[uint16]int{}, answers: map[uint16]string{}}
}

func (p *cameraPort) lastOffer() wpdef.OfferArgs {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.offers[len(p.offers)-1]
}

func (p *cameraPort) ended(id uint16) uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.ends[id]; ok {
		return r
	}
	return 0xFF
}

func (p *cameraPort) candidates(id uint16) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cands[id]
}

func (p *cameraPort) answer(id uint16) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.answers[id]
}

func (p *cameraPort) session(args wpdef.OfferArgs) wpdef.WebRTCSessionStruct {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.offers = append(p.offers, args)
	return wpdef.WebRTCSessionStruct{
		Id: args.SessionID, PeerNodeId: args.PeerNodeID, PeerEndpointId: args.OriginatingEndpointID,
		StreamUsage: args.StreamUsage, VideoStreams: args.VideoStreams, AudioStreams: args.AudioStreams, FabricIndex: args.FabricIndex,
	}
}

func (p *cameraPort) SolicitOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return p.session(args), false, nil
}

func (p *cameraPort) ProvideOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return p.session(args), nil
}

func (p *cameraPort) ProvideAnswer(_ context.Context, id uint16, sdp string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answers[id] = sdp
	return nil
}

func (p *cameraPort) ProvideIceCandidates(_ context.Context, id uint16, c []wpdef.ICECandidateStruct) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cands[id] += len(c)
	return nil
}

func (p *cameraPort) EndSession(_ context.Context, id uint16, reason uint8) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ends[id] = reason
	return nil
}

func (p *cameraPort) VideoStreamAllocate(_ context.Context, args csdef.VideoStreamStruct) (uint16, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if args.VideoStreamId != 0 {
		return args.VideoStreamId, nil
	}
	p.nextVid++
	return p.nextVid, nil
}

func (p *cameraPort) VideoStreamAllocated(context.Context, csdef.VideoStreamStruct, csdef.StreamAllocationAction) {
}

func (p *cameraPort) VideoStreamModify(context.Context, uint16, *bool, *bool) error { return nil }

func (p *cameraPort) VideoStreamDeallocate(context.Context, uint16) error { return nil }

func (p *cameraPort) AudioStreamAllocate(context.Context, csdef.AudioStreamStruct) (uint16, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextVid++
	return p.nextVid, nil
}

func (p *cameraPort) AudioStreamDeallocate(context.Context, uint16) error { return nil }

func (p *cameraPort) SnapshotStreamAllocate(context.Context, csdef.SnapshotStreamStruct) (uint16, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextVid++
	return p.nextVid, nil
}

func (p *cameraPort) SnapshotStreamModify(context.Context, uint16, *bool, *bool) error { return nil }

func (p *cameraPort) SnapshotStreamDeallocate(context.Context, uint16) error { return nil }

func (p *cameraPort) CaptureSnapshot(_ context.Context, _ *uint16, r csdef.VideoResolutionStruct) (csdef.CaptureSnapshotResponse, error) {
	return csdef.CaptureSnapshotResponse{Data: []byte{0xFF, 0xD8}, ImageCodec: csdef.ImageCodecJpeg, Resolution: r}, nil
}
