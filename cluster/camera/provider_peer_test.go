// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The camera's signals to the requestor, after connectedhomeip's
// webrtc-provider-manager.cpp at 6170af84 (SendOfferCommand :1093,
// SendAnswerCommand :1189, SendICECandidatesCommand :1221, SendEndCommand
// :1301): each an Invoke on the session's peer node and fabric at its
// PeerEndpointId. chip has no test of the manager; the cases are the
// route's own.
package camera_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/im"
)

type peerCall struct {
	fabric   uint8
	node     uint64
	endpoint uint16
	cluster  uint32
	command  uint32
	fields   spec.Encodable
}

// fakePeers is the PeerConnector: it records every Invoke, and answers
// connectErr on ConnectPeer or invokeErr on Invoke.
type fakePeers struct {
	mu         sync.Mutex
	calls      []peerCall
	connectErr error
	invokeErr  error
}

type fakePeerSession struct {
	f      *fakePeers
	fabric uint8
	node   uint64
}

func (f *fakePeers) ConnectPeer(_ context.Context, fabric uint8, node uint64) (camera.PeerSession, error) {
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	return &fakePeerSession{f: f, fabric: fabric, node: node}, nil
}

func (s *fakePeerSession) Invoke(_ context.Context, endpoint uint16, cluster, command uint32, fields spec.Encodable) (*spec.Node, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.calls = append(s.f.calls, peerCall{fabric: s.fabric, node: s.node, endpoint: endpoint, cluster: cluster, command: command, fields: fields})
	return nil, s.f.invokeErr
}

func (f *fakePeers) last(t *testing.T) peerCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("no Invoke")
	}
	return f.calls[len(f.calls)-1]
}

// statusErr is a peer's status answer, as the bridge's session returns it.
type statusErr struct{ code im.StatusCode }

func (e statusErr) Error() string                   { return "status " + e.code.String() }
func (e statusErr) MatterStatusCode() im.StatusCode { return e.code }

const (
	sigPeerNode     uint64 = 0x1122334455667788
	sigPeerEndpoint uint16 = 3
	sigPeerFabric   uint8  = 2
)

// signalProvider is a provider holding session 5 of the peer, with the
// fake connector attached.
func signalProvider(t *testing.T) (*camera.Provider, *fakePeers) {
	t.Helper()
	p, _ := newProvider(t, nil)
	peers := &fakePeers{}
	p.AttachPeerConnector(peers)
	if err := p.UpsertSession(camera.ProviderSession{
		Id: 5, PeerNodeId: sigPeerNode, PeerEndpointId: sigPeerEndpoint, FabricIndex: sigPeerFabric, StreamUsage: wpdef.StreamUsageLiveView,
	}); err != nil {
		t.Fatal(err)
	}
	return p, peers
}

// TestProviderSignalsNeedAConnector: without a PeerConnector every signal
// is ErrNoPeerConnector, and End keeps the session.
func TestProviderSignalsNeedAConnector(t *testing.T) {
	p, _ := signalProvider(t)
	p.AttachPeerConnector(nil)
	if err := p.Answer(context.Background(), 5, "v=0"); !errors.Is(err, camera.ErrNoPeerConnector) {
		t.Fatalf("Answer: %v", err)
	}
	if err := p.End(context.Background(), 5, 0); !errors.Is(err, camera.ErrNoPeerConnector) {
		t.Fatalf("End: %v", err)
	}
	if n := len(p.Sessions()); n != 1 {
		t.Fatalf("End without a connector removed the session: %d sessions", n)
	}
}

