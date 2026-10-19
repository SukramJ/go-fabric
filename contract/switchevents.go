// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

// SwitchEventEmitter is the press-event sink of the Matter §1.13
// GenericSwitch cluster (0x003B), as seen from the model side. The
// bridge's `cluster/switch.Server` implements the four Fire*
// methods; the model's button group calls them while narrating one
// physical HM press as the ordered Matter gesture sequence
// (InitialPress → ShortRelease, or InitialPress → LongPress →
// LongRelease).
//
// It lives here rather than on either side because both sides have to
// name the SAME interface type: Go interface satisfaction requires
// identical parameter types, so a private copy on the bridge side
// could never be satisfied by the model's `WireMatterSwitchHandler`
// method set. The bridge's type assertion would then silently never
// match and no press would ever reach a commissioner — a failure with
// no compile error and no runtime error, only silence.
//
// Mirrors matter.js packages/node/src/behaviors/switch/SwitchServer.ts,
// which derives the same sequence from one currentPosition stream per
// switch.
type SwitchEventEmitter interface {
	// FireInitialPress reports the switch leaving its idle position.
	FireInitialPress(newPosition uint8)

	// FireShortRelease closes a short-press gesture.
	FireShortRelease(previousPosition uint8)

	// FireLongPress reports the hold threshold being crossed.
	FireLongPress(newPosition uint8)

	// FireLongRelease closes a long-press gesture.
	FireLongRelease(previousPosition uint8)
}

// SwitchPositionSink is the position input of the Matter §1.13 Switch
// cluster (0x003B), as seen from the model side: a source that knows its
// switch's position — rather than the gestures a press makes — reports
// each position, and the server derives every event from that stream.
// The bridge's `cluster/switch.Server` implements it and hands it to a
// source that implements `WireMatterSwitchPositions` at every reassembly,
// the way it hands a gesture source its [SwitchEventEmitter].
//
// Mirrors matter.js packages/node/src/behaviors/switch/SwitchServer.ts at
// 862ad75f: SetRawPosition is the `rawPosition` state (debounced by the
// server's debounce delay, :128-151), SetCurrentPosition a write of
// `currentPosition` by an application that debounced the position itself
// (:177-190). Report positions through one of the two, not both (:63-64).
// A position outside 0..NumberOfPositions-1 is refused with an error that
// answers CONSTRAINT_ERROR (:122-126).
type SwitchPositionSink interface {
	// SetRawPosition reports a raw, not yet debounced position.
	SetRawPosition(position uint8) error

	// SetCurrentPosition reports a debounced position.
	SetCurrentPosition(position uint8) error
}
