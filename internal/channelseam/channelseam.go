// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package channelseam carries the one-way seam through which the bridge
// reaches the one piece of secure-session state that is written by the wire
// and by nothing else: the anchor of the session's inbound MRP
// duplicate-detection window.
//
// Direction of the edge: package secure/channel installs the hook below from
// its init; package bridge reads it. It mirrors
// [github.com/SukramJ/go-fabric/internal/bridgeseam] and exists for the same
// reason — a session's receive window is anchored by the first message that
// decrypts under it, so a consumer that never sends that message has no way
// to reach the state it would have left. Exposing an anchor method on
// channel.Session would make that a production affordance every consumer
// sees, when the only caller that can honestly use it is a test standing in
// for a handshake it did not perform.
//
// The seam is module-internal, so it cannot become a consumer-visible
// surface: an external module can import bridge/bridgetest and nothing else.
package channelseam

// AnchorPeerCounter primes a secure session's inbound
// duplicate-detection window on counter, exactly as the first message to
// decrypt under that session primes it: counter becomes the window's
// maximum, and — because a secure session's window anchors with an
// all-ones bitmap — every counter below it is thereafter a duplicate.
//
// The session is threaded through as any because this package must not
// import secure/channel: channel imports this one to install the hook, and
// the reverse edge would close an import cycle. The hook type-asserts back
// to *channel.Session and reports false when handed anything else, so a
// wrong type is a returned error at the bridgetest boundary rather than a
// panic inside it.
//
// Reports whether the window took counter as fresh. False means the type
// assertion failed, or the window had already recorded that counter — the
// session is not in the untouched post-handshake state this models.
// fabric:reachable:reason="assigned by secure/channel at init and read by bridge/seam.go:77; a function variable installed through a seam has no call edge the analyzer can follow"
var AnchorPeerCounter func(sess any, counter uint32) bool
