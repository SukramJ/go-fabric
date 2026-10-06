// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/schema"
)

// Device type validation: the port of matter.js's DeviceTypeConformance
// (packages/model/src/logic/device-types/DeviceTypeConformance.ts) with the
// condition derivation of ConditionAssertions.ts and the endpoint facts of
// ResolvedEndpoint.ts. Endpoints are identified by their id; dtFacts is
// matter.js's DeviceTypeFacts, answered for an assembled Topology by
// topologyFacts (devicetype_validation.go).
//
// What is judged, and how, is matter.js's docs/DEVICE_TYPE_VALIDATION.md:
// mandatory and disallowed server and client clusters; the feature,
// attribute, command and event requirements nested in a server cluster the
// endpoint carries; component device types (counts, instances, choices,
// the component's own requirements); Descendant condition counts;
// singleton placement; and stated condition names that name no condition.
// A condition never disallows; a provisional element is never missing;
// Base's requirements only ever make something mandatory; a requirement
// whose conformance names something unknown is not judged.

// dtFacts is what device type validation reads of a tree of endpoints
// (matter.js DeviceTypeFacts). Every endpoint it names is present.
type dtFacts interface {
	parentOf(ep uint16) (uint16, bool)
	partsOf(ep uint16) []uint16
	deviceTypeIDs(ep uint16) []uint32
	serverClusters(ep uint16) []uint32
	clientClusters(ep uint16) []uint32
	// features are the codes of the features ep's server cluster supports.
	features(ep uint16, cluster uint32) []string
	// supports reports whether ep's server cluster implements the
	// attribute, command or event with that id.
	supports(ep uint16, cluster uint32, element schema.RequirementElement, id uint32) bool
	statedConditions(ep uint16) []string
	nodeConditions(nodeEp uint16) []string
	describe(ep uint16) string
}

// dtPass is one validation pass (matter.js DeviceTypeValidationPass): the
// facts it reads and what its checks share.
type dtPass struct {
	facts       dtFacts
	l           *dtLookups
	resolved    map[uint16]*dtResolved
	collections map[uint16]*dtScopeConditions
	appCounts   map[uint16]map[uint32]int
	components  map[uint16]map[*schema.DeviceTypeDefinition][]*dtComponent
	failures    map[uint16]map[*schema.DeviceTypeRequirement][]DeviceTypeViolation
	singletons  map[uint16]map[uint32]*dtSingleton
}

func newDTPass(facts dtFacts, l *dtLookups) *dtPass {
	return &dtPass{
		facts:       facts,
		l:           l,
		resolved:    map[uint16]*dtResolved{},
		collections: map[uint16]*dtScopeConditions{},
		appCounts:   map[uint16]map[uint32]int{},
		components:  map[uint16]map[*schema.DeviceTypeDefinition][]*dtComponent{},
		failures:    map[uint16]map[*schema.DeviceTypeRequirement][]DeviceTypeViolation{},
		singletons:  map[uint16]map[uint32]*dtSingleton{},
	}
}

// --- ResolvedEndpoint ----------------------------------------------------------

// dtResolved is ResolvedEndpoint: the facts of one endpoint resolved in
// the model.
type dtResolved struct {
	ep          uint16
	deviceTypes []*schema.DeviceTypeDefinition
	servers     map[uint32]bool
	clients     map[uint32]bool
	features    map[uint32][]string
	children    []uint16
	scope       []uint16
	scopeSet    map[uint16]bool
	scopeBuilt  bool
	p           *dtPass
}

func (p *dtPass) of(ep uint16) *dtResolved {
	if r, ok := p.resolved[ep]; ok {
		return r
	}
	r := &dtResolved{ep: ep, p: p, servers: map[uint32]bool{}, clients: map[uint32]bool{}, features: map[uint32][]string{}}
	for _, id := range p.facts.deviceTypeIDs(ep) {
		// A device type the model does not define states no requirement
		// that could be checked.
		if dt, ok := p.l.m.deviceTypes[id]; ok {
			r.deviceTypes = append(r.deviceTypes, dt)
		}
	}
	for _, c := range p.facts.serverClusters(ep) {
		r.servers[c] = true
	}
	for _, c := range p.facts.clientClusters(ep) {
		r.clients[c] = true
	}
	r.children = p.facts.partsOf(ep)
	p.resolved[ep] = r
	return r
}

// featuresOf are the codes of the features the endpoint's server cluster
// supports; none when it has no such server.
func (r *dtResolved) featuresOf(cluster uint32) []string {
	if !r.servers[cluster] {
		return nil
	}
	if f, ok := r.features[cluster]; ok {
		return f
	}
	f := r.p.facts.features(r.ep, cluster)
	r.features[cluster] = f
	return f
}

// hasApplicationServer: a server application cluster exists on the
// endpoint (Base's Server condition).
func (r *dtResolved) hasApplicationServer() bool {
	for c := range r.servers {
		if r.p.l.m.classification(c) == "application" {
			return true
		}
	}
	return false
}

// hasBindableApplicationClient: a client application cluster a binding may
// direct exists on the endpoint (Base's Client condition, with matter.js's
// interpretation that an unbindable client does not count).
func (r *dtResolved) hasBindableApplicationClient() bool {
	for c := range r.clients {
		if r.p.l.m.classification(c) == "application" && r.p.l.m.bindable(c) {
			return true
		}
	}
	return false
}

// isNodeEndpoint: a device type of the endpoint is classified a node, which
// makes the endpoint the root of a node scope.
func (r *dtResolved) isNodeEndpoint() bool {
	for _, dt := range r.deviceTypes {
		if dt.Classification == "node" {
			return true
		}
	}
	return false
}

// composesFullFamily: the endpoint's PartsList holds every descendant.
func (r *dtResolved) composesFullFamily() bool {
	for _, dt := range r.deviceTypes {
		if dt.Composition == schema.CompositionFullFamily {
			return true
		}
	}
	return false
}

