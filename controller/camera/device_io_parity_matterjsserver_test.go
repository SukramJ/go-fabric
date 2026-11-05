// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/im"
)

// The cases of matterjs-server's
// packages/ws-controller/test/MatterCameraDeviceIoTest.ts:28-733 at
// 0bd06f2 that need no device: the conversion of the image's AVSM state
// and the routing of the controller's invokes. The image reads run on
// fabrictest in device_fabrictest_internal_test.go.

// dioAttrs are the AVSM attributes toCameraState reads.
var dioAttrs = []uint32{
	csdef.AttrMaxConcurrentEncoders, csdef.AttrMaxEncodedPixelRate, csdef.AttrVideoSensorParams, csdef.AttrMinViewportResolution,
	csdef.AttrRateDistortionTradeOffPoints, csdef.AttrMicrophoneCapabilities, csdef.AttrTwoWayTalkSupport, csdef.AttrSnapshotCapabilities,
	csdef.AttrMaxNetworkBandwidth, csdef.AttrSupportedStreamUsages, csdef.AttrAllocatedVideoStreams, csdef.AttrAllocatedAudioStreams,
	csdef.AttrAllocatedSnapshotStreams, csdef.AttrStreamUsagePriorities, csdef.AttrSoftRecordingPrivacyModeEnabled,
	csdef.AttrSoftLivestreamPrivacyModeEnabled, csdef.AttrHardPrivacyModeOn,
}

// dioState is MINIMAL_STATE (MatterCameraDeviceIoTest.ts:17-21,
// matterjs-server at 0bd06f2) changed by edit; stated are the attributes
// the camera reports beyond it.
func dioState(edit func(*csdef.State), stated ...uint32) (st csdef.State, absent []uint32) {
	st = csdef.State{SupportedStreamUsages: []csdef.StreamUsageEnum{3}, StreamUsagePriorities: []csdef.StreamUsageEnum{3}, MaxNetworkBandwidth: 8000000}
	if edit != nil {
		edit(&st)
	}
	stated = append(stated, csdef.AttrSupportedStreamUsages, csdef.AttrStreamUsagePriorities, csdef.AttrMaxNetworkBandwidth)
	for _, a := range dioAttrs {
		if !slices.Contains(stated, a) {
			absent = append(absent, a)
		}
	}
	return st, absent
}

const dioFeatures = csdef.FeatureAudio | csdef.FeatureVideo | csdef.FeatureSnapshot

func dioConvert(edit func(*csdef.State), features csdef.Feature, stated ...uint32) *cameraState {
	st, absent := dioState(edit, stated...)
	return toCameraState(st, absent, features)
}

