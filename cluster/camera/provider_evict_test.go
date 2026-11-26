// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/im"
)

// The eviction at the session bounds (issue #634, owner decisions
// 702-711): with ProviderConfig.EvictIdle set, a new session at a bound
// takes the place of the accessing fabric's entry whose accepted
// signalling is oldest, once that is EvictIdle old — the port's
// EndSession and the requestor's End, both OutOfResources; with
// EvictAcrossFabrics also set, at MaxSessions a fabric under its fair
// share takes a borrowed entry of a fabric over it. Neither matter.js
// (the empty WebRtcTransportProviderServer) nor connectedhomeip's camera
// app evicts, so no reference test is ported; the idle time runs on a
// fake clock.

const evictIdle = time.Minute

// portEnd is one EndSession the port was asked for.
type portEnd struct {
	id     uint16
	reason uint8
}

// evictPort is the mockProvider recording EndSession's reason, failing it
// with endErr.
type evictPort struct {
	*mockProvider
	endMu  sync.Mutex
	ends   []portEnd
	endErr error
}

func (e *evictPort) EndSession(_ context.Context, id uint16, reason uint8) error {
	e.endMu.Lock()
	defer e.endMu.Unlock()
	e.ends = append(e.ends, portEnd{id, reason})
	return e.endErr
}

func (e *evictPort) ended() []portEnd {
	e.endMu.Lock()
	defer e.endMu.Unlock()
	return slices.Clone(e.ends)
}

// evictRig is a provider with eviction, its port, its requestors and its
// clock.
type evictRig struct {
	p     *camera.Provider
	port  *evictPort
	peers *endPeers
	clk   *clocktest.Fake
}

func newEvictRig(t *testing.T, edit func(*camera.ProviderConfig)) *evictRig {
	t.Helper()
	port := &evictPort{mockProvider: &mockProvider{}}
	cfg := camera.ProviderConfig{Port: port, Streams: cameraStreams(), EvictIdle: evictIdle, Logger: slog.New(slog.DiscardHandler)}
	edit(&cfg)
	p, err := camera.NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	clk := clocktest.New(time.Unix(1_000_000, 0))
	camera.SetProviderClock(p, clk)
	peers := newEndPeers()
	p.AttachPeerConnector(peers)
	return &evictRig{p: p, port: port, peers: peers, clk: clk}
}

func bounds(total, perFabric int) func(*camera.ProviderConfig) {
	return func(c *camera.ProviderConfig) { c.MaxSessions, c.MaxSessionsPerFabric = total, perFabric }
}

func across(total, perFabric int) func(*camera.ProviderConfig) {
	return func(c *camera.ProviderConfig) {
		c.MaxSessions, c.MaxSessionsPerFabric, c.EvictAcrossFabrics = total, perFabric, true
	}
}

// open is a new session of ctx's requestor, then a second on the clock.
func (r *evictRig) open(t *testing.T, ctx context.Context) uint16 {
	t.Helper()
	id, err := provideNew(ctx, t, r.p)
	if err != nil {
		t.Fatalf("ProvideOffer: %v", err)
	}
	r.clk.Advance(time.Second)
	return id
}

// nextEnd is the next End a requestor was sent.
func (r *evictRig) nextEnd(t *testing.T) endCall {
	t.Helper()
	for c := range r.peers.calls {
		if c.command == wrdef.CmdEnd {
			return c
		}
	}
	t.Fatal("the End channel closed")
	return endCall{}
}

func fabricsOf(p *camera.Provider) map[uint8]int {
	held := map[uint8]int{}
	for _, s := range p.Sessions() {
		held[s.FabricIndex]++
	}
	return held
}

func wantEnded(t *testing.T, port *evictPort, ids ...uint16) {
	t.Helper()
	want := make([]portEnd, len(ids))
	for i, id := range ids {
		want[i] = portEnd{id, endOutOfResources}
	}
	if got := port.ended(); !slices.Equal(got, want) {
		t.Fatalf("the port's EndSession %v, want %v", got, want)
	}
}

