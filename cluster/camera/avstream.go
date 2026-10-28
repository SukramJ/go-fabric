// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// AvStreamClusterID is the CameraAvStreamManagement cluster id.
const AvStreamClusterID = csdef.ClusterID

// Generated types the stream management speaks.
type (
	// AvStreamFeature is a FeatureMap bit of CameraAvStreamManagement.
	AvStreamFeature = csdef.Feature
	// VideoStream is an allocated video stream.
	VideoStream = csdef.VideoStreamStruct
	// AudioStream is an allocated audio stream.
	AudioStream = csdef.AudioStreamStruct
	// SnapshotStream is an allocated snapshot stream.
	SnapshotStream = csdef.SnapshotStreamStruct
	// StreamUsage is a stream's usage.
	StreamUsage = csdef.StreamUsageEnum
	// VideoResolution is a width and a height in pixels.
	VideoResolution = csdef.VideoResolutionStruct
	// SnapshotImage is a captured image: its data, codec and resolution.
	SnapshotImage = csdef.CaptureSnapshotResponse
	// StreamAllocationAction tells the VideoStreamPort how a stream came
	// out of an allocation.
	StreamAllocationAction = csdef.StreamAllocationAction
	// VideoStreamPort is the camera's video encoder.
	VideoStreamPort = csdef.VideoStreamPort
	// AudioStreamPort is the camera's audio encoder.
	AudioStreamPort = csdef.AudioStreamPort
	// SnapshotStreamPort is the camera's snapshot encoder.
	SnapshotStreamPort = csdef.SnapshotStreamPort
	// SnapshotCapturePort takes a picture.
	SnapshotCapturePort = csdef.SnapshotCapturePort
)

// The features.
const (
	FeatureAudio            = csdef.FeatureAudio
	FeatureVideo            = csdef.FeatureVideo
	FeatureSnapshot         = csdef.FeatureSnapshot
	FeaturePrivacy          = csdef.FeaturePrivacy
	FeatureSpeaker          = csdef.FeatureSpeaker
	FeatureImageControl     = csdef.FeatureImageControl
	FeatureWatermark        = csdef.FeatureWatermark
	FeatureOnScreenDisplay  = csdef.FeatureOnScreenDisplay
	FeatureLocalStorage     = csdef.FeatureLocalStorage
	FeatureHighDynamicRange = csdef.FeatureHighDynamicRange
	FeatureNightVision      = csdef.FeatureNightVision
)

// The allocation actions.
const (
	StreamNewAllocation = csdef.StreamNewAllocation
	StreamModification  = csdef.StreamModification
	StreamReuse         = csdef.StreamReuse
)

// DefaultMaxSnapshotSize bounds a captured image when the configuration
// names no bound: chip answers RESOURCE_EXHAUSTED for an image larger than
// its large-message payload less 100 bytes of room for the response's
// other fields (CameraAVStreamManagementCluster.h:53-55,
// kMaxSnapshotImageSize); here the large-message bound is the session's
// MaxTCPMessageSize fallback (matter.js's 64000, [sessionparams]).
const DefaultMaxSnapshotSize = int(sessionparams.DefaultMaxTCPMessageSize) - 100

// Configuration and reference-count errors.
var (
	// ErrAvStreamConfig reports a configuration chip's Init refuses or
	// the cluster's model does not hold.
	ErrAvStreamConfig = errors.New("camera: CameraAvStreamManagement configuration outside the cluster's model")
	// ErrNoStreamPort: VDO without a VideoStreamPort, ADO without an
	// AudioStreamPort, or SNP without a SnapshotStreamPort and a
	// SnapshotCapturePort.
	ErrNoStreamPort = errors.New("camera: VDO needs a VideoStreamPort, ADO an AudioStreamPort, SNP a SnapshotStreamPort and a SnapshotCapturePort")
	// ErrStreamNotFound: no allocated stream of the id (chip's
	// CHIP_ERROR_NOT_FOUND).
	ErrStreamNotFound = errors.New("camera: no allocated stream of the id")
	// ErrStreamRefCount: a reference count would leave 0..255 (chip's
	// CHIP_ERROR_INVALID_INTEGER_VALUE).
	ErrStreamRefCount = errors.New("camera: the stream's reference count would leave its range")
)

// The optional attributes chip's server takes (its OptionalAttribute set,
// CameraAVStreamManagementCluster.h:374-384).
var avStreamOptional = []uint32{
	csdef.AttrHardPrivacyModeOn, csdef.AttrNightVisionIllum, csdef.AttrMicrophoneAgcEnabled,
	csdef.AttrImageRotation, csdef.AttrImageFlipHorizontal, csdef.AttrImageFlipVertical,
	csdef.AttrStatusLightEnabled, csdef.AttrStatusLightBrightness,
}

// AvStreamConfig carries the construction parameters of an [AvStream].
type AvStreamConfig struct {
	Features AvStreamFeature
	// Attributes are the optional attributes served, among
	// HardPrivacyModeOn, NightVisionIllum, MicrophoneAgcEnabled,
	// ImageRotation, ImageFlipHorizontal, ImageFlipVertical,
	// StatusLightEnabled and StatusLightBrightness.
	Attributes []uint32
	// Initial are the values the server starts with — the capabilities
	// and limits (MaxConcurrentEncoders … MaxNetworkBandwidth,
	// SupportedStreamUsages) and the settings the host persisted — set
	// as [AvStream.SetAttributes] sets them.
	Initial map[uint32]any
	// StreamUsagePriorities is the ranked list of stream usages; nil
	// starts with SupportedStreamUsages, as chip's InitArguments pass the
	// same list for both.
	StreamUsagePriorities []StreamUsage
	// The streams the host persisted; their reference counts start at 0
	// (chip LoadAllocatedStreams, CameraAVStreamManagementCluster.cpp:
	// 1834-1838).
	AllocatedVideoStreams    []VideoStream
	AllocatedAudioStreams    []AudioStream
	AllocatedSnapshotStreams []SnapshotStream
	// The ports: VDO needs Video, ADO Audio, SNP Snapshot and Capture.
	Video    VideoStreamPort
	Audio    AudioStreamPort
	Snapshot SnapshotStreamPort
	Capture  SnapshotCapturePort
	// MaxSnapshotSize bounds a captured image; 0 is
	// [DefaultMaxSnapshotSize].
	MaxSnapshotSize int
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// AvStream implements [contract.ClusterServer] for
// CameraAvStreamManagement: chip's server logic over the generated server
// — the allocated video, audio and snapshot streams, validated against the
// cluster's rules and the capabilities before the host's port allocates
// them, reused and narrowed by the id the port returns, reference-counted
// for the transports that use them; the ranked stream priorities; the
// viewport's aspect-ratio rule; and the snapshot capture behind the
// privacy modes. The rules are connectedhomeip's
// CameraAVStreamManagementCluster.cpp at 6170af84 (matter.js's server is
// the generated behaviour).
type AvStream struct {
	*spec.Instance
	spec.SelfHeld

	srv      *spec.Server
	video    VideoStreamPort
	audio    AudioStreamPort
	snapshot SnapshotStreamPort
	capture  SnapshotCapturePort
	maxImage int

	// mu guards the streams and the priorities.
	mu         sync.Mutex
	videos     []VideoStream
	audios     []AudioStream
	snapshots  []SnapshotStream
	priorities []StreamUsage
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*AvStream)(nil)
	_ contract.ClusterDataVersion             = (*AvStream)(nil)
	_ contract.ClusterAttributeLister         = (*AvStream)(nil)
	_ contract.ClusterCommandLister           = (*AvStream)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*AvStream)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*AvStream)(nil)
	_ contract.AttributeChangeNotifier        = (*AvStream)(nil)
	_ spec.AttributeSetter                    = (*AvStream)(nil)
	_ spec.CommandRegistrar                   = (*AvStream)(nil)
)

