// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// descriptorClusterID is the Descriptor cluster (matter.js
// requirement-coverage.ts:46 DESCRIPTOR_CLUSTER_ID).
const descriptorClusterID = 0x1D

// assemblerMounted are the clusters the endpoint assembler mounts itself
// (endpoint/materialize.go; device/device.go assemblerMounted), so the
// generated types give them no accessor.
var assemblerMounted = map[string]bool{
	"Descriptor": true, "BridgedDeviceBasicInformation": true, "Identify": true,
	"Groups": true, "ScenesManagement": true,
}

// deviceMethods are the methods a generated type promotes from
// device.Device; an accessor may not shadow one.
var deviceMethods = map[string]bool{
	"Type": true, "Server": true, "Servers": true, "AddPart": true, "Parts": true,
	"Spec": true, "OnChanged": true, "OnChangedAsync": true, "MatterDeviceType": true, "MatterClusterServers": true,
	"EndpointNumber": true, "SetReachable": true, "Reachable": true, "Rename": true,
}

// systemEndpointTypes are matter.js's simple device types that are
// generated as endpoints (EndpointFile.ts:17 SYSTEM_ENDPOINT_TYPES).
var systemEndpointTypes = map[string]bool{"BridgedNode": true, "Aggregator": true}

// errRequirementLost is a requirement the specification states that would
// reach no generated artifact — matter.js's reportRequirementLost, which
// fails its build (requirement-coverage.ts:13-24).
var errRequirementLost = errors.New("requirement lost")

// generator generates the device files from one snapshot. defs holds the
// clusters whose generated definitions are committed under cluster/spec
// (by package name).
type generator struct {
	snap *snapshot
	defs map[string]bool
}

// output is one generated file.
type output struct {
	File   string
	Source []byte
}

// clusterReq is one generated cluster requirement.
type clusterReq struct {
	req          *requirement
	def          *cluster
	mandatory    bool
	baseMandated bool
	features     []*feature
	elements     []elementReq
}

// elementReq is one attribute, command or event alteration.
type elementReq struct {
	kind, name string
	id         uint32
	presence   string
	hasDefault bool
	def        float64
	hasMin     bool
	min        float64
	hasMax     bool
	max        float64
}

// typeModel is everything one device file states.
type typeModel struct {
	dt      *deviceType
	servers []*clusterReq
	clients []*clusterReq
}

// model derives a device type's requirements as matter.js's EndpointFile
// does (support/codegen/src/endpoints/EndpointFile.ts:76-113).
func (g *generator) model(name string) (*typeModel, error) {
	dt := g.snap.deviceType(name)
	if dt == nil {
		return nil, fmt.Errorf("no device type %s in the snapshot", name)
	}
	if dt.ID == nil {
		return nil, fmt.Errorf("%s has no id: the Base device type emits requirements only (EndpointFile.ts:104-109)", name)
	}
	// assertAllKindsHandled (requirement-coverage.ts:68-78): cluster
	// requirements are generated, device-type and condition requirements
	// are model-only (requirement-coverage.ts:41-44) and stay in the
	// schema, where ADR 0016's validation reads them.
	for _, r := range dt.Effective.Requirements {
		switch r.Element {
		case "serverCluster", "clientCluster", "deviceType", "condition":
		default:
			return nil, fmt.Errorf("%s states a %s requirement (%s) that no generator claims", name, r.Element, r.Name)
		}
	}
	m := &typeModel{dt: dt}
	var err error
	if m.servers, err = g.side(dt, "server"); err != nil {
		return nil, err
	}
	if m.clients, err = g.side(dt, "client"); err != nil {
		return nil, err
	}
	return m, nil
}