// TestProviderEvictOwnIdleSession (issue test 1): at the fabric's bound
// with an entry idle for EvictIdle, ProvideOffer succeeds; the oldest
// entry leaves CurrentSessions, the port ends it and its requestor is
// sent End, both with OutOfResources.
func TestProviderEvictOwnIdleSession(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	r := newEvictRig(t, bounds(0, 2))
	first, second := r.open(t, a), r.open(t, a)
	r.clk.Advance(evictIdle)
	id, err := provideNew(a, t, r.p)
	if err != nil {
		t.Fatalf("ProvideOffer at the bound with an idle session: %v", err)
	}
	if got := sessionIDs(r.p); !sameIDs(got, []uint16{second, id}) {
		t.Fatalf("CurrentSessions %v, want [%d %d]", got, second, id)
	}
	wantEnded(t, r.port, first)
	if c := r.nextEnd(t); c.fabric != 1 || c.node != 0x55 || c.endpoint != 1 || c.req.WebRtcSessionId != first || c.req.Reason != endOutOfResources {
		t.Fatalf("End %+v", c)
	}
}

// TestProviderEvictNoneIdleLongEnough (issue test 2): at the bound with
// no entry idle for EvictIdle, ProvideOffer is RESOURCE_EXHAUSTED as
// without eviction, and nothing is ended.
func TestProviderEvictNoneIdleLongEnough(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	r := newEvictRig(t, bounds(0, 2))
	r.open(t, a)
	r.open(t, a)
	r.clk.Advance(evictIdle - 3*time.Second) // the oldest is EvictIdle less a second
	before, offers := sessionIDs(r.p), r.port.offerCount()
	_, err := provideNew(a, t, r.p)
	wantCode(t, "at the bound, nothing idle", statusOf(err), im.StatusResourceExhausted)
	if !sameIDs(sessionIDs(r.p), before) || r.port.offerCount() != offers || len(r.port.ended()) != 0 {
		t.Fatal("a refused offer changed the table, asked the port or ended a session")
	}
	r.clk.Advance(time.Second) // exactly EvictIdle
	if _, err := provideNew(a, t, r.p); err != nil {
		t.Fatalf("at EvictIdle: %v", err)
	}
}

// TestProviderEvictNeverAnotherFabric (issue test 3): another fabric's
// idle session is never taken — not when it is older than the accessing
// fabric's, and not at the provider's bound when the accessing fabric
// holds none.
func TestProviderEvictNeverAnotherFabric(t *testing.T) {
	t.Parallel()
	a, b := peer(1, 0x55), peer(2, 0x66)
	r := newEvictRig(t, bounds(0, 2))
	b0, b1 := r.open(t, b), r.open(t, b)
	a0, a1 := r.open(t, a), r.open(t, a)
	r.clk.Advance(evictIdle)
	if _, err := provideNew(a, t, r.p); err != nil {
		t.Fatal(err)
	}
	wantEnded(t, r.port, a0)
	if held := fabricsOf(r.p); held[2] != 2 || !slices.Contains(sessionIDs(r.p), b0) || !slices.Contains(sessionIDs(r.p), b1) || !slices.Contains(sessionIDs(r.p), a1) {
		t.Fatalf("CurrentSessions %v", sessionIDs(r.p))
	}

	q := newEvictRig(t, bounds(2, 0))
	q.open(t, a)
	q.open(t, a)
	q.clk.Advance(evictIdle)
	_, err := provideNew(b, t, q.p)
	wantCode(t, "fabric B holding none at the provider's bound", statusOf(err), im.StatusResourceExhausted)
	if len(q.port.ended()) != 0 {
		t.Fatal("fabric B's offer ended fabric A's session")
	}
}

// TestProviderEvictSolicitOffer (issue test 4): SolicitOffer at the bound
// follows the same rule — with a victim it opens its session through the
// port; without one it is answered with an id and End, as before.
func TestProviderEvictSolicitOffer(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	r := newEvictRig(t, bounds(0, 1))
	old, err := solicitNew(a, t, r.p, 1)
	if err != nil {
		t.Fatal(err)
	}
	r.clk.Advance(30 * time.Second)
	offers := r.port.offerCount()
	refused, err := solicitNew(a, t, r.p, 3)
	if err != nil || r.port.offerCount() != offers {
		t.Fatalf("SolicitOffer at the bound, nothing idle = %+v, %v; want answered without the port", refused, err)
	}
	if c := r.nextEnd(t); c.req.WebRtcSessionId != refused.WebRtcSessionId || c.endpoint != 3 || c.req.Reason != endOutOfResources {
		t.Fatalf("End of the refused SolicitOffer %+v", c)
	}
	r.clk.Advance(30 * time.Second)
	resp, err := solicitNew(a, t, r.p, 1)
	if err != nil || r.port.offerCount() != offers+1 {
		t.Fatalf("SolicitOffer at the bound with an idle session = %+v, %v; want through the port", resp, err)
	}
	if got := sessionIDs(r.p); !sameIDs(got, []uint16{resp.WebRtcSessionId}) {
		t.Fatalf("CurrentSessions %v, want [%d]", got, resp.WebRtcSessionId)
	}
	wantEnded(t, r.port, old.WebRtcSessionId)
	if c := r.nextEnd(t); c.req.WebRtcSessionId != old.WebRtcSessionId || c.req.Reason != endOutOfResources {
		t.Fatalf("End of the evicted session %+v", c)
	}
}

