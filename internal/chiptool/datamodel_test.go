// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// The data-model sweep. It has no table of expected clusters: it reads what
// the daemon reports about itself — every endpoint, every cluster, every
// attribute a wildcard read returns — and holds each against two
// authorities: the daemon's own global lists (AttributeList,
// AcceptedCommandList, GeneratedCommandList, ServerList) and the matter.js
// schema snapshot this module embeds (parity/schema.json). A cluster added
// to the reference daemon is covered the moment it is mounted.

// Global attribute ids (Matter Core §7.13).
const (
	attrGeneratedCommandList uint32 = 0xFFF8
	attrAcceptedCommandList  uint32 = 0xFFF9
	attrAttributeList        uint32 = 0xFFFB
	attrFeatureMap           uint32 = 0xFFFC
	attrClusterRevision      uint32 = 0xFFFD
)

// Cluster ids the sweep treats specially.
const (
	clusterDescriptor       uint32 = 0x001D
	deviceTypeRootNode      uint32 = 0x0016
	deviceTypeAggregator    uint32 = 0x000E
	deviceTypeBridgedNode   uint32 = 0x0013
	deviceTypePowerSource   uint32 = 0x0011
	clusterBridgedBasicInfo uint32 = 0x0039
)

// schemaCluster is one cluster of the snapshot, as far as the sweep reads it.
type schemaCluster struct {
	ID         uint32 `json:"id"`
	Name       string `json:"name"`
	Revision   uint16 `json:"revision"`
	Attributes []struct {
		ID          uint32 `json:"id"`
		Name        string `json:"name"`
		Conformance string `json:"conformance"`
	} `json:"attributes"`
	Commands []struct {
		ID          uint32 `json:"id"`
		Name        string `json:"name"`
		Direction   string `json:"direction"`
		Conformance string `json:"conformance"`
		Response    string `json:"response"`
	} `json:"commands"`
	Features []struct {
		Name string `json:"name"`
		Bit  uint   `json:"bit"`
	} `json:"features"`
}

