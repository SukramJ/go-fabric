// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// simTHOffer is an offer of the shape the harness's requestor sends in
// TC-AVSM-2.16/2.17: connectedhomeip WebRTCClient::CreateOffer
// (src/controller/webrtc/WebRTCClient.cpp:252-263 at 6170af84) adds a
// recvonly H264 video track, mid "video", payload type 96, and a recvonly
// opus audio track, mid "audio", payload type 111 (:187-245,
// WebRTCClient.h:28-38), and libdatachannel at chip's submodule pin
// 443f6934 writes the description (src/description.cpp:290-360 session
// part, :636-700 entries, :1053-1080 media lines, :1206-1290 codecs).
// Built by hand from those lines, not captured from a run: no run log
// carries the offer.
const simTHOffer = "v=0\r\n" +
	"o=rtc 2890844526 0 IN IP4 127.0.0.1\r\n" +
	"s=-\r\n" +
	"t=0 0\r\n" +
	"a=group:BUNDLE video audio\r\n" +
	"a=group:LS video audio\r\n" +
	"a=msid-semantic:WMS *\r\n" +
	"a=ice-options:ice2,trickle\r\n" +
	"a=fingerprint:sha-256 0F:4E:2B:7C:11:9A:5D:3E:88:21:C4:6B:7F:10:2E:93:A5:5C:61:0D:E2:38:9B:47:16:F0:AA:3C:72:85:D9:04\r\n" +
	"m=video 54321 UDP/TLS/RTP/SAVPF 96\r\n" +
	"c=IN IP4 192.0.2.10\r\n" +
	"b=AS:3000\r\n" +
	"a=mid:video\r\n" +
	"a=recvonly\r\n" +
	"a=rtcp-mux\r\n" +
	"a=rtpmap:96 H264/90000\r\n" +
	"a=rtcp-fb:96 nack\r\n" +
	"a=rtcp-fb:96 nack pli\r\n" +
	"a=rtcp-fb:96 goog-remb\r\n" +
	"a=setup:actpass\r\n" +
	"a=ice-ufrag:Xk3d\r\n" +
	"a=ice-pwd:Fq0t9cVb8aZr6Lw2Hn5Yp1Ue\r\n" +
	"a=candidate:1 1 UDP 2122317823 192.0.2.10 54321 typ host\r\n" +
	"a=end-of-candidates\r\n" +
	"m=audio 54321 UDP/TLS/RTP/SAVPF 111\r\n" +
	"c=IN IP4 192.0.2.10\r\n" +
	"b=AS:64000\r\n" +
	"a=mid:audio\r\n" +
	"a=recvonly\r\n" +
	"a=rtcp-mux\r\n" +
	"a=rtpmap:111 opus/48000/2\r\n" +
	"a=setup:actpass\r\n" +
	"a=ice-ufrag:Xk3d\r\n" +
	"a=ice-pwd:Fq0t9cVb8aZr6Lw2Hn5Yp1Ue\r\n"

