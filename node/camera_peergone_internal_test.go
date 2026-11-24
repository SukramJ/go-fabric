// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// #590 item 2 (owner decisions 611, 612): a camera's WebRTC sessions of a
// requestor end when the node has proven the requestor lost — an exchange
// it opened went unanswered — and holds no session with it in either
// stack; every other close (the idle reaper's cause, a TCP eviction, the
// peer's CloseSession) ends nothing.

import (
	"context"
	"net"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/cluster/camera"
	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/sigma"
)

const (
	pgFabric uint8  = 1
	pgLocal  uint64 = 0x1111
	pgPeerA  uint64 = 0xA0A0
	pgPeerB  uint64 = 0xB0B0
)

// pgPort is the camera's ProviderPort: it records EndSession and can hold
// ProvideAnswer until release closes.
type pgPort struct {
	mu      sync.Mutex
	ended   []uint16
	hold    bool
	entered chan struct{}
	release chan struct{}
}

func (p *pgPort) SolicitOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return pgSession(a), false, nil
}

func (p *pgPort) ProvideOffer(_ context.Context, a wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return pgSession(a), nil
}

func (p *pgPort) ProvideAnswer(context.Context, uint16, string) error {
	if p.hold {
		close(p.entered)
		<-p.release
	}
	return nil
}

func (p *pgPort) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}

func (p *pgPort) EndSession(_ context.Context, id uint16, _ uint8) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ended = append(p.ended, id)
	return nil
}

func (p *pgPort) endedIDs() []uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.ended)
}

func pgSession(a wpdef.OfferArgs) wpdef.WebRTCSessionStruct {
	return wpdef.WebRTCSessionStruct{
		Id: a.SessionID, PeerNodeId: a.PeerNodeID, PeerEndpointId: a.OriginatingEndpointID,
		StreamUsage: a.StreamUsage, FabricIndex: a.FabricIndex, VideoStreams: a.VideoStreams,
	}
}

// pgDone is one pair the camera queue worked off.
type pgDone struct {
	addr      exchange.PeerAddress
	ended     int
	cancelled bool
}

// pgNode is a started node serving one camera whose provider holds two
// sessions of peer A and one of peer B on fabric 1.
type pgNode struct {
	n    *Node
	port *pgPort
	prov *camera.Provider
	done chan pgDone
	next uint16
}

