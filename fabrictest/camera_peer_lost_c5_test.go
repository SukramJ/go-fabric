// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/cluster/camera"
	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
)

// lostCameraPortC5 is the camera's WebRTC stack: it stores the sessions
// it is asked for and records the ones the node ends.
type lostCameraPortC5 struct {
	mu    sync.Mutex
	ended []uint16
}

func (*lostCameraPortC5) SolicitOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return wpdef.WebRTCSessionStruct{Id: a.SessionID, PeerNodeId: a.PeerNodeID, PeerEndpointId: a.OriginatingEndpointID, FabricIndex: a.FabricIndex, StreamUsage: a.StreamUsage}, false, nil
}

func (*lostCameraPortC5) ProvideOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return wpdef.WebRTCSessionStruct{Id: a.SessionID, PeerNodeId: a.PeerNodeID, PeerEndpointId: a.OriginatingEndpointID, FabricIndex: a.FabricIndex, StreamUsage: a.StreamUsage}, nil
}

func (*lostCameraPortC5) ProvideAnswer(context.Context, uint16, string) error { return nil }

func (*lostCameraPortC5) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}

func (p *lostCameraPortC5) EndSession(_ context.Context, id uint16, _ uint8) error {
	p.mu.Lock()
	p.ended = append(p.ended, id)
	p.mu.Unlock()
	return nil
}

func (p *lostCameraPortC5) endedIDs() []uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.ended)
}

// silentRequestorC5 is a requestor's host that stops answering: its
// Answer does not return until the test releases it, so the requestor
// sends no response to the camera's command.
type silentRequestorC5 struct {
	release chan struct{}
	once    sync.Once
}

func (*silentRequestorC5) Offer(context.Context, wrdef.WebRTCSessionStruct, wrdef.OfferRequest) {}

func (r *silentRequestorC5) Answer(ctx context.Context, _ wrdef.WebRTCSessionStruct, _ string) {
	select {
	case <-r.release:
	case <-ctx.Done():
	}
}

func (*silentRequestorC5) IceCandidates(context.Context, wrdef.WebRTCSessionStruct, []wrdef.ICECandidateStruct) {
}

func (*silentRequestorC5) End(context.Context, wrdef.WebRTCSessionStruct, uint8) {}

func (r *silentRequestorC5) free() { r.once.Do(func() { close(r.release) }) }

