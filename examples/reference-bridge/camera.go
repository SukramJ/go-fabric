// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
)

// --camera-sim (TESTING ONLY): a Camera and a Chime for the certification
// harness's camera families (WEBRTCP, AVSM, PAVST, AVSUM, ZONEMGMT, CHIME),
// a simulation of connectedhomeip's camera-app at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20 (examples/camera-app/), whose
// camera the cases were written against. Nothing is captured, encoded or
// sent: every port answers the way the camera-app's managers answer and
// keeps the state they keep.
//
//   - "camera" is a Camera (camera-app endpoint 1, device type ma_camera,
//     camera-app.matter): CameraAvStreamManagement with every feature the
//     camera-app's HAL reports (camera-app.cpp
//     CreateAndInitializeCameraAVStreamMgmt; camera-device.cpp's
//     GetCameraSupports* all true, Has* all true), its capabilities from
//     camera-device.h:41-88 and camera-device.cpp:1491-1640, and the
//     camera-app's available streams (camera-device.cpp
//     InitializeVideoStreams, InitializeAudioStreams,
//     InitializeSnapshotStreams); WebRtcTransportProvider (the session the
//     webrtc-provider-manager.cpp HandleSolicitOffer builds, deferred with
//     --camera-deferred-offer as LinuxDeviceOptions.cameraDeferredOffer
//     says); PushAvStreamTransport over the node's TLS clusters
//     (--tls-sim); CameraAvSettingsUserLevelManagement with DPTZ, MPAN,
//     MTILT, MZOOM and MPRESETS, five presets, pan and tilt -90..90 and
//     zoom 75 (camera-app.cpp:43-64, camera-device.h:84-88); ZoneManagement
//     with TWODCART, PERZONESENS, USERDEFINED and FOCUSZONES, ten zones and
//     sensitivity 1..10 (camera-app.cpp:65-77, camera-device.h:69-71).
//   - "chime" is a Chime with the camera-app's three sounds
//     (chime-manager.h:41-45). The camera-app serves Chime on its camera
//     endpoint; the Camera device type lists no Chime, so here it is a
//     device of its own, where the CHIME family finds it by device type.
//
// The state the module does not persist (cluster/camera/doc.go: the
// streams, priorities and settings of the stream management, the camera's
// position, presets and DPTZ streams, the push transports, the chime's
// SelectedChime and Enabled) is kept here in the daemon's settings
// (cameraStateSetting) and restored as the declarations' Initial, so a
// daemon restarted on the same --db keeps it, as the camera-app keeps it
// in its KVS.
//
// The camera-app's switches the cases pass (their CI arguments):
// --camera-deferred-offer answers SolicitOffer with DeferredOffer true;
// --camera-test-videosrc gives CaptureSnapshot an image (a generated
// JPEG at the requested resolution), where without it the camera-app has
// no video device and the capture fails; --camera-test-audiosrc changes
// nothing observable here (no audio is captured).
//
// The app pipe: SetHardPrivacyModeOn {"Value": bool}, ZoneTriggered
// {"ZoneId": id | [ids]} (the zone manager's trigger timing,
// zone-manager.cpp OnZoneTriggeredEvent and OnZoneTriggerTimeout, and the
// push transports' motion trigger), and AvAnalysisSessionEnd, which the
// camera-app hands to its AvAnalysis cluster — not served here (no
// AvAnalysis cluster in the matter.js snapshot), so it is logged and
// dropped.

// The camera-app's capabilities (camera-device.h:41-88 at the pin).
const (
	camMaxContentBufferSize   uint32 = 4096
	camMaxNetworkBandwidthBps uint32 = 128000000
	camMaxConcurrentEncoders  uint8  = 1
	camMaxEncodedPixelRate    uint32 = 248832000
	camMinWidth, camMinHeight uint16 = 640, 480
	cam720Width, cam720Height uint16 = 1280, 720
	camMaxWidth, camMaxHeight uint16 = 1920, 1080
	camSnapshotFrameRate      uint16 = 30
	camMaxVideoFrameRate      uint16 = 120
	cam60fps, cam30fps        uint16 = 60, 30
	camMinVideoFrameRate      uint16 = 15
	camMinBitRate             uint32 = 10000
	camMaxBitRate             uint32 = 2000000
	camKeyFrameInterval       uint16 = 4000
	camSpeakerMinLevel        uint8  = 1
	camSpeakerMaxLevel        uint8  = 254
	camMicrophoneMinLevel     uint8  = 1
	camMicrophoneMaxLevel     uint8  = 254
	camMaxChannelCount        uint8  = 8
	camMaxZones               uint8  = 10
	camMaxUserDefinedZones    uint8  = 10
	camSensitivityMax         uint8  = 10
	camPanMin, camPanMax      int16  = -90, 90
	camTiltMin, camTiltMax    int16  = -90, 90
	camZoomMax                uint8  = 75
	camMaxPresets             uint8  = 5
	// camZoneTimerPeriod is the zone manager's trigger timer
	// (zone-manager.cpp:27 kTimerPeriod, one second).
	camZoneTimerPeriod = time.Second
	// cameraStateSetting is the settings key the camera's state is kept
	// under.
	cameraStateSetting = "camerasim.state"
)

// cameraOptions are the camera-app switches the daemon mirrors.
type cameraOptions struct {
	deferredOffer, testVideoSource, testAudioSource bool
}

// demoCamera is the --camera-sim pair: the camera and the chime, and the
// simulation behind their ports.
type demoCamera struct {
	logger  *slog.Logger
	surface surface.Policies
	opts    cameraOptions
	// clk paces the zone manager's trigger timer, a triggered push
	// transport's recording and the PTZ movement; the system clock,
	// a fake one in the tests.
	clk clock.Clock

	camera *device.Camera
	chime  *device.Chime

	mu sync.Mutex
	// st keeps the state (attach).
	st settings
	// restored is the state read from st, applied by devices.
	restored cameraState
	// videos and audios are the camera-app's available streams
	// (camera-device.cpp InitializeVideoStreams, InitializeAudioStreams);
	// snapshots every snapshot stream id it made (AddSnapshotStream).
	videos    []camAvailableVideo
	audios    []camAvailableAudio
	snapshots map[uint16]bool
	// zones are the zone ids the zone manager holds, triggers the
	// running trigger contexts (zone-manager.h ZoneTriggerContext).
	zones    []uint16
	triggers []*camTriggerContext
	timer    clock.Timer
	// tickBase and ticks place the trigger timer's ticks on the second
	// grid from the first trigger (zone-manager.cpp's periodic one-second
	// timer), so they do not drift by the work each tick does.
	tickBase time.Time
	ticks    int
	// transports are the push transports and their CMAF session numbers.
	transports map[uint16]*camTransport
	// sessions are the WebRTC sessions the provider manager holds
	// (webrtc-provider-manager.cpp mWebrtcTransportMap).
	sessions map[uint16]bool
}

type camAvailableVideo struct {
	params    csdef.VideoStreamStruct
	allocated bool
	// viewport is the stream's own (camera-device.cpp SetViewport per
	// stream), the camera's at its allocation.
	viewport avdef.ViewportStruct
}

type camAvailableAudio struct {
	params    csdef.AudioStreamStruct
	allocated bool
}