// NewAvStream builds a CameraAvStreamManagement server. The feature and
// optional-attribute rules are chip's Init
// (CameraAVStreamManagementCluster.cpp:76-160).
func NewAvStream(cfg AvStreamConfig) (*AvStream, error) {
	if err := checkAvStreamConfig(cfg); err != nil {
		return nil, err
	}
	f := cfg.Features
	if f&FeatureVideo != 0 && cfg.Video == nil || f&FeatureAudio != 0 && cfg.Audio == nil ||
		f&FeatureSnapshot != 0 && (cfg.Snapshot == nil || cfg.Capture == nil) {
		return nil, ErrNoStreamPort
	}
	a := &AvStream{
		video: cfg.Video, audio: cfg.Audio, snapshot: cfg.Snapshot, capture: cfg.Capture,
		maxImage: cfg.MaxSnapshotSize,
	}
	if a.maxImage <= 0 {
		a.maxImage = DefaultMaxSnapshotSize
	}
	handlers := map[uint32]spec.CommandHandler{
		csdef.CmdVideoStreamAllocate:      a.videoStreamAllocate,
		csdef.CmdVideoStreamModify:        a.videoStreamModify,
		csdef.CmdVideoStreamDeallocate:    a.videoStreamDeallocate,
		csdef.CmdAudioStreamAllocate:      a.audioStreamAllocate,
		csdef.CmdAudioStreamDeallocate:    a.audioStreamDeallocate,
		csdef.CmdSnapshotStreamAllocate:   a.snapshotStreamAllocate,
		csdef.CmdSnapshotStreamModify:     a.snapshotStreamModify,
		csdef.CmdSnapshotStreamDeallocate: a.snapshotStreamDeallocate,
		csdef.CmdSetStreamPriorities:      a.setStreamPriorities,
		csdef.CmdCaptureSnapshot:          a.captureSnapshot,
	}
	opts := spec.Options{Features: uint32(f), Attributes: slices.Clone(cfg.Attributes)}
	srv, err := spec.NewServer(csdef.Definition, opts, spec.ServerConfig{
		Handlers: handlers, DataVersion: cfg.DataVersion, BeforeWrite: a.beforeWrite,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAvStreamConfig, err)
	}
	a.srv, a.Instance, a.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	if err := srv.SetAttributes(a.served(chipDefaults())); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAvStreamConfig, err)
	}
	if len(cfg.Initial) > 0 {
		if err := a.SetAttributes(cfg.Initial); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAvStreamConfig, err)
		}
	}
	a.priorities = slices.Clone(cfg.StreamUsagePriorities)
	if a.priorities == nil {
		a.priorities = a.supportedStreamUsages()
	}
	a.videos = slices.Clone(cfg.AllocatedVideoStreams)
	for i := range a.videos {
		a.videos[i].ReferenceCount = 0
	}
	a.audios = slices.Clone(cfg.AllocatedAudioStreams)
	for i := range a.audios {
		a.audios[i].ReferenceCount = 0
	}
	a.snapshots = slices.Clone(cfg.AllocatedSnapshotStreams)
	for i := range a.snapshots {
		a.snapshots[i].ReferenceCount = 0
	}
	a.mu.Lock()
	values := a.held()
	a.mu.Unlock()
	if err := srv.SetAttributes(a.served(values)); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAvStreamConfig, err)
	}
	return a, nil
}

// chipDefaults are chip's starting values (CameraAVStreamManagementCluster.h:
// 405-445 for the InitArguments, :740-764 for the members) of the
// attributes whose definition names no default; a definition's default
// (matter.js's model) stands where it names one.
func chipDefaults() map[uint32]any {
	all := map[uint32]any{
		csdef.AttrMaxConcurrentEncoders:            uint8(0),
		csdef.AttrMaxEncodedPixelRate:              uint32(0),
		csdef.AttrNightVisionUsesInfrared:          false,
		csdef.AttrMaxContentBufferSize:             uint32(0),
		csdef.AttrTwoWayTalkSupport:                csdef.TwoWayTalkSupportTypeNotSupported,
		csdef.AttrMaxNetworkBandwidth:              uint32(0),
		csdef.AttrCurrentFrameRate:                 uint16(0),
		csdef.AttrHdrModeEnabled:                   false,
		csdef.AttrSoftRecordingPrivacyModeEnabled:  false,
		csdef.AttrSoftLivestreamPrivacyModeEnabled: false,
		csdef.AttrHardPrivacyModeOn:                false,
		csdef.AttrNightVision:                      csdef.TriStateAutoOn,
		csdef.AttrNightVisionIllum:                 csdef.TriStateAutoOn,
		csdef.AttrViewport:                         csdef.ViewportStruct{},
		csdef.AttrSpeakerMuted:                     false,
		csdef.AttrSpeakerVolumeLevel:               uint8(0),
		csdef.AttrSpeakerMaxLevel:                  uint8(254), // kMaxSpeakerLevel
		csdef.AttrSpeakerMinLevel:                  uint8(0),
		csdef.AttrMicrophoneMuted:                  false,
		csdef.AttrMicrophoneVolumeLevel:            uint8(0),
		csdef.AttrMicrophoneMaxLevel:               uint8(254), // kMaxMicrophoneLevel
		csdef.AttrMicrophoneMinLevel:               uint8(0),
		csdef.AttrMicrophoneAgcEnabled:             false,
		csdef.AttrImageRotation:                    uint16(0),
		csdef.AttrImageFlipHorizontal:              false,
		csdef.AttrImageFlipVertical:                false,
		csdef.AttrLocalVideoRecordingEnabled:       false,
		csdef.AttrLocalSnapshotRecordingEnabled:    false,
		csdef.AttrStatusLightEnabled:               false,
		csdef.AttrStatusLightBrightness:            csdef.ThreeLevelAutoMedium,
	}
	for id := range all {
		if a := csdef.Definition.Attribute(id); a == nil || a.Default != nil {
			delete(all, id)
		}
	}
	return all
}

