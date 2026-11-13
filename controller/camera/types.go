// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"strconv"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// The quirks of tested cameras that this package works around, each a named
// constant so the workaround is found by its name.
const (
	// keyFrameIntervalMS is the only key frame interval a tested camera
	// accepted in raw VideoStreamAllocate probes (2000, 3000, 3999, 5000 and
	// 8000 were refused with DynamicConstraintError); the reference camera
	// app uses it for every stream. Mirrors matterjs-server
	// packages/ws-controller/src/camera/streamPolicy.ts:22-26 at 0bd06f2. The
	// limitation is docs/CAMERA_LIMITATIONS.md:91 of matterjs-server
	// ("key frame intervals other than 4000 ms").
	keyFrameIntervalMS = 4000

	// defaultMaxFrameRate is the frame rate ceiling when the caller states
	// no max_frame_rate: a tested camera accepted 640x480 at 90 fps and sent
	// black video. Mirrors matterjs-server
	// packages/ws-controller/src/camera/streamPolicy.ts:28-32 at 0bd06f2. The
	// limitation is docs/CAMERA_LIMITATIONS.md:88 of matterjs-server.
	defaultMaxFrameRate = 30

	// currentSessionsStreamIDsTrusted is false: a tested camera reported
	// random stream ids for each WebRTC session in CurrentSessions, not the
	// streams the session uses, so a session's streams are the ids this
	// controller sent itself. The limitation is docs/CAMERA_LIMITATIONS.md:89
	// of matterjs-server at 0bd06f2.
	currentSessionsStreamIDsTrusted = false

	// answerDirectionsNarrowed is true: a tested camera answered a=sendrecv
	// to a section offered a=recvonly, which browsers reject, so every answer
	// is narrowed to the directions its offer allows
	// ([answerWithinOfferedDirections]) before it is passed on. The
	// limitation is docs/CAMERA_LIMITATIONS.md:90 of matterjs-server at
	// 0bd06f2.
	answerDirectionsNarrowed = true
)

// resolution is a frame size in pixels. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:14-17 at 0bd06f2.
type resolution struct {
	width, height int
}

// pixels is the frame's area.
func (r resolution) pixels() int { return r.width * r.height }

// String is the "1920x1080" spelling an error-102 bound states. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:248-250
// at 0bd06f2.
func (r resolution) String() string {
	return strconv.Itoa(r.width) + "x" + strconv.Itoa(r.height)
}

// fitsUnder reports whether r fits under ceiling on each side.
func (r resolution) fitsUnder(ceiling resolution) bool {
	return r.width <= ceiling.width && r.height <= ceiling.height
}

// resolutionTarget is a size a caller wants: at least one side is stated,
// and 0 is a side not stated (the command boundary admits no 0). Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraTypes.ts:19-20 at
// 0bd06f2.
type resolutionTarget struct {
	width, height int
}

// overlayFlag is an overlay flag that may be unstated: an absent field is
// "not stated", which is not the same as off. Mirrors matterjs-server
// packages/ws-controller/src/camera/overlayPolicy.ts:9-13 at 0bd06f2.
type overlayFlag uint8

// The three states of an overlay flag.
const (
	flagUnstated overlayFlag = iota
	flagOff
	flagOn
)

// flagOf states b.
func flagOf(b bool) overlayFlag {
	if b {
		return flagOn
	}
	return flagOff
}

// videoEnvelope is the shape VideoStreamAllocate takes; an unstated
// overlay flag is not sent (§11.2.8.4). Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:22-33 at 0bd06f2.
type videoEnvelope struct {
	overlays         overlayBounds
	codec            csdef.VideoCodecEnum
	minResolution    resolution
	maxResolution    resolution
	minFrameRate     int
	maxFrameRate     int
	minBitRate       int
	maxBitRate       int
	keyFrameInterval int
}

// videoBudgetNarrowing holds each ceiling of a request's first window that
// the encoder budget made the request give up; a zero field gave up
// nothing. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:35-39 at 0bd06f2.
type videoBudgetNarrowing struct {
	maxFrameRate  int
	maxResolution resolution
}

// audioEnvelope is the shape AudioStreamAllocate takes. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraTypes.ts:41-47 at
// 0bd06f2.
type audioEnvelope struct {
	codec        csdef.AudioCodecEnum
	channelCount int
	sampleRate   int
	bitRate      int
	bitDepth     int
}

