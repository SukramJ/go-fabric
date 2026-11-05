// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"errors"
	"slices"

	"github.com/SukramJ/go-fabric/client/interaction"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/im"
)

// privacyState is the camera's three privacy switches; a nil switch is one
// the camera does not report. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:194-204 at 0bd06f2.
type privacyState struct {
	softRecording  *bool
	softLivestream *bool
	hard           *bool
}

// sensorParams is VideoSensorParams with the HDR capability the feature
// map states. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:444-450 at
// 0bd06f2.
type sensorParams struct {
	width, height int
	maxFPS        int
	maxHDRFPS     *int
	hdrCapable    bool
}

// cameraState is the AVSM state the policies read: the attributes and the
// FeatureMap, absent ones unset. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:436-466 at
// 0bd06f2.
type cameraState struct {
	features              csdef.Feature
	privacy               privacyState
	maxConcurrentEncoders ceiling
	maxEncodedPixelRate   ceiling
	maxNetworkBandwidth   ceiling
	sensor                *sensorParams
	minViewport           *resolution
	rateDistortionPoints  []rateDistortionPoint
	snapshotCapabilities  []snapshotCapability
	supportedStreamUsages []csdef.StreamUsageEnum
	streamUsagePriorities []csdef.StreamUsageEnum
	video                 []allocatedVideoStream
	audio                 []allocatedAudioStream
	snapshot              []allocatedSnapshotStream
	microphone            *audioCapabilities
	twoWayTalkSupport     *csdef.TwoWayTalkSupportTypeEnum
}

// codecs is the camera's video codecs in RateDistortionTradeOffPoints
// order, each once. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1109-1112 at
// 0bd06f2.
func (s *cameraState) codecs() []csdef.VideoCodecEnum {
	var out []csdef.VideoCodecEnum
	for _, p := range s.rateDistortionPoints {
		if !slices.Contains(out, p.codec) {
			out = append(out, p.codec)
		}
	}
	return out
}

