// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modeselect

// The ModeSelect list and label bounds, from the model, beside the
// generated definition so a declaration-only host reaches them: the host's
// cluster/modeselect.ModeSource.SupportedModes must stay within them. They are not applied
// by the server or the encoder — matter.js trims nothing either; its
// validator refuses an out-of-bounds value with a ConstraintError
// (packages/node/src/behavior/state/validation/constraint.ts). A list
// returned past them goes on the wire as given.
const (
	// LabelMaxBytes is the ModeOptionStruct Label constraint "max 64"
	// (mode-select-cluster.element.ts:63), in UTF-8 bytes.
	LabelMaxBytes = 64

	// SemanticTagsMaxEntries is the ModeOptionStruct SemanticTags
	// constraint "max 64" (mode-select-cluster.element.ts:66), in list
	// entries.
	SemanticTagsMaxEntries = 64
)

// SupportedModesMaxEntries is the SupportedModes constraint "max 255"
// (matter.js mode-select-cluster.element.ts:37), in list entries: a bound
// on the host's cluster/modeselect.ModeSource.SupportedModes, which the
// server returns as given.
const SupportedModesMaxEntries = 255
