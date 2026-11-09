// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package cameratest is a test camera for a host's in-process tests of
// controller/camera: a [Camera] device to bridge into a fabrictest site,
// and the scripted [Port] it serves its WebRTC transport and its streams
// on. The port answers every offer with a session, numbers its allocations,
// records the host's answers, candidates and ends, and refuses an
// operation on request ([Port.Refuse]). The camera's own signals to the
// controller — [Camera.Offer], [Camera.Answer], [Camera.IceCandidates],
// [Camera.End] — need no import of cluster/camera.
//
//	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true})
//	cam, err := cameratest.New(cameratest.Options{})
//	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{cam.Device()}, Configure: func(cfg *node.Config) {
//		cfg.Network.TCP = node.TCPConfig{Incoming: true}
//	}})
//
// The camera serves H.264 video up to 1920x1080 at 30 fps, audio and
// snapshots, with one allocated live-view stream (id 1, 640x480 to
// 1280x720 at 15 to 30 fps), LiveView and Recording as its usages and a
// JPEG snapshot capability at 640x480. The signals, like every call into a
// node, run where the site runs the node's work (fabrictest.Site.Run).
package cameratest

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	clustercamera "github.com/SukramJ/go-fabric/cluster/camera"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
)

// AllocatedVideoStream is the id of the live-view video stream the camera
// holds from the start.
const AllocatedVideoStream uint16 = 1

// Options name the camera. The zero value is a camera keyed "camera" and
// named "Camera" on a fresh [Port].
type Options struct {
	// Key is the device's stable key; "camera" when empty.
	Key string
	// Name is the device's name; "Camera" when empty.
	Name string
	// Port is the camera's scripted port; [NewPort] when nil.
	Port *Port
}

// Camera is the test camera: the device, its port and the provider its
// signals go out through.
type Camera struct {
	dev      *device.Camera
	port     *Port
	provider *clustercamera.Provider
}

// New builds the camera.
func New(opts Options) (*Camera, error) {
	if opts.Key == "" {
		opts.Key = "camera"
	}
	if opts.Name == "" {
		opts.Name = "Camera"
	}
	if opts.Port == nil {
		opts.Port = NewPort()
	}
	port := opts.Port
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView, csdef.StreamUsageRecording}
	off := false
	streams := []csdef.VideoStreamStruct{{
		VideoStreamId: AllocatedVideoStream, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264,
		MinFrameRate: 15, MaxFrameRate: 30,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720},
		MinBitRate: 100_000, MaxBitRate: 2_000_000, KeyFrameInterval: 4000, WatermarkEnabled: &off, OsdEnabled: &off,
	}}
	points := []csdef.RateDistortionTradeOffPointsStruct{{Codec: csdef.VideoCodecH264, Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MinBitRate: 100_000}}
	sensor := csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30}
	minView := csdef.VideoResolutionStruct{Width: 640, Height: 480}
	snapshots := []csdef.SnapshotCapabilitiesStruct{{Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxFrameRate: 1, ImageCodec: csdef.ImageCodecJpeg}}
	encoders := uint8(4)
	pixelRate := uint32(1920 * 1080 * 30 * 4)
	bandwidth := uint32(20_000_000)
	dev, err := device.NewCamera(device.Options{StableKey: device.StringKey(opts.Key), Name: opts.Name},
		csdef.Declare{
			Features: csdef.FeatureVideo | csdef.FeatureAudio | csdef.FeatureSnapshot,
			Initial: csdef.Patch{
				SupportedStreamUsages: &usages, StreamUsagePriorities: &usages, AllocatedVideoStreams: &streams,
				RateDistortionTradeOffPoints: &points, VideoSensorParams: &sensor, MinViewportResolution: &minView, SnapshotCapabilities: &snapshots,
				MaxConcurrentEncoders: &encoders, MaxEncodedPixelRate: &pixelRate, MaxNetworkBandwidth: &bandwidth,
			},
			VideoStreamPort: port, AudioStreamPort: port, SnapshotStreamPort: port, SnapshotCapturePort: port,
		},
		wpdef.Declare{ProviderPort: port},
	)
	if err != nil {
		return nil, err
	}
	provider, ok := dev.WebRtcTransportProvider().Underlying().(*clustercamera.Provider)
	if !ok {
		return nil, errors.New("cameratest: the camera serves no WebRTC provider")
	}
	port.mu.Lock()
	port.answer = provider.Answer
	port.mu.Unlock()
	return &Camera{dev: dev, port: port, provider: provider}, nil
}