// camTransport is a push transport of the manager (push-av-stream-manager.cpp
// mTransportMap) and the recording its PushAVTransport runs.
type camTransport struct {
	trigger   padef.TransportTriggerOptionsStruct
	session   uint64
	status    padef.TransportStatusEnum
	streaming bool
	stop      clock.Timer
	// activated and detected are the last motion's activation time and
	// its detected duration (PushAVTransport mClipInfo.mActivationTime,
	// mMotionDetectedDurationS), kept while the transport is Inactive.
	activated time.Time
	detected  time.Duration
	// manual is an activation by ManuallyTriggerTransport, whose end
	// starts no blind period (mCurrentActivationByManualTrigger).
	manual bool
	// maxClip bounds an augmented recording (mMaxClipDurationS).
	maxClip time.Duration
}

// camTriggerContext is the zone manager's ZoneTriggerContext.
type camTriggerContext struct {
	trigger          zmdef.ZoneTriggerControlStruct
	blind            bool
	detectedDuration uint32
	prevDetected     uint32
	sinceInitial     uint32
	remainingBlind   uint32
	count            int
}

// cameraState is what the daemon keeps of the camera across restarts.
type cameraState struct {
	AvStream   json.RawMessage `json:"avStream,omitempty"`
	AvSettings json.RawMessage `json:"avSettings,omitempty"`
	Push       json.RawMessage `json:"push,omitempty"`
	Chime      json.RawMessage `json:"chime,omitempty"`
}

// The persisted attributes of each cluster (cluster/camera/doc.go
// persistence note): the State field names.
var (
	camPersistAvStream = []string{
		"AllocatedVideoStreams", "AllocatedAudioStreams", "AllocatedSnapshotStreams", "StreamUsagePriorities",
		"HdrModeEnabled", "SoftRecordingPrivacyModeEnabled", "SoftLivestreamPrivacyModeEnabled", "NightVision",
		"NightVisionIllum", "Viewport", "SpeakerMuted", "SpeakerVolumeLevel", "MicrophoneMuted", "MicrophoneVolumeLevel",
		"MicrophoneAgcEnabled", "ImageRotation", "ImageFlipHorizontal", "ImageFlipVertical", "LocalVideoRecordingEnabled",
		"LocalSnapshotRecordingEnabled", "StatusLightEnabled", "StatusLightBrightness",
	}
	camPersistAvSettings = []string{"MptzPosition", "MptzPresets", "DptzStreams"}
	camPersistChime      = []string{"SelectedChime", "Enabled"}
	camPersistPush       = []string{"CurrentConnections"}
)

// newDemoCamera returns the camera simulation; devices builds the
// endpoints once attach has restored the state.
func newDemoCamera(policies surface.Policies, opts cameraOptions, logger *slog.Logger) *demoCamera {
	c := &demoCamera{
		logger: logger, surface: policies, opts: opts, clk: clock.System{},
		snapshots: map[uint16]bool{}, transports: map[uint16]*camTransport{}, sessions: map[uint16]bool{},
	}
	c.videos = []camAvailableVideo{
		{params: camVideo(1, camMinVideoFrameRate, cam60fps, camMinWidth, camMinHeight, cam720Width, cam720Height)},
		{params: camVideo(2, cam60fps, camMaxVideoFrameRate, cam720Width, cam720Height, camMaxWidth, camMaxHeight)},
		{params: camVideo(3, camMinVideoFrameRate, camMaxVideoFrameRate, camMinWidth, camMinHeight, camMaxWidth, camMaxHeight)},
	}
	c.audios = []camAvailableAudio{
		{params: csdef.AudioStreamStruct{AudioStreamId: 1, StreamUsage: csdef.StreamUsageLiveView, AudioCodec: csdef.AudioCodecOpus, ChannelCount: 1, SampleRate: 48000, BitRate: 20000, BitDepth: 24}},
		{params: csdef.AudioStreamStruct{AudioStreamId: 2, StreamUsage: csdef.StreamUsageLiveView, AudioCodec: csdef.AudioCodecOpus, ChannelCount: 2, SampleRate: 48000, BitRate: 32000, BitDepth: 24}},
		{params: csdef.AudioStreamStruct{AudioStreamId: 3, StreamUsage: csdef.StreamUsageLiveView, AudioCodec: csdef.AudioCodecOpus, ChannelCount: camMaxChannelCount, SampleRate: 48000, BitRate: 64000, BitDepth: 24}},
	}
	// InitializeSnapshotStreams: one available stream, id 0.
	c.snapshots[0] = false
	return c
}

func camVideo(id, minFPS, maxFPS, minW, minH, maxW, maxH uint16) csdef.VideoStreamStruct {
	off := false
	return csdef.VideoStreamStruct{
		VideoStreamId: id, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264,
		MinFrameRate: minFPS, MaxFrameRate: maxFPS,
		MinResolution: csdef.VideoResolutionStruct{Width: minW, Height: minH}, MaxResolution: csdef.VideoResolutionStruct{Width: maxW, Height: maxH},
		MinBitRate: camMinBitRate, MaxBitRate: camMaxBitRate, KeyFrameInterval: camKeyFrameInterval,
		WatermarkEnabled: &off, OsdEnabled: &off,
	}
}

// attach reads the kept state from st; call it before devices.
func (c *demoCamera) attach(ctx context.Context, st settings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st = st
	v, ok, err := st.GetSetting(ctx, cameraStateSetting)
	if err != nil || !ok {
		return
	}
	if err := json.Unmarshal([]byte(v), &c.restored); err != nil {
		c.logger.Warn("camerasim.load", slog.Any("err", err))
	}
}

