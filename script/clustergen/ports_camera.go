// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// The host ports of the camera servers (brief 26-A1, concept v10 §D): each
// is the port its server in cluster/camera runs on, in the generated
// types, and cluster/camera names it by alias — one form. The module holds
// the state and the rules; the host's media stack moves the camera, plays
// the chime and carries the WebRTC session.
var cameraPorts = map[string]portSet{
	"WebRtcTransportRequestor": {
		ports: []portDecl{{
			name: "RequestorPort",
			doc: []string{
				"RequestorPort receives the signalling a peer sends for a session",
				"the kit's server tracks (cluster/camera Requestor): the SDP offer,",
				"the SDP answer, ICE candidates and the end of the session, each",
				"with the session it names. End is called after the session left",
				"CurrentSessions. matter.js's WebRtcTransportRequestorServer.Events",
				"(offer, answer, iceCandidates, end).",
			},
			methods: []string{
				"Offer(ctx context.Context, session WebRTCSessionStruct, req OfferRequest)",
				"Answer(ctx context.Context, session WebRTCSessionStruct, sdp string)",
				"IceCandidates(ctx context.Context, session WebRTCSessionStruct, candidates []ICECandidateStruct)",
				"End(ctx context.Context, session WebRTCSessionStruct, reason uint8)",
			},
		}, {
			name:     "RequestorRefusals",
			optional: true,
			doc: []string{
				"RequestorRefusals is an optional interface of a [RequestorPort]:",
				"Refused is called for each signal the kit's server answered",
				"NOT_FOUND because it tracks no session of that id for the peer's",
				"fabric and node — matter.js's refused event, with the id the peer",
				"named.",
			},
			methods: []string{"Refused(ctx context.Context, signal Signal, webRtcSessionID uint16, fabricIndex uint8, peerNodeID uint64)"},
		}},
		types: []string{
			"// Signal is a signalling command a peer invokes on the requestor",
			"// (matter.js WebRtcTransportRequestorServer.SignalKind).",
			"type Signal string",
			"",
			"// The signals.",
			"const (",
			"\tSignalOffer         Signal = \"offer\"",
			"\tSignalAnswer        Signal = \"answer\"",
			"\tSignalIceCandidates Signal = \"iceCandidates\"",
			"\tSignalEnd           Signal = \"end\"",
			")",
		},
	},
	"Chime": {ports: []portDecl{{
		name: "ChimePort",
		doc: []string{
			"ChimePort is the device's chime: InstalledChimeSounds lists the",
			"sounds it has, read on every read of the attribute (an error fails",
			"the read), and PlayChimeSound plays one; an error answers its",
			"status, FAILURE without one (cluster/camera Chime, after",
			"connectedhomeip's ChimeDelegate).",
		},
		methods: []string{
			"InstalledChimeSounds() ([]ChimeSoundStruct, error)",
			"PlayChimeSound(ctx context.Context, chimeID uint8) error",
		},
	}}},
	"CameraAvSettingsUserLevelManagement": {ports: []portDecl{{
		name: "MechanicalPTZ",
		doc: []string{
			"MechanicalPTZ moves the camera (MPAN, MTILT, MZOOM) to a target",
			"the kit's server validated: it starts the movement and returns,",
			"and the host reports the end through the server's",
			"PhysicalMovementComplete; an error answers its status, FAILURE",
			"without one, and BUSY is how a device that cannot move now",
			"refuses (cluster/camera AvSettings, after connectedhomeip's",
			"CameraAvSettingsUserLevelManagementDelegate).",
		},
		methods: []string{
			"MptzSetPosition(ctx context.Context, target MPTZStruct) error",
			"MptzRelativeMove(ctx context.Context, target MPTZStruct) error",
		},
	}, {
		name:     "MechanicalPresets",
		optional: true,
		doc: []string{
			"MechanicalPresets is an optional interface of a [MechanicalPTZ]",
			"that MechanicalPresets (MPRESETS) requires: MptzMoveToPreset moves",
			"to a stored preset, MptzSavePreset and MptzRemovePreset are told",
			"before the server stores or erases one; an error refuses the",
			"command with its status.",
		},
		methods: []string{
			"MptzMoveToPreset(ctx context.Context, presetID uint8, target MPTZStruct) error",
			"MptzSavePreset(ctx context.Context, presetID uint8) error",
			"MptzRemovePreset(ctx context.Context, presetID uint8) error",
		},
	}, {
		name: "DigitalPTZ",
		doc: []string{
			"DigitalPTZ changes the viewport of a video stream (DPTZ):",
			"DptzSetViewport applies a viewport, DptzRelativeMove moves and",
			"zooms the current one and returns the viewport it arrived at;",
			"the stream is one the server knows. An error answers its status,",
			"FAILURE without one (cluster/camera AvSettings).",
		},
		methods: []string{
			"DptzSetViewport(ctx context.Context, videoStreamID uint16, viewport ViewportStruct) error",
			"DptzRelativeMove(ctx context.Context, req DptzRelativeMoveRequest) (ViewportStruct, error)",
		},
	}}},
}

func init() {
	for name, ps := range cameraPorts {
		ports[name] = ps
	}
}
