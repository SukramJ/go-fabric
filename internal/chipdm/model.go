// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package chipdm cross-checks the matter.js schema snapshot
// (parity/schema.json) against connectedhomeip's spec-derived data model
// (data_model/<version>/clusters/*.xml, device_types/*.xml) — the model the
// CSA's Python certification cases judge a device against.
//
// Both sides are reduced to one representation (Model) and compared element
// by element: clusters (id, revision, features, attributes, commands,
// events, data types and their members) and device types (id, revision,
// classification, conditions, cluster requirements with their quality and
// their feature / attribute / command / event requirements, condition
// requirements — the Base device type included). Every difference is either
// normalized away as representation (the rules in compare.go, each
// counted), or listed with a class and a reason in the acknowledged table
// (acknowledged.go). The cross-check test fails on any other difference and
// on any acknowledgement that no longer matches one.
//
// The comparison is a port of matter.js's own
// (support/codegen/src/chipdm/, validate-chipdm-model.ts), which validates
// matter.js's specification scrape against the same XML; it reads the CHIP
// XML the same way, normalizes the same representation differences, and
// starts its acknowledged table from matter.js's by-design.ts. What differs:
// matter.js compares its model objects, this package compares the extract
// matter.js emits for this module (the snapshot), so an element the
// snapshot does not carry is not compared (see NotCompared).
//
// The CHIP side is read at test time from a connectedhomeip checkout at the
// harness image's commit (the Makefile's CHIP_TEST_IMAGE_COMMIT) and kept in
// memory only: the data model XML carries a CSA notice that forbids
// publishing it or deriving works from it, so nothing read from it is
// committed. Without a checkout the cross-check skips; CI provides one.
// ADR 0015 records why.
package chipdm

// Model is one side of the comparison, reduced to what is compared.
type Model struct {
	Clusters []*Cluster `json:"clusters"`

	// BaseClusters are CHIP's clusters without an id of their own (Mode
	// Base, Alarm Base, …), kept to resolve the derived clusters; the
	// snapshot side has none — its derived clusters are already resolved.
	BaseClusters []*Cluster `json:"baseClusters,omitempty"`

	DeviceTypes []*DeviceType `json:"deviceTypes"`

	// Globals are the data types defined at global scope.
	Globals []*Element `json:"globals,omitempty"`

	// Uncompared counts, per aspect of notCarried, what CHIP states that
	// the reduced model does not keep because the snapshot cannot be
	// compared with it (device-type conditions, element requirements, …).
	Uncompared map[string]int `json:"uncompared,omitempty"`
}

// Cluster is one cluster (or, on the CHIP side, one cluster id of a family
// that shares a definition, as the concentration-measurement clusters do).
type Cluster struct {
	ID             *uint32 `json:"id,omitempty"`
	Name           string  `json:"name"`
	Revision       int     `json:"revision"`
	Classification string  `json:"classification,omitempty"`

	// Base names the cluster this one derives from (CHIP: the
	// classification's baseCluster).
	Base string `json:"base,omitempty"`

	// Provisional is CHIP's provisional mark on the cluster id, which a
	// certification run rejects on a device under test
	// (device_conformance_tests.py check_conformance).
	Provisional bool `json:"provisional,omitempty"`

	Features   []*Element `json:"features,omitempty"`
	Attributes []*Element `json:"attributes,omitempty"`
	Commands   []*Element `json:"commands,omitempty"`
	Events     []*Element `json:"events,omitempty"`
	Datatypes  []*Element `json:"datatypes,omitempty"`
}

