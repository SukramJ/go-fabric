// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// WebRtcTransportRequestor: a port of matter.js
// packages/node/test/behaviors/web-rtc-transport-requestor/
// WebRtcTransportRequestorServerTest.ts at 862ad75f, by meaning. Its 28
// `it` cases (37 with the four commands of :258-282 expanded) are the
// subtests of the three tests below. matter.js's withPeerContext (:52-64)
// invokes the server's methods under a mocked remote actor with a fabric
// and a peer node; here peer is the context the bridge stamps for a CASE
// invoke (im.WithFabricFilter, im.WithSubject) and the command runs
// through MatterInvoke. The ACL cases (:550-604) and one signal of each
// kind run over the wire on fabrictest.
package camera_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// makeSession is WebRtcTransportRequestorServerTest.ts:66-78.
func makeSession(edit ...func(*camera.WebRTCSession)) camera.WebRTCSession {
	video, audio := []uint16{10}, []uint16{20}
	s := camera.WebRTCSession{
		Id: 1, PeerNodeId: 42, PeerEndpointId: 1, StreamUsage: wrdef.StreamUsageLiveView,
		MetadataEnabled: false, VideoStreams: &video, AudioStreams: &audio, FabricIndex: 1,
	}
	for _, e := range edit {
		e(&s)
	}
	return s
}

func withID(id uint16) func(*camera.WebRTCSession) {
	return func(s *camera.WebRTCSession) { s.Id = id }
}

func withPeer(nodeID uint64) func(*camera.WebRTCSession) {
	return func(s *camera.WebRTCSession) { s.PeerNodeId = nodeID }
}

func withFabric(f uint8) func(*camera.WebRTCSession) {
	return func(s *camera.WebRTCSession) { s.FabricIndex = f }
}

func withUsage(u wrdef.StreamUsageEnum) func(*camera.WebRTCSession) {
	return func(s *camera.WebRTCSession) { s.StreamUsage = u }
}

// recorder is the RequestorPort and its refusal observer: the
// eventsOf(WebRtcTransportRequestorServer) listeners of the test.
type recorder struct {
	mu       sync.Mutex
	offers   []camera.WebRTCSession
	answers  []answered
	ice      int
	ends     []camera.WebRTCSession
	refusals []refusal
}

type answered struct {
	peerNodeID uint64
	sdp        string
}

type refusal struct {
	signal    camera.Signal
	sessionID uint16
}

func (r *recorder) Offer(_ context.Context, s camera.WebRTCSession, _ wrdef.OfferRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offers = append(r.offers, s)
}

func (r *recorder) Answer(_ context.Context, s camera.WebRTCSession, sdp string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers = append(r.answers, answered{s.PeerNodeId, sdp})
}

func (r *recorder) IceCandidates(context.Context, camera.WebRTCSession, []camera.ICECandidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ice++
}

func (r *recorder) End(_ context.Context, s camera.WebRTCSession, _ uint8) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, s)
}

func (r *recorder) Refused(_ context.Context, signal camera.Signal, id uint16, _ uint8, _ uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refusals = append(r.refusals, refusal{signal, id})
}

