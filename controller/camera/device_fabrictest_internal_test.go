// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	clustercamera "github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// The cases of matterjs-server's
// packages/ws-controller/test/WebRtcProviderDeviceTest.ts:66-392 at
// 0bd06f2 that run against a device, ported onto fabrictest: a camera
// device with cluster/camera's Provider, the controller over TCP, the
// service attached with its Reports. The clock cases run against the
// session layer in provider_sessions_parity_matterjsserver_test.go.

// devPort is the simulated camera's WebRTC stack and encoders; every
// offer becomes session 11 and the device log records the ends.
type devPort struct {
	mu  sync.Mutex
	log []string
}

func (p *devPort) logged() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.log)
}

func (p *devPort) session(args wpdef.OfferArgs) wpdef.WebRTCSessionStruct {
	return wpdef.WebRTCSessionStruct{Id: args.SessionID, PeerNodeId: args.PeerNodeID, PeerEndpointId: args.OriginatingEndpointID, StreamUsage: args.StreamUsage, VideoStreams: args.VideoStreams, AudioStreams: args.AudioStreams, FabricIndex: args.FabricIndex}
}

func (p *devPort) SolicitOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return p.session(a), false, nil
}

func (p *devPort) ProvideOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return p.session(a), nil
}
func (p *devPort) ProvideAnswer(context.Context, uint16, string) error { return nil }
func (p *devPort) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}

func (p *devPort) EndSession(_ context.Context, id uint16, _ uint8) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, fmt.Sprintf("endSession:%d", id))
	return nil
}

func (p *devPort) VideoStreamAllocate(context.Context, csdef.VideoStreamStruct) (uint16, error) {
	return 20, nil
}

func (p *devPort) VideoStreamAllocated(context.Context, csdef.VideoStreamStruct, csdef.StreamAllocationAction) {
}
func (p *devPort) VideoStreamModify(context.Context, uint16, *bool, *bool) error { return nil }
func (p *devPort) VideoStreamDeallocate(context.Context, uint16) error           { return nil }
func (p *devPort) AudioStreamAllocate(context.Context, csdef.AudioStreamStruct) (uint16, error) {
	return 21, nil
}
func (p *devPort) AudioStreamDeallocate(context.Context, uint16) error { return nil }
func (p *devPort) SnapshotStreamAllocate(context.Context, csdef.SnapshotStreamStruct) (uint16, error) {
	return 22, nil
}
func (p *devPort) SnapshotStreamModify(context.Context, uint16, *bool, *bool) error { return nil }
func (p *devPort) SnapshotStreamDeallocate(context.Context, uint16) error           { return nil }
func (p *devPort) CaptureSnapshot(context.Context, *uint16, csdef.VideoResolutionStruct) (csdef.CaptureSnapshotResponse, error) {
	return csdef.CaptureSnapshotResponse{}, errors.New("no snapshot")
}

func devCamera(t *testing.T, port *devPort, key string) *device.Camera {
	t.Helper()
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView}
	streams := []csdef.VideoStreamStruct{{
		VideoStreamId: 1, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264, MinFrameRate: 15, MaxFrameRate: 30,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720},
		MinBitRate: 100_000, MaxBitRate: 2_000_000, KeyFrameInterval: 4000,
	}}
	cam, err := device.NewCamera(device.Options{StableKey: device.StringKey(key), Name: "Camera"},
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

// devSite is a commissioned camera, the controller and the service.
type devSite struct {
	t       *testing.T
	site    *fabrictest.Site
	dev     *fabrictest.Device
	port    *devPort
	cam     *device.Camera
	c       *controller.Controller
	n       *client.Node
	ep      uint16
	reports func() []string
	signals func() []Signal
	svc     *Service
	cfg     controller.Config
	rep     *Reports
}

func (d *devSite) run(fn func(ctx context.Context) error) {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := d.site.Run(ctx, fn); err != nil {
		d.t.Fatal(err)
	}
}

func (d *devSite) eventually(what string, cond func() bool) {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := d.site.Eventually(ctx, what, cond); err != nil {
		d.t.Fatal(err)
	}
}

func (d *devSite) provider() *clustercamera.Provider {
	p, ok := d.cam.WebRtcTransportProvider().Underlying().(*clustercamera.Provider)
	if !ok {
		d.t.Fatal("no camera.Provider")
	}
	return p
}

// startService attaches a service that records its reports and the
// signals no session holds.
func (d *devSite) startService() {
	d.t.Helper()
	var mu sync.Mutex
	var reports []string
	var signals []Signal
	svc, err := New(context.Background(), d.c, Config{Reports: d.rep, Observer: Observer{Signal: func(s Signal) { mu.Lock(); signals = append(signals, s); mu.Unlock() }}})
	if err != nil {
		d.t.Fatal(err)
	}
	reconcile := svc.reported
	svc.reported = func(node uint64, ep uint16, present map[uint16]bool, restarted bool) {
		ids := make([]int, 0, len(present))
		for id := range present {
			ids = append(ids, int(id))
		}
		sort.Ints(ids)
		mu.Lock()
		reports = append(reports, fmt.Sprintf("%d [%s] restarted=%v", ep, strings.Trim(fmt.Sprint(ids), "[]"), restarted))
		mu.Unlock()
		reconcile(node, ep, present, restarted)
	}
	d.svc = svc
	d.reports = func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(reports) }
	d.signals = func() []Signal { mu.Lock(); defer mu.Unlock(); return slices.Clone(signals) }
	d.t.Cleanup(func() { d.run(svc.Close) })
}