// checkAvStreamConfig is chip's Init: at least one of VDO, ADO and SNP;
// ICTL, WMARK, OSD and HDR need VDO or SNP; SPKR needs ADO;
// NightVisionIllum needs NV, MicrophoneAgcEnabled ADO; the image
// attributes need ICTL and ICTL needs one of them
// (CameraAVStreamManagementCluster.cpp:85-159). Of the optional
// attributes, only the ones chip knows are taken.
func checkAvStreamConfig(cfg AvStreamConfig) error {
	f := cfg.Features
	has := func(id uint32) bool { return slices.Contains(cfg.Attributes, id) }
	mandatory, err := spec.New(csdef.Definition, spec.Options{Features: uint32(f)})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAvStreamConfig, err)
	}
	for _, id := range cfg.Attributes {
		if !slices.Contains(avStreamOptional, id) && !mandatory.Serves(id) {
			return fmt.Errorf("%w: attribute 0x%04X is no optional attribute chip's server serves", ErrAvStreamConfig, id)
		}
	}
	imageAttrs := has(csdef.AttrImageFlipHorizontal) || has(csdef.AttrImageFlipVertical) || has(csdef.AttrImageRotation)
	switch {
	case f&(FeatureVideo|FeatureAudio|FeatureSnapshot) == 0: // :85-90
		return fmt.Errorf("%w: none of VDO, ADO and SNP", ErrAvStreamConfig)
	case f&(FeatureImageControl|FeatureWatermark|FeatureOnScreenDisplay|FeatureHighDynamicRange) != 0 &&
		f&(FeatureVideo|FeatureSnapshot) == 0: // :93-102
		return fmt.Errorf("%w: ICTL, WMARK, OSD and HDR need VDO or SNP", ErrAvStreamConfig)
	case f&FeatureSpeaker != 0 && f&FeatureAudio == 0: // :104-111
		return fmt.Errorf("%w: SPKR needs ADO", ErrAvStreamConfig)
	case has(csdef.AttrNightVisionIllum) && f&FeatureNightVision == 0: // :114-121
		return fmt.Errorf("%w: NightVisionIllum needs NV", ErrAvStreamConfig)
	case has(csdef.AttrMicrophoneAgcEnabled) && f&FeatureAudio == 0: // :123-129
		return fmt.Errorf("%w: MicrophoneAgcEnabled needs ADO", ErrAvStreamConfig)
	case imageAttrs && f&FeatureImageControl == 0: // :133-142
		return fmt.Errorf("%w: ImageRotation and the image flips need ICTL", ErrAvStreamConfig)
	case f&FeatureImageControl != 0 && !imageAttrs: // :144-153
		return fmt.Errorf("%w: ICTL needs ImageRotation or an image flip", ErrAvStreamConfig)
	}
	return nil
}

// held is the value of every attribute the server holds itself; the
// caller holds a.mu.
func (a *AvStream) held() map[uint32]any {
	return map[uint32]any{
		csdef.AttrAllocatedVideoStreams:    spec.List[VideoStream](nonNil(a.videos)),
		csdef.AttrAllocatedAudioStreams:    spec.List[AudioStream](nonNil(a.audios)),
		csdef.AttrAllocatedSnapshotStreams: spec.List[SnapshotStream](nonNil(a.snapshots)),
		csdef.AttrStreamUsagePriorities:    nonNil(a.priorities),
	}
}

// served keeps the values of the attributes the instance serves.
func (a *AvStream) served(values map[uint32]any) map[uint32]any {
	out := make(map[uint32]any, len(values))
	for id, v := range values {
		if a.Serves(id) {
			out[id] = v
		}
	}
	return out
}

// publish stores the held attributes among ids at their current value;
// the generated server reports the ones that changed. Mirrors chip's
// PersistAndNotify (CameraAVStreamManagementCluster.cpp:1763-1773), the
// persistence being the host's.
func (a *AvStream) publish(origin contract.Origin, ids ...uint32) {
	a.mu.Lock()
	all := a.held()
	a.mu.Unlock()
	values := make(map[uint32]any, len(ids))
	for _, id := range ids {
		values[id] = all[id]
	}
	_ = a.srv.SetAttributesFrom(origin, a.served(values))
}

// stored returns attribute id's value as T; the zero T when the server
// does not hold one.
func stored[T any](a *AvStream, id uint32) T {
	var t T
	if v, ok := a.srv.Value(id); ok {
		spec.Assign(&t, v)
	}
	return t
}

func (a *AvStream) supportedStreamUsages() []StreamUsage {
	return stored[[]StreamUsage](a, csdef.AttrSupportedStreamUsages)
}

func (a *AvStream) snapshotCapabilities() []csdef.SnapshotCapabilitiesStruct {
	return stored[[]csdef.SnapshotCapabilitiesStruct](a, csdef.AttrSnapshotCapabilities)
}

// beforeWrite holds a controller's write of Viewport to chip's rule; the
// other writes are the definition's (their constraints are the levels'
// ranges and the rotation's bound, as chip's setters check them,
// CameraAVStreamManagementCluster.cpp:1178-1256).
func (a *AvStream) beforeWrite(_ context.Context, attrID uint32, value any) error {
	if attrID != csdef.AttrViewport {
		return nil
	}
	var vp csdef.ViewportStruct
	if !spec.Assign(&vp, value) {
		return spec.Errorf(im.StatusConstraintError, "CameraAvStreamManagement: Viewport %T", value)
	}
	return a.checkViewport(vp)
}

// checkViewport is chip's SetViewport: the viewport is no smaller than
// MinViewportResolution, no larger than the sensor, and has the sensor's
// aspect ratio to two decimal places, each CONSTRAINT_ERROR otherwise.
// Mirrors chip CameraAVStreamManagementCluster.cpp:1137-1171, including
// its 16-bit width and height and its float32 ratio.
func (a *AvStream) checkViewport(vp csdef.ViewportStruct) error {
	return viewportRule(vp, stored[VideoResolution](a, csdef.AttrMinViewportResolution),
		stored[csdef.VideoSensorParamsStruct](a, csdef.AttrVideoSensorParams))
}

func viewportRule(vp csdef.ViewportStruct, minRes VideoResolution, sensor csdef.VideoSensorParamsStruct) error {
	w, h := vp.X2-vp.X1, vp.Y2-vp.Y1
	if w < minRes.Width || h < minRes.Height || w > sensor.SensorWidth || h > sensor.SensorHeight {
		return spec.Errorf(im.StatusConstraintError, "CameraAvStreamManagement: Viewport %dx%d outside %dx%d to the sensor's %dx%d",
			w, h, minRes.Width, minRes.Height, sensor.SensorWidth, sensor.SensorHeight)
	}
	if aspectRatio(w, h) != aspectRatio(sensor.SensorWidth, sensor.SensorHeight) {
		return spec.Errorf(im.StatusConstraintError, "CameraAvStreamManagement: Viewport %dx%d has not the sensor's aspect ratio", w, h)
	}
	return nil
}

