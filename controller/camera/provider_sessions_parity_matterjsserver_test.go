// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/im"
)

// The cases of matterjs-server's
// packages/ws-controller/test/WebRtcProviderSessionsTest.ts:159-464 at
// 0bd06f2, ported by meaning onto the service's session layer, its watch
// and its signal routing: attributeChanged is a changed CurrentSessions in
// the image at the batch end, eventTriggered the StartUp filter of the
// reports, an answer event a requestor signal routed through the service.

const (
	psCamera        = uint64(5)
	psOtherCamera   = uint64(6)
	psEndpoint      = uint16(1)
	psOtherEndpoint = uint16(2)
	psServer        = uint64(100)
	psFabric        = uint8(1)
	psOtherFabric   = uint8(2)
	psRequestorEP   = uint16(3)
)

const (
	psOffer    = "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=recvonly\r\n"
	psAnswer   = "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=sendrecv\r\n"
	psNarrowed = "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=sendonly\r\n"
)

// psFixture is the fixture of WebRtcProviderSessionsTest.ts:62-138,
// matterjs-server at 0bd06f2: a camera's CurrentSessions per endpoint, the
// requestor's table, every invoke and time push, the reports, and the
// signals the service routes.
type psFixture struct {
	mu        sync.Mutex
	camera    map[uint16][]sessionPeer
	requestor []wrdef.WebRTCSessionStruct
	calls     []string
	reports   []string
	signals   []Signal
	respond   func(command string) (any, error)
	svc       *Service
}

func (f *psFixture) providerEndpoints(uint64) []uint16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uint16, 0, len(f.camera))
	for ep := range f.camera {
		out = append(out, ep)
	}
	slices.Sort(out)
	return out
}

func (f *psFixture) currentSessions(_ uint64, ep uint16) (sessions []sessionPeer, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sessions, ok = f.camera[ep]
	return slices.Clone(sessions), ok
}

func (f *psFixture) providerRevision(t target) (revision uint16, known bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.camera[t.endpointID]; !ok {
		return 0, false, invalidArguments("no provider on %d", t.nodeID)
	}
	return 2, true, nil
}

// psShown is a request's fields without the SDP, as the fixture shows
// them.
func psShown(fields spec.Encodable) string {
	switch f := fields.(type) {
	case wpdef.ProvideOfferRequest:
		f.Sdp = ""
		return fmt.Sprintf("%+v", f)
	case wpdef.EndSessionRequest:
		return fmt.Sprintf(`{"webRtcSessionId":%d,"reason":%d}`, f.WebRtcSessionId, f.Reason)
	default:
		return fmt.Sprintf("%+v", f)
	}
}

func (f *psFixture) invokeProvider(_ context.Context, t target, command string, fields spec.Encodable) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("%s:%d/%d:%s", command, t.nodeID, t.endpointID, psShown(fields)))
	respond := f.respond
	f.mu.Unlock()
	if respond != nil {
		return respond(command)
	}
	if command == callSolicitOffer {
		return wpdef.SolicitOfferResponse{WebRtcSessionId: 9}, nil
	}
	return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
}

func (f *psFixture) syncTime(_ context.Context, nodeID uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("sync:%d", nodeID))
	return nil
}

func (f *psFixture) endpointID() uint16 { return psRequestorEP }

func (f *psFixture) sessions() []wrdef.WebRTCSessionStruct {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requestor)
}

func (f *psFixture) upsert(s wrdef.WebRTCSessionStruct) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := slices.IndexFunc(f.requestor, func(o wrdef.WebRTCSessionStruct) bool { return wrdef.KeyOf(o) == wrdef.KeyOf(s) }); i >= 0 {
		f.requestor[i] = s
	} else {
		f.requestor = append(f.requestor, s)
	}
	return nil
}

func (f *psFixture) remove(k wrdef.SessionKey) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requestor = slices.DeleteFunc(f.requestor, func(o wrdef.WebRTCSessionStruct) bool { return wrdef.KeyOf(o) == k })
}

