// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/im"
)

// The session bounds of a WebRtcTransportProvider (issue #590 item 1,
// decisions 606-609): ProviderConfig.MaxSessions bounds the provider's
// sessions, as the camera app's delegate does (connectedhomeip
// examples/camera-app/linux/src/clusters/webrtc-provider/
// webrtc-provider-manager.cpp at 6170af84, kMaxConcurrentWebRTCSessions
// :39, SolicitOffer :281-293, ProvideOffer :472-477, the End :990-999);
// ProviderConfig.MaxSessionsPerFabric is this module's own bound per
// accessing fabric.

// endOutOfResources is WebRTCEndReasonEnum OutOfResources
// (parity/schema.json, WebRTCEndReasonEnum id 8).
const endOutOfResources = uint8(wrdef.WebRTCEndReasonOutOfResources)

// endCall is one End the provider sent a requestor.
type endCall struct {
	fabric   uint8
	node     uint64
	endpoint uint16
	command  uint32
	req      wrdef.EndRequest
}

// endPeers is a PeerConnector that hands every Invoke to calls; while gate
// is non-nil each Invoke waits for it to be closed.
type endPeers struct {
	calls chan endCall
	gate  chan struct{}
}

func newEndPeers() *endPeers { return &endPeers{calls: make(chan endCall, 64)} }

func (e *endPeers) ConnectPeer(_ context.Context, fabric uint8, node uint64) (camera.PeerSession, error) {
	return endPeerSession{e: e, fabric: fabric, node: node}, nil
}

type endPeerSession struct {
	e      *endPeers
	fabric uint8
	node   uint64
}

func (s endPeerSession) Invoke(_ context.Context, endpoint uint16, _, command uint32, fields spec.Encodable) (*spec.Node, error) {
	if s.e.gate != nil {
		<-s.e.gate
	}
	req, _ := fields.(wrdef.EndRequest)
	s.e.calls <- endCall{fabric: s.fabric, node: s.node, endpoint: endpoint, command: command, req: req}
	return nil, nil
}

// capProvider is a provider with the bounds and an endPeers attached.
func capProvider(t *testing.T, maxSessions, perFabric int) (*camera.Provider, *mockProvider, *endPeers) {
	t.Helper()
	p, m := newProvider(t, cameraStreams(), func(c *camera.ProviderConfig) {
		c.MaxSessions, c.MaxSessionsPerFabric = maxSessions, perFabric
	})
	peers := newEndPeers()
	p.AttachPeerConnector(peers)
	return p, m, peers
}

// newSessionOffer is a ProvideOffer of a new LiveView session from
// endpoint 1.
func newSessionOffer() wpdef.ProvideOfferRequest {
	u, ep := wpdef.StreamUsageLiveView, uint16(1)
	return wpdef.ProvideOfferRequest{WebRtcSessionId: spec.NullOf[uint16](), Sdp: "v=0", StreamUsage: &u, OriginatingEndpointId: &ep, VideoStreamId: nnull()}
}

func provideNew(ctx context.Context, t *testing.T, p *camera.Provider) (uint16, error) {
	t.Helper()
	resp, err := p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newSessionOffer())
	if err != nil {
		return 0, err
	}
	return resp.(wpdef.ProvideOfferResponse).WebRtcSessionId, nil
}

func solicitNew(ctx context.Context, t *testing.T, p *camera.Provider, endpoint uint16) (wpdef.SolicitOfferResponse, error) {
	t.Helper()
	resp, err := p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageLiveView, func(r *wpdef.SolicitOfferRequest) { r.OriginatingEndpointId = endpoint }))
	if err != nil {
		return wpdef.SolicitOfferResponse{}, err
	}
	return resp.(wpdef.SolicitOfferResponse), nil
}

func (m *mockProvider) offerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.offers)
}

func sessionIDs(p *camera.Provider) []uint16 {
	sessions := p.Sessions()
	ids := make([]uint16, 0, len(sessions))
	for _, s := range sessions {
		ids = append(ids, s.Id)
	}
	return ids
}