// TestDeviceIOToCameraStateParityMatterjsServer is describe("toCameraState")
// (MatterCameraDeviceIoTest.ts:28-322, matterjs-server at 0bd06f2).
func TestDeviceIOToCameraStateParityMatterjsServer(t *testing.T) {
	t.Run("defaults every feature-gated list to empty when the source omits it", func(t *testing.T) {
		s := dioConvert(nil, dioFeatures)
		if len(s.rateDistortionPoints)+len(s.snapshotCapabilities)+len(s.video)+len(s.audio)+len(s.snapshot) != 0 || s.sensor != nil || s.microphone != nil {
			t.Errorf("%+v", s)
		}
	})
	t.Run("copies supportedStreamUsages/streamUsagePriorities into plain mutable arrays", func(t *testing.T) {
		source := []csdef.StreamUsageEnum{1, 2, 3}
		s := dioConvert(func(st *csdef.State) { st.SupportedStreamUsages, st.StreamUsagePriorities = source, source }, dioFeatures)
		s.supportedStreamUsages[0] = 9
		if !slices.Equal(source, []csdef.StreamUsageEnum{1, 2, 3}) || !slices.Equal(s.streamUsagePriorities, []csdef.StreamUsageEnum{1, 2, 3}) {
			t.Errorf("the state shares the source: %v", source)
		}
	})
	t.Run("renames maxHdrfps to maxHdrFps and reads hdrCapable from the feature map", func(t *testing.T) {
		hdr := uint16(15)
		s := dioConvert(func(st *csdef.State) {
			st.VideoSensorParams = csdef.VideoSensorParamsStruct{SensorWidth: 2560, SensorHeight: 1440, MaxFps: 30, MaxHdrfps: &hdr}
		}, csdef.FeatureHighDynamicRange, csdef.AttrVideoSensorParams)
		if p := s.sensor; p == nil || p.width != 2560 || p.height != 1440 || p.maxFPS != 30 || p.maxHDRFPS == nil || *p.maxHDRFPS != 15 || !p.hdrCapable {
			t.Errorf("%+v", s.sensor)
		}
	})
	t.Run("carries videoSensorParams through without maxHdrfps when the device omits it", func(t *testing.T) {
		s := dioConvert(func(st *csdef.State) {
			st.VideoSensorParams = csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30}
		}, dioFeatures, csdef.AttrVideoSensorParams)
		if s.sensor == nil || s.sensor.maxHDRFPS != nil {
			t.Errorf("%+v", s.sensor)
		}
	})
	t.Run("reports hdrCapable false when the device has no HighDynamicRange feature", func(t *testing.T) {
		s := dioConvert(func(st *csdef.State) {
			st.VideoSensorParams = csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30}
		}, dioFeatures, csdef.AttrVideoSensorParams)
		if s.sensor == nil || s.sensor.hdrCapable {
			t.Errorf("%+v", s.sensor)
		}
	})
	t.Run("reports the feature map and the privacy attributes as the device states them", func(t *testing.T) {
		features := csdef.FeatureAudio | csdef.FeaturePrivacy
		s := dioConvert(func(st *csdef.State) { st.SoftLivestreamPrivacyModeEnabled = true }, features,
			csdef.AttrSoftRecordingPrivacyModeEnabled, csdef.AttrSoftLivestreamPrivacyModeEnabled, csdef.AttrHardPrivacyModeOn)
		p := s.privacy
		if s.features != features || p.softRecording == nil || *p.softRecording || p.softLivestream == nil || !*p.softLivestream || p.hard == nil || *p.hard {
			t.Errorf("%v %+v", s.features, p)
		}
	})
	t.Run("leaves a privacy switch the camera does not state absent rather than reporting it off", func(t *testing.T) {
		if p := dioConvert(nil, 0).privacy; p.softRecording != nil || p.softLivestream != nil || p.hard != nil {
			t.Errorf("%+v", p)
		}
	})
	t.Run("maps rateDistortionTradeOffPoints field names one-to-one", func(t *testing.T) {
		s := dioConvert(func(st *csdef.State) {
			st.RateDistortionTradeOffPoints = []csdef.RateDistortionTradeOffPointsStruct{{Codec: 1, Resolution: csdef.VideoResolutionStruct{Width: 1920, Height: 1080}, MinBitRate: 800000}}
		}, dioFeatures, csdef.AttrRateDistortionTradeOffPoints)
		if !slices.Equal(s.rateDistortionPoints, []rateDistortionPoint{{codec: 1, resolution: resolution{1920, 1080}, minBitRate: 800000}}) {
			t.Errorf("%+v", s.rateDistortionPoints)
		}
	})
	t.Run("defaults requiresHardwareEncoder to false when the device omits it", func(t *testing.T) {
		s := dioConvert(func(st *csdef.State) {
			st.SnapshotCapabilities = []csdef.SnapshotCapabilitiesStruct{{Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxFrameRate: 1}}
		}, dioFeatures, csdef.AttrSnapshotCapabilities)
		if s.snapshotCapabilities[0].requiresHardwareEncoder {
			t.Error("true")
		}
	})
	t.Run("carries a stated requiresHardwareEncoder through unchanged", func(t *testing.T) {
		s := dioConvert(func(st *csdef.State) {
			st.SnapshotCapabilities = []csdef.SnapshotCapabilitiesStruct{{Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxFrameRate: 1, RequiresEncodedPixels: true, RequiresHardwareEncoder: boolp(true)}}
		}, dioFeatures, csdef.AttrSnapshotCapabilities)
		if !s.snapshotCapabilities[0].requiresHardwareEncoder {
			t.Error("false")
		}
	})
	t.Run("maps an allocated video stream field-for-field", func(t *testing.T) {
		v := csdef.VideoStreamStruct{
			VideoStreamId: 4, StreamUsage: 3, VideoCodec: 1, MinFrameRate: 15, MaxFrameRate: 30,
			MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 360}, MaxResolution: csdef.VideoResolutionStruct{Width: 1920, Height: 1080},
			MinBitRate: 800000, MaxBitRate: 4000000, KeyFrameInterval: 4000, ReferenceCount: 2, WatermarkEnabled: boolp(true),
		}
		s := dioConvert(func(st *csdef.State) { st.AllocatedVideoStreams = []csdef.VideoStreamStruct{v} }, dioFeatures, csdef.AttrAllocatedVideoStreams)
		want := allocatedVideoStream{
			overlays: overlayBounds{watermark: flagOn}, videoStreamID: 4, streamUsage: 3, videoCodec: 1, minResolution: resolution{640, 360}, maxResolution: resolution{1920, 1080},
			minFrameRate: 15, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000, keyFrameInterval: 4000, referenceCount: 2,
		}
		if len(s.video) != 1 || s.video[0] != want {
			t.Errorf("%+v", s.video)
		}
	})
	t.Run("maps an allocated audio stream field-for-field", func(t *testing.T) {
		a := csdef.AudioStreamStruct{AudioStreamId: 5, StreamUsage: 3, AudioCodec: 0, ChannelCount: 2, SampleRate: 48000, BitRate: 64000, BitDepth: 16, ReferenceCount: 1}
		s := dioConvert(func(st *csdef.State) { st.AllocatedAudioStreams = []csdef.AudioStreamStruct{a} }, dioFeatures, csdef.AttrAllocatedAudioStreams)
		want := allocatedAudioStream{audioStreamID: 5, streamUsage: 3, channelCount: 2, sampleRate: 48000, bitRate: 64000, bitDepth: 16, referenceCount: 1}
		if len(s.audio) != 1 || s.audio[0] != want {
			t.Errorf("%+v", s.audio)
		}
	})
	t.Run("keeps an allocated snapshot stream's independent min and max resolution", func(t *testing.T) {
		sn := csdef.SnapshotStreamStruct{
			SnapshotStreamId: 6, FrameRate: 1, MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480},
			MaxResolution: csdef.VideoResolutionStruct{Width: 1920, Height: 1080}, Quality: 90, EncodedPixels: true, HardwareEncoder: true,
		}
		s := dioConvert(func(st *csdef.State) { st.AllocatedSnapshotStreams = []csdef.SnapshotStreamStruct{sn} }, dioFeatures, csdef.AttrAllocatedSnapshotStreams)
		if len(s.snapshot) != 1 || s.snapshot[0].minResolution != (resolution{640, 480}) || s.snapshot[0].maxResolution != (resolution{1920, 1080}) || !s.snapshot[0].hardwareEncoder {
			t.Errorf("%+v", s.snapshot)
		}
	})
	t.Run("copies microphoneCapabilities' list fields into plain mutable arrays", func(t *testing.T) {
		codecs := []csdef.AudioCodecEnum{0, 1}
		s := dioConvert(func(st *csdef.State) {
			st.MicrophoneCapabilities = csdef.AudioCapabilitiesStruct{MaxNumberOfChannels: 2, SupportedCodecs: codecs, SupportedSampleRates: []uint32{48000}, SupportedBitDepths: []uint8{16}}
		}, dioFeatures, csdef.AttrMicrophoneCapabilities)
		s.microphone.supportedCodecs[0] = 9
		if codecs[0] != 0 || s.microphone.maxNumberOfChannels != 2 || !slices.Equal(s.microphone.supportedSampleRates, []int{48000}) || !slices.Equal(s.microphone.supportedBitDepths, []int{16}) {
			t.Errorf("%+v", s.microphone)
		}
	})
	t.Run("carries twoWayTalkSupport and scalar limits through unchanged", func(t *testing.T) {
		s := dioConvert(func(st *csdef.State) {
			st.MaxConcurrentEncoders, st.MaxEncodedPixelRate, st.MaxNetworkBandwidth, st.TwoWayTalkSupport = 1, 248832000, 4000000, 2
		}, dioFeatures, csdef.AttrMaxConcurrentEncoders, csdef.AttrMaxEncodedPixelRate, csdef.AttrTwoWayTalkSupport)
		if s.maxConcurrentEncoders != stated(1) || s.maxEncodedPixelRate != stated(248832000) || s.maxNetworkBandwidth != stated(4000000) || s.twoWayTalkSupport == nil || *s.twoWayTalkSupport != 2 {
			t.Errorf("%+v", s)
		}
	})
}

