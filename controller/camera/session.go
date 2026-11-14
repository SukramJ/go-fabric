// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"slices"
	"strings"
	"sync"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller/client"
)

// clientSessionKeyMaxLength bounds an owner's key for a session. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraCommands.ts:308
// at 0bd06f2.
const clientSessionKeyMaxLength = 128

// StartRequest is one session start. A zero numeric bound and a nil
// pointer are unstated.
type StartRequest struct {
	Node     *client.Node
	Endpoint uint16
	// Usage is the session's stream usage: Recording, Analysis or
	// LiveView (Internal is the camera's own).
	Usage csdef.StreamUsageEnum
	// Offer is the host's SDP offer (ModeProvideOffer); nil asks the
	// camera for one (ModeSolicitOffer).
	Offer *string
	// Video demands a video stream within its bounds; nil takes one when
	// available. DeclineVideo asks for none.
	Video        *VideoBounds
	DeclineVideo bool
	// Audio demands an audio stream; nil takes one when available.
	// DeclineAudio asks for none.
	Audio        *AudioBounds
	DeclineAudio bool
	// ICEServers and ICETransportPolicy go to the camera as stated.
	ICEServers         []wpdef.ICEServerStruct
	ICETransportPolicy string
	MetadataEnabled    bool
	// AllowEviction lets the start deallocate an unused stream to make
	// room; nil allows it.
	AllowEviction *bool
	// Talkback forwards the offer's sending audio sections unchanged;
	// without it they are rewritten not to send. It needs an Offer.
	Talkback bool
	// Owner is who the session belongs to, as the host names it (a
	// connection); its sessions end with [Service.Release]. Key is the
	// owner's own name for the session, unique among its starting and open
	// sessions, which [Service.StopByKey] takes.
	Owner string
	Key   string
}

// VideoBounds are a start's video bounds; each stated one is hard. A nil
// field is unstated; a stated one is held to its field's range in the
// cluster's model, so a stated 0 is refused where the floor is 1, as
// matterjs-server refuses it (toOptionalNumber against
// CAMERA_FIELD_RANGES, cameraCommands.ts:378-382 and
// cameraFieldRanges.ts:48-56 at 0bd06f2).
type VideoBounds struct {
	// Codecs are the codecs the host accepts, in its order of preference,
	// as SDP spells them (H264, H265, …); nil is any.
	Codecs                       []string
	MinResolution, MaxResolution *csdef.VideoResolutionStruct
	MinFrameRate, MaxFrameRate   *uint16
	MinBitRate, MaxBitRate       *uint32
	Overlays                     Overlays
}

// Overlays are the watermark and on-screen display flags; nil is
// unstated, which is not the same as off.
type Overlays struct {
	Watermark *bool
	OSD       *bool
}

// AudioBounds are a start's audio values; each stated one is exact. A
// nil field is unstated; a stated one is held to its field's range, as
// [VideoBounds]' are.
type AudioBounds struct {
	Codecs       []string
	ChannelCount *uint8
	SampleRate   *uint32
	BitRate      *uint32
}

// VideoEnvelope is what a video stream delivers.
type VideoEnvelope struct {
	Codec                        csdef.VideoCodecEnum
	MinResolution, MaxResolution csdef.VideoResolutionStruct
	MinFrameRate, MaxFrameRate   uint16
	MinBitRate, MaxBitRate       uint32
	KeyFrameInterval             uint16
	// Overlays are nil when the stream has no such overlay.
	Overlays Overlays
}

// AudioEnvelope is what an audio stream delivers.
type AudioEnvelope struct {
	Codec        csdef.AudioCodecEnum
	ChannelCount uint8
	SampleRate   uint32
	BitRate      uint32
	BitDepth     uint8
}

// BudgetNarrowing is what the camera's encoder budget made a start give
// up of its first window; a zero field gave up nothing.
type BudgetNarrowing struct {
	MaxFrameRate  uint16
	MaxResolution *csdef.VideoResolutionStruct
}