// Device is the camera device, the endpoint a fabrictest topology bridges.
func (c *Camera) Device() *device.Camera { return c.dev }

// Port is the camera's scripted port.
func (c *Camera) Port() *Port { return c.port }

// Offer sends the camera's offer for a solicited session to its requestor.
func (c *Camera) Offer(ctx context.Context, sessionID uint16, sdp string) error {
	return c.provider.Offer(ctx, sessionID, sdp)
}

// Answer sends the camera's answer to the requestor's offer.
func (c *Camera) Answer(ctx context.Context, sessionID uint16, sdp string) error {
	return c.provider.Answer(ctx, sessionID, sdp)
}

// IceCandidates sends the camera's ICE candidates; an empty list is
// refused.
func (c *Camera) IceCandidates(ctx context.Context, sessionID uint16, candidates []wpdef.ICECandidateStruct) error {
	return c.provider.IceCandidates(ctx, sessionID, candidates)
}

// End ends the session from the camera's side with reason.
func (c *Camera) End(ctx context.Context, sessionID uint16, reason uint8) error {
	return c.provider.End(ctx, sessionID, reason)
}

// Op names an operation of the [Port] a test can refuse.
type Op string

// The port's operations.
const (
	OpSolicitOffer             Op = "SolicitOffer"
	OpProvideOffer             Op = "ProvideOffer"
	OpProvideAnswer            Op = "ProvideAnswer"
	OpProvideICECandidates     Op = "ProvideICECandidates"
	OpEndSession               Op = "EndSession"
	OpVideoStreamAllocate      Op = "VideoStreamAllocate"
	OpAudioStreamAllocate      Op = "AudioStreamAllocate"
	OpSnapshotStreamAllocate   Op = "SnapshotStreamAllocate"
	OpCaptureSnapshot          Op = "CaptureSnapshot"
	OpVideoStreamModify        Op = "VideoStreamModify"
	OpVideoStreamDeallocate    Op = "VideoStreamDeallocate"
	OpAudioStreamDeallocate    Op = "AudioStreamDeallocate"
	OpSnapshotStreamModify     Op = "SnapshotStreamModify"
	OpSnapshotStreamDeallocate Op = "SnapshotStreamDeallocate"
)

// Port is the camera's WebRTC stack and encoders: every offer becomes a
// session, every allocation a new stream id from 11 up (an allocation
// that names a stream reuses it), every capture a two-byte JPEG. It is
// safe for concurrent use.
type Port struct {
	mu      sync.Mutex
	offers  []wpdef.OfferArgs
	ends    map[uint16]uint8
	cands   map[uint16][]wpdef.ICECandidateStruct
	answers map[uint16]string
	nextID  uint16
	refuse  map[Op]error
	// answer is the camera's Answer signal (the provider's); afterSDP the
	// SDP AnswerAfterResponse sends, afterErrs each such Answer's outcome.
	answer    func(ctx context.Context, sessionID uint16, sdp string) error
	afterSDP  string
	afterErrs map[uint16]error
}

// NewPort returns an empty port.
func NewPort() *Port {
	return &Port{
		nextID: 10, ends: map[uint16]uint8{}, cands: map[uint16][]wpdef.ICECandidateStruct{},
		answers: map[uint16]string{}, refuse: map[Op]error{}, afterErrs: map[uint16]error{},
	}
}