// restore decodes a kept cluster state into its Patch.
func restore[P any](raw json.RawMessage) P {
	var p P
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// keep returns the named fields of a cluster's State as JSON.
func keep(state any, fields []string) json.RawMessage {
	data, err := json.Marshal(state)
	if err != nil {
		return nil
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(fields))
	for _, f := range fields {
		if v, ok := all[f]; ok {
			out[f] = v
		}
	}
	data, _ = json.Marshal(out)
	return data
}

// persist writes the camera's state.
func (c *demoCamera) persist() {
	c.mu.Lock()
	st := c.st
	c.mu.Unlock()
	if st == nil || c.camera == nil {
		return
	}
	state := cameraState{
		AvStream:   keep(c.camera.CameraAvStreamManagement().Get(), camPersistAvStream),
		AvSettings: keep(c.camera.CameraAvSettingsUserLevelManagement().Get(), camPersistAvSettings),
		Chime:      keep(c.chime.Chime().Get(), camPersistChime),
		Push:       keep(c.camera.PushAvStreamTransport().Get(), camPersistPush),
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := st.SetSetting(context.Background(), cameraStateSetting, string(data)); err != nil {
		c.logger.Warn("camerasim.persist", slog.Any("err", err))
	}
}

// devices builds the camera and the chime.
func (c *demoCamera) devices() ([]node.Endpoint, error) {
	if c.camera == nil {
		if err := c.build(); err != nil {
			return nil, err
		}
	}
	return []node.Endpoint{c.camera, c.chime}, nil
}

//nolint:funlen // one declaration per cluster of the camera
func (c *demoCamera) build() error {
	c.mu.Lock()
	restored := c.restored
	c.mu.Unlock()

	// The stream management (camera-app.cpp:185-283).
	avs := restore[csdef.Patch](restored.AvStream)
	maxHDR := cam30fps
	sensor := csdef.VideoSensorParamsStruct{SensorWidth: camMaxWidth, SensorHeight: camMaxHeight, MaxFps: camMaxVideoFrameRate, MaxHdrfps: &maxHDR}
	minViewport := csdef.VideoResolutionStruct{Width: camMinWidth, Height: camMinHeight}
	rdt := []csdef.RateDistortionTradeOffPointsStruct{{Codec: csdef.VideoCodecH264, Resolution: minViewport, MinBitRate: 10000}}
	audioCaps := csdef.AudioCapabilitiesStruct{
		MaxNumberOfChannels:  camMaxChannelCount,
		SupportedCodecs:      []csdef.AudioCodecEnum{csdef.AudioCodecOpus, csdef.AudioCodecAacLc},
		SupportedSampleRates: []uint32{48000, 32000}, SupportedBitDepths: []uint8{24, 32},
	}
	speakerCaps := audioCaps
	no, yes := false, true
	snapshotCaps := []csdef.SnapshotCapabilitiesStruct{
		{Resolution: minViewport, MaxFrameRate: camSnapshotFrameRate, ImageCodec: csdef.ImageCodecJpeg, RequiresEncodedPixels: false, RequiresHardwareEncoder: &no},
		{Resolution: csdef.VideoResolutionStruct{Width: cam720Width, Height: cam720Height}, MaxFrameRate: camSnapshotFrameRate, ImageCodec: csdef.ImageCodecJpeg, RequiresEncodedPixels: true, RequiresHardwareEncoder: &yes},
	}
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView, csdef.StreamUsageRecording}
	twoWay := csdef.TwoWayTalkSupportTypeFullDuplex
	maxEncoders, pixelRate, buffer, bandwidth := camMaxConcurrentEncoders, camMaxEncodedPixelRate, camMaxContentBufferSize, camMaxNetworkBandwidthBps
	frameRate := cam30fps
	infrared := false
	avs.MaxConcurrentEncoders, avs.MaxEncodedPixelRate, avs.VideoSensorParams = &maxEncoders, &pixelRate, &sensor
	avs.NightVisionUsesInfrared, avs.MinViewportResolution, avs.RateDistortionTradeOffPoints = &infrared, &minViewport, &rdt
	avs.MaxContentBufferSize, avs.MicrophoneCapabilities, avs.SpeakerCapabilities = &buffer, &audioCaps, &speakerCaps
	avs.TwoWayTalkSupport, avs.SnapshotCapabilities, avs.MaxNetworkBandwidth = &twoWay, &snapshotCaps, &bandwidth
	avs.CurrentFrameRate, avs.SupportedStreamUsages = &frameRate, &usages
	if avs.StreamUsagePriorities == nil {
		avs.StreamUsagePriorities = &usages
	}
	defaultBool := func(p **bool) {
		if *p == nil {
			*p = &no
		}
	}
	for _, p := range []**bool{
		&avs.HdrModeEnabled, &avs.SoftRecordingPrivacyModeEnabled, &avs.SoftLivestreamPrivacyModeEnabled,
		&avs.SpeakerMuted, &avs.MicrophoneMuted, &avs.MicrophoneAgcEnabled, &avs.LocalVideoRecordingEnabled,
		&avs.LocalSnapshotRecordingEnabled, &avs.StatusLightEnabled, &avs.ImageFlipHorizontal, &avs.ImageFlipVertical,
	} {
		defaultBool(p)
	}
	// The hard privacy switch is the camera's, not kept (camera-device.h:427).
	avs.HardPrivacyModeOn = &no
	if avs.NightVision == nil {
		off := csdef.TriStateAutoOff
		avs.NightVision, avs.NightVisionIllum = &off, &off
	}
	if avs.Viewport == nil {
		vp := csdef.ViewportStruct{X1: 0, Y1: 0, X2: camMaxWidth, Y2: camMaxHeight}
		avs.Viewport = &vp
	}
	speakerMin, speakerMax, micMin, micMax := camSpeakerMinLevel, camSpeakerMaxLevel, camMicrophoneMinLevel, camMicrophoneMaxLevel
	avs.SpeakerMinLevel, avs.SpeakerMaxLevel, avs.MicrophoneMinLevel, avs.MicrophoneMaxLevel = &speakerMin, &speakerMax, &micMin, &micMax
	if avs.SpeakerVolumeLevel == nil {
		avs.SpeakerVolumeLevel = &speakerMin
	}
	if avs.MicrophoneVolumeLevel == nil {
		avs.MicrophoneVolumeLevel = &micMin
	}
	if avs.ImageRotation == nil {
		rot := uint16(0)
		avs.ImageRotation = &rot
	}
	if avs.StatusLightBrightness == nil {
		medium := csdef.ThreeLevelAutoMedium
		avs.StatusLightBrightness = &medium
	}
	// A kept allocation is the available stream's again.
	if avs.AllocatedVideoStreams != nil {
		for _, s := range *avs.AllocatedVideoStreams {
			for i := range c.videos {
				if c.videos[i].params.VideoStreamId == s.VideoStreamId {
					c.videos[i].allocated = true
				}
			}
		}
	}
	if avs.AllocatedAudioStreams != nil {
		for _, s := range *avs.AllocatedAudioStreams {
			for i := range c.audios {
				if c.audios[i].params.AudioStreamId == s.AudioStreamId {
					c.audios[i].allocated = true
				}
			}
		}
	}
	if avs.AllocatedSnapshotStreams != nil {
		for _, s := range *avs.AllocatedSnapshotStreams {
			c.snapshots[s.SnapshotStreamId] = true
		}
	}
	avsFeatures := csdef.FeatureAudio | csdef.FeatureVideo | csdef.FeatureSnapshot | csdef.FeaturePrivacy | csdef.FeatureSpeaker |
		csdef.FeatureImageControl | csdef.FeatureWatermark | csdef.FeatureOnScreenDisplay | csdef.FeatureLocalStorage |
		csdef.FeatureHighDynamicRange | csdef.FeatureNightVision

	// The camera's pan, tilt and zoom (camera-app.cpp:43-64).
	avset := restore[avdef.Patch](restored.AvSettings)
	panMin, panMax, tiltMin, tiltMax, zoomMax, maxPresets := camPanMin, camPanMax, camTiltMin, camTiltMax, camZoomMax, camMaxPresets
	avset.PanMin, avset.PanMax, avset.TiltMin, avset.TiltMax, avset.ZoomMax, avset.MaxPresets = &panMin, &panMax, &tiltMin, &tiltMax, &zoomMax, &maxPresets
	avsetFeatures := avdef.FeatureDigitalPtz | avdef.FeatureMechanicalPan | avdef.FeatureMechanicalTilt | avdef.FeatureMechanicalZoom | avdef.FeatureMechanicalPresets

	// The zones (camera-app.cpp:65-77).
	var zm zmdef.Patch
	maxZones, maxUser, sensMax := camMaxZones, camMaxUserDefinedZones, camSensitivityMax
	twoDMax := zmdef.TwoDCartesianVertexStruct{X: camMaxWidth - 1, Y: camMaxHeight - 1}
	zm.MaxZones, zm.MaxUserDefinedZones, zm.SensitivityMax, zm.TwoDCartesianMax = &maxZones, &maxUser, &sensMax, &twoDMax
	// With PERZONESENS the sensitivity is each trigger's: the cluster
	// serves no Sensitivity ("!PERZONESENS"), and the camera-app's
	// SetSensitivity (camera-app.cpp:77) sets nothing a controller reads.
	zm.Sensitivity = nil
	zmFeatures := zmdef.FeatureTwoDimensionalCartesianZone | zmdef.FeaturePerZoneSensitivity | zmdef.FeatureUserDefined | zmdef.FeatureFocusZones

	// The push transports, kept as the camera-app keeps them
	// (push-av-stream-manager.cpp LoadCurrentConnections, :630-671).
	push := restore[padef.Patch](restored.Push)
	push.SupportedFormats = nil
	if push.CurrentConnections != nil {
		for _, conn := range *push.CurrentConnections {
			t := &camTransport{status: conn.TransportStatus}
			if conn.TransportOptions != nil {
				t.trigger = conn.TransportOptions.TriggerOptions
			}
			c.transports[conn.ConnectionId] = t
		}
	}

	cam, err := device.NewCamera(c.options("camera", "Camera"),
		csdef.Declare{
			Features: avsFeatures, Initial: avs,
			VideoStreamPort: camVideoPort{c}, AudioStreamPort: camAudioPort{c}, SnapshotStreamPort: camSnapshotPort{c}, SnapshotCapturePort: camCapturePort{c},
		},
		wpdef.Declare{ProviderPort: camProviderPort{c}},
		// camera-app.matter's PushAvStreamTransport, featureMap 3:
		// PERZONESENS and METADATA.
		padef.Declare{Features: padef.FeaturePerZoneSensitivity | padef.FeatureMetadata, Initial: push, PushTransportPort: camPushPort{c}},
		avdef.Declare{Features: avsetFeatures, Initial: avset, MechanicalPTZ: camPTZ{c}, DigitalPTZ: camPTZ{c}},
		zmdef.Declare{Features: zmFeatures, Initial: zm, ZonePort: camZonePort{c}},
	)
	if err != nil {
		return fmt.Errorf("camera: %w", err)
	}
	c.camera = cam

	// The chime (chime-manager.h:41-45; the chime server's defaults).
	ch := restore[chimedef.Patch](restored.Chime)
	ch.InstalledChimeSounds = nil // the ChimePort's
	chime, err := device.NewChime(c.options("chime", "Chime"), chimedef.Declare{Initial: ch, ChimePort: camChimePort{c}})
	if err != nil {
		return fmt.Errorf("chime: %w", err)
	}
	c.chime = chime

	persist := func() { c.persist() }
	cam.CameraAvStreamManagement().OnChangedAsync(func(csdef.State, contract.Change) { persist() })
	cam.CameraAvSettingsUserLevelManagement().OnChangedAsync(func(avdef.State, contract.Change) { persist() })
	cam.ZoneManagement().OnChangedAsync(func(zmdef.State, contract.Change) { persist() })
	cam.PushAvStreamTransport().OnChangedAsync(func(padef.State, contract.Change) { persist() })
	chime.Chime().OnChangedAsync(func(chimedef.State, contract.Change) { persist() })
	return nil
}

func (c *demoCamera) options(key, name string) device.Options {
	o := bridged(key, name)
	o.Surface = c.surface
	return o
}

// camChimeSounds are the camera-app's sounds (chime-manager.h:41-45).
func camChimeSounds() []chimedef.ChimeSoundStruct {
	return []chimedef.ChimeSoundStruct{
		{ChimeId: 0, Name: "Basic Door Chime"},
		{ChimeId: 1, Name: "Enhanced Door Chime"},
		{ChimeId: 2, Name: "Star Wars Door Chime"},
	}
}

// statusErr is a port refusal with the status the camera-app's manager
// answers.
func statusErr(status im.StatusCode, format string, args ...any) error {
	return spec.Errorf(status, "camera-sim: "+format, args...)
}

// --- CameraAvStreamManagement (camera-av-stream-manager.cpp) ---------------

type camVideoPort struct{ c *demoCamera }

// VideoStreamAllocate is the manager's VideoStreamAllocate (:347-411):
// DYNAMIC_CONSTRAINT_ERROR when no available stream is compatible, an
// allocated compatible stream reused, else an unused compatible one if
// the encoders allow it, RESOURCE_EXHAUSTED otherwise.
func (p camVideoPort) VideoStreamAllocate(_ context.Context, args csdef.VideoStreamStruct) (uint16, error) {
	c := p.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.ContainsFunc(c.videos, func(v camAvailableVideo) bool { return camVideoCompatible(v.params, args) }) {
		return 0, statusErr(im.StatusDynamicConstraintError, "no available video stream is compatible")
	}
	host := c.camera.CameraAvStreamManagement().Host()
	if id, ok := host.ReusableVideoStreamID(args); ok {
		return id, nil
	}
	for i := range c.videos {
		if c.videos[i].allocated || !camVideoCompatible(c.videos[i].params, args) {
			continue
		}
		rate := uint32(args.MaxFrameRate) * uint32(args.MaxResolution.Height) * uint32(args.MaxResolution.Width)
		if !host.ResourceAvailable(rate, true) {
			return 0, statusErr(im.StatusResourceExhausted, "no encoder for the stream")
		}
		c.videos[i].allocated = true
		vp := c.camera.CameraAvStreamManagement().Get().Viewport
		c.videos[i].viewport = avdef.ViewportStruct{X1: vp.X1, Y1: vp.Y1, X2: vp.X2, Y2: vp.Y2}
		return c.videos[i].params.VideoStreamId, nil
	}
	return 0, statusErr(im.StatusResourceExhausted, "no compatible video stream is free")
}

// VideoStreamAllocated hands a new stream to the DPTZ streams with the
// camera's viewport (camera-av-stream-manager.cpp:395-398, the AVSUM
// manager's VideoStreamAllocated).
func (p camVideoPort) VideoStreamAllocated(_ context.Context, stream csdef.VideoStreamStruct, action csdef.StreamAllocationAction) {
	if action != csdef.StreamNewAllocation {
		return
	}
	vp := p.c.camera.CameraAvStreamManagement().Get().Viewport
	p.c.camera.CameraAvSettingsUserLevelManagement().Host().AddMoveCapableVideoStream(stream.VideoStreamId, avdef.ViewportStruct{X1: vp.X1, Y1: vp.Y1, X2: vp.X2, Y2: vp.Y2})
}

// camVideoCompatible is VideoStream::IsCompatible
// (camera-device-interface.h:51-62).
func camVideoCompatible(s, in csdef.VideoStreamStruct) bool {
	return s.VideoCodec == in.VideoCodec && s.MinFrameRate <= in.MinFrameRate && s.MaxFrameRate >= in.MaxFrameRate &&
		s.MinResolution.Width <= in.MinResolution.Width && s.MinResolution.Height <= in.MinResolution.Height &&
		s.MaxResolution.Width >= in.MaxResolution.Width && s.MaxResolution.Height >= in.MaxResolution.Height &&
		s.MinBitRate <= in.MinBitRate && s.MaxBitRate >= in.MaxBitRate && in.KeyFrameInterval != 0
}

// VideoStreamModify is the manager's VideoStreamModify (:447-472).
func (p camVideoPort) VideoStreamModify(_ context.Context, id uint16, _, _ *bool) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	for _, v := range p.c.videos {
		if v.params.VideoStreamId == id && v.allocated {
			return nil
		}
	}
	return statusErr(im.StatusNotFound, "video stream %d", id)
}

