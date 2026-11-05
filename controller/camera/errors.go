// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"errors"
	"strings"

	"github.com/SukramJ/go-fabric/im"
)

// The typed errors of camera streaming, one per matterjs-server error code
// 102 to 106 (packages/ws-controller/src/types/WebSocketMessageTypes.ts:107-116
// of matterjs-server at 0bd06f2): [*IncompatibleError] is 102
// CameraStreamIncompatible, [*ExhaustedError] 103 CameraResourceExhausted,
// [ErrStreamInUse] 104 CameraStreamInUse, [*NotSupportedError] 105
// CameraNotSupported and [*PrivacyError] 106 CameraPrivacyMode. A host
// serving that wire protocol maps each onto its code with errors.As.

// IncompatibleReason says what ruled a stream out. Mirrors matterjs-server
// packages/ws-client/src/models/model.ts:782-808 at 0bd06f2.
type IncompatibleReason string

// The reasons of an [*IncompatibleError], spelled as matterjs-server's
// error-102 payload spells them.
const (
	// ReasonCodec: the camera and the request (or its SDP offer) share no
	// codec.
	ReasonCodec IncompatibleReason = "codec"
	// ReasonBounds: the camera cannot meet a resolution, frame-rate or
	// bit-rate limit; [IncompatibleError.Bound] names it when it was
	// decided before asking the camera.
	ReasonBounds IncompatibleReason = "bounds"
	// ReasonFeature: the camera lacks a feature the request needs;
	// [IncompatibleError.Feature] names it.
	ReasonFeature IncompatibleReason = "feature"
	// ReasonCapability: the camera has the feature but reports nothing
	// usable, such as an empty snapshot capability list.
	ReasonCapability IncompatibleReason = "capability"
	// ReasonOffer: the SDP offer refuses the track, does not receive it, or
	// has no media section for it.
	ReasonOffer IncompatibleReason = "offer"
	// ReasonNoMedia: no track is left, so the session would carry no media.
	ReasonNoMedia IncompatibleReason = "no_media"
	// ReasonLevel: the decode level in the offer's a=fmtp line cannot be
	// read.
	ReasonLevel IncompatibleReason = "level"
)

// incompatibleMessages are the messages of the reasons. Mirrors
// matterjs-server packages/ws-controller/src/types/WebSocketMessageTypes.ts:147-155
// at 0bd06f2.
var incompatibleMessages = map[IncompatibleReason]string{
	ReasonCodec:      "No codec supported by both the camera and the caller",
	ReasonBounds:     "Camera cannot serve the requested stream parameters",
	ReasonFeature:    "Camera does not advertise the feature this request needs",
	ReasonCapability: "Camera states no capability this request can use",
	ReasonOffer:      "The offer does not carry this track's media to the peer",
	ReasonNoMedia:    "The request leaves no media for the session to carry",
	ReasonLevel:      "Offer states a codec level this server cannot bound a stream by",
}

// Track names the track of a session start that failed.
type Track string

// The two tracks of a session start.
const (
	TrackVideo Track = "video"
	TrackAudio Track = "audio"
)

// BoundField is the caller hint an [IncompatibleBound] names. Mirrors
// matterjs-server packages/ws-client/src/models/model.ts:766-779 at 0bd06f2.
type BoundField string

// The hints a bound can name: three video hints, two audio hints.
const (
	BoundMinResolution BoundField = "min_resolution"
	BoundMinFrameRate  BoundField = "min_frame_rate"
	BoundMinBitRate    BoundField = "min_bit_rate"
	BoundSampleRate    BoundField = "sample_rate"
	BoundChannelCount  BoundField = "channel_count"
)

// IncompatibleBound is the caller limit ruled out before the camera was
// asked. Mirrors matterjs-server packages/ws-client/src/models/model.ts:872-883
// at 0bd06f2.
type IncompatibleBound struct {
	Field BoundField
	// Requested is the value the caller stated, as text: "1920x1080" for a
	// resolution, digits otherwise.
	Requested string
	// Limit is what the limit ran into, as text: the highest possible value
	// for a range, or the camera's list of values for a sample rate.
	Limit string
}

// IncompatibleError is error 102: the camera, the offer or the request
// rules the stream out. Mirrors matterjs-server
// packages/ws-controller/src/types/WebSocketMessageTypes.ts:119-145 at
// 0bd06f2.
type IncompatibleError struct {
	Reason IncompatibleReason
	// Track is the track that failed; "" for [ReasonNoMedia] and for
	// commands without tracks, such as a snapshot.
	Track Track
	// Feature is the AVSM feature the camera does not advertise, for
	// [ReasonFeature] only.
	Feature string
	// Bound is the caller limit that could not be met, for [ReasonBounds]
	// only and only when it was decided before asking the camera.
	Bound *IncompatibleBound
	// Device is the camera's codec names; empty when the camera did not
	// refuse (the offer did), not "supports nothing".
	Device []string
	// Requested is the codec names the caller asked for.
	Requested []string
	// DeviceStatus is the status the camera answered with, when a device
	// rejection produced the error.
	DeviceStatus *im.StatusCode
}

