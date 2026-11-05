// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"slices"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// Capabilities is what a camera endpoint states and holds: its features,
// privacy switches, video, audio and snapshot capabilities, its limits,
// the streams allocated on it and the WebRTC sessions it reports for this
// fabric. A pointer field is nil when the camera does not state it.
// Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:500-535 at
// 0bd06f2.
type Capabilities struct {
	// Features are the advertised AVSM features in bit order, nil while the
	// camera has not stated its FeatureMap.
	Features  []string
	Privacy   Privacy
	Video     VideoCapabilities
	Audio     AudioCapabilities
	Snapshot  []csdef.SnapshotCapabilitiesStruct
	Limits    Limits
	Allocated Allocated
	// Sessions is the provider's CurrentSessions, empty without a provider.
	Sessions []DeviceSession
}

// Privacy is the camera's privacy switches (§11.2.7.20 to §11.2.7.22); nil
// is a switch the camera does not have.
type Privacy struct {
	SoftRecording  *bool
	SoftLivestream *bool
	Hard           *bool
}

// VideoCapabilities is the camera's video. Codecs are the codecs of
// RateDistortionPoints, each once, in their order.
type VideoCapabilities struct {
	Sensor               *csdef.VideoResolutionStruct
	MaxFPS               *uint16
	MaxHDRFPS            *uint16
	HDRCapable           *bool
	MinViewport          *csdef.VideoResolutionStruct
	RateDistortionPoints []csdef.RateDistortionTradeOffPointsStruct
	Codecs               []csdef.VideoCodecEnum
}

// AudioCapabilities is the camera's microphone and talkback.
type AudioCapabilities struct {
	Codecs            []csdef.AudioCodecEnum
	Channels          *uint8
	SampleRates       []uint32
	BitDepths         []uint8
	TwoWayTalkSupport *csdef.TwoWayTalkSupportTypeEnum
}

// Limits is the camera's encoder and bandwidth limits and its stream
// usages.
type Limits struct {
	MaxEncodedPixelRate   *uint32
	MaxConcurrentEncoders *uint8
	MaxNetworkBandwidth   *uint32
	SupportedStreamUsages []csdef.StreamUsageEnum
	StreamUsagePriorities []csdef.StreamUsageEnum
}

// Allocated is the streams the camera reports, each marked when this
// controller allocated it in this run.
type Allocated struct {
	Video    []AllocatedVideo
	Audio    []AllocatedAudio
	Snapshot []AllocatedSnapshot
}

// AllocatedVideo is one AllocatedVideoStreams entry.
type AllocatedVideo struct {
	csdef.VideoStreamStruct
	AllocatedByUs bool
}

// AllocatedAudio is one AllocatedAudioStreams entry.
type AllocatedAudio struct {
	csdef.AudioStreamStruct
	AllocatedByUs bool
}

// AllocatedSnapshot is one AllocatedSnapshotStreams entry.
type AllocatedSnapshot struct {
	csdef.SnapshotStreamStruct
	AllocatedByUs bool
}

func uint16Of(v int) *uint16 {
	u := uint16(v) //nolint:gosec // G115: a value the camera reported as uint16
	return &u
}

func ceilingUint32(c ceiling) *uint32 {
	if !c.set {
		return nil
	}
	v := uint32(c.value)
	return &v
}

// videoStreamStruct is the cluster form of a stream.
func videoStreamStruct(s allocatedVideoStream) csdef.VideoStreamStruct {
	return csdef.VideoStreamStruct{
		VideoStreamId: s.videoStreamID, StreamUsage: s.streamUsage, VideoCodec: s.videoCodec,
		MinFrameRate: uint16(s.minFrameRate), MaxFrameRate: uint16(s.maxFrameRate), //nolint:gosec // G115: reported as uint16
		MinResolution: wireResolution(s.minResolution), MaxResolution: wireResolution(s.maxResolution),
		MinBitRate: uint32(s.minBitRate), MaxBitRate: uint32(s.maxBitRate), //nolint:gosec // G115: reported as uint32
		KeyFrameInterval: uint16(s.keyFrameInterval), ReferenceCount: uint8(s.referenceCount), //nolint:gosec // G115: reported as uint16 and uint8
		WatermarkEnabled: wireFlag(s.overlays.watermark), OsdEnabled: wireFlag(s.overlays.osd),
	}
}