// evictTrace is everything a command sequence shows from outside.
type evictTrace struct {
	responses []any
	codes     []im.StatusCode
	sessions  []camera.ProviderSession
	offers    int
	ended     []portEnd
}

// TestProviderEvictIdleZeroByteForByte (issue test 5): EvictIdle 0 — and
// EvictAcrossFabrics without it, or EvictIdle without a bound — answers a
// sequence of offers, past every idle time, exactly as the provider
// without either field: the same responses and statuses, the same table,
// the same port calls. The tests of provider_cap_test.go run unchanged.
func TestProviderEvictIdleZeroByteForByte(t *testing.T) {
	t.Parallel()
	a, b, c := peer(1, 0x55), peer(2, 0x66), peer(3, 0x77)
	run := func(edit func(*camera.ProviderConfig)) evictTrace {
		r := newEvictRig(t, edit)
		var tr evictTrace
		for _, ctx := range []context.Context{a, a, b, b, a, a, c, a, b, c} {
			for _, solicit := range []bool{false, true} {
				var (
					resp any
					err  error
				)
				if solicit {
					offers := r.port.offerCount()
					resp, err = r.p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicitReq(1))
					if err == nil && r.port.offerCount() == offers {
						r.nextEnd(t) // the refusal's End is out before the next command
					}
				} else {
					resp, err = r.p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newSessionOffer())
				}
				tr.responses, tr.codes = append(tr.responses, resp), append(tr.codes, statusOf(err))
				r.clk.Advance(time.Hour)
			}
		}
		tr.sessions, tr.offers, tr.ended = r.p.Sessions(), r.port.offerCount(), r.port.ended()
		return tr
	}
	// The provider without either field: EvictIdle 0, the default.
	plain := run(func(c *camera.ProviderConfig) { c.MaxSessions, c.MaxSessionsPerFabric, c.EvictIdle = 4, 2, 0 })
	if len(plain.ended) != 0 || plain.offers != 4 {
		t.Fatalf("the provider without eviction ended %v, asked the port %d times", plain.ended, plain.offers)
	}
	inert := run(func(c *camera.ProviderConfig) {
		c.MaxSessions, c.MaxSessionsPerFabric, c.EvictIdle, c.EvictAcrossFabrics = 4, 2, 0, true
	})
	if !reflect.DeepEqual(inert, plain) {
		t.Errorf("EvictAcrossFabrics without EvictIdle: %+v\nwant %+v", inert, plain)
	}
	// The same sequence with EvictIdle does evict: the trace is not
	// blind to the field.
	if evicting := run(bounds(4, 2)); reflect.DeepEqual(evicting, plain) || len(evicting.ended) == 0 {
		t.Fatal("EvictIdle set: the trace equals the provider's without it")
	}
	unbounded := run(func(c *camera.ProviderConfig) { c.EvictAcrossFabrics = true })
	if want := run(func(c *camera.ProviderConfig) { c.EvictIdle = 0 }); !reflect.DeepEqual(unbounded, want) || len(unbounded.ended) != 0 {
		t.Fatalf("EvictIdle without a bound: %+v, want %+v", unbounded, want)
	}
}

func solicitReq(endpoint uint16) wpdef.SolicitOfferRequest {
	return solicit(wpdef.StreamUsageLiveView, func(r *wpdef.SolicitOfferRequest) { r.OriginatingEndpointId = endpoint })
}

