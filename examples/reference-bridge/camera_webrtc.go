// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// The camera's WebRTC signalling, as connectedhomeip's camera-app at the
// harness pin 6170af84 does it
// (examples/camera-app/linux/src/clusters/webrtc-provider/
// webrtc-provider-manager.cpp): a ProvideOffer counts the session's
// streams (AcquireAudioVideoStreams, :522), the offer goes to the peer
// connection and its answer to the requestor as the Answer command once
// the local description is there (:531-532, OnLocalDescription
// :1124-1150, ScheduleAnswerSend :865-889, SendAnswerCommand :1189-1218);
// a session's end releases the streams (CleanupSession :1021-1032); soft
// livestream privacy turned on ends every session with End reason
// PrivacyMode (LiveStreamPrivacyModeChanged :1063-1087, the End at
// :990-1000; camera-device.cpp:1670-1677).
//
// The camera-app's answer comes from libdatachannel; this simulation has
// no media stack, so the answer is synthetic: the offer's media sections
// mirrored, a fresh ICE ufrag and pwd, and the SHA-256 fingerprint of a
// self-signed certificate generated for the camera. It is shaped to what
// the harness's peer connection accepts in set_remote_answer
// (camAnswerSDP). No ICE candidate is gathered or sent and no DTLS
// handshake answered: a case that waits for the camera's candidates, a
// connected peer or media (TC-WEBRTCP-2.22, 2.23, 2.33) still times out.

// camSessionStreams are the streams a WebRTC session counted.
type camSessionStreams struct{ videos, audios []uint16 }

// camAnswerAttempts and camAnswerRetry bound the Answer's send. The
// session enters CurrentSessions when the port has returned — the send
// waits for that, on the provider's change notification — and the
// requestor knows it only once it read ProvideOfferResponse
// (WebRTCTransportProviderClient::HandleProvideOfferResponse →
// UpsertSession, src/controller/webrtc/WebRTCTransportProviderClient.cpp:
// 312-334 at 6170af84); before that its Answer handler answers NOT_FOUND
// (WebRTCTransportRequestorCluster.cpp:199-202). The camera-app sends the
// Answer from a lambda scheduled after the command (ScheduleAnswerSend),
// so the response leaves first; here a NOT_FOUND is retried instead, on
// the camera's clock, a few times: the window is the requestor reading
// one response. A requestor that never learns the session (a ProvideOffer
// sent without its provider client) gets camAnswerAttempts Answers, not
// one per retry for seconds.
const (
	camAnswerAttempts = 10
	camAnswerRetry    = 50 * time.Millisecond
)

// camSessionSignal is what a WebRTC session's answers share: one ICE
// ufrag and pwd for every answer of the session — another pair on the
// requestor's peer connection is an ICE restart, which libjuice refuses
// and libdatachannel throws for (libjuice agent.c:536-550 at 3c40a354,
// libdatachannel impl/icetransport.cpp:224-226 at 443f6934) — and ended,
// closed when the session ends, which stops a pending Answer.
type camSessionSignal struct {
	ufrag, pwd string
	ended      chan struct{}
}

// sessionSignal is the session's signal, made at its first answer.
func (c *demoCamera) sessionSignal(id uint16) *camSessionSignal {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sig, ok := c.signals[id]; ok {
		return sig
	}
	if c.signals == nil {
		c.signals = map[uint16]*camSessionSignal{}
	}
	sig := &camSessionSignal{ufrag: camICEString(8), pwd: camICEString(24), ended: make(chan struct{})}
	c.signals[id] = sig
	return sig
}

// sessionEnded is the camera-app's CleanupSession (:1021-1050) for the
// simulation: the session's streams are released and its pending Answer
// stopped.
func (c *demoCamera) sessionEnded(id uint16) {
	c.releaseStreams(id)
	c.mu.Lock()
	sig, ok := c.signals[id]
	delete(c.signals, id)
	c.mu.Unlock()
	if ok {
		close(sig.ended)
	}
}

// lifeContext is the context the camera's background work ends with: the
// daemon's, from attach; the background one before attach (tests).
func (c *demoCamera) lifeContext() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.life == nil {
		return context.Background()
	}
	return c.life
}

// provider is the camera's WebRtcTransportProvider kit server.
func (c *demoCamera) provider() (*camera.Provider, bool) {
	p, ok := c.camera.WebRtcTransportProvider().Underlying().(*camera.Provider)
	return p, ok
}