// toCameraState converts the AVSM state the controller's image holds. An
// attribute in absent is unset; RequiresHardwareEncoder absent reads as
// false and the overlay flags of a stream are not defaulted. Mirrors
// matterjs-server packages/ws-controller/src/camera/MatterCameraDeviceIo.ts:61-150
// at 0bd06f2.
func toCameraState(st csdef.State, absent []uint32, features csdef.Feature) *cameraState {
	has := func(id uint32) bool { return !slices.Contains(absent, id) }
	boolOf := func(id uint32, v bool) *bool {
		if !has(id) {
			return nil
		}
		return &v
	}
	ceilingOf := func(id uint32, v float64) ceiling {
		if !has(id) {
			return ceiling{}
		}
		return stated(v)
	}
	out := &cameraState{
		features: features,
		privacy: privacyState{
			softRecording:  boolOf(csdef.AttrSoftRecordingPrivacyModeEnabled, st.SoftRecordingPrivacyModeEnabled),
			softLivestream: boolOf(csdef.AttrSoftLivestreamPrivacyModeEnabled, st.SoftLivestreamPrivacyModeEnabled),
			hard:           boolOf(csdef.AttrHardPrivacyModeOn, st.HardPrivacyModeOn),
		},
		maxConcurrentEncoders: ceilingOf(csdef.AttrMaxConcurrentEncoders, float64(st.MaxConcurrentEncoders)),
		maxEncodedPixelRate:   ceilingOf(csdef.AttrMaxEncodedPixelRate, float64(st.MaxEncodedPixelRate)),
		maxNetworkBandwidth:   ceilingOf(csdef.AttrMaxNetworkBandwidth, float64(st.MaxNetworkBandwidth)),
		supportedStreamUsages: slices.Clone(st.SupportedStreamUsages),
		streamUsagePriorities: slices.Clone(st.StreamUsagePriorities),
	}
	if has(csdef.AttrVideoSensorParams) {
		p := st.VideoSensorParams
		sp := &sensorParams{width: int(p.SensorWidth), height: int(p.SensorHeight), maxFPS: int(p.MaxFps), hdrCapable: features&csdef.FeatureHighDynamicRange != 0}
		if p.MaxHdrfps != nil {
			v := int(*p.MaxHdrfps)
			sp.maxHDRFPS = &v
		}
		out.sensor = sp
	}
	if has(csdef.AttrMinViewportResolution) {
		r := resolutionOf(st.MinViewportResolution)
		out.minViewport = &r
	}
	for _, p := range st.RateDistortionTradeOffPoints {
		out.rateDistortionPoints = append(out.rateDistortionPoints, rateDistortionPoint{codec: p.Codec, resolution: resolutionOf(p.Resolution), minBitRate: int(p.MinBitRate)})
	}
	for _, c := range st.SnapshotCapabilities {
		out.snapshotCapabilities = append(out.snapshotCapabilities, snapshotCapability{
			resolution: resolutionOf(c.Resolution), maxFrameRate: int(c.MaxFrameRate), imageCodec: c.ImageCodec,
			requiresEncodedPixels: c.RequiresEncodedPixels, requiresHardwareEncoder: c.RequiresHardwareEncoder != nil && *c.RequiresHardwareEncoder,
		})
	}
	for _, v := range st.AllocatedVideoStreams {
		out.video = append(out.video, videoStreamOf(v))
	}
	for _, a := range st.AllocatedAudioStreams {
		out.audio = append(out.audio, allocatedAudioStream{
			audioStreamID: a.AudioStreamId, streamUsage: a.StreamUsage, audioCodec: a.AudioCodec, channelCount: int(a.ChannelCount),
			sampleRate: int(a.SampleRate), bitRate: int(a.BitRate), bitDepth: int(a.BitDepth), referenceCount: int(a.ReferenceCount),
		})
	}
	for _, s := range st.AllocatedSnapshotStreams {
		out.snapshot = append(out.snapshot, allocatedSnapshotStream{
			overlays:         overlayBoundsOf(s.WatermarkEnabled, s.OsdEnabled),
			snapshotStreamID: s.SnapshotStreamId, imageCodec: s.ImageCodec,
			minResolution: resolutionOf(s.MinResolution), maxResolution: resolutionOf(s.MaxResolution),
			quality: int(s.Quality), referenceCount: int(s.ReferenceCount), frameRate: int(s.FrameRate),
			encodedPixels: s.EncodedPixels, hardwareEncoder: s.HardwareEncoder,
		})
	}
	if has(csdef.AttrMicrophoneCapabilities) {
		m := st.MicrophoneCapabilities
		caps := &audioCapabilities{supportedCodecs: slices.Clone(m.SupportedCodecs), maxNumberOfChannels: int(m.MaxNumberOfChannels)}
		for _, r := range m.SupportedSampleRates {
			caps.supportedSampleRates = append(caps.supportedSampleRates, int(r))
		}
		for _, d := range m.SupportedBitDepths {
			caps.supportedBitDepths = append(caps.supportedBitDepths, int(d))
		}
		out.microphone = caps
	}
	if has(csdef.AttrTwoWayTalkSupport) {
		v := st.TwoWayTalkSupport
		out.twoWayTalkSupport = &v
	}
	return out
}

// videoStreamOf converts one AllocatedVideoStreams entry; its overlays are
// not defaulted (§11.2.6.11).
func videoStreamOf(v csdef.VideoStreamStruct) allocatedVideoStream {
	return allocatedVideoStream{
		overlays:      overlayBoundsOf(v.WatermarkEnabled, v.OsdEnabled),
		videoStreamID: v.VideoStreamId, streamUsage: v.StreamUsage, videoCodec: v.VideoCodec,
		minResolution: resolutionOf(v.MinResolution), maxResolution: resolutionOf(v.MaxResolution),
		minFrameRate: int(v.MinFrameRate), maxFrameRate: int(v.MaxFrameRate),
		minBitRate: int(v.MinBitRate), maxBitRate: int(v.MaxBitRate),
		keyFrameInterval: int(v.KeyFrameInterval), referenceCount: int(v.ReferenceCount),
	}
}

func resolutionOf(r csdef.VideoResolutionStruct) resolution {
	return resolution{width: int(r.Width), height: int(r.Height)}
}

// wireResolution is r as the cluster encodes it; the policies keep every
// size within uint16.
func wireResolution(r resolution) csdef.VideoResolutionStruct {
	return csdef.VideoResolutionStruct{Width: uint16(r.width), Height: uint16(r.height)} //nolint:gosec // G115: within the cluster's uint16 range by construction
}

func overlayBoundsOf(watermark, osd *bool) overlayBounds {
	of := func(b *bool) overlayFlag {
		if b == nil {
			return flagUnstated
		}
		return flagOf(*b)
	}
	return overlayBounds{watermark: of(watermark), osd: of(osd)}
}