// VideoStreamDeallocate is the manager's VideoStreamDeallocate (:474-494).
func (p camVideoPort) VideoStreamDeallocate(_ context.Context, id uint16) error {
	p.c.mu.Lock()
	found := false
	for i := range p.c.videos {
		if p.c.videos[i].params.VideoStreamId == id && p.c.videos[i].allocated {
			p.c.videos[i].allocated, found = false, true
		}
	}
	p.c.mu.Unlock()
	if !found {
		return statusErr(im.StatusNotFound, "video stream %d", id)
	}
	p.c.camera.CameraAvSettingsUserLevelManagement().Host().RemoveMoveCapableVideoStream(id)
	return nil
}

type camAudioPort struct{ c *demoCamera }

// AudioStreamAllocate is the manager's AudioStreamAllocate (:496-524):
// the first compatible available stream, allocated or not.
func (p camAudioPort) AudioStreamAllocate(_ context.Context, args csdef.AudioStreamStruct) (uint16, error) {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	for i := range p.c.audios {
		s := p.c.audios[i].params
		if s.AudioCodec == args.AudioCodec && s.ChannelCount >= args.ChannelCount && s.SampleRate >= args.SampleRate && s.BitDepth >= args.BitDepth {
			p.c.audios[i].allocated = true
			return s.AudioStreamId, nil
		}
	}
	return 0, statusErr(im.StatusDynamicConstraintError, "no available audio stream is compatible")
}