// acquireStreams is AcquireAudioVideoStreams
// (webrtc-provider-manager.cpp:1331-1344 →
// camera-av-stream-manager.cpp:966-1025 OnTransportAcquireAudioVideoStreams):
// each of the session's streams is counted up; one the stream management
// refuses is logged and not counted. What was counted is kept for the
// release.
func (c *demoCamera) acquireStreams(id uint16, videos, audios *[]uint16) {
	host := c.camera.CameraAvStreamManagement().Host()
	if host == nil {
		return
	}
	var got camSessionStreams
	for _, a := range deref(audios) {
		if err := host.UpdateAudioStreamRefCount(a, true); err != nil {
			c.logger.Info("camerasim.acquire", slog.Uint64("session", uint64(id)), slog.Uint64("audio", uint64(a)), slog.Any("err", err))
			continue
		}
		got.audios = append(got.audios, a)
	}
	for _, v := range deref(videos) {
		if err := host.UpdateVideoStreamRefCount(v, true); err != nil {
			c.logger.Info("camerasim.acquire", slog.Uint64("session", uint64(id)), slog.Uint64("video", uint64(v)), slog.Any("err", err))
			continue
		}
		got.videos = append(got.videos, v)
	}
	c.mu.Lock()
	if c.sessionStreams == nil {
		c.sessionStreams = map[uint16]camSessionStreams{}
	}
	c.sessionStreams[id] = got
	c.mu.Unlock()
}

// releaseStreams is ReleaseAudioVideoStreams (:1346-1359 →
// OnTransportReleaseAudioVideoStreams, camera-av-stream-manager.cpp:
// 1027-1086): the streams the session counted are counted down, once.
func (c *demoCamera) releaseStreams(id uint16) {
	c.mu.Lock()
	got, ok := c.sessionStreams[id]
	delete(c.sessionStreams, id)
	c.mu.Unlock()
	host := c.camera.CameraAvStreamManagement().Host()
	if !ok || host == nil {
		return
	}
	for _, a := range got.audios {
		if err := host.UpdateAudioStreamRefCount(a, false); err != nil {
			c.logger.Info("camerasim.release", slog.Uint64("session", uint64(id)), slog.Uint64("audio", uint64(a)), slog.Any("err", err))
		}
	}
	for _, v := range got.videos {
		if err := host.UpdateVideoStreamRefCount(v, false); err != nil {
			c.logger.Info("camerasim.release", slog.Uint64("session", uint64(id)), slog.Uint64("video", uint64(v)), slog.Any("err", err))
		}
	}
}

func deref(ids *[]uint16) []uint16 {
	if ids == nil {
		return nil
	}
	return *ids
}