// capabilities reads what the camera states and holds. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:969-1034
// at 0bd06f2.
func (m *manager) capabilities(ctx context.Context, t target) (*Capabilities, error) {
	st, err := m.requireState(ctx, t)
	if err != nil {
		return nil, err
	}
	sessions, _ := m.io.readWebRTCSessions(ctx, t)
	if sessions == nil {
		sessions = []DeviceSession{}
	}
	c := &Capabilities{
		Features: statedFeatureNames(st.features),
		Privacy:  Privacy{SoftRecording: st.privacy.softRecording, SoftLivestream: st.privacy.softLivestream, Hard: st.privacy.hard},
		Sessions: sessions,
	}
	if s := st.sensor; s != nil {
		r := wireResolution(resolution{s.width, s.height})
		c.Video.Sensor = &r
		c.Video.MaxFPS = uint16Of(s.maxFPS)
		if s.maxHDRFPS != nil {
			c.Video.MaxHDRFPS = uint16Of(*s.maxHDRFPS)
		}
		hdr := s.hdrCapable
		c.Video.HDRCapable = &hdr
	}
	if st.minViewport != nil {
		r := wireResolution(*st.minViewport)
		c.Video.MinViewport = &r
	}
	c.Video.RateDistortionPoints = []csdef.RateDistortionTradeOffPointsStruct{}
	for _, p := range st.rateDistortionPoints {
		c.Video.RateDistortionPoints = append(c.Video.RateDistortionPoints, csdef.RateDistortionTradeOffPointsStruct{
			Codec: p.codec, Resolution: wireResolution(p.resolution), MinBitRate: uint32(p.minBitRate), //nolint:gosec // G115: reported as uint32
		})
	}
	c.Video.Codecs = append([]csdef.VideoCodecEnum{}, st.codecs()...)
	c.Audio = AudioCapabilities{Codecs: []csdef.AudioCodecEnum{}, SampleRates: []uint32{}, BitDepths: []uint8{}, TwoWayTalkSupport: st.twoWayTalkSupport}
	if mic := st.microphone; mic != nil {
		c.Audio.Codecs = slices.Clone(mic.supportedCodecs)
		ch := uint8(mic.maxNumberOfChannels) //nolint:gosec // G115: reported as uint8
		c.Audio.Channels = &ch
		for _, r := range mic.supportedSampleRates {
			c.Audio.SampleRates = append(c.Audio.SampleRates, uint32(r)) //nolint:gosec // G115: reported as uint32
		}
		for _, d := range mic.supportedBitDepths {
			c.Audio.BitDepths = append(c.Audio.BitDepths, uint8(d)) //nolint:gosec // G115: reported as uint8
		}
	}
	c.Snapshot = []csdef.SnapshotCapabilitiesStruct{}
	for _, s := range st.snapshotCapabilities {
		hw := s.requiresHardwareEncoder
		c.Snapshot = append(c.Snapshot, csdef.SnapshotCapabilitiesStruct{
			Resolution: wireResolution(s.resolution), MaxFrameRate: uint16(s.maxFrameRate), ImageCodec: s.imageCodec, //nolint:gosec // G115: reported as uint16
			RequiresEncodedPixels: s.requiresEncodedPixels, RequiresHardwareEncoder: &hw,
		})
	}
	c.Limits = Limits{
		MaxEncodedPixelRate:   ceilingUint32(st.maxEncodedPixelRate),
		MaxNetworkBandwidth:   ceilingUint32(st.maxNetworkBandwidth),
		SupportedStreamUsages: slices.Clone(st.supportedStreamUsages),
		StreamUsagePriorities: slices.Clone(st.streamUsagePriorities),
	}
	if st.maxConcurrentEncoders.set {
		v := uint8(st.maxConcurrentEncoders.value)
		c.Limits.MaxConcurrentEncoders = &v
	}
	c.Allocated = Allocated{Video: []AllocatedVideo{}, Audio: []AllocatedAudio{}, Snapshot: []AllocatedSnapshot{}}
	for _, v := range st.video {
		c.Allocated.Video = append(c.Allocated.Video, AllocatedVideo{videoStreamStruct(v), m.ownsStream(t, StreamVideo, v.videoStreamID)})
	}
	for _, a := range st.audio {
		c.Allocated.Audio = append(c.Allocated.Audio, AllocatedAudio{csdef.AudioStreamStruct{
			AudioStreamId: a.audioStreamID, StreamUsage: a.streamUsage, AudioCodec: a.audioCodec,
			ChannelCount: uint8(a.channelCount), SampleRate: uint32(a.sampleRate), BitRate: uint32(a.bitRate), //nolint:gosec // G115: reported widths
			BitDepth: uint8(a.bitDepth), ReferenceCount: uint8(a.referenceCount), //nolint:gosec // G115: reported widths
		}, m.ownsStream(t, StreamAudio, a.audioStreamID)})
	}
	for _, s := range st.snapshot {
		c.Allocated.Snapshot = append(c.Allocated.Snapshot, AllocatedSnapshot{csdef.SnapshotStreamStruct{
			SnapshotStreamId: s.snapshotStreamID, ImageCodec: s.imageCodec, FrameRate: uint16(s.frameRate), //nolint:gosec // G115: reported as uint16
			MinResolution: wireResolution(s.minResolution), MaxResolution: wireResolution(s.maxResolution),
			Quality: uint8(s.quality), ReferenceCount: uint8(s.referenceCount), //nolint:gosec // G115: reported as uint8
			EncodedPixels: s.encodedPixels, HardwareEncoder: s.hardwareEncoder,
			WatermarkEnabled: wireFlag(s.overlays.watermark), OsdEnabled: wireFlag(s.overlays.osd),
		}, m.ownsStream(t, StreamSnapshot, s.snapshotStreamID)})
	}
	return c, nil
}