// Lease is a stream a session carries: how it came to serve, and what it
// delivers (Video for a video stream, Audio for audio). Degraded: the
// stream misses the window the controller would have allocated.
type Lease struct {
	Kind           StreamKind
	ID             uint16
	Provenance     Provenance
	Degraded       bool
	Video          *VideoEnvelope
	Audio          *AudioEnvelope
	BudgetNarrowed *BudgetNarrowing
}

func leaseOf(kind StreamKind, r *resolvedStream) Lease {
	l := Lease{Kind: kind, ID: r.streamID, Provenance: r.provenance, Degraded: r.degraded}
	if v := r.video; v != nil {
		l.Video = &VideoEnvelope{
			Codec: v.codec, MinResolution: wireResolution(v.minResolution), MaxResolution: wireResolution(v.maxResolution),
			MinFrameRate: uint16(v.minFrameRate), MaxFrameRate: uint16(v.maxFrameRate), //nolint:gosec // G115: within the stream's uint16 range
			MinBitRate: uint32(v.minBitRate), MaxBitRate: uint32(v.maxBitRate), //nolint:gosec // G115: within the stream's uint32 range
			KeyFrameInterval: uint16(v.keyFrameInterval), //nolint:gosec // G115: within the stream's uint16 range
			Overlays:         Overlays{Watermark: wireFlag(v.overlays.watermark), OSD: wireFlag(v.overlays.osd)},
		}
	}
	if a := r.audio; a != nil {
		l.Audio = &AudioEnvelope{Codec: a.codec, ChannelCount: uint8(a.channelCount), SampleRate: uint32(a.sampleRate), BitRate: uint32(a.bitRate), BitDepth: uint8(a.bitDepth)} //nolint:gosec // G115: the camera's own widths
	}
	if n := r.budgetNarrowed; n != nil {
		b := &BudgetNarrowing{MaxFrameRate: uint16(n.maxFrameRate)} //nolint:gosec // G115: a frame rate
		if n.maxResolution != (resolution{}) {
			res := wireResolution(n.maxResolution)
			b.MaxResolution = &res
		}
		l.BudgetNarrowed = b
	}
	return l
}

// Session is a WebRTC session the service opened. Signals carries the
// camera's half of the signalling, from the camera's first signal on —
// one sent before StartStream returned is kept — and is closed once the
// session ended, whatever ended it.
type Session struct {
	ID       uint16
	Node     *client.Node
	Endpoint uint16
	Mode     Mode
	// Streams are the session's video and audio streams.
	Streams []Lease
	// Evicted are the video streams the start deallocated to make room.
	Evicted []uint16
	// Talkback: the host asked for it, the offer's audio can send, the
	// camera states half or full duplex and the session carries audio.
	Talkback bool
	Owner    string
	Key      string

	svc *Service
	t   target
	box *mailbox
	// life is the session's identity in the manager's registry: an end
	// announced for another session that had the same id leaves this one
	// alone (review II, R2-P1-cam-b-02).
	life *lifecycle
}

// Signals is the camera's signalling of the session. The service holds
// what the host has not read yet up to 1024 signals and 1 MiB of SDP,
// candidate and ICE server text; a camera that signals past either bound
// while the host does not read ends the session: Signals closes at once,
// without the signals it held, and the end is announced as
// [EndSignalOverflow] after an EndSession to the camera.
func (s *Session) Signals() <-chan Signal { return s.box.out }

// ProvideAnswer sends the host's SDP answer to a camera's Offer
// (ModeSolicitOffer); it travels over TCP (the command's Large Message
// quality).
func (s *Session) ProvideAnswer(ctx context.Context, sdp string) error {
	_, err := s.svc.io.invokeProvider(ctx, s.t, callProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: s.ID, Sdp: sdp})
	return err
}

// ProvideICECandidates sends the host's candidates, in order, in as many
// ProvideICECandidates as keep each within one Matter message over UDP;
// every send is made, and the first failure is returned.
func (s *Session) ProvideICECandidates(ctx context.Context, candidates []wpdef.ICECandidateStruct) error {
	if len(candidates) == 0 {
		return invalidArguments("ice_candidates names no candidate")
	}
	return provideCandidates(ctx, candidates, func(ctx context.Context, send []wpdef.ICECandidateStruct) error {
		_, err := s.svc.io.invokeProvider(ctx, s.t, callProvideIceCandidates, wpdef.ProvideIceCandidatesRequest{WebRtcSessionId: s.ID, IceCandidates: send})
		return err
	})
}

