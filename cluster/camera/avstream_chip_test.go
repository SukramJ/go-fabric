// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// CameraAvStreamManagement: a port of connectedhomeip
// src/app/clusters/camera-av-stream-management-server/tests/
// TestCameraAVStreamManagementCluster.cpp at 6170af84, by meaning. chip's
// ClusterTester reads, writes and invokes the cluster directly; here the
// server's MatterRead, MatterWrite and MatterInvoke do. The fixture is
// MakeInitArguments (:85-125) with InitializeCameraAVSMDefaults
// (:291-314), the mock delegate MockCameraAVStreamManagementDelegate
// (:127-282). The 57 cases of TestCameraAVStreamManagementCluster are
// ported; the seven of TestCodegenCameraAVStreamManagementMigration
// (:1801-1929) test chip's storage migration, which has no counterpart
// here (the host persists), and are left out.
package camera_test

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/im"
)

// The fixture's capabilities (:50-83).
func audioCapabilities() csdef.AudioCapabilitiesStruct {
	return csdef.AudioCapabilitiesStruct{
		MaxNumberOfChannels:  2,
		SupportedCodecs:      []csdef.AudioCodecEnum{csdef.AudioCodecOpus, csdef.AudioCodecAacLc},
		SupportedSampleRates: []uint32{48000, 32000},
		SupportedBitDepths:   []uint8{24, 32},
	}
}

func videoSensorParams() csdef.VideoSensorParamsStruct {
	hdr := uint16(30)
	return csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 120, MaxHdrfps: &hdr}
}

func rateDistortionTradeOffPoints() []csdef.RateDistortionTradeOffPointsStruct {
	return []csdef.RateDistortionTradeOffPointsStruct{{Codec: csdef.VideoCodecH264, Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MinBitRate: 10000}}
}

func snapshotCapabilities() []csdef.SnapshotCapabilitiesStruct {
	f, t := false, true
	return []csdef.SnapshotCapabilitiesStruct{
		{Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxFrameRate: 30, ImageCodec: csdef.ImageCodecJpeg, RequiresEncodedPixels: false, RequiresHardwareEncoder: &f},
		{Resolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720}, MaxFrameRate: 30, ImageCodec: csdef.ImageCodecJpeg, RequiresEncodedPixels: true, RequiresHardwareEncoder: &t},
	}
}

var supportedStreamUsages = []camera.StreamUsage{csdef.StreamUsageLiveView, csdef.StreamUsageRecording}

// dummyJPEG is the start of the mock's image (:251-260).
var dummyJPEG = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0xFF, 0xD9}

// mockStreams is MockCameraAVStreamManagementDelegate (:127-282): one
// stream of each kind at a time, the codecs, sample rates and bit depths
// of the capabilities, ids counting from 1.
type mockStreams struct {
	mu                          sync.Mutex
	videos, audios, snapshots   int
	videoN, audioN, snapshotN   int
	allocated                   []camera.StreamAllocationAction
	captureErr                  error
	image                       []byte
	videoModified, snapModified int
}

func (m *mockStreams) VideoStreamAllocate(_ context.Context, args camera.VideoStream) (uint16, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !slices.ContainsFunc(rateDistortionTradeOffPoints(), func(p csdef.RateDistortionTradeOffPointsStruct) bool { return p.Codec == args.VideoCodec }) {
		return 0, spec.Errorf(im.StatusDynamicConstraintError, "codec")
	}
	if m.videos >= 1 {
		return 0, spec.Errorf(im.StatusResourceExhausted, "one video stream")
	}
	m.videos++
	m.videoN++
	return uint16(m.videoN), nil //nolint:gosec // a test's counter
}

func (m *mockStreams) VideoStreamAllocated(_ context.Context, _ camera.VideoStream, action camera.StreamAllocationAction) {
	m.mu.Lock()
	m.allocated = append(m.allocated, action)
	m.mu.Unlock()
}

func (m *mockStreams) VideoStreamModify(context.Context, uint16, *bool, *bool) error {
	m.mu.Lock()
	m.videoModified++
	m.mu.Unlock()
	return nil
}

func (m *mockStreams) VideoStreamDeallocate(context.Context, uint16) error {
	m.mu.Lock()
	m.videos--
	m.mu.Unlock()
	return nil
}

func (m *mockStreams) AudioStreamAllocate(_ context.Context, args camera.AudioStream) (uint16, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.audios >= 1 {
		return 0, spec.Errorf(im.StatusResourceExhausted, "one audio stream")
	}
	caps := audioCapabilities()
	if !slices.Contains(caps.SupportedCodecs, args.AudioCodec) || !slices.Contains(caps.SupportedSampleRates, args.SampleRate) ||
		!slices.Contains(caps.SupportedBitDepths, args.BitDepth) {
		return 0, spec.Errorf(im.StatusDynamicConstraintError, "audio capabilities")
	}
	m.audios++
	m.audioN++
	return uint16(m.audioN), nil //nolint:gosec // a test's counter
}

func (m *mockStreams) AudioStreamDeallocate(context.Context, uint16) error {
	m.mu.Lock()
	m.audios--
	m.mu.Unlock()
	return nil
}

func (m *mockStreams) SnapshotStreamAllocate(_ context.Context, args camera.SnapshotStream) (uint16, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshots >= 1 {
		return 0, spec.Errorf(im.StatusResourceExhausted, "one snapshot stream")
	}
	if !slices.ContainsFunc(snapshotCapabilities(), func(c csdef.SnapshotCapabilitiesStruct) bool { return c.ImageCodec == args.ImageCodec }) {
		return 0, spec.Errorf(im.StatusDynamicConstraintError, "image codec")
	}
	m.snapshots++
	m.snapshotN++
	return uint16(m.snapshotN), nil //nolint:gosec // a test's counter
}

func (m *mockStreams) SnapshotStreamModify(context.Context, uint16, *bool, *bool) error {
	m.mu.Lock()
	m.snapModified++
	m.mu.Unlock()
	return nil
}

func (m *mockStreams) SnapshotStreamDeallocate(context.Context, uint16) error {
	m.mu.Lock()
	m.snapshots--
	m.mu.Unlock()
	return nil
}