// AudioStreamDeallocate is the manager's AudioStreamDeallocate (:526-544).
func (p camAudioPort) AudioStreamDeallocate(_ context.Context, id uint16) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	for i := range p.c.audios {
		if p.c.audios[i].params.AudioStreamId == id && p.c.audios[i].allocated {
			p.c.audios[i].allocated = false
			return nil
		}
	}
	return statusErr(im.StatusNotFound, "audio stream %d", id)
}

type camSnapshotPort struct{ c *demoCamera }

// SnapshotStreamAllocate is the manager's SnapshotStreamAllocate
// (:546-625): DYNAMIC_CONSTRAINT_ERROR unless the camera's stream is
// compatible, an allocated compatible stream reused, the encoders
// checked, then a new stream with the next id (AddSnapshotStream).
func (p camSnapshotPort) SnapshotStreamAllocate(_ context.Context, args csdef.SnapshotStreamStruct) (uint16, error) {
	c := p.c
	// InitializeSnapshotStreams' stream: JPEG, 30 fps, 640x480 to 1080p.
	compatible := args.ImageCodec == csdef.ImageCodecJpeg && camSnapshotFrameRate <= args.FrameRate &&
		camMinWidth <= args.MinResolution.Width && camMinHeight <= args.MinResolution.Height &&
		camMaxWidth >= args.MaxResolution.Width && camMaxHeight >= args.MaxResolution.Height
	if !compatible {
		return 0, statusErr(im.StatusDynamicConstraintError, "the snapshot stream is not compatible")
	}
	host := c.camera.CameraAvStreamManagement().Host()
	if id, ok := host.ReusableSnapshotStreamID(args); ok {
		return id, nil
	}
	rate, encoder := uint32(0), false
	if args.EncodedPixels {
		rate = uint32(args.FrameRate) * uint32(args.MaxResolution.Height) * uint32(args.MaxResolution.Width)
		encoder = args.HardwareEncoder
	}
	if !host.ResourceAvailable(rate, encoder) {
		return 0, statusErr(im.StatusResourceExhausted, "no encoder for the snapshot stream")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	next := uint16(0)
	for id := range c.snapshots {
		next = max(next, id)
	}
	next++
	c.snapshots[next] = true
	return next, nil
}

// SnapshotStreamModify is the manager's SnapshotStreamModify (:627-652).
func (p camSnapshotPort) SnapshotStreamModify(_ context.Context, id uint16, _, _ *bool) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	if !p.c.snapshots[id] {
		return statusErr(im.StatusNotFound, "snapshot stream %d", id)
	}
	return nil
}

// SnapshotStreamDeallocate is the manager's SnapshotStreamDeallocate
// (:654-677); the reference count is the server's check.
func (p camSnapshotPort) SnapshotStreamDeallocate(_ context.Context, id uint16) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	if !p.c.snapshots[id] {
		return statusErr(im.StatusNotFound, "snapshot stream %d", id)
	}
	p.c.snapshots[id] = false
	return nil
}

type camCapturePort struct{ c *demoCamera }

// CaptureSnapshot returns a generated JPEG at the requested resolution,
// bounded by the camera's sensor, with --camera-test-videosrc; without it
// the camera-app has no video device to capture from and fails.
func (p camCapturePort) CaptureSnapshot(_ context.Context, _ *uint16, res csdef.VideoResolutionStruct) (csdef.CaptureSnapshotResponse, error) {
	if !p.c.opts.testVideoSource {
		return csdef.CaptureSnapshotResponse{}, statusErr(im.StatusFailure, "no video source (the camera-app captures from /dev/video0 without --camera-test-videosrc)")
	}
	w, h := min(max(res.Width, 1), camMaxWidth), min(max(res.Height, 1), camMaxHeight)
	img := image.NewGray(image.Rect(0, 0, int(w), int(h)))
	for y := range int(h) {
		for x := range int(w) {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) % 256)}) //nolint:gosec // a test pattern
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 50}); err != nil {
		return csdef.CaptureSnapshotResponse{}, err
	}
	return csdef.CaptureSnapshotResponse{Data: buf.Bytes(), ImageCodec: csdef.ImageCodecJpeg, Resolution: csdef.VideoResolutionStruct{Width: w, Height: h}}, nil
}

// --- WebRtcTransportProvider (webrtc-provider-manager.cpp) -----------------

type camProviderPort struct{ c *demoCamera }

// camSession is HandleSolicitOffer's and HandleProvideOffer's session:
// the request's peer and streams, the first of each list as the
// former single id.
func camSession(args wpdef.OfferArgs) wpdef.WebRTCSessionStruct {
	s := wpdef.WebRTCSessionStruct{
		Id: args.SessionID, PeerNodeId: args.PeerNodeID, PeerEndpointId: args.OriginatingEndpointID,
		StreamUsage: args.StreamUsage, FabricIndex: args.FabricIndex,
		VideoStreamId: camFirst(args.VideoStreams), AudioStreamId: camFirst(args.AudioStreams),
	}
	if args.MetadataEnabled != nil {
		s.MetadataEnabled = *args.MetadataEnabled
	}
	if args.VideoStreams != nil && len(*args.VideoStreams) > 0 {
		v := slices.Clone(*args.VideoStreams)
		s.VideoStreams = &v
	}
	if args.AudioStreams != nil && len(*args.AudioStreams) > 0 {
		a := slices.Clone(*args.AudioStreams)
		s.AudioStreams = &a
	}
	return s
}

func camFirst(ids *[]uint16) *spec.Nullable[uint16] {
	n := spec.NullOf[uint16]()
	if ids != nil && len(*ids) > 0 {
		n = spec.Nullable[uint16]{Value: (*ids)[0]}
	}
	return &n
}

// camMaxWebRTCSessions is the provider manager's capacity
// (webrtc-provider-manager.cpp:39 kMaxConcurrentWebRTCSessions).
const camMaxWebRTCSessions = 5

// camEndOutOfResources is WebRTCEndReasonEnum OutOfResources (matter.js
// web-rtc-transport-definitions.element.ts:26 at 862ad75f).
const camEndOutOfResources = 8

// SolicitOffer answers DeferredOffer as --camera-deferred-offer says
// (HandleSolicitOffer: LinuxDeviceOptions cameraDeferredOffer). Over the
// capacity the session is still made and then ended OutOfResources
// (:279-292).
func (p camProviderPort) SolicitOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	if p.c.addSession(args.SessionID) > camMaxWebRTCSessions {
		go p.c.endSession(args.SessionID, camEndOutOfResources) //nolint:gosec,contextcheck // the End outlives the SolicitOffer request; the camera-app schedules it (ScheduleEndSend)
	}
	return camSession(args), p.c.opts.deferredOffer, nil
}

// ProvideOffer keeps the session, RESOURCE_EXHAUSTED over the capacity
// (:470-477); the answer would come from a media stack this simulation
// does not have.
func (p camProviderPort) ProvideOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	if p.c.addSession(args.SessionID) > camMaxWebRTCSessions {
		p.c.dropSession(args.SessionID)
		return wpdef.WebRTCSessionStruct{}, statusErr(im.StatusResourceExhausted, "%d WebRTC sessions", camMaxWebRTCSessions)
	}
	return camSession(args), nil
}

