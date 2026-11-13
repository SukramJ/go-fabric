// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"errors"
	"testing"
)

// TestStartAfterShutdownIsRefused (review II, R2-P1-cam-b-04): a start that
// registers after the Service's Close claimed every session — it passed
// StartStream's closed check just before Close ran — is refused with
// ErrClosed and opens no session. Before, the registry had no closed
// state: the start established and tracked a session nothing would end.
func TestStartAfterShutdownIsRefused(t *testing.T) {
	t.Parallel()
	m, io, _ := managerWith(fixtureState(), sliceBNumbering())
	m.shutdown(sliceBCtx)
	if _, err := sliceBStartKeyed(sliceBCtx, m, "conn-1", ""); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after the shutdown = %v, want ErrClosed", err)
	}
	if n := len(m.sessions.trackedSessions()); n != 0 {
		t.Fatalf("%d sessions tracked after the shutdown, want none", n)
	}
	for _, r := range io.recorded() {
		if r.command == callProvideOffer {
			t.Fatal("the refused start sent a ProvideOffer")
		}
	}
}