func (f *psFixture) setCamera(ep uint16, s []sessionPeer) {
	f.mu.Lock()
	f.camera[ep] = s
	f.mu.Unlock()
}

func (f *psFixture) callsNow() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *psFixture) reportsNow() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reports)
}

// trackedSession is the fixture's trackedSession
// (WebRtcProviderSessionsTest.ts:46-57, matterjs-server at 0bd06f2).
func trackedSession(edit func(*wrdef.WebRTCSessionStruct)) wrdef.WebRTCSessionStruct {
	s := wrdef.WebRTCSessionStruct{Id: 9, PeerNodeId: psCamera, PeerEndpointId: psEndpoint, StreamUsage: 3, VideoStreams: &[]uint16{1}, FabricIndex: psFabric}
	if edit != nil {
		edit(&s)
	}
	return s
}

// newPSFixture is a service over the fixture: its session layer, its
// watch and its routing, with no controller under it.
func newPSFixture() *psFixture {
	f := &psFixture{camera: map[uint16][]sessionPeer{psEndpoint: {}}}
	clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	log := slog.New(slog.DiscardHandler)
	s := &Service{log: log, sessions: map[sessionKey]*Session{}, lastSeen: map[watchKey][]sessionPeer{}, image: f, localNodeID: func() uint64 { return psServer }}
	s.m = newManager(&fakeIO{}, clk, log)
	s.reported = func(node uint64, ep uint16, present map[uint16]bool, restarted bool) {
		ids := make([]string, 0, len(present))
		for id := range present {
			ids = append(ids, strconv.FormatUint(uint64(id), 10))
		}
		slices.Sort(ids)
		f.mu.Lock()
		f.reports = append(f.reports, fmt.Sprintf("%d/%d [%s] restarted=%v", node, ep, strings.Join(ids, ","), restarted))
		f.mu.Unlock()
	}
	s.w = newSessionWatch(context.Background(), serviceWatch{s}, clk, log)
	s.ps = &providerSessions{nodes: f, requestor: f, fabricIndex: psFabric, watch: s.w, log: log, cleanup: s.m.cleanupContext, offers: map[providerKey]string{}}
	s.cfg.Observer.Signal = func(sig Signal) {
		f.mu.Lock()
		f.signals = append(f.signals, sig)
		f.mu.Unlock()
	}
	f.svc = s
	return f
}

// pastFirstBatch: the first batch end reports and sweeps every provider
// endpoint (WebRtcProviderSessionsTest.ts:140-144, matterjs-server at
// 0bd06f2).
func (f *psFixture) pastFirstBatch() {
	f.batch()
	f.mu.Lock()
	f.reports = nil
	f.mu.Unlock()
}

// batch is one report batch end; the sweeps it starts are awaited.
func (f *psFixture) batch() {
	f.svc.reportCompleted(psCamera)
	f.svc.w.wait()
}

// answerSDP routes the camera's Answer for the session and returns the
// SDP the service passed on (WebRtcProviderSessionsTest.ts:157-161,
// matterjs-server at 0bd06f2).
func (f *psFixture) answerSDP(s wrdef.WebRTCSessionStruct) string {
	f.svc.onSignal(Signal{Kind: controller.CameraSignalAnswer, Session: s, SDP: psAnswer})
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.signals[len(f.signals)-1].SDP
}

func psOfferRequest(edit func(*wpdef.ProvideOfferRequest)) wpdef.ProvideOfferRequest {
	usage := wpdef.StreamUsageLiveView
	r := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}, Sdp: psOffer, StreamUsage: &usage, VideoStreams: &[]uint16{1}}
	if edit != nil {
		edit(&r)
	}
	return r
}

func (f *psFixture) establish(t *testing.T, ep uint16, req spec.Encodable) (any, error) {
	t.Helper()
	return f.svc.ps.establish(context.Background(), target{psCamera, ep}, req, nil)
}