// side mirrors matter.js's RequirementGenerator constructor
// (RequirementGenerator.ts:133-205) for one side: the device type's
// cluster requirements of that side, plus — for servers — the Base
// servers the type would otherwise lack; unknown clusters fail,
// disallowed and obsolete ones and an unspecialised Descriptor are
// skipped; mandatory ones first.
func (g *generator) side(dt *deviceType, side string) ([]*clusterReq, error) {
	var reqs []*requirement
	for _, r := range dt.Effective.Requirements {
		if r.Element == side+"Cluster" {
			reqs = append(reqs, r)
		}
	}
	baseMandated := map[uint32]bool{}
	if side == "server" {
		for _, b := range g.missingBaseServers(dt) {
			id := *b.ID
			baseMandated[id] = true
			if !slices.ContainsFunc(reqs, func(r *requirement) bool { return r.ID != nil && *r.ID == id }) {
				reqs = append(reqs, b)
			}
		}
	}
	var mandatory, optional []*clusterReq
	for _, r := range reqs {
		def := g.clusterOf(r)
		if def == nil {
			return nil, fmt.Errorf("%w: %s %s requirement for unknown cluster %s", errRequirementLost, dt.Name, side, r.Name)
		}
		if isDisallowed(r.Conformance) || isObsolete(r.Conformance) {
			continue // RequirementGenerator.ts:172-174
		}
		cr := &clusterReq{req: r, def: def}
		if err := g.ingest(dt, cr); err != nil {
			return nil, err
		}
		// Every server endpoint receives a Descriptor server, so a device
		// type needs a generated one only for what it states beyond the
		// base implementation (RequirementGenerator.ts:178-183).
		isDescriptor := side == "server" && def.ID == descriptorClusterID
		if isDescriptor && !cr.specialized() {
			continue
		}
		// The translator states a Descriptor requirement without
		// conformance, but Base mandates Descriptor
		// (RequirementGenerator.ts:187-188).
		if isMandatory(r.Conformance) || isDescriptor || baseMandated[def.ID] {
			cr.mandatory = true
			cr.baseMandated = baseMandated[def.ID]
			mandatory = append(mandatory, cr)
		} else {
			optional = append(optional, cr)
		}
	}
	return append(mandatory, optional...), nil
}

// clusterOf resolves a cluster requirement (RequirementResolver.clusterOf).
func (g *generator) clusterOf(r *requirement) *cluster {
	switch {
	case r.Referent != nil && r.Referent.ID != nil:
		return g.snap.clusterByID(*r.Referent.ID)
	case r.ID != nil:
		return g.snap.clusterByID(*r.ID)
	default:
		return nil
	}
}

// missingBaseServers mirrors RequirementGenerator.ts missingBaseServersOf
// (:71-106): the Base server cluster requirements an endpoint of the type
// would miss, judged with the clusters the type mandates and no parent,
// parts or stated conditions, the Descriptor left out. The structural
// conditions are matter.js's ownStructuralConditionsOf
// (packages/model/src/logic/device-types/ConditionAssertions.ts:567-604).
func (g *generator) missingBaseServers(dt *deviceType) []*requirement {
	conds := map[string]bool{}
	switch dt.Classification {
	case "node":
		conds["Node"] = true
	case "application":
		conds["App"] = true
	case "simple":
		conds["App"], conds["Simple"] = true, true
	case "dynamic":
		conds["App"], conds["Dynamic"] = true, true
	default:
	}
	for _, r := range dt.Effective.Requirements {
		if r.Element == "deviceType" {
			conds["Composed"] = true
		}
		if !isMandatory(r.Conformance) {
			continue
		}
		c := g.clusterOf(r)
		if c == nil || c.Classification != "application" {
			continue
		}
		switch r.Element {
		case "serverCluster":
			conds["Server"] = true
		case "clientCluster":
			if c.Bindable == nil || *c.Bindable {
				conds["Client"] = true
			}
		default:
		}
	}
	var out []*requirement
	for _, b := range g.snap.BaseDeviceTypes {
		for _, r := range b.Effective.Requirements {
			if r.Element != "serverCluster" || r.ID == nil || *r.ID == descriptorClusterID {
				continue
			}
			if mandatedUnder(r.Conformance.root(), conds) {
				out = append(out, r)
			}
		}
	}
	return out
}

// specialized mirrors ClusterRequirements.isSpecialized
// (ClusterRequirements.ts:61-63).
func (cr *clusterReq) specialized() bool { return len(cr.features) > 0 || len(cr.elements) > 0 }

// ingest mirrors ClusterRequirements's constructor
// (ClusterRequirements.ts:65-100).
func (g *generator) ingest(dt *deviceType, cr *clusterReq) error {
	for _, n := range cr.req.Requirements {
		switch n.Element {
		case "feature":
			if err := g.ingestFeature(dt, cr, n); err != nil {
				return err
			}
		case "attribute", "command", "event":
			// A Descriptor DeviceTypeList requirement states the device type
			// itself, which the generated type carries as its id and
			// revision (requirement-coverage.ts:48-60 isModelCarried).
			if cr.def.ID == descriptorClusterID && n.Element == "attribute" && n.Name == "DeviceTypeList" {
				continue
			}
			if err := g.ingestElement(dt, cr, n); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: %s %s requirement %s nested in cluster %s", errRequirementLost, dt.Name, n.Element, n.Name, cr.def.Name)
		}
	}
	return nil
}

