// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package camera contains the servers of the camera surface (milestone 6,
// concept v10 §C waves 26-A1 to 26-A3): the signalling side of a WebRTC
// stream, the chime, the camera's pan, tilt and zoom, its streams, its
// zones and its transports.
//
//   - [Requestor] serves WebRtcTransportRequestor (0x0554) after matter.js's
//     WebRtcTransportRequestorServer
//     (packages/node/src/behaviors/web-rtc-transport-requestor/
//     WebRtcTransportRequestorServer.ts at 862ad75f): the session table, the
//     peer's identity checks on Offer, Answer, IceCandidates and End, and the
//     signalling handed to the host's [RequestorPort].
//   - [Chime] serves Chime (0x0556) and [AvSettings] serves
//     CameraAvSettingsUserLevelManagement (0x0552). matter.js serves both
//     with the generated behaviour only (ChimeServer.ts,
//     CameraAvSettingsUserLevelManagementServer.ts at the pin), so their
//     rules are connectedhomeip's at the harness commit 6170af84
//     (src/app/clusters/chime-server/ChimeCluster.cpp,
//     src/app/clusters/camera-av-settings-user-level-management-server/
//     CameraAvSettingsUserLevelManagementLogic.cpp), cited by line; the
//     Ahead-of-matter.js register in notes/parity/by_design.md lists them.
//   - [AvStream] serves CameraAvStreamManagement (0x0551) and [Zones]
//     ZoneManagement (0x0550), both after connectedhomeip as well
//     (src/app/clusters/camera-av-stream-management-server/
//     CameraAVStreamManagementCluster.cpp, src/app/clusters/
//     zone-management-server/ZoneManagementCluster.cpp and zone-geometry.h
//     at 6170af84): the allocated streams behind a port per encoder and the
//     capture, reference-counted for the transports; the zones and
//     triggers behind the [ZonePort], with chip's polygon rules
//     ([ZoneSelfIntersecting]).
//   - [Provider] serves WebRtcTransportProvider (0x0553) and [PushTransport]
//     serves PushAvStreamTransport (0x0555), the camera's two transports, after
//     connectedhomeip (src/app/clusters/webrtc-transport-provider-server/
//     WebRTCTransportProviderCluster.cpp, push-av-stream-transport-server/
//     PushAVStreamTransportLogic.cpp at 6170af84): the WebRTC sessions and
//     the push connections, each request checked against the camera's
//     [StreamSource] and, for a push transport, its [ZoneSource] and the
//     node's TLS servers (cluster/tls, node.Config.TLS) before the host's [ProviderPort] or
//     [PushTransportPort] runs it. The camera's own signals to the
//     requestor — [Provider.Offer], [Provider.Answer], [Provider.IceCandidates],
//     [Provider.End] — follow the camera app's provider manager
//     (examples/camera-app/linux/src/clusters/webrtc-provider/
//     webrtc-provider-manager.cpp at 6170af84) over the [PeerConnector] the
//     node attaches.
//
// Each server is built on its generated definition (cluster/spec/…, ADR
// 0013) and the generated server ([spec.Server]); its port is the
// generated one (<cluster>def.Declare), aliased here. The module holds the
// state and the rules and no media stack: the host plays the sound, moves
// the camera and runs the WebRTC peer connection (concept v10 §D).
//
// Persistence: chip persists SelectedChime and Enabled
// (ChimeCluster.cpp:83-104, :221-223, :233-235) and the camera's position,
// presets and DPTZ streams (CameraAvSettingsUserLevelManagementLogic.cpp:
// 139-332), the allocated streams, the priorities and the settings of the
// stream management (CameraAVStreamManagementCluster.cpp:1300-1850) and
// the Sensitivity of the zone management (ZoneManagementCluster.cpp:
// 139-168; its zones and triggers are the delegate's). The store port has
// no record for such attributes, so here the host persists them: every
// change is reported through the server's change notification (the typed
// OnChanged), and the declaration's Initial restores them.
package camera