// TestProviderEvictTieTableOrder: entries of the same activity go in the
// table's order.
func TestProviderEvictTieTableOrder(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	r := newEvictRig(t, bounds(0, 3))
	var ids []uint16
	for range 3 {
		id, err := provideNew(a, t, r.p) // the clock stands: one activity
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	r.clk.Advance(evictIdle)
	if _, err := provideNew(a, t, r.p); err != nil {
		t.Fatal(err)
	}
	wantEnded(t, r.port, ids[0])
}

// TestProviderEvictActivity (decision 704): each accepted signal, in
// either direction, makes its session the youngest — the oldest then goes
// next; a refused one (another node's, or one the port fails) does not.
func TestProviderEvictActivity(t *testing.T) {
	t.Parallel()
	a, other := peer(1, 0x55), peer(1, 0x56)
	cand := []wpdef.ICECandidateStruct{{Candidate: "c", SdpMid: spec.ValueOf("0")}}
	signals := map[string]func(r *evictRig, id uint16) error{
		"ProvideICECandidates": func(r *evictRig, id uint16) error {
			_, err := r.p.MatterInvoke(a, wpdef.CmdProvideIceCandidates, wpdef.ProvideIceCandidatesRequest{WebRtcSessionId: id, IceCandidates: cand})
			return err
		},
		"ProvideAnswer": func(r *evictRig, id uint16) error {
			_, err := r.p.MatterInvoke(a, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: id, Sdp: "v=0"})
			return err
		},
		"re-offer": func(r *evictRig, id uint16) error {
			re := newSessionOffer()
			re.WebRtcSessionId, re.StreamUsage, re.OriginatingEndpointId, re.VideoStreamId = spec.ValueOf(id), nil, nil, nv(1)
			_, err := r.p.MatterInvoke(a, wpdef.CmdProvideOffer, re)
			return err
		},
		"Offer":         func(r *evictRig, id uint16) error { return r.p.Offer(context.Background(), id, "v=0") },
		"Answer":        func(r *evictRig, id uint16) error { return r.p.Answer(context.Background(), id, "v=0") },
		"IceCandidates": func(r *evictRig, id uint16) error { return r.p.IceCandidates(context.Background(), id, cand) },
	}
	for name, signal := range signals {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newEvictRig(t, bounds(0, 2))
			first, second := r.open(t, a), r.open(t, a)
			r.clk.Advance(evictIdle)
			if err := signal(r, first); err != nil {
				t.Fatal(err)
			}
			r.clk.Advance(time.Second)
			if _, err := provideNew(a, t, r.p); err != nil {
				t.Fatal(err)
			}
			wantEnded(t, r.port, second)
		})
	}
	t.Run("refused", func(t *testing.T) {
		t.Parallel()
		r := newEvictRig(t, bounds(0, 2))
		first := r.open(t, a)
		r.open(t, a)
		r.clk.Advance(evictIdle)
		_, err := r.p.MatterInvoke(other, wpdef.CmdProvideIceCandidates, wpdef.ProvideIceCandidatesRequest{WebRtcSessionId: first, IceCandidates: cand})
		wantCode(t, "another node's candidates", statusOf(err), im.StatusNotFound)
		r.port.mu.Lock()
		r.port.err = errors.New("stack down")
		r.port.mu.Unlock()
		if _, err := r.p.MatterInvoke(a, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: first, Sdp: "v=0"}); err == nil {
			t.Fatal("ProvideAnswer the port fails: accepted")
		}
		r.port.mu.Lock()
		r.port.err = nil
		r.port.mu.Unlock()
		if _, err := provideNew(a, t, r.p); err != nil {
			t.Fatal(err)
		}
		wantEnded(t, r.port, first)
	})
}

// TestProviderEvictAtTotalBoundOwnFabric: at the provider's bound,
// without EvictAcrossFabrics, the accessing fabric's own idle entry goes.
func TestProviderEvictAtTotalBoundOwnFabric(t *testing.T) {
	t.Parallel()
	a, b := peer(1, 0x55), peer(2, 0x66)
	r := newEvictRig(t, bounds(3, 0))
	b0 := r.open(t, b)
	a0 := r.open(t, a)
	r.open(t, a)
	r.clk.Advance(evictIdle)
	if _, err := provideNew(a, t, r.p); err != nil {
		t.Fatal(err)
	}
	wantEnded(t, r.port, a0)
	if !slices.Contains(sessionIDs(r.p), b0) {
		t.Fatal("fabric B's session went")
	}
}

