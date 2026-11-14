// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"sync"

	"github.com/SukramJ/go-fabric/im"
)

// WriteChanges records, for one write interaction, which written
// attributes changed nothing: the verdict a server gives through
// [contract.ChangeReportingWriter], per (endpoint, cluster, attribute).
// The transport that reports written attributes to subscribers attaches
// one with [WithWriteChanges] and leaves out the paths it holds as
// unchanged, as matter.js reports only the properties a commit changed
// (packages/node/src/behavior/state/managed/Datasource.ts:1319 at
// db021da0, onChange with the change list the commit collects at
// :1446-1454 and :1484). [TopologyDispatcher.WriteAuthorized] records every
// successful write; a dispatcher that records nothing leaves every path
// reported. The zero value is ready to use and safe for concurrent use.
type WriteChanges struct {
	mu sync.Mutex
	// changed is, per written attribute, whether any successful write of
	// it in the interaction changed the cluster.
	changed map[writtenAttribute]bool
}

// writtenAttribute is the concrete attribute a write landed on, without
// the path's list index.
type writtenAttribute struct {
	endpoint  uint16
	cluster   uint32
	attribute uint32
}

func writtenAt(p im.ConcreteAttributePath) writtenAttribute {
	return writtenAttribute{p.Endpoint, p.Cluster, p.Attribute}
}

// record notes a successful write of p: a change of the attribute in the
// interaction outlasts an unchanged write of it before or after — a
// chunked list's REPLACE of the list it held and the ADD that follows
// change the list.
func (w *WriteChanges) record(p im.ConcreteAttributePath, changed bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.changed == nil {
		w.changed = make(map[writtenAttribute]bool, 1)
	}
	key := writtenAt(p)
	w.changed[key] = w.changed[key] || changed
}

// Unchanged reports whether every successful write of p in the
// interaction changed nothing. A path no write was recorded for is not
// unchanged: the caller reports it as it always has.
func (w *WriteChanges) Unchanged(p im.ConcreteAttributePath) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	changed, written := w.changed[writtenAt(p)]
	return written && !changed
}

type writeChangesKey struct{}

// WithWriteChanges attaches w to ctx: the write interaction ctx carries
// records into it.
func WithWriteChanges(ctx context.Context, w *WriteChanges) context.Context {
	return context.WithValue(ctx, writeChangesKey{}, w)
}

// writeChangesFrom is the record ctx carries, nil without one.
func writeChangesFrom(ctx context.Context) *WriteChanges {
	w, _ := ctx.Value(writeChangesKey{}).(*WriteChanges)
	return w
}