// TestProviderSessionsWatchParityMatterjsServer is describe("CurrentSessions
// watch") (WebRtcProviderSessionsTest.ts:160-294, matterjs-server at
// 0bd06f2).
func TestProviderSessionsWatchParityMatterjsServer(t *testing.T) {
	t.Run("reports a CurrentSessions change at the end of the report batch", func(t *testing.T) {
		f := newPSFixture()
		f.pastFirstBatch()
		f.setCamera(psEndpoint, []sessionPeer{{3, psServer}})
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id = 3 }))
		if got := f.reportsNow(); len(got) != 0 {
			t.Fatalf("reported before the batch end: %q", got)
		}
		f.batch()
		if got := f.reportsNow(); !slices.Equal(got, []string{"5/1 [3] restarted=false"}) {
			t.Errorf("reports = %q", got)
		}
	})
	t.Run("ignores other attributes of the provider and the same attribute id of another cluster", func(t *testing.T) {
		// The image is compared on CurrentSessions only, so a change of
		// another attribute, or of another cluster, reaches nothing.
		f := newPSFixture()
		f.pastFirstBatch()
		f.batch()
		if got := f.reportsNow(); len(got) != 0 {
			t.Errorf("reports = %q", got)
		}
	})
	t.Run("marks the report as a restart after a StartUp event, and not after another event or another cluster's event", func(t *testing.T) {
		f := newPSFixture()
		f.pastFirstBatch()
		for _, e := range []struct {
			cluster, event uint32
		}{{basicinformation.ClusterID, basicinformation.EventShutDown}, {0x6, basicinformation.EventStartUp}} {
			if isStartUp(0, e.cluster, e.event) {
				f.svc.w.startedUp(psCamera)
			}
		}
		f.setCamera(psEndpoint, []sessionPeer{{4, psOtherCamera}})
		f.batch()
		if isStartUp(0, basicinformation.ClusterID, basicinformation.EventStartUp) {
			f.svc.w.startedUp(psCamera)
		}
		f.setCamera(psEndpoint, []sessionPeer{})
		f.batch()
		if got := f.reportsNow(); !slices.Equal(got, []string{"5/1 [] restarted=false", "5/1 [] restarted=true"}) {
			t.Errorf("reports = %q", got)
		}
	})
	t.Run("ends a leftover session with UnknownReason, also when the requestor holds its id for another endpoint or fabric", func(t *testing.T) {
		f := newPSFixture()
		f.setCamera(psEndpoint, []sessionPeer{{1, psServer}, {2, psServer}, {3, psServer}})
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id = 1 }))
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id, s.PeerEndpointId = 2, psOtherEndpoint }))
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id, s.FabricIndex = 3, psOtherFabric }))
		f.batch()
		want := []string{`endSession:5/1:{"webRtcSessionId":2,"reason":12}`, `endSession:5/1:{"webRtcSessionId":3,"reason":12}`}
		if got := f.callsNow(); !slices.Equal(got, want) {
			t.Errorf("calls = %q, want %q", got, want)
		}
	})
	t.Run("waits with the sweep until an offer in flight has its session in the requestor", func(t *testing.T) {
		f := newPSFixture()
		f.setCamera(psEndpoint, []sessionPeer{{9, psServer}})
		gate, asked := make(chan struct{}), make(chan struct{})
		f.respond = func(string) (any, error) {
			close(asked)
			<-gate
			return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
		}
		done := make(chan error, 1)
		go func() { _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); done <- err }()
		<-asked
		f.batch()
		close(gate)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		f.batch()
		for _, c := range f.callsNow() {
			if strings.HasPrefix(c, "endSession") {
				t.Errorf("swept %q", c)
			}
		}
	})
	t.Run("forgets a removed node: drops its requestor sessions and sweeps it anew", func(t *testing.T) {
		f := newPSFixture()
		f.batch()
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id = 1 }))
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id, s.FabricIndex = 2, psOtherFabric }))
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id, s.PeerNodeId = 3, psOtherCamera }))
		f.svc.nodeRemoved(psCamera)
		f.setCamera(psEndpoint, []sessionPeer{{7, psServer}})
		f.batch()
		left := make([]string, 0, 2)
		for _, s := range f.sessions() {
			left = append(left, fmt.Sprintf("%d/%d", s.PeerNodeId, s.Id))
		}
		if !slices.Equal(left, []string{"5/2", "6/3"}) {
			t.Errorf("requestor = %q", left)
		}
		n := 0
		for _, c := range f.callsNow() {
			if strings.HasPrefix(c, `endSession:5/1:{"webRtcSessionId":7`) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("leftover 7 ended %d times", n)
		}
	})
	t.Run("drops the offer of a removed node's session", func(t *testing.T) {
		f := newPSFixture()
		if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
			t.Fatal(err)
		}
		f.svc.nodeRemoved(psCamera)
		if got := f.answerSDP(trackedSession(nil)); got != psAnswer {
			t.Errorf("answer %q", got)
		}
	})
	t.Run("sweeps an added endpoint at the next batch end", func(t *testing.T) {
		f := newPSFixture()
		f.batch()
		f.setCamera(psOtherEndpoint, []sessionPeer{{4, psServer}})
		f.batch()
		if got := f.callsNow(); len(got) != 0 {
			t.Fatalf("calls before endpointAdded = %q", got)
		}
		f.svc.w.endpointAdded(psCamera)
		f.batch()
		if got := f.callsNow(); len(got) != 1 || !strings.HasPrefix(got[0], `endSession:5/2:{"webRtcSessionId":4,`) {
			t.Errorf("calls = %q", got)
		}
	})
}

