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
// The policies are pure functions over small types:
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
// camera error codes 102 to 106, and [ErrInvalidArgument] onto its error
// 8. The camera quirks matterjs-server works around are named constants in
// types.go.
//
// Over the policies runs the [Service], one per controller:
//
//   - the manager (manager*.go): the per-endpoint lock, the stream leases
//     and their grace window, the allocation ladders for video, audio and
//     snapshots, the session start and the stops and releases, every effect
//     of a request given back on its failure;
//   - the session layer (provider_sessions.go): ProvideOffer and
//     SolicitOffer with the controller's requestor endpoint as originating
//     endpoint, the stream fields reconciled with the camera's cluster
//     revision, the UTC-time retry, the requestor's session table kept in
//     step, the offers an answer is narrowed to;
//   - the watch (session_watch.go): each camera's CurrentSessions at the
//     end of a report batch, a restart told from a drop by the StartUp
//     event, and the sweep of the sessions a camera holds for this
//     controller that nothing here does;
//   - the facade (service.go, session.go, capabilities.go,
//     provider_route.go, reports.go): [Service.StartStream] and its
//     [Session] with the camera's signals, [Service.Snapshot],
//     [Service.Capabilities], [Service.ReleaseStream], the raw [Provider]
//     route and the [Observer].
//
// The controller serves the requestor the cameras call back
// (controller.Config.Camera) under its one operational identity; the
// service needs outgoing TCP (controller.Config.TCP.Outgoing), since
// ProvideOffer and ProvideAnswer carry the Large Message quality, and the
// cameras' reports through a [Reports] installed on the controller's
// configuration.
package camera