// ingestFeature mirrors ClusterRequirements.ingestFeature
// (ClusterRequirements.ts:102-128): a mandated feature is added; a
// permitted or unstated one is ignored, and so is every other
// disposition, which matter.js only logs.
func (g *generator) ingestFeature(dt *deviceType, cr *clusterReq, n *requirement) error {
	f := cr.def.feature(n.Name)
	if f == nil {
		return fmt.Errorf("%w: %s unknown feature %s for server cluster %s", errRequirementLost, dt.Name, n.Name, cr.def.Name)
	}
	if dispositionOf(n.Conformance, isMandatory(f.Effective.Conformance), int(dt.Revision)) == dispMandate {
		cr.features = append(cr.features, f)
	}
	return nil
}

// ingestElement mirrors ClusterRequirements.ingestElement
// (ClusterRequirements.ts:137-208): Mandate → optional:false, Relax →
// optional:true, the stated default copied, a constraint value → min = max
// = value and default ??= value, else min / max — numeric only.
func (g *generator) ingestElement(dt *deviceType, cr *clusterReq, n *requirement) error {
	e := cr.def.element(n.Element, n.Name)
	if e == nil {
		return fmt.Errorf("%w: %s unknown %s %s for server cluster %s", errRequirementLost, dt.Name, n.Element, n.Name, cr.def.Name)
	}
	er := elementReq{kind: n.Element, name: n.Name, id: e.ID}
	switch dispositionOf(n.Conformance, isMandatory(e.Effective.Conformance), int(dt.Revision)) {
	case dispMandate:
		er.presence = "PresenceMandatory"
	case dispRelax:
		er.presence = "PresenceOptional"
	default:
		// Nothing the device type says about this element changes whether
		// it is required.
	}
	if len(n.Default) > 0 {
		v, ok := numeric(n.Default)
		if !ok {
			return fmt.Errorf("%s %s.%s: the default %s is not numeric, which the device layer cannot carry", dt.Name, cr.def.Name, n.Name, n.Default)
		}
		er.hasDefault, er.def = true, v
	}
	if c := n.Constraint; c != nil {
		if v, ok := numeric(c.Value); ok {
			er.hasMin, er.min, er.hasMax, er.max = true, v, true, v
			if !er.hasDefault {
				er.hasDefault, er.def = true, v
			}
		} else if len(c.Value) == 0 {
			if v, ok := numeric(c.Min); ok {
				er.hasMin, er.min = true, v
			}
			if v, ok := numeric(c.Max); ok {
				er.hasMax, er.max = true, v
			}
		}
	}
	if er.presence == "" && !er.hasDefault && !er.hasMin && !er.hasMax {
		return nil
	}
	cr.elements = append(cr.elements, er)
	return nil
}

// numeric is matter.js FieldValue.numericValue for a JSON value: a number.
func numeric(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var v float64
	if json.Unmarshal(raw, &v) != nil {
		return 0, false
	}
	return v, true
}

// generate renders one device type's file.
func (g *generator) generate(name string) (*output, error) {
	m, err := g.model(name)
	if err != nil {
		return nil, err
	}
	var w bytes.Buffer
	imports := map[string]string{"github.com/SukramJ/go-fabric/schema": ""}
	body, err := g.body(m, imports)
	if err != nil {
		return nil, err
	}
	w.WriteString("// SPDX-License-Identifier: Apache-2.0\n// Copyright (C) 2026 SukramJ.\n\n")
	w.WriteString("// Code generated by script/devicegen from parity/schema.json. DO NOT EDIT.\n\n")
	w.WriteString("package device\n\nimport (\n")
	paths := make([]string, 0, len(imports))
	for p := range imports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if alias := imports[p]; alias != "" {
			fmt.Fprintf(&w, "\t%s %q\n", alias, p)
		} else {
			fmt.Fprintf(&w, "\t%q\n", p)
		}
	}
	w.WriteString(")\n\n")
	w.Write(body)
	out, err := format.Source(w.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%s: format: %w\n%s", name, err, w.Bytes())
	}
	return &output{File: snakeName(name) + "_gen.go", Source: out}, nil
}

