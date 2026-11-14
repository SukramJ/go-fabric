// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"slices"
	"testing"

	gendiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/contract"
)

// lateFaultHost is a fault port whose faults change while the server
// subscribes, before the subscription is in place: the notification of
// that change reaches nobody.
type lateFaultHost struct {
	faultHost
	late contract.Faults
}

func (h *lateFaultHost) OnMatterValueChanged(cb func()) func() {
	h.mu.Lock()
	h.faults = h.late
	h.mu.Unlock()
	return h.faultHost.OnMatterValueChanged(cb)
}

// TestGeneralDiagnosticsFaultsChangedWhileAttaching pins R2-P1-core-09: a
// fault the host registers while SetFaultSource attaches — after the old
// order's read, before its subscription — is served, not lost until the
// host's next notification.
func TestGeneralDiagnosticsFaultsChangedWhileAttaching(t *testing.T) {
	t.Parallel()
	sensor := uint8(gendiagdef.HardwareFaultSensor)
	g := NewGeneralDiagnostics(BootReasonPowerOnReboot)
	t.Cleanup(g.Stop)
	host := &lateFaultHost{late: contract.Faults{Hardware: []uint8{sensor}}}
	g.SetFaultSource(host)
	settle(t, g)
	if got := readFaults(t, g, gendiagAttrActiveHardwareFaults); !slices.Equal(got, []uint8{sensor}) {
		t.Fatalf("ActiveHardwareFaults %v, want the fault registered while the server attached", got)
	}
}
