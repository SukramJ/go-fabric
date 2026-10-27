// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The CameraAvStreamManagement rules no chip case reaches, each against
// the CameraAVStreamManagementCluster.cpp line it holds.
package camera_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/im"
)

func TestAvStreamConfigRules(t *testing.T) {
	m := &mockStreams{}
	base := camera.AvStreamConfig{Video: m, Audio: m, Snapshot: m, Capture: m}
	cases := []struct {
		name  string
		edit  func(*camera.AvStreamConfig)
		want  error
	}{
		{"none of VDO ADO SNP :85-90", func(c *camera.AvStreamConfig) { c.Features = camera.FeaturePrivacy }, camera.ErrAvStreamConfig},
		{"WMARK without VDO or SNP :93-102", func(c *camera.AvStreamConfig) { c.Features = camera.FeatureAudio | camera.FeatureWatermark }, camera.ErrAvStreamConfig},
		{"SPKR without ADO :104-111", func(c *camera.AvStreamConfig) { c.Features = camera.FeatureVideo | camera.FeatureSpeaker }, camera.ErrAvStreamConfig},
		{"NightVisionIllum without NV :114-121", func(c *camera.AvStreamConfig) {
			c.Features, c.Attributes = camera.FeatureVideo, []uint32{csdef.AttrNightVisionIllum}
		}, camera.ErrAvStreamConfig},
		{"MicrophoneAgcEnabled without ADO :123-129", func(c *camera.AvStreamConfig) {
			c.Features, c.Attributes = camera.FeatureVideo, []uint32{csdef.AttrMicrophoneAgcEnabled}
		}, camera.ErrAvStreamConfig},
		{"an image flip without ICTL :133-142", func(c *camera.AvStreamConfig) {
			c.Features, c.Attributes = camera.FeatureVideo, []uint32{csdef.AttrImageFlipVertical}
		}, camera.ErrAvStreamConfig},
		{"ICTL without an image attribute :144-153", func(c *camera.AvStreamConfig) {
			c.Features = camera.FeatureVideo | camera.FeatureImageControl
		}, camera.ErrAvStreamConfig},
		{"an attribute chip does not take", func(c *camera.AvStreamConfig) {
			c.Features, c.Attributes = camera.FeatureVideo, []uint32{csdef.AttrImageRotationDiscreteAngles}
		}, camera.ErrAvStreamConfig},
		{"VDO without its port", func(c *camera.AvStreamConfig) { c.Features, c.Video = camera.FeatureVideo, nil }, camera.ErrNoStreamPort},
		{"SNP without the capture", func(c *camera.AvStreamConfig) { c.Features, c.Capture = camera.FeatureSnapshot, nil }, camera.ErrNoStreamPort},
	}
	for _, c := range cases {
		cfg := base
		c.edit(&cfg)
		if _, err := camera.NewAvStream(cfg); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// A persisted stream the definition refuses fails the construction.
	cfg := base
	cfg.Features = camera.FeatureAudio
	cfg.AllocatedAudioStreams = []camera.AudioStream{{AudioStreamId: 1, ChannelCount: 0, SampleRate: 1, BitRate: 1, BitDepth: 8}}
	if _, err := camera.NewAvStream(cfg); !errors.Is(err, camera.ErrAvStreamConfig) {
		t.Errorf("a stream of 0 channels: %v", err)
	}
}

func TestAvStreamCommandRules(t *testing.T) { //nolint:funlen // one rule per block
	t.Run("the request's optional fields follow WMARK and OSD :2076-2081, :2191-2218", func(t *testing.T) {
		a, _ := newChipAvStream(t)
		req := videoAllocate()
		req.WatermarkEnabled = nil
		_, st := invokeStream(a, csdef.CmdVideoStreamAllocate, req)
		wantCode(t, "no WatermarkEnabled with WMARK", st, im.StatusInvalidCommand)
		req = videoAllocate()
		req.OsdEnabled = bptr(true)
		_, st = invokeStream(a, csdef.CmdVideoStreamAllocate, req)
		wantCode(t, "OSDEnabled without OSD", st, im.StatusInvalidCommand)
		_, st = invokeStream(a, csdef.CmdVideoStreamAllocate, videoAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		_, st = invokeStream(a, csdef.CmdVideoStreamModify, csdef.VideoStreamModifyRequest{VideoStreamId: 1, OsdEnabled: bptr(true)})
		wantCode(t, "modify OSD without OSD", st, im.StatusInvalidCommand)
		_, st = invokeStream(a, csdef.CmdVideoStreamModify, csdef.VideoStreamModifyRequest{VideoStreamId: 1})
		wantCode(t, "modify nothing", st, im.StatusInvalidCommand)
		_, st = invokeStream(a, csdef.CmdSnapshotStreamModify, csdef.SnapshotStreamModifyRequest{SnapshotStreamId: 1})
		wantCode(t, "snapshot modify nothing", st, im.StatusInvalidCommand)
	})
	t.Run("the request's constraints :2083-2116, :2265-2293, :2381-2408", func(t *testing.T) {
		a, _ := newChipAvStream(t)
		for _, c := range []struct {
			what string
			cmd  uint32
			req  any
		}{
			{"video Internal", csdef.CmdVideoStreamAllocate, func() any { r := videoAllocate(); r.StreamUsage = csdef.StreamUsageInternal; return r }()},
			{"video codec 9", csdef.CmdVideoStreamAllocate, func() any { r := videoAllocate(); r.VideoCodec = 9; return r }()},
			{"video max resolution 0", csdef.CmdVideoStreamAllocate, func() any { r := videoAllocate(); r.MaxResolution = res(0, 720); return r }()},
			{"audio Internal", csdef.CmdAudioStreamAllocate, func() any { r := audioAllocate(); r.StreamUsage = csdef.StreamUsageInternal; return r }()},
			{"audio codec 9", csdef.CmdAudioStreamAllocate, func() any { r := audioAllocate(); r.AudioCodec = 9; return r }()},
			{"image codec 9", csdef.CmdSnapshotStreamAllocate, func() any { r := snapshotAllocate(); r.ImageCodec = 9; return r }()},
			{"snapshot min resolution 0", csdef.CmdSnapshotStreamAllocate, func() any { r := snapshotAllocate(); r.MinResolution = res(640, 0); return r }()},
		} {
			_, st := invokeStream(a, c.cmd, c.req)
			wantCode(t, c.what, st, im.StatusConstraintError)
		}
		_, st := invokeStream(a, csdef.CmdCaptureSnapshot, struct{}{})
		wantCode(t, "a foreign payload", st, im.StatusInvalidCommand)
	})
	t.Run("a reused video stream narrows to the intersection :2160-2171, :330-368", func(t *testing.T) {
		a, m := newChipAvStream(t)
		m.videos = -5 // room for more streams
		_, st := invokeStream(a, csdef.CmdVideoStreamAllocate, videoAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		req := videoAllocate()
		req.MinFrameRate, req.MaxFrameRate = 60, 90
		got, ok := a.ReusableVideoStreamID(camera.VideoStream{
			VideoCodec: req.VideoCodec, MinFrameRate: 60, MaxFrameRate: 90, MinResolution: req.MinResolution,
			MaxResolution: req.MaxResolution, MinBitRate: req.MinBitRate, MaxBitRate: req.MaxBitRate, KeyFrameInterval: req.KeyFrameInterval,
		})
		if !ok || got != 1 {
			t.Fatalf("reusable %d %v", got, ok)
		}
		if _, ok := a.ReusableVideoStreamID(camera.VideoStream{VideoCodec: csdef.VideoCodecHevc}); ok {
			t.Fatal("an HEVC stream is reusable")
		}
		m.videoN = 0 // the port hands out id 1 again: a reuse
		_, st = invokeStream(a, csdef.CmdVideoStreamAllocate, req)
		wantCode(t, "reuse", st, im.StatusSuccess)
		if s := a.VideoStreams(); len(s) != 1 || s[0].MinFrameRate != 60 || s[0].MaxFrameRate != 90 {
			t.Fatalf("streams %+v", s)
		}
		m.videoN = 0
		_, st = invokeStream(a, csdef.CmdVideoStreamAllocate, req)
		wantCode(t, "reuse unchanged", st, im.StatusSuccess)
		want := []camera.StreamAllocationAction{camera.StreamNewAllocation, camera.StreamModification, camera.StreamReuse}
		if len(m.allocated) != 3 || m.allocated[0] != want[0] || m.allocated[1] != want[1] || m.allocated[2] != want[2] {
			t.Fatalf("actions %v", m.allocated)
		}
	})
	t.Run("a reused snapshot stream narrows its resolution :2479-2484, :441-474", func(t *testing.T) {
		a, m := newChipAvStream(t)
		m.snapshots = -5
		_, st := invokeStream(a, csdef.CmdSnapshotStreamAllocate, snapshotAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		if id, ok := a.ReusableSnapshotStreamID(camera.SnapshotStream{ImageCodec: csdef.ImageCodecJpeg, Quality: 80, FrameRate: 30, MinResolution: res(640, 480), MaxResolution: res(1280, 720)}); !ok || id != 1 {
			t.Fatalf("reusable %d %v", id, ok)
		}
		if _, ok := a.ReusableSnapshotStreamID(camera.SnapshotStream{ImageCodec: csdef.ImageCodecJpeg, Quality: 50}); ok {
			t.Fatal("another quality is reusable")
		}
		req := snapshotAllocate()
		req.MaxResolution = res(1000, 700)
		m.snapshotN = 0
		_, st = invokeStream(a, csdef.CmdSnapshotStreamAllocate, req)
		wantCode(t, "reuse", st, im.StatusSuccess)
		if s := a.SnapshotStreams(); len(s) != 1 || s[0].MaxResolution != res(1000, 700) {
			t.Fatalf("streams %+v", s)
		}
	})
	t.Run("a reused audio stream is left as it is :2320-2331", func(t *testing.T) {
		a, m := newChipAvStream(t)
		m.audios = -5
		_, st := invokeStream(a, csdef.CmdAudioStreamAllocate, audioAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		m.audioN = 0
		_, st = invokeStream(a, csdef.CmdAudioStreamAllocate, audioAllocate())
		wantCode(t, "reuse", st, im.StatusSuccess)
		if len(a.AudioStreams()) != 1 {
			t.Fatalf("streams %+v", a.AudioStreams())
		}
	})
	t.Run("a stream in use or Internal is not deallocated (CameraAVStreamManagementCluster.h:793-821)", func(t *testing.T) {
		a, _ := newChipAvStream(t, func(cfg *camera.AvStreamConfig) {
			cfg.AllocatedAudioStreams = []camera.AudioStream{{AudioStreamId: 7, StreamUsage: csdef.StreamUsageInternal, ChannelCount: 1, SampleRate: 1, BitRate: 1, BitDepth: 8}}
			cfg.AllocatedVideoStreams = []camera.VideoStream{{
				VideoStreamId: 8, StreamUsage: csdef.StreamUsageInternal, MinFrameRate: 1, MaxFrameRate: 1,
				MinResolution: res(1, 1), MaxResolution: res(1, 1), MinBitRate: 1, MaxBitRate: 1, WatermarkEnabled: bptr(false),
			}}
		})
		_, st := invokeStream(a, csdef.CmdAudioStreamDeallocate, csdef.AudioStreamDeallocateRequest{AudioStreamId: 7})
		wantCode(t, "Internal audio", st, im.StatusDynamicConstraintError)
		_, st = invokeStream(a, csdef.CmdVideoStreamModify, csdef.VideoStreamModifyRequest{VideoStreamId: 8, WatermarkEnabled: bptr(true)})
		wantCode(t, "Internal video", st, im.StatusDynamicConstraintError)
		if err := a.UpdateVideoStreamRefCount(8, true); err != nil {
			t.Fatal(err)
		}
		_, st = invokeStream(a, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: 8})
		wantCode(t, "video in use", st, im.StatusInvalidInState)
		_, st = invokeStream(a, csdef.CmdSnapshotStreamAllocate, snapshotAllocate())
		wantCode(t, "snapshot", st, im.StatusSuccess)
		if err := a.UpdateSnapshotStreamRefCount(1, true); err != nil {
			t.Fatal(err)
		}
		_, st = invokeStream(a, csdef.CmdSnapshotStreamDeallocate, csdef.SnapshotStreamDeallocateRequest{SnapshotStreamId: 1})
		wantCode(t, "snapshot in use", st, im.StatusInvalidInState)
	})
	t.Run("a snapshot stream on a video stream is not modifiable (CameraAVStreamManagementCluster.h:827-855)", func(t *testing.T) {
		a, _ := newChipAvStream(t)
		_, st := invokeStream(a, csdef.CmdSnapshotStreamAllocate, snapshotAllocate()) // matches the 640x480 entry, RequiresHardwareEncoder false
		wantCode(t, "allocate", st, im.StatusSuccess)
		_, st = invokeStream(a, csdef.CmdSnapshotStreamModify, csdef.SnapshotStreamModifyRequest{SnapshotStreamId: 1, WatermarkEnabled: bptr(true)})
		wantCode(t, "modify", st, im.StatusInvalidInState)
		if err := a.SetAttributes(map[uint32]any{csdef.AttrSnapshotCapabilities: spec.List[csdef.SnapshotCapabilitiesStruct]{}}); err != nil {
			t.Fatal(err)
		}
		_, st = invokeStream(a, csdef.CmdSnapshotStreamModify, csdef.SnapshotStreamModifyRequest{SnapshotStreamId: 1, WatermarkEnabled: bptr(true)})
		wantCode(t, "no capability", st, im.StatusInvalidInState)
	})
	t.Run("the capture's bound and the port's refusal :2645-2657", func(t *testing.T) {
		a, m := newChipAvStream(t, func(cfg *camera.AvStreamConfig) { cfg.MaxSnapshotSize = 4 })
		_, st := invokeStream(a, csdef.CmdSnapshotStreamAllocate, snapshotAllocate())
		wantCode(t, "allocate", st, im.StatusSuccess)
		if err := a.SetAttributes(map[uint32]any{csdef.AttrSoftLivestreamPrivacyModeEnabled: false, csdef.AttrHardPrivacyModeOn: false}); err != nil {
			t.Fatal(err)
		}
		req := csdef.CaptureSnapshotRequest{SnapshotStreamId: spec.NullOf[uint16](), RequestedResolution: res(640, 480)}
		_, st = invokeStream(a, csdef.CmdCaptureSnapshot, req)
		wantCode(t, "too large", st, im.StatusResourceExhausted)
		m.captureErr = errors.New("sensor gone")
		_, st = invokeStream(a, csdef.CmdCaptureSnapshot, req)
		wantCode(t, "refused", st, im.StatusFailure)
		req.RequestedResolution = res(0, 480)
		_, st = invokeStream(a, csdef.CmdCaptureSnapshot, req)
		wantCode(t, "resolution 0", st, im.StatusConstraintError)
	})
}

func TestAvStreamHostRules(t *testing.T) {
	a, _ := newChipAvStream(t)
	if err := a.SetAttributes(map[uint32]any{csdef.AttrAllocatedVideoStreams: spec.List[camera.VideoStream]{}}); !errors.Is(err, spec.ErrNotHeld) {
		t.Fatalf("streams set by the host: %v", err)
	}
	// The aspect ratio (:1150-1167): 1280x960 is 4:3 on a 16:9 sensor.
	wantCode(t, "4:3", writeStream(a, csdef.AttrViewport, csdef.ViewportStruct{X2: 1280, Y2: 960}), im.StatusConstraintError)
	if err := a.SetAttributes(map[uint32]any{csdef.AttrViewport: csdef.ViewportStruct{X2: 1280, Y2: 960}}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Fatalf("host 4:3: %v", err)
	}
	// A new sensor in the same change is the one the rule reads.
	if err := a.SetAttributes(map[uint32]any{
		csdef.AttrVideoSensorParams: csdef.VideoSensorParamsStruct{SensorWidth: 1280, SensorHeight: 960, MaxFps: 30},
		csdef.AttrViewport:          csdef.ViewportStruct{X2: 1280, Y2: 960},
	}); err != nil {
		t.Fatalf("4:3 sensor: %v", err)
	}
	if err := a.SetAttributes(map[uint32]any{csdef.AttrStreamUsagePriorities: []camera.StreamUsage{csdef.StreamUsageRecording}}); err != nil {
		t.Fatal(err)
	}
	if p := a.StreamUsagePriorities(); len(p) != 1 || p[0] != csdef.StreamUsageRecording {
		t.Fatalf("priorities %v", p)
	}
	_, st := invokeStream(a, csdef.CmdVideoStreamAllocate, videoAllocate())
	wantCode(t, "LiveView no longer ranked", st, im.StatusInvalidInState)
	// The encoder budget (:2717-2756): one encoder, 248832000 pixels/s.
	if !a.ResourceAvailable(1920*1080*30, true) {
		t.Fatal("a first stream")
	}
	if a.ResourceAvailable(248832001, false) {
		t.Fatal("beyond MaxEncodedPixelRate")
	}
	if err := a.UpdateAudioStreamRefCount(1, true); !errors.Is(err, camera.ErrStreamNotFound) {
		t.Fatalf("no audio stream: %v", err)
	}
}