// simCheckAnswer holds an answer to the rules the harness applies before
// and in set_remote_answer (libdatachannel_peer_connection.py:167-173 →
// WebRTCClient::SetRemoteDescription, WebRTCClient.cpp:278-306 at
// 6170af84 → rtc::PeerConnection::setRemoteDescription at 443f6934). An
// answer that fails one of chip's substring checks is dropped without an
// error; one that fails a libdatachannel rule throws out of a C call the
// harness makes through ctypes, which nothing catches
// (python/matter/webrtc/WebRTC.cpp:86-94, RTCClient.cpp:51-54).
func simCheckAnswer(t *testing.T, offer, answer string) {
	t.Helper()
	// chip ValidateSdpFields (WebRTCClient.cpp:72-82): each substring.
	for _, s := range []string{"v=", "o=", "s=", "c=", "t=", "m=", "a=ice-ufrag:", "a=ice-pwd:", "a=fingerprint:"} {
		if !strings.Contains(answer, s) {
			t.Errorf("answer has no %q (chip ValidateSdpFields)", s)
		}
	}
	type section struct {
		kind, port, proto, mid, dir string
		pts                         []string
	}
	parse := func(sdp string) (sections []section, ufrag, pwd, fingerprint, setup string) {
		for line := range strings.SplitSeq(sdp, "\n") {
			line = strings.TrimRight(line, "\r")
			switch {
			case strings.HasPrefix(line, "m="):
				// description.cpp:557-561: type and protocol, else
				// "Invalid media description line".
				f := strings.Fields(line[2:])
				if len(f) < 3 {
					t.Fatalf("m-line %q: no type and protocol", line)
				}
				sections = append(sections, section{kind: f[0], port: f[1], proto: f[2], pts: f[3:], mid: strconv.Itoa(len(sections))})
			case strings.HasPrefix(line, "a="):
				key, value, _ := strings.Cut(line[2:], ":")
				var cur *section
				if len(sections) > 0 {
					cur = &sections[len(sections)-1]
				}
				switch {
				case key == "setup":
					setup = value
				case key == "fingerprint" && fingerprint == "":
					fingerprint = value
				case key == "ice-ufrag" && ufrag == "":
					ufrag = value
				case key == "ice-pwd" && pwd == "":
					pwd = value
				case key == "mid" && cur != nil:
					cur.mid = value
				case slices.Contains([]string{"sendonly", "recvonly", "sendrecv", "inactive"}, key) && cur != nil:
					cur.dir = key
				case key == "rtpmap":
					// description.cpp:1146-1160: "<pt> <format>/<clock>".
					pt, desc, ok := strings.Cut(value, " ")
					if _, err := strconv.Atoi(pt); err != nil || !ok || !strings.Contains(desc, "/") {
						t.Errorf("rtpmap %q is not \"<pt> <format>/<clock>\"", value)
					}
				}
			}
		}
		return sections, ufrag, pwd, fingerprint, setup
	}
	offered, _, _, _, _ := parse(offer)
	answered, ufrag, pwd, fingerprint, setup := parse(answer)

	// PeerConnection::validateRemoteDescription (impl/peerconnection.cpp:
	// 924-953): ufrag, pwd, a valid fingerprint, an active media line.
	if ufrag == "" || pwd == "" {
		t.Errorf("answer ice-ufrag %q, ice-pwd %q", ufrag, pwd)
	}
	// Description::Description :108-143 and CertificateFingerprint::isValid
	// :1378-1395: a known algorithm and exactly its size in colon-separated
	// hex bytes, else setFingerprint throws.
	sizes := map[string]int{"sha-1": 20, "sha-224": 28, "sha-256": 32, "sha-384": 48, "sha-512": 64}
	alg, value, _ := strings.Cut(fingerprint, " ")
	n, known := sizes[strings.ToLower(alg)]
	bytes := strings.Split(value, ":")
	if !known || len(bytes) != n || strings.Contains(value, " ") {
		t.Errorf("fingerprint %q: not %s with %d colon-separated bytes", fingerprint, alg, n)
	}
	for _, b := range bytes {
		if len(b) != 2 || strings.Trim(b, "0123456789abcdefABCDEF") != "" {
			t.Errorf("fingerprint byte %q is not two hex digits", b)
		}
	}
	// IceTransport::setRemoteDescription (impl/icetransport.cpp:204-226):
	// an answer's role is active or passive, never actpass or absent (the
	// parsed default is ActPass, description.cpp:187-189).
	if setup != "active" && setup != "passive" {
		t.Errorf("answer a=setup:%q, want active or passive", setup)
	}
	// RFC 3264 §6, and libdatachannel's tracks found by mid
	// (impl/peerconnection.cpp:1113-1122): one answered section per offered
	// one, in order, of its kind and mid, active.
	if len(answered) != len(offered) {
		t.Fatalf("answer has %d media sections, the offer %d", len(answered), len(offered))
	}
	opposite := map[string]string{"recvonly": "sendonly", "sendonly": "recvonly", "sendrecv": "sendrecv", "inactive": "inactive"}
	for i, a := range answered {
		o := offered[i]
		if a.kind != o.kind || a.mid != o.mid || a.proto != o.proto {
			t.Errorf("section %d: %s mid %q %s, offered %s mid %q %s", i, a.kind, a.mid, a.proto, o.kind, o.mid, o.proto)
		}
		if a.port == "0" || a.dir == "inactive" {
			t.Errorf("section %d is not active (port %s, %s)", i, a.port, a.dir)
		}
		if a.dir != opposite[o.dir] {
			t.Errorf("section %d direction %q, offered %q", i, a.dir, o.dir)
		}
		if len(a.pts) == 0 || !slices.Contains(o.pts, a.pts[0]) {
			t.Errorf("section %d payload types %v, offered %v", i, a.pts, o.pts)
		}
	}
}