// aspectRatio is w/h floored to two decimal places in float32, as chip
// computes it (floorf, :1155-1156).
func aspectRatio(w, h uint16) float32 {
	return float32(math.Floor(float64(float32(w)/float32(h)*100))) / 100
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change —
// a capability, a limit, a setting it persisted, the frame rate or the
// hard privacy switch (chip's setters) — checked against the definition,
// and a Viewport against chip's rule; StreamUsagePriorities sets the
// ranked list (chip SetStreamUsagePriorities, :271-279). The allocated
// streams are the server's ([spec.ErrNotHeld]): the persisted ones arrive
// through [AvStreamConfig]. All or nothing.
func (a *AvStream) SetAttributes(values map[uint32]any) error {
	ids := slices.Sorted(maps.Keys(values))
	rest := make(map[uint32]any, len(values))
	var priorities []StreamUsage
	setPriorities := false
	for _, id := range ids {
		switch id {
		case csdef.AttrAllocatedVideoStreams, csdef.AttrAllocatedAudioStreams, csdef.AttrAllocatedSnapshotStreams:
			return fmt.Errorf("%w: camera: attribute 0x%04X is the server's allocated streams", spec.ErrNotHeld, id)
		case csdef.AttrStreamUsagePriorities:
			if _, err := a.CheckSet(id, values[id], nil); err != nil {
				return err
			}
			if !spec.Assign(&priorities, values[id]) {
				return fmt.Errorf("%w: camera: StreamUsagePriorities %T", spec.ErrInvalidValue, values[id])
			}
			setPriorities = true
		default:
			rest[id] = values[id]
		}
	}
	if v, ok := rest[csdef.AttrViewport]; ok {
		var vp csdef.ViewportStruct
		if !spec.Assign(&vp, v) {
			return fmt.Errorf("%w: camera: Viewport %T", spec.ErrInvalidValue, v)
		}
		// The rule reads the sensor and the minimum as they are after
		// this change.
		if err := a.checkViewportWith(vp, rest); err != nil {
			return fmt.Errorf("%w: %w", spec.ErrInvalidValue, err)
		}
	}
	if setPriorities {
		rest[csdef.AttrStreamUsagePriorities] = nonNil(priorities)
	}
	if err := a.srv.SetAttributes(rest); err != nil {
		return err
	}
	if setPriorities {
		a.mu.Lock()
		a.priorities = slices.Clone(priorities)
		a.mu.Unlock()
	}
	return nil
}

// checkViewportWith is checkViewport against the sensor and the minimum
// the pending values set, the stored ones otherwise.
func (a *AvStream) checkViewportWith(vp csdef.ViewportStruct, pending map[uint32]any) error {
	minRes := stored[VideoResolution](a, csdef.AttrMinViewportResolution)
	sensor := stored[csdef.VideoSensorParamsStruct](a, csdef.AttrVideoSensorParams)
	if v, ok := pending[csdef.AttrMinViewportResolution]; ok {
		spec.Assign(&minRes, v)
	}
	if v, ok := pending[csdef.AttrVideoSensorParams]; ok {
		spec.Assign(&sensor, v)
	}
	return viewportRule(vp, minRes, sensor)
}

// VideoStreams returns AllocatedVideoStreams.
func (a *AvStream) VideoStreams() []VideoStream {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.videos)
}

// AudioStreams returns AllocatedAudioStreams.
func (a *AvStream) AudioStreams() []AudioStream {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.audios)
}

// SnapshotStreams returns AllocatedSnapshotStreams.
func (a *AvStream) SnapshotStreams() []SnapshotStream {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.snapshots)
}

// StreamUsagePriorities returns the ranked stream usages.
func (a *AvStream) StreamUsagePriorities() []StreamUsage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.priorities)
}

// SupportedStreamUsages returns SupportedStreamUsages.
func (a *AvStream) SupportedStreamUsages() []StreamUsage {
	return slices.Clone(a.supportedStreamUsages())
}

// PrivacyModes returns HardPrivacyModeOn, SoftRecordingPrivacyModeEnabled
// and SoftLivestreamPrivacyModeEnabled, false where the server does not
// serve one — what the transports check before they stream (chip's
// IsHardPrivacyModeActive … in the camera app's stream manager,
// camera-av-stream-manager.cpp:309-329).
func (a *AvStream) PrivacyModes() (hard, softRecording, softLivestream bool) {
	return stored[bool](a, csdef.AttrHardPrivacyModeOn),
		stored[bool](a, csdef.AttrSoftRecordingPrivacyModeEnabled),
		stored[bool](a, csdef.AttrSoftLivestreamPrivacyModeEnabled)
}

// The reference counts, chip's UpdateVideoStreamRefCount …
// UpdateSnapshotStreamRefCount (CameraAVStreamManagementCluster.cpp:
// 486-557): a transport that uses a stream (WebRTC, PushAV) counts it up
// and down; a stream in use cannot be deallocated.

// UpdateVideoStreamRefCount counts the video stream's use up or down.
func (a *AvStream) UpdateVideoStreamRefCount(videoStreamID uint16, increment bool) error {
	return a.updateRefCount(csdef.AttrAllocatedVideoStreams, func() (*uint8, bool) {
		i := slices.IndexFunc(a.videos, func(s VideoStream) bool { return s.VideoStreamId == videoStreamID })
		if i < 0 {
			return nil, false
		}
		return &a.videos[i].ReferenceCount, true
	}, increment)
}

// UpdateAudioStreamRefCount counts the audio stream's use up or down.
func (a *AvStream) UpdateAudioStreamRefCount(audioStreamID uint16, increment bool) error {
	return a.updateRefCount(csdef.AttrAllocatedAudioStreams, func() (*uint8, bool) {
		i := slices.IndexFunc(a.audios, func(s AudioStream) bool { return s.AudioStreamId == audioStreamID })
		if i < 0 {
			return nil, false
		}
		return &a.audios[i].ReferenceCount, true
	}, increment)
}

// UpdateSnapshotStreamRefCount counts the snapshot stream's use up or
// down.
func (a *AvStream) UpdateSnapshotStreamRefCount(snapshotStreamID uint16, increment bool) error {
	return a.updateRefCount(csdef.AttrAllocatedSnapshotStreams, func() (*uint8, bool) {
		i := slices.IndexFunc(a.snapshots, func(s SnapshotStream) bool { return s.SnapshotStreamId == snapshotStreamID })
		if i < 0 {
			return nil, false
		}
		return &a.snapshots[i].ReferenceCount, true
	}, increment)
}

func (a *AvStream) updateRefCount(attrID uint32, find func() (*uint8, bool), increment bool) error {
	a.mu.Lock()
	count, ok := find()
	switch {
	case !ok:
		a.mu.Unlock()
		return ErrStreamNotFound
	case increment && *count == math.MaxUint8, !increment && *count == 0:
		a.mu.Unlock()
		return ErrStreamRefCount
	case increment:
		*count++
	default:
		*count--
	}
	a.mu.Unlock()
	a.publish(contract.OriginHostSet, attrID)
	return nil
}

// ReusableVideoStreamID is chip's GetReusableVideoStreamId
// (CameraAVStreamManagementCluster.cpp:281-321): an allocated stream of
// the requested codec and key-frame interval whose ranges contain the
// requested ones, for a VideoStreamPort that reuses streams.
func (a *AvStream) ReusableVideoStreamID(req VideoStream) (uint16, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.videos {
		if req.VideoCodec != s.VideoCodec ||
			req.MinFrameRate < s.MinFrameRate || req.MaxFrameRate > s.MaxFrameRate ||
			req.MinResolution.Width < s.MinResolution.Width || req.MinResolution.Height < s.MinResolution.Height ||
			req.MaxResolution.Width > s.MaxResolution.Width || req.MaxResolution.Height > s.MaxResolution.Height ||
			req.MinBitRate < s.MinBitRate || req.MaxBitRate > s.MaxBitRate ||
			req.KeyFrameInterval != s.KeyFrameInterval {
			continue
		}
		return s.VideoStreamId, true
	}
	return 0, false
}

// ReusableSnapshotStreamID is chip's GetReusableSnapshotStreamId
// (:397-432): an allocated stream of the requested codec and quality whose
// frame rate and resolution range contain the requested ones.
func (a *AvStream) ReusableSnapshotStreamID(req SnapshotStream) (uint16, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.snapshots {
		if req.ImageCodec != s.ImageCodec || req.Quality != s.Quality || req.FrameRate > s.FrameRate ||
			req.MinResolution.Width < s.MinResolution.Width || req.MinResolution.Height < s.MinResolution.Height ||
			req.MaxResolution.Width > s.MaxResolution.Width || req.MaxResolution.Height > s.MaxResolution.Height {
			continue
		}
		return s.SnapshotStreamId, true
	}
	return 0, false
}

