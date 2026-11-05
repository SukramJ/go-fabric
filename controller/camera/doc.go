// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package camera is the controller side of Matter camera streaming: how
// the controller picks, allocates, reuses and frees a camera's streams and
// keeps the WebRTC sessions it opens on them. It is a port of
// matterjs-server's stream management at 0bd06f2
// (github.com/matter-js/matterjs-server,
// packages/ws-controller/src/camera/), whose behaviour this package
// mirrors; matter.js itself models only the WebRTC requestor of a camera
// controller, so this package is ahead of matter.js.
//
// This first part holds the policies, pure functions over small types:
//
//   - the stream policy (policy_stream.go): the video window a request
//     asks for (computeVideoEnvelope), the reuse, degraded and eviction
//     choices, and the audio envelope; with the window search
//     (policy_stream_search.go) that walks a plan's windows best first
//     against the camera's answers and the encoder budget;
//   - the SDP policy (policy_sdp.go, policy_sdp_levels.go): what an offer
//     constrains — media dispositions, per-codec decode ceilings from
//     a=fmtp and the H.264 / H.265 level tables, talkback — and the
//     rewrites of an offer without talkback and of an answer narrowed to
//     the offer's directions;
//   - the snapshot policy (policy_snapshot.go) and the overlay policy
//     (policy_overlay.go);
//   - the provider-command boundary (provider_arguments.go): a client's
//     WebRTCTransportProvider payload, keys matched as matterjs-server
//     matches them, checked against the generated model and decoded into
//     the generated request;
//   - the field ranges of the model (field_ranges.go) and the session
//     registry (registry.go), which ends and announces every WebRTC
//     session once, whatever order the camera's reports, stops, releases
//     and peer ends arrive in.
//
// The typed errors (errors.go) map one-to-one onto matterjs-server's
// camera error codes 102 to 106. The camera quirks matterjs-server works
// around are named constants in types.go.
//
// The service that composes these over a controller's nodes, the
// sessions and the signalling channel follow in this package (phase 37
// wave C); until then nothing outside it imports it.
package camera