func (m *mockStreams) CaptureSnapshot(_ context.Context, _ *uint16, res camera.VideoResolution) (camera.SnapshotImage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.captureErr != nil {
		return camera.SnapshotImage{}, m.captureErr
	}
	data := dummyJPEG
	if m.image != nil {
		data = m.image
	}
	return camera.SnapshotImage{Data: slices.Clone(data), ImageCodec: csdef.ImageCodecJpeg, Resolution: res}, nil
}

// chipAvStreamConfig is MakeInitArguments (:85-125) and
// InitializeCameraAVSMDefaults (:291-314).
func chipAvStreamConfig(m *mockStreams) camera.AvStreamConfig {
	return camera.AvStreamConfig{
		Features: camera.FeatureVideo | camera.FeatureAudio | camera.FeatureSnapshot | camera.FeatureSpeaker |
			camera.FeatureImageControl | camera.FeaturePrivacy | camera.FeatureWatermark | camera.FeatureHighDynamicRange |
			camera.FeatureNightVision,
		Attributes: []uint32{
			csdef.AttrHardPrivacyModeOn, csdef.AttrMicrophoneAgcEnabled, csdef.AttrImageRotation,
			csdef.AttrImageFlipHorizontal, csdef.AttrImageFlipVertical, csdef.AttrStatusLightEnabled, csdef.AttrStatusLightBrightness,
		},
		Initial: map[uint32]any{
			csdef.AttrMaxConcurrentEncoders:            uint8(1),
			csdef.AttrMaxEncodedPixelRate:              uint32(248832000),
			csdef.AttrVideoSensorParams:                videoSensorParams(),
			csdef.AttrNightVisionUsesInfrared:          false,
			csdef.AttrMinViewportResolution:            csdef.VideoResolutionStruct{Width: 640, Height: 480},
			csdef.AttrRateDistortionTradeOffPoints:     spec.List[csdef.RateDistortionTradeOffPointsStruct](rateDistortionTradeOffPoints()),
			csdef.AttrMaxContentBufferSize:             uint32(4096),
			csdef.AttrMicrophoneCapabilities:           audioCapabilities(),
			csdef.AttrSpeakerCapabilities:              audioCapabilities(),
			csdef.AttrTwoWayTalkSupport:                csdef.TwoWayTalkSupportTypeFullDuplex,
			csdef.AttrSnapshotCapabilities:             spec.List[csdef.SnapshotCapabilitiesStruct](snapshotCapabilities()),
			csdef.AttrMaxNetworkBandwidth:              uint32(128000000),
			csdef.AttrSupportedStreamUsages:            slices.Clone(supportedStreamUsages),
			csdef.AttrSoftRecordingPrivacyModeEnabled:  true,
			csdef.AttrSoftLivestreamPrivacyModeEnabled: true,
			csdef.AttrHardPrivacyModeOn:                true,
			csdef.AttrNightVision:                      csdef.TriStateAutoAuto,
			csdef.AttrViewport:                         csdef.ViewportStruct{X1: 0, Y1: 0, X2: 1920, Y2: 1080},
			csdef.AttrSpeakerMuted:                     true,
			csdef.AttrSpeakerVolumeLevel:               uint8(1),
			csdef.AttrSpeakerMinLevel:                  uint8(1),
			csdef.AttrSpeakerMaxLevel:                  uint8(254),
			csdef.AttrMicrophoneMuted:                  true,
			csdef.AttrMicrophoneVolumeLevel:            uint8(1),
			csdef.AttrMicrophoneMinLevel:               uint8(1),
			csdef.AttrMicrophoneMaxLevel:               uint8(254),
			csdef.AttrMicrophoneAgcEnabled:             true,
			csdef.AttrImageRotation:                    uint16(0),
			csdef.AttrImageFlipHorizontal:              false,
			csdef.AttrImageFlipVertical:                false,
			csdef.AttrStatusLightEnabled:               true,
			csdef.AttrStatusLightBrightness:            csdef.ThreeLevelAutoMedium,
		},
		Video: m, Audio: m, Snapshot: m, Capture: m,
	}
}

func newChipAvStream(t *testing.T, edit ...func(*camera.AvStreamConfig)) (*camera.AvStream, *mockStreams) {
	t.Helper()
	m := &mockStreams{}
	cfg := chipAvStreamConfig(m)
	for _, e := range edit {
		e(&cfg)
	}
	a, err := camera.NewAvStream(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a, m
}

// readAv reads an attribute as T.
func readStream[T any](t *testing.T, a *camera.AvStream, attrID uint32) T {
	t.Helper()
	v, ok := a.MatterRead(attrID)
	if !ok {
		t.Fatalf("attribute 0x%04X not readable", attrID)
	}
	var got T
	if !spec.Assign(&got, v) {
		t.Fatalf("attribute 0x%04X is %T", attrID, v)
	}
	return got
}

// writeStream writes v as a controller's write arrives: an integer or
// enum as the uint64 the TLV decoder yields.
func writeStream(a *camera.AvStream, attrID uint32, v any) im.StatusCode {
	if rv := reflect.ValueOf(v); rv.CanUint() {
		v = rv.Uint()
	}
	return statusOf(a.MatterWrite(context.Background(), attrID, v))
}

func invokeStream(a *camera.AvStream, cmdID uint32, req any) (any, im.StatusCode) {
	resp, err := a.MatterInvoke(context.Background(), cmdID, req)
	return resp, statusOf(err)
}

func wantCode(t *testing.T, what string, got, want im.StatusCode) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: status %s, want %s", what, got, want)
	}
}

func bptr(b bool) *bool { return &b }

func res(w, h uint16) csdef.VideoResolutionStruct { return csdef.VideoResolutionStruct{Width: w, Height: h} }

// videoAllocate is the request the command cases share (:1098-1113).
func videoAllocate() csdef.VideoStreamAllocateRequest {
	return csdef.VideoStreamAllocateRequest{
		StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264,
		MinFrameRate: 30, MaxFrameRate: 120, MinResolution: res(640, 480), MaxResolution: res(1280, 720),
		MinBitRate: 10000, MaxBitRate: 10000, KeyFrameInterval: 4, WatermarkEnabled: bptr(false),
	}
}