// AnswerAfterResponse makes the camera answer each ProvideOffer with sdp
// as soon as the offer's response is on the wire, as chip's camera app
// schedules its Answer once the response is out
// (examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp:865-889
// at 6170af84); "" stops it. AnsweredAfterResponse reports each such
// Answer. A port without a [Camera] sends none.
func (p *Port) AnswerAfterResponse(sdp string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.afterSDP = sdp
}

// AnsweredAfterResponse reports whether the Answer AnswerAfterResponse
// sent for the session came back, and its outcome — nil when the
// requestor took it.
func (p *Port) AnsweredAfterResponse(sessionID uint16) (sent bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	err, sent = p.afterErrs[sessionID]
	return sent, err
}

// answerAfterResponse defers the Answer of AnswerAfterResponse until the
// response of the offer ctx carries is sent.
func (p *Port) answerAfterResponse(ctx context.Context, sessionID uint16) {
	p.mu.Lock()
	sdp, answer := p.afterSDP, p.answer
	p.mu.Unlock()
	if sdp == "" || answer == nil {
		return
	}
	// The Answer outlives the offer's interaction, as the camera's own
	// signal does.
	detached := context.WithoutCancel(ctx)
	send := func() {
		go func() {
			actx, cancel := context.WithTimeout(detached, 30*time.Second)
			defer cancel()
			err := answer(actx, sessionID, sdp)
			p.mu.Lock()
			p.afterErrs[sessionID] = err
			p.mu.Unlock()
		}()
	}
	if !im.DeferAfterResponse(ctx, send) {
		send()
	}
}

// Refuse makes op fail with err from now on; a nil err accepts it again.
func (p *Port) Refuse(op Op, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		delete(p.refuse, op)
		return
	}
	p.refuse[op] = err
}

func (p *Port) refused(op Op) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refuse[op]
}

// Offers are the offers the camera accepted, ProvideOffer and
// SolicitOffer alike, in order.
func (p *Port) Offers() []wpdef.OfferArgs {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.offers)
}

// LastOffer is the latest accepted offer, the zero value before one.
func (p *Port) LastOffer() wpdef.OfferArgs {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.offers) == 0 {
		return wpdef.OfferArgs{}
	}
	return p.offers[len(p.offers)-1]
}

// Ended is the reason the session was ended with, false while it is not.
func (p *Port) Ended(sessionID uint16) (reason uint8, ended bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reason, ended = p.ends[sessionID]
	return reason, ended
}

// Candidates are the ICE candidates the host provided for the session.
func (p *Port) Candidates(sessionID uint16) []wpdef.ICECandidateStruct {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.cands[sessionID])
}

// Answered is the answer the host provided for the session.
func (p *Port) Answered(sessionID uint16) (sdp string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sdp, ok = p.answers[sessionID]
	return sdp, ok
}

func (p *Port) session(args wpdef.OfferArgs) wpdef.WebRTCSessionStruct {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.offers = append(p.offers, args)
	return wpdef.WebRTCSessionStruct{
		Id: args.SessionID, PeerNodeId: args.PeerNodeID, PeerEndpointId: args.OriginatingEndpointID,
		StreamUsage: args.StreamUsage, VideoStreams: args.VideoStreams, AudioStreams: args.AudioStreams, FabricIndex: args.FabricIndex,
	}
}

func (p *Port) allocate() uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	return p.nextID
}

// SolicitOffer implements the WebRTC provider port: the session, deferred
// offer false.
func (p *Port) SolicitOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	if err := p.refused(OpSolicitOffer); err != nil {
		return wpdef.WebRTCSessionStruct{}, false, err
	}
	return p.session(args), false, nil
}