// loadSchemaClusters decodes the embedded matter.js snapshot.
//
// A derived cluster (RvcOperationalState from OperationalState, the ModeBase
// family) carries in the snapshot only what it overrides; an inherited
// element appears with its id and name but without direction or
// conformance. Those are completed from the element of the same name and id
// in another cluster — the base — so the sweep judges a derived cluster by
// the base's rules wherever the derivation does not override them, as
// matter.js does.
func loadSchemaClusters(t *testing.T) map[uint32]*schemaCluster {
	t.Helper()
	var snap struct {
		Clusters []*schemaCluster `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatalf("decode the schema snapshot: %v", err)
	}
	out := make(map[uint32]*schemaCluster, len(snap.Clusters))
	type key struct {
		id   uint32
		name string
	}
	baseCmd := map[key]struct{ direction, conformance, response string }{}
	for _, c := range snap.Clusters {
		out[c.ID] = c
		for _, cmd := range c.Commands {
			if cmd.Direction != "" {
				baseCmd[key{cmd.ID, cmd.Name}] = struct{ direction, conformance, response string }{cmd.Direction, cmd.Conformance, cmd.Response}
			}
		}
	}
	for _, c := range snap.Clusters {
		for i := range c.Commands {
			cmd := &c.Commands[i]
			if cmd.Direction != "" {
				continue
			}
			if b, ok := baseCmd[key{cmd.ID, cmd.Name}]; ok {
				cmd.Direction = b.direction
				if cmd.Conformance == "" {
					cmd.Conformance = b.conformance
				}
				if cmd.Response == "" {
					cmd.Response = b.response
				}
			}
		}
	}
	return out
}

// deviceTypeRequirements caches the snapshot's device-type requirement rows.
var deviceTypeRequirements map[uint32]map[uint32]string

// deviceTypeForbids reports whether deviceType lists cluster as a server
// requirement with conformance "X".
func deviceTypeForbids(t *testing.T, deviceType, cluster uint32) bool {
	t.Helper()
	if deviceTypeRequirements == nil {
		var snap struct {
			DeviceTypes []struct {
				ID           uint32 `json:"id"`
				Requirements []struct {
					ID          uint32 `json:"id"`
					Element     string `json:"element"`
					Conformance string `json:"conformance"`
				} `json:"requirements"`
			} `json:"deviceTypes"`
		}
		if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
			t.Fatalf("decode the schema snapshot: %v", err)
		}
		deviceTypeRequirements = map[uint32]map[uint32]string{}
		for _, dt := range snap.DeviceTypes {
			m := map[uint32]string{}
			for _, r := range dt.Requirements {
				if r.Element == "serverCluster" {
					m[r.ID] = r.Conformance
				}
			}
			deviceTypeRequirements[dt.ID] = m
		}
	}
	return deviceTypeRequirements[deviceType][cluster] == "X"
}

// knownDivergences are the sweep findings this module records as
// deliberate in notes/parity/by_design.md. Each is reported as a skip
// naming its record, never dropped silently; a divergence that stops
// occurring fails the sweep, so the list cannot go stale.
var knownDivergences = map[string]string{
	// The ScenesManagement stub has no scene table and accepts no command.
	"0x0062/command-missing/0x00": "BD-Matter-P2-D18",
	"0x0062/command-missing/0x01": "BD-Matter-P2-D18",
	"0x0062/command-missing/0x02": "BD-Matter-P2-D18",
	"0x0062/command-missing/0x03": "BD-Matter-P2-D18",
	"0x0062/command-missing/0x04": "BD-Matter-P2-D18",
	"0x0062/command-missing/0x05": "BD-Matter-P2-D18",
	"0x0062/command-missing/0x06": "BD-Matter-P2-D18",
}

// conformance is the verdict of one conformance string under a FeatureMap.
type conformance int

const (
	confUnknown    conformance = iota // an expression this evaluator does not model
	confMandatory                     // must be present
	confOptional                      // may be present
	confDisallowed                    // must be absent
)

// evalConformance evaluates a matter.js conformance string for the features
// a cluster advertises. It models the shapes that decide presence — "M",
// "O", "X", "D", "P", a feature expression with !, &, | and parentheses,
// and a bracketed (optional) feature expression — and answers confUnknown for
// anything else (revision guards, choice sets like "O.a+", "desc"), which the
// sweep then does not assert on. Under-asserting is the safe direction: a
// wrong "must be absent" would fail a correct device.
//
// The first top-level comma-separated clause that applies wins, as in the
// specification's otherwise-conformance.
func evalConformance(expr string, features map[string]bool) conformance {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return confUnknown
	}
	for _, clause := range splitTopLevel(expr, ',') {
		clause = strings.TrimSpace(clause)
		switch clause {
		case "M":
			return confMandatory
		case "O":
			return confOptional
		case "X", "D", "P":
			return confDisallowed
		}
		optional := false
		if strings.HasPrefix(clause, "[") && strings.HasSuffix(clause, "]") {
			optional = true
			clause = clause[1 : len(clause)-1]
		}
		v, ok := evalFeatureExpr(clause, features)
		if !ok {
			return confUnknown
		}
		if v {
			if optional {
				return confOptional
			}
			return confMandatory
		}
		// False: fall through to the next clause; with none left, the
		// element is disallowed.
	}
	return confDisallowed
}

// splitTopLevel splits on sep outside brackets and parentheses.
func splitTopLevel(s string, sep rune) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// evalFeatureExpr evaluates an expression over feature names with !, &, |
// and parentheses. ok is false for anything else.
func evalFeatureExpr(expr string, features map[string]bool) (value, ok bool) {
	p := &featureParser{src: strings.TrimSpace(expr), features: features}
	v, ok := p.or()
	p.skip()
	if !ok || p.pos != len(p.src) {
		return false, false
	}
	return v, true
}

type featureParser struct {
	src      string
	pos      int
	features map[string]bool
}

func (p *featureParser) skip() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

func (p *featureParser) or() (bool, bool) {
	v, ok := p.and()
	for ok {
		p.skip()
		if p.pos < len(p.src) && p.src[p.pos] == '|' {
			p.pos++
			r, rok := p.and()
			v, ok = v || r, rok
			continue
		}
		break
	}
	return v, ok
}

func (p *featureParser) and() (bool, bool) {
	v, ok := p.unary()
	for ok {
		p.skip()
		if p.pos < len(p.src) && p.src[p.pos] == '&' {
			p.pos++
			r, rok := p.unary()
			v, ok = v && r, rok
			continue
		}
		break
	}
	return v, ok
}

func (p *featureParser) unary() (bool, bool) {
	p.skip()
	if p.pos >= len(p.src) {
		return false, false
	}
	switch c := p.src[p.pos]; {
	case c == '!':
		p.pos++
		v, ok := p.unary()
		return !v, ok
	case c == '(':
		p.pos++
		v, ok := p.or()
		p.skip()
		if !ok || p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return false, false
		}
		p.pos++
		return v, true
	case unicode.IsUpper(rune(c)):
		start := p.pos
		for p.pos < len(p.src) && (unicode.IsUpper(rune(p.src[p.pos])) || unicode.IsDigit(rune(p.src[p.pos])) || p.src[p.pos] == '_') {
			p.pos++
		}
		name := p.src[start:p.pos]
		set, known := p.features[name]
		if !known {
			// Not a feature of this cluster: an attribute name or some
			// other condition this evaluator does not model.
			return false, false
		}
		return set, true
	}
	return false, false
}

// testDataModelSweep is the sweep leg of TestChipToolSuite.
func testDataModelSweep(t *testing.T, f *fixture) {
	specs := loadSchemaClusters(t)

	// One wildcard read of the whole node: every endpoint, cluster and
	// attribute the daemon is willing to report.
	a := f.run(t, fmt.Sprintf("any read-by-id 0xFFFFFFFF 0xFFFFFFFF %s 0xFFFF", f.node()))
	if a.failed() {
		t.Fatalf("wildcard read failed: %v\n%s", a.Results, a.logText())
	}
	wild := map[uint16]map[uint32]map[uint32]any{}
	for _, r := range a.Results {
		ep, ok1 := asInt(r["endpointId"])
		cl, ok2 := asInt(r["clusterId"])
		at, ok3 := asInt(r["attributeId"])
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		if _, isErr := r["error"]; isErr {
			t.Errorf("wildcard read returned an error for endpoint %d cluster 0x%04X attribute 0x%04X: %v "+
				"(a wildcard read must leave out what it cannot report, not answer it with a status)", ep, cl, at, r["error"])
			continue
		}
		if wild[uint16(ep)] == nil {
			wild[uint16(ep)] = map[uint32]map[uint32]any{}
		}
		if wild[uint16(ep)][uint32(cl)] == nil {
			wild[uint16(ep)][uint32(cl)] = map[uint32]any{}
		}
		wild[uint16(ep)][uint32(cl)][uint32(at)] = r["value"]
	}

	t.Run("descriptor/endpoints-match-wildcard", func(t *testing.T) {
		var wildEps []uint16
		for ep := range wild {
			wildEps = append(wildEps, ep)
		}
		slices.Sort(wildEps)
		if got, want := wildEps, f.sortedEndpoints(); !slices.Equal(got, want) {
			t.Errorf("wildcard read covers endpoints %v; root PartsList + 0 says %v", got, want)
		}
	})

	for _, epID := range f.sortedEndpoints() {
		ep := f.topology[epID]
		t.Run(fmt.Sprintf("ep%d/descriptor", epID), func(t *testing.T) {
			checkDescriptor(t, f, ep, wild[epID])
		})
		for _, cl := range ep.servers {
			spec := specs[cl]
			name := fmt.Sprintf("0x%04X", cl)
			if spec != nil {
				name = spec.Name
			}
			attrs := wild[epID][cl]
			t.Run(fmt.Sprintf("ep%d/%s", epID, name), func(t *testing.T) {
				if spec == nil {
					t.Fatalf("cluster 0x%04X is in ServerList but not in the matter.js schema", cl)
				}
				checkCluster(t, f, epID, spec, attrs)
			})
		}
	}
}

// checkDescriptor holds an endpoint's Descriptor against the wildcard read
// and the schema's device-type library.
func checkDescriptor(t *testing.T, f *fixture, ep *endpointInfo, wild map[uint32]map[uint32]any) {
	// ServerList names exactly the clusters the endpoint answers for.
	var answered []uint32
	for cl := range wild {
		answered = append(answered, cl)
	}
	slices.Sort(answered)
	servers := slices.Clone(ep.servers)
	slices.Sort(servers)
	if !slices.Equal(answered, servers) {
		t.Errorf("ServerList %s, but a wildcard read answers for %s", hexList(servers), hexList(answered))
	}

	if len(ep.deviceTypes) == 0 {
		t.Fatal("DeviceTypeList is empty")
	}
	for dt, rev := range ep.deviceTypes {
		want, ok := schema.DeviceTypeRevision(dt)
		if !ok {
			t.Errorf("device type 0x%04X is not in the matter.js schema", dt)
			continue
		}
		if rev != want {
			t.Errorf("device type 0x%04X revision %d, schema says %d", dt, rev, want)
		}
		for _, cl := range schema.DeviceTypeMandatoryServerClusters[dt] {
			if !ep.has(cl) {
				name, _ := schema.ClusterName(cl)
				dtName, _ := schema.DeviceTypeName(dt)
				t.Errorf("device type %s (0x%04X) mandates server cluster %s (0x%04X); ServerList lacks it",
					dtName, dt, name, cl)
			}
		}
	}
	// A device type may mark a cluster "X" — must not be on its endpoint
	// (DoorLock and Groups / ScenesManagement, for one). Clusters a device
	// type does not mention at all are allowed: an endpoint may carry more
	// than its device types require (Identify on a bridged endpoint is the
	// common case).
	for _, cl := range ep.servers {
		for dt := range ep.deviceTypes {
			if deviceTypeForbids(t, dt, cl) {
				name, _ := schema.ClusterName(cl)
				dtName, _ := schema.DeviceTypeName(dt)
				t.Errorf("cluster %s (0x%04X) is served, but device type %s (0x%04X) marks it X (disallowed)",
					name, cl, dtName, dt)
			}
		}
	}

	// The three-tier shape: root and aggregator are full-family, bridged
	// endpoints are leaves.
	var others []uint16
	for _, id := range f.sortedEndpoints() {
		if id != 0 {
			others = append(others, id)
		}
	}
	switch {
	case ep.id == 0:
		if _, ok := ep.deviceTypes[deviceTypeRootNode]; !ok {
			t.Errorf("endpoint 0 does not advertise RootNode")
		}
		if parts := sortedU16(ep.parts); !slices.Equal(parts, others) {
			t.Errorf("root PartsList %v, want every other endpoint %v", parts, others)
		}
	case hasDeviceType(ep, deviceTypeAggregator):
		var bridged []uint16
		for _, id := range others {
			if id != ep.id {
				bridged = append(bridged, id)
			}
		}
		if parts := sortedU16(ep.parts); !slices.Equal(parts, bridged) {
			t.Errorf("aggregator PartsList %v, want every bridged endpoint %v", parts, bridged)
		}
	default:
		if !hasDeviceType(ep, deviceTypeBridgedNode) {
			t.Errorf("bridged endpoint does not advertise BridgedNode (0x0013): %v", ep.deviceTypes)
		}
		if !ep.has(clusterBridgedBasicInfo) {
			t.Errorf("bridged endpoint does not serve BridgedDeviceBasicInformation")
		}
		if len(ep.parts) != 0 {
			t.Errorf("bridged endpoint PartsList %v, want empty (a leaf)", ep.parts)
		}
	}
}

// checkCluster holds one cluster instance against its own global lists and
// the schema.
func checkCluster(t *testing.T, f *fixture, epID uint16, spec *schemaCluster, wild map[uint32]any) {
	attrList, ok := asInts(wild[attrAttributeList])
	if !ok {
		t.Fatalf("AttributeList missing or not a list in the wildcard read: %v", wild[attrAttributeList])
	}
	listed := map[uint32]bool{}
	for _, a := range attrList {
		listed[uint32(a)] = true
	}

	// Every listed attribute came back from the wildcard read, and nothing
	// unlisted did.
	for _, a := range sortedKeysU32(listed) {
		if _, ok := wild[a]; !ok {
			t.Errorf("AttributeList names 0x%04X (%s) but the wildcard read did not return it", a, attrName(spec, a))
		}
	}
	for _, a := range sortedKeysAny(wild) {
		if !listed[a] {
			t.Errorf("the wildcard read returned 0x%04X (%s), which AttributeList does not name", a, attrName(spec, a))
		}
	}

	// A concrete read of every listed attribute succeeds — the concrete
	// path is a different code path from the wildcard expansion.
	ids := make([]string, 0, len(listed))
	for _, a := range sortedKeysU32(listed) {
		ids = append(ids, strconv.FormatUint(uint64(a), 10))
	}
	_, errs := f.readByID(t, spec.ID, strings.Join(ids, ","), epID)
	for a, e := range errs {
		t.Errorf("concrete read of listed attribute 0x%04X (%s) answered %s", a, attrName(spec, a), e)
	}

	// EventList is conformance "D" in matter.js (event-list.element.ts), and
	// the dispatcher leaves it out of every synthesised AttributeList and
	// wildcard expansion; a server that lists it on its own disagrees with
	// both.
	if listed[0xFFFA] {
		t.Errorf("AttributeList names EventList (0xFFFA), which matter.js marks deprecated (D) and the " +
			"dispatcher omits everywhere else")
	}

	// The globals every cluster carries (Matter Core §7.13; EventList left
	// the spec in 1.3).
	for _, g := range []uint32{attrGeneratedCommandList, attrAcceptedCommandList, attrAttributeList, attrFeatureMap, attrClusterRevision} {
		if !listed[g] {
			t.Errorf("AttributeList lacks the global 0x%04X", g)
		}
	}

	rev, _ := asInt(wild[attrClusterRevision])
	if want, ok := schema.ClusterRevision(spec.ID); ok && uint16(rev) != want {
		t.Errorf("ClusterRevision %d, matter.js schema says %d", rev, want)
	}

	fm, _ := asInt(wild[attrFeatureMap])
	features := map[string]bool{}
	var known uint64
	for _, ft := range spec.Features {
		features[ft.Name] = uint64(fm)&(1<<ft.Bit) != 0
		known |= 1 << ft.Bit
	}
	if extra := uint64(fm) &^ known; extra != 0 {
		t.Errorf("FeatureMap 0x%X sets bits 0x%X the schema defines no feature for", fm, extra)
	}

	// Attributes against their conformance under the advertised features.
	for _, at := range spec.Attributes {
		if at.ID >= 0xFFF0 {
			continue
		}
		switch evalConformance(at.Conformance, features) {
		case confMandatory:
			if !listed[at.ID] {
				t.Errorf("attribute %s (0x%04X) has conformance %q, mandatory under FeatureMap 0x%X; AttributeList lacks it",
					at.Name, at.ID, at.Conformance, fm)
			}
		case confDisallowed:
			if listed[at.ID] {
				t.Errorf("attribute %s (0x%04X) has conformance %q, disallowed under FeatureMap 0x%X; AttributeList names it",
					at.Name, at.ID, at.Conformance, fm)
			}
		}
	}
	for a := range listed {
		if a >= 0xFFF0 {
			continue
		}
		if attrName(spec, a) == "?" && a < 0xF000 {
			t.Errorf("AttributeList names 0x%04X, which the matter.js schema does not define for %s", a, spec.Name)
		}
	}

	// Commands.
	accepted, _ := asInts(wild[attrAcceptedCommandList])
	generated, _ := asInts(wild[attrGeneratedCommandList])
	acc := map[uint32]bool{}
	for _, c := range accepted {
		acc[uint32(c)] = true
	}
	gen := map[uint32]bool{}
	for _, c := range generated {
		gen[uint32(c)] = true
	}
	responses := map[string]uint32{}
	for _, c := range spec.Commands {
		if c.Direction == "response" {
			responses[c.Name] = c.ID
		}
	}
	for _, c := range spec.Commands {
		if c.Direction != "request" {
			continue
		}
		verdict := evalConformance(c.Conformance, features)
		switch {
		case verdict == confMandatory && !acc[c.ID]:
			if id, ok := knownDivergences[fmt.Sprintf("0x%04X/command-missing/0x%02X", spec.ID, c.ID)]; ok {
				t.Run(fmt.Sprintf("divergence/%s-%s", id, c.Name), func(t *testing.T) {
					t.Skipf("command %s (0x%02X) is mandatory and not accepted: a recorded divergence, %s in notes/parity/by_design.md",
						c.Name, c.ID, id)
				})
				continue
			}
			t.Errorf("command %s (0x%02X) has conformance %q, mandatory under FeatureMap 0x%X; AcceptedCommandList lacks it",
				c.Name, c.ID, c.Conformance, fm)
		case verdict == confDisallowed && acc[c.ID]:
			t.Errorf("command %s (0x%02X) has conformance %q, disallowed under FeatureMap 0x%X; AcceptedCommandList names it",
				c.Name, c.ID, c.Conformance, fm)
		}
		if acc[c.ID] && c.Response != "" && c.Response != "status" {
			if id, ok := responses[c.Response]; ok && !gen[id] {
				t.Errorf("accepted command %s answers with %s (0x%02X), which GeneratedCommandList lacks", c.Name, c.Response, id)
			}
		}
	}
	for c := range acc {
		if !specHasCommand(spec, c, "request") {
			t.Errorf("AcceptedCommandList names 0x%02X, which the schema defines as no request of %s", c, spec.Name)
		}
	}
	for c := range gen {
		if !specHasCommand(spec, c, "response") {
			t.Errorf("GeneratedCommandList names 0x%02X, which the schema defines as no response of %s", c, spec.Name)
		}
	}
}

func specHasCommand(spec *schemaCluster, id uint32, direction string) bool {
	for _, c := range spec.Commands {
		if c.ID == id && c.Direction == direction {
			return true
		}
	}
	return false
}

func attrName(spec *schemaCluster, id uint32) string {
	for _, a := range spec.Attributes {
		if a.ID == id {
			return a.Name
		}
	}
	switch id {
	case attrGeneratedCommandList:
		return "GeneratedCommandList"
	case attrAcceptedCommandList:
		return "AcceptedCommandList"
	case attrAttributeList:
		return "AttributeList"
	case attrFeatureMap:
		return "FeatureMap"
	case attrClusterRevision:
		return "ClusterRevision"
	}
	return "?"
}

func hasDeviceType(ep *endpointInfo, dt uint32) bool {
	_, ok := ep.deviceTypes[dt]
	return ok
}

func sortedU16(in []uint16) []uint16 {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func sortedKeysU32(m map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func sortedKeysAny(m map[uint32]any) []uint32 {
	out := make([]uint32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func hexList(ids []uint32) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("0x%04X", id)
	}
	return "[" + strings.Join(parts, " ") + "]"
}
