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
	"CameraAvStreamManagement": {
		ports: []portDecl{{
			name: "VideoStreamPort",
			doc: []string{
				"VideoStreamPort is the camera's video encoder (VDO): the kit's",
				"server (cluster/camera AvStream) validates a request against the",
				"cluster's rules before it calls VideoStreamAllocate with the",
				"requested stream (its id and reference count ignored), which",
				"allocates or reuses a stream and returns its id; the server then",
				"narrows a reused stream to the intersection of both ranges and",
				"calls VideoStreamAllocated with the stream as allocated, which",
				"is where the host starts it. VideoStreamModify and",
				"VideoStreamDeallocate act on a stream the server knows. An error",
				"answers its status, FAILURE without one (after connectedhomeip's",
				"CameraAVStreamManagementDelegate).",
			},
			methods: []string{
				"VideoStreamAllocate(ctx context.Context, args VideoStreamStruct) (videoStreamID uint16, err error)",
				"VideoStreamAllocated(ctx context.Context, stream VideoStreamStruct, action StreamAllocationAction)",
				"VideoStreamModify(ctx context.Context, videoStreamID uint16, watermarkEnabled, osdEnabled *bool) error",
				"VideoStreamDeallocate(ctx context.Context, videoStreamID uint16) error",
			},
		}, {
			name: "AudioStreamPort",
			doc: []string{
				"AudioStreamPort is the camera's audio encoder (ADO):",
				"AudioStreamAllocate allocates or reuses a stream for a request",
				"the kit's server validated (its id and reference count ignored)",
				"and returns its id; AudioStreamDeallocate releases a stream the",
				"server knows. An error answers its status, FAILURE without one.",
			},
			methods: []string{
				"AudioStreamAllocate(ctx context.Context, args AudioStreamStruct) (audioStreamID uint16, err error)",
				"AudioStreamDeallocate(ctx context.Context, audioStreamID uint16) error",
			},
		}, {
			name: "SnapshotStreamPort",
			doc: []string{
				"SnapshotStreamPort is the camera's snapshot encoder (SNP):",
				"SnapshotStreamAllocate allocates or reuses a stream for a request",
				"the kit's server validated and matched against",
				"SnapshotCapabilities (FrameRate is the requested MaxFrameRate,",
				"EncodedPixels and HardwareEncoder the matching capability's; the",
				"id and reference count ignored) and returns its id;",
				"SnapshotStreamModify and SnapshotStreamDeallocate act on a stream",
				"the server knows. An error answers its status, FAILURE without one.",
			},
			methods: []string{
				"SnapshotStreamAllocate(ctx context.Context, args SnapshotStreamStruct) (snapshotStreamID uint16, err error)",
				"SnapshotStreamModify(ctx context.Context, snapshotStreamID uint16, watermarkEnabled, osdEnabled *bool) error",
				"SnapshotStreamDeallocate(ctx context.Context, snapshotStreamID uint16) error",
			},
		}, {
			name: "SnapshotCapturePort",
			doc: []string{
				"SnapshotCapturePort takes a picture (SNP): CaptureSnapshot is",
				"called once the kit's server checked that a snapshot stream",
				"exists (the named one, when snapshotStreamID is not nil), the",
				"resolution and the privacy modes, and returns the image; an image",
				"larger than the server's bound answers RESOURCE_EXHAUSTED, an",
				"error its status, FAILURE without one.",
			},
			methods: []string{
				"CaptureSnapshot(ctx context.Context, snapshotStreamID *uint16, resolution VideoResolutionStruct) (CaptureSnapshotResponse, error)",
			},
		}},
		types: []string{
			"// StreamAllocationAction tells a VideoStreamPort how a stream came",
			"// out of an allocation (connectedhomeip's StreamAllocationAction,",
			"// CameraAVStreamManagementCluster.h:127-132).",
			"type StreamAllocationAction uint8",
			"",
			"// The allocation actions.",
			"const (",
			"\t// StreamNewAllocation is a fresh stream: start it.",
			"\tStreamNewAllocation StreamAllocationAction = iota",
			"\t// StreamModification is a reused stream whose ranges narrowed:",
			"\t// restart it if it runs.",
			"\tStreamModification",
			"\t// StreamReuse is a reused stream left as it was: nothing to do.",
			"\tStreamReuse",
			")",
		},
	},
	"ZoneManagement": {ports: []portDecl{{
		name: "ZonePort",
		doc: []string{
			"ZonePort is the camera's zone and trigger store (cluster/camera",
			"Zones, after connectedhomeip's ZoneManagement Delegate): the kit's",
			"server validates a zone (its constraints, the vertex bounds,",
			"duplicates and self-intersection) or a trigger before it calls the",
			"port, and keeps Zones and Triggers. CreateTwoDCartesianZone returns",
			"the new zone's id. An error answers its status, FAILURE without one.",
		},
		methods: []string{
			"CreateTwoDCartesianZone(ctx context.Context, zone TwoDCartesianZoneStruct) (zoneID uint16, err error)",
			"UpdateTwoDCartesianZone(ctx context.Context, zoneID uint16, zone TwoDCartesianZoneStruct) error",
			"RemoveZone(ctx context.Context, zoneID uint16) error",
			"CreateTrigger(ctx context.Context, trigger ZoneTriggerControlStruct) error",
			"UpdateTrigger(ctx context.Context, trigger ZoneTriggerControlStruct) error",
			"RemoveTrigger(ctx context.Context, zoneID uint16) error",
		},
	}}},
	// Brief 26-A3: the camera's two transports.
	"WebRtcTransportProvider": {
		ports: []portDecl{{
			name: "ProviderPort",
			doc: []string{
				"ProviderPort is the camera's WebRTC stack (cluster/camera Provider,",
				"after connectedhomeip's WebRTCTransportProvider Delegate): the",
				"kit's server validates each command — its constraints, the privacy",
				"modes, the stream usage and the streams against the camera's",
				"CameraAvStreamManagement, the requestor's session — before it",
				"calls the port, and keeps CurrentSessions. SolicitOffer and",
				"ProvideOffer return the session to store; deferredOffer reports a",
				"camera in standby that sends its offer later. The host counts the",
				"streams a session uses (the AvStream's reference counts). An error",
				"answers its status, FAILURE without one; EndSession's ends the",
				"session too.",
			},
			methods: []string{
				"SolicitOffer(ctx context.Context, args OfferArgs) (session WebRTCSessionStruct, deferredOffer bool, err error)",
				"ProvideOffer(ctx context.Context, args OfferArgs) (session WebRTCSessionStruct, err error)",
				"ProvideAnswer(ctx context.Context, sessionID uint16, sdp string) error",
				"ProvideIceCandidates(ctx context.Context, sessionID uint16, candidates []ICECandidateStruct) error",
				"EndSession(ctx context.Context, sessionID uint16, reason uint8) error",
			},
		}},
		types: []string{
			"// OfferArgs is a SolicitOffer or ProvideOffer the server validated",
			"// (connectedhomeip Delegate::OfferRequestArgs and",
			"// ProvideOfferRequestArgs, WebRTCTransportProviderCluster.h:50-68):",
			"// the session id the server allocated (or the re-offered session's),",
			"// the stream usage, the streams after the server's checks and",
			"// selection (nil: the kind was not requested), the ICE settings, the",
			"// requestor's node, fabric and endpoint, and — for ProvideOffer —",
			"// the SDP offer.",
			"type OfferArgs struct {",
			"\tSessionID             uint16",
			"\tStreamUsage           StreamUsageEnum",
			"\tVideoStreams          *[]uint16",
			"\tAudioStreams          *[]uint16",
			"\tIceServers            *[]ICEServerStruct",
			"\tIceTransportPolicy    *string",
			"\tMetadataEnabled       *bool",
			"\tPeerNodeID            uint64",
			"\tFabricIndex           uint8",
			"\tOriginatingEndpointID uint16",
			"\tSdp                   string",
			"}",
		},
	},
	"PushAvStreamTransport": {
		ports: []portDecl{{
			name: "PushTransportPort",
			doc: []string{
				"PushTransportPort is the camera's push uploader (cluster/camera",
				"PushTransport, after connectedhomeip's PushAvStreamTransportDelegate,",
				"push-av-stream-transport-delegate.h:42-348): the kit's server",
				"validates each command — the transport options, the TLS endpoint,",
				"the URL, the streams against the camera's CameraAvStreamManagement",
				"and the motion zones against its ZoneManagement — before it calls",
				"the port, and keeps CurrentConnections. TransportBusy reports a",
				"connection that uploads now; ValidateBandwidthLimit refuses a",
				"transport the network cannot carry; CMAFSessionNumber is the CMAF",
				"session number of the events. An error answers its status,",
				"FAILURE without one.",
			},
			methods: []string{
				"AllocatePushTransport(ctx context.Context, connectionID uint16, fabricIndex uint8, options TransportOptionsStruct, credentials TLSCredentials) error",
				"DeallocatePushTransport(ctx context.Context, connectionID uint16) error",
				"ModifyPushTransport(ctx context.Context, connectionID uint16, options TransportOptionsStruct) error",
				"SetTransportStatus(ctx context.Context, connectionIDs []uint16, status TransportStatusEnum) error",
				"ManuallyTriggerTransport(ctx context.Context, connectionID uint16, reason TriggerActivationReasonEnum, timeControl *TransportMotionTriggerTimeControlStruct) error",
				"TransportBusy(connectionID uint16) bool",
				"ValidateBandwidthLimit(ctx context.Context, usage StreamUsageEnum, videoStreamID, audioStreamID *uint16) error",
				"CMAFSessionNumber(connectionID uint16) (sessionNumber uint64, ok bool)",
			},
		}, {
			name: "TLSEndpoints",
			doc: []string{
				"TLSEndpoints is the node's TlsClientManagement server, where a",
				"transport's TLSEndpointID must name a provisioned endpoint of the",
				"accessing fabric with a client certificate (its typed server's",
				"Host is one). Without it every AllocatePushTransport is",
				"InvalidTLSEndpoint, as chip answers without a TLS client delegate.",
			},
			methods: []string{
				"ProvisionedEndpoint(fabricIndex uint8, endpointID uint16) (tlsclientmanagement.TLSEndpointStruct, bool)",
			},
		}, {
			name: "TLSCertificates",
			doc: []string{
				"TLSCertificates is the node's TlsCertificateManagement server, which",
				"holds the endpoint's root and client certificates (its typed",
				"server's Host is one). Without it the transport is allocated",
				"without certificates, as chip's.",
			},
			methods: []string{
				"RootCertificate(fabricIndex uint8, caid uint16) (tlscertificatemanagement.TLSCertStruct, bool)",
				"ClientCertificate(fabricIndex uint8, ccdid uint16) (tlscertificatemanagement.TLSClientCertificateDetailStruct, bool)",
			},
		}},
		types: []string{
			"// TLSCredentials are what a transport uploads with: its TLS",
			"// endpoint and the endpoint's root and client certificates, DER",
			"// (connectedhomeip's SetTLSCerts, PushAVStreamTransportLogic.cpp",
			"// :920-1011, which runs just before AllocatePushTransport). The",
			"// certificates are empty without a TLSCertificates lookup.",
			"type TLSCredentials struct {",
			"\tEndpoint                 tlsclientmanagement.TLSEndpointStruct",
			"\tRootCertificate          []byte",
			"\tClientCertificate        []byte",
			"\tIntermediateCertificates [][]byte",
			"}",
		},
		imports: []string{
			"github.com/SukramJ/go-fabric/cluster/spec/tlscertificatemanagement",
			"github.com/SukramJ/go-fabric/cluster/spec/tlsclientmanagement",
		},
	},
}

func init() {
	for name, ps := range cameraPorts {
		ports[name] = ps
	}
}
