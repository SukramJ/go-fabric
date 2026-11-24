// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/im"
)

// unknownReason is WebRTCEndReasonEnum UnknownReason, the reason the node
// ends a lost peer's sessions with.
const unknownReason uint8 = 12

// goneSessions is the table of the PeerGone tests: peer A (7) holds two
// sessions on fabric 1 and one on fabric 2, peer B (8) one on fabric 1.
func goneSessions(t *testing.T, p *camera.Provider) {
	t.Helper()
	video := []uint16{1}
	for _, s := range []camera.ProviderSession{
		{Id: 1, PeerNodeId: 7, FabricIndex: 1, VideoStreams: &video},
		{Id: 2, PeerNodeId: 8, FabricIndex: 1, VideoStreams: &video},
		{Id: 3, PeerNodeId: 7, FabricIndex: 1, VideoStreams: &video},
		{Id: 4, PeerNodeId: 7, FabricIndex: 2, VideoStreams: &video},
	} {
		if err := p.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
}

// TestProviderPeerGoneEndsThePeersSessions (#590 item 2, owner decision
// 612): the port's EndSession runs once for each session of the pair,
// they leave CurrentSessions; another peer's on the fabric and the same
// node id's on another fabric stay. No End command reaches the requestor:
// the peer is not reachable.
func TestProviderPeerGoneEndsThePeersSessions(t *testing.T) {
	t.Parallel()
	p, m := newProvider(t, nil)
	peers := &fakePeers{}
	p.AttachPeerConnector(peers)
	goneSessions(t, p)
	if err := p.PeerGone(context.Background(), 1, 7, unknownReason); err != nil {
		t.Fatal(err)
	}
	if got := sessionIDs(p); !slices.Equal(got, []uint16{2, 4}) {
		t.Errorf("sessions after PeerGone %v, want [2 4]", got)
	}
	m.mu.Lock()
	ended := slices.Clone(m.ended)
	m.mu.Unlock()
	if !slices.Equal(ended, []uint16{1, 3}) {
		t.Errorf("port ended %v, want [1 3]", ended)
	}
	peers.mu.Lock()
	calls := len(peers.calls)
	peers.mu.Unlock()
	if calls != 0 {
		t.Errorf("the requestor was sent %d commands, want 0: no End to a lost peer", calls)
	}
	// A second report of the same loss ends nothing more.
	if err := p.PeerGone(context.Background(), 1, 7, unknownReason); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	again := len(m.ended)
	m.mu.Unlock()
	if again != 2 {
		t.Errorf("port ended %d sessions after the second PeerGone, want 2", again)
	}
}

// TestProviderPeerGoneRemovesWhateverThePortAnswers: the port's error is
// returned and the session leaves CurrentSessions anyway, as
// MatterDropFabric.
func TestProviderPeerGoneRemovesWhateverThePortAnswers(t *testing.T) {
	t.Parallel()
	p, m := newProvider(t, nil)
	goneSessions(t, p)
	m.mu.Lock()
	m.err = errors.New("stack gone")
	m.mu.Unlock()
	err := p.PeerGone(context.Background(), 1, 7, unknownReason)
	if err == nil || !errors.Is(err, m.err) {
		t.Fatalf("PeerGone = %v, want the port's error", err)
	}
	if got := sessionIDs(p); !slices.Equal(got, []uint16{2, 4}) {
		t.Errorf("sessions after PeerGone %v, want [2 4]", got)
	}
}

// blockingAnswerPort holds ProvideAnswer until release closes, and
// records an EndSession that runs while it holds.
type blockingAnswerPort struct {
	*mockProvider
	entered   chan struct{}
	release   chan struct{}
	answering atomic.Bool
	overlap   atomic.Bool
}

func (b *blockingAnswerPort) ProvideAnswer(ctx context.Context, id uint16, sdp string) error {
	b.answering.Store(true)
	close(b.entered)
	<-b.release
	b.answering.Store(false)
	return b.mockProvider.ProvideAnswer(ctx, id, sdp)
}

func (b *blockingAnswerPort) EndSession(ctx context.Context, id uint16, reason uint8) error {
	if b.answering.Load() {
		b.overlap.Store(true)
	}
	return b.mockProvider.EndSession(ctx, id, reason)
}

// TestProviderPeerGoneWaitsForACommandInFlight: PeerGone takes the
// provider's op behind a running handler and its port call — no end
// overlaps the port's answer — and returns once the handler did: no
// deadlock.
func TestProviderPeerGoneWaitsForACommandInFlight(t *testing.T) {
	t.Parallel()
	port := &blockingAnswerPort{mockProvider: &mockProvider{}, entered: make(chan struct{}), release: make(chan struct{})}
	p, err := camera.NewProvider(camera.ProviderConfig{Port: port, Streams: cameraStreams()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := peer(1, 0x55)
	resp, err := p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageLiveView))
	if statusOf(err) != im.StatusSuccess {
		t.Fatalf("solicit: %v", err)
	}
	id := resp.(wpdef.SolicitOfferResponse).WebRtcSessionId
	answered := make(chan error, 1)
	go func() {
		_, err := p.MatterInvoke(ctx, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: id, Sdp: "a"})
		answered <- err
	}()
	<-port.entered
	gone := make(chan error, 1)
	go func() { gone <- p.PeerGone(context.Background(), 1, 0x55, unknownReason) }()
	for range 1000 {
		runtime.Gosched()
	}
	select {
	case err := <-gone:
		t.Fatalf("PeerGone returned (%v) while a handler held the provider", err)
	default:
	}
	close(port.release)
	if err := <-answered; statusOf(err) != im.StatusSuccess {
		t.Fatalf("answer: %v", err)
	}
	if err := <-gone; err != nil {
		t.Fatal(err)
	}
	if port.overlap.Load() {
		t.Error("EndSession ran while the port's ProvideAnswer was in flight")
	}
	if got := sessionIDs(p); len(got) != 0 {
		t.Errorf("sessions after PeerGone %v, want none", got)
	}
}