// ResourceAvailable is chip's IsResourceAvailableForStreamAllocation
// (:2717-2756): the allocated video streams, the encoded snapshot streams
// and the candidate stay within MaxEncodedPixelRate and
// MaxConcurrentEncoders — for a port to decide RESOURCE_EXHAUSTED.
func (a *AvStream) ResourceAvailable(candidateEncodedPixelRate uint32, encoderRequired bool) bool {
	maxRate := stored[uint32](a, csdef.AttrMaxEncodedPixelRate)
	maxEncoders := stored[uint8](a, csdef.AttrMaxConcurrentEncoders)
	a.mu.Lock()
	defer a.mu.Unlock()
	rate := uint64(candidateEncodedPixelRate)
	encoders := 0
	if encoderRequired {
		encoders = 1
	}
	for _, s := range a.videos {
		rate += uint64(s.MaxFrameRate) * uint64(s.MaxResolution.Height) * uint64(s.MaxResolution.Width)
	}
	for _, s := range a.snapshots {
		if s.EncodedPixels {
			rate += uint64(s.FrameRate) * uint64(s.MaxResolution.Height) * uint64(s.MaxResolution.Width)
			if s.HardwareEncoder {
				encoders++
			}
		}
	}
	return rate <= uint64(maxRate) && len(a.videos)+encoders <= int(maxEncoders)
}

// liveStreamUsage reports a usage a controller may allocate a stream for:
// Recording, Analysis or LiveView, not Internal (the request field's
// constraint, chip :2083-2089, :2265-2271).
func liveStreamUsage(u StreamUsage) bool {
	return u == csdef.StreamUsageRecording || u == csdef.StreamUsageAnalysis || u == csdef.StreamUsageLiveView
}

func (a *AvStream) prioritised(u StreamUsage) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Contains(a.priorities, u)
}

// optionalFieldsMatch is chip's rule that a request carries
// WatermarkEnabled exactly with WMARK and OSDEnabled exactly with OSD
// (:2076, :2081, :2374, :2379).
func (a *AvStream) optionalFieldsMatch(watermark, osd *bool) bool {
	return a.HasFeature("WMARK") == (watermark != nil) && a.HasFeature("OSD") == (osd != nil)
}

// videoStreamAllocate carries out VideoStreamAllocate. Mirrors chip
// HandleVideoStreamAllocate (CameraAVStreamManagementCluster.cpp:
// 2065-2182): the request's checks in chip's order, the port's
// allocation, then the new stream added or the reused one narrowed to the
// intersection of both ranges (UpdateVideoStreamRangeParams, :330-368),
// and the port told the stream as allocated (OnVideoStreamAllocated).
func (a *AvStream) videoStreamAllocate(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.VideoStreamAllocateRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: VideoStreamAllocate payload %T", fields)
	}
	if !a.optionalFieldsMatch(req.WatermarkEnabled, req.OsdEnabled) {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: WatermarkEnabled or OSDEnabled does not match the features")
	}
	constraint := func(what string) error {
		return spec.Errorf(im.StatusConstraintError, "CameraAvStreamManagement: VideoStreamAllocate %s", what)
	}
	switch {
	case !liveStreamUsage(req.StreamUsage): // :2083-2089
		return nil, constraint("StreamUsage")
	case req.VideoCodec > csdef.VideoCodecAv1: // :2091-2095
		return nil, constraint("VideoCodec")
	case req.MinFrameRate < 1 || req.MinFrameRate > req.MaxFrameRate: // :2097-2099
		return nil, constraint("frame rates")
	case req.MinResolution.Width < 1 || req.MinResolution.Height < 1 || req.MaxResolution.Width < 1 || req.MaxResolution.Height < 1,
		req.MinResolution.Width > req.MaxResolution.Width || req.MinResolution.Height > req.MaxResolution.Height: // :2101-2110
		return nil, constraint("resolutions")
	case req.MinBitRate < 1 || req.MinBitRate > req.MaxBitRate: // :2112-2114
		return nil, constraint("bit rates")
	case req.KeyFrameInterval > 65500: // :2116, kMaxKeyFrameIntervalMaxValue
		return nil, constraint("KeyFrameInterval")
	}
	if !a.prioritised(req.StreamUsage) { // :2118-2123
		return nil, spec.Errorf(im.StatusInvalidInState, "CameraAvStreamManagement: StreamUsage %d is not in StreamUsagePriorities", req.StreamUsage)
	}
	args := VideoStream{
		StreamUsage: req.StreamUsage, VideoCodec: req.VideoCodec,
		MinFrameRate: req.MinFrameRate, MaxFrameRate: req.MaxFrameRate,
		MinResolution: req.MinResolution, MaxResolution: req.MaxResolution,
		MinBitRate: req.MinBitRate, MaxBitRate: req.MaxBitRate,
		KeyFrameInterval: req.KeyFrameInterval,
		WatermarkEnabled: cloneBool(req.WatermarkEnabled), OsdEnabled: cloneBool(req.OsdEnabled),
	}
	id, err := a.video.VideoStreamAllocate(ctx, args) // :2141
	if err != nil {
		return nil, portError("VideoStreamAllocate", err)
	}
	a.mu.Lock()
	action, stream, changed := StreamNewAllocation, args, true
	if i := slices.IndexFunc(a.videos, func(s VideoStream) bool { return s.VideoStreamId == id }); i < 0 { // :2151-2159
		stream.VideoStreamId = id
		a.videos = append(a.videos, stream)
	} else { // :2160-2171
		changed = narrowVideoStream(&a.videos[i], args)
		action, stream = StreamReuse, a.videos[i]
		if changed {
			action = StreamModification
		}
	}
	a.mu.Unlock()
	if changed {
		a.publish(contract.OriginCommand, csdef.AttrAllocatedVideoStreams)
	}
	a.video.VideoStreamAllocated(ctx, stream, action)
	return csdef.VideoStreamAllocateResponse{VideoStreamId: id}, nil
}

// narrowVideoStream narrows s to the intersection of its ranges and r's
// and reports a change. Mirrors chip UpdateVideoStreamRangeParams
// (:330-368).
func narrowVideoStream(s *VideoStream, r VideoStream) bool {
	before := *s
	s.MinFrameRate = max(s.MinFrameRate, r.MinFrameRate)
	s.MaxFrameRate = min(s.MaxFrameRate, r.MaxFrameRate)
	s.MinResolution.Width = max(s.MinResolution.Width, r.MinResolution.Width)
	s.MinResolution.Height = max(s.MinResolution.Height, r.MinResolution.Height)
	s.MaxResolution.Width = min(s.MaxResolution.Width, r.MaxResolution.Width)
	s.MaxResolution.Height = min(s.MaxResolution.Height, r.MaxResolution.Height)
	s.MinBitRate = max(s.MinBitRate, r.MinBitRate)
	s.MaxBitRate = min(s.MaxBitRate, r.MaxBitRate)
	return before.MinFrameRate != s.MinFrameRate || before.MaxFrameRate != s.MaxFrameRate ||
		before.MinResolution != s.MinResolution || before.MaxResolution != s.MaxResolution ||
		before.MinBitRate != s.MinBitRate || before.MaxBitRate != s.MaxBitRate
}