// Error is the reason's message.
func (e *IncompatibleError) Error() string {
	msg := incompatibleMessages[e.Reason]
	if msg == "" {
		msg = "camera stream incompatible: " + string(e.Reason)
	}
	return msg
}

// StreamKind is the kind of a camera stream.
type StreamKind string

// The three stream kinds.
const (
	StreamVideo    StreamKind = "video"
	StreamAudio    StreamKind = "audio"
	StreamSnapshot StreamKind = "snapshot"
)

// OccupyingStream is a stream that holds capacity a refused request
// needed. Its kind can differ from the request's: a refused snapshot
// allocation reports the video streams, which hold the encoders. Mirrors
// matterjs-server packages/ws-controller/src/types/WebSocketMessageTypes.ts:157-174
// at 0bd06f2.
type OccupyingStream struct {
	Kind           StreamKind
	StreamID       uint16
	ReferenceCount int
}

// ExhaustedError is error 103: the camera refused the allocation for lack
// of capacity. Mirrors matterjs-server
// packages/ws-controller/src/types/WebSocketMessageTypes.ts:185-189 at
// 0bd06f2.
type ExhaustedError struct {
	Allocated []OccupyingStream
	// MaxEncoders is the camera's MaxConcurrentEncoders, nil when it states
	// none.
	MaxEncoders *int
	// MaxPixelRate is the camera's MaxEncodedPixelRate, nil when it states
	// none.
	MaxPixelRate *int
}

// Error is matterjs-server's message for error 103.
func (*ExhaustedError) Error() string { return "Camera has no capacity for this stream" }

// ErrStreamInUse is error 104: a stream release refused because the device
// still references the stream. Mirrors matterjs-server
// packages/ws-controller/src/types/WebSocketMessageTypes.ts:317-324 at
// 0bd06f2.
var ErrStreamInUse = errors.New("camera: stream is in use and cannot be released")

// InUseError is the [ErrStreamInUse] of one release: the stream and, when
// the camera's cached report states one above zero, its reference count.
// It matches ErrStreamInUse with errors.Is and unwraps to the camera's
// refusal. Mirrors matterjs-server
// packages/ws-controller/src/types/WebSocketMessageTypes.ts:317-324 at
// 0bd06f2.
type InUseError struct {
	StreamID uint16
	// ReferenceCount is the cached count; 0 when the report states none
	// above zero, since the cache can lag.
	ReferenceCount int
	cause          error
}

func (e *InUseError) Error() string { return ErrStreamInUse.Error() }

// Is makes an InUseError [ErrStreamInUse].
func (e *InUseError) Is(target error) bool { return target == ErrStreamInUse }

// Unwrap is the camera's refusal.
func (e *InUseError) Unwrap() error { return e.cause }

// NotSupportedError is error 105: the endpoint lacks clusters camera
// streaming needs. Mirrors matterjs-server
// packages/ws-controller/src/types/WebSocketMessageTypes.ts:335-341 at
// 0bd06f2.
type NotSupportedError struct {
	// Missing is the ids of the clusters the endpoint lacks.
	Missing []uint32
}

// Error is matterjs-server's message for error 105.
func (*NotSupportedError) Error() string { return "Endpoint does not support camera streaming" }

// PrivacyMode names one privacy switch of a camera. Mirrors matterjs-server
// packages/ws-client/src/models/model.ts:629-633 at 0bd06f2.
type PrivacyMode string

// The three privacy switches (§11.2.7.20 to §11.2.7.22).
const (
	// PrivacySoftRecording is SoftRecordingPrivacyModeEnabled: it blocks a
	// session of stream usage Recording or Analysis.
	PrivacySoftRecording PrivacyMode = "soft_recording_mode_enabled"
	// PrivacySoftLivestream is SoftLivestreamPrivacyModeEnabled: it blocks a
	// session of stream usage LiveView, and every snapshot.
	PrivacySoftLivestream PrivacyMode = "soft_livestream_mode_enabled"
	// PrivacyHard is HardPrivacyModeOn, the physical switch: it blocks every
	// session and every snapshot.
	PrivacyHard PrivacyMode = "hard_mode_on"
)

// PrivacyError is error 106: a privacy switch of the camera forbids the
// session or the snapshot. Mirrors matterjs-server
// packages/ws-controller/src/types/WebSocketMessageTypes.ts:176-183 at
// 0bd06f2.
type PrivacyError struct {
	// Modes names every switch that forbids the call, since the device
	// answers one status for all of them.
	Modes []PrivacyMode
	// DeviceStatus is the status the camera answered with.
	DeviceStatus im.StatusCode
}

// Error is matterjs-server's message for error 106, with the modes.
func (e *PrivacyError) Error() string {
	if len(e.Modes) == 0 {
		return "Camera privacy mode is enabled"
	}
	modes := make([]string, len(e.Modes))
	for i, m := range e.Modes {
		modes[i] = string(m)
	}
	return "Camera privacy mode is enabled: " + strings.Join(modes, ", ")
}