// TestProviderSessionsEstablishParityMatterjsServer is describe("establish")
// (WebRtcProviderSessionsTest.ts:296-383, matterjs-server at 0bd06f2).
func TestProviderSessionsEstablishParityMatterjsServer(t *testing.T) {
	t.Run("stores the session with the requestor and sends the requestor endpoint as originating endpoint", func(t *testing.T) {
		f := newPSFixture()
		resp, err := f.establish(t, psEndpoint, psOfferRequest(nil))
		if err != nil {
			t.Fatal(err)
		}
		if id, _, _, _ := sessionIDOf(resp); id != 9 {
			t.Errorf("session %d", id)
		}
		calls := f.callsNow()
		if len(calls) != 1 || !strings.HasPrefix(calls[0], "provideOffer:5/1:") || !strings.Contains(calls[0], "OriginatingEndpointId:0x") {
			t.Fatalf("calls = %q", calls)
		}
		sent := f.svc.ps
		_ = sent
		got := make([][3]int, 0, 1)
		for _, s := range f.sessions() {
			got = append(got, [3]int{int(s.Id), int(s.PeerEndpointId), int(s.FabricIndex)})
		}
		if !slices.Equal(got, [][3]int{{9, int(psEndpoint), int(psFabric)}}) {
			t.Errorf("requestor = %v", got)
		}
	})
	t.Run("narrows the camera's answer to the offer it sent", func(t *testing.T) {
		f := newPSFixture()
		if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
			t.Fatal(err)
		}
		if got := f.answerSDP(f.sessions()[0]); got != psNarrowed {
			t.Errorf("answer %q", got)
		}
	})
	t.Run("records the offer under the endpoint it went to", func(t *testing.T) {
		f := newPSFixture()
		if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
			t.Fatal(err)
		}
		if got := f.answerSDP(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.PeerEndpointId = psOtherEndpoint })); got != psAnswer {
			t.Errorf("answer %q", got)
		}
	})
	t.Run("drops the offer of a session it no longer tracks", func(t *testing.T) {
		f := newPSFixture()
		if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
			t.Fatal(err)
		}
		f.svc.ps.removeTracked(target{psCamera, psEndpoint}, 9)
		if len(f.sessions()) != 0 {
			t.Errorf("requestor = %v", f.sessions())
		}
		if got := f.answerSDP(trackedSession(nil)); got != psAnswer {
			t.Errorf("answer %q", got)
		}
	})
	t.Run("keeps the requestor's session on another endpoint when removing one", func(t *testing.T) {
		f := newPSFixture()
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.PeerEndpointId = psOtherEndpoint }))
		f.svc.ps.removeTracked(target{psCamera, psEndpoint}, 9)
		if len(f.sessions()) != 1 {
			t.Errorf("requestor = %v", f.sessions())
		}
	})
	reOffer := func(edit func(*wpdef.ProvideOfferRequest)) wpdef.ProvideOfferRequest {
		r := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Value: 9}, Sdp: psOffer}
		if edit != nil {
			edit(&r)
		}
		return r
	}
	t.Run("withdraws a re-offer the requestor does not hold", func(t *testing.T) {
		f := newPSFixture()
		if _, err := f.establish(t, psEndpoint, reOffer(nil)); err != nil {
			t.Fatal(err)
		}
		_ = f.upsert(trackedSession(nil))
		if got := f.answerSDP(trackedSession(nil)); got != psAnswer {
			t.Errorf("answer %q", got)
		}
	})
	t.Run("keeps a re-offer of a session the requestor holds", func(t *testing.T) {
		f := newPSFixture()
		_ = f.upsert(trackedSession(nil))
		if _, err := f.establish(t, psEndpoint, reOffer(nil)); err != nil {
			t.Fatal(err)
		}
		if got := f.answerSDP(trackedSession(nil)); got != psNarrowed {
			t.Errorf("answer %q", got)
		}
	})
	t.Run("keeps the requestor's entry when a re-offer restates other streams", func(t *testing.T) {
		f := newPSFixture()
		_ = f.upsert(trackedSession(nil))
		usage := wpdef.StreamUsageLiveView
		if _, err := f.establish(t, psEndpoint, reOffer(func(r *wpdef.ProvideOfferRequest) { r.StreamUsage, r.VideoStreams = &usage, &[]uint16{2} })); err != nil {
			t.Fatal(err)
		}
		if got := f.sessions(); len(got) != 1 || !slices.Equal(*got[0].VideoStreams, []uint16{1}) {
			t.Errorf("requestor = %+v", got)
		}
	})
	t.Run("sends nothing to an endpoint without a WebRTC provider", func(t *testing.T) {
		f := newPSFixture()
		_, err := f.establish(t, psOtherEndpoint, psOfferRequest(nil))
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("err = %v", err)
		}
		if got := f.callsNow(); len(got) != 0 {
			t.Errorf("calls = %q", got)
		}
	})
}