// modifyFields is chip's check of a Modify request: a given
// WatermarkEnabled needs WMARK and a given OSDEnabled OSD, and one of them
// is given, each INVALID_COMMAND otherwise (:2191-2218, :2501-2529).
func (a *AvStream) modifyFields(watermark, osd *bool) error {
	switch {
	case watermark != nil && !a.HasFeature("WMARK"):
		return spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: WatermarkEnabled without WMARK")
	case osd != nil && !a.HasFeature("OSD"):
		return spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: OSDEnabled without OSD")
	case watermark == nil && osd == nil:
		return spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: neither WatermarkEnabled nor OSDEnabled")
	}
	return nil
}

// checkStream is chip's ValidateStreamForModifyOrDeallocateImpl for the
// video and audio streams (CameraAVStreamManagementCluster.h:793-821): an
// unknown stream is NOT_FOUND, one in use cannot be deallocated
// (INVALID_IN_STATE), an Internal stream is the device's
// (DYNAMIC_CONSTRAINT_ERROR).
func checkStream(kind string, found bool, refCount uint8, usage StreamUsage, deallocate bool, id uint16) error {
	switch {
	case !found:
		return spec.Errorf(im.StatusNotFound, "CameraAvStreamManagement: %s stream %d not found", kind, id)
	case deallocate && refCount > 0:
		return spec.Errorf(im.StatusInvalidInState, "CameraAvStreamManagement: %s stream %d still in use", kind, id)
	case usage == csdef.StreamUsageInternal:
		return spec.Errorf(im.StatusDynamicConstraintError, "CameraAvStreamManagement: %s stream %d is Internal", kind, id)
	}
	return nil
}

func (a *AvStream) checkVideoStream(id uint16, deallocate bool) error {
	a.mu.Lock()
	i := slices.IndexFunc(a.videos, func(s VideoStream) bool { return s.VideoStreamId == id })
	var s VideoStream
	if i >= 0 {
		s = a.videos[i]
	}
	a.mu.Unlock()
	return checkStream("video", i >= 0, s.ReferenceCount, s.StreamUsage, deallocate, id)
}

// videoStreamModify carries out VideoStreamModify. Mirrors chip
// HandleVideoStreamModify (:2184-2235) and ModifyVideoStream
// (:1022-1048).
func (a *AvStream) videoStreamModify(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.VideoStreamModifyRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: VideoStreamModify payload %T", fields)
	}
	if err := a.modifyFields(req.WatermarkEnabled, req.OsdEnabled); err != nil {
		return nil, err
	}
	if err := a.checkVideoStream(req.VideoStreamId, false); err != nil {
		return nil, err
	}
	if err := a.video.VideoStreamModify(ctx, req.VideoStreamId, req.WatermarkEnabled, req.OsdEnabled); err != nil {
		return nil, portError("VideoStreamModify", err)
	}
	a.mu.Lock()
	changed := false
	if i := slices.IndexFunc(a.videos, func(s VideoStream) bool { return s.VideoStreamId == req.VideoStreamId }); i >= 0 {
		changed = applyModify(&a.videos[i].WatermarkEnabled, &a.videos[i].OsdEnabled, req.WatermarkEnabled, req.OsdEnabled)
	}
	a.mu.Unlock()
	if changed {
		a.publish(contract.OriginCommand, csdef.AttrAllocatedVideoStreams)
	}
	return nil, nil
}

// applyModify sets the given flags and reports a change (chip
// ModifyVideoStream / ModifySnapshotStream, :1022-1076).
func applyModify(watermark, osd **bool, newWatermark, newOSD *bool) bool {
	changed := false
	if newWatermark != nil {
		changed = *watermark == nil || **watermark != *newWatermark
		*watermark = cloneBool(newWatermark)
	}
	if newOSD != nil {
		changed = changed || *osd == nil || **osd != *newOSD
		*osd = cloneBool(newOSD)
	}
	return changed
}

func cloneBool(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

// videoStreamDeallocate carries out VideoStreamDeallocate. Mirrors chip
// HandleVideoStreamDeallocate (:2237-2257).
func (a *AvStream) videoStreamDeallocate(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.VideoStreamDeallocateRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: VideoStreamDeallocate payload %T", fields)
	}
	if err := a.checkVideoStream(req.VideoStreamId, true); err != nil {
		return nil, err
	}
	if err := a.video.VideoStreamDeallocate(ctx, req.VideoStreamId); err != nil {
		return nil, portError("VideoStreamDeallocate", err)
	}
	a.mu.Lock()
	a.videos = slices.DeleteFunc(a.videos, func(s VideoStream) bool { return s.VideoStreamId == req.VideoStreamId })
	a.mu.Unlock()
	a.publish(contract.OriginCommand, csdef.AttrAllocatedVideoStreams)
	return nil, nil
}

// audioStreamAllocate carries out AudioStreamAllocate. Mirrors chip
// HandleAudioStreamAllocate (:2259-2339): a reused id leaves its stream
// as it is.
func (a *AvStream) audioStreamAllocate(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.AudioStreamAllocateRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: AudioStreamAllocate payload %T", fields)
	}
	constraint := func(what string) error {
		return spec.Errorf(im.StatusConstraintError, "CameraAvStreamManagement: AudioStreamAllocate %s", what)
	}
	switch {
	case !liveStreamUsage(req.StreamUsage): // :2265-2271
		return nil, constraint("StreamUsage")
	case req.AudioCodec > csdef.AudioCodecAacLc: // :2273-2277
		return nil, constraint("AudioCodec")
	case req.ChannelCount < 1 || req.ChannelCount > 8: // :2279-2283, kMaxChannelCount
		return nil, constraint("ChannelCount")
	case req.SampleRate == 0: // :2285
		return nil, constraint("SampleRate")
	case req.BitRate == 0: // :2287
		return nil, constraint("BitRate")
	case req.BitDepth != 8 && req.BitDepth != 16 && req.BitDepth != 24 && req.BitDepth != 32: // :2289-2293
		return nil, constraint("BitDepth")
	}
	if !a.prioritised(req.StreamUsage) { // :2295-2300
		return nil, spec.Errorf(im.StatusInvalidInState, "CameraAvStreamManagement: StreamUsage %d is not in StreamUsagePriorities", req.StreamUsage)
	}
	args := AudioStream{
		StreamUsage: req.StreamUsage, AudioCodec: req.AudioCodec, ChannelCount: req.ChannelCount,
		SampleRate: req.SampleRate, BitRate: req.BitRate, BitDepth: req.BitDepth,
	}
	id, err := a.audio.AudioStreamAllocate(ctx, args) // :2313
	if err != nil {
		return nil, portError("AudioStreamAllocate", err)
	}
	a.mu.Lock()
	added := false
	if !slices.ContainsFunc(a.audios, func(s AudioStream) bool { return s.AudioStreamId == id }) { // :2320-2331
		args.AudioStreamId = id
		a.audios = append(a.audios, args)
		added = true
	}
	a.mu.Unlock()
	if added {
		a.publish(contract.OriginCommand, csdef.AttrAllocatedAudioStreams)
	}
	return csdef.AudioStreamAllocateResponse{AudioStreamId: id}, nil
}