func newDevSite(t *testing.T) *devSite {
	t.Helper()
	d := &devSite{t: t, site: fabrictest.NewSite(t, fabrictest.Options{Loopback: true}), port: &devPort{}, rep: &Reports{}}
	d.cam = devCamera(t, d.port, "camera")
	d.dev = d.site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{d.cam}, Configure: func(cfg *node.Config) {
		cfg.Network.TCP = node.TCPConfig{Incoming: true}
	}})
	d.cfg = d.site.ControllerConfig(fabrictest.ControllerOptions{})
	d.cfg.Camera = &controller.CameraConfig{}
	d.cfg.TCP = controller.TCPConfig{Outgoing: true}
	d.rep.Install(&d.cfg)
	d.run(func(ctx context.Context) (err error) { d.c, err = controller.New(ctx, d.cfg); return err })
	t.Cleanup(func() { d.run(d.c.Close) })
	d.run(func(ctx context.Context) error {
		code, err := setup.ManualCode(d.dev.Discriminator(), d.dev.Passcode())
		if err != nil {
			return err
		}
		d.n, err = d.c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	var ok bool
	if d.ep, ok = d.dev.EndpointOf("camera"); !ok {
		t.Fatal("no camera endpoint")
	}
	return d
}

func (d *devSite) cameraSession(id uint16, peer uint64) wpdef.WebRTCSessionStruct {
	return wpdef.WebRTCSessionStruct{Id: id, PeerNodeId: peer, PeerEndpointId: controller.CameraControllerEndpoint, StreamUsage: wpdef.StreamUsageLiveView, VideoStreams: &[]uint16{1}, FabricIndex: d.c.Fabric().FabricIndex}
}

// TestProviderDeviceParityMatterjsServer is describe("WebRTC provider
// sessions against a simulated camera") (WebRtcProviderDeviceTest.ts:66-392,
// matterjs-server at 0bd06f2).
func TestProviderDeviceParityMatterjsServer(t *testing.T) {
	t.Parallel()
	t.Run("reports the camera's CurrentSessions change at the end of the report", func(t *testing.T) {
		t.Parallel()
		d := newDevSite(t)
		d.startService()
		d.eventually("the first report", func() bool { return len(d.reports()) > 0 })
		before := len(d.reports())
		root := d.c.Fabric().RootNodeID
		if err := d.provider().UpsertSession(d.cameraSession(3, root)); err != nil {
			t.Fatal(err)
		}
		d.eventually("the change report", func() bool { return slices.Contains(d.reports()[before:], fmt.Sprintf("%d [3] restarted=false", d.ep)) })
	})
	t.Run("marks the report as a restart when the camera reported StartUp in the same report", func(t *testing.T) {
		t.Parallel()
		d := newDevSite(t)
		d.startService()
		root := d.c.Fabric().RootNodeID
		d.eventually("the first report", func() bool { return len(d.reports()) > 0 })
		// A device restart emits StartUp; the session the camera then
		// lists is the restart's report.
		d.run(d.dev.Restart)
		if err := d.provider().UpsertSession(d.cameraSession(3, root)); err != nil {
			t.Fatal(err)
		}
		d.eventually("the restart report", func() bool { return slices.Contains(d.reports(), fmt.Sprintf("%d [3] restarted=true", d.ep)) })
	})
	t.Run("ends the sessions the camera holds for this server from an earlier run at the first report", func(t *testing.T) {
		t.Parallel()
		d := newDevSite(t)
		root := d.c.Fabric().RootNodeID
		if err := d.provider().UpsertSession(d.cameraSession(3, root)); err != nil {
			t.Fatal(err)
		}
		if err := d.provider().UpsertSession(d.cameraSession(4, 999)); err != nil {
			t.Fatal(err)
		}
		d.startService()
		d.eventually("the leftover's end", func() bool { return slices.Equal(d.port.logged(), []string{"endSession:3"}) })
	})
	t.Run("relays the camera's answer narrowed to the offer this server sent", func(t *testing.T) {
		t.Parallel()
		d := newDevSite(t)
		d.startService()
		offer := "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=recvonly\r\n"
		p, err := d.svc.Provider(d.n, d.ep)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]any{"webRtcSessionId": nil, "sdp": offer, "streamUsage": 3, "videoStreams": []int{1}})
		var id uint16
		d.run(func(ctx context.Context) error {
			resp, _, err := p.Send(ctx, "ProvideOffer", payload)
			if err == nil {
				id = resp.(wpdef.ProvideOfferResponse).WebRtcSessionId
			}
			return err
		})
		d.run(func(ctx context.Context) error {
			return d.provider().Answer(ctx, id, strings.Replace(offer, "recvonly", "sendrecv", 1))
		})
		want := strings.Replace(offer, "recvonly", "sendonly", 1)
		d.eventually("the narrowed answer", func() bool {
			for _, s := range d.signals() {
				if s.Kind == SignalAnswer && s.SDP == want {
					return true
				}
			}
			return false
		})
	})
	t.Run("drops the requestor's sessions of a removed node", func(t *testing.T) {
		t.Parallel()
		d := newDevSite(t)
		d.startService()
		p, err := d.svc.Provider(d.n, d.ep)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]any{"webRtcSessionId": nil, "sdp": "v=0", "streamUsage": 3, "videoStreams": []int{1}})
		d.run(func(ctx context.Context) error { _, _, err := p.Send(ctx, "ProvideOffer", payload); return err })
		if got := d.c.CameraRequestor().Sessions(); len(got) != 1 {
			t.Fatalf("requestor %+v", got)
		}
		d.run(func(ctx context.Context) error { return d.c.Registry().Decommission(ctx, d.n) })
		d.eventually("the requestor dropped the node", func() bool { return len(d.c.CameraRequestor().Sessions()) == 0 })
	})
	t.Run("ends a leftover session on a camera endpoint the node adds later", func(t *testing.T) {
		t.Parallel()
		d := newDevSite(t)
		d.startService()
		d.eventually("the first report", func() bool { return len(d.reports()) > 0 })
		added := devCamera(t, d.port, "camera2")
		d.run(func(ctx context.Context) error { return d.dev.Node().Aggregator().Add(ctx, added) })
		p, ok := added.WebRtcTransportProvider().Underlying().(*clustercamera.Provider)
		if !ok {
			t.Fatal("no provider on the added camera")
		}
		if err := p.UpsertSession(d.cameraSession(8, d.c.Fabric().RootNodeID)); err != nil {
			t.Fatal(err)
		}
		d.eventually("the added endpoint's leftover end", func() bool { return slices.Contains(d.port.logged(), "endSession:8") })
	})
	t.Run("hands a camera's session report to the camera manager", func(t *testing.T) {
		t.Parallel()
		d := newDevSite(t)
		d.startService()
		d.eventually("the first report", func() bool { return len(d.reports()) > 0 })
		before := len(d.reports())
		if err := d.provider().UpsertSession(d.cameraSession(5, d.c.Fabric().RootNodeID)); err != nil {
			t.Fatal(err)
		}
		d.eventually("the report", func() bool { return slices.Contains(d.reports()[before:], fmt.Sprintf("%d [5] restarted=false", d.ep)) })
	})
}

