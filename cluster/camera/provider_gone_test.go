// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
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
	if ran, err := p.PeerGone(context.Background(), 1, 7, unknownReason, nil); err != nil || !ran {
		t.Fatalf("PeerGone = %v, %v; want ran", ran, err)
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
	if ran, err := p.PeerGone(context.Background(), 1, 7, unknownReason, nil); err != nil || !ran {
		t.Fatalf("PeerGone = %v, %v; want ran", ran, err)
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
	_, err := p.PeerGone(context.Background(), 1, 7, unknownReason, nil)
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
	go func() {
		_, err := p.PeerGone(context.Background(), 1, 0x55, unknownReason, nil)
		gone <- err
	}()
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

// TestProviderPeerGoneCheckFalseEndsNothing (owner decision 632): a check
// that no longer holds ends nothing — no port call, every session stays —
// and PeerGone reports it did not run.
func TestProviderPeerGoneCheckFalseEndsNothing(t *testing.T) {
	t.Parallel()
	p, m := newProvider(t, nil)
	goneSessions(t, p)
	asked := 0
	ran, err := p.PeerGone(context.Background(), 1, 7, unknownReason, func() bool { asked++; return false })
	if err != nil || ran {
		t.Fatalf("PeerGone = %v, %v; want not ran, no error", ran, err)
	}
	if asked != 1 {
		t.Errorf("check asked %d times, want once", asked)
	}
	if got := sessionIDs(p); !slices.Equal(got, []uint16{1, 2, 3, 4}) {
		t.Errorf("sessions %v, want [1 2 3 4]", got)
	}
	m.mu.Lock()
	ended := len(m.ended)
	m.mu.Unlock()
	if ended != 0 {
		t.Errorf("port ended %d sessions, want 0", ended)
	}
}

// TestProviderPeerGoneChecksUnderTheSerialisation (owner decision 632):
// the check is asked only once the command in flight returned — under the
// provider's serialisation, not before the wait — so evidence that
// changed during the wait decides.
func TestProviderPeerGoneChecksUnderTheSerialisation(t *testing.T) {
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
	var duringAnswer atomic.Bool
	type result struct {
		ran bool
		err error
	}
	gone := make(chan result, 1)
	header := make(chan string, 1)
	go func() {
		header <- goroutineHeader()
		ran, err := p.PeerGone(context.Background(), 1, 0x55, unknownReason, func() bool {
			duringAnswer.Store(port.answering.Load())
			return false
		})
		gone <- result{ran, err}
	}()
	waitGoroutineLocksIn(t, <-header, "camera.(*Provider).PeerGone")
	close(port.release)
	if err := <-answered; statusOf(err) != im.StatusSuccess {
		t.Fatalf("answer: %v", err)
	}
	if r := <-gone; r.err != nil || r.ran {
		t.Fatalf("PeerGone = %v, %v; want not ran", r.ran, r.err)
	}
	if duringAnswer.Load() {
		t.Error("the check ran while the port's ProvideAnswer was in flight")
	}
	if got := sessionIDs(p); !slices.Equal(got, []uint16{id}) {
		t.Errorf("sessions %v, want [%d]", got, id)
	}
}

// goroutineHeader is the calling goroutine's stack header, "goroutine N ".
func goroutineHeader() string {
	buf := make([]byte, 64)
	buf = buf[:runtime.Stack(buf, false)]
	head, _, _ := strings.Cut(string(buf), "[")
	return head
}

// waitGoroutineLocksIn yields until the goroutine named by header waits
// for a mutex inside fn, read from the goroutines' stacks: no wall-clock
// wait, and a parallel test's goroutine parked in fn does not count.
func waitGoroutineLocksIn(t *testing.T, header, fn string) {
	t.Helper()
	buf := make([]byte, 1<<20)
	for range 1_000_000 {
		stacks := string(buf[:runtime.Stack(buf, true)])
		for _, g := range strings.Split(stacks, "\n\n") {
			if strings.HasPrefix(g, header+"[") && strings.Contains(g, "sync.(*Mutex).Lock") && strings.Contains(g, fn) {
				return
			}
		}
		runtime.Gosched()
	}
	t.Fatalf("%sdoes not wait for a lock in %s", header, fn)
}
