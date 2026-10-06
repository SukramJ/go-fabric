// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"slices"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/schema"
)

// Model lookups of device type validation: what every validation reads of
// the schema's device-type layer, resolved once. Mirrors matter.js
// ModelLookups (packages/model/src/logic/device-types/ModelLookups.ts) and
// RequirementResolver (packages/model/src/logic/RequirementResolver.ts).

// dtCondition is a condition and the device type declaring it.
type dtCondition struct {
	declarer string
	name     string
}

// dtModel is the model a validation resolves device types and clusters in:
// the schema's generated device-type layer, or, in tests, a synthetic one
// (as matter.js's DeviceTypeValidationPass takes a MatterModel).
type dtModel struct {
	// deviceTypes are the device types with an id; base the device types
	// classified "base".
	deviceTypes map[uint32]*schema.DeviceTypeDefinition
	base        []*schema.DeviceTypeDefinition
	// features, classification and bindable answer for a cluster id.
	features       func(cluster uint32) []schema.ClusterFeature
	classification func(cluster uint32) string
	bindable       func(cluster uint32) bool
}

// standardDTModel is the schema's model.
func standardDTModel() *dtModel {
	m := &dtModel{
		deviceTypes: map[uint32]*schema.DeviceTypeDefinition{},
		base:        schema.BaseDeviceTypes(),
		features:    schema.ClusterFeatures,
		classification: func(cluster uint32) string {
			c, _ := schema.ClusterClassification(cluster)
			return c
		},
		bindable: schema.ClusterBindable,
	}
	for id := range schema.DeviceTypeRevisions {
		if dt, ok := schema.DeviceTypeDefinitionOf(id); ok {
			m.deviceTypes[id] = dt
		}
	}
	return m
}

// all lists the base device types and every device type, in id order.
func (m *dtModel) all() []*schema.DeviceTypeDefinition {
	ids := make([]uint32, 0, len(m.deviceTypes))
	for id := range m.deviceTypes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := append([]*schema.DeviceTypeDefinition(nil), m.base...)
	for _, id := range ids {
		out = append(out, m.deviceTypes[id])
	}
	return out
}

// dtLookups holds the lookups shared by every validation in one model.
type dtLookups struct {
	m *dtModel
	// scopes is RequirementResolver.conditionsOf per device type: every
	// condition its requirements may name, keyed by lowercased name — the
	// Base device type's and its lineage's unqualified, every device type's
	// qualified ("declarer.name").
	scopes map[*schema.DeviceTypeDefinition]map[string]dtCondition
	// unqualified are the declared spellings reachable without a qualifier:
	// KnownNames.all without the cluster's features.
	unqualified map[*schema.DeviceTypeDefinition]map[string]bool
	// conformances caches the evaluable tree of each requirement.
	conformances map[*schema.DeviceTypeRequirement]spec.Conformance
	byName       map[string]*schema.DeviceTypeDefinition
	aggregator   *schema.DeviceTypeDefinition
	bridgedNode  *schema.DeviceTypeDefinition
}

var (
	dtLookupsOnce sync.Once
	dtShared      *dtLookups
)

// deviceTypeLookups returns the lookups of the schema's model, built on
// first use.
func deviceTypeLookups() *dtLookups {
	dtLookupsOnce.Do(func() { dtShared = buildDeviceTypeLookups(standardDTModel()) })
	return dtShared
}