// TestProviderEvictHostRowsOverBound (decision 706): a table the host's
// rows hold over the bound is refused — one eviction would not make room
// — and nothing is ended; a host row is evictable EvictIdle after its
// UpsertSession.
func TestProviderEvictHostRowsOverBound(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	r := newEvictRig(t, bounds(2, 0))
	for id := range uint16(3) {
		if err := r.p.UpsertSession(hostSession(100+id, 1)); err != nil {
			t.Fatal(err)
		}
	}
	r.clk.Advance(evictIdle)
	_, err := provideNew(a, t, r.p)
	wantCode(t, "over the bound through host rows", statusOf(err), im.StatusResourceExhausted)
	if len(r.port.ended()) != 0 || len(r.p.Sessions()) != 3 {
		t.Fatal("a session was ended in vain")
	}

	q := newEvictRig(t, bounds(0, 1))
	if err := q.p.UpsertSession(hostSession(100, 1)); err != nil {
		t.Fatal(err)
	}
	q.clk.Advance(evictIdle - time.Second)
	_, err = provideNew(a, t, q.p)
	wantCode(t, "a host row younger than EvictIdle", statusOf(err), im.StatusResourceExhausted)
	q.clk.Advance(time.Second)
	if _, err := provideNew(a, t, q.p); err != nil {
		t.Fatal(err)
	}
	wantEnded(t, q.port, 100)
	if c := q.nextEnd(t); c.node != 0x99 || c.req.WebRtcSessionId != 100 {
		t.Fatalf("End %+v", c)
	}
}