// wireFlag is an overlay flag as an optional command field: unstated is
// not sent.
func wireFlag(f overlayFlag) *bool {
	switch f {
	case flagOn:
		v := true
		return &v
	case flagOff:
		v := false
		return &v
	default:
		return nil
	}
}

// featureTitles are the AVSM feature names in bit order, as matter.js
// spells them. Mirrors matterjs-server
// packages/ws-controller/src/camera/wireNames.ts:96-101 at 0bd06f2.
var featureTitles = []struct {
	flag  csdef.Feature
	title string
}{
	{csdef.FeatureAudio, "Audio"},
	{csdef.FeatureVideo, "Video"},
	{csdef.FeatureSnapshot, "Snapshot"},
	{csdef.FeaturePrivacy, "Privacy"},
	{csdef.FeatureSpeaker, "Speaker"},
	{csdef.FeatureImageControl, "ImageControl"},
	{csdef.FeatureWatermark, "Watermark"},
	{csdef.FeatureOnScreenDisplay, "OnScreenDisplay"},
	{csdef.FeatureLocalStorage, "LocalStorage"},
	{csdef.FeatureHighDynamicRange, "HighDynamicRange"},
	{csdef.FeatureNightVision, "NightVision"},
}

// featureName is the title of one feature. Mirrors matterjs-server
// packages/ws-controller/src/camera/wireNames.ts:114-116 at 0bd06f2.
func featureName(f csdef.Feature) string {
	for _, t := range featureTitles {
		if t.flag == f {
			return t.title
		}
	}
	return ""
}

// statedFeatureNames is the titles of the advertised features in bit
// order, nil while the FeatureMap has not arrived. Mirrors matterjs-server
// packages/ws-controller/src/camera/devicePolicy.ts:21-23 at 0bd06f2.
func statedFeatureNames(features csdef.Feature) []string {
	if !featureMapStated(features) {
		return nil
	}
	out := []string{}
	for _, t := range featureTitles {
		if features&t.flag != 0 {
			out = append(out, t.title)
		}
	}
	return out
}

// lacksFeature: an unstated map never lacks a feature, so the request
// goes to the device and it answers for itself. Mirrors matterjs-server
// packages/ws-controller/src/camera/devicePolicy.ts:25-28 at 0bd06f2.
func lacksFeature(features, feature csdef.Feature) bool {
	return featureMapStated(features) && features&feature == 0
}

func isOn(b *bool) bool { return b != nil && *b }

// sessionPrivacyModes are the switches that forbid a session of usage, per
// SolicitOffer (§11.5.6.1.10) and ProvideOffer (§11.5.6.3.12). Mirrors
// matterjs-server packages/ws-controller/src/camera/devicePolicy.ts:35-49
// at 0bd06f2.
func sessionPrivacyModes(p privacyState, usage csdef.StreamUsageEnum) []PrivacyMode {
	var modes []PrivacyMode
	if isOn(p.hard) {
		modes = append(modes, PrivacyHard)
	}
	if isOn(p.softLivestream) && usage == csdef.StreamUsageLiveView {
		modes = append(modes, PrivacySoftLivestream)
	}
	if isOn(p.softRecording) && (usage == csdef.StreamUsageRecording || usage == csdef.StreamUsageAnalysis) {
		modes = append(modes, PrivacySoftRecording)
	}
	return modes
}

// snapshotPrivacyModes are the switches that forbid a CaptureSnapshot
// (§11.2.8.13.3). Mirrors matterjs-server
// packages/ws-controller/src/camera/devicePolicy.ts:51-57 at 0bd06f2.
func snapshotPrivacyModes(p privacyState) []PrivacyMode {
	var modes []PrivacyMode
	if isOn(p.hard) {
		modes = append(modes, PrivacyHard)
	}
	if isOn(p.softLivestream) {
		modes = append(modes, PrivacySoftLivestream)
	}
	return modes
}

// deviceStatusOf is the Matter status a device answered a command with, ok
// false for any other failure (a timeout, a local error). Mirrors
// matterjs-server packages/ws-controller/src/camera/deviceStatus.ts:9-15 at
// 0bd06f2.
func deviceStatusOf(err error) (im.StatusCode, bool) {
	var se *interaction.StatusError
	if errors.As(err, &se) {
		return se.Status, true
	}
	return 0, false
}

// statusIs reports whether err is the device's answer status.
func statusIs(err error, status im.StatusCode) bool {
	s, ok := deviceStatusOf(err)
	return ok && s == status
}