// Element is a feature, attribute, command, event, data type, or a member
// of one (a struct, command or event field, an enum item, a bitmap bit).
// An aspect left empty is "not stated", which compares as no opinion — not
// as an empty definition (matter.js chipdm/data-model.ts DmElement).
type Element struct {
	ID   *uint32 `json:"id,omitempty"`
	Name string  `json:"name"`

	// Kind is a data type's kind (enum, bitmap, struct, number, typedef).
	Kind string `json:"kind,omitempty"`

	Type      string `json:"type,omitempty"`
	EntryType string `json:"entryType,omitempty"`

	Conformance *Conf `json:"conformance,omitempty"`

	// Constraint is the constraint in matter.js's notation
	// (Constraint.serialize), which both sides are rendered in.
	Constraint string `json:"constraint,omitempty"`

	Access *Access `json:"access,omitempty"`

	// Quality is the sorted set of quality flags; nil is "not stated".
	Quality []string `json:"quality,omitempty"`

	Default *Value `json:"default,omitempty"`

	Direction string `json:"direction,omitempty"` // "request" or "response"
	Response  string `json:"response,omitempty"`
	Priority  string `json:"priority,omitempty"`

	Fields []*Element `json:"fields,omitempty"`

	// Metatype and Primitive are the snapshot's resolution of Type
	// (matter.js effectiveMetatype and primitiveBase); the CHIP side
	// states neither.
	Metatype  string `json:"metatype,omitempty"`
	Primitive string `json:"primitive,omitempty"`

	// Entry is the snapshot's list entry (the CHIP side states only
	// EntryType).
	Entry *Element `json:"entry,omitempty"`
}

// Access is an element's access, facet by facet; an empty facet is not
// stated. Rw is matter.js's notation ("R", "W", "RW", "R[W]").
type Access struct {
	Rw        string `json:"rw,omitempty"`
	ReadPriv  string `json:"readPriv,omitempty"`
	WritePriv string `json:"writePriv,omitempty"`
	Fabric    string `json:"fabric,omitempty"` // "F" scoped, "S" sensitive
	Timed     bool   `json:"timed,omitempty"`
}

// Value is a default value. Kind is one of null, bool, number, reference,
// string, list, celsius, percent, bytes; Text its rendering.
type Value struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// DeviceType is one device type.
type DeviceType struct {
	ID             *uint32 `json:"id,omitempty"`
	Name           string  `json:"name"`
	Revision       int     `json:"revision"`
	Classification string  `json:"classification,omitempty"`

	// Superset names the device type this one is a superset of (CHIP's
	// classification superset).
	Superset string `json:"superset,omitempty"`

	Requirements []*Requirement `json:"requirements,omitempty"`

	// Conditions are the conditions the device type declares (CHIP:
	// <conditions>; matter.js: the device type's Condition children).
	Conditions []string `json:"conditions,omitempty"`

	// ConditionRequirements are the conditions of other device types this
	// one asserts (CHIP: <conditionRequirements>; matter.js: requirements
	// of element "condition").
	ConditionRequirements []*ConditionRequirement `json:"conditionRequirements,omitempty"`
}

// Requirement is a device type's requirement of a cluster on one side.
type Requirement struct {
	ID          uint32 `json:"id"`
	Name        string `json:"name"`
	Side        string `json:"side"` // "server" or "client"
	Conformance *Conf  `json:"conformance,omitempty"`

	// Quality is the requirement's quality flags (the singleton "I"); nil
	// is "not stated".
	Quality []string `json:"quality,omitempty"`

	// Elements are the feature, attribute, command and event requirements
	// the device type states for the cluster (CHIP: the <features>,
	// <attributes>, <commands> and <events> of the cluster requirement;
	// matter.js: its nested requirements).
	Elements []*ElementRequirement `json:"elements,omitempty"`
}

// ElementRequirement is a device type's requirement of one element of a
// cluster: whether the endpoint must, may or must not offer it, and the
// constraint it narrows the element to.
type ElementRequirement struct {
	Element     string  `json:"element"` // feature, attribute, command, event
	ID          *uint32 `json:"id,omitempty"`
	Name        string  `json:"name"` // a feature's code, an element's name
	Conformance *Conf   `json:"conformance,omitempty"`
	Constraint  string  `json:"constraint,omitempty"`
}

// ConditionRequirement is a device type's assertion of a condition another
// device type declares.
type ConditionRequirement struct {
	DeviceType  string `json:"deviceType"` // the declaring device type
	Name        string `json:"name"`
	Conformance *Conf  `json:"conformance,omitempty"`
	Constraint  string `json:"constraint,omitempty"`
}

func u32(v uint32) *uint32 { return &v }