func newPGNode(t *testing.T) *pgNode {
	t.Helper()
	ctx := t.Context()
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	cfg.DeviceTypeValidation = endpoint.DeviceTypeValidationOff
	n := newMDNSTestNode(t, cfg)
	g := &pgNode{n: n, port: &pgPort{}, done: make(chan pgDone, 16), next: 0x0100}
	n.sec.peerGone.testDone = func(addr exchange.PeerAddress, ended int, cancelled bool) {
		g.done <- pgDone{addr, ended, cancelled}
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cam, err := device.NewCamera(device.Options{StableKey: endpoint.StringKey("cam:1"), Name: "cam"}, wpdef.Declare{ProviderPort: g.port}, csdef.Declare{Initial: csdef.Patch{
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
	if err := n.Aggregator().Add(ctx, cam); err != nil {
		t.Fatal(err)
	}
	var ok bool
	if g.prov, ok = cam.Server(wpdef.ClusterID).(*camera.Provider); !ok {
		t.Fatalf("the camera's WebRtcTransportProvider is %T", cam.Server(wpdef.ClusterID))
	}
	video := []uint16{1}
	for _, s := range []camera.ProviderSession{
		{Id: 1, PeerNodeId: pgPeerA, FabricIndex: pgFabric, StreamUsage: wpdef.StreamUsageLiveView, VideoStreams: &video},
		{Id: 2, PeerNodeId: pgPeerB, FabricIndex: pgFabric, StreamUsage: wpdef.StreamUsageLiveView, VideoStreams: &video},
		{Id: 3, PeerNodeId: pgPeerA, FabricIndex: pgFabric, StreamUsage: wpdef.StreamUsageLiveView, VideoStreams: &video},
	} {
		if err := g.prov.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// openBridge opens a CASE session with peer in the bridge's session table.
func (g *pgNode) openBridge(t *testing.T, peer uint64) uint16 {
	t.Helper()
	g.next++
	var keys sigma.SessionKeys
	for i := range keys.I2RKey {
		keys.I2RKey[i], keys.R2IKey[i] = byte(g.next)+byte(i), byte(g.next)+byte(i)+16
	}
	stake, err := g.n.sec.sessions.AllocateStake()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.n.sec.sessions.OpenFromSigmaWithStake(stake, pgFabric, pgLocal, peer, g.next, nil, keys); err != nil {
		t.Fatalf("open session %#x: %v", stake.ID, err)
	}
	return stake.ID
}

// openClient registers a secure session with peer in the peer path's
// client stack.
func (g *pgNode) openClient(t *testing.T, peer uint64) *exchange.Session {
	t.Helper()
	cs, err := g.n.peerStack()
	if err != nil {
		t.Fatal(err)
	}
	id, err := cs.exchanges.AllocateSessionID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 16)
	s, err := cs.exchanges.NewSecureSession(exchange.SecureSessionConfig{
		ID: id, Addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, FabricIndex: pgFabric,
		Channel: channel.Config{EncryptKey: key, DecryptKey: key, LocalNodeID: pgLocal, PeerNodeID: peer, PeerSessionID: id},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// lose is the bridge's peer loss of peer: every session registered so far
// force-closed, as closeLostPeer does on an unanswered exchange.
func (g *pgNode) lose(peer uint64) {
	g.n.sec.sessions.ClosePeerLostBefore(pgFabric, peer, g.n.sec.sessions.RegistrationCount())
}

// drain queues a sentinel pair the camera holds no session of and
// returns every pair the queue worked off before it: the queue works in
// order, and a trigger queues synchronously, so what a trigger queued is
// in the result and what it did not queue is not.
func (g *pgNode) drain(t *testing.T) []pgDone {
	t.Helper()
	g.n.cameraPeerLost(pgSentinel)
	var out []pgDone
	for {
		d := <-g.done
		if d.addr == pgSentinel {
			return out
		}
		out = append(out, d)
	}
}

func (g *pgNode) sessions() []uint16 {
	sessions := g.prov.Sessions()
	ids := make([]uint16, 0, len(sessions))
	for _, s := range sessions {
		ids = append(ids, s.Id)
	}
	return ids
}

var (
	pgAddrA = exchange.PeerAddress{FabricIndex: pgFabric, NodeID: pgPeerA}
	pgAddrB = exchange.PeerAddress{FabricIndex: pgFabric, NodeID: pgPeerB}
	// pgSentinel is a peer the camera holds no session of ([pgNode.drain]).
	pgSentinel = exchange.PeerAddress{FabricIndex: pgFabric, NodeID: 0xC0C0}
)

// TestCameraPeerLossEndsTheLostPeersSessions: the node loses A (an
// exchange it opened unanswered) and holds no session with A in either
// stack: the port's EndSession runs once for each of A's sessions, they
// leave CurrentSessions, B's stays.
func TestCameraPeerLossEndsTheLostPeersSessions(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.openBridge(t, pgPeerA)
	g.openBridge(t, pgPeerA)
	g.openBridge(t, pgPeerB)
	g.lose(pgPeerA)
	if got := g.drain(t); !slices.Equal(got, []pgDone{{pgAddrA, 1, false}}) {
		t.Fatalf("worked off %+v, want A ended on one provider", got)
	}
	if got := g.port.endedIDs(); !slices.Equal(got, []uint16{1, 3}) {
		t.Errorf("port ended %v, want [1 3]", got)
	}
	if got := g.sessions(); !slices.Equal(got, []uint16{2}) {
		t.Errorf("sessions %v, want [2]", got)
	}
}

// TestCameraCloseThatIsNoLossEndsNothing: a session closed without proof
// of loss — Manager.Close (the peer's CloseSession, and a TCP eviction:
// bridge evictSession → the registry's Close) and the idle
// reaper's cause (PeerGoneClosed, pinned for the reaper in
// secure/operational TestPeerGoneHookCarriesTheCause) — ends nothing,
// although no session with the peer is left. B's loss queued afterwards
// is the first pair worked off.
func TestCameraCloseThatIsNoLossEndsNothing(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	if err := g.n.sec.sessions.Close(g.openBridge(t, pgPeerA)); err != nil {
		t.Fatal(err)
	}
	g.n.bridgePeerGone(pgFabric, pgPeerA, operational.PeerGoneClosed)
	g.openBridge(t, pgPeerB)
	g.lose(pgPeerB)
	if got := g.drain(t); !slices.Equal(got, []pgDone{{pgAddrB, 1, false}}) {
		t.Fatalf("worked off %+v, want B alone: no close of A is evidence", got)
	}
	if got := g.port.endedIDs(); !slices.Equal(got, []uint16{2}) {
		t.Errorf("port ended %v, want [2]", got)
	}
}

// TestCameraLiveSessionKeepsTheLostPeers: A's loss is worked off while a
// session with A is live — a second CASE session in the bridge's table,
// or the client stack's session — and nothing ends. The client stack's
// own close of its session is no loss either; its loss is, and with no
// session left A's sessions end.
func TestCameraLiveSessionKeepsTheLostPeers(t *testing.T) {
	t.Parallel()
	t.Run("a second CASE session", func(t *testing.T) {
		t.Parallel()
		g := newPGNode(t)
		g.openBridge(t, pgPeerA)
		g.n.cameraPeerLost(pgAddrA)
		if got := g.drain(t); !slices.Equal(got, []pgDone{{pgAddrA, 0, true}}) {
			t.Fatalf("worked off %+v, want A cancelled", got)
		}
		if got := g.port.endedIDs(); len(got) != 0 {
			t.Errorf("port ended %v, want none", got)
		}
	})
	t.Run("the client stack's session", func(t *testing.T) {
		t.Parallel()
		g := newPGNode(t)
		s := g.openClient(t, pgPeerA)
		g.openBridge(t, pgPeerA)
		g.lose(pgPeerA)
		if got := g.drain(t); !slices.Equal(got, []pgDone{{pgAddrA, 0, true}}) {
			t.Fatalf("worked off %+v, want A cancelled by the client session", got)
		}
		s.Close()
		lost := g.openClient(t, pgPeerA)
		lost.HandlePeerLoss(&exchange.PeerInitiatedCloseError{})
		if got := g.drain(t); len(got) != 0 {
			t.Fatalf("worked off %+v, want nothing: the client stack's close and the peer's CloseSession are no loss", got)
		}
		lost = g.openClient(t, pgPeerA)
		lost.HandlePeerLoss(&exchange.PeerUnresponsiveError{})
		if got := g.drain(t); !slices.Equal(got, []pgDone{{pgAddrA, 1, false}}) {
			t.Fatalf("worked off %+v, want A ended: the client stack lost it", got)
		}
		if got := g.port.endedIDs(); !slices.Equal(got, []uint16{1, 3}) {
			t.Errorf("port ended %v, want [1 3]", got)
		}
	})
}

// TestCameraNewSessionDuringDispatchCancels: a CASE session A opens after
// the loss was queued and before the provider's check cancels the end —
// the pair is checked again immediately before each provider's PeerGone.
func TestCameraNewSessionDuringDispatchCancels(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.openBridge(t, pgPeerA)
	g.n.sec.peerGone.testBeforeEnd = func(addr exchange.PeerAddress) {
		if addr == pgAddrA {
			g.openBridge(t, pgPeerA)
		}
	}
	g.lose(pgPeerA)
	if got := g.drain(t); !slices.Equal(got, []pgDone{{pgAddrA, 0, true}}) {
		t.Fatalf("worked off %+v, want A cancelled by the new session", got)
	}
	if got := g.port.endedIDs(); len(got) != 0 {
		t.Errorf("port ended %v, want none", got)
	}
}

// TestCameraPeerLossWaitsForACommandInFlight: a command holds the
// provider (its port call in flight) when A is lost. The session table's
// close returns and the table stays usable; the end waits for the
// command and then runs.
func TestCameraPeerLossWaitsForACommandInFlight(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.port.hold, g.port.entered, g.port.release = true, make(chan struct{}), make(chan struct{})
	answered := make(chan error, 1)
	go func() {
		ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, pgFabric), pgPeerB, nil)
		_, err := g.prov.MatterInvoke(ctx, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: 2, Sdp: "a"})
		answered <- err
	}()
	<-g.port.entered
	g.openBridge(t, pgPeerA)
	g.lose(pgPeerA)
	// The table is not held by the waiting end.
	g.openBridge(t, pgPeerB)
	if got := g.n.sec.sessions.SessionIDsForPeer(pgFabric, pgPeerB); len(got) != 1 {
		t.Fatalf("B's sessions %v, want one", got)
	}
	select {
	case d := <-g.done:
		t.Fatalf("worked off %+v while the command held the provider", d)
	default:
	}
	close(g.port.release)
	if err := <-answered; err != nil {
		t.Fatalf("answer: %v", err)
	}
	if got := g.drain(t); !slices.Equal(got, []pgDone{{pgAddrA, 1, false}}) {
		t.Fatalf("worked off %+v, want A ended", got)
	}
	if got := g.port.endedIDs(); !slices.Equal(got, []uint16{1, 3}) {
		t.Errorf("port ended %v, want [1 3]", got)
	}
}

// TestCameraNewSessionWhilePeerGoneWaitsCancels (fable-diff-h13 N1, owner
// decision 632): a command holds the provider when A is lost; the queue
// passes the provider's turn and waits on the provider's serialisation;
// A establishes a new CASE session during that wait. The check of "no
// session in either stack" runs under the serialisation, after the
// command, so it sees A's new session and A's WebRTC sessions stay.
func TestCameraNewSessionWhilePeerGoneWaitsCancels(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.port.hold, g.port.entered, g.port.release = true, make(chan struct{}), make(chan struct{})
	answered := make(chan error, 1)
	go func() {
		ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, pgFabric), pgPeerB, nil)
		_, err := g.prov.MatterInvoke(ctx, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: 2, Sdp: "a"})
		answered <- err
	}()
	<-g.port.entered
	queue := make(chan string, 1)
	g.n.sec.peerGone.testBeforeEnd = func(addr exchange.PeerAddress) {
		if addr == pgAddrA {
			queue <- goroutineHeader()
		}
	}
	g.openBridge(t, pgPeerA)
	g.lose(pgPeerA)
	waitGoroutineBlockedIn(t, <-queue, "camera.(*Provider).PeerGone")
	g.openBridge(t, pgPeerA)
	close(g.port.release)
	if err := <-answered; err != nil {
		t.Fatalf("answer: %v", err)
	}
	if got := g.drain(t); !slices.Equal(got, []pgDone{{pgAddrA, 0, true}}) {
		t.Fatalf("worked off %+v, want A cancelled by the session opened during the wait", got)
	}
	if got := g.port.endedIDs(); len(got) != 0 {
		t.Errorf("port ended %v, want none", got)
	}
	if got := g.sessions(); !slices.Equal(got, []uint16{1, 2, 3}) {
		t.Errorf("sessions %v, want [1 2 3]", got)
	}
}

// goroutineHeader is the calling goroutine's stack header, "goroutine N ".
func goroutineHeader() string {
	buf := make([]byte, 64)
	buf = buf[:runtime.Stack(buf, false)]
	head, _, _ := strings.Cut(string(buf), "[")
	return head
}

// waitGoroutineBlockedIn yields until the goroutine named by header
// (from [goroutineHeader]) waits for a mutex inside fn, read from the
// goroutines' stacks as [waitBlockedIn] does, but for that one goroutine:
// a parallel test's goroutine parked in the same function does not count.
func waitGoroutineBlockedIn(t *testing.T, header, fn string) {
	t.Helper()
	buf := make([]byte, 1<<20)
	for range 1_000_000 {
		stacks := string(buf[:runtime.Stack(buf, true)])
		for _, g := range strings.Split(stacks, "\n\n") {
			if strings.HasPrefix(g, header+"[") && (strings.Contains(g, "sync.(*Mutex).Lock") || strings.Contains(g, "camera.(*opLock).")) && strings.Contains(g, fn) {
				return
			}
		}
		runtime.Gosched()
	}
	t.Fatalf("%sdoes not wait for a lock in %s", header, fn)
}

// TestCameraFabricRemovalEndsOnce: the sessions of a removed fabric end
// through MatterDropFabric, once each; the fabric's session teardown is
// no peer loss and queues nothing.
func TestCameraFabricRemovalEndsOnce(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.openBridge(t, pgPeerA)
	g.n.sec.sessions.CloseFabric(pgFabric)
	g.n.dropFabricState(t.Context(), pgFabric)
	if got := g.port.endedIDs(); !slices.Equal(got, []uint16{1, 2, 3}) {
		t.Errorf("port ended %v, want [1 2 3] once each", got)
	}
	if got := g.drain(t); len(got) != 0 {
		t.Fatalf("worked off %+v, want nothing: a fabric's removal is no loss", got)
	}
	if got := g.port.endedIDs(); len(got) != 3 {
		t.Errorf("port ended %v after the removal, want nothing more", got)
	}
}