// TestProviderEvictWithoutEnd (decision 707): without a PeerConnector, or
// with the End queue full, the entry is evicted without an End.
func TestProviderEvictWithoutEnd(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	r := newEvictRig(t, bounds(1, 0))
	r.p.AttachPeerConnector(nil)
	first := r.open(t, a)
	r.clk.Advance(evictIdle)
	if _, err := provideNew(a, t, r.p); err != nil {
		t.Fatal(err)
	}
	wantEnded(t, r.port, first)

	q := newEvictRig(t, bounds(1, 0))
	q.peers.gate = make(chan struct{})
	held := q.open(t, a)
	refused, err := solicitNew(a, t, q.p, 1) // its End holds the queue of one
	if err != nil {
		t.Fatal(err)
	}
	q.clk.Advance(evictIdle)
	if _, err := provideNew(a, t, q.p); err != nil {
		t.Fatal(err)
	}
	wantEnded(t, q.port, held)
	close(q.peers.gate)
	if c := q.nextEnd(t); c.req.WebRtcSessionId != refused.WebRtcSessionId {
		t.Fatalf("End %+v, want the refused SolicitOffer's", c)
	}
	// The next End is a later refusal's: the eviction queued none.
	again, err := solicitNew(a, t, q.p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c := q.nextEnd(t); c.req.WebRtcSessionId != again.WebRtcSessionId {
		t.Fatalf("End %+v, want the second refusal's (%d): the eviction sent one", c, again.WebRtcSessionId)
	}
}

// TestProviderEvictPortErrors: the port's EndSession error does not stop
// the offer (the entry is gone); a ProvideOffer the port fails after the
// eviction leaves the victim ended; the evicted id is NOT_FOUND to its
// requestor afterwards.
func TestProviderEvictPortErrors(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	r := newEvictRig(t, bounds(1, 0))
	r.port.endErr = errors.New("stack gone")
	first := r.open(t, a)
	r.clk.Advance(evictIdle)
	id, err := provideNew(a, t, r.p)
	if err != nil {
		t.Fatalf("offer after a failing EndSession: %v", err)
	}
	if got := sessionIDs(r.p); !sameIDs(got, []uint16{id}) {
		t.Fatalf("CurrentSessions %v", got)
	}
	_, err = r.p.MatterInvoke(a, wpdef.CmdEndSession, wpdef.EndSessionRequest{WebRtcSessionId: first, Reason: 2})
	wantCode(t, "EndSession of the evicted id", statusOf(err), im.StatusNotFound)
	_, err = r.p.MatterInvoke(a, wpdef.CmdProvideIceCandidates, wpdef.ProvideIceCandidatesRequest{WebRtcSessionId: first, IceCandidates: []wpdef.ICECandidateStruct{{Candidate: "c", SdpMid: spec.ValueOf("0")}}})
	wantCode(t, "candidates for the evicted id", statusOf(err), im.StatusNotFound)

	r.clk.Advance(evictIdle)
	r.port.mu.Lock()
	r.port.err = errors.New("no stream")
	r.port.mu.Unlock()
	if _, err := provideNew(a, t, r.p); err == nil {
		t.Fatal("a ProvideOffer the port fails: accepted")
	}
	if len(r.p.Sessions()) != 0 {
		t.Fatalf("CurrentSessions %v, want the victim ended", sessionIDs(r.p))
	}
}

// TestProviderEvictFreshStampForAReusedID: an id that left the table
// starts with a fresh activity when it is used again.
func TestProviderEvictFreshStampForAReusedID(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	r := newEvictRig(t, bounds(0, 1))
	if err := r.p.UpsertSession(hostSession(7, 1)); err != nil {
		t.Fatal(err)
	}
	r.clk.Advance(evictIdle)
	r.p.RemoveSession(7)
	if err := r.p.UpsertSession(hostSession(7, 1)); err != nil {
		t.Fatal(err)
	}
	_, err := provideNew(a, t, r.p)
	wantCode(t, "a reused id", statusOf(err), im.StatusResourceExhausted)
}

// TestProviderEvictNegative: a negative EvictIdle is refused.
func TestProviderEvictNegative(t *testing.T) {
	t.Parallel()
	_, err := camera.NewProvider(camera.ProviderConfig{Port: &mockProvider{}, EvictIdle: -time.Second})
	if !errors.Is(err, camera.ErrNegativeEvictIdle) {
		t.Fatalf("NewProvider = %v, want ErrNegativeEvictIdle", err)
	}
}

// TestProviderEvictRaces (-race): the host's End of the victim against an
// evicting offer — one End and one removal; two offers at the bound evict
// one victim each; EndLostPeer and MatterDropFabric against an evicting
// offer end each session once.
func TestProviderEvictRaces(t *testing.T) {
	t.Parallel()
	a := peer(1, 0x55)
	t.Run("host End", func(t *testing.T) {
		t.Parallel()
		r := newEvictRig(t, bounds(1, 0))
		victim := r.open(t, a)
		r.clk.Advance(evictIdle)
		var wg sync.WaitGroup
		var hostErr, offerErr error
		wg.Go(func() { hostErr = r.p.End(context.Background(), victim, 2) })
		wg.Go(func() { _, offerErr = provideNew(a, t, r.p) })
		wg.Wait()
		if offerErr != nil {
			t.Fatalf("offer: %v", offerErr)
		}
		c := r.nextEnd(t)
		if c.req.WebRtcSessionId != victim {
			t.Fatalf("End %+v", c)
		}
		switch {
		case hostErr == nil: // the host's End won: no eviction, no port call
			if c.req.Reason != 2 || len(r.port.ended()) != 0 {
				t.Fatalf("host End won, then End %+v, port %v", c, r.port.ended())
			}
		case errors.Is(hostErr, camera.ErrUnknownSession): // the eviction won
			if c.req.Reason != endOutOfResources {
				t.Fatalf("End %+v", c)
			}
			wantEnded(t, r.port, victim)
		default:
			t.Fatalf("host End: %v", hostErr)
		}
		sessions := sessionIDs(r.p)
		if len(sessions) != 1 {
			t.Fatalf("CurrentSessions %v", sessions)
		}
		// One End only: the next is the sentinel's.
		if err := r.p.End(context.Background(), sessions[0], 3); err != nil {
			t.Fatal(err)
		}
		if c := r.nextEnd(t); c.req.WebRtcSessionId != sessions[0] || c.req.Reason != 3 {
			t.Fatalf("End %+v, want the sentinel: the victim was sent two", c)
		}
	})
	t.Run("two offers", func(t *testing.T) {
		t.Parallel()
		r := newEvictRig(t, bounds(0, 2))
		v0, v1 := r.open(t, a), r.open(t, a)
		r.clk.Advance(evictIdle)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Go(func() { _, errs[i] = provideNew(a, t, r.p) })
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		wantEnded(t, r.port, v0, v1)
		if len(r.p.Sessions()) != 2 {
			t.Fatalf("CurrentSessions %v", sessionIDs(r.p))
		}
	})
	t.Run("EndLostPeer", func(t *testing.T) {
		t.Parallel()
		r := newEvictRig(t, bounds(1, 0))
		victim := r.open(t, peer(1, 0x44))
		r.clk.Advance(evictIdle)
		var wg sync.WaitGroup
		var offerErr error
		wg.Go(func() { _, _, _ = r.p.EndLostPeer(context.Background(), 1, 0x44, 1, nil) })
		wg.Go(func() { _, offerErr = provideNew(a, t, r.p) })
		wg.Wait()
		if offerErr != nil {
			t.Fatal(offerErr)
		}
		if ends := r.port.ended(); len(ends) != 1 || ends[0].id != victim {
			t.Fatalf("the port's EndSession %v, want the victim once", ends)
		}
	})
	t.Run("MatterDropFabric", func(t *testing.T) {
		t.Parallel()
		b, c := peer(2, 0x66), peer(3, 0x77)
		r := newEvictRig(t, across(4, 0))
		for _, ctx := range []context.Context{a, a, a, b} {
			r.open(t, ctx)
		}
		r.clk.Advance(evictIdle)
		var wg sync.WaitGroup
		var offerErr error
		wg.Go(func() { _ = r.p.MatterDropFabric(context.Background(), 1) })
		wg.Go(func() { _, offerErr = provideNew(c, t, r.p) })
		wg.Wait()
		if offerErr != nil {
			t.Fatal(offerErr)
		}
		seen := map[uint16]int{}
		for _, e := range r.port.ended() {
			seen[e.id]++
			if seen[e.id] > 1 {
				t.Fatalf("session %d ended twice: %v", e.id, r.port.ended())
			}
		}
		if held := fabricsOf(r.p); held[1] != 0 || held[2] != 1 || held[3] != 1 {
			t.Fatalf("CurrentSessions by fabric %v", held)
		}
	})
}

// TestProviderEvictAcrossFabricsWalk (§8.6): 8 sessions, 4 per fabric; A
// and B hold four each, C offers. With three fabrics the share is 2, A
// and B hold borrowed entries: C's first two offers take the longest-idle
// of them (A's, then B's) — A3 B3 C2; at its share C's third has the own
// rule only (refused while its entries are younger than EvictIdle, then
// its own oldest goes). A, over its share, never takes C's or B's back:
// no ping-pong at a fixed number of fabrics.
func TestProviderEvictAcrossFabricsWalk(t *testing.T) {
	t.Parallel()
	a, b, c := peer(1, 0x55), peer(2, 0x66), peer(3, 0x77)
	r := newEvictRig(t, across(8, 4))
	var aIDs, bIDs []uint16
	for range 4 {
		aIDs = append(aIDs, r.open(t, a))
		bIDs = append(bIDs, r.open(t, b))
	}
	r.clk.Advance(evictIdle)
	c0, c1 := r.open(t, c), r.open(t, c)
	wantEnded(t, r.port, aIDs[0], bIDs[0])
	if held := fabricsOf(r.p); held[1] != 3 || held[2] != 3 || held[3] != 2 {
		t.Fatalf("CurrentSessions by fabric %v, want A3 B3 C2", held)
	}
	for _, want := range []uint16{aIDs[0], bIDs[0]} {
		if e := r.nextEnd(t); e.req.WebRtcSessionId != want || e.req.Reason != endOutOfResources {
			t.Fatalf("End %+v, want session %d", e, want)
		}
	}
	_, err := provideNew(c, t, r.p)
	wantCode(t, "C at its share, its own entries young", statusOf(err), im.StatusResourceExhausted)
	r.clk.Advance(evictIdle)
	r.open(t, c)
	wantEnded(t, r.port, aIDs[0], bIDs[0], c0)
	r.clk.Advance(evictIdle)
	r.open(t, a) // A over its share: its own oldest, never B's or C's
	wantEnded(t, r.port, aIDs[0], bIDs[0], c0, aIDs[1])
	if held := fabricsOf(r.p); held[1] != 3 || held[2] != 3 || held[3] != 2 || !slices.Contains(sessionIDs(r.p), c1) {
		t.Fatalf("CurrentSessions by fabric %v", held)
	}
}

// TestProviderEvictAcrossFabricsWithinShare: a fabric at exactly its share
// is never a victim — two fabrics at 4 of 8, 4 per fabric, each end only
// their own; three fabrics on two slots (share 1) find no borrowed entry.
func TestProviderEvictAcrossFabricsWithinShare(t *testing.T) {
	t.Parallel()
	a, b, c := peer(1, 0x55), peer(2, 0x66), peer(3, 0x77)
	r := newEvictRig(t, across(8, 4))
	var bIDs []uint16
	for range 4 {
		bIDs = append(bIDs, r.open(t, b))
	}
	aFirst := r.open(t, a)
	for range 3 {
		r.open(t, a)
	}
	r.clk.Advance(evictIdle)
	r.open(t, a)
	wantEnded(t, r.port, aFirst)
	r.open(t, b)
	wantEnded(t, r.port, aFirst, bIDs[0])

	q := newEvictRig(t, across(2, 0))
	q.open(t, a)
	q.open(t, b)
	q.clk.Advance(evictIdle)
	_, err := provideNew(c, t, q.p)
	wantCode(t, "a third fabric on two slots", statusOf(err), im.StatusResourceExhausted)
	if len(q.port.ended()) != 0 {
		t.Fatal("an entry within its share was ended")
	}
}

// TestProviderEvictAcrossFabricsFallback: no borrowed entry idle long
// enough — the fabric's own idle entry goes; neither — RESOURCE_EXHAUSTED.
// Without MaxSessionsPerFabric the share is MaxSessions over the fabrics.
func TestProviderEvictAcrossFabricsFallback(t *testing.T) {
	t.Parallel()
	a, b, c := peer(1, 0x55), peer(2, 0x66), peer(3, 0x77)
	r := newEvictRig(t, across(4, 0))
	cOld := r.open(t, c)
	r.clk.Advance(evictIdle)
	for range 3 {
		r.open(t, a) // A holds 3 > share 2 (n 2), all young
	}
	r.open(t, c)
	wantEnded(t, r.port, cOld)
	_, err := provideNew(c, t, r.p)
	wantCode(t, "nothing idle", statusOf(err), im.StatusResourceExhausted)
	_, err = provideNew(b, t, r.p)
	wantCode(t, "B under its share, A's entries young", statusOf(err), im.StatusResourceExhausted)

	q := newEvictRig(t, across(8, 0))
	var aIDs []uint16
	for range 8 {
		aIDs = append(aIDs, q.open(t, a))
	}
	q.clk.Advance(evictIdle)
	q.open(t, b)
	wantEnded(t, q.port, aIDs[0])
}

// TestProviderEvictAcrossFabricsInert (decision 711): EvictAcrossFabrics
// without MaxSessions has the own-fabric rule only; a table over
// MaxSessions through host rows is refused (decision 706); SolicitOffer
// takes a borrowed entry as ProvideOffer does.
func TestProviderEvictAcrossFabricsInert(t *testing.T) {
	t.Parallel()
	a, b, c := peer(1, 0x55), peer(2, 0x66), peer(3, 0x77)
	r := newEvictRig(t, across(0, 2))
	r.open(t, a)
	r.open(t, a)
	r.clk.Advance(evictIdle)
	r.open(t, b)
	r.open(t, b)
	_, err := provideNew(b, t, r.p)
	wantCode(t, "B at its bound, its own entries young", statusOf(err), im.StatusResourceExhausted)
	if len(r.port.ended()) != 0 {
		t.Fatal("A's entry was taken without MaxSessions")
	}

	q := newEvictRig(t, across(2, 0))
	for id := range uint16(3) {
		if err := q.p.UpsertSession(hostSession(100+id, 1)); err != nil {
			t.Fatal(err)
		}
	}
	q.clk.Advance(evictIdle)
	_, err = provideNew(c, t, q.p)
	wantCode(t, "over MaxSessions through host rows", statusOf(err), im.StatusResourceExhausted)
	if len(q.port.ended()) != 0 {
		t.Fatal("a session was ended in vain")
	}

	s := newEvictRig(t, across(4, 0))
	var aIDs []uint16
	for range 4 {
		aIDs = append(aIDs, s.open(t, a))
	}
	s.clk.Advance(evictIdle)
	resp, err := solicitNew(c, t, s.p, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantEnded(t, s.port, aIDs[0])
	if !slices.Contains(sessionIDs(s.p), resp.WebRtcSessionId) {
		t.Fatalf("CurrentSessions %v, want the solicited session", sessionIDs(s.p))
	}
}