// simSignals is a PeerConnector that records the Answer and End commands
// the provider sends the requestor.
type simSignals struct {
	mu      sync.Mutex
	answers []wrdef.AnswerRequest
	ends    []wrdef.EndRequest
}

func (s *simSignals) ConnectPeer(context.Context, uint8, uint64) (camera.PeerSession, error) {
	return s, nil
}

func (s *simSignals) Invoke(_ context.Context, _ uint16, _, command uint32, fields spec.Encodable) (*spec.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch req := fields.(type) {
	case wrdef.AnswerRequest:
		if command == wrdef.CmdAnswer {
			s.answers = append(s.answers, req)
		}
	case wrdef.EndRequest:
		if command == wrdef.CmdEnd {
			s.ends = append(s.ends, req)
		}
	}
	return nil, nil
}

func (s *simSignals) sent() ([]wrdef.AnswerRequest, []wrdef.EndRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.answers), slices.Clone(s.ends)
}

// simStreams allocates one video and one audio stream, as TC-AVSM-2.16/
// 2.17's preconditions do (precondition_one_allocated_video_stream,
// precondition_one_allocated_audio_stream).
func simStreams(t *testing.T, c *demoCamera) (video, audio uint16) {
	t.Helper()
	avsm := c.camera.CameraAvStreamManagement().Underlying()
	resp, status := simInvoke(avsm, csdef.CmdVideoStreamAllocate, simVideoRequest())
	if status != im.StatusSuccess {
		t.Fatalf("VideoStreamAllocate: %v", status)
	}
	video = resp.(csdef.VideoStreamAllocateResponse).VideoStreamId
	mono := csdef.AudioStreamAllocateRequest{StreamUsage: csdef.StreamUsageLiveView, AudioCodec: csdef.AudioCodecOpus, ChannelCount: 1, SampleRate: 48000, BitRate: 20000, BitDepth: 24}
	resp, status = simInvoke(avsm, csdef.CmdAudioStreamAllocate, mono)
	if status != im.StatusSuccess {
		t.Fatalf("AudioStreamAllocate: %v", status)
	}
	return video, resp.(csdef.AudioStreamAllocateResponse).AudioStreamId
}

// simRefCounts reads the reference counts of the camera's one allocated
// video and audio stream.
func simRefCounts(t *testing.T, c *demoCamera) (video, audio uint8) {
	t.Helper()
	st := c.camera.CameraAvStreamManagement().Get()
	if len(st.AllocatedVideoStreams) != 1 || len(st.AllocatedAudioStreams) != 1 {
		t.Fatalf("allocated streams %d video, %d audio; want 1 and 1", len(st.AllocatedVideoStreams), len(st.AllocatedAudioStreams))
	}
	return st.AllocatedVideoStreams[0].ReferenceCount, st.AllocatedAudioStreams[0].ReferenceCount
}

