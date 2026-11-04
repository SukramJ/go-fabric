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
	"slices"
	"sync"
	"time"

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
// position, presets and DPTZ streams, the chime's
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
	timer    *time.Timer
	// tickBase and ticks place the trigger timer's ticks on the second
	// grid from the first trigger (zone-manager.cpp's periodic one-second
	// timer), so they do not drift by the work each tick does.
	tickBase time.Time
	ticks    int
	// transports are the push transports and their CMAF session numbers.
	transports map[uint16]*camTransport
}

type camAvailableVideo struct {
	params    csdef.VideoStreamStruct
	allocated bool
}

type camAvailableAudio struct {
	params    csdef.AudioStreamStruct
	allocated bool
}

type camTransport struct {
	options csdef.StreamUsageEnum
	session uint64
	status  padef.TransportStatusEnum
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
)

// newDemoCamera returns the camera simulation; devices builds the
// endpoints once attach has restored the state.
func newDemoCamera(policies surface.Policies, opts cameraOptions, logger *slog.Logger) *demoCamera {
	c := &demoCamera{logger: logger, surface: policies, opts: opts, snapshots: map[uint16]bool{}, transports: map[uint16]*camTransport{}}
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

	cam, err := device.NewCamera(c.options("camera", "Camera"),
		csdef.Declare{
			Features: avsFeatures, Initial: avs,
			VideoStreamPort: camVideoPort{c}, AudioStreamPort: camAudioPort{c}, SnapshotStreamPort: camSnapshotPort{c}, SnapshotCapturePort: camCapturePort{c},
		},
		wpdef.Declare{ProviderPort: camProviderPort{c}},
		padef.Declare{PushTransportPort: camPushPort{c}},
		avdef.Declare{Features: avsetFeatures, Initial: avset, MechanicalPTZ: camPTZ{}, DigitalPTZ: camPTZ{}},
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
		if !c.videos[i].allocated && camVideoCompatible(c.videos[i].params, args) {
			rate := uint32(args.MaxFrameRate) * uint32(args.MaxResolution.Height) * uint32(args.MaxResolution.Width)
			if !host.ResourceAvailable(rate, true) {
				return 0, statusErr(im.StatusResourceExhausted, "no encoder for the stream")
			}
			c.videos[i].allocated = true
			return c.videos[i].params.VideoStreamId, nil
		}
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
// deprecated single id.
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

// SolicitOffer answers DeferredOffer as --camera-deferred-offer says
// (HandleSolicitOffer: LinuxDeviceOptions cameraDeferredOffer).
func (p camProviderPort) SolicitOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return camSession(args), p.c.opts.deferredOffer, nil
}

// ProvideOffer keeps the session; the answer would come from a media
// stack this simulation does not have.
func (p camProviderPort) ProvideOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	return camSession(args), nil
}

func (camProviderPort) ProvideAnswer(context.Context, uint16, string) error { return nil }

func (camProviderPort) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	return nil
}

func (camProviderPort) EndSession(context.Context, uint16, uint8) error { return nil }

// --- PushAvStreamTransport (push-av-stream-manager.cpp) --------------------

type camPushPort struct{ c *demoCamera }

func (p camPushPort) AllocatePushTransport(_ context.Context, id uint16, _ uint8, options padef.TransportOptionsStruct, _ padef.TLSCredentials) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	p.c.transports[id] = &camTransport{options: csdef.StreamUsageEnum(options.StreamUsage), status: padef.TransportStatusInactive}
	return nil
}

func (p camPushPort) DeallocatePushTransport(_ context.Context, id uint16) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	delete(p.c.transports, id)
	return nil
}

func (camPushPort) ModifyPushTransport(context.Context, uint16, padef.TransportOptionsStruct) error {
	return nil
}

func (p camPushPort) SetTransportStatus(_ context.Context, ids []uint16, status padef.TransportStatusEnum) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	for _, id := range ids {
		if t, ok := p.c.transports[id]; ok {
			t.status = status
		}
	}
	return nil
}

func (p camPushPort) ManuallyTriggerTransport(_ context.Context, id uint16, _ padef.TriggerActivationReasonEnum, _ *padef.TransportMotionTriggerTimeControlStruct) error {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	if t, ok := p.c.transports[id]; ok {
		t.session++
	}
	return nil
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

// camPTZ moves at once (camera-avsettingsuserlevel-manager.cpp: the
// camera-app's HAL sets the values and reports success).
type camPTZ struct{}

func (camPTZ) MptzSetPosition(context.Context, avdef.MPTZStruct) error             { return nil }
func (camPTZ) MptzRelativeMove(context.Context, avdef.MPTZStruct) error            { return nil }
func (camPTZ) MptzMoveToPreset(context.Context, uint8, avdef.MPTZStruct) error     { return nil }
func (camPTZ) MptzSavePreset(context.Context, uint8) error                         { return nil }
func (camPTZ) MptzRemovePreset(context.Context, uint8) error                       { return nil }
func (camPTZ) DptzSetViewport(context.Context, uint16, avdef.ViewportStruct) error { return nil }
func (camPTZ) DptzRelativeMove(_ context.Context, _ avdef.DptzRelativeMoveRequest) (avdef.ViewportStruct, error) {
	return avdef.ViewportStruct{}, errors.ErrUnsupported
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
				c.tickBase, c.ticks = time.Now(), 1
				c.timer = time.AfterFunc(camZoneTimerPeriod, c.zoneTick)
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
		case t.sinceInitial > t.detectedDuration:
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
		c.timer = time.AfterFunc(time.Until(c.tickBase.Add(time.Duration(c.ticks)*camZoneTimerPeriod)), c.zoneTick)
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
		return c.zoneTriggered(ids)
	default:
		// AmbientContextTriggered and the AvAnalysis commands drive the
		// camera-app's AvAnalysis cluster, which is not served here.
		c.logger.Info("camerasim.ignored", slog.String("name", cmd.Name), slog.String("why", "AvAnalysis is not in the matter.js snapshot; no cluster serves it"))
		return nil
	}
}
