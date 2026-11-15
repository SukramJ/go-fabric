// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import "context"

// ChangeNamingWriter is the optional capability a [ClusterServer]
// implements to name the attributes a controller's write changed: the
// refinement of [ChangeReportingWriter]'s one verdict for the cluster.
// matter.js notes a property in a commit's change list only when its value
// differs from the one held
// (packages/node/src/behavior/state/managed/Datasource.ts:1476-1484 at
// db021da0), advances the datasource's version once when the list is not
// empty (:1456-1457) and hands the list on as data (:1319, onChange); a
// server with this capability gives the dispatcher the same list.
//
// The endpoint dispatcher prefers it to MatterWriteChanged and
// MatterWrite: a successful write whose list is not empty advances the
// cluster's DataVersion once, and only the attributes it names are
// reported to subscribers — the written one only when it is named. An
// empty list is a write that changed nothing, with the meaning
// [ChangeReportingWriter] gives "unchanged". The list carries the result
// of the write (im.WriteResult.Commit).
//
// A type that embeds a server implementing this and declares its own
// MatterWrite or MatterWriteChanged declares its own MatterWriteCommit as
// well: the promoted method would apply the write without the type's.
type ChangeNamingWriter interface {
	// MatterWriteCommit applies the write as MatterWrite does and returns
	// the ids of the cluster's attributes it changed — the written one
	// when its value moved, and any other the write moves with it. The
	// order does not matter, and an id named twice counts once. changed
	// is ignored with a non-nil err: a refused write has changed nothing.
	MatterWriteCommit(ctx context.Context, attrID uint32, value any) (changed []uint32, err error)
}