// compositionScope is the endpoints the PartsList reaches within the node
// scope: children for the tree pattern, descendants for full-family,
// never entering a node endpoint below.
func (r *dtResolved) compositionScope() []uint16 {
	if r.scopeBuilt {
		return r.scope
	}
	full := r.composesFullFamily()
	var visit func(ep uint16)
	visit = func(ep uint16) {
		for _, child := range r.p.of(ep).children {
			if r.p.of(child).isNodeEndpoint() {
				continue
			}
			r.scope = append(r.scope, child)
			if full {
				visit(child)
			}
		}
	}
	visit(r.ep)
	r.scopeSet = map[uint16]bool{}
	for _, ep := range r.scope {
		r.scopeSet[ep] = true
	}
	r.scopeBuilt = true
	return r.scope
}

func (r *dtResolved) composes(ep uint16) bool {
	r.compositionScope()
	return r.scopeSet[ep]
}

func (r *dtResolved) listsDeviceType(id uint32) bool {
	for _, dt := range r.deviceTypes {
		if dt.ID == id {
			return true
		}
	}
	return false
}

// --- ConditionAssertions -----------------------------------------------------------

// nodeEndpointOf: the closest endpoint at or above ep whose device type is
// classified a node; false when ep belongs to no node scope.
func (p *dtPass) nodeEndpointOf(ep uint16) (uint16, bool) {
	for cur := ep; ; {
		if p.of(cur).isNodeEndpoint() {
			return cur, true
		}
		parent, ok := p.facts.parentOf(cur)
		if !ok {
			return 0, false
		}
		cur = parent
	}
}

// treeRootOf: the root of ep's tree.
func (p *dtPass) treeRootOf(ep uint16) uint16 {
	for {
		parent, ok := p.facts.parentOf(ep)
		if !ok {
			return ep
		}
		ep = parent
	}
}

// isInScope: whether ep is in the node scope of nodeEp — no node endpoint
// between them.
func (p *dtPass) isInScope(ep, nodeEp uint16) bool {
	for cur := ep; cur != nodeEp; {
		parent, ok := p.facts.parentOf(cur)
		if !ok || p.of(cur).isNodeEndpoint() {
			return false
		}
		cur = parent
	}
	return true
}

// nodeScopeOf: the node endpoint and its descendants, without a node
// endpoint below it and that one's descendants.
func (p *dtPass) nodeScopeOf(nodeEp uint16) []uint16 {
	scope := []uint16{nodeEp}
	var visit func(ep uint16)
	visit = func(ep uint16) {
		for _, child := range p.of(ep).children {
			if p.of(child).isNodeEndpoint() {
				continue
			}
			scope = append(scope, child)
			visit(child)
		}
	}
	visit(nodeEp)
	return scope
}

// applicationDeviceTypeIDs: the endpoint's application device types.
func applicationDeviceTypeIDs(r *dtResolved) []uint32 {
	var ids []uint32
	for _, dt := range r.deviceTypes {
		switch dt.Classification {
		case "application", "simple", "dynamic":
			ids = append(ids, dt.ID)
		default:
		}
	}
	return ids
}

// isDuplicate is ConditionAssertions.isDuplicate: the endpoint and a
// sibling share an application device type (Base's Duplicate condition).
func (p *dtPass) isDuplicate(ep uint16) bool {
	parent, ok := p.facts.parentOf(ep)
	if !ok {
		return false
	}
	own := applicationDeviceTypeIDs(p.of(ep))
	if len(own) == 0 {
		return false
	}
	counts, ok := p.appCounts[parent]
	if !ok {
		counts = map[uint32]int{}
		for _, child := range p.of(parent).children {
			for _, id := range applicationDeviceTypeIDs(p.of(child)) {
				counts[id]++
			}
		}
		p.appCounts[parent] = counts
	}
	for _, id := range own {
		if counts[id] > 1 {
			return true
		}
	}
	return false
}

// ownStructuralConditions are the structural conditions ep's own facts
// decide: all but Duplicate.
func (p *dtPass) ownStructuralConditions(ep uint16) map[string]bool {
	r := p.of(ep)
	conds := map[string]bool{}
	for _, dt := range r.deviceTypes {
		switch dt.Classification {
		case "node":
			conds[condNode] = true
		case "application":
			conds[condApp] = true
		case "simple":
			conds[condApp], conds[condSimple] = true, true
		case "dynamic":
			conds[condApp], conds[condDynamic] = true, true
		default:
		}
		if componentsDeclaredBy(dt) {
			conds[condComposed] = true
		}
	}
	if r.hasApplicationServer() {
		conds[condServer] = true
	}
	if r.hasBindableApplicationClient() {
		conds[condClient] = true
	}
	return conds
}

// conditionScopesOf: the device types whose conditions a stated name
// resolves in — the endpoint's, or Base's when it lists none.
func (p *dtPass) conditionScopesOf(ep uint16) []*schema.DeviceTypeDefinition {
	if dts := p.of(ep).deviceTypes; len(dts) > 0 {
		return dts
	}
	return p.l.m.base
}