// TestProviderSessionsClockParityMatterjsServer is describe("clock for a TLS
// ICE server") (WebRtcProviderSessionsTest.ts:385-445, matterjs-server at
// 0bd06f2).
func TestProviderSessionsClockParityMatterjsServer(t *testing.T) {
	turns := &[]wpdef.ICEServerStruct{{Urls: []string{"turns:turn.example.org"}}}
	refuseFirst := func(f *psFixture) {
		offers := 0
		f.respond = func(command string) (any, error) {
			offers++
			if offers == 1 {
				return nil, statusErr(im.StatusInvalidInState)
			}
			if command == callSolicitOffer {
				return wpdef.SolicitOfferResponse{WebRtcSessionId: 9}, nil
			}
			return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
		}
	}
	commands := func(f *psFixture) []string {
		calls := f.callsNow()
		out := make([]string, 0, len(calls))
		for _, c := range calls {
			out = append(out, strings.SplitN(c, ":", 2)[0])
		}
		return out
	}
	t.Run("sends an offer the camera accepts once, with nothing before it", func(t *testing.T) {
		f := newPSFixture()
		if _, err := f.establish(t, psEndpoint, psOfferRequest(func(r *wpdef.ProvideOfferRequest) { r.IceServers = turns })); err != nil {
			t.Fatal(err)
		}
		if got := commands(f); !slices.Equal(got, []string{"provideOffer"}) {
			t.Errorf("calls %q", got)
		}
	})
	t.Run("pushes time and sends the offer again after an INVALID_IN_STATE refusal, then tracks the session", func(t *testing.T) {
		f := newPSFixture()
		refuseFirst(f)
		if _, err := f.establish(t, psEndpoint, psOfferRequest(func(r *wpdef.ProvideOfferRequest) { r.IceServers = turns })); err != nil {
			t.Fatal(err)
		}
		if got := commands(f); !slices.Equal(got, []string{"provideOffer", "sync", "provideOffer"}) {
			t.Errorf("calls %q", got)
		}
		if got := f.sessions(); len(got) != 1 || got[0].Id != 9 {
			t.Errorf("requestor %+v", got)
		}
	})
	t.Run("does the same for a SolicitOffer", func(t *testing.T) {
		f := newPSFixture()
		refuseFirst(f)
		req := wpdef.SolicitOfferRequest{StreamUsage: wpdef.StreamUsageLiveView, VideoStreams: &[]uint16{1}, IceServers: turns}
		if _, err := f.establish(t, psEndpoint, req); err != nil {
			t.Fatal(err)
		}
		if got := commands(f); !slices.Equal(got, []string{"solicitOffer", "sync", "solicitOffer"}) {
			t.Errorf("calls %q", got)
		}
	})
	t.Run("hands the camera's second INVALID_IN_STATE refusal to the caller", func(t *testing.T) {
		// WebRtcProviderDeviceTest.ts:340-349, matterjs-server at 0bd06f2.
		f := newPSFixture()
		f.respond = func(string) (any, error) { return nil, statusErr(im.StatusInvalidInState) }
		_, err := f.establish(t, psEndpoint, psOfferRequest(func(r *wpdef.ProvideOfferRequest) { r.IceServers = turns }))
		if !statusIs(err, im.StatusInvalidInState) {
			t.Errorf("err = %v", err)
		}
		if got := commands(f); !slices.Equal(got, []string{"provideOffer", "sync", "provideOffer"}) {
			t.Errorf("calls %q", got)
		}
	})
	t.Run("does not retry a re-offer", func(t *testing.T) {
		f := newPSFixture()
		refuseFirst(f)
		_, err := f.establish(t, psEndpoint, wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Value: 9}, Sdp: psOffer, IceServers: turns})
		if !statusIs(err, im.StatusInvalidInState) {
			t.Errorf("err = %v", err)
		}
		if got := commands(f); !slices.Equal(got, []string{"provideOffer"}) {
			t.Errorf("calls %q", got)
		}
	})
}

// TestProviderSessionsAbandonParityMatterjsServer is describe("abandon")
// (WebRtcProviderSessionsTest.ts:447-463, matterjs-server at 0bd06f2).
func TestProviderSessionsAbandonParityMatterjsServer(t *testing.T) {
	t.Run("stops tracking the session and ends it at the next batch end if the camera still lists it", func(t *testing.T) {
		f := newPSFixture()
		f.batch()
		if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
			t.Fatal(err)
		}
		f.setCamera(psEndpoint, []sessionPeer{{9, psServer}})
		f.mu.Lock()
		f.calls = nil
		f.mu.Unlock()
		f.svc.ps.abandon(target{psCamera, psEndpoint}, 9)
		f.batch()
		if len(f.sessions()) != 0 {
			t.Errorf("requestor %+v", f.sessions())
		}
		if got := f.callsNow(); len(got) != 1 || !strings.HasPrefix(got[0], `endSession:5/1:{"webRtcSessionId":9,`) {
			t.Errorf("calls %q", got)
		}
	})
}
