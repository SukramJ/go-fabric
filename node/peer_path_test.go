// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The node's peer path (docs/adr/0032, review II wave B): a camera's
// signals — Large Message commands — reach the requestor over a TCP
// session, the requestor's own (reused, whoever opened it) or one the
// camera's client stack establishes; without TCP they are refused before
// anything is sent. On a loopback site: TCP runs over kernel sockets.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/camera"
	gcdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// cameraPeers is a camera and a camera controller (the requestor) on one
// controller's fabric, with one WebRTC session registered on both sides.
type cameraPeers struct {
	site      *fabrictest.Site
	cam, ctl  *fabrictest.Device
	prov      *camera.Provider
	stack     *requestorStack
	camFabric node.FabricInfo
	ctlFabric node.FabricInfo
}

// newCameraPeers builds the pair: camTCP and ctlTCP are the devices' TCP
// roles (nil for none); the requestor runs a client stack
// ([node.Config].Client) to open sessions to the camera itself.
func newCameraPeers(ctx context.Context, t *testing.T, camTCP, ctlTCP *node.TCPConfig) *cameraPeers {
	t.Helper()
	holdSite(t)
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true, Logger: slog.New(slog.DiscardHandler)})
	cam, err := device.NewCamera(device.Options{StableKey: endpoint.StringKey("cam:1"), Name: "cam"}, wpdef.Declare{ProviderPort: cameraStack{}}, cameraRequired())
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{cam}, Configure: func(cfg *node.Config) {
		if camTCP != nil {
			cfg.Network.TCP = *camTCP
		}
	}}})
	stack := &requestorStack{}
	ctl, err := device.NewCameraController(device.Options{StableKey: endpoint.StringKey("ctl:1"), Name: "ctl"}, wrdef.Declare{RequestorPort: stack})
	if err != nil {
		t.Fatal(err)
	}
	ctlDev := site.AddDevice(fabrictest.Topology{
		Bridged: []node.Endpoint{ctl},
		Configure: func(cfg *node.Config) {
			cfg.WebRTCRequestor = &node.WebRTCRequestorConfig{GrantFabric: func(node.FabricInfo) bool { return true }}
			cfg.Client = &node.ClientConfig{}
			if ctlTCP != nil {
				cfg.Network.TCP = *ctlTCP
			}
		},
	})
	if _, err := pair.Controller.Commission(ctx, ctlDev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	ctlEp, _ := ctlDev.EndpointOf("ctl:1")
	cp := &cameraPeers{site: site, cam: pair.Device, ctl: ctlDev, stack: stack, camFabric: oneFabric(t, pair.Device), ctlFabric: oneFabric(t, ctlDev)}
	var ok bool
	if cp.prov, ok = cam.Server(wpdef.ClusterID).(*camera.Provider); !ok {
		t.Fatalf("the camera's WebRtcTransportProvider is %T", cam.Server(wpdef.ClusterID))
	}
	if err := cp.prov.UpsertSession(camera.ProviderSession{
		Id: 1, PeerNodeId: cp.ctlFabric.NodeID, PeerEndpointId: ctlEp, FabricIndex: cp.camFabric.FabricIndex, StreamUsage: wpdef.StreamUsageLiveView,
		VideoStreams: &[]uint16{1}, // a session names a stream ("O.a+", review II R2-P6-par-1304)
	}); err != nil {
		t.Fatal(err)
	}
	req, ok := ctl.Server(wrdef.ClusterID).(*camera.Requestor)
	if !ok {
		t.Fatalf("the controller's WebRtcTransportRequestor is %T", ctl.Server(wrdef.ClusterID))
	}
	camEp, _ := pair.Device.EndpointOf("cam:1")
	if err := req.UpsertSession(camera.WebRTCSession{
		Id: 1, PeerNodeId: cp.camFabric.NodeID, PeerEndpointId: camEp, FabricIndex: cp.ctlFabric.FabricIndex, StreamUsage: wrdef.StreamUsageLiveView,
		VideoStreams: &[]uint16{1}, // a session names a stream ("O.a+", review II R2-P6-par-1304)
	}); err != nil {
		t.Fatal(err)
	}
	return cp
}

// requestorOpens has the requestor open a session to the camera over the
// transport t from its own client stack.
func (cp *cameraPeers) requestorOpens(ctx context.Context, t *testing.T, tr exchange.Transport) {
	t.Helper()
	err := cp.site.Run(ctx, func(ctx context.Context) error {
		pr, err := cp.ctl.Node().Peers().For(handshake.PeerAddress{FabricIndex: cp.ctlFabric.FabricIndex, NodeID: cp.camFabric.NodeID})
		if err != nil {
			return err
		}
		s, err := pr.Connect(ctx, peer.ConnectOptions{RequiredTransport: &tr})
		if err == nil && s.Transport() != tr {
			t.Errorf("the requestor's session runs over %v, want %v", s.Transport(), tr)
		}
		return err
	})
	if err != nil {
		t.Fatalf("the requestor's connect over %v: %v", tr, err)
	}
}

// camClientSessions is the camera's peer-path client sessions with the
// requestor.
func (cp *cameraPeers) camClientSessions() []*exchange.Session {
	return node.PeerPathClientSessions(cp.cam.Node(), cp.camFabric.FabricIndex, cp.ctlFabric.NodeID)
}