// provideCandidates sends the candidates in sends that fit the byte
// budget, each with provide, and answers with the first failure, else
// success. Mirrors matterjs-server
// packages/ws-controller/src/server/startingSessionSignalling.ts:181-226
// at 0bd06f2.
func provideCandidates(ctx context.Context, candidates []wpdef.ICECandidateStruct, provide func(context.Context, []wpdef.ICECandidateStruct) error) error {
	var first error
	for _, send := range iceCandidateSends(candidates) {
		if err := provide(ctx, send); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Stop ends the session on the camera; its streams stay allocated for a
// later start. ended false: the camera no longer held it (NOT_FOUND). A
// refused EndSession is returned; the session stays open then, and Stop
// again is the retry.
func (s *Session) Stop(ctx context.Context) (ended bool, err error) {
	return s.svc.m.stopStream(ctx, s.t, s.ID, s.Owner)
}

// iceCandidatesByteBudget is the room for the candidates of one
// ProvideICECandidates that fits a Matter message not sent over TCP: 1232
// bytes (Core §4.4.4) less 128 for the headers and the invoke around the
// fields. iceCandidateFramingBytes is one candidate's TLV framing. Mirrors
// matterjs-server packages/ws-controller/src/server/startingSessionSignalling.ts:15-27
// at 0bd06f2.
const (
	iceCandidatesByteBudget  = 1232 - 128
	iceCandidateFramingBytes = 16
)

// iceCandidateSends cuts the candidates, in order, into sends that each
// fit the byte budget; a candidate larger than the budget goes alone.
// Mirrors matterjs-server packages/ws-controller/src/server/startingSessionSignalling.ts:233-265
// at 0bd06f2.
func iceCandidateSends(candidates []wpdef.ICECandidateStruct) [][]wpdef.ICECandidateStruct {
	var sends [][]wpdef.ICECandidateStruct
	cur := make([]wpdef.ICECandidateStruct, 0, len(candidates))
	bytes := 0
	for _, c := range candidates {
		n := candidateBytes(c)
		if len(cur) > 0 && bytes+n > iceCandidatesByteBudget {
			sends = append(sends, cur)
			cur, bytes = nil, 0
		}
		cur = append(cur, c)
		bytes += n
	}
	if len(cur) > 0 {
		sends = append(sends, cur)
	}
	return sends
}

// candidateBytes is a candidate's size in a send: its framing and its
// strings.
func candidateBytes(c wpdef.ICECandidateStruct) int {
	n := iceCandidateFramingBytes + len(c.Candidate)
	if !c.SdpMid.Null {
		n += len(c.SdpMid.Value)
	}
	return n
}

// signalMailboxLimit and signalMailboxBytes bound what a session's
// mailbox holds for a host that has not read it yet: signals, and their
// SDP, candidate and ICE server bytes (signalBytes). The module's own
// choice; neither reference bounds the queue. A legitimate session sends
// one Offer or Answer, one End and its ICE candidates; trickled one per
// ICECandidates command, a camera that gathers one server-reflexive or
// relayed candidate per ICE server URL and address family sends about
// 200 for the most a start can hand it, 10 servers of 10 URLs each (the
// IceServers and Urls bounds of ProvideOffer in the generated definition,
// iceServerLimits). 1024 is that with fourfold headroom. In bytes such a
// session holds one SDP of at most one TCP message (64000 bytes,
// transport/tcp DefaultMaxPayloadSize) and a few hundred candidates of
// some hundred bytes; 1 MiB is that with ample headroom, and bounds what
// a flood of maximum-size messages can hold, where the count alone would
// let it hold 64 MB.
const (
	signalMailboxLimit = 1024
	signalMailboxBytes = 1 << 20
)

// signalBytes is a signal's size against signalMailboxBytes: its SDP, its
// candidates as a send counts them, and the ICE servers an Offer carries.
func signalBytes(sig Signal) int {
	n := len(sig.SDP)
	for _, c := range sig.Candidates {
		n += iceCandidateFramingBytes + len(c.Candidate)
		if !c.SdpMid.Null {
			n += len(c.SdpMid.Value)
		}
	}
	if servers := sig.Offer.IceServers; servers != nil {
		for _, srv := range *servers {
			for _, u := range srv.Urls {
				n += len(u)
			}
			if srv.Username != nil {
				n += len(*srv.Username)
			}
			if srv.Credential != nil {
				n += len(*srv.Credential)
			}
		}
	}
	if sig.Offer.IceTransportPolicy != nil {
		n += len(*sig.Offer.IceTransportPolicy)
	}
	return n
}

// mailbox holds a session's signals until the host reads them, so the
// serving goroutine that delivers a camera's signal never waits for the
// host — up to signalMailboxLimit signals and signalMailboxBytes bytes
// held. A signal past either bound overflows the box: it drops what it
// holds, closes out and refuses everything after; the Service ends the
// session (review II, R2-P1-cam-b-06).
type mailbox struct {
	mu     sync.Mutex
	queue  []Signal
	closed bool
	// held and heldBytes count the signals put and not yet handed to the
	// host: the queue's and the ones the pump took and still offers.
	held, heldBytes int
	overflowed      bool
	wake            chan struct{}
	out             chan Signal
	// done closes when the pump has ended.
	done chan struct{}
	// abandoned closes when no one will ever read out: the pump drops
	// what it holds and ends.
	abandoned chan struct{}
	abandon   sync.Once
}

func newMailbox(life context.Context) *mailbox {
	b := &mailbox{wake: make(chan struct{}, 1), out: make(chan Signal), done: make(chan struct{}), abandoned: make(chan struct{})}
	go b.pump(life)
	return b
}

// put queues sig. It reports true once, for the signal that took the box
// past a bound: the box has then dropped its signals and closed, and the
// caller ends the session. A closed box drops sig.
func (b *mailbox) put(sig Signal) (overflowed bool) {
	n := signalBytes(sig)
	b.mu.Lock()
	switch {
	case b.closed:
	case b.held >= signalMailboxLimit || b.heldBytes+n > signalMailboxBytes:
		b.closed, b.overflowed, overflowed = true, true, true
		b.queue, b.held, b.heldBytes = nil, 0, 0
	default:
		b.queue = append(b.queue, sig)
		b.held++
		b.heldBytes += n
	}
	b.mu.Unlock()
	if overflowed {
		b.abandon.Do(func() { close(b.abandoned) })
		return true
	}
	b.notify()
	return false
}

// handed counts a signal the host took off the box.
func (b *mailbox) handed(sig Signal) {
	b.mu.Lock()
	if b.held > 0 {
		b.held--
		b.heldBytes = max(b.heldBytes-signalBytes(sig), 0)
	}
	b.mu.Unlock()
}

func (b *mailbox) close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.notify()
}

// discard ends a mailbox whose Session never reached the host: the queued
// signals go and the pump ends at once, where close would have it wait for
// a reader of out that never comes (review II, R2-P1-cam-b-01).
func (b *mailbox) discard() {
	b.mu.Lock()
	b.closed = true
	b.queue, b.held, b.heldBytes = nil, 0, 0
	b.mu.Unlock()
	b.abandon.Do(func() { close(b.abandoned) })
}

func (b *mailbox) notify() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// pump hands the queued signals on in order and closes out once the box
// is closed and drained, discarded, or the service's life ended.
func (b *mailbox) pump(life context.Context) {
	defer close(b.done)
	defer close(b.out)
	for {
		b.mu.Lock()
		queue := b.queue
		b.queue = nil
		closed := b.closed
		b.mu.Unlock()
		for i := range queue {
			select {
			case b.out <- queue[i]:
				b.handed(queue[i])
			case <-life.Done():
				return
			case <-b.abandoned:
				return
			}
		}
		if len(queue) > 0 {
			continue
		}
		if closed {
			return
		}
		select {
		case <-b.wake:
		case <-life.Done():
			return
		case <-b.abandoned:
			return
		}
	}
}

// StartStream resolves the session's streams — reuse, allocate, narrow,
// evict, degrade — and opens the WebRTC session with ProvideOffer
// (Offer set) or SolicitOffer, as the controller's identity: the camera
// stores RootNodeID as the session's peer and signals back to the
// controller's requestor. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1955-2171 at
// 0bd06f2.
func (s *Service) StartStream(ctx context.Context, req StartRequest) (*Session, error) {
	if s.isClosed() {
		return nil, ErrClosed
	}
	args, err := req.args()
	if err != nil {
		return nil, err
	}
	sess := &Session{Node: req.Node, Endpoint: req.Endpoint, Owner: req.Owner, Key: req.Key, svc: s, t: args.t, box: newMailbox(s.life)} //nolint:contextcheck // the mailbox lives as long as the service, not the call
	var registered *sessionKey
	args.establishing = func(id uint16, life *lifecycle) {
		k := sessionKey{args.t.nodeID, args.t.endpointID, id}
		registered = &k
		sess.ID = id
		s.mu.Lock()
		sess.life = life
		s.sessions[k] = sess
		s.mu.Unlock()
	}
	res, err := s.m.startStream(ctx, args)
	if err != nil {
		// The host never learned the session's id: its held signals go,
		// and with them the pump that would wait for a reader.
		if registered != nil {
			s.mu.Lock()
			if s.sessions[*registered] == sess {
				delete(s.sessions, *registered)
			}
			s.mu.Unlock()
		}
		sess.box.discard()
		return nil, err
	}
	sess.ID, sess.Mode, sess.Talkback = res.sessionID, res.mode, res.talkback
	if res.video != nil {
		sess.Streams = append(sess.Streams, leaseOf(StreamVideo, res.video))
		sess.Evicted = slices.Clone(res.video.evicted)
	}
	if res.audio != nil {
		sess.Streams = append(sess.Streams, leaseOf(StreamAudio, res.audio))
	}
	return sess, nil
}

// args checks a start against the cluster's field ranges and the rules a
// camera_start_stream follows, and converts it. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraCommands.ts:397-552 at 0bd06f2.
func (req StartRequest) args() (startArgs, error) {
	t, err := targetOf(req.Node, req.Endpoint)
	if err != nil {
		return startArgs{}, err
	}
	if !usageAllowed(req.Usage) {
		return startArgs{}, invalidArguments("Unknown or device-only stream_usage %d", req.Usage)
	}
	if req.Talkback && req.Offer == nil {
		return startArgs{}, invalidArguments("talkback: true requires sdp; without it the answer decides talkback")
	}
	if req.Key != "" && jsLength(req.Key) > clientSessionKeyMaxLength {
		return startArgs{}, invalidArguments("client_session_key must be a string of 1 to %d characters", clientSessionKeyMaxLength)
	}
	if req.ICETransportPolicy != "" && jsLength(req.ICETransportPolicy) > iceServerLimits.maxTransportPolicyLength {
		return startArgs{}, invalidArguments("ice_transport_policy must be a string of 1 to %d characters", iceServerLimits.maxTransportPolicyLength)
	}
	if err := validateICEServers(req.ICEServers); err != nil {
		return startArgs{}, err
	}
	a := startArgs{t: t, connectionID: req.Owner, usage: req.Usage, sdp: req.Offer, metadata: req.MetadataEnabled, allowEviction: req.AllowEviction == nil || *req.AllowEviction, talkback: req.Talkback, clientSessionKey: req.Key}
	if req.ICEServers != nil {
		servers := slices.Clone(req.ICEServers)
		a.iceServers = &servers
	}
	if req.ICETransportPolicy != "" {
		p := req.ICETransportPolicy
		a.icePolicy = &p
	}
	switch {
	case req.DeclineVideo && req.Video != nil:
		return startArgs{}, invalidArguments("video cannot be declined and bounded at once")
	case req.DeclineVideo:
		a.videoState = trackDeclined
	case req.Video != nil:
		a.videoState = trackDemanded
		if a.video, a.videoCodecs, err = req.Video.hints(); err != nil {
			return startArgs{}, err
		}
	}
	switch {
	case req.DeclineAudio && req.Audio != nil:
		return startArgs{}, invalidArguments("audio cannot be declined and bounded at once")
	case req.DeclineAudio:
		a.audioState = trackDeclined
	case req.Audio != nil:
		a.audioState = trackDemanded
		if a.audio, err = req.Audio.hints(); err != nil {
			return startArgs{}, err
		}
	}
	return a, nil
}

// upperCodecs upper-cases codec names to the SDP rtpmap spelling. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraCommands.ts:131-134
// at 0bd06f2.
func upperCodecs(names []string) []string {
	if names == nil {
		return nil
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = strings.ToUpper(n)
	}
	return out
}

// checkResolution refuses a stated resolution outside the cluster's
// range. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraCommands.ts:75-88 at 0bd06f2.
func checkResolution(r *csdef.VideoResolutionStruct, field string) (resolution, error) {
	if r == nil {
		return resolution{}, nil
	}
	w, h := cameraFieldRanges.resolutionWidth, cameraFieldRanges.resolutionHeight
	if int64(r.Width) < w.min || int64(r.Width) > w.max || int64(r.Height) < h.min || int64(r.Height) > h.max {
		return resolution{}, invalidArguments("%s must be an object whose width is %s and whose height is %s", field, rangeText(w), rangeText(h))
	}
	return resolutionOf(*r), nil
}

// checkRange refuses a stated value outside its field's range and is the
// value, 0 for an unstated (nil) one — every range's floor is at least 1,
// so 0 stays free to mean unstated inside the policy (a package test
// holds every hint range's floor at 1 or more). Mirrors
// matterjs-server's toOptionalNumber (cameraCommands.ts:378-382 at
// 0bd06f2): undefined is unstated, a stated value is held to the range
// fieldRange takes from the constraint (cameraFieldRanges.ts:48-56).
func checkRange[T ~uint8 | ~uint16 | ~uint32](v *T, field string, r fieldRange) (int, error) {
	if v == nil {
		return 0, nil
	}
	if n := int64(*v); n < r.min || n > r.max {
		return 0, invalidArguments("%s must be %s", field, rangeText(r))
	}
	return int(*v), nil
}

// hints converts the video bounds. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraCommands.ts:427-458 at 0bd06f2.
func (b *VideoBounds) hints() (*videoHints, []string, error) {
	r := cameraFieldRanges
	minFrameRate, err := checkRange(b.MinFrameRate, "video.min_frame_rate", r.minFrameRate)
	if err != nil {
		return nil, nil, err
	}
	maxFrameRate, err := checkRange(b.MaxFrameRate, "video.max_frame_rate", r.maxFrameRate)
	if err != nil {
		return nil, nil, err
	}
	minBitRate, err := checkRange(b.MinBitRate, "video.min_bit_rate", r.minBitRate)
	if err != nil {
		return nil, nil, err
	}
	maxBitRate, err := checkRange(b.MaxBitRate, "video.max_bit_rate", r.maxBitRate)
	if err != nil {
		return nil, nil, err
	}
	minRes, err := checkResolution(b.MinResolution, "video.min_resolution")
	if err != nil {
		return nil, nil, err
	}
	maxRes, err := checkResolution(b.MaxResolution, "video.max_resolution")
	if err != nil {
		return nil, nil, err
	}
	h := &videoHints{
		videoRangeBounds: videoRangeBounds{
			minResolution: minRes, maxResolution: maxRes,
			minFrameRate: minFrameRate, maxFrameRate: maxFrameRate,
			minBitRate: minBitRate, maxBitRate: maxBitRate,
		},
		overlays: overlayBoundsOf(b.Overlays.Watermark, b.Overlays.OSD),
	}
	return h, upperCodecs(b.Codecs), nil
}

// hints converts the audio values. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraCommands.ts:460-475 at 0bd06f2.
func (b *AudioBounds) hints() (*audioHints, error) {
	r := cameraFieldRanges
	channelCount, err := checkRange(b.ChannelCount, "audio.channel_count", r.channelCount)
	if err != nil {
		return nil, err
	}
	sampleRate, err := checkRange(b.SampleRate, "audio.sample_rate", r.sampleRate)
	if err != nil {
		return nil, err
	}
	bitRate, err := checkRange(b.BitRate, "audio.bit_rate", r.audioBitRate)
	if err != nil {
		return nil, err
	}
	return &audioHints{codecs: upperCodecs(b.Codecs), channelCount: channelCount, sampleRate: sampleRate, bitRate: bitRate}, nil
}

// validateICEServers holds typed ICE servers to the struct's ceilings and
// this controller's floor of one character. Mirrors matterjs-server
// packages/ws-controller/src/camera/webRtcProviderArguments.ts:56-120 at
// 0bd06f2.
func validateICEServers(servers []wpdef.ICEServerStruct) error {
	l := iceServerLimits
	if len(servers) > l.maxServers {
		return invalidArguments("ice_servers takes 0 to %d entries", l.maxServers)
	}
	for i, s := range servers {
		if len(s.Urls) == 0 || len(s.Urls) > l.maxUrls {
			return invalidArguments("ice_servers[%d].urls takes 1 to %d URLs", i, l.maxUrls)
		}
		for _, u := range s.Urls {
			if n := jsLength(u); n == 0 || n > l.maxURLLength {
				return invalidArguments("ice_servers[%d].urls takes URLs of 1 to %d characters", i, l.maxURLLength)
			}
		}
		if s.Username != nil && (jsLength(*s.Username) == 0 || jsLength(*s.Username) > l.maxUsernameLength) {
			return invalidArguments("ice_servers[%d].username must be 1 to %d characters", i, l.maxUsernameLength)
		}
		if s.Credential != nil && (jsLength(*s.Credential) == 0 || jsLength(*s.Credential) > l.maxCredentialLength) {
			return invalidArguments("ice_servers[%d].credential must be 1 to %d characters", i, l.maxCredentialLength)
		}
		if s.Caid != nil && int64(*s.Caid) > l.caid.max {
			return invalidArguments("ice_servers[%d].caid must be %s", i, rangeText(l.caid))
		}
	}
	return nil
}

// ResolutionTarget is a snapshot size a host wants: at least one side
// stated, 0 unstated.
type ResolutionTarget struct {
	Width, Height uint16
}

// SnapshotRequest is one still frame. Nil fields are unstated.
type SnapshotRequest struct {
	Node     *client.Node
	Endpoint uint16
	// MaxResolution is a ceiling on the frame.
	MaxResolution *csdef.VideoResolutionStruct
	// Resolution is the size the host wants; the camera gets the closest
	// it can give.
	Resolution *ResolutionTarget
	Codec      *csdef.ImageCodecEnum
	Overlays   Overlays
}

// Snapshot is one still frame and the stream it came from, which the
// camera keeps. Degraded: the frame is smaller than the best the request
// allowed.
type Snapshot struct {
	Data       []byte
	Codec      csdef.ImageCodecEnum
	Resolution csdef.VideoResolutionStruct
	Degraded   bool
	StreamID   uint16
	Provenance Provenance
}

// Snapshot captures one still frame from a snapshot stream the camera
// keeps, adopting one within the request's bounds or allocating one.
// After Close it is [ErrClosed] and allocates nothing (review II,
// R2-P1-cam-b-04). Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2389-2649 at
// 0bd06f2.
func (s *Service) Snapshot(ctx context.Context, req SnapshotRequest) (*Snapshot, error) {
	if s.isClosed() {
		return nil, ErrClosed
	}
	t, err := targetOf(req.Node, req.Endpoint)
	if err != nil {
		return nil, err
	}
	maxRes, err := checkResolution(req.MaxResolution, "max_resolution")
	if err != nil {
		return nil, err
	}
	args := snapshotArgs{t: t, maxResolution: maxRes, codec: req.Codec, overlays: overlayBoundsOf(req.Overlays.Watermark, req.Overlays.OSD)}
	if rt := req.Resolution; rt != nil {
		if rt.Width == 0 && rt.Height == 0 {
			return nil, invalidArguments("resolution must be an object with a width, a height, or both")
		}
		args.target = &resolutionTarget{width: int(rt.Width), height: int(rt.Height)}
	}
	r, err := s.m.snapshot(ctx, args)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Data: r.data, Codec: r.codec, Resolution: wireResolution(r.resolution), Degraded: r.degraded, StreamID: r.streamID, Provenance: r.provenance}, nil
}

var _ = wrdef.ClusterID