// audioAllocate is the request the command cases share (:978-984).
func audioAllocate() csdef.AudioStreamAllocateRequest {
	return csdef.AudioStreamAllocateRequest{
		StreamUsage: csdef.StreamUsageLiveView, AudioCodec: csdef.AudioCodecOpus,
		ChannelCount: 2, SampleRate: 48000, BitRate: 128000, BitDepth: 24,
	}
}

// snapshotAllocate is the request the command cases share (:1278-1285).
func snapshotAllocate() csdef.SnapshotStreamAllocateRequest {
	return csdef.SnapshotStreamAllocateRequest{
		ImageCodec: csdef.ImageCodecJpeg, MaxFrameRate: 30, MinResolution: res(640, 480), MaxResolution: res(1280, 720),
		Quality: 80, WatermarkEnabled: bptr(false),
	}
}

func TestChipAvStreamAttributes(t *testing.T) { //nolint:funlen // one subtest per chip TEST_F
	t.Run("TestAttributes", func(t *testing.T) { // :340-384
		a, _ := newChipAvStream(t)
		want := []uint32{
			csdef.AttrMaxConcurrentEncoders, csdef.AttrMaxEncodedPixelRate, csdef.AttrVideoSensorParams,
			csdef.AttrNightVisionUsesInfrared, csdef.AttrMinViewportResolution, csdef.AttrRateDistortionTradeOffPoints,
			csdef.AttrMaxContentBufferSize, csdef.AttrMicrophoneCapabilities, csdef.AttrSpeakerCapabilities,
			csdef.AttrTwoWayTalkSupport, csdef.AttrSnapshotCapabilities, csdef.AttrMaxNetworkBandwidth,
			csdef.AttrCurrentFrameRate, csdef.AttrHdrModeEnabled, csdef.AttrSupportedStreamUsages,
			csdef.AttrAllocatedVideoStreams, csdef.AttrAllocatedAudioStreams, csdef.AttrAllocatedSnapshotStreams,
			csdef.AttrStreamUsagePriorities, csdef.AttrSoftRecordingPrivacyModeEnabled, csdef.AttrSoftLivestreamPrivacyModeEnabled,
			csdef.AttrHardPrivacyModeOn, csdef.AttrNightVision, csdef.AttrViewport, csdef.AttrSpeakerMuted,
			csdef.AttrSpeakerVolumeLevel, csdef.AttrSpeakerMaxLevel, csdef.AttrSpeakerMinLevel, csdef.AttrMicrophoneMuted,
			csdef.AttrMicrophoneVolumeLevel, csdef.AttrMicrophoneMaxLevel, csdef.AttrMicrophoneMinLevel,
			csdef.AttrMicrophoneAgcEnabled, csdef.AttrImageRotation, csdef.AttrImageFlipHorizontal,
			csdef.AttrImageFlipVertical, csdef.AttrStatusLightEnabled, csdef.AttrStatusLightBrightness,
		}
		var got []uint32
		for _, id := range a.MatterAttributes() {
			if id < 0xFFF0 {
				got = append(got, id)
			}
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("attributes %v, want %v", got, want)
		}
	})
	t.Run("TestAcceptedCommands", func(t *testing.T) { // :386-403
		a, _ := newChipAvStream(t)
		want := []uint32{
			csdef.CmdVideoStreamAllocate, csdef.CmdVideoStreamDeallocate, csdef.CmdVideoStreamModify,
			csdef.CmdAudioStreamAllocate, csdef.CmdAudioStreamDeallocate, csdef.CmdSnapshotStreamAllocate,
			csdef.CmdSnapshotStreamDeallocate, csdef.CmdSnapshotStreamModify, csdef.CmdSetStreamPriorities, csdef.CmdCaptureSnapshot,
		}
		got := slices.Clone(a.MatterAcceptedCommands())
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("accepted %v, want %v", got, want)
		}
	})
	t.Run("TestReadClusterRevisionAttribute", func(t *testing.T) { // :405-429
		a, _ := newChipAvStream(t)
		if got := readStream[uint16](t, a, 0xFFFD); got != csdef.Revision {
			t.Fatalf("ClusterRevision %d", got)
		}
	})
	reads := []struct {
		name  string
		line  string
		check func(t *testing.T, a *camera.AvStream)
	}{
		{"TestReadMaxConcurrentEncoders", ":431-436", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint8](t, a, csdef.AttrMaxConcurrentEncoders) != 1 {
				t.Fatal("MaxConcurrentEncoders")
			}
		}},
		{"TestReadMaxEncodedPixelRate", ":438-443", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint32](t, a, csdef.AttrMaxEncodedPixelRate) != 248832000 {
				t.Fatal("MaxEncodedPixelRate")
			}
		}},
		{"TestReadVideoSensorParams", ":445-454", func(t *testing.T, a *camera.AvStream) {
			p := readStream[csdef.VideoSensorParamsStruct](t, a, csdef.AttrVideoSensorParams)
			if p.SensorWidth != 1920 || p.SensorHeight != 1080 || p.MaxFps != 120 || p.MaxHdrfps == nil || *p.MaxHdrfps != 30 {
				t.Fatalf("VideoSensorParams %+v", p)
			}
		}},
		{"TestReadNightVisionUsesInfrared", ":456-461", func(t *testing.T, a *camera.AvStream) {
			if readStream[bool](t, a, csdef.AttrNightVisionUsesInfrared) {
				t.Fatal("NightVisionUsesInfrared")
			}
		}},
		{"TestReadMinViewportResolution", ":463-469", func(t *testing.T, a *camera.AvStream) {
			if r := readStream[csdef.VideoResolutionStruct](t, a, csdef.AttrMinViewportResolution); r != res(640, 480) {
				t.Fatalf("MinViewportResolution %+v", r)
			}
		}},
		{"TestReadRateDistortionTradeOffPoints", ":471-484", func(t *testing.T, a *camera.AvStream) {
			if l := readStream[[]csdef.RateDistortionTradeOffPointsStruct](t, a, csdef.AttrRateDistortionTradeOffPoints); len(l) != 1 {
				t.Fatalf("RateDistortionTradeOffPoints %d", len(l))
			}
		}},
		{"TestReadMaxContentBufferSize", ":486-491", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint32](t, a, csdef.AttrMaxContentBufferSize) != 4096 {
				t.Fatal("MaxContentBufferSize")
			}
		}},
		{"TestReadMicrophoneCapabilities", ":493-508", func(t *testing.T, a *camera.AvStream) {
			c := readStream[csdef.AudioCapabilitiesStruct](t, a, csdef.AttrMicrophoneCapabilities)
			if c.MaxNumberOfChannels != 2 || len(c.SupportedCodecs) != 2 {
				t.Fatalf("MicrophoneCapabilities %+v", c)
			}
		}},
		{"TestReadSpeakerCapabilities", ":510-525", func(t *testing.T, a *camera.AvStream) {
			c := readStream[csdef.AudioCapabilitiesStruct](t, a, csdef.AttrSpeakerCapabilities)
			if c.MaxNumberOfChannels != 2 || len(c.SupportedCodecs) != 2 {
				t.Fatalf("SpeakerCapabilities %+v", c)
			}
		}},
		{"TestReadTwoWayTalkSupport", ":527-532", func(t *testing.T, a *camera.AvStream) {
			if readStream[csdef.TwoWayTalkSupportTypeEnum](t, a, csdef.AttrTwoWayTalkSupport) != csdef.TwoWayTalkSupportTypeFullDuplex {
				t.Fatal("TwoWayTalkSupport")
			}
		}},
		{"TestReadSnapshotCapabilities", ":534-546", func(t *testing.T, a *camera.AvStream) {
			if l := readStream[[]csdef.SnapshotCapabilitiesStruct](t, a, csdef.AttrSnapshotCapabilities); len(l) != 2 {
				t.Fatalf("SnapshotCapabilities %d", len(l))
			}
		}},
		{"TestReadMaxNetworkBandwidth", ":548-553", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint32](t, a, csdef.AttrMaxNetworkBandwidth) != 128000000 {
				t.Fatal("MaxNetworkBandwidth")
			}
		}},
		{"TestReadCurrentFrameRate", ":555-560", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint16](t, a, csdef.AttrCurrentFrameRate) != 0 {
				t.Fatal("CurrentFrameRate")
			}
		}},
		{"TestReadSupportedStreamUsages", ":577-589", func(t *testing.T, a *camera.AvStream) {
			if l := readStream[[]camera.StreamUsage](t, a, csdef.AttrSupportedStreamUsages); len(l) != 2 {
				t.Fatalf("SupportedStreamUsages %v", l)
			}
		}},
		{"TestReadAllocatedVideoStreams", ":591-603", func(t *testing.T, a *camera.AvStream) {
			if l := readStream[[]camera.VideoStream](t, a, csdef.AttrAllocatedVideoStreams); len(l) != 0 {
				t.Fatalf("AllocatedVideoStreams %v", l)
			}
		}},
		{"TestReadAllocatedAudioStreams", ":605-617", func(t *testing.T, a *camera.AvStream) {
			if l := readStream[[]camera.AudioStream](t, a, csdef.AttrAllocatedAudioStreams); len(l) != 0 {
				t.Fatalf("AllocatedAudioStreams %v", l)
			}
		}},
		{"TestReadAllocatedSnapshotStreams", ":619-631", func(t *testing.T, a *camera.AvStream) {
			if l := readStream[[]camera.SnapshotStream](t, a, csdef.AttrAllocatedSnapshotStreams); len(l) != 0 {
				t.Fatalf("AllocatedSnapshotStreams %v", l)
			}
		}},
		{"TestReadStreamUsagePriorities", ":633-645", func(t *testing.T, a *camera.AvStream) {
			if l := readStream[[]camera.StreamUsage](t, a, csdef.AttrStreamUsagePriorities); len(l) != 2 {
				t.Fatalf("StreamUsagePriorities %v", l)
			}
		}},
		{"TestReadHardPrivacyModeOn", ":683-688", func(t *testing.T, a *camera.AvStream) {
			if !readStream[bool](t, a, csdef.AttrHardPrivacyModeOn) {
				t.Fatal("HardPrivacyModeOn")
			}
		}},
		{"TestReadSpeakerMaxLevel", ":794-799", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint8](t, a, csdef.AttrSpeakerMaxLevel) != 254 {
				t.Fatal("SpeakerMaxLevel")
			}
		}},
		{"TestReadSpeakerMinLevel", ":801-806", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint8](t, a, csdef.AttrSpeakerMinLevel) != 1 {
				t.Fatal("SpeakerMinLevel")
			}
		}},
		{"TestReadMicrophoneMaxLevel", ":855-860", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint8](t, a, csdef.AttrMicrophoneMaxLevel) != 254 {
				t.Fatal("MicrophoneMaxLevel")
			}
		}},
		{"TestReadMicrophoneMinLevel", ":862-867", func(t *testing.T, a *camera.AvStream) {
			if readStream[uint8](t, a, csdef.AttrMicrophoneMinLevel) != 1 {
				t.Fatal("MicrophoneMinLevel")
			}
		}},
		{"TestReadMicrophoneAGCEnabled", ":869-874", func(t *testing.T, a *camera.AvStream) {
			if !readStream[bool](t, a, csdef.AttrMicrophoneAgcEnabled) {
				t.Fatal("MicrophoneAgcEnabled")
			}
		}},
	}
	for _, r := range reads {
		t.Run(r.name+" "+r.line, func(t *testing.T) {
			a, _ := newChipAvStream(t)
			r.check(t, a)
		})
	}
}

