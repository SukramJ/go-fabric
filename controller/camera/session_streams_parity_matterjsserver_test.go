// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
)

// The cases of matterjs-server's
// packages/ws-controller/test/WebRtcSessionStreamsTest.ts:21-933 at
// 0bd06f2 the typed Go fields can carry: a list or id that is not a
// number, a fractional, negative or NaN id, cannot arise in them.

func ssID(v uint16) *spec.Nullable[uint16] { return &spec.Nullable[uint16]{Value: v} }

var ssNull = &spec.Nullable[uint16]{Null: true}

func ssList(ids ...uint16) *[]uint16 { return &ids }

// TestSessionStreamsResolveParityMatterjsServer is describe("resolveWebRtcSessionStreams")
// (WebRtcSessionStreamsTest.ts:21-93, matterjs-server at 0bd06f2).
func TestSessionStreamsResolveParityMatterjsServer(t *testing.T) {
	cases := []struct {
		name              string
		list              *[]uint16
		request, response *spec.Nullable[uint16]
		want              []uint16
	}{
		{"uses the requested rev-2 list verbatim", ssList(3, 5), nil, nil, []uint16{3, 5}},
		{"prefers the requested list over the deprecated scalar echoes", ssList(3), ssID(9), ssID(9), []uint16{3}},
		{"wraps an explicit rev-1 request id even when the response omits its echo", nil, ssID(3), nil, []uint16{3}},
		{"keeps the explicit request id when the response echoes a different value", nil, ssID(3), ssID(7), []uint16{3}},
		{"falls back to the response echo when the request asked for auto-selection", nil, ssNull, ssID(7), []uint16{7}},
		{"yields no stream when auto-selection is requested but the provider reports none", nil, ssNull, ssNull, nil},
		{"yields no stream when auto-selection is requested but the provider reports none (absent echo)", nil, ssNull, nil, nil},
		{"yields no stream when the media kind is absent from the request", nil, nil, nil, nil},
		{"ignores an empty requested list and falls through to the scalar path", ssList(), ssID(3), nil, []uint16{3}},
		{"ignores an empty requested list and falls through to the scalar path (no id)", ssList(), nil, nil, nil},
		{"treats a non-numeric, non-null request id as an omitted media kind (absent id, echo 7)", nil, nil, ssID(7), nil},
		{"accepts stream id 0", nil, ssID(0), nil, []uint16{0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveWebRTCSessionStreams(c.list, c.request, c.response); !slices.Equal(got, c.want) || (got == nil) != (c.want == nil) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestSessionStreamsSelectParityMatterjsServer is describe("selectWebRtcStreamFields")
// (WebRtcSessionStreamsTest.ts:95-194, matterjs-server at 0bd06f2).
func TestSessionStreamsSelectParityMatterjsServer(t *testing.T) {
	offer := func(edit func(*wpdef.ProvideOfferRequest)) wpdef.ProvideOfferRequest {
		r := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}, Sdp: "v=0"}
		edit(&r)
		return r
	}
	selected := func(t *testing.T, r wpdef.ProvideOfferRequest, revision uint16, known bool) wpdef.ProvideOfferRequest {
		t.Helper()
		got, err := selectWebRTCStreamFields(r, revision, known)
		if err != nil {
			t.Fatal(err)
		}
		return got.(wpdef.ProvideOfferRequest)
	}
	refused := func(t *testing.T, r wpdef.ProvideOfferRequest, revision uint16, known bool, want string) {
		t.Helper()
		_, err := selectWebRTCStreamFields(r, revision, known)
		if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	same := func(a, b *[]uint16) bool { return (a == nil) == (b == nil) && (a == nil || slices.Equal(*a, *b)) }
	sameID := func(a, b *spec.Nullable[uint16]) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	t.Run("sends the lists verbatim for a rev-2 provider", func(t *testing.T) {
		got := selected(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams, r.AudioStreams = ssList(7), ssList(9) }), 2, true)
		if !same(got.VideoStreams, ssList(7)) || !same(got.AudioStreams, ssList(9)) || got.VideoStreamId != nil {
			t.Errorf("%+v", got)
		}
	})
	t.Run("keeps multiple streams for a rev-2 provider", func(t *testing.T) {
		if got := selected(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams = ssList(3, 5) }), 2, true); !same(got.VideoStreams, ssList(3, 5)) {
			t.Errorf("%+v", got)
		}
	})
	t.Run("sends a legacy caller's singular ids unchanged to a rev-2 provider", func(t *testing.T) {
		got := selected(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreamId, r.AudioStreamId = ssID(5), ssNull }), 3, true)
		if !sameID(got.VideoStreamId, ssID(5)) || !sameID(got.AudioStreamId, ssNull) {
			t.Errorf("%+v", got)
		}
	})
	t.Run("down-converts the lists to singular ids for a rev-1 provider", func(t *testing.T) {
		got := selected(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams, r.AudioStreams = ssList(7), ssList(9) }), 1, true)
		if got.VideoStreams != nil || got.AudioStreams != nil || !sameID(got.VideoStreamId, ssID(7)) || !sameID(got.AudioStreamId, ssID(9)) {
			t.Errorf("%+v", got)
		}
	})
	t.Run("refuses a multi-stream list for a rev-1 provider rather than truncating it", func(t *testing.T) {
		refused(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams = ssList(3, 5) }), 1, true, "videoStreams names 2 streams")
	})
	t.Run("down-converts the lists when the provider revision is unknown", func(t *testing.T) {
		got := selected(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams = ssList(7) }), 0, false)
		if got.VideoStreams != nil || !sameID(got.VideoStreamId, ssID(7)) {
			t.Errorf("%+v", got)
		}
	})
	t.Run("leaves a rev-1 auto-select request untouched", func(t *testing.T) {
		if got := selected(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreamId = ssNull }), 1, true); !sameID(got.VideoStreamId, ssNull) {
			t.Errorf("%+v", got)
		}
	})
	t.Run("preserves a legacy null auto-select request on a rev-2 provider when no list is sent", func(t *testing.T) {
		got := selected(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreamId, r.AudioStreamId = ssNull, ssNull }), 2, true)
		if !sameID(got.VideoStreamId, ssNull) || !sameID(got.AudioStreamId, ssNull) {
			t.Errorf("%+v", got)
		}
	})
	t.Run("refuses a list sent beside a null auto-select on the other media kind", func(t *testing.T) {
		refused(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams, r.AudioStreamId = ssList(5), ssNull }), 2, true, "videoStreams cannot be sent together with audioStreamId")
	})
	t.Run("refuses a list sent beside the singular id of the same media kind", func(t *testing.T) {
		refused(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams, r.VideoStreamId = ssList(5), ssID(7) }), 1, true, "videoStreams cannot be sent together with videoStreamId")
	})
	t.Run("refuses an empty list whatever the provider's revision, rather than dropping it", func(t *testing.T) {
		refused(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams = ssList() }), 1, true, "videoStreams names no stream")
		refused(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams = ssList() }), 2, true, "videoStreams names no stream")
	})
	t.Run("says the revision is unread, not that the camera states revision 1, when it has not been read", func(t *testing.T) {
		refused(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams = ssList(3, 5) }), 0, false, "has not read this camera's WebRTC Provider ClusterRevision")
		refused(t, offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams = ssList(3, 5) }), 1, true, "states cluster revision 1")
	})
	t.Run("leaves the request untouched when the other media kind is refused", func(t *testing.T) {
		r := offer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams, r.AudioStreams = ssList(7), ssList(3, 5) })
		refused(t, r, 1, true, "audioStreams names 2 streams")
		if !same(r.VideoStreams, ssList(7)) || !same(r.AudioStreams, ssList(3, 5)) || r.VideoStreamId != nil {
			t.Errorf("changed: %+v", r)
		}
	})
}

