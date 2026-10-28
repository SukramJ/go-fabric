// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// WebRtcTransportProvider handler rules: chip has no handler tests, so
// each case here is the parity proof of the connectedhomeip line it cites
// (WebRTCTransportProviderCluster.cpp at 6170af84; the stream answers are
// the camera app's, camera-av-stream-manager.cpp).
package camera_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/im"
)

// fakeStreams is a StreamSource.
type fakeStreams struct {
	videos                  []camera.VideoStream
	audios                  []camera.AudioStream
	priorities, supported   []camera.StreamUsage
	hard, softRec, softLive bool
}

func (f *fakeStreams) VideoStreams() []camera.VideoStream          { return f.videos }
func (f *fakeStreams) AudioStreams() []camera.AudioStream          { return f.audios }
func (f *fakeStreams) StreamUsagePriorities() []camera.StreamUsage { return f.priorities }
func (f *fakeStreams) SupportedStreamUsages() []camera.StreamUsage { return f.supported }
func (f *fakeStreams) PrivacyModes() (hard, rec, live bool)        { return f.hard, f.softRec, f.softLive }

const (
	liveView  = csdef.StreamUsageLiveView
	recording = csdef.StreamUsageRecording
)

// cameraStreams: video 1 (LiveView) and 2 (Recording), audio 3 (LiveView).
func cameraStreams() *fakeStreams {
	return &fakeStreams{
		videos:     []camera.VideoStream{{VideoStreamId: 1, StreamUsage: liveView, KeyFrameInterval: 1000}, {VideoStreamId: 2, StreamUsage: recording, KeyFrameInterval: 1000}},
		audios:     []camera.AudioStream{{AudioStreamId: 3, StreamUsage: liveView}},
		priorities: []camera.StreamUsage{liveView, recording},
		supported:  []camera.StreamUsage{liveView, recording},
	}
}

func nv(v uint16) *spec.Nullable[uint16] { x := spec.ValueOf(v); return &x }
func nnull() *spec.Nullable[uint16]      { x := spec.NullOf[uint16](); return &x }

func solicit(usage wpdef.StreamUsageEnum, edit ...func(*wpdef.SolicitOfferRequest)) wpdef.SolicitOfferRequest {
	r := wpdef.SolicitOfferRequest{StreamUsage: usage, OriginatingEndpointId: 1, VideoStreamId: nnull()}
	for _, e := range edit {
		e(&r)
	}
	return r
}

func TestProviderConfig(t *testing.T) {
	if _, err := camera.NewProvider(camera.ProviderConfig{}); !errors.Is(err, camera.ErrNoProviderPort) {
		t.Fatal(err)
	}
}