// matterjsPath is the matter.js file the device type is generated into
// (EndpointFile.ts:35-49).
func matterjsPath(dt *deviceType) string {
	if dt.Classification == "simple" && !systemEndpointTypes[dt.Name] {
		return "packages/node/src/devices/" + strings.ReplaceAll(snakeName(dt.Name), "_", "-") + ".ts"
	}
	spec := dt.Name
	if spec == "RootNode" {
		spec = "Root"
	}
	return "packages/node/src/endpoints/" + strings.ReplaceAll(snakeName(spec), "_", "-") + ".ts"
}

func (g *generator) body(m *typeModel, imports map[string]string) ([]byte, error) {
	dt := m.dt
	n := exportName(dt.Name)
	var w bytes.Buffer
	fmt.Fprintf(&w, "// %s device type identity: 0x%04X, revision %d.\n", dt.Name, *dt.ID, dt.Revision)
	fmt.Fprintf(&w, "const (\n\t%sID uint16 = 0x%04X\n\t%sRevision uint16 = %d\n)\n\n", n, *dt.ID, n, dt.Revision)
	fmt.Fprintf(&w, "// %sType is the %s device type, from the matter.js model at %s (Matter %s):\n",
		n, dt.Name, shortCommit(g.snap.Matter.SourceCommit), g.snap.Matter.Revision)
	fmt.Fprintf(&w, "// what matter.js generates into %s.\n", matterjsPath(dt))
	fmt.Fprintf(&w, "var %sType = Type{\n", n)
	fmt.Fprintf(&w, "\tID: %sID, Name: %q, Revision: %sRevision,\n", n, dt.Name, n)
	fmt.Fprintf(&w, "\tClassification: %q, Composition: %s,\n", dt.Classification, compositionConst(dt.Effective.Composition))
	if dt.Effective.Base != "" {
		fmt.Fprintf(&w, "\tBase: %q,\n", dt.Effective.Base)
	}
	if len(dt.Effective.Conditions) > 0 {
		fmt.Fprintf(&w, "\tConditions: []string{%s},\n", quoteList(dt.Effective.Conditions))
	}
	for _, side := range []struct {
		field string
		reqs  []*clusterReq
	}{{"Servers", m.servers}, {"Clients", m.clients}} {
		if len(side.reqs) == 0 {
			continue
		}
		fmt.Fprintf(&w, "\t%s: []ClusterRequirement{\n", side.field)
		for _, cr := range side.reqs {
			g.writeRequirement(&w, cr, side.field == "Servers", imports)
		}
		w.WriteString("\t},\n")
	}
	w.WriteString("}\n\n")

	fmt.Fprintf(&w, "// %s is an endpoint of the %s device type.\n", n, dt.Name)
	fmt.Fprintf(&w, "type %s struct{ *Device }\n\n", n)
	fmt.Fprintf(&w, "// New%s returns a %s device: its mandatory server clusters and the\n", n, dt.Name)
	w.WriteString("// optional ones declared, each with its default server or the host's —\n")
	fmt.Fprintf(&w, "// [FromSpec] with Type %sID (see [New]).\n", n)
	fmt.Fprintf(&w, "func New%s(opts Options, decls ...Declaration) (*%s, error) {\n", n, n)
	fmt.Fprintf(&w, "\td, err := FromSpec(Spec{Type: %sID, Options: opts, Declarations: decls})\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn &%s{d}, nil\n}\n", n, n)

	for _, cr := range m.servers {
		acc, ok := g.accessor(cr)
		if !ok {
			continue
		}
		// The typed server's package: the definition's, "def" alias.
		imports[acc.Import] = acc.Alias
		method := exportName(cr.def.Name)
		if deviceMethods[method] {
			return nil, fmt.Errorf("%s: accessor %s shadows a Device method", dt.Name, method)
		}
		state := "// The device type mandates the cluster.\n"
		if !cr.mandatory {
			state = "// It is nil until a declaration mounts the optional cluster.\n"
		}
		fmt.Fprintf(&w, "\n// %s returns the typed %s server over the one the device mounts,\n", method, cr.def.Name)
		w.WriteString("// whatever its type (Underlying returns it); its OnChangedAsync runs on\n// the device's goroutine.\n" + state)
		fmt.Fprintf(&w, "func (d *%s) %s() %s { return %s.WrapOn(d.Server(0x%04X), d.queue) }\n", n, method, acc.Type, acc.Alias, cr.def.ID)
	}
	return w.Bytes(), nil
}