func (camProviderPort) ProvideAnswer(context.Context, uint16, string) error { return nil }

func (camProviderPort) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}

func (p camProviderPort) EndSession(_ context.Context, id uint16, _ uint8) error {
	p.c.dropSession(id)
	return nil
}

// addSession records a session and returns how many the manager holds.
func (c *demoCamera) addSession(id uint16) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[id] = true
	return len(c.sessions)
}

func (c *demoCamera) dropSession(id uint16) {
	c.mu.Lock()
	delete(c.sessions, id)
	c.mu.Unlock()
}

// endSession ends a session from the camera's side (the provider's End to
// the requestor).
func (c *demoCamera) endSession(id uint16, reason uint8) {
	c.dropSession(id)
	if p, ok := c.camera.WebRtcTransportProvider().Underlying().(*camera.Provider); ok {
		if err := p.End(context.Background(), id, reason); err != nil {
			c.logger.Info("camerasim.end", slog.Uint64("session", uint64(id)), slog.Any("err", err))
		}
	}
}

// --- PushAvStreamTransport (push-av-stream-manager.cpp) --------------------

type camPushPort struct{ c *demoCamera }

// The push transports follow the camera-app's PushAvStreamTransportManager
// and PushAVTransport (push-av-stream-manager.cpp:187-315,
// pushav-transport.cpp:409-427, :520-627) without a recorder or uploader:
// a continuous transport begins when set Active and ends when set
// Inactive; a triggered one — the ManuallyTriggerTransport command, or a
// ZoneTriggered motion for a motion transport — begins on an Active
// transport and ends after its InitialDuration. Begin and end are the
// server's PushTransportBegin and PushTransportEnd events
// (NotifyTransportStarted, NotifyTransportStopped); each begin is a new
// CMAF session.

func (p camPushPort) AllocatePushTransport(_ context.Context, id uint16, _ uint8, options padef.TransportOptionsStruct, _ padef.TLSCredentials) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	p.c.transports[id] = &camTransport{trigger: options.TriggerOptions, status: padef.TransportStatusInactive}
	return nil
}

func (p camPushPort) DeallocatePushTransport(_ context.Context, id uint16) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	if t, ok := p.c.transports[id]; ok && t.stop != nil {
		t.stop.Stop()
	}
	delete(p.c.transports, id)
	return nil
}

func (p camPushPort) ModifyPushTransport(_ context.Context, id uint16, options padef.TransportOptionsStruct) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	if t, ok := p.c.transports[id]; ok {
		t.trigger = options.TriggerOptions
	}
	return nil
}

// SetTransportStatus is the manager's SetTransportStatus (:239-285): no
// Active while a privacy mode is on (INVALID_IN_STATE).
func (p camPushPort) SetTransportStatus(_ context.Context, ids []uint16, status padef.TransportStatusEnum) error {
	if status == padef.TransportStatusActive {
		if av, ok := p.c.camera.CameraAvStreamManagement().Underlying().(*camera.AvStream); ok {
			if hard, rec, live := av.PrivacyModes(); hard || rec || live {
				return statusErr(im.StatusInvalidInState, "a privacy mode is on")
			}
		}
	}
	for _, id := range ids {
		p.c.mu.Lock()
		t, ok := p.c.transports[id]
		if !ok || t.status == status {
			p.c.mu.Unlock()
			continue
		}
		t.status = status
		continuous := t.trigger.TriggerType == padef.TransportTriggerTypeContinuous
		// A motion within its detected duration before the transport
		// went Active starts the recording for the rest of it
		// (pushav-transport.cpp:616-638).
		var rest time.Duration
		if t.trigger.TriggerType == padef.TransportTriggerTypeMotion && !t.activated.IsZero() {
			rest = t.detected - clock.Since(p.c.clk, t.activated)
		}
		p.c.mu.Unlock()
		switch {
		case status == padef.TransportStatusActive && continuous:
			p.c.beginTransport(id, nil, 0)
		case status == padef.TransportStatusActive && rest > 0:
			automation := padef.TriggerActivationReasonAutomation
			p.c.beginTransport(id, &automation, rest)
		case status == padef.TransportStatusInactive:
			p.c.endTransport(id)
		}
	}
	return nil
}

func (p camPushPort) ManuallyTriggerTransport(_ context.Context, id uint16, reason padef.TriggerActivationReasonEnum, tc *padef.TransportMotionTriggerTimeControlStruct) error {
	p.c.mu.Lock()
	t, ok := p.c.transports[id]
	if !ok {
		p.c.mu.Unlock()
		return statusErr(im.StatusNotFound, "connection %d", id)
	}
	active := t.status == padef.TransportStatusActive
	d, maxClip := time.Second, time.Second
	if tc != nil {
		d = time.Duration(max(tc.InitialDuration, 1)) * time.Second
		maxClip = max(time.Duration(tc.MaxDuration)*time.Second, d)
	}
	if active {
		// A manual activation (TriggerTransport with the manual flag,
		// pushav-transport.cpp:345-405).
		t.activated, t.detected, t.manual, t.maxClip = p.c.clk.Now(), d, true, maxClip
	}
	p.c.mu.Unlock()
	if active {
		p.c.beginTransport(id, &reason, d)
	}
	return nil
}

// beginTransport starts a transport's recording: a new CMAF session and
// PushTransportBegin; a triggered one ends after d.
func (c *demoCamera) beginTransport(id uint16, reason *padef.TriggerActivationReasonEnum, d time.Duration) {
	c.mu.Lock()
	t, ok := c.transports[id]
	if !ok {
		c.mu.Unlock()
		return
	}
	trigger := t.trigger.TriggerType
	already := t.streaming
	if !already {
		t.streaming = true
		t.session++
	}
	if d > 0 {
		if t.stop != nil {
			t.stop.Stop()
		}
		t.stop = c.clk.AfterFunc(d, func() { c.endTransport(id) })
	}
	c.mu.Unlock()
	if already {
		return
	}
	go func() {
		if err := c.pushTransport().TransportStarted(id, trigger, reason); err != nil {
			c.logger.Info("camerasim.pushbegin", slog.Uint64("connection", uint64(id)), slog.Any("err", err))
		}
	}()
}

// endTransport ends a transport's recording: PushTransportEnd.
func (c *demoCamera) endTransport(id uint16) {
	c.mu.Lock()
	t, ok := c.transports[id]
	if !ok || !t.streaming {
		c.mu.Unlock()
		return
	}
	t.streaming = false
	if t.stop != nil {
		t.stop.Stop()
		t.stop = nil
	}
	c.mu.Unlock()
	go func() {
		if err := c.pushTransport().TransportStopped(id); err != nil {
			c.logger.Info("camerasim.pushend", slog.Uint64("connection", uint64(id)), slog.Any("err", err))
		}
	}()
}

// pushTransport is the camera's push transport server's host side.
func (c *demoCamera) pushTransport() padef.Host {
	return c.camera.PushAvStreamTransport().Host()
}