// simProvideOffer sends TC-AVSM-2.17 step 6's ProvideOffer
// (TC_AVSM_2_17.py:268-283 at 6170af84) and returns the session id.
func simProvideOffer(t *testing.T, c *demoCamera, video, audio uint16) uint16 {
	t.Helper()
	usage, origin := wpdef.StreamUsageLiveView, uint16(1)
	nv, na := spec.Nullable[uint16]{Value: video}, spec.Nullable[uint16]{Value: audio}
	resp, status := simInvoke(c.camera.WebRtcTransportProvider().Underlying(), wpdef.CmdProvideOffer, wpdef.ProvideOfferRequest{
		WebRtcSessionId: spec.NullOf[uint16](), Sdp: simTHOffer, StreamUsage: &usage, OriginatingEndpointId: &origin,
		VideoStreamId: &nv, AudioStreamId: &na,
	})
	if status != im.StatusSuccess {
		t.Fatalf("ProvideOffer: %v", status)
	}
	return resp.(wpdef.ProvideOfferResponse).WebRtcSessionId
}

// TestCameraSimAnswersOfferAndEndsOnPrivacy: TC-AVSM-2.17 against the
// simulation. After ProvideOffer the camera sends the requestor an Answer
// for the session (step 7) whose SDP set_remote_answer accepts, the
// session counts its streams (steps 9-10), and SoftLivestreamPrivacyMode
// turned on ends it with End reason PrivacyMode, 11 (steps 11-12,
// TC_AVSM_2_17.py:318-325), out of CurrentSessions and its streams
// released (steps 13-15). Mirrors chip's camera-app
// (webrtc-provider-manager.cpp:522, :531-532, :1124-1150, :1063-1087,
// :990-1000 at 6170af84).
func TestCameraSimAnswersOfferAndEndsOnPrivacy(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	provider := c.camera.WebRtcTransportProvider().Underlying().(*camera.Provider)
	signals := &simSignals{}
	provider.AttachPeerConnector(signals)
	video, audio := simStreams(t, c)
	v0, a0 := simRefCounts(t, c)

	id := simProvideOffer(t, c, video, audio)
	waitFor(t, "the Answer", func() bool { a, _ := signals.sent(); return len(a) > 0 })
	answers, _ := signals.sent()
	if len(answers) != 1 || answers[0].WebRtcSessionId != id {
		t.Fatalf("Answer commands %+v, want one for session %d", answers, id)
	}
	simCheckAnswer(t, simTHOffer, answers[0].Sdp)
	if v, a := simRefCounts(t, c); v != v0+1 || a != a0+1 {
		t.Fatalf("reference counts after ProvideOffer %d/%d, want %d/%d", v, a, v0+1, a0+1)
	}

	avsm := c.camera.CameraAvStreamManagement().Underlying()
	ctx := contract.WithOrigin(simPeer(), contract.OriginControllerWrite)
	if err := avsm.MatterWrite(ctx, csdef.AttrSoftLivestreamPrivacyModeEnabled, true); err != nil {
		t.Fatalf("write SoftLivestreamPrivacyModeEnabled: %v", err)
	}
	waitFor(t, "the End", func() bool { _, e := signals.sent(); return len(e) > 0 })
	_, ends := signals.sent()
	if len(ends) != 1 || ends[0].WebRtcSessionId != id || ends[0].Reason != uint8(wrdef.WebRTCEndReasonPrivacyMode) {
		t.Fatalf("End commands %+v, want one for session %d with PrivacyMode", ends, id)
	}
	waitFor(t, "CurrentSessions empty", func() bool { return len(provider.Sessions()) == 0 })
	if v, a := simRefCounts(t, c); v != v0 || a != a0 {
		t.Fatalf("reference counts after End %d/%d, want %d/%d", v, a, v0, a0)
	}
}