// TestDeviceIOImageParityMatterjsServer is the image reads of
// describe("MatterCameraDeviceIo.readCameraState"), ("…readWebRtcSessions")
// and ("…missingCameraClusters") (MatterCameraDeviceIoTest.ts:734-909,
// matterjs-server at 0bd06f2), on a camera and a snapshot camera.
func TestDeviceIOImageParityMatterjsServer(t *testing.T) {
	d := &devSite{t: t, site: fabrictest.NewSite(t, fabrictest.Options{Loopback: true}), port: &devPort{}, rep: &Reports{}}
	d.cam = devCamera(t, d.port, "camera")
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView}
	snap, err := device.NewSnapshotCamera(device.Options{StableKey: device.StringKey("snapshot"), Name: "Snapshot"},
		csdef.Declare{Features: csdef.FeatureSnapshot, Initial: csdef.Patch{SupportedStreamUsages: &usages, StreamUsagePriorities: &usages}, SnapshotStreamPort: d.port, SnapshotCapturePort: d.port})
	if err != nil {
		t.Fatal(err)
	}
	d.dev = d.site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{d.cam, snap}, Configure: func(cfg *node.Config) {
		cfg.Network.TCP = node.TCPConfig{Incoming: true}
	}})
	d.cfg = d.site.ControllerConfig(fabrictest.ControllerOptions{})
	d.cfg.Camera = &controller.CameraConfig{}
	d.cfg.TCP = controller.TCPConfig{Outgoing: true}
	d.rep.Install(&d.cfg)
	d.run(func(ctx context.Context) (err error) { d.c, err = controller.New(ctx, d.cfg); return err })
	t.Cleanup(func() { d.run(d.c.Close) })
	d.run(func(ctx context.Context) error {
		code, err := setup.ManualCode(d.dev.Discriminator(), d.dev.Passcode())
		if err != nil {
			return err
		}
		d.n, err = d.c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()})
		return err
	})
	d.ep, _ = d.dev.EndpointOf("camera")
	snapEP, _ := d.dev.EndpointOf("snapshot")
	d.startService()
	io := d.svc.io
	at := func(ep uint16) target { addr, _ := d.n.PeerAddress(); return target{addr.NodeID, ep} }
	ctx := context.Background()
	const missingEP = 99

	t.Run("reports nothing for an endpoint that does not exist", func(t *testing.T) {
		if st, err := io.readCameraState(ctx, at(missingEP)); st != nil || err != nil {
			t.Errorf("%+v %v", st, err)
		}
	})
	t.Run("reports nothing for an endpoint without AV stream management", func(t *testing.T) {
		if st, err := io.readCameraState(ctx, at(0)); st != nil || err != nil {
			t.Errorf("%+v %v", st, err)
		}
	})
	t.Run("reports nothing for an endpoint without the provider cluster", func(t *testing.T) {
		if _, ok := io.readWebRTCSessions(ctx, at(snapEP)); ok {
			t.Error("sessions for a provider-less endpoint")
		}
	})
	t.Run("reports nothing for an endpoint that does not exist (sessions)", func(t *testing.T) {
		if _, ok := io.readWebRTCSessions(ctx, at(missingEP)); ok {
			t.Error("sessions for a missing endpoint")
		}
	})
	root := d.c.Fabric().RootNodeID
	seen := func(ids ...uint16) []DeviceSession {
		var got []DeviceSession
		d.eventually("the sessions in the image", func() bool {
			got, _ = io.readWebRTCSessions(ctx, at(d.ep))
			if len(got) != len(ids) {
				return false
			}
			for i, s := range got {
				if s.ID != ids[i] {
					return false
				}
			}
			return true
		})
		return got
	}
	// The requestor holds this run's sessions 3 and 5, so the leftover
	// sweep leaves them alone.
	for _, id := range []uint16{3, 5} {
		if err := d.c.CameraRequestor().UpsertSession(wrdef.WebRTCSessionStruct{Id: id, PeerNodeId: at(d.ep).nodeID, PeerEndpointId: d.ep, StreamUsage: 3, VideoStreams: &[]uint16{1}, FabricIndex: d.c.Fabric().FabricIndex}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("names this server's own sessions by the peer node id the camera recorded", func(t *testing.T) {
		if err := d.provider().UpsertSession(d.cameraSession(3, root)); err != nil {
			t.Fatal(err)
		}
		if err := d.provider().UpsertSession(d.cameraSession(4, 999)); err != nil {
			t.Fatal(err)
		}
		got := seen(3, 4)
		if !got[0].EstablishedByUs || got[1].EstablishedByUs || got[1].PeerNodeID != 999 {
			t.Errorf("%+v", got)
		}
	})
	t.Run("names the streams a revision-1 camera states as singular ids", func(t *testing.T) {
		s := d.cameraSession(5, root)
		s.VideoStreams, s.VideoStreamId = nil, &spec.Nullable[uint16]{Value: 1}
		if err := d.provider().UpsertSession(s); err != nil {
			t.Fatal(err)
		}
		got := seen(3, 4, 5)
		if !slices.Equal(got[2].VideoStreams, []uint16{1}) || len(got[2].AudioStreams) != 0 {
			t.Errorf("%+v", got[2])
		}
	})
	t.Run("reports a session that names no stream either way with empty lists", func(t *testing.T) {
		got, _ := io.readWebRTCSessions(ctx, at(d.ep))
		for _, s := range got {
			if s.VideoStreams == nil || s.AudioStreams == nil {
				t.Errorf("a nil list: %+v", s)
			}
		}
	})
	t.Run("reports both clusters missing when the endpoint does not exist", func(t *testing.T) {
		if got := io.missingCameraClusters(ctx, at(missingEP)); !slices.Equal(got, []uint32{csdef.ClusterID, wpdef.ClusterID}) {
			t.Errorf("%v", got)
		}
	})
	t.Run("reports only the transport provider missing when the endpoint carries just AV stream management", func(t *testing.T) {
		if got := io.missingCameraClusters(ctx, at(snapEP)); !slices.Equal(got, []uint32{wpdef.ClusterID}) {
			t.Errorf("%v", got)
		}
	})
	t.Run("reports nothing missing when the endpoint carries both clusters", func(t *testing.T) {
		if got := io.missingCameraClusters(ctx, at(d.ep)); len(got) != 0 {
			t.Errorf("%v", got)
		}
	})
}
