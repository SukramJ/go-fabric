// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"sync/atomic"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
)

// TestOtaApplierCloseWaitsForTheRestart pins the default OTA applier as
// the owner of its in-place restart (G1-14, ADR 0026 P5): close returns
// only after a restart in progress returned, and a closed applier starts
// none.
func TestOtaApplierCloseWaitsForTheRestart(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	a := (&otaApplier{requestor: func() *mattercore.OTASoftwareUpdateRequestor {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return nil // no requestor: the restart ends here
	}}).started()

	a.goRestart()
	<-entered
	closed := make(chan struct{})
	go func() {
		a.close()
		close(closed)
	}()
	// A close that does not wait returns at once; one that waits never
	// returns before the release, however long this wait is.
	select {
	case <-closed:
		t.Fatal("close returned while the restart still ran")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-closed

	a.goRestart() // closed: starts nothing
	a.close()
	if n := calls.Load(); n != 1 {
		t.Fatalf("restart ran %d times, want once", n)
	}
}