// TestProviderSignalsUnknownSession: a session id CurrentSessions does not
// hold is ErrUnknownSession, and nothing is sent (chip :1198-1202,
// "WebTransport not found for sessionId").
func TestProviderSignalsUnknownSession(t *testing.T) {
	p, peers := signalProvider(t)
	for name, signal := range map[string]func() error{
		"Offer":  func() error { return p.Offer(context.Background(), 6, "v=0") },
		"Answer": func() error { return p.Answer(context.Background(), 6, "v=0") },
		"IceCandidates": func() error {
			return p.IceCandidates(context.Background(), 6, []camera.ProviderICECandidate{{Candidate: "c"}})
		},
		"End": func() error { return p.End(context.Background(), 6, 0) },
	} {
		if err := signal(); !errors.Is(err, camera.ErrUnknownSession) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(peers.calls) != 0 {
		t.Fatalf("%d invokes for unknown sessions", len(peers.calls))
	}
}

// TestProviderSignalsReachThePeer: Offer, Answer and IceCandidates go to
// the session's peer node on its fabric, at its PeerEndpointId, as the
// requestor's commands with the session's id (chip :1093-1298: the
// ScopedNodeId of the request, requestArgs.originatingEndpointId).
func TestProviderSignalsReachThePeer(t *testing.T) {
	p, peers := signalProvider(t)
	check := func(t *testing.T, c peerCall, command uint32) {
		t.Helper()
		if c.fabric != sigPeerFabric || c.node != sigPeerNode || c.endpoint != sigPeerEndpoint || c.cluster != wrdef.ClusterID || c.command != command {
			t.Fatalf("invoke %+v, want fabric %d node %#x endpoint %d cluster %#x command %#x", c, sigPeerFabric, sigPeerNode, sigPeerEndpoint, wrdef.ClusterID, command)
		}
	}
	t.Run("Offer", func(t *testing.T) {
		if err := p.Offer(context.Background(), 5, "v=0 offer"); err != nil {
			t.Fatal(err)
		}
		c := peers.last(t)
		check(t, c, wrdef.CmdOffer)
		if got := c.fields.(wrdef.OfferRequest); got.WebRtcSessionId != 5 || got.Sdp != "v=0 offer" || got.IceServers != nil || got.IceTransportPolicy != nil {
			t.Fatalf("Offer fields %+v", got)
		}
	})
	t.Run("Answer", func(t *testing.T) {
		if err := p.Answer(context.Background(), 5, "v=0 answer"); err != nil {
			t.Fatal(err)
		}
		c := peers.last(t)
		check(t, c, wrdef.CmdAnswer)
		if got := c.fields.(wrdef.AnswerRequest); got.WebRtcSessionId != 5 || got.Sdp != "v=0 answer" {
			t.Fatalf("Answer fields %+v", got)
		}
	})
	t.Run("IceCandidates", func(t *testing.T) {
		in := []camera.ProviderICECandidate{
			{Candidate: "candidate:1", SdpMid: spec.ValueOf("0"), SdpmLineIndex: spec.ValueOf[uint16](0)},
			{Candidate: "candidate:2", SdpMid: spec.NullOf[string](), SdpmLineIndex: spec.NullOf[uint16]()},
			// A candidate without a mid: chip :1261-1268 sends SDPMid null.
			{Candidate: "candidate:3"},
		}
		if err := p.IceCandidates(context.Background(), 5, in); err != nil {
			t.Fatal(err)
		}
		c := peers.last(t)
		check(t, c, wrdef.CmdIceCandidates)
		got := c.fields.(wrdef.IceCandidatesRequest)
		if got.WebRtcSessionId != 5 || len(got.IceCandidates) != 3 {
			t.Fatalf("IceCandidates fields %+v", got)
		}
		for i, want := range in[:2] {
			if g := got.IceCandidates[i]; g.Candidate != want.Candidate || g.SdpMid != want.SdpMid || g.SdpmLineIndex != want.SdpmLineIndex {
				t.Fatalf("candidate %d %+v, want %+v", i, g, want)
			}
		}
		if g := got.IceCandidates[2]; g.Candidate != "candidate:3" || !g.SdpMid.Null || g.SdpmLineIndex.Null || g.SdpmLineIndex.Value != 0 {
			t.Fatalf("candidate without a mid %+v, want SDPMid null and SDPMLineIndex 0", g)
		}
	})
	t.Run("IceCandidates without a candidate", func(t *testing.T) {
		before := len(peers.calls)
		if err := p.IceCandidates(context.Background(), 5, nil); !errors.Is(err, camera.ErrNoICECandidates) {
			t.Fatalf("empty candidates: %v", err)
		}
		if len(peers.calls) != before {
			t.Fatal("an empty candidate list was sent")
		}
	})
	if n := len(p.Sessions()); n != 1 {
		t.Fatalf("the signals changed CurrentSessions: %d sessions", n)
	}
}

// TestProviderEndRemovesTheSession: End sends the requestor's End with the
// reason and removes the session whether the peer took it or not (chip
// :999-1000, CleanupSession after SendEndCommand regardless of its
// result); the peer's status is the error.
func TestProviderEndRemovesTheSession(t *testing.T) {
	t.Run("delivered", func(t *testing.T) {
		p, peers := signalProvider(t)
		v := p.MatterDataVersion()
		if err := p.End(context.Background(), 5, 3); err != nil {
			t.Fatal(err)
		}
		c := peers.last(t)
		if c.command != wrdef.CmdEnd || c.endpoint != sigPeerEndpoint {
			t.Fatalf("invoke %+v", c)
		}
		if got := c.fields.(wrdef.EndRequest); got.WebRtcSessionId != 5 || got.Reason != 3 {
			t.Fatalf("End fields %+v", got)
		}
		if n := len(p.Sessions()); n != 0 {
			t.Fatalf("%d sessions after End", n)
		}
		if p.MatterDataVersion() == v {
			t.Fatal("CurrentSessions' data version did not move")
		}
	})
	t.Run("refused by the peer", func(t *testing.T) {
		p, peers := signalProvider(t)
		peers.invokeErr = statusErr{im.StatusNotFound}
		err := p.End(context.Background(), 5, 3)
		var se im.StatusCodeError
		if !errors.As(err, &se) || se.MatterStatusCode() != im.StatusNotFound {
			t.Fatalf("End: %v", err)
		}
		if n := len(p.Sessions()); n != 0 {
			t.Fatalf("%d sessions after a refused End", n)
		}
	})
	t.Run("peer unreachable", func(t *testing.T) {
		p, peers := signalProvider(t)
		peers.connectErr = errors.New("no route")
		if err := p.End(context.Background(), 5, 3); err == nil || !errors.Is(err, peers.connectErr) {
			t.Fatalf("End: %v", err)
		}
		if n := len(p.Sessions()); n != 0 {
			t.Fatalf("%d sessions after an undeliverable End", n)
		}
	})
}

// TestProviderSignalErrors: the connector's error and the peer's status
// come back wrapped, naming the session.
func TestProviderSignalErrors(t *testing.T) {
	p, peers := signalProvider(t)
	peers.connectErr = errors.New("no route")
	if err := p.Answer(context.Background(), 5, "v=0"); !errors.Is(err, peers.connectErr) {
		t.Fatalf("Answer with an unreachable peer: %v", err)
	}
	peers.connectErr = nil
	peers.invokeErr = statusErr{im.StatusUnsupportedAccess}
	err := p.Answer(context.Background(), 5, "v=0")
	var se im.StatusCodeError
	if !errors.As(err, &se) || se.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("Answer refused: %v", err)
	}
}
