// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package onoff carries the OnOff cluster's Matter identity for every
// projection that exposes it.
//
// Four device projections declared this block independently — a generic
// switch plus the switch, light and siren profiles — with the matter.js
// citation copied along rather than the values. They had already drifted: the
// siren omitted Toggle while advertising the FeatureMap that makes it
// mandatory, which is the shape a controller can end a commissioning over.
// A revision bump or a newly feature-gated attribute had four edit sites and
// nothing tying them together.
//
// Nothing here is transcribed any more: the ids, the revision and the two
// Lighting lists come from the generated OnOff definition
// (cluster/spec/onoff, generated from matter.js on-off.element.ts, ADR 0013).
// Linking this package registers that definition, so the bridge decodes the
// OnOff request payloads into its typed request structs
// ([onoffdef.OnWithTimedOffRequest], [onoffdef.OffWithEffectRequest], …)
// with the statuses matter.js's request schema answers a rejected payload
// with.
package onoff

import (
	"github.com/SukramJ/go-fabric/cluster/spec"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
)

// ClusterID is the OnOff cluster id.
const ClusterID = onoffdef.ClusterID

// Device types whose projections carry this cluster.
const (
	// DeviceTypeOnOffLight is the OnOffLight device type (0x0100).
	DeviceTypeOnOffLight uint16 = 0x0100
	// DeviceTypeOnOffPlugInUnit is the OnOffPlugInUnit device type (0x010A).
	// It marks the LT feature mandatory on this cluster
	// (matter.js on-off-plug-in-unit.element.ts), which is why the LT-gated
	// attributes and commands below are not optional for it.
	DeviceTypeOnOffPlugInUnit uint16 = 0x010A
)

// Attribute ids. The four 0x40xx attributes carry conformance "LT"
// (matter.js packages/model/src/standard/elements/on-off.element.ts), so
// they exist exactly while [FeatureLighting] is advertised.
const (
	AttrOnOff              = onoffdef.AttrOnOff
	AttrGlobalSceneControl = onoffdef.AttrGlobalSceneControl
	AttrOnTime             = onoffdef.AttrOnTime
	AttrOffWaitTime        = onoffdef.AttrOffWaitTime
	AttrStartUpOnOff       = onoffdef.AttrStartUpOnOff
)

// FeatureLighting is the LT (Lighting) FeatureMap bit, as the uint32 a
// FeatureMap read carries.
const FeatureLighting = uint32(onoffdef.FeatureLighting)

// Command ids. Off is mandatory unconditionally; On and Toggle carry
// conformance "!OFFONLY" and so are mandatory unless the cluster advertises
// the OffOnly feature; the three 0x4x commands carry "LT".
const (
	CmdOff                     = onoffdef.CmdOff
	CmdOn                      = onoffdef.CmdOn
	CmdToggle                  = onoffdef.CmdToggle
	CmdOffWithEffect           = onoffdef.CmdOffWithEffect
	CmdOnWithRecallGlobalScene = onoffdef.CmdOnWithRecallGlobalScene
	CmdOnWithTimedOff          = onoffdef.CmdOnWithTimedOff
)

// Revision returns the cluster revision of the generated definition, which
// a regeneration from the matter.js snapshot moves; a hand-written copy
// would not follow.
func Revision() uint16 { return onoffdef.Revision }

// lighting is the definition bound to the LT feature: the mandatory
// elements of a Lighting OnOff cluster that is not OffOnly. The generated
// definition admits LT on its own, so New has nothing to refuse;
// TestLightingSetsAreOrderedAndComplete holds the lists it yields.
var lighting, _ = spec.New(onoffdef.Definition, spec.Options{Features: FeatureLighting})

// LightingAttributes returns the attribute ids an OnOff cluster advertising
// [FeatureLighting] must expose, in id order.
func LightingAttributes() []uint32 { return lighting.MatterAttributes() }

// LightingCommands returns the accepted command ids for an OnOff cluster
// advertising [FeatureLighting] and not OffOnly, in id order.
func LightingCommands() []uint32 { return lighting.MatterAcceptedCommands() }