func sameIDs(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hostSession(id uint16, fabric uint8) camera.ProviderSession {
	return camera.ProviderSession{Id: id, PeerNodeId: 0x99, FabricIndex: fabric, StreamUsage: wpdef.StreamUsageLiveView, VideoStreams: &[]uint16{1}}
}

// TestProviderMaxSessionsProvideOffer (red test 1): n new sessions
// through both commands are accepted; ProvideOffer n+1 is
// RESOURCE_EXHAUSTED before the id and the port — the port is not asked,
// CurrentSessions and the next id stay (webrtc-provider-manager.cpp
// :472-477 counts in the delegate).
func TestProviderMaxSessionsProvideOffer(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	p, m, _ := capProvider(t, 3, 0)
	if _, err := solicitNew(ctx, t, p, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := provideNew(ctx, t, p); err != nil {
		t.Fatal(err)
	}
	if _, err := solicitNew(ctx, t, p, 1); err != nil {
		t.Fatal(err)
	}
	before, offers := sessionIDs(p), m.offerCount()
	_, err := provideNew(ctx, t, p)
	wantCode(t, "ProvideOffer over the bound", statusOf(err), im.StatusResourceExhausted)
	if m.offerCount() != offers {
		t.Fatal("the port was asked for a session over the bound")
	}
	if got := sessionIDs(p); !sameIDs(got, before) {
		t.Fatalf("CurrentSessions %v, want %v", got, before)
	}
	// The refusal took no id: after one session ends, the next is 3.
	if _, err := p.MatterInvoke(ctx, wpdef.CmdEndSession, wpdef.EndSessionRequest{WebRtcSessionId: 0, Reason: 2}); err != nil {
		t.Fatal(err)
	}
	if id, err := provideNew(ctx, t, p); err != nil || id != 3 {
		t.Fatalf("the offer after the refusal = id %d, %v; want id 3", id, err)
	}
}

// TestProviderMaxSessionsSolicitOffer (red test 2): SolicitOffer over the
// bound is answered with a fresh id and no deferred offer; the port is not
// asked, CurrentSessions stays, and the requestor's End with OutOfResources
// reaches the invoker's node and fabric at its OriginatingEndpointID, after
// the response (webrtc-provider-manager.cpp :281-293, :990-999).
func TestProviderMaxSessionsSolicitOffer(t *testing.T) {
	t.Parallel()
	p, m, peers := capProvider(t, 1, 0)
	ctx := peer(2, 0x77)
	if _, err := solicitNew(ctx, t, p, 1); err != nil {
		t.Fatal(err)
	}
	before, offers := sessionIDs(p), m.offerCount()

	after := &im.AfterResponse{}
	resp, err := solicitNew(im.WithAfterResponse(ctx, after), t, p, 4)
	if err != nil {
		t.Fatalf("SolicitOffer over the bound: %v; want a response", err)
	}
	if resp.WebRtcSessionId != 1 || resp.DeferredOffer {
		t.Fatalf("response %+v, want id 1 without a deferred offer", resp)
	}
	if m.offerCount() != offers {
		t.Fatal("the port was asked for a session over the bound")
	}
	if got := sessionIDs(p); !sameIDs(got, before) {
		t.Fatalf("CurrentSessions %v, want %v", got, before)
	}
	if len(peers.calls) != 0 {
		t.Fatal("the End went out before the response")
	}
	after.Run()
	c := <-peers.calls
	if c.fabric != 2 || c.node != 0x77 || c.endpoint != 4 || c.command != wrdef.CmdEnd ||
		c.req.WebRtcSessionId != 1 || c.req.Reason != endOutOfResources {
		t.Fatalf("End %+v", c)
	}
}

// TestProviderMaxSessionsSolicitOfferEndQueue (red test 2): the Ends wait
// in a queue of at most the bound; with the queue full, and without a
// connector, SolicitOffer over the bound is RESOURCE_EXHAUSTED instead.
func TestProviderMaxSessionsSolicitOfferEndQueue(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	p, m, peers := capProvider(t, 1, 0)
	peers.gate = make(chan struct{})
	if _, err := solicitNew(ctx, t, p, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := solicitNew(ctx, t, p, 1); err != nil { // its End waits at the gate
		t.Fatal(err)
	}
	offers := m.offerCount()
	_, err := solicitNew(ctx, t, p, 1)
	wantCode(t, "SolicitOffer with the End queue full", statusOf(err), im.StatusResourceExhausted)
	if m.offerCount() != offers {
		t.Fatal("the port was asked")
	}
	close(peers.gate)
	if c := <-peers.calls; c.req.WebRtcSessionId != 1 {
		t.Fatalf("End %+v", c)
	}

	q, _, _ := capProvider(t, 1, 0)
	q.AttachPeerConnector(nil)
	if _, err := solicitNew(ctx, t, q, 1); err != nil {
		t.Fatal(err)
	}
	_, err = solicitNew(ctx, t, q, 1)
	wantCode(t, "SolicitOffer without a connector", statusOf(err), im.StatusResourceExhausted)
}

// TestProviderMaxSessionsReOffer (red test 3): a re-offer of a held
// session is not a new session and passes at the bound.
func TestProviderMaxSessionsReOffer(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	p, _, _ := capProvider(t, 1, 0)
	id, err := provideNew(ctx, t, p)
	if err != nil {
		t.Fatal(err)
	}
	re := newSessionOffer()
	re.WebRtcSessionId, re.StreamUsage, re.OriginatingEndpointId, re.VideoStreamId = spec.ValueOf(id), nil, nil, nv(1)
	if _, err := p.MatterInvoke(ctx, wpdef.CmdProvideOffer, re); err != nil {
		t.Fatalf("re-offer at the bound: %v", err)
	}
}

// TestProviderMaxSessionsFreedSlot (red test 4): each way a session
// leaves — EndSession, Provider.End, RemoveSession, the fabric's removal —
// lets a new one in.
func TestProviderMaxSessionsFreedSlot(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	ends := map[string]func(p *camera.Provider, id uint16) error{
		"EndSession": func(p *camera.Provider, id uint16) error {
			_, err := p.MatterInvoke(ctx, wpdef.CmdEndSession, wpdef.EndSessionRequest{WebRtcSessionId: id, Reason: 2})
			return err
		},
		"End":              func(p *camera.Provider, id uint16) error { return p.End(context.Background(), id, 2) },
		"RemoveSession":    func(p *camera.Provider, id uint16) error { p.RemoveSession(id); return nil },
		"MatterDropFabric": func(p *camera.Provider, _ uint16) error { return p.MatterDropFabric(context.Background(), 1) },
	}
	for name, end := range ends {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, _, peers := capProvider(t, 1, 0)
			id, err := provideNew(ctx, t, p)
			if err != nil {
				t.Fatal(err)
			}
			_, err = provideNew(ctx, t, p)
			wantCode(t, "at the bound", statusOf(err), im.StatusResourceExhausted)
			if err := end(p, id); err != nil {
				t.Fatal(err)
			}
			if name == "End" {
				<-peers.calls
			}
			if _, err := provideNew(ctx, t, p); err != nil {
				t.Fatalf("an offer after %s: %v", name, err)
			}
		})
	}
}

// TestProviderMaxSessionsInvalidRequestFirst (red test 5): at the bound an
// invalid request answers its own status — the bound is checked after
// every validation, directly before the session id.
func TestProviderMaxSessionsInvalidRequestFirst(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	p, _, peers := capProvider(t, 1, 0)
	if _, err := provideNew(ctx, t, p); err != nil {
		t.Fatal(err)
	}
	bad := newSessionOffer()
	u := wpdef.StreamUsageEnum(9)
	bad.StreamUsage = &u
	_, err := p.MatterInvoke(ctx, wpdef.CmdProvideOffer, bad)
	wantCode(t, "unknown usage", statusOf(err), im.StatusConstraintError)
	bad = newSessionOffer()
	bad.WebRtcSessionId = spec.ValueOf[uint16](7)
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, bad)
	wantCode(t, "unknown session", statusOf(err), im.StatusNotFound)
	bad = newSessionOffer()
	a := wpdef.StreamUsageAnalysis
	bad.StreamUsage = &a
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, bad)
	wantCode(t, "usage not prioritised", statusOf(err), im.StatusDynamicConstraintError)
	_, err = p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageAnalysis))
	wantCode(t, "solicit usage not prioritised", statusOf(err), im.StatusDynamicConstraintError)
	_, err = p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(4))
	wantCode(t, "solicit unknown usage", statusOf(err), im.StatusConstraintError)
	if len(peers.calls) != 0 {
		t.Fatal("a refused request sent an End")
	}
}

