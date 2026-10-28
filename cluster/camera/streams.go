// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// StreamSource is the camera's stream state the transports validate a
// request against: its allocated streams, its stream usages and its
// privacy modes. [*AvStream] is one; the kit wires the device's
// CameraAvStreamManagement server. chip's transports ask their delegates,
// which the camera app answers from its CameraAvStreamManagement server
// (examples/camera-app/linux/src/clusters/camera-avstream-mgmt/
// camera-av-stream-manager.cpp, push-av-stream-manager.cpp at 6170af84);
// the rules below are those answers. A nil StreamSource is a camera
// without streams.
type StreamSource interface {
	VideoStreams() []VideoStream
	AudioStreams() []AudioStream
	StreamUsagePriorities() []StreamUsage
	SupportedStreamUsages() []StreamUsage
	PrivacyModes() (hard, softRecording, softLivestream bool)
}

// The stream usages the privacy rules name (StreamUsageEnum).
const (
	usageRecording = 1
	usageAnalysis  = 2
	usageLiveView  = 3
)

var _ StreamSource = (*AvStream)(nil)

// streamView reads a StreamSource that may be nil.
type streamView struct{ src StreamSource }

func (v streamView) videos() []VideoStream {
	if v.src == nil {
		return nil
	}
	return v.src.VideoStreams()
}

func (v streamView) audios() []AudioStream {
	if v.src == nil {
		return nil
	}
	return v.src.AudioStreams()
}

// checkPrivacy is chip's CheckPrivacyModes
// (WebRTCTransportProviderCluster.cpp:400-445,
// PushAVStreamTransportLogic.cpp:1847-1902): hard privacy refuses every
// usage, soft livestream privacy LiveView, soft recording privacy
// Recording and Analysis — INVALID_IN_STATE.
func (v streamView) checkPrivacy(cluster string, usage uint8) error {
	if v.src == nil {
		return nil
	}
	hard, rec, live := v.src.PrivacyModes()
	switch {
	case hard:
		return spec.Errorf(im.StatusInvalidInState, "%s: hard privacy mode is on", cluster)
	case live && usage == usageLiveView:
		return spec.Errorf(im.StatusInvalidInState, "%s: soft livestream privacy mode refuses LiveView", cluster)
	case rec && (usage == usageRecording || usage == usageAnalysis):
		return spec.Errorf(im.StatusInvalidInState, "%s: soft recording privacy mode refuses usage %d", cluster, usage)
	}
	return nil
}

// usagePrioritised is the WebRTC provider's IsStreamUsageSupported: the
// usage is in StreamUsagePriorities (webrtc-provider-manager.cpp:703-724).
func (v streamView) usagePrioritised(usage uint8) bool {
	return v.src != nil && slices.ContainsFunc(v.src.StreamUsagePriorities(), func(u StreamUsage) bool { return uint8(u) == usage })
}

// usageSupported is the stream managers' supported-usage check: the usage
// is in SupportedStreamUsages (camera-av-stream-manager.cpp:66-72,
// push-av-stream-manager.cpp:385-395).
func (v streamView) usageSupported(usage uint8) bool {
	return v.src != nil && slices.ContainsFunc(v.src.SupportedStreamUsages(), func(u StreamUsage) bool { return uint8(u) == usage })
}

// video returns the allocated video stream of the id
// (ValidateVideoStreamID, camera-av-stream-manager.cpp:219-235).
func (v streamView) video(id uint16) (VideoStream, bool) {
	for _, s := range v.videos() {
		if s.VideoStreamId == id {
			return s, true
		}
	}
	return VideoStream{}, false
}

// allocated reports whether a video (ValidateVideoStreamID, :219-235) or
// audio stream (ValidateAudioStreamID, :237-253) of the id is allocated.
func (v streamView) allocated(id uint16, video bool) bool {
	if video {
		_, ok := v.video(id)
		return ok
	}
	return slices.ContainsFunc(v.audios(), func(s AudioStream) bool { return s.AudioStreamId == id })
}

// autoSelect is the stream manager's ValidateStreamUsage selection
// (camera-av-stream-manager.cpp:52-135): for a requested kind whose list
// is empty, the first allocated stream of the usage, else the last
// allocated stream of another usage; a list with ids stays. The usage
// must be supported (false otherwise).
func (v streamView) autoSelect(usage uint8, videos, audios *[]uint16) bool {
	if !v.usageSupported(usage) { // :66-72
		return false
	}
	if videos != nil && len(*videos) == 0 { // :82-95, :128-131
		vs := v.videos()
		ids := make([]uint16, len(vs))
		for i, s := range vs {
			ids[i] = s.VideoStreamId
		}
		*videos = pick(ids, func(i int) bool { return uint8(vs[i].StreamUsage) == usage })
	}
	if audios != nil && len(*audios) == 0 { // :108-121, :123-126
		as := v.audios()
		ids := make([]uint16, len(as))
		for i, s := range as {
			ids[i] = s.AudioStreamId
		}
		*audios = pick(ids, func(i int) bool { return uint8(as[i].StreamUsage) == usage })
	}
	return true
}

// pick is the selection loop of autoSelect: the first id whose stream
// matches, else the last id seen before it (every id when none matches).
func pick(ids []uint16, match func(int) bool) []uint16 {
	var loose *uint16
	for i, id := range ids {
		if match(i) {
			return []uint16{id}
		}
		loose = &ids[i]
	}
	if loose != nil {
		return []uint16{*loose}
	}
	return []uint16{}
}
