// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/im"
)

// TestFlushSessionFollowsAMovedSubscription locks the invariant that
// makes matter.js's per-call flush session (ServerSubscription close's
// flushViaSession argument) no gap here: the flush of a session's
// subscriptions selects them by their current session, so a subscription
// moved onto a newer session is flushed with that one and not with the
// one it left. matter.js hands every subscription of a closing session
// that very session (packages/protocol/src/session/NodeSession.ts:383-395
// at b25ed960, closeSubscriptions, `flush ? this : undefined` at :386),
// and a subscription is a member of the set of the session it currently
// reports on: activation adds it there
// (packages/node/src/node/server/ServerSubscription.ts:425 at b25ed960),
// and #follow moves it into the newer session's set
// (ServerSubscription.ts:820-834 at b25ed960). Review II, pin lift
// b25ed960, F-b25ed960-43 (P6-B).
func TestFlushSessionFollowsAMovedSubscription(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, rs, _ := newSupersessionManager(t)
	sub := subscribeOn(t, m, 1, false)

	if moved := m.MoveSession(1, 2); !slices.Equal(moved, []uint32{sub.ID}) {
		t.Fatalf("MoveSession moved %v; want [%d]", moved, sub.ID)
	}
	m.OnAttributeChanged(supersessionPath)

	m.FlushSession(ctx, 1)
	if got := rs.reports(); len(got) != 0 {
		t.Fatalf("FlushSession of the former session sent %+v; want nothing — the subscription left it", got)
	}

	m.FlushSession(ctx, 2)
	got := rs.reports()
	if len(got) != 1 {
		t.Fatalf("FlushSession of the newer session sent %+v; want one flush report", got)
	}
	r := got[0]
	if !r.report.Flush || r.report.KeepAlive || r.session != 2 {
		t.Fatalf("flush report = %+v; want a Flush report handed over on session 2", r)
	}
	if !slices.Equal(r.report.Paths, []im.ConcreteAttributePath{supersessionPath}) {
		t.Fatalf("flush report paths = %v; want only the dirty %v", r.report.Paths, supersessionPath)
	}
	if got := sub.CurrentSessionID(); got != 2 {
		t.Fatalf("CurrentSessionID = %d; want 2", got)
	}
	if sub.IsClosed() {
		t.Fatal("the flush closed the subscription; it stays open to close with its session")
	}
}