// accessor returns the Go type of a server cluster's typed accessor, when
// the generated type gets one: not for a cluster the assembler mounts, and
// only for a cluster with a committed definition, whose typed server
// (<pkg>def.Server) wraps any server of the cluster — the generated one or
// a self-held one (brief BT, addendum).
func (g *generator) accessor(cr *clusterReq) (serverType, bool) {
	if assemblerMounted[cr.def.Name] {
		return serverType{}, false
	}
	if pkg := packageName(cr.def.Name); g.defs[pkg] {
		return typedServer(pkg), true
	}
	return serverType{}, false
}

func (g *generator) writeRequirement(w *bytes.Buffer, cr *clusterReq, server bool, imports map[string]string) {
	c := cr.def
	fmt.Fprintf(w, "\t\t{\n\t\t\tID: 0x%04X, Name: %q", c.ID, c.Name)
	if t := cr.req.Conformance.text(); t != "" {
		fmt.Fprintf(w, ", Conformance: %q", t)
	}
	w.WriteString(",\n")
	if cr.mandatory {
		w.WriteString("\t\t\tMandatory: true,\n")
	}
	if cr.baseMandated {
		w.WriteString("\t\t\tBaseMandated: true,\n")
	}
	if len(cr.features) > 0 {
		var mask uint32
		names := make([]string, 0, len(cr.features))
		for _, f := range cr.features {
			mask |= 1 << f.Bit
			names = append(names, f.Name)
		}
		fmt.Fprintf(w, "\t\t\tFeatures: 0x%X, FeatureNames: []string{%s},\n", mask, quoteList(names))
	}
	if len(cr.elements) > 0 {
		w.WriteString("\t\t\tElements: []ElementRequirement{\n")
		for _, e := range cr.elements {
			fmt.Fprintf(w, "\t\t\t\t{Element: %s, Name: %q, ID: 0x%04X", elementConst(e.kind), e.name, e.id)
			if e.presence != "" {
				fmt.Fprintf(w, ", Presence: %s", e.presence)
			}
			if e.hasDefault {
				fmt.Fprintf(w, ", HasDefault: true, Default: %s", num(e.def))
			}
			if e.hasMin {
				fmt.Fprintf(w, ", HasMin: true, Min: %s", num(e.min))
			}
			if e.hasMax {
				fmt.Fprintf(w, ", HasMax: true, Max: %s", num(e.max))
			}
			w.WriteString("},\n")
		}
		w.WriteString("\t\t\t},\n")
	}
	if server && g.defs[packageName(c.Name)] {
		pkg := packageName(c.Name)
		alias := pkg + "def"
		imports["github.com/SukramJ/go-fabric/cluster/spec/"+pkg] = alias
		fmt.Fprintf(w, "\t\t\tDefinition: %s.Definition,\n", alias)
	}
	w.WriteString("\t\t},\n")
}

func num(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func quoteList(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = strconv.Quote(s)
	}
	return strings.Join(q, ", ")
}

func compositionConst(c string) string {
	if c == "full-family" {
		return "schema.CompositionFullFamily"
	}
	return "schema.CompositionTree"
}

