// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package schema

import "strings"

// RequirementElement is what a device-type requirement requires: matter.js's
// RequirementElement.ElementType values verbatim
// (packages/model/src/elements/RequirementElement.ts).
type RequirementElement string

// Requirement elements.
const (
	RequirementServerCluster RequirementElement = "serverCluster"
	RequirementClientCluster RequirementElement = "clientCluster"
	RequirementFeature       RequirementElement = "feature"
	RequirementAttribute     RequirementElement = "attribute"
	RequirementCommand       RequirementElement = "command"
	RequirementEvent         RequirementElement = "event"
	RequirementCommandField  RequirementElement = "commandField"
	RequirementDeviceType    RequirementElement = "deviceType"
	RequirementCondition     RequirementElement = "condition"
)

// ConditionLocation is where a condition requirement asserts its condition:
// matter.js's RequirementElement.Location (Core § 9.2.6). Empty for every
// requirement that is not a condition requirement.
type ConditionLocation string

// Condition locations.
const (
	// LocationRoot asserts the condition on the node endpoint.
	LocationRoot ConditionLocation = "Root"
	// LocationSelf asserts it on the endpoint of the stating device type.
	LocationSelf ConditionLocation = "Self"
	// LocationDescendant asserts it on the endpoints of the condition's
	// declaring device type in the stating endpoint's composition.
	LocationDescendant ConditionLocation = "Descendant"
)

// Composition is how a device type composes its endpoint's PartsList:
// matter.js's EndpointComposition (DeviceTypeModel.effectiveComposition,
// Core § 9.2.7).
type Composition string

// Compositions.
const (
	// CompositionTree: the PartsList holds the endpoint's children.
	CompositionTree Composition = "tree"
	// CompositionFullFamily: the PartsList holds every descendant.
	CompositionFullFamily Composition = "full-family"
)

// Conformance is a conformance expression as matter.js parses it
// (Conformance.Ast, packages/model/src/aspects/Conformance.ts): the same
// tree cluster/spec's Conformance evaluates, kept here as a plain value so
// package schema keeps no dependency. Op is matter.js's node type ("M",
// "O", "name", "&", "otherwise", …); the zero value is "no conformance
// stated", which matter.js evaluates as optional.
type Conformance struct {
	// Text is the expression as matter.js prints it; set on the root only.
	Text string
	Op   string
	// Name is the referenced name of a "name" node: a condition or a
	// feature code.
	Name string
	// Value is the JSON text of a "value" node.
	Value string
	// Rev is the revision of a "revision" node ("Rev >= v4").
	Rev int
	// Choice is the parameter of a "choice" node ("O.a+").
	Choice *ConformanceChoice
	// Args are the operands, in matter.js's order: the terms of an
	// otherwise list, [lhs, rhs] of a binary operator, the operand of "!",
	// an optionalIf or a choice.
	Args []Conformance
}

// ConformanceChoice is the parameter of a choice node.
type ConformanceChoice struct {
	Name   string
	Num    int
	OrMore bool
	OrLess bool
}

// CountRange is the numeric range a requirement's constraint states —
// matter.js's RequirementModel.componentCountRange: the number of endpoints
// a component device type or a Descendant condition requirement must
// reach. Set is false when the constraint bounds no number.
type CountRange struct {
	Set    bool
	Min    int
	HasMin bool
	Max    int
	HasMax bool
}

// Contains reports whether n lies in the range (both ends inclusive).
func (r CountRange) Contains(n int) bool {
	return (!r.HasMin || n >= r.Min) && (!r.HasMax || n <= r.Max)
}

// Referent is what a requirement resolves to in matter.js's model
// (RequirementResolver): the cluster, the feature, the attribute / command /
// event, the command field, the component device type or the asserted
// condition. Resolved is false when it names nothing the model defines,
// which matter.js leaves unjudged.
type Referent struct {
	Resolved bool
	// ID is the cluster, attribute, command, event, command-field or
	// device-type id, or a feature's bit.
	ID   uint32
	Name string
	// Command names the command of a command field.
	Command string
	// Declarer names the device type declaring an asserted condition.
	Declarer string
	// Provisional marks a feature, attribute, command or event whose own
	// conformance is provisional: matter.js never reports one missing.
	Provisional bool
}

