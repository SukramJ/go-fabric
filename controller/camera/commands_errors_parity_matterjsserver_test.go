// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The Matter rules of matterjs-server's CameraCommandsTest.ts and
// CameraErrorsTest.ts at 0bd06f2, ported by meaning onto the typed API: the
// JSON argument parsing and the wire rendering are the host's.

// cmdErrNode is a commissioned node as its store holds it: the record
// cluster (0xFFFFFFFE) on endpoint 0 with the peer address {1, 5} as field
// 0, the persisted layout of controller/client/record.go:28-30.
func cmdErrNode(t *testing.T) *client.Node {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutUint(tlv.ContextTag(0), 1)
	e.PutUint(tlv.ContextTag(1), tNode)
	_ = e.EndContainer()
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	st := &client.MemoryStore{}
	if err := st.Save("camera", 0, 0xFFFF_FFFE, map[uint32][]byte{0: b}, nil); err != nil {
		t.Fatal(err)
	}
	n, err := client.New(client.Config{ID: "camera", Store: st, Interaction: &interaction.Interaction{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := n.PeerAddress(); !ok {
		t.Fatal("the stored node is not commissioned")
	}
	return n
}

// cmdErrStart converts a LiveView start on the node edit changes.
func cmdErrStart(t *testing.T, edit func(*StartRequest)) (startArgs, error) {
	t.Helper()
	req := StartRequest{Node: cmdErrNode(t), Endpoint: tEndpoint, Usage: csdef.StreamUsageLiveView}
	if edit != nil {
		edit(&req)
	}
	return req.args()
}

// cmdErrStarted is cmdErrStart that must succeed.
func cmdErrStarted(t *testing.T, edit func(*StartRequest)) startArgs {
	t.Helper()
	a, err := cmdErrStart(t, edit)
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	return a
}

// cmdErrRefused fails unless err is an argument refusal (error 8).
func cmdErrRefused(t *testing.T, err error, what string) {
	t.Helper()
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("%s: want ErrInvalidArgument, got %v", what, err)
	}
}

func cmdErrStr(s string) *string { return &s }

func cmdErrRes(w, h uint16) *csdef.VideoResolutionStruct {
	return &csdef.VideoResolutionStruct{Width: w, Height: h}
}

// The parseStartStreamArgs cases whose meaning is a Matter rule
// (CameraCommandsTest.ts:159-695, matterjs-server at 0bd06f2).
func TestCommandsErrorsParseStartStreamArgsParityMatterjsServer(t *testing.T) { //nolint:maintidx // one subtest per ported case
	t.Run("reads snake_case resolution ranges into internal hints", func(t *testing.T) {
		a := cmdErrStarted(t, func(r *StartRequest) {
			r.Video = &VideoBounds{MinResolution: cmdErrRes(1280, 720), MaxResolution: cmdErrRes(1920, 1080), MaxFrameRate: ptr[uint16](15)}
		})
		want := videoRangeBounds{minResolution: resolution{1280, 720}, maxResolution: resolution{1920, 1080}, maxFrameRate: 15}
		if a.videoState != trackDemanded || a.video == nil || a.video.videoRangeBounds != want || a.video.overlays != (overlayBounds{}) {
			t.Fatalf("video = %v %+v", a.videoState, a.video)
		}
	})
	t.Run("maps the stream usage name to its enum value", func(t *testing.T) {
		if csdef.StreamUsageLiveView != 3 {
			t.Fatalf("LiveView = %d", csdef.StreamUsageLiveView)
		}
		if a := cmdErrStarted(t, nil); a.usage != csdef.StreamUsageLiveView {
			t.Fatalf("usage = %d", a.usage)
		}
	})
	t.Run("maps Recording and Analysis to their enum values", func(t *testing.T) {
		for usage, want := range map[csdef.StreamUsageEnum]csdef.StreamUsageEnum{csdef.StreamUsageRecording: 1, csdef.StreamUsageAnalysis: 2} {
			if usage != want {
				t.Fatalf("usage %d, want %d", usage, want)
			}
			if a := cmdErrStarted(t, func(r *StartRequest) { r.Usage = usage }); a.usage != want {
				t.Fatalf("usage = %d, want %d", a.usage, want)
			}
		}
	})
	t.Run("rejects an unknown stream usage", func(t *testing.T) {
		_, err := cmdErrStart(t, func(r *StartRequest) { r.Usage = 7 })
		cmdErrRefused(t, err, "usage 7")
	})
	t.Run("rejects the device-only Internal stream usage", func(t *testing.T) {
		_, err := cmdErrStart(t, func(r *StartRequest) { r.Usage = csdef.StreamUsageInternal })
		cmdErrRefused(t, err, "Internal")
	})
	t.Run("carries an explicit false through as no track", func(t *testing.T) {
		if a := cmdErrStarted(t, func(r *StartRequest) { r.DeclineAudio = true }); a.audioState != trackDeclined || a.audio != nil {
			t.Fatalf("audio = %v %+v", a.audioState, a.audio)
		}
		if a := cmdErrStarted(t, func(r *StartRequest) { r.DeclineVideo = true }); a.videoState != trackDeclined || a.video != nil {
			t.Fatalf("video = %v %+v", a.videoState, a.video)
		}
	})
	t.Run("leaves video/audio undefined when omitted", func(t *testing.T) {
		a := cmdErrStarted(t, nil)
		if a.videoState != trackDeferred || a.video != nil || a.audioState != trackDeferred || a.audio != nil {
			t.Fatalf("video %v %+v, audio %v %+v", a.videoState, a.video, a.audioState, a.audio)
		}
	})
	t.Run("reads audio hints from snake_case fields", func(t *testing.T) {
		a := cmdErrStarted(t, func(r *StartRequest) {
			r.Audio = &AudioBounds{Codecs: []string{"OPUS"}, ChannelCount: ptr[uint8](1), SampleRate: ptr[uint32](16000), BitRate: ptr[uint32](32000)}
		})
		want := &audioHints{codecs: []string{"OPUS"}, channelCount: 1, sampleRate: 16000, bitRate: 32000}
		if a.audioState != trackDemanded || !reflect.DeepEqual(a.audio, want) {
			t.Fatalf("audio = %v %+v", a.audioState, a.audio)
		}
	})
	t.Run("passes sdp, ice and metadata fields through, translating ice_servers to the struct", func(t *testing.T) {
		servers := []wpdef.ICEServerStruct{{Urls: []string{"stun:example.com"}}}
		a := cmdErrStarted(t, func(r *StartRequest) {
			r.Offer, r.ICEServers, r.ICETransportPolicy, r.MetadataEnabled = cmdErrStr("v=0"), servers, "relay", true
		})
		if a.sdp == nil || *a.sdp != "v=0" || a.iceServers == nil || !reflect.DeepEqual(*a.iceServers, servers) ||
			a.icePolicy == nil || *a.icePolicy != "relay" || !a.metadata {
			t.Fatalf("args = %+v", a)
		}
	})
	// The manager's default (true, matterjs-server CameraStreamManager.ts:1107 at 0bd06f2)
	// is resolved by the conversion here; nil is the unstated request.
	t.Run("reads allow_eviction, and leaves it unstated when the caller says nothing", func(t *testing.T) {
		no := false
		if a := cmdErrStarted(t, func(r *StartRequest) { r.AllowEviction = &no }); a.allowEviction {
			t.Fatal("allow_eviction false read as true")
		}
		if a := cmdErrStarted(t, nil); !a.allowEviction {
			t.Fatal("unstated allow_eviction did not take the manager's default")
		}
	})
	// Unstated is Go's false.
	t.Run("reads talkback, and leaves it unstated when the caller says nothing", func(t *testing.T) {
		for _, talkback := range []bool{true, false} {
			a := cmdErrStarted(t, func(r *StartRequest) { r.Offer, r.Talkback = cmdErrStr("v=0"), talkback })
			if a.talkback != talkback {
				t.Fatalf("talkback = %v, want %v", a.talkback, talkback)
			}
		}
	})
	t.Run("refuses talkback true without sdp, and accepts talkback false without it", func(t *testing.T) {
		_, err := cmdErrStart(t, func(r *StartRequest) { r.Talkback = true })
		cmdErrRefused(t, err, "talkback without sdp")
		if !strings.HasPrefix(err.Error(), "talkback: true requires sdp") {
			t.Fatalf("message = %q", err)
		}
		if _, err := cmdErrStart(t, func(r *StartRequest) { r.Talkback = false }); err != nil {
			t.Fatalf("talkback false without sdp: %v", err)
		}
	})
	t.Run("reads the overlay hints, and leaves each unstated when the caller says nothing", func(t *testing.T) {
		on, off := true, false
		a := cmdErrStarted(t, func(r *StartRequest) { r.Video = &VideoBounds{Overlays: Overlays{Watermark: &on, OSD: &off}} })
		if a.video == nil || a.video.overlays != (overlayBounds{watermark: flagOn, osd: flagOff}) || a.video.videoRangeBounds != (videoRangeBounds{}) {
			t.Fatalf("video = %+v", a.video)
		}
		a = cmdErrStarted(t, func(r *StartRequest) { r.Video = &VideoBounds{} })
		if a.video == nil || a.video.overlays != (overlayBounds{watermark: flagUnstated, osd: flagUnstated}) {
			t.Fatalf("video = %+v", a.video)
		}
	})
	t.Run("keeps a urls list, the credentials and the caid on the struct", func(t *testing.T) {
		caid := uint16(7)
		servers := []wpdef.ICEServerStruct{{Urls: []string{"turn:a.example:3478", "turns:a.example:5349"}, Username: cmdErrStr("u"), Credential: cmdErrStr("p"), Caid: &caid}}
		a := cmdErrStarted(t, func(r *StartRequest) { r.ICEServers = servers })
		if a.iceServers == nil || !reflect.DeepEqual(*a.iceServers, servers) {
			t.Fatalf("ice servers = %+v", a.iceServers)
		}
	})
	iceRefused := func(t *testing.T, entry wpdef.ICEServerStruct) {
		t.Helper()
		_, err := cmdErrStart(t, func(r *StartRequest) { r.ICEServers = []wpdef.ICEServerStruct{entry} })
		cmdErrRefused(t, err, "ice server")
	}
	stun := []string{"stun:a.example"}
	t.Run("rejects an ice_servers entry with no urls", func(t *testing.T) {
		iceRefused(t, wpdef.ICEServerStruct{Username: cmdErrStr("u")})
		iceRefused(t, wpdef.ICEServerStruct{Urls: []string{}})
	})
	t.Run("rejects an empty url, username or credential", func(t *testing.T) {
		iceRefused(t, wpdef.ICEServerStruct{Urls: []string{""}})
		iceRefused(t, wpdef.ICEServerStruct{Urls: []string{"stun:a.example", ""}})
		iceRefused(t, wpdef.ICEServerStruct{Urls: stun, Username: cmdErrStr("")})
		iceRefused(t, wpdef.ICEServerStruct{Urls: stun, Credential: cmdErrStr("")})
	})
	t.Run("rejects an ice_servers entry past the struct's own limits", func(t *testing.T) {
		eleven := make([]string, 11)
		for i := range eleven {
			eleven[i] = "stun:a.example"
		}
		caid := uint16(65535)
		iceRefused(t, wpdef.ICEServerStruct{Urls: eleven})
		iceRefused(t, wpdef.ICEServerStruct{Urls: []string{"stun:" + strings.Repeat("a", 2000)}})
		iceRefused(t, wpdef.ICEServerStruct{Urls: stun, Username: cmdErrStr(strings.Repeat("u", 509))})
		iceRefused(t, wpdef.ICEServerStruct{Urls: stun, Credential: cmdErrStr(strings.Repeat("p", 513))})
		iceRefused(t, wpdef.ICEServerStruct{Urls: stun, Caid: &caid})
	})
	t.Run("rejects more ice_servers than the command's own list takes", func(t *testing.T) {
		servers := make([]wpdef.ICEServerStruct, 11)
		for i := range servers {
			servers[i] = wpdef.ICEServerStruct{Urls: stun}
		}
		_, err := cmdErrStart(t, func(r *StartRequest) { r.ICEServers = servers })
		cmdErrRefused(t, err, "eleven ice servers")
	})
	t.Run("rejects an ice_transport_policy past the field's length", func(t *testing.T) {
		_, err := cmdErrStart(t, func(r *StartRequest) { r.ICETransportPolicy = strings.Repeat("r", 17) })
		cmdErrRefused(t, err, "ice_transport_policy")
	})
	// A uint16 carries no negative, fraction, NaN or infinity: 0 remains.
	t.Run("rejects a negative, zero, fractional, NaN, or infinite max_resolution width or height hint", func(t *testing.T) {
		for _, res := range []*csdef.VideoResolutionStruct{cmdErrRes(0, 480), cmdErrRes(640, 0)} {
			_, err := cmdErrStart(t, func(r *StartRequest) { r.Video = &VideoBounds{MaxResolution: res} })
			cmdErrRefused(t, err, "max_resolution")
		}
	})
	t.Run("rejects a channel count above the eight the cluster allows, not merely a non-positive one", func(t *testing.T) {
		_, err := cmdErrStart(t, func(r *StartRequest) { r.Audio = &AudioBounds{ChannelCount: ptr[uint8](9)} })
		cmdErrRefused(t, err, "channel_count 9")
	})
	// A stated 0 is held to the model's floor of 1, as matterjs-server's
	// toOptionalNumber holds it (matterjs-server
	// packages/ws-controller/src/camera/cameraCommands.ts:378-382 and
	// packages/ws-controller/src/camera/cameraFieldRanges.ts:48-56 at
	// 0bd06f2); an unstated (nil) bound passes.
	t.Run("rejects a stated zero below each bound's floor, and passes an unstated one", func(t *testing.T) {
		zero16, zero32, zero8 := ptr[uint16](0), ptr[uint32](0), ptr[uint8](0)
		for _, c := range []struct {
			field string
			set   func(r *StartRequest)
		}{
			{"video.min_frame_rate", func(r *StartRequest) { r.Video = &VideoBounds{MinFrameRate: zero16} }},
			{"video.max_frame_rate", func(r *StartRequest) { r.Video = &VideoBounds{MaxFrameRate: zero16} }},
			{"video.min_bit_rate", func(r *StartRequest) { r.Video = &VideoBounds{MinBitRate: zero32} }},
			{"video.max_bit_rate", func(r *StartRequest) { r.Video = &VideoBounds{MaxBitRate: zero32} }},
			{"audio.channel_count", func(r *StartRequest) { r.Audio = &AudioBounds{ChannelCount: zero8} }},
			{"audio.sample_rate", func(r *StartRequest) { r.Audio = &AudioBounds{SampleRate: zero32} }},
			{"audio.bit_rate", func(r *StartRequest) { r.Audio = &AudioBounds{BitRate: zero32} }},
		} {
			_, err := cmdErrStart(t, c.set)
			cmdErrRefused(t, err, c.field+" 0")
			if err == nil || !strings.Contains(err.Error(), c.field+" must be an integer between 1 and ") {
				t.Errorf("%s 0: %v", c.field, err)
			}
		}
		a := cmdErrStarted(t, func(r *StartRequest) {
			r.Video = &VideoBounds{}
			r.Audio = &AudioBounds{}
		})
		if a.video == nil || a.video.minFrameRate != 0 || a.video.maxBitRate != 0 || a.audio == nil || a.audio.sampleRate != 0 {
			t.Fatalf("unstated bounds: video %+v, audio %+v", a.video, a.audio)
		}
	})
	t.Run("accepts the largest value each field can carry", func(t *testing.T) {
		a := cmdErrStarted(t, func(r *StartRequest) {
			r.Video = &VideoBounds{MaxResolution: cmdErrRes(65535, 65535), MaxFrameRate: ptr[uint16](65535), MaxBitRate: ptr[uint32](4294967295)}
			r.Audio = &AudioBounds{ChannelCount: ptr[uint8](8), SampleRate: ptr[uint32](4294967295)}
		})
		if a.video == nil || a.video.maxFrameRate != 65535 || a.audio == nil || a.audio.channelCount != 8 {
			t.Fatalf("video %+v, audio %+v", a.video, a.audio)
		}
	})
	t.Run("upper-cases codec hints, so a lower-case name is not a hard codec failure", func(t *testing.T) {
		a := cmdErrStarted(t, func(r *StartRequest) {
			r.Video = &VideoBounds{Codecs: []string{"h265"}}
			r.Audio = &AudioBounds{Codecs: []string{"opus"}}
		})
		if !reflect.DeepEqual(a.videoCodecs, []string{"H265"}) || a.audio == nil || !reflect.DeepEqual(a.audio.codecs, []string{"OPUS"}) {
			t.Fatalf("video codecs %v, audio %+v", a.videoCodecs, a.audio)
		}
	})
}

// cmdErrSnapshot runs a snapshot on a camera that states no AVSM: a request
// past the argument checks fails with error 105, a refused one with error 8.
func cmdErrSnapshot(t *testing.T, req SnapshotRequest) error {
	t.Helper()
	m, _, _ := managerWith(nil, nil)
	req.Node, req.Endpoint = cmdErrNode(t), tEndpoint
	_, err := (&Service{m: m}).Snapshot(context.Background(), req)
	return err
}

// cmdErrSnapshotTaken fails unless the request passed the argument checks.
func cmdErrSnapshotTaken(t *testing.T, req SnapshotRequest) {
	t.Helper()
	var ns *NotSupportedError
	if err := cmdErrSnapshot(t, req); !errors.As(err, &ns) {
		t.Fatalf("snapshot %+v: want the camera reached, got %v", req, err)
	}
}

// The parseSnapshotArgs cases whose meaning is a Matter rule
// (CameraCommandsTest.ts:739-872, matterjs-server at 0bd06f2).
func TestCommandsErrorsParseSnapshotArgsParityMatterjsServer(t *testing.T) {
	t.Run("parses an optional max_resolution and codec name", func(t *testing.T) {
		got, err := checkResolution(cmdErrRes(640, 480), "max_resolution")
		if err != nil || got != (resolution{640, 480}) {
			t.Fatalf("max_resolution = %+v, %v", got, err)
		}
		codec := csdef.ImageCodecJpeg
		cmdErrSnapshotTaken(t, SnapshotRequest{MaxResolution: cmdErrRes(640, 480), Codec: &codec})
	})
	t.Run("reads the overlay arguments, and leaves each unstated when the caller says nothing", func(t *testing.T) {
		on, off := true, false
		if got := overlayBoundsOf(&off, &on); got != (overlayBounds{watermark: flagOff, osd: flagOn}) {
			t.Fatalf("overlays = %+v", got)
		}
		if got := overlayBoundsOf(nil, nil); got != (overlayBounds{watermark: flagUnstated, osd: flagUnstated}) {
			t.Fatalf("overlays = %+v", got)
		}
	})
	t.Run("leaves max_resolution and codec undefined when omitted", func(t *testing.T) {
		if got, err := checkResolution(nil, "max_resolution"); err != nil || got != (resolution{}) {
			t.Fatalf("max_resolution = %+v, %v", got, err)
		}
		cmdErrSnapshotTaken(t, SnapshotRequest{})
	})
	// A height left out is Go's 0.
	t.Run("rejects a malformed max_resolution", func(t *testing.T) {
		cmdErrRefused(t, cmdErrSnapshot(t, SnapshotRequest{MaxResolution: &csdef.VideoResolutionStruct{Width: 1280}}), "max_resolution")
	})
	t.Run("accepts a resolution target with a width, a height, or both", func(t *testing.T) {
		for _, rt := range []*ResolutionTarget{{Width: 320}, {Height: 240}, {Width: 320, Height: 240}, nil} {
			cmdErrSnapshotTaken(t, SnapshotRequest{Resolution: rt})
		}
	})
	// The JSON shapes are the host's; a target stating no side is the rule.
	t.Run("rejects a resolution target that is not an object, states no side, or names an unknown key", func(t *testing.T) {
		cmdErrRefused(t, cmdErrSnapshot(t, SnapshotRequest{Resolution: &ResolutionTarget{}}), "resolution {}")
	})
	// A uint16 carries no negative, fraction, NaN or infinity: 0 remains.
	t.Run("rejects a negative, zero, fractional, NaN, or infinite max_resolution width or height", func(t *testing.T) {
		for _, res := range []*csdef.VideoResolutionStruct{cmdErrRes(0, 480), cmdErrRes(640, 0)} {
			cmdErrRefused(t, cmdErrSnapshot(t, SnapshotRequest{MaxResolution: res}), "max_resolution")
		}
	})
}

// The parseReleaseStreamArgs cases whose meaning is a Matter rule
// (CameraCommandsTest.ts:874-916, matterjs-server at 0bd06f2).
func TestCommandsErrorsParseReleaseStreamArgsParityMatterjsServer(t *testing.T) {
	t.Run("rejects a kind that is not video, audio, or snapshot", func(t *testing.T) {
		err := (&Service{}).ReleaseStream(context.Background(), cmdErrNode(t), tEndpoint, "metadata", 3)
		cmdErrRefused(t, err, "kind metadata")
	})
	// 65536 is past the uint16 the API takes.
	t.Run("takes the highest stream_id each kind holds and refuses the one above it", func(t *testing.T) {
		for _, kind := range []StreamKind{StreamVideo, StreamAudio, StreamSnapshot} {
			if err := validateStreamID(kind, 65535); err != nil {
				t.Fatalf("%s 65535: %v", kind, err)
			}
		}
	})
}

// cmdErrCapabilities is what manager.capabilities makes of st.
func cmdErrCapabilities(t *testing.T, st *cameraState) *Capabilities {
	t.Helper()
	m, _, _ := managerWith(st, nil)
	c, err := m.capabilities(context.Background(), tTarget)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The toWireCapabilities cases whose meaning the Capabilities type carries
// (CameraCommandsTest.ts:918-1201, matterjs-server at 0bd06f2).
func TestCommandsErrorsToWireCapabilitiesParityMatterjsServer(t *testing.T) {
	t.Run("reports each privacy switch under its own wire key", func(t *testing.T) {
		on, off := true, false
		c := cmdErrCapabilities(t, &cameraState{features: csdef.FeatureAudio | csdef.FeaturePrivacy, privacy: privacyState{softRecording: &on, softLivestream: &off, hard: &on}})
		if !reflect.DeepEqual(c.Features, []string{"Audio", "Privacy"}) {
			t.Fatalf("features = %v", c.Features)
		}
		p := c.Privacy
		if p.SoftRecording == nil || !*p.SoftRecording || p.SoftLivestream == nil || *p.SoftLivestream || p.Hard == nil || !*p.Hard {
			t.Fatalf("privacy = %+v", p)
		}
	})
	// Absent means the camera has no such switch, not that it is off.
	t.Run("emits snake_case keys and omits absent capabilities", func(t *testing.T) {
		c := cmdErrCapabilities(t, &cameraState{})
		if c.Privacy != (Privacy{}) || len(c.Features) != 0 || c.Video.Sensor != nil || c.Limits.MaxNetworkBandwidth != nil {
			t.Fatalf("capabilities = %+v", c)
		}
		if c.Video.RateDistortionPoints == nil {
			t.Fatal("rate distortion points absent, want an empty list")
		}
	})
	t.Run("publishes the bandwidth ceiling the server caps a stream's bit rate at", func(t *testing.T) {
		c := cmdErrCapabilities(t, &cameraState{maxNetworkBandwidth: stated(2000000)})
		if bw := c.Limits.MaxNetworkBandwidth; bw == nil || *bw != 2000000 {
			t.Fatalf("max network bandwidth = %v", bw)
		}
	})
	t.Run("includes a stated sensor and viewport rather than omitting them", func(t *testing.T) {
		hdr := 15
		c := cmdErrCapabilities(t, &cameraState{
			sensor:      &sensorParams{width: 2560, height: 1440, maxFPS: 30, maxHDRFPS: &hdr, hdrCapable: true},
			minViewport: &resolution{640, 360},
		})
		v := c.Video
		if v.Sensor == nil || *v.Sensor != *cmdErrRes(2560, 1440) || v.MinViewport == nil || *v.MinViewport != *cmdErrRes(640, 360) ||
			v.MaxFPS == nil || *v.MaxFPS != 30 || v.MaxHDRFPS == nil || *v.MaxHDRFPS != 15 || v.HDRCapable == nil || !*v.HDRCapable {
			t.Fatalf("video = %+v", v)
		}
	})
}

func cmdErrEnvelope() *videoEnvelope {
	return &videoEnvelope{
		codec: csdef.VideoCodecHevc, minResolution: resolution{640, 360}, maxResolution: resolution{1920, 1080},
		minFrameRate: 1, maxFrameRate: 30, minBitRate: 100000, maxBitRate: 8000000, keyFrameInterval: 2000,
	}
}

var cmdErrAudioEnvelope = audioEnvelope{codec: csdef.AudioCodecOpus, channelCount: 1, sampleRate: 48000, bitRate: 64000, bitDepth: 16}

// The toWireStartStreamResult cases whose meaning the Lease type carries
// (CameraCommandsTest.ts:1203-1340, matterjs-server at 0bd06f2).
func TestCommandsErrorsToWireStartStreamResultParityMatterjsServer(t *testing.T) {
	t.Run("reports a video-only session with codec and envelope ranges", func(t *testing.T) {
		env := cmdErrEnvelope()
		env.overlays = overlayBounds{watermark: flagOff, osd: flagOn}
		got := leaseOf(StreamVideo, &resolvedStream{streamID: 1, provenance: ProvenanceAllocated, video: env})
		off, on := false, true
		want := Lease{Kind: StreamVideo, ID: 1, Provenance: ProvenanceAllocated, Video: &VideoEnvelope{
			Codec: csdef.VideoCodecHevc, MinResolution: *cmdErrRes(640, 360), MaxResolution: *cmdErrRes(1920, 1080),
			MinFrameRate: 1, MaxFrameRate: 30, MinBitRate: 100000, MaxBitRate: 8000000, KeyFrameInterval: 2000,
			Overlays: Overlays{Watermark: &off, OSD: &on},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("lease = %+v, want %+v", got, want)
		}
	})
	t.Run("carries all three provenances through unchanged, on both tracks", func(t *testing.T) {
		for _, p := range []Provenance{ProvenanceAllocated, ProvenanceReused, ProvenanceAdopted} {
			v := leaseOf(StreamVideo, &resolvedStream{streamID: 1, provenance: p, video: cmdErrEnvelope()})
			env := cmdErrAudioEnvelope
			a := leaseOf(StreamAudio, &resolvedStream{streamID: 2, provenance: p, audio: &env})
			if v.Provenance != p || a.Provenance != p {
				t.Fatalf("provenance %s: video %s, audio %s", p, v.Provenance, a.Provenance)
			}
		}
	})
	t.Run("reports the ceilings the encoder budget lowered, and omits the field when it lowered none", func(t *testing.T) {
		narrowed := leaseOf(StreamVideo, &resolvedStream{streamID: 1, provenance: ProvenanceAllocated, video: cmdErrEnvelope(), budgetNarrowed: &videoBudgetNarrowing{maxFrameRate: 30}})
		if !reflect.DeepEqual(narrowed.BudgetNarrowed, &BudgetNarrowing{MaxFrameRate: 30}) {
			t.Fatalf("narrowed = %+v", narrowed.BudgetNarrowed)
		}
		if untouched := leaseOf(StreamVideo, &resolvedStream{streamID: 1, provenance: ProvenanceAllocated, video: cmdErrEnvelope()}); untouched.BudgetNarrowed != nil {
			t.Fatalf("untouched = %+v", untouched.BudgetNarrowed)
		}
	})
	t.Run("reports a degraded stream and an audio track", func(t *testing.T) {
		if v := leaseOf(StreamVideo, &resolvedStream{streamID: 1, provenance: ProvenanceReused, degraded: true, video: cmdErrEnvelope()}); !v.Degraded {
			t.Fatal("degraded lost")
		}
		env := cmdErrAudioEnvelope
		a := leaseOf(StreamAudio, &resolvedStream{streamID: 2, provenance: ProvenanceAllocated, audio: &env})
		want := Lease{Kind: StreamAudio, ID: 2, Provenance: ProvenanceAllocated, Audio: &AudioEnvelope{Codec: csdef.AudioCodecOpus, ChannelCount: 1, SampleRate: 48000, BitRate: 64000, BitDepth: 16}}
		if !reflect.DeepEqual(a, want) {
			t.Fatalf("audio lease = %+v", a)
		}
	})
}

// cmdErrReasons is CAMERA_INCOMPATIBLE_REASONS (matterjs-server
// packages/ws-client/src/models/model.ts:797-805 at 0bd06f2) in its order.
var cmdErrReasons = []IncompatibleReason{ReasonCodec, ReasonBounds, ReasonFeature, ReasonCapability, ReasonOffer, ReasonNoMedia, ReasonLevel}

// The camera errors' messages, fields and codes as types
// (CameraErrorsTest.ts:28-201, matterjs-server at 0bd06f2).
func TestCommandsErrorsCameraServerErrorsParityMatterjsServer(t *testing.T) {
	asIncompatible := func(t *testing.T, err error) *IncompatibleError {
		t.Helper()
		var e *IncompatibleError
		if !errors.As(err, &e) {
			t.Fatalf("%v is no IncompatibleError", err)
		}
		return e
	}
	t.Run("carries the incompatibility detail as JSON in the message", func(t *testing.T) {
		e := asIncompatible(t, &IncompatibleError{Reason: ReasonCodec, Device: []string{"H265"}, Requested: []string{"H264"}})
		if e.Error() != "No codec supported by both the camera and the caller" || e.Reason != "codec" ||
			!reflect.DeepEqual(e.Device, []string{"H265"}) || !reflect.DeepEqual(e.Requested, []string{"H264"}) {
			t.Fatalf("error = %q %+v", e, e)
		}
	})
	t.Run("distinguishes a missing capability from a bound the caller can change", func(t *testing.T) {
		e := asIncompatible(t, &IncompatibleError{Reason: ReasonCapability, Device: []string{}, Requested: []string{}})
		if e.Error() != "Camera states no capability this request can use" || e.Error() == (&IncompatibleError{Reason: ReasonBounds}).Error() {
			t.Fatalf("message = %q", e)
		}
	})
	t.Run("states one message per reason, over the vocabulary the client package publishes", func(t *testing.T) {
		names := make([]string, len(cmdErrReasons))
		seen := map[string]bool{}
		for i, r := range cmdErrReasons {
			names[i] = string(r)
			msg := (&IncompatibleError{Reason: r}).Error()
			if msg == "" || strings.HasPrefix(msg, "camera stream incompatible: ") || seen[msg] {
				t.Fatalf("reason %s: message %q", r, msg)
			}
			seen[msg] = true
		}
		if want := []string{"codec", "bounds", "feature", "capability", "offer", "no_media", "level"}; !reflect.DeepEqual(names, want) {
			t.Fatalf("reasons = %v", names)
		}
	})
	t.Run("reports the device status that produced a bounds failure", func(t *testing.T) {
		status := im.StatusCode(0x87)
		e := asIncompatible(t, &IncompatibleError{Reason: ReasonBounds, DeviceStatus: &status})
		if e.DeviceStatus == nil || *e.DeviceStatus != 0x87 {
			t.Fatalf("device status = %v", e.DeviceStatus)
		}
	})
	t.Run("names the streams in the way when resources are exhausted", func(t *testing.T) {
		enc, rate := 1, 248832000
		var e *ExhaustedError
		if !errors.As(error(&ExhaustedError{Allocated: []OccupyingStream{{Kind: StreamVideo, StreamID: 1, ReferenceCount: 1}}, MaxEncoders: &enc, MaxPixelRate: &rate}), &e) {
			t.Fatal("no ExhaustedError")
		}
		if e.Error() != "Camera has no capacity for this stream" ||
			!reflect.DeepEqual(e.Allocated, []OccupyingStream{{Kind: "video", StreamID: 1, ReferenceCount: 1}}) ||
			*e.MaxEncoders != 1 || *e.MaxPixelRate != 248832000 {
			t.Fatalf("error = %q %+v", e, e)
		}
	})
	// The message keeps the module's "camera:" prefix (errors.go).
	t.Run("names the stream and its reference count when release is refused", func(t *testing.T) {
		var err error = &InUseError{StreamID: 3, ReferenceCount: 2}
		var e *InUseError
		if !errors.Is(err, ErrStreamInUse) || !errors.As(err, &e) || e.StreamID != 3 || e.ReferenceCount != 2 || err.Error() != ErrStreamInUse.Error() {
			t.Fatalf("error = %q %+v", err, e)
		}
	})
	// Go names the codes 102 to 106 by type: each error is its own and no other.
	t.Run("numbers the camera codes 102 to 106 with no gap, as the ws-client constants spell them", func(t *testing.T) {
		is := []func(error) bool{
			func(err error) bool { var e *IncompatibleError; return errors.As(err, &e) },
			func(err error) bool { var e *ExhaustedError; return errors.As(err, &e) },
			func(err error) bool { return errors.Is(err, ErrStreamInUse) },
			func(err error) bool { var e *NotSupportedError; return errors.As(err, &e) },
			func(err error) bool { var e *PrivacyError; return errors.As(err, &e) },
		}
		errs := []error{&IncompatibleError{Reason: ReasonCodec}, &ExhaustedError{}, &InUseError{StreamID: 1}, &NotSupportedError{}, &PrivacyError{Modes: []PrivacyMode{PrivacyHard}}}
		for i, err := range errs {
			for j, match := range is {
				if match(err) != (i == j) {
					t.Fatalf("error %d (code %d) matched as code %d: %v", i, 102+i, 102+j, match(err))
				}
			}
		}
	})
	t.Run("carries the track and the device status for every reason that has them", func(t *testing.T) {
		status := im.StatusCode(1)
		for _, r := range cmdErrReasons {
			track := TrackAudio
			if r == ReasonNoMedia {
				track = ""
			}
			e := asIncompatible(t, &IncompatibleError{Reason: r, Track: track, DeviceStatus: &status})
			if e.Track != track || e.DeviceStatus == nil || *e.DeviceStatus != 1 {
				t.Fatalf("reason %s: track %q, status %v", r, e.Track, e.DeviceStatus)
			}
		}
	})
	t.Run("names the clusters an endpoint is missing", func(t *testing.T) {
		var e *NotSupportedError
		if !errors.As(error(&NotSupportedError{Missing: []uint32{0x551}}), &e) ||
			e.Error() != "Endpoint does not support camera streaming" || !reflect.DeepEqual(e.Missing, []uint32{0x551}) {
			t.Fatalf("error = %+v", e)
		}
	})
}