// audioStreamDeallocate carries out AudioStreamDeallocate. Mirrors chip
// HandleAudioStreamDeallocate (:2341-2362).
func (a *AvStream) audioStreamDeallocate(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.AudioStreamDeallocateRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: AudioStreamDeallocate payload %T", fields)
	}
	a.mu.Lock()
	i := slices.IndexFunc(a.audios, func(s AudioStream) bool { return s.AudioStreamId == req.AudioStreamId })
	var s AudioStream
	if i >= 0 {
		s = a.audios[i]
	}
	a.mu.Unlock()
	if err := checkStream("audio", i >= 0, s.ReferenceCount, s.StreamUsage, true, req.AudioStreamId); err != nil {
		return nil, err
	}
	if err := a.audio.AudioStreamDeallocate(ctx, req.AudioStreamId); err != nil {
		return nil, portError("AudioStreamDeallocate", err)
	}
	a.mu.Lock()
	a.audios = slices.DeleteFunc(a.audios, func(s AudioStream) bool { return s.AudioStreamId == req.AudioStreamId })
	a.mu.Unlock()
	a.publish(contract.OriginCommand, csdef.AttrAllocatedAudioStreams)
	return nil, nil
}

// matchingSnapshotCapability is the first SnapshotCapabilities entry of
// the codec whose frame rate reaches frameRate and whose resolution lies
// within [minRes, maxRes] (chip :2423-2429, CameraAVStreamManagementCluster.h:
// 831-838).
func matchingSnapshotCapability(caps []csdef.SnapshotCapabilitiesStruct, codec csdef.ImageCodecEnum, frameRate uint16, minRes, maxRes VideoResolution) (csdef.SnapshotCapabilitiesStruct, bool) {
	for _, c := range caps {
		if c.ImageCodec == codec && c.MaxFrameRate >= frameRate &&
			c.Resolution.Width >= minRes.Width && c.Resolution.Height >= minRes.Height &&
			c.Resolution.Width <= maxRes.Width && c.Resolution.Height <= maxRes.Height {
			return c, true
		}
	}
	return csdef.SnapshotCapabilitiesStruct{}, false
}

// snapshotStreamAllocate carries out SnapshotStreamAllocate. Mirrors chip
// HandleSnapshotStreamAllocate (:2364-2491): the request's checks, the
// matching capability (DYNAMIC_CONSTRAINT_ERROR without one), which sets
// EncodedPixels and HardwareEncoder, the port's allocation, then the new
// stream added or the reused one's resolution range narrowed
// (UpdateSnapshotStreamRangeParams, :441-474).
func (a *AvStream) snapshotStreamAllocate(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.SnapshotStreamAllocateRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: SnapshotStreamAllocate payload %T", fields)
	}
	if !a.optionalFieldsMatch(req.WatermarkEnabled, req.OsdEnabled) { // :2374, :2379
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: WatermarkEnabled or OSDEnabled does not match the features")
	}
	constraint := func(what string) error {
		return spec.Errorf(im.StatusConstraintError, "CameraAvStreamManagement: SnapshotStreamAllocate %s", what)
	}
	switch {
	case req.ImageCodec > csdef.ImageCodecHeic: // :2381-2385
		return nil, constraint("ImageCodec")
	case req.MaxFrameRate == 0: // :2387-2391
		return nil, constraint("MaxFrameRate")
	case req.MinResolution.Width < 1 || req.MinResolution.Height < 1 || req.MaxResolution.Width < 1 || req.MaxResolution.Height < 1,
		req.MinResolution.Width > req.MaxResolution.Width || req.MinResolution.Height > req.MaxResolution.Height: // :2393-2402
		return nil, constraint("resolutions")
	case req.Quality < 1 || req.Quality > 100: // :2404-2408, kMaxImageQualityMetric
		return nil, constraint("Quality")
	}
	capability, ok := matchingSnapshotCapability(a.snapshotCapabilities(), req.ImageCodec, req.MaxFrameRate, req.MinResolution, req.MaxResolution)
	if !ok { // :2441-2445
		return nil, spec.Errorf(im.StatusDynamicConstraintError, "CameraAvStreamManagement: no SnapshotCapabilities entry matches the request")
	}
	args := SnapshotStream{
		ImageCodec: req.ImageCodec, FrameRate: req.MaxFrameRate,
		MinResolution: req.MinResolution, MaxResolution: req.MaxResolution, Quality: req.Quality,
		EncodedPixels:    capability.RequiresEncodedPixels, // :2435-2439
		WatermarkEnabled: cloneBool(req.WatermarkEnabled), OsdEnabled: cloneBool(req.OsdEnabled),
	}
	if capability.RequiresEncodedPixels && capability.RequiresHardwareEncoder != nil {
		args.HardwareEncoder = *capability.RequiresHardwareEncoder
	}
	id, err := a.snapshot.SnapshotStreamAllocate(ctx, args) // :2448
	if err != nil {
		return nil, portError("SnapshotStreamAllocate", err)
	}
	a.mu.Lock()
	changed := true
	if i := slices.IndexFunc(a.snapshots, func(s SnapshotStream) bool { return s.SnapshotStreamId == id }); i < 0 { // :2461-2478
		args.SnapshotStreamId = id
		a.snapshots = append(a.snapshots, args)
	} else { // :2479-2484
		changed = narrowSnapshotStream(&a.snapshots[i], args)
	}
	a.mu.Unlock()
	if changed {
		a.publish(contract.OriginCommand, csdef.AttrAllocatedSnapshotStreams)
	}
	return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: id}, nil
}

// narrowSnapshotStream narrows s's resolution range to the intersection
// with r's and reports a change. Mirrors chip
// UpdateSnapshotStreamRangeParams (:441-474).
func narrowSnapshotStream(s *SnapshotStream, r SnapshotStream) bool {
	before := *s
	s.MinResolution.Width = max(s.MinResolution.Width, r.MinResolution.Width)
	s.MinResolution.Height = max(s.MinResolution.Height, r.MinResolution.Height)
	s.MaxResolution.Width = min(s.MaxResolution.Width, r.MaxResolution.Width)
	s.MaxResolution.Height = min(s.MaxResolution.Height, r.MaxResolution.Height)
	return before.MinResolution != s.MinResolution || before.MaxResolution != s.MaxResolution
}

// checkSnapshotStream is chip's ValidateStreamForModifyOrDeallocateImpl
// for a snapshot stream (CameraAVStreamManagementCluster.h:793-859): an
// unknown stream is NOT_FOUND, one in use cannot be deallocated; a
// Modify needs a capability that matches the stream and does not name it
// as based on a video stream (RequiresHardwareEncoder false), each
// INVALID_IN_STATE otherwise.
func (a *AvStream) checkSnapshotStream(id uint16, deallocate bool) error {
	a.mu.Lock()
	i := slices.IndexFunc(a.snapshots, func(s SnapshotStream) bool { return s.SnapshotStreamId == id })
	var s SnapshotStream
	if i >= 0 {
		s = a.snapshots[i]
	}
	a.mu.Unlock()
	switch {
	case i < 0:
		return spec.Errorf(im.StatusNotFound, "CameraAvStreamManagement: snapshot stream %d not found", id)
	case deallocate && s.ReferenceCount > 0:
		return spec.Errorf(im.StatusInvalidInState, "CameraAvStreamManagement: snapshot stream %d still in use", id)
	case deallocate:
		return nil
	}
	c, ok := matchingSnapshotCapability(a.snapshotCapabilities(), s.ImageCodec, s.FrameRate, s.MinResolution, s.MaxResolution)
	if !ok || c.RequiresHardwareEncoder != nil && !*c.RequiresHardwareEncoder {
		return spec.Errorf(im.StatusInvalidInState, "CameraAvStreamManagement: snapshot stream %d is not modifiable", id)
	}
	return nil
}