// toggles are the chip cases that read a value, write others and read
// them back, each write succeeding.
func TestChipAvStreamReadWrite(t *testing.T) {
	cases := []struct {
		name    string // chip TEST_F, its lines
		attrID  uint32
		initial any
		writes  []any
	}{
		{"TestReadWriteHDRModeEnabled :562-575", csdef.AttrHdrModeEnabled, false, []any{true, false}},
		{"TestReadWriteSoftRecordingPrivacyModeEnabled :647-663", csdef.AttrSoftRecordingPrivacyModeEnabled, true, []any{false, true}},
		{"TestReadWriteSoftLivestreamPrivacyModeEnabled :665-681", csdef.AttrSoftLivestreamPrivacyModeEnabled, true, []any{false, true}},
		{"TestReadWriteNightVision :690-704", csdef.AttrNightVision, csdef.TriStateAutoAuto, []any{csdef.TriStateAutoOn, csdef.TriStateAutoOff}},
		{"TestReadWriteSpeakerMuted :747-760", csdef.AttrSpeakerMuted, true, []any{false, true}},
		{"TestReadWriteMicrophoneMuted :808-821", csdef.AttrMicrophoneMuted, true, []any{false, true}},
		{"TestReadWriteImageFlipHorizontal :900-913", csdef.AttrImageFlipHorizontal, false, []any{true, false}},
		{"TestReadWriteImageFlipVertical :915-928", csdef.AttrImageFlipVertical, false, []any{true, false}},
		{"TestReadWriteStatusLightEnabled :930-943", csdef.AttrStatusLightEnabled, true, []any{false, true}},
		{"TestReadWriteStatusLightBrightness :945-968", csdef.AttrStatusLightBrightness, csdef.ThreeLevelAutoMedium,
			[]any{csdef.ThreeLevelAutoLow, csdef.ThreeLevelAutoHigh, csdef.ThreeLevelAutoAuto, csdef.ThreeLevelAutoMedium}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := newChipAvStream(t)
			same := func(want any) bool {
				got, ok := a.MatterRead(c.attrID)
				if !ok {
					return false
				}
				v, ok := spec.Convert(got, reflect.TypeOf(want))
				return ok && v.Interface() == want
			}
			if !same(c.initial) {
				t.Fatalf("initial, want %v", c.initial)
			}
			for _, w := range c.writes {
				wantCode(t, "write", writeStream(a, c.attrID, w), im.StatusSuccess)
				if !same(w) {
					t.Fatalf("after writing %v", w)
				}
			}
		})
	}
	t.Run("TestReadWriteViewport :706-745", func(t *testing.T) {
		a, _ := newChipAvStream(t)
		full := csdef.ViewportStruct{X2: 1920, Y2: 1080}
		if vp := readStream[csdef.ViewportStruct](t, a, csdef.AttrViewport); vp != full {
			t.Fatalf("Viewport %+v", vp)
		}
		wantCode(t, "too small", writeStream(a, csdef.AttrViewport, csdef.ViewportStruct{X1: 10, Y1: 20, X2: 640, Y2: 480}), im.StatusConstraintError)
		if vp := readStream[csdef.ViewportStruct](t, a, csdef.AttrViewport); vp != full {
			t.Fatalf("Viewport changed to %+v", vp)
		}
		for _, vp := range []csdef.ViewportStruct{{X2: 1280, Y2: 720}, full} {
			wantCode(t, "valid", writeStream(a, csdef.AttrViewport, vp), im.StatusSuccess)
			if got := readStream[csdef.ViewportStruct](t, a, csdef.AttrViewport); got != vp {
				t.Fatalf("Viewport %+v, want %+v", got, vp)
			}
		}
	})
	levels := []struct {
		name               string
		level, minA, maxA  uint32
		write, readBack    uint8
		outOfRange         []uint8
	}{
		{"TestReadWriteSpeakerVolumeLevel :762-792", csdef.AttrSpeakerVolumeLevel, csdef.AttrSpeakerMinLevel, csdef.AttrSpeakerMaxLevel, 100, 100, []uint8{0, 255}},
		{"TestReadWriteMicrophoneVolumeLevel :823-853", csdef.AttrMicrophoneVolumeLevel, csdef.AttrMicrophoneMinLevel, csdef.AttrMicrophoneMaxLevel, 50, 50, []uint8{0, 255}},
	}
	for _, l := range levels {
		t.Run(l.name, func(t *testing.T) {
			a, _ := newChipAvStream(t)
			if readStream[uint8](t, a, l.level) != 1 {
				t.Fatal("initial level")
			}
			wantCode(t, "write", writeStream(a, l.level, l.write), im.StatusSuccess)
			if readStream[uint8](t, a, l.level) != l.readBack {
				t.Fatal("read back")
			}
			for _, bound := range []uint32{l.minA, l.maxA} {
				b := readStream[uint8](t, a, bound)
				wantCode(t, "bound", writeStream(a, l.level, b), im.StatusSuccess)
				if readStream[uint8](t, a, l.level) != b {
					t.Fatalf("level at bound %d", b)
				}
			}
			for _, v := range l.outOfRange {
				wantCode(t, "out of range", writeStream(a, l.level, v), im.StatusConstraintError)
			}
			wantCode(t, "restore", writeStream(a, l.level, uint8(1)), im.StatusSuccess)
		})
	}
	t.Run("TestReadWriteImageRotation :876-898", func(t *testing.T) {
		a, _ := newChipAvStream(t)
		if readStream[uint16](t, a, csdef.AttrImageRotation) != 0 {
			t.Fatal("initial rotation")
		}
		for _, r := range []uint16{90, 180, 270} {
			wantCode(t, "rotate", writeStream(a, csdef.AttrImageRotation, r), im.StatusSuccess)
			if readStream[uint16](t, a, csdef.AttrImageRotation) != r {
				t.Fatalf("rotation %d", r)
			}
		}
		wantCode(t, "360", writeStream(a, csdef.AttrImageRotation, uint16(360)), im.StatusConstraintError)
		wantCode(t, "restore", writeStream(a, csdef.AttrImageRotation, uint16(0)), im.StatusSuccess)
	})
}