// motionTriggered is the manager's HandleZoneTrigger (:673-694): every
// Active motion transport begins with the reason Automation and ends after
// its InitialDuration.
func (c *demoCamera) motionTriggered() {
	automation := padef.TriggerActivationReasonAutomation
	now := c.clk.Now()
	c.mu.Lock()
	var ids []uint16
	var durations []time.Duration
	for id, t := range c.transports {
		if t.trigger.TriggerType != padef.TransportTriggerTypeMotion {
			continue
		}
		var tc padef.TransportMotionTriggerTimeControlStruct
		if t.trigger.MotionTimeControl != nil {
			tc = *t.trigger.MotionTimeControl
		}
		sec := func(v uint32) time.Duration { return time.Duration(v) * time.Second }
		// HandleTriggerDetected (pushav-transport.cpp:345-405), whether
		// or not the transport is Active: nothing in the blind period
		// after a recording; a new activation once the detected
		// duration is over, else the recording augmented up to the
		// maximum.
		if !t.manual && !t.activated.IsZero() && now.Before(t.activated.Add(t.detected).Add(sec(uint32(tc.BlindDuration)))) && now.After(t.activated.Add(t.detected)) {
			continue
		}
		if t.activated.IsZero() || now.Sub(t.activated) >= t.detected {
			t.activated, t.detected, t.manual = now, sec(uint32(max(tc.InitialDuration, 1))), false
			t.maxClip = sec(max(tc.MaxDuration, uint32(tc.InitialDuration)))
		} else {
			t.detected = min(t.detected+sec(uint32(tc.AugmentationDuration)), max(t.maxClip, t.detected))
		}
		if t.status == padef.TransportStatusActive {
			ids, durations = append(ids, id), append(durations, t.activated.Add(t.detected).Sub(now))
		}
	}
	c.mu.Unlock()
	for i, id := range ids {
		c.beginTransport(id, &automation, durations[i])
	}
}

// TransportBusy is GetTransportBusyStatus: nothing uploads here.
func (camPushPort) TransportBusy(uint16) bool { return false }

func (camPushPort) ValidateBandwidthLimit(context.Context, padef.StreamUsageEnum, *uint16, *uint16) error {
	return nil
}

func (p camPushPort) CMAFSessionNumber(id uint16) (uint64, bool) {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	t, ok := p.c.transports[id]
	if !ok {
		return 0, false
	}
	return t.session, true
}

// --- CameraAvSettingsUserLevelManagement -----------------------------------

// camPTZ is the camera-app's PTZ manager: a mechanical move completes two
// seconds after it starts, as the camera-app's timer mimics the physical
// movement (camera-avsettingsuserlevel-manager.cpp:75, :93, :111,
// onTimerExpiry :300-306); a preset save or removal and a digital move
// take effect at once.
type camPTZ struct{ c *demoCamera }

// camMoveDuration is the camera-app's simulated movement.
const camMoveDuration = 2 * time.Second

func (p camPTZ) moved() error {
	p.c.clk.AfterFunc(camMoveDuration, func() {
		p.c.camera.CameraAvSettingsUserLevelManagement().Host().PhysicalMovementComplete(nil)
	})
	return nil
}

func (p camPTZ) MptzSetPosition(context.Context, avdef.MPTZStruct) error { return p.moved() }

func (p camPTZ) MptzRelativeMove(context.Context, avdef.MPTZStruct) error { return p.moved() }

func (p camPTZ) MptzMoveToPreset(context.Context, uint8, avdef.MPTZStruct) error { return p.moved() }
func (camPTZ) MptzSavePreset(context.Context, uint8) error                       { return nil }
func (camPTZ) MptzRemovePreset(context.Context, uint8) error                     { return nil }

// DptzSetViewport sets an allocated stream's viewport: no smaller than
// the minimum viewport, no larger than the sensor, at the stream's aspect
// ratio to two places, CONSTRAINT_ERROR otherwise
// (camera-avsettingsuserlevel-manager.cpp:131-175).
func (p camPTZ) DptzSetViewport(_ context.Context, id uint16, vp avdef.ViewportStruct) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	ratio := func(w, h uint16) float32 { return float32(math.Floor(float64(float32(w)/float32(h)*100))) / 100 }
	for i := range p.c.videos {
		v := &p.c.videos[i]
		if v.params.VideoStreamId == id && v.allocated {
			w, h := vp.X2-vp.X1, vp.Y2-vp.Y1
			if w < camMinWidth || h < camMinHeight || w > camMaxWidth || h > camMaxHeight {
				return statusErr(im.StatusConstraintError, "viewport %dx%d", w, h)
			}
			if ratio(w, h) != ratio(v.params.MaxResolution.Width, v.params.MaxResolution.Height) {
				return statusErr(im.StatusConstraintError, "viewport %dx%d has not the stream's aspect ratio", w, h)
			}
			v.viewport = vp
			return nil
		}
	}
	return statusErr(im.StatusNotFound, "video stream %d", id)
}

// DptzRelativeMove is the camera-app's DPTZRelativeMove
// (camera-avsettingsuserlevel-manager.cpp:182-290): the deltas clamped at
// the sensor's left and top edges, the zoom scaling the area by the
// percentage at the viewport's aspect ratio, then no smaller than the
// minimum viewport and no larger than the sensor — in its uint16
// arithmetic.
func (p camPTZ) DptzRelativeMove(_ context.Context, req avdef.DptzRelativeMoveRequest) (avdef.ViewportStruct, error) {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	for i := range p.c.videos {
		v := &p.c.videos[i]
		if v.params.VideoStreamId != req.VideoStreamId || !v.allocated {
			continue
		}
		vp := v.viewport
		move := func(lo, hi *uint16, d int16) {
			if d == 0 {
				return
			}
			m := func(edge uint16) uint16 {
				if d < 0 && int(-d) > int(edge) {
					return 0
				}
				return uint16(int(edge) + int(d)) //nolint:gosec // chip's uint16 arithmetic
			}
			*lo, *hi = m(*lo), m(*hi)
		}
		if req.DeltaX != nil {
			move(&vp.X1, &vp.X2, *req.DeltaX)
		}
		if req.DeltaY != nil {
			move(&vp.Y1, &vp.Y2, *req.DeltaY)
		}
		if req.ZoomDelta != nil && *req.ZoomDelta != 0 {
			z := int(*req.ZoomDelta)
			w, h := vp.X2-vp.X1, vp.Y2-vp.Y1
			size := uint32(w) * uint32(h)
			delta := size * uint32(max(z, -z)) / 100 //nolint:gosec // |z| ≤ 100
			newSize := size + delta
			if z < 0 {
				newSize = size - delta
			}
			if minSize := uint32(camMinWidth) * uint32(camMinHeight); newSize < minSize {
				newSize = minSize
			}
			ar := math.Floor(float64(float32(w)/float32(h)*100)) / 100
			nw := uint16(math.Round(math.Sqrt(float64(newSize) * ar)))
			nh := uint16(float64(nw) / ar)
			vp.X1, vp.X2 = vp.X1-(nw-w), vp.X2+(nw-w)
			vp.Y1, vp.Y2 = vp.Y1-(nh-h), vp.Y2+(nh-h)
		}
		// The bounds compare in int, as C++ promotes the uint16 operands.
		if int(vp.X2)-int(vp.X1) < int(camMinWidth) || int(vp.Y2)-int(vp.Y1) < int(camMinHeight) {
			vp.X2, vp.Y2 = vp.X1+camMinWidth, vp.Y1+camMinHeight
		}
		if int(vp.X2)-int(vp.X1) > int(camMaxWidth) || int(vp.Y2)-int(vp.Y1) > int(camMaxHeight) {
			vp = avdef.ViewportStruct{X1: 0, Y1: 0, X2: camMaxWidth - 1, Y2: camMaxHeight - 1}
		}
		v.viewport = vp
		return vp, nil
	}
	return avdef.ViewportStruct{}, statusErr(im.StatusNotFound, "video stream %d", req.VideoStreamId)
}

// --- ZoneManagement (zone-manager.cpp) -------------------------------------

type camZonePort struct{ c *demoCamera }

