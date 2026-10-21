// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
)

// The Switch cluster (0x003B): the server is cluster/switch
// (switchserver.Server), the port of matter.js SwitchServer.ts, which the
// measurement materialiser builds; this file keeps the model-side source
// interfaces the materialiser reads and the event payload aliases.
//
// The constants below name the cluster's identity from its generated
// definition (cluster/spec/switchcluster, ADR 0013, from matter.js
// switch.element.ts).
const (
	matterClusterGenericSwitch = swdef.ClusterID

	// MatterEventSwitchLatched fires when a latching switch flips
	// position (LS feature).
	MatterEventSwitchLatched = swdef.EventSwitchLatched
	// MatterEventInitialPress fires on the first press (MS / AS).
	MatterEventInitialPress = swdef.EventInitialPress
	// MatterEventLongPress fires once the long-press threshold is
	// crossed (MSL feature).
	MatterEventLongPress = swdef.EventLongPress
	// MatterEventShortRelease fires when a short press ends (MSR).
	MatterEventShortRelease = swdef.EventShortRelease
	// MatterEventLongRelease fires when a long press ends (MSL).
	MatterEventLongRelease = swdef.EventLongRelease
	// MatterEventMultiPressOngoing fires during multi-press capture
	// (MSM feature).
	MatterEventMultiPressOngoing = swdef.EventMultiPressOngoing
	// MatterEventMultiPressComplete fires once the multi-press window
	// closes (MSM feature).
	MatterEventMultiPressComplete = swdef.EventMultiPressComplete

	// switchClusterRevision is the definition's (Switch bumped 1→2 in
	// Matter 1.4 with the action-button (AS) feature codification).
	switchClusterRevision = swdef.Revision
)

// GenericSwitchSource is the model-side surface a HM button source
// (the per-channel press group, or a lone Button / Action DP) exposes.
// The cluster server reads `NumberOfPositions` once at construction
// and supports long-press recognition opt-in via `SupportsLongPress`.
type GenericSwitchSource interface {
	// MatterSwitchPositions returns the static NumberOfPositions
	// (Matter §1.13.5.1). Typical: 2 for a single button (idle +
	// pressed). 0 → cluster falls back to 2.
	MatterSwitchPositions() uint8
	// MatterSwitchSupportsLongPress reports whether the source can
	// distinguish long press from short press. When true, the
	// FeatureMap exposes MSL and the cluster forwards LongPress /
	// LongRelease events when the source fires them.
	MatterSwitchSupportsLongPress() bool
}

// GenericSwitchPositionSource is the optional source capability for a
// live CurrentPosition (Matter §1.13.5.2). Sources that run a press-
// cycle state machine report 1 (pressed) while a hold is open and 0
// (idle) otherwise, mirroring matter.js
// packages/node/src/behaviors/switch/SwitchServer.ts, where
// `state.currentPosition` moves to the pressed position for the
// duration of the press and returns to `momentaryNeutralPosition`
// (default 0) on release. Sources without the capability read as the
// constant neutral position.
type GenericSwitchPositionSource interface {
	MatterSwitchCurrentPosition() uint8
}

// GenericSwitchMultiPressSource is the optional source capability for
// multi-press recognition: MatterSwitchMultiPressMax returns the
// MultiPressMax attribute (Matter §1.13.5.3, "min 2"). A value of 2 or more
// makes the materialised server advertise MSM and count presses up to it
// (matter.js SwitchServer.ts:328-340); 0 (or 1) is no MSM.
type GenericSwitchMultiPressSource interface {
	MatterSwitchMultiPressMax() uint8
}

// GenericSwitchActionSource is the optional source capability for an
// action switch (AS): true makes the materialised server advertise AS
// instead of MSR — one press reported per press cycle, with no short
// release and no count while the sequence runs (matter.js
// SwitchServer.ts:248-251, :337-339) — and MSM, which AS makes mandatory
// (switch.element.ts conformance "AS, [MS & MSR]").
type GenericSwitchActionSource interface {
	MatterSwitchActionSwitch() bool
}

// The Switch{event}Event types are the event payloads cluster/switch
// emits: the generated payloads of switch.element.ts, a structure
// with its one uint8 field at context tag 0 — InitialPress / LongPress
// carry NewPosition, ShortRelease / LongRelease PreviousPosition. They
// encode themselves (spec.Encodable).
//
// MultiPressOngoing / MultiPressComplete have no alias here: their payloads
// are cluster/spec/switchcluster's, which cluster/switch emits.

// SwitchInitialPressEvent is the InitialPress (0x01) payload.
type SwitchInitialPressEvent = swdef.InitialPressEvent

// SwitchLongPressEvent is the LongPress (0x02) payload.
type SwitchLongPressEvent = swdef.LongPressEvent

// SwitchShortReleaseEvent is the ShortRelease (0x03) payload.
type SwitchShortReleaseEvent = swdef.ShortReleaseEvent

// SwitchLongReleaseEvent is the LongRelease (0x04) payload.
type SwitchLongReleaseEvent = swdef.LongReleaseEvent