func elementConst(kind string) string {
	switch kind {
	case "attribute":
		return "schema.RequirementAttribute"
	case "command":
		return "schema.RequirementCommand"
	default:
		return "schema.RequirementEvent"
	}
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// generateRegistry renders device/types_gen.go: every committed type, by
// id.
func (g *generator) generateRegistry(names []string) (*output, error) {
	type entry struct {
		id   uint32
		name string
	}
	entries := make([]entry, 0, len(names))
	for _, name := range names {
		dt := g.snap.deviceType(name)
		if dt == nil || dt.ID == nil {
			return nil, fmt.Errorf("no device type %s with an id in the snapshot", name)
		}
		entries = append(entries, entry{*dt.ID, exportName(name)})
	}
	slices.SortFunc(entries, func(a, b entry) int { return int(a.id) - int(b.id) })
	var w bytes.Buffer
	w.WriteString("// SPDX-License-Identifier: Apache-2.0\n// Copyright (C) 2026 SukramJ.\n\n")
	w.WriteString("// Code generated by script/devicegen from parity/schema.json. DO NOT EDIT.\n\n")
	w.WriteString("package device\n\n")
	w.WriteString("// committedTypes are the generated device types, by id.\n")
	w.WriteString("var committedTypes = []*Type{\n")
	for _, e := range entries {
		fmt.Fprintf(&w, "\t&%sType,\n", e.name)
	}
	w.WriteString("}\n")
	out, err := format.Source(w.Bytes())
	if err != nil {
		return nil, err
	}
	return &output{File: "types_gen.go", Source: out}, nil
}

// generateTest renders device/types_gen_test.go: each generated
// constructor refusing a device without a StableKey, and called with the
// options a host gives a device of the type (the hand-written testWith);
// where it constructs, each accessor is read: a mandatory cluster's is
// mounted, an optional one's is the device's server for the cluster; and
// the device as data (FromSpec with the type's id and the same
// declarations) is the same device, or refused with the same error.
func (g *generator) generateTest(names []string) (*output, error) {
	var w bytes.Buffer
	w.WriteString("// SPDX-License-Identifier: Apache-2.0\n// Copyright (C) 2026 SukramJ.\n\n")
	w.WriteString("// Code generated by script/devicegen from parity/schema.json. DO NOT EDIT.\n\n")
	w.WriteString("package device\n\nimport \"testing\"\n\n")
	w.WriteString("// TestGeneratedConstructorsAndAccessors calls every generated constructor\n")
	w.WriteString("// with a host's options (testWith); a device that constructs has every\n")
	w.WriteString("// mandatory accessor mounted. Which types refuse, and why, is\n")
	w.WriteString("// TestEveryTypeConstructsAndConforms's. FromSpec with the type's id and\n")
	w.WriteString("// the same declarations builds the same device (sameDevice), or refuses\n")
	w.WriteString("// it with the same error.\n")
	w.WriteString("func TestGeneratedConstructorsAndAccessors(t *testing.T) {\n\tt.Parallel()\n")
	for _, name := range names {
		m, err := g.model(name)
		if err != nil {
			return nil, err
		}
		n := exportName(name)
		fmt.Fprintf(&w, "\tif _, err := New%s(Options{}); err == nil {\n\t\tt.Error(\"%s: constructed without a StableKey\")\n\t}\n", n, name)
		fmt.Fprintf(&w, "\tif d, err := New%s(testOptions(%q), testWith(t, %q)...); err == nil {\n", n, name, name)
		fmt.Fprintf(&w, "\t\tif d.Type() != &%sType {\n\t\t\tt.Errorf(\"%s: type %%v\", d.Type().Name)\n\t\t}\n", n, name)
		for _, cr := range m.servers {
			if _, ok := g.accessor(cr); !ok {
				continue
			}
			method := exportName(cr.def.Name)
			if cr.mandatory {
				fmt.Fprintf(&w, "\t\tif s := d.%s(); s == nil || s.Underlying() != d.Server(0x%04X) {\n\t\t\tt.Error(\"%s: mandatory %s not mounted\")\n\t\t}\n", method, cr.def.ID, name, cr.def.Name)
			} else {
				fmt.Fprintf(&w, "\t\tif s, srv := d.%s(), d.Server(0x%04X); (s == nil) != (srv == nil) || s != nil && s.Underlying() != srv {\n\t\t\tt.Error(\"%s: optional %s is not the device's server\")\n\t\t}\n", method, cr.def.ID, name, cr.def.Name)
			}
		}
		fmt.Fprintf(&w, "\t\tif s, err := FromSpec(Spec{Type: %sID, Options: testOptions(%q), Declarations: testWith(t, %q)}); err != nil {\n\t\t\tt.Errorf(\"%s: FromSpec: %%v\", err)\n\t\t} else {\n\t\t\tsameDevice(t, %q, s, d.Device)\n\t\t}\n", n, name, name, name, name)
		fmt.Fprintf(&w, "\t} else if _, serr := FromSpec(Spec{Type: %sID, Options: testOptions(%q), Declarations: testWith(t, %q)}); serr == nil || serr.Error() != err.Error() {\n\t\tt.Errorf(\"%s: FromSpec refuses with %%v, New%s with %%v\", serr, err)\n\t} else if !refusesFixture(%q) {\n\t\tt.Errorf(\"%s: %%v\", err)\n\t}\n", n, name, name, name, n, name, name)
	}
	w.WriteString("}\n")
	out, err := format.Source(w.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format: %w\n%s", err, w.Bytes())
	}
	return &output{File: "types_gen_test.go", Source: out}, nil
}