// DeviceTypeRequirement is one requirement of a device type, as matter.js's
// device type validation reads it (RequirementModel): a server or client
// cluster with its nested feature / attribute / command / event
// requirements, a component device type with the cluster requirements its
// endpoint must meet, or a condition requirement.
type DeviceTypeRequirement struct {
	Element RequirementElement
	// Name is the requirement's name as matter.js states it.
	Name string
	// ID is the cluster or device-type id; HasID is false for an element or
	// condition requirement, which names its referent by Name.
	ID    uint32
	HasID bool
	// Type is a condition requirement's qualified condition
	// ("RootNode.GroupcastListenerCond").
	Type string
	// Instance numbers a component requirement one of several of the same
	// device type; zero when it states none.
	Instance int
	// Location is where a condition requirement asserts its condition.
	Location    ConditionLocation
	Conformance Conformance
	// Constraint is the requirement's constraint as matter.js prints it;
	// Count is the range it states, where it states one.
	Constraint string
	Count      CountRange
	// Singleton is the "I" quality (Core § 7.7.3): only the declaring
	// endpoint, or one whose device types list the cluster, may carry it.
	Singleton bool
	Referent  Referent
	// Requirements are the nested requirements, in declaration order.
	Requirements []DeviceTypeRequirement
}

// DeviceTypeDefinition is one device type of the snapshot as matter.js's
// device type validation reads it (DeviceTypeModel).
type DeviceTypeDefinition struct {
	// ID is the device-type id; zero for the Base device type, which has
	// none.
	ID             uint32
	Name           string
	Classification string
	Revision       uint16
	Composition    Composition
	// Base names the device type this one derives from ("OnOffLight" for
	// DimmableLight); empty when it derives from none.
	Base string
	// Conditions are the conditions the device type declares itself.
	Conditions []string
	// Requirements are the device type's own requirements, in declaration
	// order — matter.js judges and reports them in that order.
	Requirements []DeviceTypeRequirement
}

// DeviceTypeDefinitionOf returns the snapshot's definition of a device
// type, or (nil, false) when the snapshot has none. The definition is
// shared: callers must not modify it.
//
// Mirrors matter.js ModelLookups.deviceTypeOf (packages/model/src/logic/
// device-types/ModelLookups.ts).
func DeviceTypeDefinitionOf(id uint32) (*DeviceTypeDefinition, bool) {
	dt, ok := deviceTypeDefinitions[id]
	return dt, ok
}

// BaseDeviceTypes returns the device types classified "base" — the Base
// device type, whose requirements apply to every endpoint that lists a
// device type and whose conditions every device type may name. Shared:
// callers must not modify them.
//
// Mirrors matter.js ModelLookups.baseDeviceTypes.
func BaseDeviceTypes() []*DeviceTypeDefinition {
	return baseDeviceTypeDefinitions
}

// DeviceTypeConditions returns the conditions a device type declares
// itself, in declaration order, or nil for an unknown device type.
func DeviceTypeConditions(id uint32) []string {
	dt, ok := deviceTypeDefinitions[id]
	if !ok {
		return nil
	}
	return dt.Conditions
}

// DeviceTypeConditionScope returns every condition the requirements of a
// device type may name unqualified: its own, those of the device types it
// derives from, and the Base device type's — each spelled as declared, the
// nearest declaration winning. Nil for an unknown device type.
//
// Mirrors matter.js RequirementResolver.conditionsOf, without its qualified
// ("Declarer.Condition") keys (packages/model/src/logic/
// RequirementResolver.ts).
func DeviceTypeConditionScope(id uint32) []string {
	dt, ok := deviceTypeDefinitions[id]
	if !ok {
		return nil
	}
	return conditionScopeOf(dt)
}