func buildDeviceTypeLookups(m *dtModel) *dtLookups {
	l := &dtLookups{
		m:           m,
		scopes:      map[*schema.DeviceTypeDefinition]map[string]dtCondition{},
		unqualified: map[*schema.DeviceTypeDefinition]map[string]bool{},

		conformances: map[*schema.DeviceTypeRequirement]spec.Conformance{},
	}
	all := m.all()
	byName := map[string]*schema.DeviceTypeDefinition{}
	for _, dt := range all {
		byName[dt.Name] = dt
	}
	l.byName = byName
	l.aggregator, l.bridgedNode = byName["Aggregator"], byName["BridgedNode"]

	// conditionsIn: every condition keyed qualified, the Base device type's
	// unqualified as well.
	common := map[string]dtCondition{}
	for _, declarer := range all {
		for _, name := range declarer.Conditions {
			c := dtCondition{declarer: declarer.Name, name: name}
			common[strings.ToLower(declarer.Name+"."+name)] = c
			if declarer.Classification == "base" {
				common[strings.ToLower(name)] = c
			}
		}
	}
	for _, dt := range all {
		scope := make(map[string]dtCondition, len(common))
		for k, v := range common {
			scope[k] = v
		}
		// The lineage, farthest base first, so a nearer declaration wins.
		var lineage []*schema.DeviceTypeDefinition
		seen := map[string]bool{}
		for cur := dt; cur != nil && !seen[cur.Name]; cur = byName[cur.Base] {
			seen[cur.Name] = true
			lineage = append(lineage, cur)
		}
		for i := len(lineage) - 1; i >= 0; i-- {
			for _, name := range lineage[i].Conditions {
				c := dtCondition{declarer: lineage[i].Name, name: name}
				scope[strings.ToLower(name)] = c
				scope[strings.ToLower(lineage[i].Name+"."+name)] = c
			}
		}
		l.scopes[dt] = scope
		names := map[string]bool{}
		for key, c := range scope {
			if !strings.Contains(key, ".") {
				names[c.name] = true
			}
		}
		l.unqualified[dt] = names
	}
	for _, dt := range all {
		walkRequirements(dt.Requirements, func(r *schema.DeviceTypeRequirement) {
			l.conformances[r] = spec.ConformanceFromSchema(r.Conformance)
		})
	}
	return l
}

// walkRequirements visits every requirement of a tree, depth first.
func walkRequirements(reqs []schema.DeviceTypeRequirement, visit func(*schema.DeviceTypeRequirement)) {
	for i := range reqs {
		visit(&reqs[i])
		walkRequirements(reqs[i].Requirements, visit)
	}
}

// conformanceOf returns a requirement of the model's device types as
// cluster/spec evaluates it; the lookups converted every one up front.
func (l *dtLookups) conformanceOf(r *schema.DeviceTypeRequirement) spec.Conformance {
	return l.conformances[r]
}

// componentOf is ModelLookups.componentOf: the device type a component
// requirement names, nil for any other requirement or an undefined one.
func (l *dtLookups) componentOf(r *schema.DeviceTypeRequirement) *schema.DeviceTypeDefinition {
	if r.Element != schema.RequirementDeviceType || !r.Referent.Resolved {
		return nil
	}
	return l.m.deviceTypes[r.Referent.ID]
}

// componentsDeclaredBy is ModelLookups.componentsDeclaredBy: whether a
// device type states a component requirement, which makes its endpoint
// composed.
func componentsDeclaredBy(dt *schema.DeviceTypeDefinition) bool {
	for i := range dt.Requirements {
		if dt.Requirements[i].Element == schema.RequirementDeviceType {
			return true
		}
	}
	return false
}

// knownNames is ModelLookups.knownNamesOf for a requirement whose endpoint
// scope is (dt, cluster): all are the unqualified conditions of dt and the
// features of the cluster, features the features alone. dt is nil for a
// component device type the model does not define; hasCluster is false for
// a cluster requirement's own conformance.
func (l *dtLookups) knownNames(dt *schema.DeviceTypeDefinition, cluster uint32, hasCluster bool) (all, features map[string]bool) {
	features = map[string]bool{}
	if hasCluster {
		for _, f := range l.m.features(cluster) {
			features[f.Name] = true
		}
	}
	all = make(map[string]bool, len(features)+len(l.unqualified[dt]))
	for n := range features {
		all[n] = true
	}
	for n := range l.unqualified[dt] {
		all[n] = true
	}
	return all, features
}

