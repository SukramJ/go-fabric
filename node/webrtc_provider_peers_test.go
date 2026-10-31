// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/camera"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// cameraStack is the camera's WebRTC stack: it stores whatever it is
// asked (the sessions a controller opens are not this test's subject).
type cameraStack struct{}

func (cameraStack) SolicitOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return wpdef.WebRTCSessionStruct{Id: a.SessionID, PeerNodeId: a.PeerNodeID, PeerEndpointId: a.OriginatingEndpointID, FabricIndex: a.FabricIndex, StreamUsage: a.StreamUsage}, false, nil
}

func (cameraStack) ProvideOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return wpdef.WebRTCSessionStruct{Id: a.SessionID, PeerNodeId: a.PeerNodeID, PeerEndpointId: a.OriginatingEndpointID, FabricIndex: a.FabricIndex, StreamUsage: a.StreamUsage}, nil
}

func (cameraStack) ProvideAnswer(context.Context, uint16, string) error { return nil }

func (cameraStack) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}

func (cameraStack) EndSession(context.Context, uint16, uint8) error { return nil }

// requestorStack records the signals the camera controller's requestor
// hands to its host.
type requestorStack struct {
	mu         sync.Mutex
	answers    []string
	candidates [][]wrdef.ICECandidateStruct
	ended      []uint8
	sessions   []wrdef.WebRTCSessionStruct
}

func (r *requestorStack) Offer(context.Context, wrdef.WebRTCSessionStruct, wrdef.OfferRequest) {}

func (r *requestorStack) Answer(_ context.Context, s wrdef.WebRTCSessionStruct, sdp string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers = append(r.answers, sdp)
	r.sessions = append(r.sessions, s)
}

func (r *requestorStack) IceCandidates(_ context.Context, _ wrdef.WebRTCSessionStruct, c []wrdef.ICECandidateStruct) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.candidates = append(r.candidates, c)
}

func (r *requestorStack) End(_ context.Context, _ wrdef.WebRTCSessionStruct, reason uint8) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ended = append(r.ended, reason)
}