// TestProviderMaxSessionsConcurrentOffers (red test 6): two offers at n-1
// from two goroutines — exactly one is accepted. Run with -race -count=20.
func TestProviderMaxSessionsConcurrentOffers(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	p, _, _ := capProvider(t, 2, 0)
	if _, err := provideNew(ctx, t, p); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, errs[i] = provideNew(ctx, t, p) })
	}
	wg.Wait()
	accepted, exhausted := 0, 0
	for _, err := range errs {
		switch statusOf(err) {
		case im.StatusSuccess:
			accepted++
		case im.StatusResourceExhausted:
			exhausted++
		default:
			t.Fatalf("offer: %v", err)
		}
	}
	if accepted != 1 || exhausted != 1 || len(p.Sessions()) != 2 {
		t.Fatalf("accepted %d, exhausted %d, %d sessions; want 1, 1, 2", accepted, exhausted, len(p.Sessions()))
	}
}

// TestProviderMaxSessionsHostRows (red test 7, decision 608): the host's
// rows count against the bound, and UpsertSession at the bound still
// stores.
func TestProviderMaxSessionsHostRows(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	p, _, _ := capProvider(t, 2, 0)
	for id := range uint16(2) {
		if err := p.UpsertSession(hostSession(100+id, 3)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := provideNew(ctx, t, p)
	wantCode(t, "over host rows", statusOf(err), im.StatusResourceExhausted)
	if err := p.UpsertSession(hostSession(200, 3)); err != nil || len(p.Sessions()) != 3 {
		t.Fatalf("UpsertSession at the bound = %v, %d sessions; want stored", err, len(p.Sessions()))
	}
}

// TestProviderMaxSessionsZero (red test 8): no bound is v0.25.0's
// behaviour.
func TestProviderMaxSessionsZero(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	p, _, _ := capProvider(t, 0, 0)
	for range 12 {
		if _, err := provideNew(ctx, t, p); err != nil {
			t.Fatal(err)
		}
	}
}

// TestProviderMaxSessionsPerFabric (decision 607): fabric A at its bound
// is refused while fabric B is accepted; the host's rows of A count for A;
// the whole provider's bound holds over both.
func TestProviderMaxSessionsPerFabric(t *testing.T) {
	t.Parallel()
	a, b := peer(1, 0x55), peer(2, 0x66)
	p, m, peers := capProvider(t, 0, 2)
	if err := p.UpsertSession(hostSession(100, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := provideNew(a, t, p); err != nil {
		t.Fatal(err)
	}
	offers := m.offerCount()
	_, err := provideNew(a, t, p)
	wantCode(t, "fabric A over its bound", statusOf(err), im.StatusResourceExhausted)
	resp, err := solicitNew(a, t, p, 6)
	if err != nil || resp.DeferredOffer {
		t.Fatalf("fabric A's SolicitOffer over its bound = %+v, %v", resp, err)
	}
	if c := <-peers.calls; c.fabric != 1 || c.req.Reason != endOutOfResources || c.req.WebRtcSessionId != resp.WebRtcSessionId {
		t.Fatalf("End %+v", c)
	}
	if m.offerCount() != offers {
		t.Fatal("the port was asked for fabric A's session over its bound")
	}
	for range 2 {
		if _, err := provideNew(b, t, p); err != nil {
			t.Fatalf("fabric B: %v", err)
		}
	}
	_, err = provideNew(b, t, p)
	wantCode(t, "fabric B over its bound", statusOf(err), im.StatusResourceExhausted)

	q, _, _ := capProvider(t, 3, 2)
	for _, ctx := range []context.Context{a, a, b} {
		if _, err := provideNew(ctx, t, q); err != nil {
			t.Fatal(err)
		}
	}
	_, err = provideNew(peer(3, 0x77), t, q)
	wantCode(t, "the provider's bound over the fabric's", statusOf(err), im.StatusResourceExhausted)
}

// TestProviderSessionBoundParityChip (red test 9): the pair the camera
// app's delegate answers at kMaxConcurrentWebRTCSessions (5,
// webrtc-provider-manager.cpp:39 at 6170af84) — ProvideOffer
// RESOURCE_EXHAUSTED (:472-477); SolicitOffer answered, then the
// requestor's End with OutOfResources (:281-293, :990-999); TC-WEBRTCP-2.12
// and 2.16 drive them.
func TestProviderSessionBoundParityChip(t *testing.T) {
	t.Parallel()
	ctx := peer(1, 0x55)
	p, _, peers := capProvider(t, 5, 0)
	for range 5 {
		if _, err := provideNew(ctx, t, p); err != nil {
			t.Fatal(err)
		}
	}
	_, err := provideNew(ctx, t, p)
	wantCode(t, "a sixth ProvideOffer", statusOf(err), im.StatusResourceExhausted)
	resp, err := solicitNew(ctx, t, p, 1)
	if err != nil {
		t.Fatalf("a sixth SolicitOffer: %v", err)
	}
	if c := <-peers.calls; c.command != wrdef.CmdEnd || c.req.WebRtcSessionId != resp.WebRtcSessionId || c.req.Reason != endOutOfResources {
		t.Fatalf("End %+v", c)
	}
	if len(p.Sessions()) != 5 {
		t.Fatalf("%d sessions, want 5", len(p.Sessions()))
	}
}