// statedConditions are the conditions ep states, as declared.
func (p *dtPass) statedConditions(ep uint16) []string {
	var out []string
	for _, name := range p.facts.statedConditions(ep) {
		if c, _ := p.l.resolveStated(p.conditionScopesOf(ep), name); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// unknownStated is ConditionAssertions.unknownNames.
func (p *dtPass) unknownStated(ep uint16) []DeviceTypeViolation {
	var out []DeviceTypeViolation
	first := ""
	if dts := p.of(ep).deviceTypes; len(dts) > 0 {
		first = dts[0].Name
	}
	for _, name := range p.facts.statedConditions(ep) {
		c, suggestion := p.l.resolveStated(p.conditionScopesOf(ep), name)
		if c != "" {
			continue
		}
		detail := fmt.Sprintf("Unknown condition %q", name)
		if suggestion != "" {
			detail += fmt.Sprintf("; did you mean %q?", suggestion)
		}
		out = append(out, DeviceTypeViolation{DeviceType: first, Requirement: name, Kind: ViolationUnknownCondition, Detail: detail})
	}
	return out
}

// dtAssertion is a condition requirement of an endpoint's device type that
// asserts its condition.
type dtAssertion struct {
	req       *schema.DeviceTypeRequirement
	owner     string
	declarer  string
	condition string
}

// dtScopeConditions is ConditionAssertions ScopeConditions: the conditions
// of the endpoints of one node scope, derived per endpoint on request.
type dtScopeConditions struct {
	p              *dtPass
	nodeEp         uint16
	nodeConditions map[string]bool
	underived      map[uint16]map[string]bool
	assertions     map[uint16][]dtAssertion
	conditions     map[uint16]map[string]bool
}

func (p *dtPass) collect(nodeEp uint16) *dtScopeConditions {
	if c, ok := p.collections[nodeEp]; ok {
		return c
	}
	c := &dtScopeConditions{
		p: p, nodeEp: nodeEp,
		underived:  map[uint16]map[string]bool{},
		assertions: map[uint16][]dtAssertion{},
		conditions: map[uint16]map[string]bool{},
	}
	p.collections[nodeEp] = c
	return c
}

// nodeConditionsOf: what the node's configuration answers — the facts'
// node conditions and the network interfaces a NetworkCommissioning server
// in the node scope supports.
func (s *dtScopeConditions) nodeConditionsOf() map[string]bool {
	if s.nodeConditions != nil {
		return s.nodeConditions
	}
	conds := map[string]bool{}
	for _, c := range s.p.facts.nodeConditions(s.nodeEp) {
		conds[c] = true
	}
	for _, ep := range s.p.nodeScopeOf(s.nodeEp) {
		for _, f := range s.p.of(ep).featuresOf(networkCommissioningClusterID) {
			if c, ok := interfaceConditions[f]; ok {
				conds[c] = true
			}
		}
	}
	s.nodeConditions = conds
	return conds
}

// underivedOf: structural, node and stated conditions of ep.
func (s *dtScopeConditions) underivedOf(ep uint16) map[string]bool {
	if u, ok := s.underived[ep]; ok {
		return u
	}
	u := s.p.ownStructuralConditions(ep)
	if s.p.isDuplicate(ep) {
		u[condDuplicate] = true
	}
	for c := range s.nodeConditionsOf() {
		u[c] = true
	}
	for _, c := range s.p.statedConditions(ep) {
		u[c] = true
	}
	s.underived[ep] = u
	return u
}

// assertionsOf: the condition requirements of ep's device types whose
// conformance is mandatory for ep's underived conditions.
func (s *dtScopeConditions) assertionsOf(ep uint16) []dtAssertion {
	if a, ok := s.assertions[ep]; ok {
		return a
	}
	var out []dtAssertion
	names := s.underivedOf(ep)
	for _, dt := range s.p.of(ep).deviceTypes {
		known := s.p.l.scopeNames(dt)
		for i := range dt.Requirements {
			r := &dt.Requirements[i]
			if r.Element != schema.RequirementCondition || !r.Referent.Resolved {
				continue
			}
			if s.p.l.requirementApplicability(r, names, known) == spec.ApplicabilityMandatory {
				out = append(out, dtAssertion{req: r, owner: dt.Name, declarer: r.Referent.Declarer, condition: r.Referent.Name})
			}
		}
	}
	s.assertions[ep] = out
	return out
}

func (s *dtScopeConditions) inScope(ep uint16) bool { return s.p.isInScope(ep, s.nodeEp) }

// conditionsOf: the conditions true for ep; none outside the node scope.
func (s *dtScopeConditions) conditionsOf(ep uint16) map[string]bool {
	if c, ok := s.conditions[ep]; ok {
		return c
	}
	conds := map[string]bool{}
	if s.inScope(ep) {
		conds = s.assertedOn(ep)
	}
	s.conditions[ep] = conds
	return conds
}

// assertedOn: ep's underived conditions with those asserted on it — by
// itself at Self, by the composers whose composition covers it at
// Descendant, and, for the node endpoint, by any endpoint of the scope at
// Root. Chains are not followed.
func (s *dtScopeConditions) assertedOn(ep uint16) map[string]bool {
	conds := map[string]bool{}
	for c := range s.underivedOf(ep) {
		conds[c] = true
	}
	for _, a := range s.assertionsOf(ep) {
		if a.req.Location == schema.LocationSelf {
			conds[a.condition] = true
		}
	}
	if ep == s.nodeEp {
		for _, other := range s.p.nodeScopeOf(s.nodeEp) {
			for _, a := range s.assertionsOf(other) {
				if a.req.Location == schema.LocationRoot {
					conds[a.condition] = true
				}
			}
		}
		return conds
	}
	own := s.p.of(ep)
	for composer, ok := s.p.facts.parentOf(ep); ok; composer, ok = s.p.facts.parentOf(composer) {
		for _, a := range s.assertionsOf(composer) {
			// Interpretation (matter.js): the assertion covers every
			// descendant of the declaring device type.
			if a.req.Location != schema.LocationDescendant {
				continue
			}
			if declarer := s.p.l.byName[a.declarer]; declarer != nil && own.listsDeviceType(declarer.ID) && s.p.of(composer).composes(ep) {
				conds[a.condition] = true
			}
		}
		if composer == s.nodeEp {
			break
		}
	}
	return conds
}

// descendantAssertionsOf: ep's asserted Descendant condition requirements,
// each with the endpoints of the declaring device type its composition
// reaches.
func (s *dtScopeConditions) descendantAssertionsOf(ep uint16) []dtDescendantAssertion {
	if !s.inScope(ep) {
		return nil
	}
	var out []dtDescendantAssertion
	for _, a := range s.assertionsOf(ep) {
		if a.req.Location != schema.LocationDescendant {
			continue
		}
		var matches []uint16
		if declarer := s.p.l.byName[a.declarer]; declarer != nil {
			for _, m := range s.p.of(ep).compositionScope() {
				if s.p.of(m).listsDeviceType(declarer.ID) {
					matches = append(matches, m)
				}
			}
		}
		out = append(out, dtDescendantAssertion{assertion: a, matches: matches})
	}
	return out
}

type dtDescendantAssertion struct {
	assertion dtAssertion
	matches   []uint16
}

// --- DeviceTypeConformance.check -------------------------------------------------------

// dtContext is what cluster requirements are judged against
// (DeviceTypeConformance.ts Context).
type dtContext struct {
	violations *[]DeviceTypeViolation
	facts      *dtResolved
	// deviceType is the device type the violation is reported for; scope
	// the one whose conditions the conformance names (the component device
	// type for a component's nested requirements).
	deviceType *schema.DeviceTypeDefinition
	scope      *schema.DeviceTypeDefinition
	conditions map[string]bool
	waived     map[string]bool
	p          *dtPass
}

// check is DeviceTypeConformance.check: the departures of ep from the
// requirements of its device types and of Base, each reported once by key,
// the first device type's report kept, never Base's.
func (p *dtPass) check(ep uint16) []DeviceTypeViolation {
	facts := p.of(ep)
	scopeRoot, ok := p.nodeEndpointOf(ep)
	if !ok {
		scopeRoot = p.treeRootOf(ep)
	}
	collection := p.collect(scopeRoot)
	conditions := collection.conditionsOf(ep)

	violations := p.unknownStated(ep)
	deviceTypes := append([]*schema.DeviceTypeDefinition(nil), facts.deviceTypes...)
	if len(deviceTypes) > 0 {
		// Every device type derives from Base, so its requirements apply
		// once per endpoint rather than once per device type.
		deviceTypes = append(deviceTypes, p.l.m.base...)
	}
	for _, dt := range deviceTypes {
		waived := map[string]bool{}
		if dt.Classification == "base" {
			waived = p.baseWaivers(facts)
		}
		ctx := &dtContext{violations: &violations, facts: facts, deviceType: dt, scope: dt, conditions: conditions, waived: waived, p: p}
		ctx.checkClusters(dt.Requirements)
		ctx.checkComposition(collection)
	}
	p.checkComponentOf(&violations, facts, collection)
	p.checkDescendantCounts(&violations, collection.descendantAssertionsOf(ep))
	p.checkSingletons(&violations, facts)

	unique := make([]DeviceTypeViolation, 0, len(violations))
	seen := map[string]bool{}
	for i := range violations {
		if v := &violations[i]; !seen[v.Key()] {
			seen[v.Key()] = true
			v.Endpoint = ep
			unique = append(unique, *v)
		}
	}
	return unique
}

// checkClusters judges the server and client cluster requirements among
// reqs.
func (c *dtContext) checkClusters(reqs []schema.DeviceTypeRequirement) {
	for i := range reqs {
		switch reqs[i].Element {
		case schema.RequirementServerCluster:
			c.checkCluster(&reqs[i], "server")
		case schema.RequirementClientCluster:
			c.checkCluster(&reqs[i], "client")
		default:
		}
	}
}

func (c *dtContext) checkCluster(r *schema.DeviceTypeRequirement, side string) {
	present, path, kind, ok := c.judgeCluster(r, side)
	if !ok || kind != "" || !present || side == "client" {
		return
	}
	cluster := r.Referent.ID
	features := c.facts.featuresOf(cluster)
	trueNames := map[string]bool{}
	for n := range c.conditions {
		trueNames[n] = true
	}
	for _, f := range features {
		trueNames[f] = true
	}
	scope := reqScope{dt: c.scope, cluster: cluster, hasCluster: true}
	for i := range r.Requirements {
		nested := &r.Requirements[i]
		if !nested.Referent.Resolved {
			continue
		}
		var held bool
		switch nested.Element {
		case schema.RequirementFeature:
			held = slices.Contains(features, nested.Referent.Name)
		case schema.RequirementAttribute, schema.RequirementCommand, schema.RequirementEvent:
			held = c.p.facts.supports(c.facts.ep, cluster, nested.Element, nested.Referent.ID)
		default:
			continue
		}
		c.judge(judgement{
			req: nested, applicability: c.p.l.applicabilityOf(nested, trueNames, scope), trueNames: trueNames,
			present: held, provisional: nested.Referent.Provisional,
			path:    path + "." + nested.Referent.Name,
			subject: fmt.Sprintf("%s %s of %s", nested.Element, nested.Referent.Name, r.Referent.Name),
			cluster: cluster, hasCluster: true,
		})
	}
}

// judgeCluster judges whether the endpoint carries the cluster r names on
// side; ok is false when the cluster does not resolve.
func (c *dtContext) judgeCluster(r *schema.DeviceTypeRequirement, side string) (present bool, path string, kind ViolationKind, ok bool) {
	if !r.Referent.Resolved {
		return false, "", "", false
	}
	if side == "server" {
		present = c.facts.servers[r.Referent.ID]
		path = r.Referent.Name
	} else {
		present = c.facts.clients[r.Referent.ID]
		path = "client:" + r.Referent.Name
	}
	kind = c.judge(judgement{
		req: r, applicability: c.p.l.applicabilityOf(r, c.conditions, reqScope{dt: c.scope}), trueNames: c.conditions,
		present: present, path: path, subject: side + " cluster " + r.Referent.Name,
		cluster: r.Referent.ID, hasCluster: true, clusterRequirement: true,
	})
	return present, path, kind, true
}

// judgement is one requirement judged against the endpoint.
type judgement struct {
	req                *schema.DeviceTypeRequirement
	applicability      spec.Applicability
	trueNames          map[string]bool
	present            bool
	provisional        bool
	path, subject      string
	cluster            uint32
	hasCluster         bool
	clusterRequirement bool
}

// judge records the violation an applicability and a presence amount to
// and answers its kind ("" for none) — DeviceTypeConformance.ts judge.
func (c *dtContext) judge(j judgement) ViolationKind {
	if c.waived[j.path] {
		return ""
	}
	var kind ViolationKind
	var detail string
	switch {
	case j.applicability == spec.ApplicabilityMandatory && !j.present && !j.provisional:
		kind, detail = ViolationMissing, "Mandatory "+j.subject+" is missing"
	case j.applicability == spec.ApplicabilityNone && j.present && c.deviceType.Classification != "base":
		// Base's requirements are duties, not restrictions.
		kind, detail = ViolationDisallowed, "Disallowed "+j.subject+" is present"
	default:
		return ""
	}
	v := DeviceTypeViolation{
		DeviceType: c.deviceType.Name, DeviceTypeID: c.deviceType.ID,
		Requirement: j.path, Kind: kind, Detail: detail,
		Conformance: j.req.Conformance.Text, Conditions: heldNames(c.p.l.conformanceOf(j.req), j.trueNames),
	}
	if j.hasCluster {
		v.Cluster, v.HasCluster = j.cluster, true
	}
	if !j.clusterRequirement {
		v.Element, v.ElementName = j.req.Element, j.req.Referent.Name
		if j.req.Element != schema.RequirementFeature {
			v.ElementID = j.req.Referent.ID
		}
	}
	*c.violations = append(*c.violations, v)
	return kind
}

// heldNames are the names a conformance references that hold: the
// conditions (and features) that made the requirement apply.
func heldNames(c spec.Conformance, trueNames map[string]bool) []string {
	var out []string
	for _, n := range c.Names() {
		if trueNames[n] {
			out = append(out, n)
		}
	}
	return out
}

// --- composition -----------------------------------------------------------------

// dtInstance is one component requirement with its applicability under the
// composing endpoint's conditions.
type dtInstance struct {
	req           *schema.DeviceTypeRequirement
	applicability spec.Applicability
}

func (i dtInstance) applies() bool {
	return i.applicability == spec.ApplicabilityMandatory || i.applicability == spec.ApplicabilityOptional
}

func (i dtInstance) tolerated() bool { return i.applicability == spec.ApplicabilityConditional }

// dtComponent is the requirements of one device type for one component
// device type.
type dtComponent struct {
	deviceType *schema.DeviceTypeDefinition
	instances  []dtInstance
	disallowed bool
}

// componentsOf: the component requirements of dt, a device type of
// composing, grouped by component device type.
func (p *dtPass) componentsOf(composing uint16, dt *schema.DeviceTypeDefinition, conditions map[string]bool) []*dtComponent {
	byDT, ok := p.components[composing]
	if !ok {
		byDT = map[*schema.DeviceTypeDefinition][]*dtComponent{}
		p.components[composing] = byDT
	}
	if found, ok := byDT[dt]; ok {
		return found
	}
	var found []*dtComponent
	index := map[uint32]*dtComponent{}
	for i := range dt.Requirements {
		r := &dt.Requirements[i]
		comp := p.l.componentOf(r)
		if comp == nil {
			continue
		}
		a := p.l.applicabilityOf(r, conditions, reqScope{dt: dt})
		entry, ok := index[comp.ID]
		if !ok {
			entry = &dtComponent{deviceType: comp, disallowed: true}
			index[comp.ID] = entry
			found = append(found, entry)
		}
		entry.instances = append(entry.instances, dtInstance{req: r, applicability: a})
		if a != spec.ApplicabilityNone {
			entry.disallowed = false
		}
	}
	byDT[dt] = found
	return found
}

// candidatesOf: the endpoints of the composition scope that list the
// component device type exactly.
func (p *dtPass) candidatesOf(facts *dtResolved, comp *dtComponent) []*dtResolved {
	var out []*dtResolved
	for _, ep := range facts.compositionScope() {
		if r := p.of(ep); r.listsDeviceType(comp.deviceType.ID) {
			out = append(out, r)
		}
	}
	return out
}

// failuresOf: the departures of candidate from the requirements nested in
// instance, a component requirement of the composing device type.
func (p *dtPass) failuresOf(candidate *dtResolved, instance *schema.DeviceTypeRequirement, composing *schema.DeviceTypeDefinition, collection *dtScopeConditions) []DeviceTypeViolation {
	byInstance, ok := p.failures[candidate.ep]
	if !ok {
		byInstance = map[*schema.DeviceTypeRequirement][]DeviceTypeViolation{}
		p.failures[candidate.ep] = byInstance
	}
	if v, ok := byInstance[instance]; ok {
		return v
	}
	violations := []DeviceTypeViolation{}
	ctx := &dtContext{
		violations: &violations, facts: candidate, deviceType: composing, scope: p.l.componentOf(instance),
		conditions: collection.conditionsOf(candidate.ep), waived: map[string]bool{}, p: p,
	}
	ctx.checkClusters(instance.Requirements)
	byInstance[instance] = violations
	return violations
}

// checkComposition judges the endpoint as the composer of the component
// device types its device type requires (Core § 9.2.3).
func (c *dtContext) checkComposition(collection *dtScopeConditions) {
	comps := c.p.componentsOf(c.facts.ep, c.deviceType, c.conditions)
	candidates := map[*dtComponent][]*dtResolved{}
	for _, comp := range comps {
		found := c.p.candidatesOf(c.facts, comp)
		candidates[comp] = found
		if comp.disallowed {
			if len(found) > 0 {
				*c.violations = append(*c.violations, DeviceTypeViolation{
					DeviceType: c.deviceType.Name, DeviceTypeID: c.deviceType.ID,
					Requirement: "device:" + comp.deviceType.Name, Kind: ViolationDisallowed,
					Detail: fmt.Sprintf("Disallowed component device type %s is present on %d endpoint(s)", comp.deviceType.Name, len(found)),
				})
			}
			continue
		}
		c.checkCount(comp, len(found))
		if len(found) > 0 {
			c.checkInstances(comp, found, collection)
		}
	}
	c.checkChoices(comps, candidates)
}

// checkCount reports a number of component endpoints outside the range an
// applying requirement's constraint states; a mandatory one without a
// constraint needs at least one.
func (c *dtContext) checkCount(comp *dtComponent, count int) {
	for _, inst := range comp.instances {
		var rng schema.CountRange
		switch {
		case inst.applicability == spec.ApplicabilityMandatory:
			rng = inst.req.Count
			if !rng.Set {
				rng = schema.CountRange{Set: true, Min: 1, HasMin: true}
			}
		case inst.applicability == spec.ApplicabilityOptional && count > 0:
			rng = inst.req.Count
		default:
		}
		if !rng.Set || rng.Contains(count) {
			continue
		}
		*c.violations = append(*c.violations, DeviceTypeViolation{
			DeviceType: c.deviceType.Name, DeviceTypeID: c.deviceType.ID,
			Requirement: "device:" + comp.deviceType.Name, Kind: ViolationInstanceCount,
			Detail: fmt.Sprintf("Component device type %s requires %s endpoint(s) in the composition; found %d", comp.deviceType.Name, describeRange(rng), count),
		})
	}
}

// checkInstances matches the mandatory instances to distinct candidates
// that satisfy them and reports each left unmatched.
func (c *dtContext) checkInstances(comp *dtComponent, candidates []*dtResolved, collection *dtScopeConditions) {
	var mandatory []*schema.DeviceTypeRequirement
	for _, inst := range comp.instances {
		if inst.applicability == spec.ApplicabilityMandatory {
			mandatory = append(mandatory, inst.req)
		}
	}
	failures := make([][][]DeviceTypeViolation, len(mandatory))
	accepts := make([][]bool, len(mandatory))
	for i, inst := range mandatory {
		for _, cand := range candidates {
			f := c.p.failuresOf(cand, inst, c.deviceType, collection)
			failures[i] = append(failures[i], f)
			accepts[i] = append(accepts[i], len(f) == 0)
		}
	}
	matched := matchInstances(accepts)
	for i, inst := range mandatory {
		if matched[i] >= 0 {
			continue
		}
		row := failures[i]
		var reason string
		satisfiable := false
		for _, f := range row {
			if len(f) == 0 {
				satisfiable = true
			}
		}
		unoffered := commonPaths(row)
		switch {
		case satisfiable:
			reason = "every endpoint that satisfies it fills another instance"
		case len(unoffered) > 0:
			reason = "no candidate offers " + strings.Join(unoffered, ", ")
		default:
			reason = "no candidate satisfies it"
		}
		*c.violations = append(*c.violations, DeviceTypeViolation{
			DeviceType: c.deviceType.Name, DeviceTypeID: c.deviceType.ID,
			Requirement: instancePath(comp, inst), Kind: ViolationInstanceCount,
			Detail: fmt.Sprintf("No endpoint fills %s: %s", describeInstance(comp, inst), reason),
		})
	}
}

// commonPaths: the requirement paths every candidate fails, in the first
// candidate's order.
func commonPaths(row [][]DeviceTypeViolation) []string {
	if len(row) == 0 {
		return nil
	}
	var out []string
	for i := range row[0] {
		v := &row[0][i]
		inAll := true
		for _, other := range row[1:] {
			if !slices.ContainsFunc(other, func(o DeviceTypeViolation) bool { return o.Requirement == v.Requirement }) {
				inAll = false
				break
			}
		}
		if inAll && !slices.Contains(out, v.Requirement) {
			out = append(out, v.Requirement)
		}
	}
	return out
}

// matchInstances assigns each instance a distinct candidate it accepts, by
// augmenting paths; -1 for an instance left unmatched.
func matchInstances(accepts [][]bool) []int {
	owners := map[int]int{}
	var assign func(instance int, visited map[int]bool) bool
	assign = func(instance int, visited map[int]bool) bool {
		for cand := range accepts[instance] {
			if !accepts[instance][cand] || visited[cand] {
				continue
			}
			visited[cand] = true
			owner, taken := owners[cand]
			if !taken || assign(owner, visited) {
				owners[cand] = instance
				return true
			}
		}
		return false
	}
	for i := range accepts {
		assign(i, map[int]bool{})
	}
	matched := make([]int, len(accepts))
	for i := range matched {
		matched[i] = -1
	}
	for cand, inst := range owners {
		matched[inst] = cand
	}
	return matched
}

// dtChoiceMember is a component device type a choice names, and whether
// it is a member because one of its requirements applies or only
// tolerated.
type dtChoiceMember struct {
	applies bool
	ranges  []schema.CountRange
}

// dtChoice is the component requirements that name one choice.
type dtChoice struct {
	param   *schema.ConformanceChoice
	order   []*dtComponent
	members map[*dtComponent]*dtChoiceMember
}

// collectChoices groups the applying and tolerated component requirements
// by the choice their conformance names at its top.
func collectChoices(comps []*dtComponent) []*dtChoice {
	var order []*dtChoice
	choices := map[string]*dtChoice{}
	for _, comp := range comps {
		for _, inst := range comp.instances {
			conf := inst.req.Conformance
			if conf.Op != "choice" || conf.Choice == nil || (!inst.applies() && !inst.tolerated()) {
				continue
			}
			ch, ok := choices[conf.Choice.Name]
			if !ok {
				ch = &dtChoice{param: conf.Choice, members: map[*dtComponent]*dtChoiceMember{}}
				choices[conf.Choice.Name] = ch
				order = append(order, ch)
			}
			ch.add(comp, inst)
		}
	}
	return order
}

// add records inst of comp: the ranges of a member that applies come only
// from its applying choice requirements.
func (ch *dtChoice) add(comp *dtComponent, inst dtInstance) {
	applying := inst.applies()
	m, ok := ch.members[comp]
	switch {
	case !ok:
		ch.order = append(ch.order, comp)
		m = &dtChoiceMember{applies: applying}
		ch.members[comp] = m
	case applying && !m.applies:
		m = &dtChoiceMember{applies: true}
		ch.members[comp] = m
	case !applying && m.applies:
		return
	default:
	}
	if inst.req.Count.Set {
		m.ranges = append(m.ranges, inst.req.Count)
	}
}

// tally counts the members with endpoints in range: those that apply as
// satisfied, those only tolerated as tolerated; judged is false when no
// member applies.
func (ch *dtChoice) tally(candidates map[*dtComponent][]*dtResolved) (satisfied, tolerated int, judged bool) {
	for _, comp := range ch.order {
		m := ch.members[comp]
		judged = judged || m.applies
		count := len(candidates[comp])
		within := count > 0
		for _, r := range m.ranges {
			within = within && r.Contains(count)
		}
		switch {
		case !within:
		case m.applies:
			satisfied++
		default:
			tolerated++
		}
	}
	return satisfied, tolerated, judged
}

// checkChoices judges choice conformance across the component requirements
// that name the same choice (Core § 7.3.14): a tolerated member counts when
// it is satisfied but is never needed.
func (c *dtContext) checkChoices(comps []*dtComponent, candidates map[*dtComponent][]*dtResolved) {
	for _, ch := range collectChoices(comps) {
		satisfied, tolerated, judged := ch.tally(candidates)
		if !judged {
			continue
		}
		num, most := ch.param.Num, satisfied+tolerated
		var ok bool
		bound := "exactly"
		switch {
		case ch.param.OrMore:
			ok, bound = most >= num, "at least"
		case ch.param.OrLess:
			ok, bound = satisfied <= num, "at most"
		default:
			ok = satisfied <= num && most >= num
		}
		if ok {
			continue
		}
		names := make([]string, 0, len(ch.order))
		for _, comp := range ch.order {
			names = append(names, comp.deviceType.Name)
		}
		*c.violations = append(*c.violations, DeviceTypeViolation{
			DeviceType: c.deviceType.Name, DeviceTypeID: c.deviceType.ID,
			Requirement: "device:" + strings.Join(names, "|"), Kind: ViolationInstanceCount,
			Detail: fmt.Sprintf("Requires %s %d of component device types %s; found %d", bound, num, strings.Join(names, ", "), satisfied),
		})
	}
}

// dtFilling is a component requirement an endpoint fills: the composing
// device type, the component and its applying or tolerated instances.
type dtFilling struct {
	deviceType *schema.DeviceTypeDefinition
	comp       *dtComponent
	instances  []dtInstance
}

// fillingsOf: the component requirements of composer's device types that
// facts' endpoint fills with at least one applying instance.
func (p *dtPass) fillingsOf(composer uint16, facts *dtResolved, conditions map[string]bool) []dtFilling {
	var filled []dtFilling
	for _, dt := range p.of(composer).deviceTypes {
		for _, comp := range p.componentsOf(composer, dt, conditions) {
			if !facts.listsDeviceType(comp.deviceType.ID) {
				continue
			}
			var insts []dtInstance
			applying := false
			for _, inst := range comp.instances {
				if inst.applies() || inst.tolerated() {
					insts = append(insts, inst)
					applying = applying || inst.applies()
				}
			}
			if applying {
				filled = append(filled, dtFilling{deviceType: dt, comp: comp, instances: insts})
			}
		}
	}
	return filled
}

// unfilled is the violation of facts' endpoint filling f but satisfying none
// of its instances, or false when it satisfies one.
func (p *dtPass) unfilled(composer uint16, facts *dtResolved, f dtFilling, collection *dtScopeConditions) (DeviceTypeViolation, bool) {
	fails := make([][]DeviceTypeViolation, len(f.instances))
	for i, inst := range f.instances {
		fails[i] = p.failuresOf(facts, inst.req, f.deviceType, collection)
		if len(fails[i]) == 0 {
			return DeviceTypeViolation{}, false
		}
	}
	closest := 0
	for i := range fails {
		if len(fails[i]) < len(fails[closest]) {
			closest = i
		}
	}
	paths := make([]string, 0, len(fails[closest]))
	for i := range fails[closest] {
		paths = append(paths, fails[closest][i].Requirement)
	}
	inst := f.instances[closest].req
	role := fmt.Sprintf("component %s of %s %s", f.comp.deviceType.Name, f.deviceType.Name, p.facts.describe(composer))
	detail := fmt.Sprintf("Endpoint is %s but fails %s", role, strings.Join(paths, ", "))
	if inst.Instance != 0 {
		detail = fmt.Sprintf("Endpoint is %s but satisfies none of its instances; instance %d, the closest, fails %s", role, inst.Instance, strings.Join(paths, ", "))
	}
	return DeviceTypeViolation{
		DeviceType: f.deviceType.Name, DeviceTypeID: f.deviceType.ID,
		Requirement: "device:" + f.deviceType.Name + "/" + f.comp.deviceType.Name,
		Kind:        fails[closest][0].Kind, Detail: detail,
	}, true
}

// checkComponentOf reports the endpoint where it fills a component
// requirement of an endpoint composing it but satisfies the nested
// requirements of none of its instances. A tolerated instance it satisfies
// is enough; only an applying one makes it a component to judge.
func (p *dtPass) checkComponentOf(violations *[]DeviceTypeViolation, facts *dtResolved, collection *dtScopeConditions) {
	if len(facts.deviceTypes) == 0 || facts.isNodeEndpoint() {
		return
	}
	for composer, ok := p.facts.parentOf(facts.ep); ok; composer, ok = p.facts.parentOf(composer) {
		filled := p.fillingsOf(composer, facts, collection.conditionsOf(composer))
		if len(filled) > 0 && p.of(composer).composes(facts.ep) {
			for _, f := range filled {
				if v, bad := p.unfilled(composer, facts, f, collection); bad {
					*violations = append(*violations, v)
				}
			}
		}
		// A composer beyond the node endpoint has its conditions elsewhere.
		if p.of(composer).isNodeEndpoint() {
			break
		}
	}
}

// checkDescendantCounts reports each Descendant condition whose number of
// endpoints lies outside the range its constraint states (Core § 9.2.6).
func (p *dtPass) checkDescendantCounts(violations *[]DeviceTypeViolation, assertions []dtDescendantAssertion) {
	for _, a := range assertions {
		rng := a.assertion.req.Count
		if !rng.Set || rng.Contains(len(a.matches)) {
			continue
		}
		*violations = append(*violations, DeviceTypeViolation{
			DeviceType: a.assertion.owner, Requirement: "condition:" + a.assertion.req.Name, Kind: ViolationInstanceCount,
			Conformance: a.assertion.req.Conformance.Text,
			Detail: fmt.Sprintf("Condition %s must hold for %s endpoint(s) of %s in the composition; found %d",
				a.assertion.req.Name, describeRange(rng), a.assertion.declarer, len(a.matches)),
		})
	}
}

func describeRange(r schema.CountRange) string {
	switch {
	case r.HasMin && r.HasMax && r.Min == r.Max:
		return fmt.Sprintf("exactly %d", r.Min)
	case r.HasMin && r.HasMax:
		return fmt.Sprintf("%d to %d", r.Min, r.Max)
	case r.HasMin:
		return fmt.Sprintf("min %d", r.Min)
	default:
		return fmt.Sprintf("max %d", r.Max)
	}
}

// instancePath: the path of one instance, always numbered.
func instancePath(comp *dtComponent, inst *schema.DeviceTypeRequirement) string {
	n := inst.Instance
	if n == 0 {
		for i, candidate := range comp.instances {
			if candidate.req == inst {
				n = i + 1
			}
		}
	}
	return fmt.Sprintf("device:%s#%d", comp.deviceType.Name, n)
}

func describeInstance(comp *dtComponent, inst *schema.DeviceTypeRequirement) string {
	subject := "component device type " + comp.deviceType.Name
	if inst.Instance == 0 {
		return subject
	}
	return fmt.Sprintf("instance %d of %s", inst.Instance, subject)
}

// --- singletons --------------------------------------------------------------------

// dtSingleton is a server cluster a device type in the node scope declares
// a singleton (Core § 7.7.3).
type dtSingleton struct {
	cluster    string
	deviceType string
	endpoints  map[uint16]bool
}

// checkSingletons reports each server cluster of the endpoint that a device
// type elsewhere in its node scope declares a singleton.
func (p *dtPass) checkSingletons(violations *[]DeviceTypeViolation, facts *dtResolved) {
	nodeEp, ok := p.nodeEndpointOf(facts.ep)
	if !ok {
		return
	}
	singletons, ok := p.singletons[nodeEp]
	if !ok {
		singletons = map[uint32]*dtSingleton{}
		for _, ep := range p.nodeScopeOf(nodeEp) {
			p.declare(singletons, ep)
		}
		p.singletons[nodeEp] = singletons
	}
	ids := make([]uint32, 0, len(singletons))
	for id := range singletons {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var listed map[uint32]bool
	for _, id := range ids {
		s := singletons[id]
		if s.endpoints[facts.ep] || !facts.servers[id] {
			continue
		}
		if listed == nil {
			listed = listedServerClusters(facts)
		}
		if listed[id] {
			continue
		}
		*violations = append(*violations, DeviceTypeViolation{
			DeviceType: s.deviceType, Requirement: s.cluster, Kind: ViolationSingletonMisplaced,
			Cluster: id, HasCluster: true,
			Detail: fmt.Sprintf("Server cluster %s is a singleton of %s in this node scope, so it may appear only on an endpoint that lists that device type or a device type listing the cluster", s.cluster, s.deviceType),
		})
	}
}

// declare adds the singletons ep's device types declare.
func (p *dtPass) declare(singletons map[uint32]*dtSingleton, ep uint16) {
	for _, dt := range p.of(ep).deviceTypes {
		for i := range dt.Requirements {
			r := &dt.Requirements[i]
			if r.Element != schema.RequirementServerCluster || !r.Singleton || !r.Referent.Resolved {
				continue
			}
			s, ok := singletons[r.Referent.ID]
			if !ok {
				s = &dtSingleton{cluster: r.Referent.Name, deviceType: dt.Name, endpoints: map[uint16]bool{}}
				singletons[r.Referent.ID] = s
			}
			s.endpoints[ep] = true
		}
	}
}

// listedServerClusters: the server clusters the endpoint's device types
// list, with any conformance.
func listedServerClusters(facts *dtResolved) map[uint32]bool {
	listed := map[uint32]bool{}
	for _, dt := range facts.deviceTypes {
		for i := range dt.Requirements {
			if r := &dt.Requirements[i]; r.Element == schema.RequirementServerCluster && r.Referent.Resolved {
				listed[r.Referent.ID] = true
			}
		}
	}
	return listed
}

// baseWaivers: the Base requirements not judged on the endpoint — a child
// of an Aggregator that lists BridgedNode is told apart by its NodeLabel,
// so Base's TagList requirement under Duplicate is waived (Core § 9.2.9;
// Device § 11.2.6).
func (p *dtPass) baseWaivers(facts *dtResolved) map[string]bool {
	parent, ok := p.facts.parentOf(facts.ep)
	if !ok || p.l.aggregator == nil || p.l.bridgedNode == nil {
		return map[string]bool{}
	}
	if !facts.listsDeviceType(p.l.bridgedNode.ID) || !p.of(parent).listsDeviceType(p.l.aggregator.ID) {
		return map[string]bool{}
	}
	return map[string]bool{"Descriptor.TAGLIST": true}
}