// TestSessionStreamsTrackableParityMatterjsServer is describe("isTrackableWebRtcSession")
// (WebRtcSessionStreamsTest.ts:196-224, matterjs-server at 0bd06f2).
func TestSessionStreamsTrackableParityMatterjsServer(t *testing.T) {
	u := func(v wpdef.StreamUsageEnum) *wpdef.StreamUsageEnum { return &v }
	cases := []struct {
		name         string
		usage        *wpdef.StreamUsageEnum
		video, audio []uint16
		want         bool
	}{
		{"accepts a session with a numeric usage and a video stream", u(3), []uint16{3}, nil, true},
		{"accepts a session with a numeric usage and an audio stream", u(3), nil, []uint16{1}, true},
		{"rejects a session with no video and no audio stream", u(3), nil, nil, false},
		{"rejects a session whose stream usage is missing or non-numeric (missing)", nil, []uint16{3}, []uint16{1}, false},
		{"accepts stream usage 0", u(0), []uint16{3}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTrackableWebRTCSession(c.usage, c.video, c.audio); got != c.want {
				t.Errorf("got %v", got)
			}
		})
	}
}

// ssFixture is the establish io of WebRtcSessionStreamsTest.ts:226-245 and
// :767-795, matterjs-server at 0bd06f2: the camera's answers, the
// requestor's upserts and the steps they took, the recorded offers.
type ssFixture struct {
	mu        sync.Mutex
	steps     []string
	invokes   []spec.Encodable
	respond   func(ctx context.Context, command string) (any, error)
	upsertErr error
	held      []wrdef.WebRTCSessionStruct
	revision  uint16
	known     bool
	ps        *providerSessions
	clk       *clocktest.Fake
}