// TestCameraProviderSignalsReachTheRequestor: a camera and a camera
// controller on one controller's fabric. The camera's host registers the
// session on both sides, as the controller's ProvideOffer and the
// requestor's host would have, then the camera's Answer, IceCandidates and
// End reach the requestor's port over a CASE session the camera's node
// initiates (connectedhomeip webrtc-provider-manager.cpp:860-1328 at
// 6170af84: FindOrEstablishSession on the session's ScopedNodeId, the
// command at originatingEndpointId). The requestor's Operate entry is the
// one the node installs (webrtc_requestor_acl.go).
func TestCameraProviderSignalsReachTheRequestor(t *testing.T) {
	site := quietSite(t)
	ctx := parityCtx(t)
	cam, err := device.NewCamera(device.Options{StableKey: endpoint.StringKey("cam:1"), Name: "cam"}, wpdef.Declare{ProviderPort: cameraStack{}})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{cam}}})
	stack := &requestorStack{}
	ctl, err := device.NewCameraController(device.Options{StableKey: endpoint.StringKey("ctl:1"), Name: "ctl"}, wrdef.Declare{RequestorPort: stack})
	if err != nil {
		t.Fatal(err)
	}
	ctlDev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{ctl}})
	if _, err := pair.Controller.Commission(ctx, ctlDev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	camEp, ok := pair.Device.EndpointOf("cam:1")
	if !ok {
		t.Fatal("the camera has no endpoint")
	}
	ctlEp, ok := ctlDev.EndpointOf("ctl:1")
	if !ok {
		t.Fatal("the camera controller has no endpoint")
	}
	camFabric := oneFabric(t, pair.Device)
	ctlFabric := oneFabric(t, ctlDev)
	prov, ok := cam.Server(wpdef.ClusterID).(*camera.Provider)
	if !ok {
		t.Fatalf("the camera's WebRtcTransportProvider is %T", cam.Server(wpdef.ClusterID))
	}
	req, ok := ctl.Server(wrdef.ClusterID).(*camera.Requestor)
	if !ok {
		t.Fatalf("the controller's WebRtcTransportRequestor is %T", ctl.Server(wrdef.ClusterID))
	}

	// The node attached its connector at Start: an unknown session is
	// refused as such, not for the missing connector.
	if err := prov.Answer(ctx, 9, "v=0"); !errors.Is(err, camera.ErrUnknownSession) {
		t.Fatalf("Answer for an unknown session: %v", err)
	}

	// The session, registered on both sides by the hosts.
	if err := prov.UpsertSession(camera.ProviderSession{
		Id: 1, PeerNodeId: ctlFabric.NodeID, PeerEndpointId: ctlEp, FabricIndex: camFabric.FabricIndex, StreamUsage: wpdef.StreamUsageLiveView,
	}); err != nil {
		t.Fatal(err)
	}
	if err := req.UpsertSession(camera.WebRTCSession{
		Id: 1, PeerNodeId: camFabric.NodeID, PeerEndpointId: camEp, FabricIndex: ctlFabric.FabricIndex, StreamUsage: wrdef.StreamUsageLiveView,
	}); err != nil {
		t.Fatal(err)
	}

	run := func(what string, fn func(ctx context.Context) error) {
		t.Helper()
		if err := site.Run(ctx, fn); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	run("Answer", func(ctx context.Context) error { return prov.Answer(ctx, 1, "v=0 answer") })
	candidates := []camera.ProviderICECandidate{{Candidate: "candidate:1 1 udp 1 192.0.2.1 5000 typ host"}}
	run("IceCandidates", func(ctx context.Context) error { return prov.IceCandidates(ctx, 1, candidates) })
	run("End", func(ctx context.Context) error { return prov.End(ctx, 1, 6) })

	stack.mu.Lock()
	defer stack.mu.Unlock()
	if len(stack.answers) != 1 || stack.answers[0] != "v=0 answer" {
		t.Fatalf("the requestor's answers: %q", stack.answers)
	}
	if s := stack.sessions[0]; s.Id != 1 || s.PeerNodeId != camFabric.NodeID || s.FabricIndex != ctlFabric.FabricIndex {
		t.Fatalf("the requestor matched session %+v", s)
	}
	if len(stack.candidates) != 1 || len(stack.candidates[0]) != 1 || stack.candidates[0][0].Candidate != candidates[0].Candidate {
		t.Fatalf("the requestor's candidates: %+v", stack.candidates)
	}
	if len(stack.ended) != 1 || stack.ended[0] != 6 {
		t.Fatalf("the requestor's ends: %v", stack.ended)
	}
	if n := len(prov.Sessions()); n != 0 {
		t.Fatalf("the camera holds %d sessions after End", n)
	}
	if n := len(req.Sessions()); n != 0 {
		t.Fatalf("the requestor holds %d sessions after End", n)
	}
}

// TestCameraProviderSignalTooLargeForUDP: the camera's CASE session to the
// requestor runs over UDP, and an answer that does not fit one message is
// refused before it is sent (camera.ErrPayloadTooLarge, the value of
// bridge.ErrPayloadTooLarge; chip sends its signals with kLargePayload over
// TCP, webrtc-provider-manager.cpp:860-942).
func TestCameraProviderSignalTooLargeForUDP(t *testing.T) {
	site := quietSite(t)
	ctx := parityCtx(t)
	cam, err := device.NewCamera(device.Options{StableKey: endpoint.StringKey("cam:1"), Name: "cam"}, wpdef.Declare{ProviderPort: cameraStack{}})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{cam}}})
	stack := &requestorStack{}
	ctl, err := device.NewCameraController(device.Options{StableKey: endpoint.StringKey("ctl:1"), Name: "ctl"}, wrdef.Declare{RequestorPort: stack})
	if err != nil {
		t.Fatal(err)
	}
	ctlDev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{ctl}})
	if _, err := pair.Controller.Commission(ctx, ctlDev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	ctlEp, _ := ctlDev.EndpointOf("ctl:1")
	camFabric, ctlFabric := oneFabric(t, pair.Device), oneFabric(t, ctlDev)
	prov := cam.Server(wpdef.ClusterID).(*camera.Provider)
	if err := prov.UpsertSession(camera.ProviderSession{
		Id: 1, PeerNodeId: ctlFabric.NodeID, PeerEndpointId: ctlEp, FabricIndex: camFabric.FabricIndex, StreamUsage: wpdef.StreamUsageLiveView,
	}); err != nil {
		t.Fatal(err)
	}
	// 1500 bytes of SDP: more than a UDP message carries (1280 - 48 bytes
	// less the 54-byte message overhead).
	sdp := strings.Repeat("a=candidate:0123456789 ", 66)
	err = site.Run(ctx, func(ctx context.Context) error { return prov.Answer(ctx, 1, sdp) })
	if !errors.Is(err, camera.ErrPayloadTooLarge) || !errors.Is(err, bridge.ErrPayloadTooLarge) {
		t.Fatalf("a %d-byte answer over UDP: %v", len(sdp), err)
	}
	stack.mu.Lock()
	defer stack.mu.Unlock()
	if len(stack.answers) != 0 {
		t.Fatalf("the requestor received %d answers", len(stack.answers))
	}
}

// oneFabric is the device's one fabric.
func oneFabric(t *testing.T, d *fabrictest.Device) node.FabricInfo {
	t.Helper()
	fabrics, err := d.Node().FabricInfos(parityCtx(t))
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %v, %v", fabrics, err)
	}
	return fabrics[0]
}
