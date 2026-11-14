// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"testing"
)

// TestServiceOutlivingItsControllerDoesNotPanic (review II, rework of
// R2-P2-stop): once the controller's Close has begun its Registry is nil.
// A camera service that outlives the controller — a late report hook, an
// attach racing Close, a command — finds no node instead of
// dereferencing it.
func TestServiceOutlivingItsControllerDoesNotPanic(t *testing.T) {
	d := newDevSite(t)
	d.startService()
	addr, _ := d.n.PeerAddress()
	d.run(d.c.Close)
	try := func(what string, fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s after the controller's Close panicked: %v", what, r)
			}
		}()
		fn()
	}
	try("clientIO.node", func() {
		if n := d.svc.io.node(addr.NodeID); n != nil {
			t.Errorf("clientIO.node after Close = %v, want nil", n)
		}
	})
	try("Reports.attach", func() { d.rep.attach(d.svc) })
	try("Reports.with", func() { d.rep.with(d.n.ID(), func(*Service, uint64) {}) })
}