func (f *ssFixture) step(s string) {
	f.mu.Lock()
	f.steps = append(f.steps, s)
	f.mu.Unlock()
}

func (f *ssFixture) providerRevision(target) (revision uint16, known bool, err error) {
	return f.revision, f.known, nil
}

func (f *ssFixture) invokeProvider(ctx context.Context, _ target, command string, fields spec.Encodable) (any, error) {
	f.step(command)
	f.mu.Lock()
	f.invokes = append(f.invokes, fields)
	f.mu.Unlock()
	if f.respond != nil {
		return f.respond(ctx, command)
	}
	if command == callSolicitOffer {
		return wpdef.SolicitOfferResponse{WebRtcSessionId: 9}, nil
	}
	return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
}

func (f *ssFixture) syncTime(context.Context, uint64) error { return nil }

func (f *ssFixture) endpointID() uint16 { return 2 }

func (f *ssFixture) sessions() []wrdef.WebRTCSessionStruct {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.held)
}

func (f *ssFixture) upsert(s wrdef.WebRTCSessionStruct) error {
	// The offer of a new session is recorded before the requestor holds it.
	if _, ok := f.ps.offerOf(f.ps.key(target{5, 1}, s.Id)); ok {
		f.step(fmt.Sprintf("offer:%d", s.Id))
	}
	f.step("upsert")
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.mu.Lock()
	f.held = append(f.held, s)
	f.mu.Unlock()
	return nil
}

func (f *ssFixture) remove(wrdef.SessionKey) {}

func newSSFixture() *ssFixture {
	f := &ssFixture{revision: 2, known: true, clk: clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	m := newManager(&fakeIO{}, f.clk, nil)
	f.ps = &providerSessions{nodes: f, requestor: f, fabricIndex: 1, log: slog.New(slog.DiscardHandler), cleanup: m.cleanupContext, offers: map[providerKey]string{}}
	return f
}

func (f *ssFixture) establish(req spec.Encodable, establishing func(uint16)) (any, error) {
	return f.ps.establish(context.Background(), target{5, 1}, req, establishing)
}

func (f *ssFixture) offered(id uint16) bool {
	_, ok := f.ps.offerOf(f.ps.key(target{5, 1}, id))
	return ok
}

func (f *ssFixture) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, s := range f.steps {
		if s == callProvideOffer || s == callSolicitOffer || s == callEndSession {
			out = append(out, s)
		}
	}
	return out
}

