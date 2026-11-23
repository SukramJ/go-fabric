// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package sessionseam carries the one-way seams into the session table of
// secure/operational that are not public API: the install of a session
// under a chosen id, which only a test fixture may ask for, and the close
// of one installed session by itself rather than by its id, which the node
// facade needs right after an install, and the CASE responder's
// establishing session — installed before its handshake completed and
// completed once it did — which only the node facade's responder drives.
//
// Direction of the edge: package secure/operational installs every hook
// from its init; node and the module's tests read them. The manager and
// the entry are threaded through as any because this package must not
// import secure/operational — operational imports this one to install the
// hooks. A hook handed another type reports an error or false.
//
// The public install takes a Stake (Manager.OpenFromSigmaWithStake and
// its siblings), and the bare-id installs are deprecated (review II,
// N3-findev-1, decisions 153, 159, 160): a production install under an id
// nobody staked is the defect they close. A fixture that must name its
// session ids — a parity case that mirrors matter.js's ids, a test that
// keys an exchange by a known id — has no stake to hand over, so the
// install under a chosen id lives here, out of every consumer's reach.
package sessionseam

import "thingfabric.dev/go-fabric/secure/sigma"

// OpenAt installs a CASE session under id in m (a
// *operational.Manager) exactly as the deprecated
// Manager.OpenFromSigmaWithID (initiator false) or
// Manager.OpenFromSigmaAsInitiatorWithID (initiator true) does — a
// placeholder under id is overwritten, a live session under it closed — and
// returns the *operational.Entry. TEST SUPPORT ONLY: production code
// installs under a Stake.
var OpenAt func(m any, id uint16, initiator bool, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (entry any, err error)

// CloseEntry closes e (an *operational.Entry) in m (a
// *operational.Manager) only while e still holds its session id, as
// Manager.Close would close that id, and reports whether it did. An id
// another session or stake holds by then is left alone.
var CloseEntry func(m, e any) bool

// OpenEstablishing installs a CASE responder session under stake (an
// operational.Stake) in m (a *operational.Manager) as
// Manager.OpenFromSigmaWithStake does, but establishing: it receives and
// routes messages, while the pickers, the session listing and the per-peer
// cap leave it out until [CompleteEstablishment] (matter.js
// CaseServer.ts:177-190, :314-327 at b25ed960, `establishing: true`).
// Returns the *operational.Entry.
var OpenEstablishing func(m, stake any, fabricIndex uint8, localNodeID, peerNodeID uint64, peerSessionID uint16, peerCATs []uint32, keys sigma.SessionKeys) (entry any, err error)

// CompleteEstablishment ends the establishment of e (an *operational.Entry
// [OpenEstablishing] installed) in m: the pickers return it from now on
// and the per-peer cap counts it and runs. Reports true when e is
// established — also when it already was — and false when it was closed
// while establishing (matter.js NodeSession.ts:241-251 at b25ed960).
var CompleteEstablishment func(m, e any) bool
