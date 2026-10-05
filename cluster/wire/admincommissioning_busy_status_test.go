// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

// TestAdmCommBusyIsTheClusterSpecificStatus pins Busy as FAILURE with the
// AdministratorCommissioning cluster status Busy (0x02), as matter.js
// AdministratorCommissioningServer.ts throws BusyError; the IM-level BUSY
// (0x9C) is a different status that TC-CADMIN-1.5 step 14 rejects.
func TestAdmCommBusyIsTheClusterSpecificStatus(t *testing.T) {
	t.Parallel()
	var cs im.MatterClusterStatusError
	if !errors.As(ErrAdmCommBusy, &cs) {
		t.Fatal("ErrAdmCommBusy carries no cluster status")
	}
	if cs.MatterStatusCode() != im.StatusFailure || cs.MatterClusterStatus() != 0x02 {
		t.Fatalf("Busy = status %v, cluster status %#x; want FAILURE with 0x02", cs.MatterStatusCode(), cs.MatterClusterStatus())
	}
}