func ssNew(edit func(*wpdef.ProvideOfferRequest)) wpdef.ProvideOfferRequest {
	usage := wpdef.StreamUsageLiveView
	r := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}, StreamUsage: &usage, VideoStreams: ssList(1)}
	if edit != nil {
		edit(&r)
	}
	return r
}

func ssReOffer(edit func(*wpdef.ProvideOfferRequest)) wpdef.ProvideOfferRequest {
	r := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Value: 9}, Sdp: "v=0"}
	if edit != nil {
		edit(&r)
	}
	return r
}

// TestSessionStreamsEstablishParityMatterjsServer is describe("establishWebRtcProviderSession")
// (WebRtcSessionStreamsTest.ts:226-877, matterjs-server at 0bd06f2).
func TestSessionStreamsEstablishParityMatterjsServer(t *testing.T) {
	var local *localError
	t.Run("throws when the device response carries no numeric webRtcSessionId", func(t *testing.T) {
		f := newSSFixture()
		f.respond = func(context.Context, string) (any, error) { return nil, nil }
		if _, err := f.establish(ssNew(nil), nil); !errors.As(err, &local) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("injects originatingEndpointId into the fields sent to the device", func(t *testing.T) {
		f := newSSFixture()
		if _, err := f.establish(ssNew(nil), nil); err != nil {
			t.Fatal(err)
		}
		if got := f.invokes[0].(wpdef.ProvideOfferRequest).OriginatingEndpointId; got == nil || *got != 2 {
			t.Errorf("originating %v", got)
		}
	})
	t.Run("establishes nothing when the request states both stream forms", func(t *testing.T) {
		f := newSSFixture()
		_, err := f.establish(ssNew(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams, r.AudioStreamId = ssList(5), ssNull }), nil)
		if !errors.Is(err, ErrInvalidArgument) || len(f.commands()) != 0 {
			t.Errorf("err = %v, sent %v", err, f.commands())
		}
	})
	t.Run("sends a legacy caller's singular stream id unchanged to a revision-2 provider", func(t *testing.T) {
		f := newSSFixture()
		if _, err := f.establish(ssNew(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams, r.VideoStreamId = nil, ssID(4) }), nil); err != nil {
			t.Fatal(err)
		}
		if sent := f.invokes[0].(wpdef.ProvideOfferRequest); sent.VideoStreams != nil || sent.VideoStreamId == nil || sent.VideoStreamId.Value != 4 {
			t.Errorf("%+v", sent)
		}
	})
	t.Run("sends singular stream ids when the provider states no revision", func(t *testing.T) {
		f := newSSFixture()
		f.known = false
		if _, err := f.establish(ssNew(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams, r.AudioStreams = ssList(4), ssList(8) }), nil); err != nil {
			t.Fatal(err)
		}
		sent := f.invokes[0].(wpdef.ProvideOfferRequest)
		if sent.VideoStreams != nil || sent.AudioStreams != nil || sent.VideoStreamId.Value != 4 || sent.AudioStreamId.Value != 8 {
			t.Errorf("%+v", sent)
		}
	})
	t.Run("states the session id before the local requestor is given the session", func(t *testing.T) {
		f := newSSFixture()
		if _, err := f.establish(ssNew(nil), func(id uint16) { f.step(fmt.Sprintf("establishing:%d", id)) }); err != nil {
			t.Fatal(err)
		}
		steps := slices.DeleteFunc(slices.Clone(f.steps), func(s string) bool { return strings.HasPrefix(s, "offer:") })
		if !slices.Equal(steps, []string{callProvideOffer, "establishing:9", "upsert"}) {
			t.Errorf("steps %v", f.steps)
		}
	})
	t.Run("states a new session's offer before the local requestor is given the session", func(t *testing.T) {
		f := newSSFixture()
		if _, err := f.establish(ssNew(func(r *wpdef.ProvideOfferRequest) { r.Sdp = "v=0" }), nil); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(f.steps, []string{callProvideOffer, "offer:9", "upsert"}) {
			t.Errorf("steps %v", f.steps)
		}
	})
	t.Run("states a re-offer before it is sent, and withdraws it when the requestor does not hold the session", func(t *testing.T) {
		f := newSSFixture()
		var offeredBefore bool
		f.respond = func(context.Context, string) (any, error) {
			offeredBefore = f.offered(9)
			return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
		}
		if _, err := f.establish(ssReOffer(nil), nil); err != nil {
			t.Fatal(err)
		}
		if !offeredBefore || f.offered(9) {
			t.Errorf("offered before the send %v, kept %v", offeredBefore, f.offered(9))
		}
	})
	t.Run("tracks a trackable ProvideOffer session in the local requestor", func(t *testing.T) {
		f := newSSFixture()
		if _, err := f.establish(ssNew(nil), nil); err != nil {
			t.Fatal(err)
		}
		want := wrdef.WebRTCSessionStruct{Id: 9, PeerNodeId: 5, PeerEndpointId: 1, StreamUsage: 3, VideoStreams: ssList(1), FabricIndex: 1}
		if got := f.sessions(); len(got) != 1 || got[0].Id != want.Id || got[0].PeerNodeId != 5 || got[0].PeerEndpointId != 1 || got[0].StreamUsage != 3 || got[0].MetadataEnabled ||
			!slices.Equal(*got[0].VideoStreams, []uint16{1}) || got[0].AudioStreams != nil || got[0].FabricIndex != 1 {
			t.Errorf("upserted %+v", got)
		}
	})
	t.Run("tracks a trackable SolicitOffer session in the local requestor", func(t *testing.T) {
		f := newSSFixture()
		f.respond = func(context.Context, string) (any, error) {
			return wpdef.SolicitOfferResponse{WebRtcSessionId: 11}, nil
		}
		if _, err := f.establish(wpdef.SolicitOfferRequest{StreamUsage: 3, AudioStreams: ssList(2)}, nil); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(f.commands(), []string{callSolicitOffer}) {
			t.Errorf("commands %v", f.commands())
		}
		if got := f.sessions(); len(got) != 1 || got[0].Id != 11 || got[0].VideoStreams != nil || !slices.Equal(*got[0].AudioStreams, []uint16{2}) {
			t.Errorf("upserted %+v", got)
		}
	})
	t.Run("tracks a ProvideOffer that asks for a new session with a null session id", func(t *testing.T) {
		f := newSSFixture()
		if _, err := f.establish(ssNew(func(r *wpdef.ProvideOfferRequest) { r.Sdp = "v=0" }), nil); err != nil || len(f.sessions()) != 1 {
			t.Errorf("err %v, upserted %v", err, f.sessions())
		}
	})
	t.Run("sends a re-offer without stream usage or streams and leaves the session it names alone", func(t *testing.T) {
		f := newSSFixture()
		f.respond = func(context.Context, string) (any, error) { return wpdef.ProvideOfferResponse{WebRtcSessionId: 7}, nil }
		var established []uint16
		resp, err := f.establish(ssReOffer(func(r *wpdef.ProvideOfferRequest) { r.WebRtcSessionId.Value = 7 }), func(id uint16) { established = append(established, id) })
		if err != nil || resp.(wpdef.ProvideOfferResponse).WebRtcSessionId != 7 {
			t.Fatalf("%v %v", resp, err)
		}
		if !slices.Equal(f.commands(), []string{callProvideOffer}) || slices.Contains(f.steps, "upsert") || len(established) != 0 {
			t.Errorf("steps %v, established %v", f.steps, established)
		}
	})
	t.Run("tracks a re-offer that restates usage and streams only when no entry exists, without announcing it", func(t *testing.T) {
		f := newSSFixture()
		f.respond = func(context.Context, string) (any, error) { return wpdef.ProvideOfferResponse{WebRtcSessionId: 7}, nil }
		usage := wpdef.StreamUsageEnum(4)
		var established []uint16
		_, err := f.establish(ssReOffer(func(r *wpdef.ProvideOfferRequest) {
			r.WebRtcSessionId.Value, r.StreamUsage, r.AudioStreams = 7, &usage, ssList(2)
		}), func(id uint16) { established = append(established, id) })
		if err != nil {
			t.Fatal(err)
		}
		if got := f.sessions(); len(got) != 1 || got[0].Id != 7 || !slices.Equal(*got[0].AudioStreams, []uint16{2}) || len(established) != 0 {
			t.Errorf("upserted %+v, established %v", got, established)
		}
		// Only when no entry exists: a second re-offer adds none.
		if _, err := f.establish(ssReOffer(func(r *wpdef.ProvideOfferRequest) {
			r.WebRtcSessionId.Value, r.StreamUsage, r.AudioStreams = 7, &usage, ssList(3)
		}), nil); err != nil || len(f.sessions()) != 1 {
			t.Errorf("second re-offer: %v, %+v", err, f.sessions())
		}
	})
	t.Run("returns a re-offer's response and ends nothing when the entry cannot be added", func(t *testing.T) {
		f := newSSFixture()
		f.upsertErr = errors.New("requestor unavailable")
		f.respond = func(context.Context, string) (any, error) { return wpdef.ProvideOfferResponse{WebRtcSessionId: 7}, nil }
		usage := wpdef.StreamUsageEnum(4)
		resp, err := f.establish(ssReOffer(func(r *wpdef.ProvideOfferRequest) {
			r.WebRtcSessionId.Value, r.StreamUsage, r.AudioStreams = 7, &usage, ssList(2)
		}), nil)
		if err != nil || resp.(wpdef.ProvideOfferResponse).WebRtcSessionId != 7 || !slices.Equal(f.commands(), []string{callProvideOffer}) {
			t.Errorf("%v %v %v", resp, err, f.commands())
		}
	})
	t.Run("passes a re-offer's NOT_FOUND through without ending any session", func(t *testing.T) {
		f := newSSFixture()
		notFound := errors.New("NOT_FOUND")
		f.respond = func(context.Context, string) (any, error) { return nil, notFound }
		if _, err := f.establish(ssReOffer(func(r *wpdef.ProvideOfferRequest) { r.WebRtcSessionId.Value = 7 }), nil); !errors.Is(err, notFound) || !slices.Equal(f.commands(), []string{callProvideOffer}) {
			t.Errorf("%v %v", err, f.commands())
		}
	})
	t.Run("tears down the device session and throws when it cannot be tracked, without upserting it", func(t *testing.T) {
		f := newSSFixture()
		_, err := f.establish(wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}}, nil)
		if !errors.As(err, &local) || !slices.Equal(f.commands(), []string{callProvideOffer, callEndSession}) || slices.Contains(f.steps, "upsert") {
			t.Errorf("%v %v", err, f.steps)
		}
	})
	t.Run("ends the device session when the local requestor refuses to track it", func(t *testing.T) {
		f := newSSFixture()
		f.upsertErr = errors.New("requestor endpoint is gone")
		if _, err := f.establish(ssNew(nil), nil); !errors.Is(err, f.upsertErr) || !slices.Equal(f.commands(), []string{callProvideOffer, callEndSession}) {
			t.Errorf("%v %v", err, f.commands())
		}
	})
	t.Run("surfaces the untrackable-session error even when the EndSession cleanup itself fails", func(t *testing.T) {
		f := newSSFixture()
		f.respond = func(_ context.Context, command string) (any, error) {
			if command == callProvideOffer {
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
			}
			return nil, errors.New("device unreachable")
		}
		_, err := f.establish(wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}}, nil)
		if !errors.As(err, &local) || !strings.Contains(err.Error(), "produced a session with no stream usage") || !slices.Equal(f.commands(), []string{callProvideOffer, callEndSession}) {
			t.Errorf("%v %v", err, f.commands())
		}
	})
	neverAnswered := func(f *ssFixture, entered chan struct{}) {
		f.respond = func(ctx context.Context, command string) (any, error) {
			if command != callEndSession {
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
			}
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
	t.Run("gives up on an EndSession the provider never answers and still reports the untrackable session", func(t *testing.T) {
		f := newSSFixture()
		entered := make(chan struct{})
		neverAnswered(f, entered)
		done := make(chan error, 1)
		go func() {
			_, err := f.establish(wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}}, nil)
			done <- err
		}()
		<-entered
		f.clk.Advance(deviceCleanupBudget)
		if err := <-done; !errors.As(err, &local) || !strings.Contains(err.Error(), "produced a session with no stream usage") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("gives up on an EndSession the provider never answers when the requestor refuses the session", func(t *testing.T) {
		f := newSSFixture()
		f.upsertErr = errors.New("requestor endpoint is gone")
		entered := make(chan struct{})
		neverAnswered(f, entered)
		done := make(chan error, 1)
		go func() { _, err := f.establish(ssNew(nil), nil); done <- err }()
		<-entered
		f.clk.Advance(deviceCleanupBudget)
		if err := <-done; !errors.Is(err, f.upsertErr) {
			t.Errorf("err = %v", err)
		}
	})
}

// TestSessionStreamsRecordedOffersParityMatterjsServer is describe("recorded
// offers") (WebRtcSessionStreamsTest.ts:763-877, matterjs-server at
// 0bd06f2).
func TestSessionStreamsRecordedOffersParityMatterjsServer(t *testing.T) {
	newOffer := ssNew(func(r *wpdef.ProvideOfferRequest) { r.Sdp = "v=0" })
	t.Run("withdraws a new session's offer when the local requestor refuses the session", func(t *testing.T) {
		f := newSSFixture()
		f.upsertErr = errors.New("requestor endpoint is gone")
		_, _ = f.establish(newOffer, nil)
		if !slices.Equal(f.steps, []string{callProvideOffer, "offer:9", "upsert", callEndSession}) || f.offered(9) {
			t.Errorf("steps %v, kept %v", f.steps, f.offered(9))
		}
	})
	t.Run("keeps a new session's offer once the requestor holds the session", func(t *testing.T) {
		f := newSSFixture()
		if _, err := f.establish(newOffer, nil); err != nil || !f.offered(9) {
			t.Errorf("%v, kept %v", err, f.offered(9))
		}
	})
	t.Run("records no offer for a new session it cannot track", func(t *testing.T) {
		f := newSSFixture()
		_, _ = f.establish(wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}, Sdp: "v=0"}, nil)
		if f.offered(9) || !slices.Equal(f.steps, []string{callProvideOffer, callEndSession}) {
			t.Errorf("steps %v", f.steps)
		}
	})
	t.Run("withdraws a re-offer the camera refused, although the requestor holds the session", func(t *testing.T) {
		f := newSSFixture()
		f.held = []wrdef.WebRTCSessionStruct{{Id: 9, PeerNodeId: 5, PeerEndpointId: 1, FabricIndex: 1}}
		f.respond = func(context.Context, string) (any, error) { return nil, errors.New("busy") }
		_, _ = f.establish(ssReOffer(nil), nil)
		if f.offered(9) {
			t.Error("kept the refused re-offer")
		}
	})
	t.Run("withdraws a re-offer whose response names no session", func(t *testing.T) {
		f := newSSFixture()
		f.held = []wrdef.WebRTCSessionStruct{{Id: 9, PeerNodeId: 5, PeerEndpointId: 1, FabricIndex: 1}}
		f.respond = func(context.Context, string) (any, error) { return nil, nil }
		_, _ = f.establish(ssReOffer(nil), nil)
		if f.offered(9) {
			t.Error("kept the re-offer")
		}
	})
	t.Run("keeps an accepted re-offer of a session the requestor holds", func(t *testing.T) {
		f := newSSFixture()
		f.held = []wrdef.WebRTCSessionStruct{{Id: 9, PeerNodeId: 5, PeerEndpointId: 1, FabricIndex: 1}}
		if _, err := f.establish(ssReOffer(nil), nil); err != nil || !f.offered(9) || !slices.Equal(f.commands(), []string{callProvideOffer}) {
			t.Errorf("%v kept %v", err, f.offered(9))
		}
	})
	t.Run("withdraws an accepted re-offer when tracking it failed and the requestor does not hold it", func(t *testing.T) {
		f := newSSFixture()
		f.upsertErr = errors.New("requestor endpoint is gone")
		usage := wpdef.StreamUsageLiveView
		if _, err := f.establish(ssReOffer(func(r *wpdef.ProvideOfferRequest) { r.StreamUsage, r.VideoStreams = &usage, ssList(1) }), nil); err != nil {
			t.Fatal(err)
		}
		if f.offered(9) || !slices.Contains(f.steps, "upsert") {
			t.Errorf("steps %v, kept %v", f.steps, f.offered(9))
		}
	})
}