func newRequestor(t *testing.T, port camera.RequestorPort) *camera.Requestor {
	t.Helper()
	r, err := camera.NewRequestor(camera.RequestorConfig{Port: port})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func upsert(t *testing.T, r *camera.Requestor, sessions ...camera.WebRTCSession) {
	t.Helper()
	for _, s := range sessions {
		if err := r.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
}

// peer is withPeerContext (:52-64): a CASE invoke of fabricIndex's node
// peerNodeID.
func peer(fabricIndex uint8, peerNodeID uint64) context.Context {
	return im.WithSubject(im.WithFabricFilter(context.Background(), false, fabricIndex), peerNodeID, nil)
}

// The command cases of :226-256.
type commandCase struct {
	name   string
	invoke func(ctx context.Context, r *camera.Requestor, id uint16) error
}

var userHangup uint8 = 2 // WebRTCEndReasonEnum UserHangup (web-rtc-transport-definitions.element.ts:20)

var commandCases = []commandCase{
	{"offer", func(ctx context.Context, r *camera.Requestor, id uint16) error {
		_, err := r.MatterInvoke(ctx, wrdef.CmdOffer, wrdef.OfferRequest{WebRtcSessionId: id, Sdp: "x"})
		return err
	}},
	{"answer", func(ctx context.Context, r *camera.Requestor, id uint16) error {
		_, err := r.MatterInvoke(ctx, wrdef.CmdAnswer, wrdef.AnswerRequest{WebRtcSessionId: id, Sdp: "y"})
		return err
	}},
	{"iceCandidates", func(ctx context.Context, r *camera.Requestor, id uint16) error {
		_, err := r.MatterInvoke(ctx, wrdef.CmdIceCandidates, wrdef.IceCandidatesRequest{
			WebRtcSessionId: id,
			IceCandidates:   []camera.ICECandidate{{Candidate: "c", SdpMid: spec.ValueOf("0"), SdpmLineIndex: spec.ValueOf[uint16](0)}},
		})
		return err
	}},
	{"end", func(ctx context.Context, r *camera.Requestor, id uint16) error {
		_, err := r.MatterInvoke(ctx, wrdef.CmdEnd, wrdef.EndRequest{WebRtcSessionId: id, Reason: userHangup})
		return err
	}},
}

func wantStatus(t *testing.T, err error, want im.StatusCode) {
	t.Helper()
	var sc im.StatusCodeError
	if !errors.As(err, &sc) || sc.MatterStatusCode() != want {
		t.Fatalf("err %v, want status %s", err, want)
	}
}

func answer(r *camera.Requestor, fabric uint8, nodeID uint64, id uint16, sdp string) error {
	_, err := r.MatterInvoke(peer(fabric, nodeID), wrdef.CmdAnswer, wrdef.AnswerRequest{WebRtcSessionId: id, Sdp: sdp})
	return err
}

func end(r *camera.Requestor, fabric uint8, nodeID uint64, id uint16) error {
	_, err := r.MatterInvoke(peer(fabric, nodeID), wrdef.CmdEnd, wrdef.EndRequest{WebRtcSessionId: id, Reason: userHangup})
	return err
}

// TestParityMatterJS_RequestorUpsertRemove is "upsertSession /
// removeSession" (:81-202).
func TestParityMatterJS_RequestorUpsertRemove(t *testing.T) {
	t.Run("inserts a new session by id", func(t *testing.T) { // :94
		r := newRequestor(t, nil)
		upsert(t, r, makeSession(withID(1)))
		if got := r.Sessions(); len(got) != 1 || got[0].Id != 1 {
			t.Fatalf("sessions %+v", got)
		}
	})
	t.Run("replaces an existing session with the same id, fabric and peer node", func(t *testing.T) { // :104
		r := newRequestor(t, nil)
		upsert(t, r, makeSession(withUsage(wrdef.StreamUsageLiveView)), makeSession(withUsage(wrdef.StreamUsageRecording)))
		if got := r.Sessions(); len(got) != 1 || got[0].StreamUsage != wrdef.StreamUsageRecording {
			t.Fatalf("sessions %+v", got)
		}
	})
	t.Run("keeps sessions with the same id from different peer nodes", func(t *testing.T) { // :117
		r := newRequestor(t, nil)
		upsert(t, r, makeSession(withPeer(10)), makeSession(withPeer(99)))
		if got := peers(r.Sessions()); !slices.Equal(got, []uint64{10, 99}) {
			t.Fatalf("peer nodes %v", got)
		}
	})
	t.Run("keeps sessions with the same id and peer node on different fabrics", func(t *testing.T) { // :127
		r := newRequestor(t, nil)
		upsert(t, r, makeSession(withFabric(1)), makeSession(withFabric(2)))
		if got := fabrics(r.Sessions()); !slices.Equal(got, []uint8{1, 2}) {
			t.Fatalf("fabrics %v", got)
		}
	})
	t.Run("mirrors videoStreams[0] into deprecated videoStreamId scalar", func(t *testing.T) { // :137
		r := newRequestor(t, nil)
		upsert(t, r, makeSession())
		if v := r.Sessions()[0].VideoStreamId; v == nil || v.Null || v.Value != 10 {
			t.Fatalf("VideoStreamId %+v", v)
		}
	})
	t.Run("mirrors audioStreams[0] into deprecated audioStreamId scalar", func(t *testing.T) { // :146
		r := newRequestor(t, nil)
		upsert(t, r, makeSession())
		if v := r.Sessions()[0].AudioStreamId; v == nil || v.Null || v.Value != 20 {
			t.Fatalf("AudioStreamId %+v", v)
		}
	})
	t.Run("removes a session by its key", func(t *testing.T) { // :155
		r := newRequestor(t, nil)
		s := makeSession()
		upsert(t, r, s)
		r.RemoveSession(wrdef.KeyOf(s))
		if got := r.Sessions(); len(got) != 0 {
			t.Fatalf("sessions %+v", got)
		}
	})
	t.Run("removeSession is a no-op when the id is absent", func(t *testing.T) { // :166
		r := newRequestor(t, nil)
		s := makeSession()
		upsert(t, r, s)
		key := wrdef.KeyOf(s)
		key.ID = 999
		r.RemoveSession(key)
		if got := r.Sessions(); len(got) != 1 {
			t.Fatalf("sessions %+v", got)
		}
	})
	t.Run("removeSession keeps the session with the same id from another peer node", func(t *testing.T) { // :177
		r := newRequestor(t, nil)
		a, b := makeSession(withPeer(10)), makeSession(withPeer(99))
		upsert(t, r, a, b)
		r.RemoveSession(wrdef.KeyOf(b))
		if got := peers(r.Sessions()); !slices.Equal(got, []uint64{10}) {
			t.Fatalf("peer nodes %v", got)
		}
	})
	t.Run("removeSession keeps the session with the same id and peer node on another fabric", func(t *testing.T) { // :190
		r := newRequestor(t, nil)
		a, b := makeSession(withFabric(1)), makeSession(withFabric(2))
		upsert(t, r, a, b)
		r.RemoveSession(wrdef.KeyOf(b))
		if got := fabrics(r.Sessions()); !slices.Equal(got, []uint8{1}) {
			t.Fatalf("fabrics %v", got)
		}
	})
}

func peers(sessions []camera.WebRTCSession) []uint64 {
	out := make([]uint64, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.PeerNodeId)
	}
	return out
}

func fabrics(sessions []camera.WebRTCSession) []uint8 {
	out := make([]uint8, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.FabricIndex)
	}
	return out
}