// TestCameraSimEndSessionReleasesStreams: TC-AVSM-2.16 steps 5-13 — the
// session's streams are counted and cannot be deallocated (INVALID_IN_STATE)
// until EndSession releases them (chip HandleEndSession → CleanupSession →
// ReleaseAudioVideoStreams, webrtc-provider-manager.cpp:618-629, :1032).
func TestCameraSimEndSessionReleasesStreams(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	c.camera.WebRtcTransportProvider().Underlying().(*camera.Provider).AttachPeerConnector(&simSignals{})
	video, audio := simStreams(t, c)
	v0, a0 := simRefCounts(t, c)
	id := simProvideOffer(t, c, video, audio)
	if v, a := simRefCounts(t, c); v != v0+1 || a != a0+1 {
		t.Fatalf("reference counts after ProvideOffer %d/%d, want %d/%d", v, a, v0+1, a0+1)
	}
	avsm := c.camera.CameraAvStreamManagement().Underlying()
	if _, status := simInvoke(avsm, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: video}); status != im.StatusInvalidInState {
		t.Fatalf("VideoStreamDeallocate of a stream in use: %v, want INVALID_IN_STATE", status)
	}
	provider := c.camera.WebRtcTransportProvider().Underlying()
	if _, status := simInvoke(provider, wpdef.CmdEndSession, wpdef.EndSessionRequest{WebRtcSessionId: id, Reason: uint8(wpdef.WebRTCEndReasonUserHangup)}); status != im.StatusSuccess {
		t.Fatalf("EndSession: %v", status)
	}
	if v, a := simRefCounts(t, c); v != v0 || a != a0 {
		t.Fatalf("reference counts after EndSession %d/%d, want %d/%d", v, a, v0, a0)
	}
	if _, status := simInvoke(avsm, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: video}); status != im.StatusSuccess {
		t.Fatalf("VideoStreamDeallocate after EndSession: %v", status)
	}
}

// TestCameraSimAnswerSDP: the synthetic answer against the harness
// peer's rules (simCheckAnswer) for the harness's offer and for an offer
// that names no mid and lists another codec before the camera-app's; an
// offer without a media section is not answered.
func TestCameraSimAnswerSDP(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	fp, err := c.dtlsFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := c.dtlsFingerprint(); again != fp {
		t.Fatalf("the camera's fingerprint changed: %s, then %s", fp, again)
	}
	answer, err := camAnswerSDP(simTHOffer, fp, camICEString(8), camICEString(24))
	if err != nil {
		t.Fatal(err)
	}
	simCheckAnswer(t, simTHOffer, answer)
	for _, want := range []string{"m=video 9 UDP/TLS/RTP/SAVPF 96\r\n", "a=rtpmap:96 H264/90000\r\n", "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\n", "a=rtpmap:111 opus/48000/2\r\n", "a=sendonly\r\n", "a=setup:active\r\n"} {
		if !strings.Contains(answer, want) {
			t.Errorf("answer has no %q:\n%s", want, answer)
		}
	}

	other := "v=0\r\no=- 1 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 97 102\r\nc=IN IP4 0.0.0.0\r\na=sendrecv\r\na=rtpmap:97 VP8/90000\r\na=rtpmap:102 h264/90000\r\na=fmtp:102 packetization-mode=1\r\n"
	answer, err = camAnswerSDP(other, fp, camICEString(8), camICEString(24))
	if err != nil {
		t.Fatal(err)
	}
	simCheckAnswer(t, other, answer)
	if !strings.Contains(answer, "a=mid:0\r\n") || !strings.Contains(answer, "m=video 9 UDP/TLS/RTP/SAVPF 102\r\n") || !strings.Contains(answer, "a=fmtp:102 packetization-mode=1\r\n") {
		t.Errorf("answer to an offer without a mid, H264 second:\n%s", answer)
	}

	if _, err := camAnswerSDP("v=0", fp, "abcd", "abcdefghijklmnopqrstuv"); err == nil {
		t.Fatal("an offer without a media section was answered")
	}
}