// TestSessionStreamsTrackedSessionOfParityMatterjsServer is describe("trackedSessionOf")
// (WebRtcSessionStreamsTest.ts:879-933, matterjs-server at 0bd06f2).
func TestSessionStreamsTrackedSessionOfParityMatterjsServer(t *testing.T) {
	session := func(id uint16, peer uint64, ep uint16, fabric uint8) wrdef.WebRTCSessionStruct {
		return wrdef.WebRTCSessionStruct{Id: id, PeerNodeId: peer, PeerEndpointId: ep, StreamUsage: 3, VideoStreams: ssList(1), FabricIndex: fabric}
	}
	keyA := func(id uint16) providerKey { return providerKey{id: id, fabricIndex: 1, peerNodeID: 5, peerEP: 1} }
	t.Run("names the session the given camera issued", func(t *testing.T) {
		if got, ok := trackedSessionOf([]wrdef.WebRTCSessionStruct{session(1, 5, 1, 1)}, keyA(1)); !ok || got.PeerNodeId != 5 {
			t.Errorf("%+v %v", got, ok)
		}
	})
	t.Run("names each camera's own session when two cameras issued the same id", func(t *testing.T) {
		if got, ok := trackedSessionOf([]wrdef.WebRTCSessionStruct{session(1, 6, 1, 1), session(1, 5, 1, 1)}, keyA(1)); !ok || got.PeerNodeId != 5 {
			t.Errorf("%+v %v", got, ok)
		}
	})
	for _, c := range []struct {
		name    string
		session wrdef.WebRTCSessionStruct
	}{
		{"does not name another camera's session carrying the same id", session(1, 6, 1, 1)},
		{"does not name the same camera's session on another fabric", session(1, 5, 1, 2)},
		{"does not name a session of another endpoint on the same node", session(1, 5, 2, 1)},
		{"reports nothing for an id no entry carries", session(2, 5, 1, 1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := trackedSessionOf([]wrdef.WebRTCSessionStruct{c.session}, keyA(1)); ok {
				t.Error("named it")
			}
		})
	}
}