// TestCameraSignalToASilentRequestorEndsItsSessions (review II,
// R5-test-5309, C5-590-closeLostPeer-test): on a loopback site the
// requestor opens its own TCP session to the camera from its client stack,
// and the camera's Answer — a Large Message command — goes over that
// session from the camera's bridge. The requestor stops answering: no
// response reaches the camera before the bridge's receive deadline, so the
// bridge proves the peer lost (closeLostPeer) and force-closes its
// sessions, and with no session left in either of the camera's stacks the
// node ends the requestor's WebRTC session through the port's EndSession.
func TestCameraSignalToASilentRequestorEndsItsSessions(t *testing.T) {
	ctx := t.Context()
	logs := &peerLostLogsC5{}
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true, Logger: slog.New(logs)})
	tcp := node.TCPConfig{Incoming: true, Outgoing: true}
	camPort := &lostCameraPortC5{}
	cam, err := device.NewCamera(device.Options{StableKey: endpoint.StringKey("cam:1"), Name: "cam"}, wpdef.Declare{ProviderPort: camPort}, csdef.Declare{Initial: csdef.Patch{
		MicrophoneCapabilities: &csdef.AudioCapabilitiesStruct{
			MaxNumberOfChannels: 1, SupportedCodecs: []csdef.AudioCodecEnum{csdef.AudioCodecOpus},
			SupportedSampleRates: []uint32{48000}, SupportedBitDepths: []uint8{16},
		},
		VideoSensorParams:     &csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30},
		MinViewportResolution: &csdef.VideoResolutionStruct{Width: 640, Height: 480},
		Viewport:              &csdef.ViewportStruct{X1: 0, Y1: 0, X2: 1920, Y2: 1080},
	}})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{cam}, Configure: func(cfg *node.Config) { cfg.Network.TCP = tcp },
	}})
	silent := &silentRequestorC5{release: make(chan struct{})}
	// Registered after the site: it runs before the site closes, so the
	// requestor's handler returns before its node stops.
	t.Cleanup(silent.free)
	ctl, err := device.NewCameraController(device.Options{StableKey: endpoint.StringKey("ctl:1"), Name: "ctl"}, wrdef.Declare{RequestorPort: silent})
	if err != nil {
		t.Fatal(err)
	}
	ctlDev := site.AddDevice(fabrictest.Topology{
		Bridged: []node.Endpoint{ctl},
		Configure: func(cfg *node.Config) {
			cfg.WebRTCRequestor = &node.WebRTCRequestorConfig{GrantFabric: func(node.FabricInfo) bool { return true }}
			cfg.Client = &node.ClientConfig{}
			cfg.Network.TCP = tcp
		},
	})
	if _, err := pair.Controller.Commission(ctx, ctlDev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	camFabric, ctlFabric := oneFabricC5(t, pair.Device), oneFabricC5(t, ctlDev)
	ctlEp, _ := ctlDev.EndpointOf("ctl:1")
	camEp, _ := pair.Device.EndpointOf("cam:1")
	prov, ok := cam.Server(wpdef.ClusterID).(*camera.Provider)
	if !ok {
		t.Fatalf("the camera's WebRtcTransportProvider is %T", cam.Server(wpdef.ClusterID))
	}
	if err := prov.UpsertSession(camera.ProviderSession{
		Id: 1, PeerNodeId: ctlFabric.NodeID, PeerEndpointId: ctlEp, FabricIndex: camFabric.FabricIndex, StreamUsage: wpdef.StreamUsageLiveView,
		VideoStreams: &[]uint16{1},
	}); err != nil {
		t.Fatal(err)
	}
	req, ok := ctl.Server(wrdef.ClusterID).(*camera.Requestor)
	if !ok {
		t.Fatalf("the controller's WebRtcTransportRequestor is %T", ctl.Server(wrdef.ClusterID))
	}
	if err := req.UpsertSession(camera.WebRTCSession{
		Id: 1, PeerNodeId: camFabric.NodeID, PeerEndpointId: camEp, FabricIndex: ctlFabric.FabricIndex, StreamUsage: wrdef.StreamUsageLiveView,
		VideoStreams: &[]uint16{1},
	}); err != nil {
		t.Fatal(err)
	}

	// The requestor's own TCP session to the camera, from its client stack.
	tr := exchange.TransportTCP
	if err := site.Run(ctx, func(ctx context.Context) error {
		pr, err := ctlDev.Node().Peers().For(handshake.PeerAddress{FabricIndex: ctlFabric.FabricIndex, NodeID: camFabric.NodeID})
		if err != nil {
			return err
		}
		_, err = pr.Connect(ctx, peer.ConnectOptions{RequiredTransport: &tr})
		return err
	}); err != nil {
		t.Fatalf("the requestor's connect over TCP: %v", err)
	}
	if err := pair.Device.AwaitCASESession(ctx, camFabric.FabricIndex, ctlFabric.NodeID); err != nil {
		t.Fatal(err)
	}

	// The Answer goes unanswered: the camera's wait ends at the bridge's
	// receive deadline, which the site's clock reaches while nothing else
	// runs. The requestor's blocked handler is work the site counts, so
	// each step waits it out first: the waits get a deadline of their own
	// rather than the site's default, which a slow run (-race) exceeds.
	wctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	answerErr := site.Run(wctx, func(ctx context.Context) error { return prov.Answer(ctx, 1, "v=0 answer") })
	if answerErr == nil {
		t.Fatal("Answer to a requestor that does not answer returned nil")
	}
	if err := site.Eventually(wctx, "the lost requestor's WebRTC session ended", func() bool { return len(prov.Sessions()) == 0 }); err != nil {
		t.Fatalf("%v (the Answer returned %v)", err, answerErr)
	}
	if got := camPort.endedIDs(); !slices.Equal(got, []uint16{1}) {
		t.Errorf("the camera's port ended %v, want [1]", got)
	}
	// The route is the bridge's: its receive deadline proved the peer
	// lost and closed the requestor's sessions (closeLostPeer's log line).
	if !logs.has("matter.session.peer_lost", ctlFabric.NodeID) {
		t.Error("no matter.session.peer_lost for the requestor: the sessions did not end through the bridge's peer loss")
	}
}

// peerLostLogsC5 keeps the message and node_id of every record.
type peerLostLogsC5 struct {
	mu  sync.Mutex
	got []string
}

func (*peerLostLogsC5) Enabled(context.Context, slog.Level) bool { return true }

func (l *peerLostLogsC5) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "node_id" {
			l.mu.Lock()
			l.got = append(l.got, fmt.Sprintf("%s %d", r.Message, a.Value.Uint64()))
			l.mu.Unlock()
		}
		return true
	})
	return nil
}

func (l *peerLostLogsC5) WithAttrs([]slog.Attr) slog.Handler { return l }

func (l *peerLostLogsC5) WithGroup(string) slog.Handler { return l }

func (l *peerLostLogsC5) has(msg string, nodeID uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Contains(l.got, fmt.Sprintf("%s %d", msg, nodeID))
}

// oneFabricC5 is the device's one fabric.
func oneFabricC5(t *testing.T, d *fabrictest.Device) node.FabricInfo {
	t.Helper()
	fabrics, err := d.Node().FabricInfos(t.Context())
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %v, %v", fabrics, err)
	}
	return fabrics[0]
}