func TestSolicitOfferRules(t *testing.T) {
	ctx := peer(1, 0x55)
	usage := wpdef.StreamUsageLiveView
	urls := func(n int) []string { return make([]string, n) }
	str := func(n int) *string { s := strings.Repeat("x", n); return &s }
	cases := []struct {
		name    string
		streams func(*fakeStreams)
		req     wpdef.SolicitOfferRequest
		utc     bool
		want    im.StatusCode
	}{
		{"unknown usage", nil, solicit(4), true, im.StatusConstraintError},                                                                                                            // :601-605
		{"no stream field", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.VideoStreamId = nil }), true, im.StatusInvalidCommand},                                         // :608-613
		{"eleven ICE servers", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { s := make([]wpdef.ICEServerStruct, 11); r.IceServers = &s }), true, im.StatusConstraintError}, // :144
		{"eleven URLs", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.IceServers = &[]wpdef.ICEServerStruct{{Urls: urls(11)}} }), true, im.StatusConstraintError},        // :158
		{"long URL", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) {
			r.IceServers = &[]wpdef.ICEServerStruct{{Urls: []string{strings.Repeat("u", 2001)}}}
		}), true, im.StatusConstraintError}, // :166
		{"long username", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.IceServers = &[]wpdef.ICEServerStruct{{Username: str(509)}} }), true, im.StatusConstraintError},     // :182
		{"long credential", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.IceServers = &[]wpdef.ICEServerStruct{{Credential: str(513)}} }), true, im.StatusConstraintError}, // :189
		{"CAID 65535", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) {
			c := uint16(65535)
			r.IceServers = &[]wpdef.ICEServerStruct{{Caid: &c}}
		}), true, im.StatusConstraintError}, // :196
		{"long policy", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.IceTransportPolicy = str(17) }), true, im.StatusConstraintError},      // :636-643
		{"hard privacy", func(f *fakeStreams) { f.hard = true }, solicit(usage), true, im.StatusInvalidInState},                                          // :413
		{"soft livestream privacy", func(f *fakeStreams) { f.softLive = true }, solicit(usage), true, im.StatusInvalidInState},                           // :427
		{"soft recording privacy", func(f *fakeStreams) { f.softRec = true }, solicit(wpdef.StreamUsageRecording), true, im.StatusInvalidInState},        // :441
		{"usage not prioritised", nil, solicit(wpdef.StreamUsageAnalysis), true, im.StatusDynamicConstraintError},                                        // :654-659
		{"lists and ids", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.VideoStreams = &[]uint16{1} }), true, im.StatusInvalidCommand},      // :663-668
		{"null id without streams", func(f *fakeStreams) { f.videos = nil }, solicit(usage), true, im.StatusInvalidInState},                              // :517
		{"unknown video id", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.VideoStreamId = nv(9) }), true, im.StatusDynamicConstraintError}, // :531
		{"unknown audio id", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.AudioStreamId = nv(9) }), true, im.StatusDynamicConstraintError}, // :531
		{"list without streams", func(f *fakeStreams) { f.audios = nil }, solicit(usage, func(r *wpdef.SolicitOfferRequest) {
			r.VideoStreamId, r.AudioStreams = nil, &[]uint16{3}
		}), true, im.StatusInvalidInState}, // :567
		{"duplicate in list", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.VideoStreamId, r.VideoStreams = nil, &[]uint16{1, 1} }), true, im.StatusAlreadyExists},        // :576
		{"unknown in list", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) { r.VideoStreamId, r.VideoStreams = nil, &[]uint16{1, 7} }), true, im.StatusDynamicConstraintError}, // :587
		{"turns without UTCTime", nil, solicit(usage, func(r *wpdef.SolicitOfferRequest) {
			r.IceServers = &[]wpdef.ICEServerStruct{{Urls: []string{"turns:relay.example:5349"}}}
		}), false, im.StatusInvalidInState}, // :490
		{"usage not supported", func(f *fakeStreams) { f.supported = []camera.StreamUsage{recording} }, solicit(usage), true, im.StatusDynamicConstraintError}, // :735-740
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := cameraStreams()
			if tc.streams != nil {
				tc.streams(f)
			}
			p, _ := newProvider(t, f, func(c *camera.ProviderConfig) {
				known := tc.utc
				c.UTCTimeKnown = func() bool { return known }
			})
			_, err := p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, tc.req)
			wantCode(t, tc.name, statusOf(err), tc.want)
		})
	}
}

func TestSolicitOfferSession(t *testing.T) {
	p, m := newProvider(t, cameraStreams())
	ctx := peer(1, 0x55)
	m.deferred = true
	resp, err := p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageLiveView))
	wantCode(t, "solicit", statusOf(err), im.StatusSuccess)
	r := resp.(wpdef.SolicitOfferResponse)
	// The first id is 0 (:371-376); the null VideoStreamID auto-selects
	// the LiveView stream (camera-av-stream-manager.cpp:82-95) and the
	// response carries it (:826-830).
	if r.WebRtcSessionId != 0 || !r.DeferredOffer || r.VideoStreamId == nil || r.VideoStreamId.Value != 1 || r.AudioStreamId != nil {
		t.Fatalf("response %+v", r)
	}
	if a := m.offers[0]; a.PeerNodeID != 0x55 || a.FabricIndex != 1 || a.OriginatingEndpointID != 1 || a.AudioStreams != nil {
		t.Fatalf("offer %+v", a)
	}
	// A Recording request without an exact stream takes the loose match.
	resp, err = p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageRecording, func(r *wpdef.SolicitOfferRequest) { r.VideoStreamId, r.AudioStreamId = nil, nnull() }))
	wantCode(t, "loose", statusOf(err), im.StatusSuccess)
	if a := m.offers[1]; a.AudioStreams == nil || len(*a.AudioStreams) != 1 || (*a.AudioStreams)[0] != 3 {
		t.Fatalf("loose audio %+v", a.AudioStreams)
	}
	if resp.(wpdef.SolicitOfferResponse).WebRtcSessionId != 1 || len(p.Sessions()) != 2 {
		t.Fatalf("sessions %+v", p.Sessions())
	}
	// Another fabric reads none of them.
	if v, _ := p.MatterReadFiltered(peer(2, 0x55), wpdef.AttrCurrentSessions); len(v.(spec.List[camera.ProviderSession])) != 0 {
		t.Fatal("another fabric reads the sessions")
	}
	m.err = spec.Errorf(im.StatusBusy, "standby")
	_, err = p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageLiveView))
	wantCode(t, "port status", statusOf(err), im.StatusBusy) // :804-808
}