// allocatedVideoStream is a video stream the camera reports. Its overlays
// are not defaulted: a re-allocation sends them as they are, and an absent
// flag stays absent (§11.2.6.11). Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:49-63 at 0bd06f2.
type allocatedVideoStream struct {
	overlays         overlayBounds
	videoStreamID    uint16
	streamUsage      csdef.StreamUsageEnum
	videoCodec       csdef.VideoCodecEnum
	minResolution    resolution
	maxResolution    resolution
	minFrameRate     int
	maxFrameRate     int
	minBitRate       int
	maxBitRate       int
	keyFrameInterval int
	referenceCount   int
}

// allocatedAudioStream is an audio stream the camera reports. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraTypes.ts:65-74 at
// 0bd06f2.
type allocatedAudioStream struct {
	audioStreamID  uint16
	streamUsage    csdef.StreamUsageEnum
	audioCodec     csdef.AudioCodecEnum
	channelCount   int
	sampleRate     int
	bitRate        int
	bitDepth       int
	referenceCount int
}

// allocatedSnapshotStream is a snapshot stream the camera reports. Its
// overlays are not a promise: §11.2.8.8.6 lets a camera use the source
// video stream's setting instead when no hardware encoder is involved.
// Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:76-94 at 0bd06f2.
type allocatedSnapshotStream struct {
	overlays         overlayBounds
	snapshotStreamID uint16
	imageCodec       csdef.ImageCodecEnum
	minResolution    resolution
	maxResolution    resolution
	quality          int
	referenceCount   int
	// frameRate is the rate this stream reserves in the encoded-pixel
	// budget (§11.2.6.13.3).
	frameRate int
	// encodedPixels: the stream counts in the camera's encoded pixel rate
	// (§11.2.6.13.8).
	encodedPixels bool
	// hardwareEncoder: the stream uses one of the camera's
	// MaxConcurrentEncoders (§11.2.6.13.9).
	hardwareEncoder bool
}

// managedSession is a WebRTC session this controller established for a
// connection. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:123-132 at 0bd06f2.
type managedSession struct {
	webRTCSessionID uint16
	nodeID          uint64
	endpointID      uint16
	connectionID    string
	// clientSessionKey is the owner's own name for the session; "" when it
	// gave none. matterjs-server's record also carries the session's video
	// and audio stream ids, which the stream manager (phase 37 wave C)
	// keeps.
	clientSessionKey string
	// life is the session's identity: the registration that tracks it.
	// The camera can hand its id to a newer session, so a callback
	// delivered late acts on the session it names, not on the id (review
	// II, R2-P1-cam-b-02).
	life *lifecycle
}

// sessionEndCause says why a tracked session ended. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:134-135 at 0bd06f2, whose
// wire causes are packages/ws-client/src/models/model.ts:816-836 of
// matterjs-server at 0bd06f2.
type sessionEndCause string

// The causes: the five a client hears, released, which it never does, and
// signalOverflow, the module's own.
const (
	// endPeer: the camera sent End.
	endPeer sessionEndCause = "peer"
	// endOtherConnection: another connection ended the session.
	endOtherConnection sessionEndCause = "other_connection"
	// endCameraRestarted: the camera restarted, or gave the session's id
	// to a new session.
	endCameraRestarted sessionEndCause = "camera_restarted"
	// endCameraDropped: the session is gone from CurrentSessions without
	// an End.
	endCameraDropped sessionEndCause = "camera_dropped"
	// endNodeRemoved: the node was removed from the fabric.
	endNodeRemoved sessionEndCause = "node_removed"
	// endReleased: this controller ended it for its closing owner or at
	// shutdown.
	endReleased sessionEndCause = "released"
	// endSignalOverflow: this controller ended it because its host let the
	// session's signals pile up past the mailbox's bounds (review II,
	// R2-P1-cam-b-06); the module's own, matterjs-server has no such end.
	endSignalOverflow sessionEndCause = "signal_overflow"
)

// sessionEnd is how a tracked session ended. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:137-144 at 0bd06f2.
type sessionEnd struct {
	cause sessionEndCause
	// reason is the camera's WebRTCEndReasonEnum (§11.4.5.2), for endPeer
	// only and only when hasReason.
	reason    uint8
	hasReason bool
	// requestedBy is the connection whose stop ended the session, for
	// endOtherConnection only.
	requestedBy string
}

// cameraSessionEnded announces the end of a session. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraTypes.ts:146-157
// at 0bd06f2.
type cameraSessionEnded struct {
	sessionEnd
	// life is the ended session's identity (managedSession.life); nil for
	// an id no registration named.
	life            *lifecycle
	nodeID          uint64
	endpointID      uint16
	webRTCSessionID uint16
	// ownerID is the connection that established the session; "" for a
	// session with no recorded owner, which every camera-aware connection
	// hears of.
	ownerID string
	// clientSessionKey is the owner's own name for the session, "" when it
	// gave none.
	clientSessionKey string
}