// conditionScopeOf is DeviceTypeConditionScope for a definition (the Base
// device type included, which has no id).
func conditionScopeOf(dt *DeviceTypeDefinition) []string {
	byKey := map[string]string{}
	var order []string
	add := func(name string) {
		key := strings.ToLower(name)
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = name
	}
	for _, b := range baseDeviceTypeDefinitions {
		for _, c := range b.Conditions {
			add(c)
		}
	}
	var lineage []*DeviceTypeDefinition
	seen := map[string]bool{}
	for cur := dt; cur != nil && !seen[cur.Name]; cur = deviceTypeNamed(cur.Base) {
		seen[cur.Name] = true
		lineage = append(lineage, cur)
	}
	// Farthest base first, so a nearer declaration of the same name wins.
	for i := len(lineage) - 1; i >= 0; i-- {
		for _, c := range lineage[i].Conditions {
			add(c)
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}

// ConditionScopeOf is [DeviceTypeConditionScope] for a definition, so the
// Base device type (no id) answers too.
func ConditionScopeOf(dt *DeviceTypeDefinition) []string {
	if dt == nil {
		return nil
	}
	return conditionScopeOf(dt)
}

// deviceTypeNamed returns the device type named name, or nil — for the
// empty name too. A device type derives only from another with an id.
func deviceTypeNamed(name string) *DeviceTypeDefinition {
	for _, dt := range deviceTypeDefinitions {
		if dt.Name == name {
			return dt
		}
	}
	return nil
}

// DeviceTypeClusterRequirement returns a device type's requirement of
// clusterID on one side (RequirementServerCluster or
// RequirementClientCluster), with its nested element requirements, or
// (zero, false) when the device type states none.
func DeviceTypeClusterRequirement(deviceType, clusterID uint32, side RequirementElement) (DeviceTypeRequirement, bool) {
	dt, ok := deviceTypeDefinitions[deviceType]
	if !ok {
		return DeviceTypeRequirement{}, false
	}
	for i := range dt.Requirements {
		if r := &dt.Requirements[i]; r.Element == side && r.Referent.Resolved && r.Referent.ID == clusterID {
			return *r, true
		}
	}
	return DeviceTypeRequirement{}, false
}

// DeviceTypeElementRequirements returns the feature, attribute, command and
// event requirements a device type states for its server cluster clusterID,
// in declaration order — what an endpoint of the type must (or must not)
// offer on that cluster beyond the cluster's own conformance. Nil when the
// device type states none.
func DeviceTypeElementRequirements(deviceType, clusterID uint32) []DeviceTypeRequirement {
	r, ok := DeviceTypeClusterRequirement(deviceType, clusterID, RequirementServerCluster)
	if !ok {
		return nil
	}
	var out []DeviceTypeRequirement
	for i := range r.Requirements {
		switch n := &r.Requirements[i]; n.Element {
		case RequirementFeature, RequirementAttribute, RequirementCommand, RequirementEvent:
			out = append(out, *n)
		default:
		}
	}
	return out
}

// ClusterFeature is one feature of a cluster: its code and its bit in the
// FeatureMap.
type ClusterFeature struct {
	Name string
	Bit  uint8
}

// ClusterFeatures returns a cluster's features in bit order, or nil for a
// cluster without features or unknown to the snapshot.
func ClusterFeatures(clusterID uint32) []ClusterFeature {
	return clusterFeatures[clusterID]
}

// ClusterFeatureNames decodes a FeatureMap value into the codes of the
// features it sets, in bit order. A set bit the snapshot defines no
// feature for is skipped.
//
// Mirrors matter.js ClusterModel.supportedFeatures read from a FeatureMap.
func ClusterFeatureNames(clusterID, featureMap uint32) []string {
	var out []string
	for _, f := range clusterFeatures[clusterID] {
		if f.Bit < 32 && featureMap&(1<<f.Bit) != 0 {
			out = append(out, f.Name)
		}
	}
	return out
}

// ClusterClassification returns a cluster's effective classification —
// "application", "endpoint" (an endpoint utility) or "node" (a node
// utility) — or ("", false) for a cluster the snapshot does not classify.
//
// Mirrors matter.js ClusterModel.effectiveClassification (Core § 7.10.8).
func ClusterClassification(clusterID uint32) (string, bool) {
	c, ok := clusterClassifications[clusterID]
	return c, ok
}

// ClusterBindable reports whether a Binding entry may direct a client of
// the cluster: false only for a cluster whose own mechanism chooses its
// peer (OTA Software Update Provider, WebRTC Transport Provider …). Such a
// client does not count for Base's Client condition.
//
// Mirrors matter.js ClusterModel.effectiveBindable.
func ClusterBindable(clusterID uint32) bool {
	_, unbindable := unbindableClusters[clusterID]
	return !unbindable
}