// dioNodes is the clientIO's provider route with a revision-2 provider.
type dioNodes struct{ clientProviderNodes }

func (dioNodes) providerRevision(target) (revision uint16, known bool, err error) {
	return 2, true, nil
}

// dioRouted is a clientIO whose camera is send, over a session layer
// with psFixture's requestor.
func dioRouted(send func(ctx context.Context, t target, cluster, command uint32, fields spec.Encodable) (any, error)) (io *clientIO, f *psFixture) {
	f = newPSFixture()
	io = &clientIO{svc: f.svc, log: slog.New(slog.DiscardHandler), send: send}
	f.svc.io = io
	f.svc.ps.nodes = dioNodes{clientProviderNodes{io}}
	f.svc.ps.watch = nil
	return io, f
}

// TestDeviceIORoutingParityMatterjsServer is describe("MatterCameraDeviceIo.invoke
// (webrtcProvider routing)") (MatterCameraDeviceIoTest.ts:324-597,
// matterjs-server at 0bd06f2).
func TestDeviceIORoutingParityMatterjsServer(t *testing.T) {
	ctx := context.Background()
	at := target{psCamera, psEndpoint}
	usage := wpdef.StreamUsageLiveView
	offer := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}, Sdp: "v=0", StreamUsage: &usage, VideoStreams: &[]uint16{1}}
	t.Run("routes a provideOffer invoke through invokeWebRtcProviderCommand as ProvideOffer", func(t *testing.T) {
		var sent []uint32
		io, f := dioRouted(func(_ context.Context, _ target, cluster, command uint32, fields spec.Encodable) (any, error) {
			sent = append(sent, command)
			if o, ok := fields.(wpdef.ProvideOfferRequest); !ok || o.OriginatingEndpointId == nil || *o.OriginatingEndpointId != psRequestorEP || cluster != wpdef.ClusterID {
				t.Errorf("fields %+v", fields)
			}
			return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
		})
		if _, err := io.invoke(ctx, deviceCall{target: at, cluster: clusterProvider, command: callProvideOffer, fields: offer}); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(sent, []uint32{wpdef.CmdProvideOffer}) || len(f.sessions()) != 1 {
			t.Errorf("sent %v, requestor %v", sent, f.sessions())
		}
	})
	t.Run("forwards the caller's session-establishing hook, which the session registry depends on", func(t *testing.T) {
		io, _ := dioRouted(func(context.Context, target, uint32, uint32, spec.Encodable) (any, error) {
			return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
		})
		var got []uint16
		if _, err := io.invoke(ctx, deviceCall{target: at, cluster: clusterProvider, command: callProvideOffer, fields: offer, establishing: func(id uint16) { got = append(got, id) }}); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []uint16{9}) {
			t.Errorf("establishing %v", got)
		}
	})
	t.Run("routes a solicitOffer invoke through invokeWebRtcProviderCommand as SolicitOffer", func(t *testing.T) {
		var sent []uint32
		io, _ := dioRouted(func(_ context.Context, _ target, _, command uint32, _ spec.Encodable) (any, error) {
			sent = append(sent, command)
			return wpdef.SolicitOfferResponse{WebRtcSessionId: 9}, nil
		})
		if _, err := io.invoke(ctx, deviceCall{target: at, cluster: clusterProvider, command: callSolicitOffer, fields: wpdef.SolicitOfferRequest{StreamUsage: usage, VideoStreams: &[]uint16{1}}}); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(sent, []uint32{wpdef.CmdSolicitOffer}) {
			t.Errorf("sent %v", sent)
		}
	})
	endWith := func(t *testing.T, answer error) ([]wrdef.WebRTCSessionStruct, error) {
		t.Helper()
		io, f := dioRouted(func(context.Context, target, uint32, uint32, spec.Encodable) (any, error) { return nil, answer })
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id = 7 }))
		_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id, s.PeerNodeId = 7, psOtherCamera }))
		_, err := io.invoke(ctx, deviceCall{target: at, cluster: clusterProvider, command: callEndSession, fields: wpdef.EndSessionRequest{WebRtcSessionId: 7, Reason: 2}})
		return f.sessions(), err
	}
	t.Run("untracks the session after a successful endSession invoke, naming the camera it was issued by", func(t *testing.T) {
		left, err := endWith(t, nil)
		if err != nil || len(left) != 1 || left[0].PeerNodeId != psOtherCamera {
			t.Errorf("%v %+v", err, left)
		}
	})
	t.Run("untracks the session when the device answers NotFound, as the manager does", func(t *testing.T) {
		left, err := endWith(t, statusErr(im.StatusNotFound))
		if !statusIs(err, im.StatusNotFound) || len(left) != 1 {
			t.Errorf("%v %+v", err, left)
		}
	})
	t.Run("keeps tracking the session when endSession fails with any other status", func(t *testing.T) {
		left, err := endWith(t, statusErr(im.StatusBusy))
		if !statusIs(err, im.StatusBusy) || len(left) != 2 {
			t.Errorf("%v %+v", err, left)
		}
	})
}