func TestChipAvStreamCommands(t *testing.T) { //nolint:gocognit,funlen,maintidx // one subtest per chip TEST_F
	t.Run("TestAudioStreamAllocateCommand", func(t *testing.T) { // :970-1029
		a, _ := newChipAvStream(t)
		resp, st := invokeStream(a, csdef.CmdAudioStreamAllocate, audioAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		if r, _ := resp.(csdef.AudioStreamAllocateResponse); r.AudioStreamId != 1 {
			t.Fatalf("response %+v", resp)
		}
		for _, c := range []struct {
			what string
			edit func(*csdef.AudioStreamAllocateRequest)
			want im.StatusCode
		}{
			{"channelCount 0", func(r *csdef.AudioStreamAllocateRequest) { r.ChannelCount = 0 }, im.StatusConstraintError},
			{"channelCount 9", func(r *csdef.AudioStreamAllocateRequest) { r.ChannelCount = 9 }, im.StatusConstraintError},
			{"sampleRate 0", func(r *csdef.AudioStreamAllocateRequest) { r.SampleRate = 0 }, im.StatusConstraintError},
			{"bitRate 0", func(r *csdef.AudioStreamAllocateRequest) { r.BitRate = 0 }, im.StatusConstraintError},
			{"bitDepth 12", func(r *csdef.AudioStreamAllocateRequest) { r.BitDepth = 12 }, im.StatusConstraintError},
			{"Analysis not prioritised", func(r *csdef.AudioStreamAllocateRequest) { r.StreamUsage = csdef.StreamUsageAnalysis }, im.StatusInvalidInState},
			{"second stream", func(*csdef.AudioStreamAllocateRequest) {}, im.StatusResourceExhausted},
		} {
			req := audioAllocate()
			c.edit(&req)
			_, st := invokeStream(a, csdef.CmdAudioStreamAllocate, req)
			wantCode(t, c.what, st, c.want)
		}
	})
	t.Run("TestAudioStreamAllocateCommandUnsupportedBitDepth", func(t *testing.T) { // :1031-1050
		a, _ := newChipAvStream(t)
		req := audioAllocate()
		req.BitDepth = 8
		_, st := invokeStream(a, csdef.CmdAudioStreamAllocate, req)
		wantCode(t, "bitDepth 8", st, im.StatusDynamicConstraintError)
	})
	t.Run("TestAudioStreamDeallocateCommand", func(t *testing.T) { // :1052-1090
		a, _ := newChipAvStream(t)
		resp, st := invokeStream(a, csdef.CmdAudioStreamAllocate, audioAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		id := resp.(csdef.AudioStreamAllocateResponse).AudioStreamId
		if len(a.AudioStreams()) != 1 {
			t.Fatal("one stream")
		}
		_, st = invokeStream(a, csdef.CmdAudioStreamDeallocate, csdef.AudioStreamDeallocateRequest{AudioStreamId: id})
		wantCode(t, "deallocate", st, im.StatusSuccess)
		if len(a.AudioStreams()) != 0 {
			t.Fatal("no stream")
		}
		for _, id := range []uint16{id, 999} {
			_, st = invokeStream(a, csdef.CmdAudioStreamDeallocate, csdef.AudioStreamDeallocateRequest{AudioStreamId: id})
			wantCode(t, "unknown", st, im.StatusNotFound)
		}
	})
	t.Run("TestVideoStreamAllocateCommand", func(t *testing.T) { // :1092-1158
		a, _ := newChipAvStream(t)
		req := videoAllocate()
		req.MaxFrameRate = videoSensorParams().MaxFps
		resp, st := invokeStream(a, csdef.CmdVideoStreamAllocate, req)
		wantCode(t, "allocate", st, im.StatusSuccess)
		if r, _ := resp.(csdef.VideoStreamAllocateResponse); r.VideoStreamId != 1 || len(a.VideoStreams()) != 1 {
			t.Fatalf("response %+v, streams %d", resp, len(a.VideoStreams()))
		}
		for _, c := range []struct {
			what string
			edit func(*csdef.VideoStreamAllocateRequest)
			want im.StatusCode
		}{
			{"Analysis not prioritised", func(r *csdef.VideoStreamAllocateRequest) { r.StreamUsage = csdef.StreamUsageAnalysis }, im.StatusInvalidInState},
			{"minFrameRate 121", func(r *csdef.VideoStreamAllocateRequest) { r.MinFrameRate = 121 }, im.StatusConstraintError},
			{"minResolution 1281x720", func(r *csdef.VideoStreamAllocateRequest) { r.MinResolution = res(1281, 720) }, im.StatusConstraintError},
			{"minBitRate 10001", func(r *csdef.VideoStreamAllocateRequest) { r.MinBitRate = 10001 }, im.StatusConstraintError},
			{"keyFrameInterval 65501", func(r *csdef.VideoStreamAllocateRequest) { r.KeyFrameInterval = 65501 }, im.StatusConstraintError},
			{"HEVC", func(r *csdef.VideoStreamAllocateRequest) { r.VideoCodec = csdef.VideoCodecHevc }, im.StatusDynamicConstraintError},
			{"second stream", func(*csdef.VideoStreamAllocateRequest) {}, im.StatusResourceExhausted},
		} {
			r := req
			c.edit(&r)
			_, st := invokeStream(a, csdef.CmdVideoStreamAllocate, r)
			wantCode(t, c.what, st, c.want)
		}
	})
	t.Run("TestVideoStreamDeallocateCommand", func(t *testing.T) { // :1160-1202
		a, _ := newChipAvStream(t)
		resp, st := invokeStream(a, csdef.CmdVideoStreamAllocate, videoAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		id := resp.(csdef.VideoStreamAllocateResponse).VideoStreamId
		_, st = invokeStream(a, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: id})
		wantCode(t, "deallocate", st, im.StatusSuccess)
		if len(a.VideoStreams()) != 0 {
			t.Fatal("no stream")
		}
		for _, id := range []uint16{id, 999} {
			_, st = invokeStream(a, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: id})
			wantCode(t, "unknown", st, im.StatusNotFound)
		}
	})
	t.Run("TestVideoStreamModifyCommand", func(t *testing.T) { // :1204-1269
		a, _ := newChipAvStream(t)
		resp, st := invokeStream(a, csdef.CmdVideoStreamAllocate, videoAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		id := resp.(csdef.VideoStreamAllocateResponse).VideoStreamId
		for _, wm := range []bool{true, false} {
			_, st = invokeStream(a, csdef.CmdVideoStreamModify, csdef.VideoStreamModifyRequest{VideoStreamId: id, WatermarkEnabled: bptr(wm)})
			wantCode(t, "modify", st, im.StatusSuccess)
			l := readStream[[]camera.VideoStream](t, a, csdef.AttrAllocatedVideoStreams)
			if len(l) != 1 || l[0].VideoStreamId != id || l[0].WatermarkEnabled == nil || *l[0].WatermarkEnabled != wm {
				t.Fatalf("streams %+v", l)
			}
		}
		_, st = invokeStream(a, csdef.CmdVideoStreamModify, csdef.VideoStreamModifyRequest{VideoStreamId: 999, WatermarkEnabled: bptr(false)})
		wantCode(t, "unknown", st, im.StatusNotFound)
	})
	t.Run("TestSnapshotStreamAllocateCommand", func(t *testing.T) { // :1271-1327
		a, _ := newChipAvStream(t)
		resp, st := invokeStream(a, csdef.CmdSnapshotStreamAllocate, snapshotAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		if r, _ := resp.(csdef.SnapshotStreamAllocateResponse); r.SnapshotStreamId != 1 || len(a.SnapshotStreams()) != 1 {
			t.Fatalf("response %+v", resp)
		}
		for _, c := range []struct {
			what string
			edit func(*csdef.SnapshotStreamAllocateRequest)
			want im.StatusCode
		}{
			{"maxFrameRate 0", func(r *csdef.SnapshotStreamAllocateRequest) { r.MaxFrameRate = 0 }, im.StatusConstraintError},
			{"minResolution 1281x720", func(r *csdef.SnapshotStreamAllocateRequest) { r.MinResolution = res(1281, 720) }, im.StatusConstraintError},
			{"quality 0", func(r *csdef.SnapshotStreamAllocateRequest) { r.Quality = 0 }, im.StatusConstraintError},
			{"quality 101", func(r *csdef.SnapshotStreamAllocateRequest) { r.Quality = 101 }, im.StatusConstraintError},
			{"maxFrameRate 200", func(r *csdef.SnapshotStreamAllocateRequest) { r.MaxFrameRate = 200 }, im.StatusDynamicConstraintError},
			{"second stream", func(*csdef.SnapshotStreamAllocateRequest) {}, im.StatusResourceExhausted},
		} {
			r := snapshotAllocate()
			c.edit(&r)
			_, st := invokeStream(a, csdef.CmdSnapshotStreamAllocate, r)
			wantCode(t, c.what, st, c.want)
		}
	})
	t.Run("TestSnapshotStreamDeallocateCommand", func(t *testing.T) { // :1329-1367
		a, _ := newChipAvStream(t)
		req := snapshotAllocate()
		req.MaxResolution = res(640, 480)
		resp, st := invokeStream(a, csdef.CmdSnapshotStreamAllocate, req)
		wantCode(t, "allocate", st, im.StatusSuccess)
		id := resp.(csdef.SnapshotStreamAllocateResponse).SnapshotStreamId
		_, st = invokeStream(a, csdef.CmdSnapshotStreamDeallocate, csdef.SnapshotStreamDeallocateRequest{SnapshotStreamId: id})
		wantCode(t, "deallocate", st, im.StatusSuccess)
		if len(a.SnapshotStreams()) != 0 {
			t.Fatal("no stream")
		}
		for _, id := range []uint16{id, 999} {
			_, st = invokeStream(a, csdef.CmdSnapshotStreamDeallocate, csdef.SnapshotStreamDeallocateRequest{SnapshotStreamId: id})
			wantCode(t, "unknown", st, im.StatusNotFound)
		}
	})
	t.Run("TestSnapshotStreamModifyCommand", func(t *testing.T) { // :1369-1430
		a, _ := newChipAvStream(t)
		req := snapshotAllocate()
		req.MinResolution, req.MaxResolution = res(1280, 720), res(1280, 720)
		resp, st := invokeStream(a, csdef.CmdSnapshotStreamAllocate, req)
		wantCode(t, "allocate", st, im.StatusSuccess)
		id := resp.(csdef.SnapshotStreamAllocateResponse).SnapshotStreamId
		for _, wm := range []bool{true, false} {
			_, st = invokeStream(a, csdef.CmdSnapshotStreamModify, csdef.SnapshotStreamModifyRequest{SnapshotStreamId: id, WatermarkEnabled: bptr(wm)})
			wantCode(t, "modify", st, im.StatusSuccess)
			l := readStream[[]camera.SnapshotStream](t, a, csdef.AttrAllocatedSnapshotStreams)
			if len(l) != 1 || l[0].SnapshotStreamId != id || l[0].WatermarkEnabled == nil || *l[0].WatermarkEnabled != wm {
				t.Fatalf("streams %+v", l)
			}
		}
		_, st = invokeStream(a, csdef.CmdSnapshotStreamModify, csdef.SnapshotStreamModifyRequest{SnapshotStreamId: 999, WatermarkEnabled: bptr(false)})
		wantCode(t, "unknown", st, im.StatusNotFound)
	})
	t.Run("TestSetStreamPrioritiesCommand", func(t *testing.T) { // :1432-1523
		a, _ := newChipAvStream(t)
		set := func(p ...camera.StreamUsage) im.StatusCode {
			_, st := invokeStream(a, csdef.CmdSetStreamPriorities, csdef.SetStreamPrioritiesRequest{StreamPriorities: p})
			return st
		}
		wantCode(t, "Recording, LiveView", set(csdef.StreamUsageRecording, csdef.StreamUsageLiveView), im.StatusSuccess)
		if l := readStream[[]camera.StreamUsage](t, a, csdef.AttrStreamUsagePriorities); !slices.Equal(l, []camera.StreamUsage{csdef.StreamUsageRecording, csdef.StreamUsageLiveView}) {
			t.Fatalf("priorities %v", l)
		}
		wantCode(t, "unknown 0xFF", set(0xFF), im.StatusInvalidCommand)
		wantCode(t, "repeated", set(csdef.StreamUsageLiveView, csdef.StreamUsageLiveView), im.StatusAlreadyExists)
		wantCode(t, "Analysis unsupported", set(csdef.StreamUsageAnalysis), im.StatusDynamicConstraintError)
		_, st := invokeStream(a, csdef.CmdVideoStreamAllocate, videoAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		wantCode(t, "with a stream", set(csdef.StreamUsageLiveView), im.StatusInvalidInState)
		_, st = invokeStream(a, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: 1})
		wantCode(t, "deallocate", st, im.StatusSuccess)
		if len(a.VideoStreams()) != 0 {
			t.Fatal("no stream")
		}
	})
	t.Run("TestCaptureSnapshotCommand", func(t *testing.T) { // :1525-1579
		a, _ := newChipAvStream(t)
		resp, st := invokeStream(a, csdef.CmdSnapshotStreamAllocate, snapshotAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		id := resp.(csdef.SnapshotStreamAllocateResponse).SnapshotStreamId
		privacy := func(on bool) {
			t.Helper()
			if err := a.SetAttributes(map[uint32]any{csdef.AttrSoftLivestreamPrivacyModeEnabled: on, csdef.AttrHardPrivacyModeOn: on}); err != nil {
				t.Fatal(err)
			}
		}
		privacy(false)
		req := csdef.CaptureSnapshotRequest{SnapshotStreamId: spec.Nullable[uint16]{Value: id}, RequestedResolution: res(640, 480)}
		resp, st = invokeStream(a, csdef.CmdCaptureSnapshot, req)
		wantCode(t, "capture", st, im.StatusSuccess)
		img, _ := resp.(camera.SnapshotImage)
		if img.ImageCodec != csdef.ImageCodecJpeg || img.Resolution != res(640, 480) || len(img.Data) < 2 || img.Data[0] != 0xFF || img.Data[1] != 0xD8 {
			t.Fatalf("image %+v", img)
		}
		privacy(true)
		_, st = invokeStream(a, csdef.CmdCaptureSnapshot, req)
		wantCode(t, "privacy", st, im.StatusInvalidInState)
		req.SnapshotStreamId.Value = 999
		_, st = invokeStream(a, csdef.CmdCaptureSnapshot, req)
		wantCode(t, "unknown stream", st, im.StatusNotFound)
	})
	t.Run("TestCaptureSnapshotCommand_NoStreamAllocated", func(t *testing.T) { // :1581-1598
		a, _ := newChipAvStream(t)
		if err := a.SetAttributes(map[uint32]any{csdef.AttrSoftLivestreamPrivacyModeEnabled: false, csdef.AttrHardPrivacyModeOn: false}); err != nil {
			t.Fatal(err)
		}
		_, st := invokeStream(a, csdef.CmdCaptureSnapshot, csdef.CaptureSnapshotRequest{SnapshotStreamId: spec.NullOf[uint16](), RequestedResolution: res(640, 480)})
		wantCode(t, "no stream", st, im.StatusNotFound)
	})
}

func TestChipAvStreamRefCounts(t *testing.T) { //nolint:funlen // one subtest per chip TEST_F
	refCase := func(t *testing.T, allocate func(a *camera.AvStream) uint16, count func(a *camera.AvStream) uint8,
		update func(a *camera.AvStream, id uint16, inc bool) error, underflow bool,
	) {
		t.Helper()
		a, _ := newChipAvStream(t)
		id := allocate(a)
		initial := count(a)
		if err := update(a, id, true); err != nil {
			t.Fatal(err)
		}
		if count(a) != initial+1 {
			t.Fatalf("count %d after increment", count(a))
		}
		if err := update(a, id, false); err != nil {
			t.Fatal(err)
		}
		if count(a) != initial {
			t.Fatalf("count %d after decrement", count(a))
		}
		if err := update(a, 999, true); err != camera.ErrStreamNotFound { //nolint:errorlint // the sentinel as is
			t.Fatalf("unknown stream: %v", err)
		}
		if underflow {
			if err := update(a, id, false); err != camera.ErrStreamRefCount { //nolint:errorlint // the sentinel as is
				t.Fatalf("below 0: %v", err)
			}
		}
	}
	t.Run("TestUpdateVideoStreamRefCount", func(t *testing.T) { // :1600-1654
		refCase(t, func(a *camera.AvStream) uint16 {
			resp, _ := invokeStream(a, csdef.CmdVideoStreamAllocate, videoAllocate())
			return resp.(csdef.VideoStreamAllocateResponse).VideoStreamId
		}, func(a *camera.AvStream) uint8 {
			return readStream[[]camera.VideoStream](t, a, csdef.AttrAllocatedVideoStreams)[0].ReferenceCount
		}, (*camera.AvStream).UpdateVideoStreamRefCount, true)
	})
	t.Run("TestUpdateAudioStreamRefCount", func(t *testing.T) { // :1656-1701
		refCase(t, func(a *camera.AvStream) uint16 {
			resp, _ := invokeStream(a, csdef.CmdAudioStreamAllocate, audioAllocate())
			return resp.(csdef.AudioStreamAllocateResponse).AudioStreamId
		}, func(a *camera.AvStream) uint8 {
			return readStream[[]camera.AudioStream](t, a, csdef.AttrAllocatedAudioStreams)[0].ReferenceCount
		}, (*camera.AvStream).UpdateAudioStreamRefCount, false)
	})
	t.Run("TestUpdateSnapshotStreamRefCount", func(t *testing.T) { // :1703-1746
		refCase(t, func(a *camera.AvStream) uint16 {
			resp, _ := invokeStream(a, csdef.CmdSnapshotStreamAllocate, snapshotAllocate())
			return resp.(csdef.SnapshotStreamAllocateResponse).SnapshotStreamId
		}, func(a *camera.AvStream) uint8 {
			return readStream[[]camera.SnapshotStream](t, a, csdef.AttrAllocatedSnapshotStreams)[0].ReferenceCount
		}, (*camera.AvStream).UpdateSnapshotStreamRefCount, false)
	})
	t.Run("TestReferenceCountResetOnBoot", func(t *testing.T) { // :1748-1778
		a, _ := newChipAvStream(t, func(cfg *camera.AvStreamConfig) {
			cfg.AllocatedVideoStreams = []camera.VideoStream{{
				VideoStreamId: 1, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264,
				MinFrameRate: 30, MaxFrameRate: 120, MinResolution: res(640, 480), MaxResolution: res(1280, 720),
				MinBitRate: 10000, MaxBitRate: 10000, KeyFrameInterval: 4, WatermarkEnabled: bptr(false), ReferenceCount: 5,
			}}
		})
		l := readStream[[]camera.VideoStream](t, a, csdef.AttrAllocatedVideoStreams)
		if len(l) != 1 || l[0].ReferenceCount != 0 {
			t.Fatalf("streams %+v", l)
		}
	})
}
