// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// target is one camera endpoint: the camera's operational node id and the
// endpoint that serves its AVSM and WebRTC provider clusters.
type target struct {
	nodeID     uint64
	endpointID uint16
}

// callCluster names the cluster a deviceCall goes to.
type callCluster uint8

// The two camera clusters the manager invokes.
const (
	clusterAVSM callCluster = iota + 1
	clusterProvider
)

// The commands, spelled as matterjs-server's manager names them, so a test
// reads like its source.
const (
	cmdVideoStreamAllocate      = "videoStreamAllocate"
	cmdVideoStreamDeallocate    = "videoStreamDeallocate"
	cmdAudioStreamAllocate      = "audioStreamAllocate"
	cmdAudioStreamDeallocate    = "audioStreamDeallocate"
	cmdSnapshotStreamAllocate   = "snapshotStreamAllocate"
	cmdSnapshotStreamDeallocate = "snapshotStreamDeallocate"
	cmdCaptureSnapshot          = "captureSnapshot"
	callProvideOffer            = "provideOffer"
	callSolicitOffer            = "solicitOffer"
	callEndSession              = "endSession"
	callProvideAnswer           = "provideAnswer"
	callProvideIceCandidates    = "provideIceCandidates"
)

// deviceCall is one command to a camera: fields is the generated request,
// and establishing (ProvideOffer and SolicitOffer only) is called with the
// provider's session id before the local requestor holds the session, so
// before any End for it can be routed. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:481-496 at
// 0bd06f2.
type deviceCall struct {
	target
	cluster      callCluster
	command      string
	fields       spec.Encodable
	establishing func(sessionID uint16)
}

// DeviceSession is one entry of a camera's CurrentSessions (§11.5.5.1),
// its stream ids from the revision-2 lists or the deprecated revision-1
// fields. EstablishedByUs: PeerNodeID is this controller's RootNodeID, so
// only this controller can end the session (§11.5.6.7.3). Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraTypes.ts:166-180
// at 0bd06f2.
type DeviceSession struct {
	ID              uint16
	PeerNodeID      uint64
	PeerEndpoint    uint16
	StreamUsage     uint8
	VideoStreams    []uint16
	AudioStreams    []uint16
	EstablishedByUs bool
}

// deviceIO is what the manager needs from the cameras: reads of the
// controller's image and invokes. It is called under the manager's
// endpoint lock, so calling back into the manager for the same endpoint
// deadlocks. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:468-498 at
// 0bd06f2.
type deviceIO interface {
	// readCameraState is nil when the endpoint has no AVSM.
	readCameraState(ctx context.Context, t target) (*cameraState, error)
	// missingCameraClusters are the camera clusters the endpoint lacks.
	missingCameraClusters(ctx context.Context, t target) []uint32
	// readWebRTCSessions is the provider's CurrentSessions; ok false
	// without a provider.
	readWebRTCSessions(ctx context.Context, t target) (sessions []DeviceSession, ok bool)
	// invoke sends one command and returns the generated response (nil
	// for a status-only command).
	invoke(ctx context.Context, call deviceCall) (any, error)
	// abandonSession gives up a session this controller failed to end:
	// its signalling stops, and the leftover sweep ends it at the node's
	// next report if the camera still lists it.
	abandonSession(ctx context.Context, t target, sessionID uint16) error
}