// The fixture of "command identity checks and events" (:204-224).
const (
	fabric1   uint8  = 1
	peerNode  uint64 = 42
	sessionID uint16 = 7
)

func signallingRequestor(t *testing.T) (*camera.Requestor, *recorder) {
	t.Helper()
	rec := &recorder{}
	r := newRequestor(t, rec)
	upsert(t, r, makeSession(withID(sessionID), withPeer(peerNode), withFabric(fabric1)))
	return r, rec
}

// TestParityMatterJS_RequestorCommands is "command identity checks and
// events" (:204-548).
func TestParityMatterJS_RequestorCommands(t *testing.T) { //nolint:gocognit,gocyclo,funlen // one subtest per matter.js case
	for _, c := range commandCases { // :258-282
		t.Run(c.name+" returns NOT_FOUND when session id is unknown", func(t *testing.T) {
			r, _ := signallingRequestor(t)
			wantStatus(t, c.invoke(peer(fabric1, peerNode), r, 999), im.StatusNotFound)
		})
		t.Run(c.name+" returns NOT_FOUND when fabric mismatches", func(t *testing.T) {
			r, _ := signallingRequestor(t)
			wantStatus(t, c.invoke(peer(2, peerNode), r, sessionID), im.StatusNotFound)
		})
		t.Run(c.name+" returns NOT_FOUND when peer node id mismatches", func(t *testing.T) {
			r, _ := signallingRequestor(t)
			wantStatus(t, c.invoke(peer(fabric1, 99), r, sessionID), im.StatusNotFound)
		})
	}
	t.Run("refused names the signal and the unknown session id", func(t *testing.T) { // :284
		r, rec := signallingRequestor(t)
		_, err := r.MatterInvoke(peer(fabric1, peerNode), wrdef.CmdOffer, wrdef.OfferRequest{WebRtcSessionId: 999, Sdp: "offer"})
		wantStatus(t, err, im.StatusNotFound)
		if want := []refusal{{camera.Signal("offer"), 999}}; !slices.Equal(rec.refusals, want) {
			t.Fatalf("refusals %+v, want %+v", rec.refusals, want)
		}
	})
	t.Run("refused does not fire for a session the peer holds", func(t *testing.T) { // :302
		r, rec := signallingRequestor(t)
		if _, err := r.MatterInvoke(peer(fabric1, peerNode), wrdef.CmdOffer, wrdef.OfferRequest{WebRtcSessionId: sessionID, Sdp: "offer"}); err != nil {
			t.Fatal(err)
		}
		if len(rec.refusals) != 0 {
			t.Fatalf("refusals %+v", rec.refusals)
		}
	})
	t.Run("iceCandidates records no refusal for an empty list, which never reaches a session", func(t *testing.T) { // :318
		r, rec := signallingRequestor(t)
		_, _ = r.MatterInvoke(peer(fabric1, peerNode), wrdef.CmdIceCandidates, wrdef.IceCandidatesRequest{WebRtcSessionId: sessionID})
		if len(rec.refusals) != 0 {
			t.Fatalf("refusals %+v", rec.refusals)
		}
	})
	t.Run("iceCandidates returns CONSTRAINT_ERROR for an empty list", func(t *testing.T) { // :334
		r, _ := signallingRequestor(t)
		_, err := r.MatterInvoke(peer(fabric1, peerNode), wrdef.CmdIceCandidates, wrdef.IceCandidatesRequest{WebRtcSessionId: sessionID})
		wantStatus(t, err, im.StatusConstraintError)
	})
	t.Run("end removes the session and a subsequent answer returns NOT_FOUND", func(t *testing.T) { // :350
		r, _ := signallingRequestor(t)
		if err := end(r, fabric1, peerNode, sessionID); err != nil {
			t.Fatal(err)
		}
		wantStatus(t, answer(r, fabric1, peerNode, sessionID, "answer"), im.StatusNotFound)
	})
	t.Run("end keeps the sessions of other fabrics", func(t *testing.T) { // :368
		r, _ := signallingRequestor(t)
		upsert(t, r, makeSession(withID(sessionID+1), withPeer(peerNode), withFabric(2)))
		if err := end(r, fabric1, peerNode, sessionID); err != nil {
			t.Fatal(err)
		}
		if got := r.Sessions(); len(got) != 1 || got[0].Id != sessionID+1 {
			t.Fatalf("sessions %+v", got)
		}
	})

	// "with a session of the same id from another peer node" (:386-444).
	const otherPeer uint64 = 43
	withOtherPeer := func(t *testing.T) (*camera.Requestor, *recorder) {
		t.Helper()
		r, rec := signallingRequestor(t)
		upsert(t, r, makeSession(withID(sessionID), withPeer(otherPeer), withFabric(fabric1), withUsage(wrdef.StreamUsageRecording)))
		return r, rec
	}
	t.Run("delivers each peer's signaling with its own session", func(t *testing.T) { // :401
		r, rec := withOtherPeer(t)
		if err := answer(r, fabric1, peerNode, sessionID, "a"); err != nil {
			t.Fatal(err)
		}
		if err := answer(r, fabric1, otherPeer, sessionID, "b"); err != nil {
			t.Fatal(err)
		}
		if want := []answered{{peerNode, "a"}, {otherPeer, "b"}}; !slices.Equal(rec.answers, want) {
			t.Fatalf("answers %+v, want %+v", rec.answers, want)
		}
	})
	t.Run("end from one peer keeps the other peer's session", func(t *testing.T) { // :423
		r, _ := withOtherPeer(t)
		if err := end(r, fabric1, peerNode, sessionID); err != nil {
			t.Fatal(err)
		}
		if got := r.Sessions(); len(got) != 1 || got[0].Id != sessionID || got[0].PeerNodeId != otherPeer {
			t.Fatalf("sessions %+v", got)
		}
		if err := answer(r, fabric1, otherPeer, sessionID, "b"); err != nil {
			t.Fatal(err)
		}
	})

	// "with a session of the same id and peer node on another fabric"
	// (:446-477).
	t.Run("end on one fabric keeps the session on the other fabric", func(t *testing.T) { // :456
		r, _ := signallingRequestor(t)
		const otherFabric uint8 = 2
		upsert(t, r, makeSession(withID(sessionID), withPeer(peerNode), withFabric(otherFabric)))
		if err := end(r, fabric1, peerNode, sessionID); err != nil {
			t.Fatal(err)
		}
		if got := r.Sessions(); len(got) != 1 || got[0].Id != sessionID || got[0].FabricIndex != otherFabric {
			t.Fatalf("sessions %+v", got)
		}
		if err := answer(r, otherFabric, peerNode, sessionID, "b"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("offer emits the offer observable exactly once", func(t *testing.T) { // :479
		r, rec := signallingRequestor(t)
		if _, err := r.MatterInvoke(peer(fabric1, peerNode), wrdef.CmdOffer, wrdef.OfferRequest{WebRtcSessionId: sessionID, Sdp: "offer"}); err != nil {
			t.Fatal(err)
		}
		if len(rec.offers) != 1 {
			t.Fatalf("%d offers", len(rec.offers))
		}
	})
	t.Run("answer emits the answer observable exactly once", func(t *testing.T) { // :495
		r, rec := signallingRequestor(t)
		if err := answer(r, fabric1, peerNode, sessionID, "answer"); err != nil {
			t.Fatal(err)
		}
		if len(rec.answers) != 1 {
			t.Fatalf("%d answers", len(rec.answers))
		}
	})
	t.Run("iceCandidates emits the iceCandidates observable exactly once", func(t *testing.T) { // :511
		r, rec := signallingRequestor(t)
		candidate := camera.ICECandidate{
			Candidate: "candidate:1 1 udp 2113937151 192.168.1.1 54321 typ host",
			SdpMid:    spec.ValueOf("0"), SdpmLineIndex: spec.ValueOf[uint16](0),
		}
		if _, err := r.MatterInvoke(peer(fabric1, peerNode), wrdef.CmdIceCandidates, wrdef.IceCandidatesRequest{
			WebRtcSessionId: sessionID, IceCandidates: []camera.ICECandidate{candidate},
		}); err != nil {
			t.Fatal(err)
		}
		if rec.ice != 1 {
			t.Fatalf("%d iceCandidates", rec.ice)
		}
	})
	t.Run("end emits the end observable exactly once", func(t *testing.T) { // :533
		r, rec := signallingRequestor(t)
		if err := end(r, fabric1, peerNode, sessionID); err != nil {
			t.Fatal(err)
		}
		if len(rec.ends) != 1 {
			t.Fatalf("%d ends", len(rec.ends))
		}
	})
}

const deviceTypeCameraController = 0x0147

// cameraPair commissions a device whose "camera-controller" endpoint
// serves r (createTestNode, :29-50). The four signalling commands are
// large messages (quality L, web-rtc-transport-requestor.element.ts:27-61), which
// a controller sends over TCP only: the site runs on loopback sockets and
// both sides speak TCP.
func cameraPair(t *testing.T, r *camera.Requestor) (*fabrictest.Site, *fabrictest.Pair, uint16) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true})
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{
			Bridged: []node.Endpoint{fabrictest.Endpoint("camera-controller", deviceTypeCameraController, r)},
			Configure: func(cfg *node.Config) {
				cfg.Network.TCP = node.TCPConfig{Incoming: true, Outgoing: true}
			},
		},
		Controller: fabrictest.ControllerOptions{TCP: controller.TCPConfig{Outgoing: true}},
	})
	ep, ok := pair.Device.EndpointOf("camera-controller")
	if !ok {
		t.Fatal("no camera-controller endpoint")
	}
	return site, pair, ep
}

func wireCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// requestorEntries are the ACL entries of :575-582 on the device's fabric.
func requestorEntries(t *testing.T, pair *fabrictest.Pair, ep uint16) int {
	t.Helper()
	ctx := wireCtx(t)
	fabs, err := pair.Device.Store().ListFabrics(ctx)
	if err != nil || len(fabs) == 0 {
		t.Fatalf("fabrics %v, %v", fabs, err)
	}
	acl, err := pair.Device.Store().ListACL(ctx, fabs[0].FabricIndex)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range acl {
		if e.Privilege == store.PrivilegeOperate && e.AuthMode == store.AuthModeCASE && len(e.Targets) == 1 &&
			e.Targets[0].Cluster != nil && *e.Targets[0].Cluster == wrdef.ClusterID &&
			e.Targets[0].Endpoint != nil && *e.Targets[0].Endpoint == ep {
			n++
		}
	}
	return n
}

// TestParityMatterJS_RequestorACLInstall is "ACL install" (:550-604): the
// node installs the Operate entry when the first fabric arrives, and once
// only. The repeated online event of :586-603 is a restart of the device,
// which runs the installation again with the fabric present.
func TestParityMatterJS_RequestorACLInstall(t *testing.T) {
	t.Run("ACL install adds one entry on first online", func(t *testing.T) { // :570
		_, pair, ep := cameraPair(t, newRequestor(t, nil))
		if n := requestorEntries(t, pair, ep); n != 1 {
			t.Fatalf("%d requestor entries, want 1", n)
		}
	})
	t.Run("does not duplicate the entry across repeated online events", func(t *testing.T) { // :586
		_, pair, ep := cameraPair(t, newRequestor(t, nil))
		if err := pair.Device.Restart(wireCtx(t)); err != nil {
			t.Fatal(err)
		}
		if n := requestorEntries(t, pair, ep); n != 1 {
			t.Fatalf("%d requestor entries, want 1", n)
		}
	})
}