func TestProvideOfferRules(t *testing.T) {
	ctx := peer(1, 0x55)
	usage := wpdef.StreamUsageLiveView
	newOffer := func(edit ...func(*wpdef.ProvideOfferRequest)) wpdef.ProvideOfferRequest {
		u, ep := usage, uint16(1)
		r := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.NullOf[uint16](), Sdp: "v=0", StreamUsage: &u, OriginatingEndpointId: &ep, VideoStreamId: nnull()}
		for _, e := range edit {
			e(&r)
		}
		return r
	}
	p, m := newProvider(t, cameraStreams())
	_, err := p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer(func(r *wpdef.ProvideOfferRequest) { u := wpdef.StreamUsageEnum(9); r.StreamUsage = &u }))
	wantCode(t, "unknown usage", statusOf(err), im.StatusConstraintError) // :859-863
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreamId = nil }))
	wantCode(t, "no stream field", statusOf(err), im.StatusInvalidCommand) // :866-871
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer(func(r *wpdef.ProvideOfferRequest) { r.StreamUsage = nil }))
	wantCode(t, "new session without usage", statusOf(err), im.StatusInvalidCommand)
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer(func(r *wpdef.ProvideOfferRequest) { r.WebRtcSessionId = spec.ValueOf[uint16](7) }))
	wantCode(t, "unknown session", statusOf(err), im.StatusNotFound) // :922
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer(func(r *wpdef.ProvideOfferRequest) { u := wpdef.StreamUsageAnalysis; r.StreamUsage = &u }))
	wantCode(t, "not prioritised", statusOf(err), im.StatusDynamicConstraintError) // :968
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer(func(r *wpdef.ProvideOfferRequest) { r.VideoStreams = &[]uint16{1} }))
	wantCode(t, "lists and ids", statusOf(err), im.StatusInvalidCommand) // :977
	f := cameraStreams()
	f.supported = nil
	q, _ := newProvider(t, f)
	_, err = q.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer())
	wantCode(t, "cannot provide", statusOf(err), im.StatusResourceExhausted) // :1025
	f = cameraStreams()
	f.hard = true
	q, _ = newProvider(t, f)
	_, err = q.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer())
	wantCode(t, "privacy", statusOf(err), im.StatusInvalidInState) // :961

	resp, err := p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer())
	wantCode(t, "offer", statusOf(err), im.StatusSuccess)
	id := resp.(wpdef.ProvideOfferResponse).WebRtcSessionId
	if a := m.offers[0]; a.Sdp != "v=0" || a.SessionID != id || a.VideoStreams == nil || (*a.VideoStreams)[0] != 1 {
		t.Fatalf("offer %+v", a)
	}
	// A re-offer keeps the id and takes the deprecated id as named
	// (:926-948); it skips the privacy and stream checks.
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, newOffer(func(r *wpdef.ProvideOfferRequest) {
		r.WebRtcSessionId, r.StreamUsage, r.OriginatingEndpointId, r.VideoStreamId = spec.ValueOf(id), nil, nil, nv(2)
	}))
	wantCode(t, "re-offer", statusOf(err), im.StatusSuccess)
	if a := m.offers[1]; a.SessionID != id || a.StreamUsage != usage || a.OriginatingEndpointID != 1 || (*a.VideoStreams)[0] != 2 {
		t.Fatalf("re-offer %+v", a)
	}
	// Another peer's re-offer of the session is NOT_FOUND (:350-369).
	_, err = p.MatterInvoke(peer(1, 0x66), wpdef.CmdProvideOffer, newOffer(func(r *wpdef.ProvideOfferRequest) { r.WebRtcSessionId = spec.ValueOf(id) }))
	wantCode(t, "other peer", statusOf(err), im.StatusNotFound)
	if len(p.Sessions()) != 1 {
		t.Fatalf("sessions %+v", p.Sessions())
	}
}