// nameSet answers spec.FeatureContext from two name sets.
type nameSet struct {
	defined, supported map[string]bool
}

func (n nameSet) Defined(name string) bool   { return n.defined[name] }
func (n nameSet) Supported(name string) bool { return n.supported[name] }

// requirementApplicability is matter.js requirementApplicability
// (packages/model/src/logic/RequirementApplicability.ts): the conformance
// evaluated with the true names as supported features and the known names
// as defined ones, so an unknown name leaves it conditional.
func (l *dtLookups) requirementApplicability(r *schema.DeviceTypeRequirement, trueNames, known map[string]bool) spec.Applicability {
	return l.conformanceOf(r).Applicability(nameSet{defined: known, supported: trueNames})
}

// applicabilityOf is DeviceTypeConformance.ts applicabilityOf: a condition
// alone never disallows — None is answered only when the features of the
// conformance forbid the element whatever the conditions; otherwise it is
// Conditional, which is not judged.
func (l *dtLookups) applicabilityOf(r *schema.DeviceTypeRequirement, trueNames map[string]bool, s reqScope) spec.Applicability {
	all, features := l.knownNames(s.dt, s.cluster, s.hasCluster)
	a := l.requirementApplicability(r, trueNames, all)
	if a == spec.ApplicabilityNone && l.requirementApplicability(r, trueNames, features) != spec.ApplicabilityNone {
		return spec.ApplicabilityConditional
	}
	return a
}

// reqScope is RequirementResolver.EndpointScope: the device type whose
// conditions and the cluster whose features a requirement's conformance
// may name.
type reqScope struct {
	dt         *schema.DeviceTypeDefinition
	cluster    uint32
	hasCluster bool
}

// scopeNames are the declared names of every condition in a device type's
// scope, qualified entries included — the known names a condition
// requirement's own conformance is judged with
// (ConditionAssertions.ts ScopeConditions #assertionsOf).
func (l *dtLookups) scopeNames(dt *schema.DeviceTypeDefinition) map[string]bool {
	names := map[string]bool{}
	for _, c := range l.scopes[dt] {
		names[c.name] = true
	}
	return names
}

// resolveStated is ConditionAssertions resolveStated: a stated condition
// name resolved in the scopes of the endpoint's device types, exactly as
// declared (a qualified name as "Declarer.Condition"); otherwise the
// declared spelling of a name that matches regardless of case is the
// suggestion.
func (l *dtLookups) resolveStated(scopes []*schema.DeviceTypeDefinition, name string) (resolved, suggestion string) {
	for _, dt := range scopes {
		c, ok := l.scopes[dt][strings.ToLower(name)]
		if !ok {
			continue
		}
		declared := c.name
		if strings.Contains(name, ".") {
			declared = c.declarer + "." + c.name
		}
		if declared == name {
			return c.name, ""
		}
		if suggestion == "" {
			suggestion = declared
		}
	}
	return "", suggestion
}

// Structural conditions the Base device type defines in terms of the tree
// (ConditionAssertions.ts StructuralCondition).
const (
	condNode      = "Node"
	condApp       = "App"
	condSimple    = "Simple"
	condDynamic   = "Dynamic"
	condComposed  = "Composed"
	condClient    = "Client"
	condServer    = "Server"
	condDuplicate = "Duplicate"
)

// Node conditions the node's configuration answers
// (ConditionAssertions.ts NodeCondition).
const (
	condCustomNetworkConfig = "CustomNetworkConfig"
	condEthernet            = "Ethernet"
	condWiFi                = "WiFi"
	condThread              = "Thread"
)

// interfaceConditions maps a NetworkCommissioning feature to the network
// interface condition it makes true (ConditionAssertions.ts
// interfaceConditions).
var interfaceConditions = map[string]string{"WI": condWiFi, "TH": condThread, "ET": condEthernet}

const networkCommissioningClusterID uint32 = 0x0031