func (cp *cameraPeers) answers() []string {
	cp.stack.mu.Lock()
	defer cp.stack.mu.Unlock()
	return append([]string(nil), cp.stack.answers...)
}

var bothTCPRoles = &node.TCPConfig{Incoming: true, Outgoing: true}

// TestPeerPathEstablishesTCPBesideAUDPSession: the requestor's session to
// the camera runs over UDP, which carries no large messages; the camera's
// client stack establishes a TCP session and the signal goes over it (case
// b).
func TestPeerPathEstablishesTCPBesideAUDPSession(t *testing.T) {
	ctx := parityCtx(t)
	cp := newCameraPeers(ctx, t, bothTCPRoles, bothTCPRoles)
	cp.requestorOpens(ctx, t, exchange.TransportUDP)
	if err := cp.site.Run(ctx, func(ctx context.Context) error { return cp.prov.Answer(ctx, 1, "v=0 answer") }); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got := cp.answers(); len(got) != 1 {
		t.Fatalf("the requestor's answers: %q", got)
	}
	sessions := cp.camClientSessions()
	if len(sessions) != 1 || sessions[0].Transport() != exchange.TransportTCP {
		t.Fatalf("the camera's client sessions with the requestor: %v, want one over TCP", sessions)
	}
}

// TestPeerPathWithoutTCPRefusesTheSignal: the camera dials no TCP; a
// signal is refused with camera.ErrNoLargeMessageTransport and nothing
// reaches the requestor (case c).
func TestPeerPathWithoutTCPRefusesTheSignal(t *testing.T) {
	ctx := parityCtx(t)
	cp := newCameraPeers(ctx, t, nil, bothTCPRoles)
	cp.requestorOpens(ctx, t, exchange.TransportUDP)
	err := cp.site.Run(ctx, func(ctx context.Context) error { return cp.prov.Answer(ctx, 1, "v=0 answer") })
	if !errors.Is(err, camera.ErrNoLargeMessageTransport) {
		t.Fatalf("Answer without TCP: %v, want ErrNoLargeMessageTransport", err)
	}
	if got := cp.answers(); len(got) != 0 {
		t.Fatalf("the requestor received %q", got)
	}
}

// TestPeerPathConcurrentSignalsShareOneHandshake: two signals at once to a
// requestor the camera holds no session with establish one session — the
// client stack's connection process is per peer (review II,
// R2-P5-peer-04).
func TestPeerPathConcurrentSignalsShareOneHandshake(t *testing.T) {
	ctx := parityCtx(t)
	cp := newCameraPeers(ctx, t, bothTCPRoles, bothTCPRoles)
	candidates := []camera.ProviderICECandidate{{Candidate: "candidate:1 1 udp 1 192.0.2.1 5000 typ host"}}
	err := cp.site.Run(ctx, func(ctx context.Context) error {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Go(func() { errs[i] = cp.prov.IceCandidates(ctx, 1, candidates) })
		}
		wg.Wait()
		return errors.Join(errs...)
	})
	if err != nil {
		t.Fatalf("IceCandidates: %v", err)
	}
	cp.stack.mu.Lock()
	n := len(cp.stack.candidates)
	cp.stack.mu.Unlock()
	if n != 2 {
		t.Fatalf("the requestor received %d candidate lists, want 2", n)
	}
	if sessions := cp.camClientSessions(); len(sessions) != 1 {
		t.Fatalf("the camera holds %d client sessions with the requestor, want 1", len(sessions))
	}
}

// TestPeerPathServedOnTheRequestorsOwnSession: the requestor opened a TCP
// session to the camera from its client stack; the camera's signal goes
// back over it (the camera builds no client stack) and the requestor's node
// serves it there, under its ACL — the requestor's Operate entry for its
// WebRtcTransportRequestor admits the signal, and a command the entry does
// not cover (GeneralCommissioning.ArmFailSafe, Administer) is refused
// UnsupportedAccess on that session as on the server stack (review II,
// Fable P-9; matter.js serves every session of the node whoever opened it,
// ExchangeManager.ts:331, :446, :463-467 at db021da0).
func TestPeerPathServedOnTheRequestorsOwnSession(t *testing.T) {
	ctx := parityCtx(t)
	cp := newCameraPeers(ctx, t, bothTCPRoles, bothTCPRoles)
	cp.requestorOpens(ctx, t, exchange.TransportTCP)
	if err := cp.site.Run(ctx, func(ctx context.Context) error { return cp.prov.Answer(ctx, 1, "v=0 answer") }); err != nil {
		t.Fatalf("Answer on the requestor's own session: %v", err)
	}
	if got := cp.answers(); len(got) != 1 || got[0] != "v=0 answer" {
		t.Fatalf("the requestor's answers: %q", got)
	}
	err := cp.site.Run(ctx, func(ctx context.Context) error {
		_, err := node.PeerPathInvoke(ctx, cp.cam.Node(), cp.camFabric.FabricIndex, cp.ctlFabric.NodeID, 0,
			gcdef.ClusterID, gcdef.CmdArmFailSafe, gcdef.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
		return err
	})
	var se im.StatusCodeError
	if !errors.As(err, &se) || se.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("ArmFailSafe without Administer on the requestor's own session: %v, want UnsupportedAccess", err)
	}
	if node.PeerPathClientBuilt(cp.cam.Node()) {
		t.Error("the camera built a client stack although the requestor's TCP session was live")
	}
}