// TestRequestorSignallingOverTheWire runs one signal of each outcome over
// a CASE session: the controller's node id and the device's fabric index
// are the session's, a tracked session is surfaced, an unknown one is
// NOT_FOUND, and CurrentSessions reads the fabric's sessions only.
func TestRequestorSignallingOverTheWire(t *testing.T) {
	rec := &recorder{}
	r := newRequestor(t, rec)
	_, pair, ep := cameraPair(t, r)
	ctx := wireCtx(t)
	fabs, err := pair.Device.Store().ListFabrics(ctx)
	if err != nil || len(fabs) == 0 {
		t.Fatalf("fabrics %v, %v", fabs, err)
	}
	own := fabs[0].FabricIndex
	upsert(t, r,
		makeSession(withID(sessionID), withPeer(bridgetest.DefaultControllerNodeID), withFabric(own)),
		makeSession(withID(sessionID), withPeer(bridgetest.DefaultControllerNodeID), withFabric(own+1)))
	path := func(cmd uint32) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: ep, Cluster: wrdef.ClusterID, Command: cmd, HasEndpoint: true}
	}
	res, err := fabrictest.Invoke(ctx, pair, path(wrdef.CmdAnswer), wrdef.AnswerRequest{WebRtcSessionId: sessionID, Sdp: "v=0"})
	if err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("Answer: %+v, %v", res, err)
	}
	if want := []answered{{bridgetest.DefaultControllerNodeID, "v=0"}}; !slices.Equal(rec.answers, want) {
		t.Fatalf("answers %+v", rec.answers)
	}
	res, err = fabrictest.Invoke(ctx, pair, path(wrdef.CmdOffer), wrdef.OfferRequest{WebRtcSessionId: 999, Sdp: "v=0"})
	if res.Status != im.StatusNotFound {
		t.Fatalf("Offer of an unknown session: %+v, %v", res, err)
	}
	d, err := fabrictest.Read(ctx, pair, im.ConcreteAttributePath{Endpoint: ep, Cluster: wrdef.ClusterID, Attribute: wrdef.AttrCurrentSessions, HasEndpoint: true, HasCluster: true, HasAttribute: true})
	if err != nil {
		t.Fatal(err)
	}
	v, err := wrdef.Definition.Attribute(wrdef.AttrCurrentSessions).Decode(d.Value)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.(spec.List[camera.WebRTCSession]); len(got) != 1 || got[0].FabricIndex != own {
		t.Fatalf("CurrentSessions %+v, want the own fabric's session only", got)
	}
}
