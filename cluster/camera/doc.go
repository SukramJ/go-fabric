// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package camera contains the servers of the camera surface (milestone 6,
// concept v10 §C wave 26-A1): the signalling side of a WebRTC stream, the
// chime, and the camera's pan, tilt and zoom.
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
// 139-332). The store port has no record for such attributes, so here the
// host persists them: every change is reported through the server's change
// notification (the typed OnChanged), and the declaration's Initial
// restores them.
package camera