// snapshotStreamModify carries out SnapshotStreamModify. Mirrors chip
// HandleSnapshotStreamModify (:2493-2545) and ModifySnapshotStream
// (:1050-1076).
func (a *AvStream) snapshotStreamModify(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.SnapshotStreamModifyRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: SnapshotStreamModify payload %T", fields)
	}
	if err := a.modifyFields(req.WatermarkEnabled, req.OsdEnabled); err != nil {
		return nil, err
	}
	if err := a.checkSnapshotStream(req.SnapshotStreamId, false); err != nil {
		return nil, err
	}
	if err := a.snapshot.SnapshotStreamModify(ctx, req.SnapshotStreamId, req.WatermarkEnabled, req.OsdEnabled); err != nil {
		return nil, portError("SnapshotStreamModify", err)
	}
	a.mu.Lock()
	changed := false
	if i := slices.IndexFunc(a.snapshots, func(s SnapshotStream) bool { return s.SnapshotStreamId == req.SnapshotStreamId }); i >= 0 {
		changed = applyModify(&a.snapshots[i].WatermarkEnabled, &a.snapshots[i].OsdEnabled, req.WatermarkEnabled, req.OsdEnabled)
	}
	a.mu.Unlock()
	if changed {
		a.publish(contract.OriginCommand, csdef.AttrAllocatedSnapshotStreams)
	}
	return nil, nil
}

// snapshotStreamDeallocate carries out SnapshotStreamDeallocate. Mirrors
// chip HandleSnapshotStreamDeallocate (:2547-2567).
func (a *AvStream) snapshotStreamDeallocate(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.SnapshotStreamDeallocateRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: SnapshotStreamDeallocate payload %T", fields)
	}
	if err := a.checkSnapshotStream(req.SnapshotStreamId, true); err != nil {
		return nil, err
	}
	if err := a.snapshot.SnapshotStreamDeallocate(ctx, req.SnapshotStreamId); err != nil {
		return nil, portError("SnapshotStreamDeallocate", err)
	}
	a.mu.Lock()
	a.snapshots = slices.DeleteFunc(a.snapshots, func(s SnapshotStream) bool { return s.SnapshotStreamId == req.SnapshotStreamId })
	a.mu.Unlock()
	a.publish(contract.OriginCommand, csdef.AttrAllocatedSnapshotStreams)
	return nil, nil
}

// setStreamPriorities carries out SetStreamPriorities. Mirrors chip
// HandleSetStreamPriorities (:2569-2614): no stream may be allocated
// (INVALID_IN_STATE); an unknown usage is INVALID_COMMAND, one outside
// SupportedStreamUsages DYNAMIC_CONSTRAINT_ERROR, a repeated one
// ALREADY_EXISTS.
func (a *AvStream) setStreamPriorities(_ context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.SetStreamPrioritiesRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: SetStreamPriorities payload %T", fields)
	}
	a.mu.Lock()
	allocated := len(a.videos) > 0 || len(a.audios) > 0 || len(a.snapshots) > 0
	a.mu.Unlock()
	if allocated { // :2576-2578
		return nil, spec.Errorf(im.StatusInvalidInState, "CameraAvStreamManagement: streams are allocated")
	}
	supported := a.supportedStreamUsages()
	for _, u := range req.StreamPriorities { // :2580-2593
		if u > csdef.StreamUsageLiveView {
			return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: unknown StreamUsage %d", u)
		}
		if !slices.Contains(supported, u) {
			return nil, spec.Errorf(im.StatusDynamicConstraintError, "CameraAvStreamManagement: StreamUsage %d is not supported", u)
		}
	}
	seen := map[StreamUsage]bool{}
	for _, u := range req.StreamPriorities { // :2600-2602, StreamPrioritiesHasDuplicates :2049-2063
		if seen[u] {
			return nil, spec.Errorf(im.StatusAlreadyExists, "CameraAvStreamManagement: StreamUsage %d is repeated", u)
		}
		seen[u] = true
	}
	a.mu.Lock()
	a.priorities = slices.Clone(req.StreamPriorities)
	a.mu.Unlock()
	a.publish(contract.OriginCommand, csdef.AttrStreamUsagePriorities)
	return nil, nil
}

// captureSnapshot carries out CaptureSnapshot. Mirrors chip
// HandleCaptureSnapshot (:2616-2669): a snapshot stream must exist, and
// the named one (NOT_FOUND); the resolution is at least 1x1
// (CONSTRAINT_ERROR); neither privacy mode may be on (INVALID_IN_STATE);
// an image larger than the bound is RESOURCE_EXHAUSTED.
func (a *AvStream) captureSnapshot(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[csdef.CaptureSnapshotRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CameraAvStreamManagement: CaptureSnapshot payload %T", fields)
	}
	a.mu.Lock()
	none := len(a.snapshots) == 0
	known := !req.SnapshotStreamId.Null && slices.ContainsFunc(a.snapshots, func(s SnapshotStream) bool { return s.SnapshotStreamId == req.SnapshotStreamId.Value })
	a.mu.Unlock()
	if none { // :2624-2627
		return nil, spec.Errorf(im.StatusNotFound, "CameraAvStreamManagement: no snapshot stream is allocated")
	}
	if !req.SnapshotStreamId.Null && !known { // :2629-2635
		return nil, spec.Errorf(im.StatusNotFound, "CameraAvStreamManagement: snapshot stream %d not found", req.SnapshotStreamId.Value)
	}
	if req.RequestedResolution.Width < 1 || req.RequestedResolution.Height < 1 { // :2637-2638
		return nil, spec.Errorf(im.StatusConstraintError, "CameraAvStreamManagement: RequestedResolution %dx%d", req.RequestedResolution.Width, req.RequestedResolution.Height)
	}
	if stored[bool](a, csdef.AttrSoftLivestreamPrivacyModeEnabled) || stored[bool](a, csdef.AttrHardPrivacyModeOn) { // :2642
		return nil, spec.Errorf(im.StatusInvalidInState, "CameraAvStreamManagement: a privacy mode is on")
	}
	var streamID *uint16
	if !req.SnapshotStreamId.Null {
		id := req.SnapshotStreamId.Value
		streamID = &id
	}
	image, err := a.capture.CaptureSnapshot(ctx, streamID, req.RequestedResolution) // :2645
	if err != nil {
		return nil, portError("CaptureSnapshot", err)
	}
	if len(image.Data) > a.maxImage { // :2652-2657
		return nil, spec.Errorf(im.StatusResourceExhausted, "CameraAvStreamManagement: image of %d bytes exceeds %d", len(image.Data), a.maxImage)
	}
	return image, nil
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (a *AvStream) MatterDataVersion() uint32 { return a.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (a *AvStream) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return a.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute.
func (a *AvStream) MatterRead(attrID uint32) (any, bool) { return a.srv.MatterRead(attrID) }

// MatterWrite applies a controller's write: the definition's checks, then
// the Viewport rule.
func (a *AvStream) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return a.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (a *AvStream) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &a.SelfHeld, cmdID, fields)
}