// CreateTwoDCartesianZone gives the lowest free id from 1 (zone-manager.h
// GetNewZoneId).
func (p camZonePort) CreateTwoDCartesianZone(context.Context, zmdef.TwoDCartesianZoneStruct) (uint16, error) {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	id := uint16(1)
	for slices.Contains(p.c.zones, id) {
		id++
	}
	p.c.zones = append(p.c.zones, id)
	return id, nil
}

func (p camZonePort) UpdateTwoDCartesianZone(_ context.Context, id uint16, _ zmdef.TwoDCartesianZoneStruct) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	if !slices.Contains(p.c.zones, id) {
		return statusErr(im.StatusNotFound, "zone %d", id)
	}
	return nil
}

func (p camZonePort) RemoveZone(_ context.Context, id uint16) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	p.c.zones = slices.DeleteFunc(p.c.zones, func(z uint16) bool { return z == id })
	return nil
}

func (camZonePort) CreateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error { return nil }

func (camZonePort) UpdateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error { return nil }

func (p camZonePort) RemoveTrigger(_ context.Context, id uint16) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	p.c.triggers = slices.DeleteFunc(p.c.triggers, func(t *camTriggerContext) bool { return t.trigger.ZoneId == id })
	return nil
}

// zoneTriggered is the app pipe's ZoneTriggered: the zone manager's
// OnZoneTriggeredEvent for each zone (zone-manager.cpp:230-289) and the
// push transports' motion trigger (HandleZoneTrigger).
func (c *demoCamera) zoneTriggered(ids []uint16) error {
	zones := c.camera.ZoneManagement().Host()
	var errs []error
	for _, id := range ids {
		trigger, ok := c.triggerFor(id)
		if !ok {
			errs = append(errs, fmt.Errorf("zone %d has no trigger", id))
			continue
		}
		c.mu.Lock()
		i := slices.IndexFunc(c.triggers, func(t *camTriggerContext) bool { return t.trigger.ZoneId == id })
		if i < 0 {
			c.triggers = append(c.triggers, &camTriggerContext{
				trigger: trigger, detectedDuration: trigger.InitialDuration, prevDetected: trigger.InitialDuration,
				remainingBlind: trigger.BlindDuration, count: 1,
			})
			if c.timer == nil {
				c.tickBase, c.ticks = c.clk.Now(), 1
				c.timer = c.clk.AfterFunc(camZoneTimerPeriod, c.zoneTick)
			}
			c.mu.Unlock()
			errs = append(errs, zones.ZoneTriggered(id, zmdef.ZoneEventTriggeredReasonMotion))
			continue
		}
		t := c.triggers[i]
		if !t.blind {
			t.count++
			if t.count <= 2 || t.sinceInitial > t.prevDetected {
				t.prevDetected = t.detectedDuration
				t.detectedDuration += t.trigger.AugmentationDuration
			}
		}
		c.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (c *demoCamera) triggerFor(zoneID uint16) (zmdef.ZoneTriggerControlStruct, bool) {
	for _, t := range c.camera.ZoneManagement().Get().Triggers {
		if t.ZoneId == zoneID {
			return t, true
		}
	}
	return zmdef.ZoneTriggerControlStruct{}, false
}

// zoneTick is OnZoneTriggerTimeout (zone-manager.cpp:169-228).
func (c *demoCamera) zoneTick() {
	type stopped struct {
		zone   uint16
		reason zmdef.ZoneEventStoppedReasonEnum
	}
	var stops []stopped
	c.mu.Lock()
	period := uint32(camZoneTimerPeriod / time.Second)
	kept := c.triggers[:0]
	for _, t := range c.triggers {
		if t.blind {
			t.remainingBlind--
			if t.remainingBlind > 0 {
				kept = append(kept, t)
			}
			continue
		}
		t.sinceInitial += period
		switch {
		case t.sinceInitial >= t.detectedDuration:
			// The camera-app stops a second later (zone-manager.cpp:197,
			// `>`), at the very end of what TC-ZONEMGMT-2.4 step 3b
			// allows (InitialDuration + 1 s from the ZoneTriggered
			// report, TC_ZONEMGMT_2_4.py:279-285); the zone stays
			// triggered for TriggerDetectedDuration here.
			stops = append(stops, stopped{t.trigger.ZoneId, zmdef.ZoneEventStoppedReasonActionStopped})
			t.blind = true
		case t.sinceInitial > t.trigger.MaxDuration:
			stops = append(stops, stopped{t.trigger.ZoneId, zmdef.ZoneEventStoppedReasonTimeout})
			t.blind = true
		}
		kept = append(kept, t)
	}
	c.triggers = kept
	if len(c.triggers) > 0 {
		c.ticks++
		c.timer = c.clk.AfterFunc(clock.Until(c.clk, c.tickBase.Add(time.Duration(c.ticks)*camZoneTimerPeriod)), c.zoneTick)
	} else {
		c.timer = nil
	}
	c.mu.Unlock()
	for _, s := range stops {
		if err := c.camera.ZoneManagement().Host().ZoneStopped(s.zone, s.reason); err != nil {
			c.logger.Warn("camerasim.zonestopped", slog.Any("err", err))
		}
	}
}

// --- Chime (chime-manager.cpp) ---------------------------------------------

type camChimePort struct{ c *demoCamera }

func (camChimePort) InstalledChimeSounds() ([]chimedef.ChimeSoundStruct, error) {
	return camChimeSounds(), nil
}

// PlayChimeSound fails while the chime is disabled (chime-manager.cpp:52-63).
func (p camChimePort) PlayChimeSound(context.Context, uint8) error {
	if !p.c.chime.Chime().Get().Enabled {
		return statusErr(im.StatusFailure, "the chime is disabled")
	}
	return nil
}

// --- the app pipe ------------------------------------------------------------

// cameraPipeCommand is a camera-app app-pipe command
// (CameraAppCommandDelegate.cpp).
type cameraPipeCommand struct {
	Name   string          `json:"Name"`
	Value  *bool           `json:"Value"`
	ZoneID json.RawMessage `json:"ZoneId"`
}

// isCameraPipeCommand names the commands cameraPipeCommand decodes.
func isCameraPipeCommand(name string) bool {
	switch name {
	case "SetHardPrivacyModeOn", "ZoneTriggered", "AmbientContextTriggered",
		"AvAnalysisSessionStart", "AvAnalysisPerceivedContext", "AvAnalysisSessionEnd":
		return true
	}
	return false
}

// apply carries out a camera-app command.
func (c *demoCamera) apply(cmd cameraPipeCommand) error {
	switch cmd.Name {
	case "SetHardPrivacyModeOn":
		if cmd.Value == nil {
			return errors.New("SetHardPrivacyModeOn needs Value")
		}
		on := *cmd.Value
		return c.camera.CameraAvStreamManagement().Set(csdef.Patch{HardPrivacyModeOn: &on})
	case "ZoneTriggered":
		var ids []uint16
		var one uint16
		if err := json.Unmarshal(cmd.ZoneID, &ids); err != nil {
			if err := json.Unmarshal(cmd.ZoneID, &one); err != nil {
				return fmt.Errorf("ZoneTriggered: ZoneId: %w", err)
			}
			ids = []uint16{one}
		}
		err := c.zoneTriggered(ids)
		c.motionTriggered()
		return err
	default:
		// AmbientContextTriggered and the AvAnalysis commands drive the
		// camera-app's AvAnalysis cluster, which is not served here.
		c.logger.Info("camerasim.ignored", slog.String("name", cmd.Name), slog.String("why", "AvAnalysis is not in the matter.js snapshot; no cluster serves it"))
		return nil
	}
}
