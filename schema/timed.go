// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

// Timed-interaction conformance (Matter §8.7). A command whose matter.js model
// element carries the "T" access quality (Access.Timed.Required — see
// ../matter.js/packages/model/src/aspects/Access.ts:38) MUST be invoked inside
// a Timed interaction. matter.js enforces this server-side in
// ../matter.js/packages/protocol/src/action/server/CommandInvokeResponse.ts:292-295:
//
//	if (limits.timed && !this.session.timed) { …; return #addStatus(…, Status.NeedsTimedInteraction); }
//
// i.e. a timed-required command received outside a valid timed window is
// rejected with NEEDS_TIMED_INTERACTION regardless of the InvokeRequest's own
// Timed flag.
//
// timedInvokePaths (timed_gen.go) is generated from parity/schema.json by
// script/generate_matter_schema.go: every request command of the snapshot
// whose effective access carries "timed". The endpoint dispatcher asks a
// cluster server implementing contract.TimedCommander first (a generated
// definition answers from its own model); this table is the fallback for
// hand-written servers and for paths no server resolves.

// IsTimedInvoke reports whether the (cluster, command) pair is timed-required
// per the matter.js "T" access quality — it may only be invoked inside a Timed
// interaction. Mirrors matter.js Access.Timed.Required
// (CommandInvokeResponse.ts:292 `limits.timed`).
func IsTimedInvoke(clusterID, commandID uint32) bool {
	cmds, ok := timedInvokePaths[clusterID]
	if !ok {
		return false
	}
	_, ok = cmds[commandID]
	return ok
}