// ProvideOffer implements the WebRTC provider port; with
// [Port.AnswerAfterResponse] the camera answers once the response is out.
func (p *Port) ProvideOffer(ctx context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	if err := p.refused(OpProvideOffer); err != nil {
		return wpdef.WebRTCSessionStruct{}, err
	}
	session := p.session(args)
	p.answerAfterResponse(ctx, session.Id)
	return session, nil
}

// ProvideAnswer implements the WebRTC provider port.
func (p *Port) ProvideAnswer(_ context.Context, id uint16, sdp string) error {
	if err := p.refused(OpProvideAnswer); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answers[id] = sdp
	return nil
}

// ProvideIceCandidates implements the WebRTC provider port.
func (p *Port) ProvideIceCandidates(_ context.Context, id uint16, c []wpdef.ICECandidateStruct) error {
	if err := p.refused(OpProvideICECandidates); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cands[id] = append(p.cands[id], c...)
	return nil
}

// EndSession implements the WebRTC provider port.
func (p *Port) EndSession(_ context.Context, id uint16, reason uint8) error {
	if err := p.refused(OpEndSession); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ends[id] = reason
	return nil
}

// VideoStreamAllocate implements the video stream port: a named stream is
// reused, otherwise a new id.
func (p *Port) VideoStreamAllocate(_ context.Context, args csdef.VideoStreamStruct) (uint16, error) {
	if err := p.refused(OpVideoStreamAllocate); err != nil {
		return 0, err
	}
	if args.VideoStreamId != 0 {
		return args.VideoStreamId, nil
	}
	return p.allocate(), nil
}

// VideoStreamAllocated implements the video stream port.
func (p *Port) VideoStreamAllocated(context.Context, csdef.VideoStreamStruct, csdef.StreamAllocationAction) {
}

// VideoStreamModify implements the video stream port.
func (p *Port) VideoStreamModify(context.Context, uint16, *bool, *bool) error {
	return p.refused(OpVideoStreamModify)
}

// VideoStreamDeallocate implements the video stream port.
func (p *Port) VideoStreamDeallocate(context.Context, uint16) error {
	return p.refused(OpVideoStreamDeallocate)
}

// AudioStreamAllocate implements the audio stream port.
func (p *Port) AudioStreamAllocate(context.Context, csdef.AudioStreamStruct) (uint16, error) {
	if err := p.refused(OpAudioStreamAllocate); err != nil {
		return 0, err
	}
	return p.allocate(), nil
}

// AudioStreamDeallocate implements the audio stream port.
func (p *Port) AudioStreamDeallocate(context.Context, uint16) error {
	return p.refused(OpAudioStreamDeallocate)
}

// SnapshotStreamAllocate implements the snapshot stream port.
func (p *Port) SnapshotStreamAllocate(context.Context, csdef.SnapshotStreamStruct) (uint16, error) {
	if err := p.refused(OpSnapshotStreamAllocate); err != nil {
		return 0, err
	}
	return p.allocate(), nil
}

// SnapshotStreamModify implements the snapshot stream port.
func (p *Port) SnapshotStreamModify(context.Context, uint16, *bool, *bool) error {
	return p.refused(OpSnapshotStreamModify)
}

// SnapshotStreamDeallocate implements the snapshot stream port.
func (p *Port) SnapshotStreamDeallocate(context.Context, uint16) error {
	return p.refused(OpSnapshotStreamDeallocate)
}

// CaptureSnapshot implements the snapshot capture port: a two-byte JPEG
// at the requested resolution.
func (p *Port) CaptureSnapshot(_ context.Context, _ *uint16, r csdef.VideoResolutionStruct) (csdef.CaptureSnapshotResponse, error) {
	if err := p.refused(OpCaptureSnapshot); err != nil {
		return csdef.CaptureSnapshotResponse{}, err
	}
	return csdef.CaptureSnapshotResponse{Data: []byte{0xFF, 0xD8}, ImageCodec: csdef.ImageCodecJpeg, Resolution: r}, nil
}