// TestDeviceIOStreamListReadParityMatterjsServer is describe("MatterCameraDeviceIo.invoke
// (stream list re-read)") (MatterCameraDeviceIoTest.ts:599-732,
// matterjs-server at 0bd06f2).
func TestDeviceIOStreamListReadParityMatterjsServer(t *testing.T) {
	ctx := context.Background()
	at := target{psCamera, 2}
	type event struct {
		what string
		attr uint32
	}
	routed := func(answer, readErr error) (*clientIO, *[]event) {
		var events []event
		io, _ := dioRouted(func(context.Context, target, uint32, uint32, spec.Encodable) (any, error) {
			events = append(events, event{what: "invoke"})
			if answer != nil {
				return nil, answer
			}
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 1}, nil
		})
		io.reread = func(_ context.Context, rt target, attr uint32) error {
			if rt != at {
				t.Errorf("read target %+v", rt)
			}
			events = append(events, event{what: "read", attr: attr})
			return readErr
		}
		return io, &events
	}
	for _, c := range []struct {
		command string
		fields  spec.Encodable
		attr    uint32
	}{
		{cmdVideoStreamAllocate, csdef.VideoStreamAllocateRequest{}, csdef.AttrAllocatedVideoStreams},
		{cmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{}, csdef.AttrAllocatedVideoStreams},
		{cmdAudioStreamAllocate, csdef.AudioStreamAllocateRequest{}, csdef.AttrAllocatedAudioStreams},
		{cmdAudioStreamDeallocate, csdef.AudioStreamDeallocateRequest{}, csdef.AttrAllocatedAudioStreams},
		{cmdSnapshotStreamAllocate, csdef.SnapshotStreamAllocateRequest{}, csdef.AttrAllocatedSnapshotStreams},
		{cmdSnapshotStreamDeallocate, csdef.SnapshotStreamDeallocateRequest{}, csdef.AttrAllocatedSnapshotStreams},
	} {
		t.Run("re-reads the stream list the camera changed after ${command} succeeds ("+c.command+")", func(t *testing.T) {
			io, events := routed(nil, nil)
			if _, err := io.invoke(ctx, deviceCall{target: at, cluster: clusterAVSM, command: c.command, fields: c.fields}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(*events, []event{{what: "invoke"}, {what: "read", attr: c.attr}}) {
				t.Errorf("events %+v", *events)
			}
		})
	}
	t.Run("does not read after a command that leaves the stream lists alone", func(t *testing.T) {
		io, events := routed(nil, nil)
		if _, err := io.invoke(ctx, deviceCall{target: at, cluster: clusterAVSM, command: cmdCaptureSnapshot, fields: csdef.CaptureSnapshotRequest{}}); err != nil {
			t.Fatal(err)
		}
		if len(*events) != 1 {
			t.Errorf("events %+v", *events)
		}
	})
	t.Run("does not read when the camera refuses the command", func(t *testing.T) {
		io, events := routed(statusErr(im.StatusResourceExhausted), nil)
		if _, err := io.invoke(ctx, deviceCall{target: at, cluster: clusterAVSM, command: cmdVideoStreamAllocate, fields: csdef.VideoStreamAllocateRequest{}}); !statusIs(err, im.StatusResourceExhausted) || len(*events) != 1 {
			t.Errorf("%v %+v", err, *events)
		}
	})
	t.Run("returns the camera's answer when the re-read fails", func(t *testing.T) {
		io, _ := routed(nil, errors.New("read failed"))
		resp, err := io.invoke(ctx, deviceCall{target: at, cluster: clusterAVSM, command: cmdVideoStreamAllocate, fields: csdef.VideoStreamAllocateRequest{}})
		if r, ok := resp.(csdef.VideoStreamAllocateResponse); err != nil || !ok || r.VideoStreamId != 1 {
			t.Errorf("%+v %v", resp, err)
		}
	})
}