func TestSignallingRules(t *testing.T) {
	ctx := peer(1, 0x55)
	p, m := newProvider(t, cameraStreams())
	resp, err := p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageLiveView))
	wantCode(t, "solicit", statusOf(err), im.StatusSuccess)
	id := resp.(wpdef.SolicitOfferResponse).WebRtcSessionId

	_, err = p.MatterInvoke(peer(2, 0x55), wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: id, Sdp: "a"})
	wantCode(t, "answer other fabric", statusOf(err), im.StatusNotFound) // :1145
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: id, Sdp: "a"})
	wantCode(t, "answer", statusOf(err), im.StatusSuccess)
	if len(m.answers) != 1 {
		t.Fatal("the answer did not reach the port")
	}
	cand := wpdef.ICECandidateStruct{Candidate: "c", SdpMid: spec.ValueOf(""), SdpmLineIndex: spec.NullOf[uint16]()}
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideIceCandidates, wpdef.ProvideIceCandidatesRequest{WebRtcSessionId: 99, IceCandidates: []wpdef.ICECandidateStruct{cand}})
	wantCode(t, "empty SDPMid", statusOf(err), im.StatusConstraintError) // :1170 before :1187
	cand.SdpMid = spec.NullOf[string]()
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideIceCandidates, wpdef.ProvideIceCandidatesRequest{WebRtcSessionId: 99, IceCandidates: []wpdef.ICECandidateStruct{cand}})
	wantCode(t, "ICE unknown session", statusOf(err), im.StatusNotFound) // :1187
	_, err = p.MatterInvoke(ctx, wpdef.CmdProvideIceCandidates, wpdef.ProvideIceCandidatesRequest{WebRtcSessionId: id, IceCandidates: []wpdef.ICECandidateStruct{cand}})
	wantCode(t, "ICE", statusOf(err), im.StatusSuccess)

	_, err = p.MatterInvoke(ctx, wpdef.CmdEndSession, wpdef.EndSessionRequest{WebRtcSessionId: id, Reason: 13})
	wantCode(t, "unknown reason", statusOf(err), im.StatusConstraintError) // :1205
	_, err = p.MatterInvoke(ctx, wpdef.CmdEndSession, wpdef.EndSessionRequest{WebRtcSessionId: 99, Reason: 2})
	wantCode(t, "end unknown", statusOf(err), im.StatusNotFound) // :1211
	m.err = errors.New("teardown failed")
	_, err = p.MatterInvoke(ctx, wpdef.CmdEndSession, wpdef.EndSessionRequest{WebRtcSessionId: id, Reason: 2})
	wantCode(t, "end with port error", statusOf(err), im.StatusFailure) // :1220
	if len(p.Sessions()) != 0 {
		t.Fatal("the session survived EndSession") // :1218
	}
}

func TestProviderHostSessions(t *testing.T) {
	p, _ := newProvider(t, nil)
	s := camera.ProviderSession{Id: 4, PeerNodeId: 1, FabricIndex: 1, StreamUsage: wpdef.StreamUsageLiveView}
	if err := p.UpsertSession(s); err != nil {
		t.Fatal(err)
	}
	s.PeerEndpointId = 3
	if err := p.UpsertSession(s); err != nil {
		t.Fatal(err)
	}
	if got := p.Sessions(); len(got) != 1 || got[0].PeerEndpointId != 3 {
		t.Fatalf("sessions %+v", got)
	}
	p.RemoveSession(4)
	if len(p.Sessions()) != 0 {
		t.Fatal("RemoveSession")
	}
	var _ wpdef.Host = p
	_ = context.Background()
}
