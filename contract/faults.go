// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

// Faults are the faults a node currently detects, as the root
// GeneralDiagnostics cluster serves them: ActiveHardwareFaults,
// ActiveRadioFaults and ActiveNetworkFaults, each a list of the cluster's
// HardwareFaultEnum, RadioFaultEnum and NetworkFaultEnum values in the
// order they were detected. A value listed twice is served once
// (Matter 1.6.1 Core §11.12.6.6: no more than one instance of a value).
type Faults struct {
	Hardware []uint8
	Radio    []uint8
	Network  []uint8
}

// FaultSource is the host port of the root GeneralDiagnostics' fault
// lists: a host whose device detects hardware, radio or network faults
// supplies them here, and the node serves the three lists and emits
// HardwareFaultChange, RadioFaultChange and NetworkFaultChange with the
// current and the previous list whenever a list changes — what matter.js
// GeneralDiagnosticsServer's registerHardwareFault / clearHardwareFault
// and their radio and network siblings do
// (packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts:296-411
// at db021da0). A node without a FaultSource serves neither the lists nor
// the events.
//
// MatterActiveFaults returns the faults detected now; the source fires
// its [ChangeNotifier] callback after a change, and the node reads the
// lists again later, on a goroutine of its own: the callback returns at
// once and calls nothing back, so the host may fire it under the lock its
// MatterActiveFaults takes, or synchronously while the node subscribes. A
// notification that changes nothing emits nothing. The node detaches the
// callback when it stops.
type FaultSource interface {
	MatterActiveFaults() Faults
	ChangeNotifier
}