// answerOffer builds the answer to a ProvideOffer's SDP and sends it to
// the requestor as the Answer command, from its own goroutine (the
// camera-app's ScheduleAnswerSend). The goroutine runs on ctx without its
// cancellation — the command that carried the offer has answered by then
// — and ends with the daemon (lifeContext) and with the session
// (sessionEnded): no Answer goes out for an ended session. Every answer
// of a session carries the session's ICE credentials (sessionSignal). An
// offer the answer cannot be built from — no media section — is logged
// and not answered.
func (c *demoCamera) answerOffer(ctx context.Context, id uint16, offer string) {
	fingerprint, err := c.dtlsFingerprint()
	if err != nil {
		c.logger.Info("camerasim.answer", slog.Uint64("session", uint64(id)), slog.Any("err", err))
		return
	}
	sig := c.sessionSignal(id)
	answer, err := camAnswerSDP(offer, fingerprint, sig.ufrag, sig.pwd)
	if err != nil {
		c.logger.Info("camerasim.answer", slog.Uint64("session", uint64(id)), slog.Any("err", err))
		return
	}
	provider, ok := c.provider()
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(c.lifeContext(), cancel) //nolint:contextcheck // the daemon's life ends the send, not the request's
	changed := make(chan struct{}, 1)
	unsubscribe := provider.OnAttributeChange(func(contract.Change) {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	go func() {
		defer cancel()
		defer stop()
		defer unsubscribe()
		for attempt := 1; ; {
			if _, ok := c.currentSession(id); !ok {
				// Not yet in CurrentSessions: the server stores it when the
				// port has returned.
				select {
				case <-changed:
					continue
				case <-sig.ended:
					return
				case <-ctx.Done():
					return
				}
			}
			err := provider.Answer(ctx, id, answer)
			if err == nil {
				return
			}
			if spec.StatusOf(err) != im.StatusNotFound || attempt >= camAnswerAttempts {
				c.logger.Info("camerasim.answer", slog.Uint64("session", uint64(id)), slog.Int("attempt", attempt), slog.Any("err", err))
				return
			}
			attempt++
			t := c.clk.NewTimer(camAnswerRetry)
			select {
			case <-t.C():
			case <-sig.ended:
				t.Stop()
				return
			case <-ctx.Done():
				t.Stop()
				return
			}
		}
	}()
}

// livestreamPrivacyOn is LiveStreamPrivacyModeChanged(true)
// (webrtc-provider-manager.cpp:1063-1087): every session ends with End
// reason PrivacyMode (the End chosen at :990-997 while
// mSoftLiveStreamPrivacyEnabled) and is cleaned up (:999-1000,
// CleanupSession: its streams released). The camera-app sends End
// asynchronously and releases at once; here the release comes first, so
// a requestor that reads the counts after End sees them released either
// way. [camera.Provider.End] takes the session out of CurrentSessions.
func (c *demoCamera) livestreamPrivacyOn() {
	provider, ok := c.provider()
	if !ok {
		return
	}
	for _, s := range provider.Sessions() {
		c.sessionEnded(s.Id)
		if err := provider.End(c.lifeContext(), s.Id, uint8(wrdef.WebRTCEndReasonPrivacyMode)); err != nil {
			c.logger.Info("camerasim.privacyend", slog.Uint64("session", uint64(s.Id)), slog.Any("err", err))
		}
	}
}

// dtlsFingerprint is the SHA-256 fingerprint of the camera's DTLS
// certificate, a self-signed ECDSA P-256 one made at the first answer.
// Its key never takes part in a handshake: no ICE pair forms.
func (c *demoCamera) dtlsFingerprint() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fingerprint != "" {
		return c.fingerprint, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("DTLS key: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "camerasim"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", fmt.Errorf("DTLS certificate: %w", err)
	}
	c.fingerprint = camFingerprint(der)
	return c.fingerprint, nil
}

// camFingerprint is the SDP form of a certificate's SHA-256 fingerprint
// (RFC 8122 §5): upper-case hex bytes joined by colons.
func camFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// camICEString is a random string of n ice-chars (RFC 8839 §5.4:
// ALPHA / DIGIT / "+" / "/"); the ufrag takes at least 4, the pwd at
// least 22.
func camICEString(n int) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand.Read does not fail (Go 1.24+)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// camOfferSection is one media section of an offer.
type camOfferSection struct {
	kind, port, proto, mid, dir string
	pts                         []string
	// rtpmaps are the a=rtpmap values by payload type, fmtps the a=fmtp
	// values.
	rtpmaps map[string]string
	fmtps   map[string][]string
}

// camAnswerSDP is the answer to offer, shaped to what the harness's peer
// connection takes in set_remote_answer (WebRTCClient::SetRemoteDescription,
// src/controller/webrtc/WebRTCClient.cpp:278-306 at 6170af84, and
// libdatachannel at chip's submodule pin 443f6934):
//
//   - the session lines and per section c=, ice-ufrag, ice-pwd and the
//     fingerprint chip's ValidateSdpFields looks for (WebRTCClient.cpp:
//     72-82; without one the answer is dropped, nothing set);
//   - a fingerprint of a known algorithm with exactly its size in hex
//     bytes (libdatachannel description.cpp:108-143, :1363-1380), the
//     ufrag and pwd, at least one active media section
//     (impl/peerconnection.cpp:924-953 validateRemoteDescription);
//   - a=setup:active, never actpass in an answer
//     (impl/icetransport.cpp:204-217); active is the role libdatachannel's
//     own answerer takes against an actpass offer (:216-218), as the
//     camera-app's does;
//   - each offered section answered in order, of its kind, protocol and
//     mid (the requestor's tracks are found by mid,
//     impl/peerconnection.cpp:1113-1122), the direction reversed, and one
//     payload type: the camera-app answers H264 video and opus audio
//     (webrtc-provider-manager.cpp:502-517 GetPayloadType), else the
//     first offered;
//   - each rtpmap as "<pt> <format>/<clock>" (description.cpp:1150-1160):
//     copied from the offer, with its fmtp lines.
//
// The session lines follow libdatachannel's generateSdp
// (description.cpp:290-360). An offer without a media section is an
// error.
func camAnswerSDP(offer, fingerprint, ufrag, pwd string) (string, error) {
	sections, err := camParseOffer(offer)
	if err != nil {
		return "", err
	}
	const eol = "\r\n"
	var b strings.Builder
	mids := make([]string, 0, len(sections))
	for _, s := range sections {
		mids = append(mids, s.mid)
	}
	b.WriteString("v=0" + eol)
	b.WriteString("o=rtc " + strconv.FormatUint(uint64(time.Now().UnixNano()&0x7fffffff), 10) + " 0 IN IP4 127.0.0.1" + eol)
	b.WriteString("s=-" + eol)
	b.WriteString("t=0 0" + eol)
	b.WriteString("a=group:BUNDLE " + strings.Join(mids, " ") + eol)
	b.WriteString("a=msid-semantic:WMS *" + eol)
	b.WriteString("a=fingerprint:sha-256 " + fingerprint + eol)
	reverse := map[string]string{"recvonly": "sendonly", "sendonly": "recvonly", "sendrecv": "sendrecv", "inactive": "inactive"}
	for _, s := range sections {
		pt := s.choosePT()
		b.WriteString("m=" + s.kind + " 9 " + s.proto + " " + pt + eol)
		b.WriteString("c=IN IP4 0.0.0.0" + eol)
		b.WriteString("a=mid:" + s.mid + eol)
		dir, ok := reverse[s.dir]
		if !ok {
			dir = "sendrecv" // an offer without one is sendrecv (RFC 3264 §5.1)
		}
		b.WriteString("a=" + dir + eol)
		if s.kind != "application" {
			b.WriteString("a=rtcp-mux" + eol)
		}
		if m, ok := s.rtpmaps[pt]; ok {
			b.WriteString("a=rtpmap:" + pt + " " + m + eol)
		}
		for _, f := range s.fmtps[pt] {
			b.WriteString("a=fmtp:" + pt + " " + f + eol)
		}
		b.WriteString("a=setup:active" + eol)
		b.WriteString("a=ice-ufrag:" + ufrag + eol)
		b.WriteString("a=ice-pwd:" + pwd + eol)
	}
	return b.String(), nil
}

// choosePT is the answered payload type: the camera-app's codec of the
// section's kind if offered, else the first offered.
func (s camOfferSection) choosePT() string {
	codec := map[string]string{"video": "h264", "audio": "opus"}[s.kind]
	for _, pt := range s.pts {
		format, _, _ := strings.Cut(s.rtpmaps[pt], "/")
		if codec != "" && strings.EqualFold(format, codec) {
			return pt
		}
	}
	return s.pts[0]
}

// camParseOffer reads the media sections of an offer: each m-line's kind,
// port, protocol and formats, its mid (the section's index without one,
// as libdatachannel numbers them, description.cpp:90), direction, rtpmap
// and fmtp lines. A section without a protocol or a format, or an offer
// without a section, is an error.
func camParseOffer(offer string) ([]camOfferSection, error) {
	var sections []camOfferSection
	for line := range strings.SplitSeq(offer, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "m=") {
			f := strings.Fields(line[2:])
			if len(f) < 4 {
				return nil, fmt.Errorf("offer media line %q: no protocol or format", line)
			}
			sections = append(sections, camOfferSection{
				kind: f[0], port: f[1], proto: f[2], pts: f[3:], mid: strconv.Itoa(len(sections)),
				rtpmaps: map[string]string{}, fmtps: map[string][]string{},
			})
			continue
		}
		if len(sections) == 0 || !strings.HasPrefix(line, "a=") {
			continue
		}
		s := &sections[len(sections)-1]
		key, value, _ := strings.Cut(line[2:], ":")
		switch key {
		case "mid":
			s.mid = value
		case "sendonly", "recvonly", "sendrecv", "inactive":
			s.dir = key
		case "rtpmap":
			if pt, m, ok := strings.Cut(value, " "); ok {
				s.rtpmaps[pt] = m
			}
		case "fmtp":
			if pt, f, ok := strings.Cut(value, " "); ok {
				s.fmtps[pt] = append(s.fmtps[pt], f)
			}
		}
	}
	if len(sections) == 0 {
		return nil, errors.New("offer has no media section")
	}
	return sections, nil
}
