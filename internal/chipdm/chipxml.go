// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The CHIP data model XML, read the way matter.js reads it
// (support/codegen/src/chipdm/load-data-model.ts, translate-conformance.ts,
// translate-constraint.ts, translate-aspects.ts, values.ts). Only what the
// comparison uses is kept: no summaries, no revision history, no PICS codes.

// xmlNode is a generic element of a data model document.
type xmlNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []*xmlNode `xml:",any"`
	Text     string     `xml:",chardata"`
}

func (n *xmlNode) tag() string { return n.XMLName.Local }

func (n *xmlNode) attr(name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value, true
		}
	}
	return "", false
}

func (n *xmlNode) str(name string) string {
	v, _ := n.attr(name)
	return v
}

func (n *xmlNode) children(names ...string) []*xmlNode {
	if len(names) == 0 {
		return n.Children
	}
	var out []*xmlNode
	for _, c := range n.Children {
		if slices.Contains(names, c.tag()) {
			out = append(out, c)
		}
	}
	return out
}

func (n *xmlNode) child(names ...string) *xmlNode {
	for _, c := range n.Children {
		if slices.Contains(names, c.tag()) {
			return c
		}
	}
	return nil
}

// num is matter.js xml.ts num: an absent attribute is nil, a non-numeric
// one an error.
func (n *xmlNode) num(name string) (*uint32, error) {
	v, ok := n.attr(name)
	if !ok {
		return nil, nil
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(v), 0, 32)
	if err != nil {
		return nil, fmt.Errorf("<%s %s=%q> is not numeric", n.tag(), name, v)
	}
	return u32(uint32(parsed)), nil
}

// maybeNum is matter.js xml.ts maybeNum: a non-numeric value (a "code"
// naming a feature) is no number rather than an error.
func (n *xmlNode) maybeNum(name string) *uint32 {
	v, err := n.num(name)
	if err != nil {
		return nil
	}
	return v
}

func (n *xmlNode) boolean(name string) (bool, error) {
	v, ok := n.attr(name)
	if !ok {
		return false, nil
	}
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("<%s %s=%q> is not boolean", n.tag(), name, v)
}

func parseXMLDocument(data []byte, filename string) (*xmlNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var root xmlNode
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	// Anything but white space and comments after the root is malformed.
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filename, err)
		}
		switch t := tok.(type) {
		case xml.Comment, xml.ProcInst:
		case xml.CharData:
			if len(bytes.TrimSpace(t)) != 0 {
				return nil, fmt.Errorf("%s: content after the root element", filename)
			}
		default:
			return nil, fmt.Errorf("%s: content after the root element", filename)
		}
	}
	return &root, nil
}

// xmlLoader accumulates one data model directory.
type xmlLoader struct {
	model Model
}

func (l *xmlLoader) uncompared(aspect string, n int) {
	if n == 0 {
		return
	}
	if l.model.Uncompared == nil {
		l.model.Uncompared = map[string]int{}
	}
	l.model.Uncompared[aspect] += n
}

// addFile reads one XML file of the clusters, device_types or globals
// directory. Namespace files are not compared (the snapshot carries no
// semantic namespaces) and are not passed here.
func (l *xmlLoader) addFile(dir, filename string, data []byte) error {
	root, err := parseXMLDocument(data, filename)
	if err != nil {
		return err
	}
	switch dir {
	case "clusters":
		clusters, err := loadCluster(root, filename)
		if err != nil {
			return err
		}
		for _, c := range clusters {
			if c.ID == nil {
				l.model.BaseClusters = append(l.model.BaseClusters, c)
			} else {
				l.model.Clusters = append(l.model.Clusters, c)
			}
		}
	case "device_types":
		dt, err := loadDeviceType(root, filename)
		if err != nil {
			return err
		}
		l.model.DeviceTypes = append(l.model.DeviceTypes, dt)
		if conds := root.child("conditions"); conds != nil {
			l.uncompared("device-type conditions", len(conds.children("condition")))
		}
		if clusters := root.child("clusters"); clusters != nil {
			for _, c := range clusters.children("cluster") {
				for _, wrapper := range []string{"features", "attributes", "commands", "events"} {
					if w := c.child(wrapper); w != nil {
						l.uncompared("device-type element requirements", len(w.Children))
					}
				}
				if c.child("quality") != nil {
					l.uncompared("device-type cluster quality", 1)
				}
			}
		}
	case "globals":
		for _, node := range root.Children {
			if node.tag() == "command" {
				// Global commands (AtomicRequest, AtomicResponse): matter.js
				// models them in the clusters that use them and does not
				// compare them (load-data-model.ts globalCommands).
				l.uncompared("global commands", 1)
				continue
			}
			dt, err := loadDatatype(node)
			if err != nil {
				return fmt.Errorf("%s: %w", filename, err)
			}
			l.model.Globals = append(l.model.Globals, dt)
		}
	default:
		return fmt.Errorf("%s: unknown data model directory %q", filename, dir)
	}
	return nil
}

// finish orders the model deterministically.
func (l *xmlLoader) finish() *Model {
	m := &l.model
	slices.SortFunc(m.Clusters, func(a, b *Cluster) int { return int(*a.ID) - int(*b.ID) })
	slices.SortFunc(m.BaseClusters, func(a, b *Cluster) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(m.DeviceTypes, func(a, b *DeviceType) int {
		switch {
		case a.ID == nil && b.ID == nil:
			return strings.Compare(a.Name, b.Name)
		case a.ID == nil:
			return -1
		case b.ID == nil:
			return 1
		}
		return int(*a.ID) - int(*b.ID)
	})
	slices.SortFunc(m.Globals, func(a, b *Element) int { return strings.Compare(a.Name, b.Name) })
	return m
}

var clusterSuffix = regexp.MustCompile(`\s*Cluster$`)

// loadCluster mirrors load-data-model.ts loadCluster: a file defines one
// cluster, a family sharing one definition, or a base cluster without id.
func loadCluster(root *xmlNode, filename string) ([]*Cluster, error) {
	if root.tag() != "cluster" {
		return nil, fmt.Errorf("%s: root element is <%s>, expected <cluster>", filename, root.tag())
	}
	revision := 1
	if r, err := root.num("revision"); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	} else if r != nil {
		revision = int(*r)
	}
	template := Cluster{Revision: revision}
	if cl := root.child("classification"); cl != nil {
		template.Classification = clusterClassification(cl)
		template.Base = cl.str("baseCluster")
	}

	var err error
	if template.Features, err = collect(root, "features", []string{"feature"}, loadFeature); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	if template.Attributes, err = collect(root, "attributes", []string{"attribute"}, loadValue); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	if template.Commands, err = collect(root, "commands", []string{"command"}, loadCommand); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	if template.Events, err = collect(root, "events", []string{"event"}, loadEvent); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	if template.Datatypes, err = collect(root, "dataTypes", nil, loadDatatype); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}

	var identities []*xmlNode
	if ids := root.child("clusterIds"); ids != nil {
		identities = ids.children("clusterId")
	}
	var instances []*Cluster
	for _, identity := range identities {
		id, err := identity.num("id")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filename, err)
		}
		if id == nil {
			continue
		}
		name, ok := identity.attr("name")
		if !ok {
			return nil, fmt.Errorf("%s: <clusterId> has no name", filename)
		}
		c := template
		c.ID, c.Name = id, name
		c.Provisional = identity.child("provisionalConform") != nil
		instances = append(instances, &c)
	}
	if len(instances) > 0 {
		return instances, nil
	}
	// A base cluster is named by its id-less clusterId entry, else by the
	// root's name without its "Cluster" suffix.
	base := template
	switch {
	case len(identities) > 0:
		name, ok := identities[0].attr("name")
		if !ok {
			return nil, fmt.Errorf("%s: <clusterId> has no name", filename)
		}
		base.Name = name
	default:
		name, ok := root.attr("name")
		if !ok {
			return nil, fmt.Errorf("%s: <cluster> has no name", filename)
		}
		base.Name = clusterSuffix.ReplaceAllString(name, "")
	}
	return []*Cluster{&base}, nil
}

// clusterClassification mirrors load-data-model.ts clusterClassificationOf.
func clusterClassification(cl *xmlNode) string {
	role := cl.str("role")
	if role == "utility" {
		if strings.EqualFold(cl.str("scope"), "node") {
			return "node"
		}
		return "endpoint"
	}
	return role
}

func collect(parent *xmlNode, wrapper string, names []string, load func(*xmlNode) (*Element, error)) ([]*Element, error) {
	container := parent.child(wrapper)
	if container == nil {
		return nil, nil
	}
	var out []*Element
	for _, node := range container.children(names...) {
		e, err := load(node)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func nameOf(n *xmlNode) (string, error) {
	name, ok := n.attr("name")
	if !ok {
		return "", fmt.Errorf("<%s> has no name", n.tag())
	}
	return name, nil
}

func loadFeature(n *xmlNode) (*Element, error) {
	name, ok := n.attr("code")
	if !ok {
		var err error
		if name, err = nameOf(n); err != nil {
			return nil, err
		}
	}
	e := &Element{Name: name}
	bit, err := n.num("bit")
	if err != nil {
		return nil, err
	}
	if bit != nil {
		e.Constraint = strconv.FormatUint(uint64(*bit), 10)
	}
	if e.Conformance, err = translateConformance(n); err != nil {
		return nil, fmt.Errorf("feature %s: %w", name, err)
	}
	return e, nil
}

// loadValue mirrors load-data-model.ts loadValue (attributes, fields,
// struct-like data types).
func loadValue(n *xmlNode) (*Element, error) {
	name, err := nameOf(n)
	if err != nil {
		return nil, err
	}
	id, err := n.num("id")
	if err != nil {
		return nil, err
	}
	if id == nil {
		id = n.maybeNum("code")
	}
	e := &Element{ID: id, Name: name, Type: n.str("type")}
	if entry := n.child("entry"); entry != nil {
		e.EntryType = entry.str("type")
	}
	if e.Conformance, err = translateConformance(n); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if e.Constraint, err = translateConstraint(n); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if e.Access, err = translateAccess(n); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if e.Quality, err = translateQuality(n); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if d, ok := n.attr("default"); ok {
		e.Default = translateValue(d)
	}
	for _, f := range n.children("field") {
		field, err := loadValue(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		e.Fields = append(e.Fields, field)
	}
	return e, nil
}

func loadCommand(n *xmlNode) (*Element, error) {
	e, err := loadValue(n)
	if err != nil {
		return nil, err
	}
	// A derived cluster restates a command without its direction; it stays
	// open and is inherited (load-data-model.ts loadCommand).
	switch d, ok := n.attr("direction"); {
	case !ok:
	case d == "responseFromServer":
		e.Direction = "response"
	default:
		e.Direction = "request"
	}
	switch r := n.str("response"); r {
	case "", "N":
	case "Y":
		e.Response = "status"
	default:
		e.Response = r
	}
	return e, nil
}

func loadEvent(n *xmlNode) (*Element, error) {
	e, err := loadValue(n)
	if err != nil {
		return nil, err
	}
	e.Priority = n.str("priority")
	return e, nil
}

// loadDatatype mirrors load-data-model.ts loadDatatype.
func loadDatatype(n *xmlNode) (*Element, error) {
	switch n.tag() {
	case "enum":
		e, err := loadValue(n)
		if err != nil {
			return nil, err
		}
		e.Kind = "enum"
		for _, item := range n.children("item") {
			name, err := nameOf(item)
			if err != nil {
				return nil, err
			}
			id, err := item.num("value")
			if err != nil {
				return nil, err
			}
			conf, err := translateConformance(item)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", e.Name, name, err)
			}
			e.Fields = append(e.Fields, &Element{ID: id, Name: name, Conformance: conf})
		}
		return e, nil
	case "bitmap":
		e, err := loadValue(n)
		if err != nil {
			return nil, err
		}
		e.Kind = "bitmap"
		for _, bf := range n.children("bitfield") {
			name, err := nameOf(bf)
			if err != nil {
				return nil, err
			}
			field := &Element{Name: name}
			bit, err := bf.num("bit")
			if err != nil {
				return nil, err
			}
			if bit != nil {
				field.Constraint = strconv.FormatUint(uint64(*bit), 10)
			}
			if field.Conformance, err = translateConformance(bf); err != nil {
				return nil, fmt.Errorf("%s.%s: %w", e.Name, name, err)
			}
			e.Fields = append(e.Fields, field)
		}
		return e, nil
	case "struct", "number", "typedef":
		e, err := loadValue(n)
		if err != nil {
			return nil, err
		}
		e.Kind = n.tag()
		return e, nil
	}
	return nil, fmt.Errorf("unsupported data type element <%s>", n.tag())
}

func loadDeviceType(root *xmlNode, filename string) (*DeviceType, error) {
	if root.tag() != "deviceType" {
		return nil, fmt.Errorf("%s: root element is <%s>, expected <deviceType>", filename, root.tag())
	}
	name, err := nameOf(root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	id, err := root.num("id")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	dt := &DeviceType{ID: id, Name: name, Revision: 1}
	if r, err := root.num("revision"); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	} else if r != nil {
		dt.Revision = int(*r)
	}
	if cl := root.child("classification"); cl != nil {
		dt.Classification = cl.str("class")
		dt.Superset = cl.str("superset")
	}
	if clusters := root.child("clusters"); clusters != nil {
		for _, c := range clusters.children("cluster") {
			req, err := loadClusterRequirement(c)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filename, err)
			}
			dt.Requirements = append(dt.Requirements, req)
		}
	}
	return dt, nil
}

// loadClusterRequirement keeps the cluster-level requirement. The nested
// feature / attribute / command overrides are not read: the snapshot
// carries no element requirements to compare them with (NotCompared).
func loadClusterRequirement(n *xmlNode) (*Requirement, error) {
	name, err := nameOf(n)
	if err != nil {
		return nil, err
	}
	id, err := n.num("id")
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, fmt.Errorf("cluster requirement %s has no id", name)
	}
	side := "server"
	if n.str("side") == "client" {
		side = "client"
	}
	conf, err := translateConformance(n)
	if err != nil {
		return nil, fmt.Errorf("cluster requirement %s: %w", name, err)
	}
	return &Requirement{ID: *id, Name: name, Side: side, Conformance: conf}, nil
}

// --- translate-conformance.ts ----------------------------------------------

var conformanceFlags = map[string]string{
	"mandatoryConform":   opMandatory,
	"optionalConform":    opOptional,
	"provisionalConform": opProvisional,
	"deprecateConform":   opDeprecated,
	"disallowConform":    opDisallowed,
	"obsoleteConform":    opObsolete,
	"describedConform":   opDesc,
}

var conformanceOperators = map[string]string{
	"andTerm":            opAnd,
	"orTerm":             opOr,
	"equalTerm":          opEQ,
	"notEqualTerm":       opNE,
	"greaterTerm":        opGT,
	"greaterOrEqualTerm": opGTE,
	"lessTerm":           opLT,
	"lessOrEqualTerm":    opLTE,
}

func isConformanceTag(tag string) bool {
	_, ok := conformanceFlags[tag]
	return ok || tag == "otherwiseConform"
}

// translateConformance returns nil when CHIP states no conformance, which
// is not the same as empty conformance.
func translateConformance(n *xmlNode) (*Conf, error) {
	var defs []*xmlNode
	for _, c := range n.Children {
		if isConformanceTag(c.tag()) {
			defs = append(defs, c)
		}
	}
	switch len(defs) {
	case 0:
		return nil, nil
	case 1:
		return conformanceOf(defs[0])
	}
	return nil, fmt.Errorf("<%s> has %d conformance definitions", n.tag(), len(defs))
}

func conformanceOf(n *xmlNode) (*Conf, error) {
	if n.tag() == "otherwiseConform" {
		out := &Conf{Op: opOtherwise}
		for _, c := range n.Children {
			sub, err := conformanceOf(c)
			if err != nil {
				return nil, err
			}
			out.Args = append(out.Args, sub)
		}
		return out, nil
	}
	flag, ok := conformanceFlags[n.tag()]
	if !ok {
		return nil, fmt.Errorf("unsupported conformance element <%s>", n.tag())
	}
	exprs := n.Children
	if len(exprs) > 1 {
		return nil, fmt.Errorf("<%s> has %d expressions", n.tag(), len(exprs))
	}
	var c *Conf
	switch {
	case len(exprs) == 0:
		c = &Conf{Op: flag}
	case flag == opMandatory:
		// Conditional mandatory conformance is the bare expression.
		e, err := expressionOf(exprs[0])
		if err != nil {
			return nil, err
		}
		c = e
	case flag == opOptional:
		e, err := expressionOf(exprs[0])
		if err != nil {
			return nil, err
		}
		c = &Conf{Op: opOptionalIf, Args: []*Conf{e}}
	default:
		return nil, fmt.Errorf("<%s> does not support a conditional expression", n.tag())
	}
	return withChoice(n, c)
}

func withChoice(n *xmlNode, c *Conf) (*Conf, error) {
	name, ok := n.attr("choice")
	if !ok {
		return c, nil
	}
	lo, err := n.num("min")
	if err != nil {
		return nil, err
	}
	hi, err := n.num("max")
	if err != nil {
		return nil, err
	}
	if lo != nil && hi != nil && *lo != *hi {
		return nil, fmt.Errorf("choice %s states both a minimum of %d and a maximum of %d", name, *lo, *hi)
	}
	num := 1
	switch {
	case lo != nil:
		num = int(*lo)
	case hi != nil:
		num = int(*hi)
	}
	more, err := n.boolean("more")
	if err != nil {
		return nil, err
	}
	return &Conf{
		Op: opChoice, Args: []*Conf{c}, Choice: name, ChoiceNum: num,
		OrMore: more,
		// matter.js translate-conformance.ts withChoice: only a maximum
		// without a minimum is "or less".
		OrLess: hi != nil && lo == nil,
	}, nil
}

func expressionOf(n *xmlNode) (*Conf, error) {
	if rev, ok, err := revisionOf(n); err != nil || ok {
		return rev, err
	}
	if op, ok := conformanceOperators[n.tag()]; ok {
		if len(n.Children) < 2 {
			return nil, fmt.Errorf("<%s> has %d operands", n.tag(), len(n.Children))
		}
		var acc *Conf
		for _, c := range n.Children {
			e, err := expressionOf(c)
			if err != nil {
				return nil, err
			}
			if acc == nil {
				acc = e
				continue
			}
			acc = &Conf{Op: op, Args: []*Conf{acc, e}}
		}
		return acc, nil
	}
	switch n.tag() {
	case "notTerm":
		if len(n.Children) != 1 {
			return nil, fmt.Errorf("<notTerm> has %d operands", len(n.Children))
		}
		e, err := expressionOf(n.Children[0])
		if err != nil {
			return nil, err
		}
		return &Conf{Op: opNot, Args: []*Conf{e}}, nil
	case "feature", "attribute", "command", "event", "field", "cluster", "condition", "deviceType":
		name, err := confName(n)
		if err != nil {
			return nil, err
		}
		return &Conf{Op: opName, Atom: name}, nil
	case "status":
		name, err := confName(n)
		if err != nil {
			return nil, err
		}
		return &Conf{Op: opName, Atom: statusName(name)}, nil
	case "enum":
		v, ok := n.attr("value")
		if !ok {
			return nil, errors.New("<enum> in conformance has no value")
		}
		return &Conf{Op: opName, Atom: v}, nil
	case "literal", "number", "value":
		if name, ok := n.attr("name"); ok {
			return &Conf{Op: opName, Atom: name}, nil
		}
		text := valueText(n)
		if text == "" {
			return nil, fmt.Errorf("<%s> in conformance has no value", n.tag())
		}
		return &Conf{Op: opValue, Atom: text}, nil
	}
	return nil, fmt.Errorf("unsupported conformance expression element <%s>", n.tag())
}

// revisionOf recognizes CHIP's revision conformance, a comparison of the
// current revision against a literal (translate-conformance.ts revisionOf).
func revisionOf(n *xmlNode) (*Conf, bool, error) {
	if n.tag() != "greaterOrEqualTerm" || len(n.Children) != 2 {
		return nil, false, nil
	}
	for _, c := range n.Children {
		if c.tag() != "revision" {
			return nil, false, nil
		}
	}
	if v := n.Children[0].str("value"); v != "current" {
		return nil, false, fmt.Errorf("unsupported revision comparison against %q", v)
	}
	rev, err := n.Children[1].num("value")
	if err != nil {
		return nil, false, err
	}
	if rev == nil {
		return nil, false, errors.New("revision conformance without a revision")
	}
	return &Conf{Op: opRevision, Rev: int(*rev)}, true, nil
}

func confName(n *xmlNode) (string, error) {
	if v, ok := n.attr("name"); ok {
		return v, nil
	}
	if v, ok := n.attr("code"); ok {
		return v, nil
	}
	return "", fmt.Errorf("<%s> in conformance has no name", n.tag())
}

// statusName is translate-conformance.ts statusNameOf: SUCCESS → Success.
func statusName(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = p[:1] + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, "")
}

// valueText is xml.ts value: the value attribute, else the trimmed text.
func valueText(n *xmlNode) string {
	if v, ok := n.attr("value"); ok {
		return v
	}
	return strings.TrimSpace(n.Text)
}

// --- translate-constraint.ts ------------------------------------------------

// constraintAST is matter.js's Constraint.Ast, with each bound already in
// matter.js's value notation.
type constraintAST struct {
	desc     bool
	value    string
	min, max string
	in       string
	cpMax    string
	entry    *constraintAST
	parts    []*constraintAST
}

func (c *constraintAST) set(key string) bool {
	switch key {
	case "desc":
		return c.desc
	case "value":
		return c.value != ""
	case "min":
		return c.min != ""
	case "max":
		return c.max != ""
	case "cpMax":
		return c.cpMax != ""
	}
	return false
}

// serialize is matter.js Constraint.serialize.
func (c *constraintAST) serialize() string {
	if len(c.parts) > 0 {
		out := make([]string, len(c.parts))
		for i, p := range c.parts {
			out[i] = p.serialize()
		}
		return strings.Join(out, ", ")
	}
	if c.entry != nil {
		return c.atom() + "[" + c.entry.serialize() + "]"
	}
	if c.cpMax != "" {
		return c.atom() + "{" + c.cpMax + "}"
	}
	return c.atom()
}

func (c *constraintAST) atom() string {
	switch {
	case c.desc:
		return "desc"
	case c.value != "":
		return c.value
	case c.min != "" && c.max != "":
		return c.min + " to " + c.max
	case c.min != "":
		return "min " + c.min
	case c.max != "":
		return "max " + c.max
	case c.in != "":
		return "in " + c.in
	}
	return "all"
}

// translateConstraint renders CHIP's constraint of an element in matter.js's
// notation; "" when CHIP states none.
func translateConstraint(n *xmlNode) (string, error) {
	defs := n.children("constraint")
	var entryDef *xmlNode
	if entry := n.child("entry"); entry != nil {
		entryDef = entry.child("constraint")
	}
	if len(defs) == 0 && entryDef == nil {
		return "", nil
	}
	ast := &constraintAST{}
	for _, d := range defs {
		part, err := constraintPart(d)
		if err != nil {
			return "", err
		}
		ast = combineConstraint(ast, part)
	}
	if entryDef != nil {
		entry, err := constraintPart(entryDef)
		if err != nil {
			return "", err
		}
		ast.entry = entry
	}
	return ast.serialize(), nil
}

// combineConstraint is translate-constraint.ts combine: sibling constraints
// on different aspects merge, on the same aspect they are alternatives.
func combineConstraint(ast, addition *constraintAST) *constraintAST {
	if ast.parts != nil {
		return &constraintAST{parts: append(ast.parts, addition)}
	}
	conflict := false
	for _, key := range []string{"desc", "value", "min", "max", "cpMax"} {
		if addition.set(key) && ast.set(key) {
			conflict = true
		}
	}
	if addition.in != "" && ast.in != "" {
		conflict = true
	}
	if conflict {
		return &constraintAST{parts: []*constraintAST{ast, addition}}
	}
	merged := *ast
	if addition.desc {
		merged.desc = true
	}
	for _, kv := range []struct {
		dst *string
		src string
	}{
		{&merged.value, addition.value},
		{&merged.min, addition.min},
		{&merged.max, addition.max},
		{&merged.in, addition.in},
		{&merged.cpMax, addition.cpMax},
	} {
		if kv.src != "" {
			*kv.dst = kv.src
		}
	}
	return &merged
}

func constraintPart(n *xmlNode) (*constraintAST, error) {
	ast := &constraintAST{}
	for _, bound := range n.Children {
		switch bound.tag() {
		case "desc":
			ast.desc = true
		case "allowed":
			v, err := boundOf(bound)
			if err != nil {
				return nil, err
			}
			ast.value = v
		case "between", "countBetween", "lengthBetween":
			from, to := bound.child("from"), bound.child("to")
			if from != nil && to != nil {
				lo, err := boundOf(from)
				if err != nil {
					return nil, err
				}
				hi, err := boundOf(to)
				if err != nil {
					return nil, err
				}
				ast.min, ast.max = lo, hi
				continue
			}
			operands := bound.Children
			lower, hasLower := bound.attr("value")
			switch {
			case hasLower && len(operands) == 1:
				hi, err := constraintExpression(operands[0])
				if err != nil {
					return nil, err
				}
				ast.min, ast.max = translateValue(lower).Text, hi
			case len(operands) == 2:
				lo, err := constraintExpression(operands[0])
				if err != nil {
					return nil, err
				}
				hi, err := constraintExpression(operands[1])
				if err != nil {
					return nil, err
				}
				ast.min, ast.max = lo, hi
			default:
				return nil, fmt.Errorf("<%s> has %d bounds", bound.tag(), len(operands))
			}
		case "min", "minCount", "minLength":
			v, err := boundOf(bound)
			if err != nil {
				return nil, err
			}
			ast.min = v
		case "max", "maxCount", "maxLength":
			v, err := boundOf(bound)
			if err != nil {
				return nil, err
			}
			ast.max = v
		case "maxCodePoints":
			v, err := bound.num("value")
			if err != nil {
				return nil, err
			}
			if v != nil {
				ast.cpMax = strconv.FormatUint(uint64(*v), 10)
			}
		default:
			return nil, fmt.Errorf("unsupported constraint element <%s>", bound.tag())
		}
	}
	return ast, nil
}

// boundOf is a value attribute or one nested expression.
func boundOf(n *xmlNode) (string, error) {
	if v, ok := n.attr("value"); ok {
		return translateValue(v).Text, nil
	}
	if len(n.Children) != 1 {
		return "", fmt.Errorf("<%s> has %d operands", n.tag(), len(n.Children))
	}
	return constraintExpression(n.Children[0])
}

var constraintOperations = map[string]string{"add": "+", "subtract": "-", "multiply": "*", "divide": "/"}

func constraintExpression(n *xmlNode) (string, error) {
	return constraintExpr(n, false)
}

// constraintExpr renders a bound expression as matter.js Constraint.ts
// serializeValue does: a nested arithmetic expression is parenthesized.
func constraintExpr(n *xmlNode, inExpr bool) (string, error) {
	switch n.tag() {
	case "attribute", "field", "feature", "constant":
		name, err := confName(n)
		if err != nil {
			return "", fmt.Errorf("constraint: %w", err)
		}
		path := []string{name}
		for _, c := range n.Children {
			seg, err := confName(c)
			if err != nil {
				return "", fmt.Errorf("constraint: %w", err)
			}
			path = append(path, seg)
		}
		return strings.Join(path, "."), nil
	case "literal", "number", "value", "enum", "bitmap", "status":
		text := valueText(n)
		if text == "" {
			return "", fmt.Errorf("<%s> in constraint has no value", n.tag())
		}
		return translateValue(text).Text, nil
	case "compute":
		opNode := n.child("operation")
		op := ""
		if opNode != nil {
			op = constraintOperations[strings.TrimSpace(opNode.Text)]
		}
		if op == "" {
			return "", errors.New("unsupported constraint operation")
		}
		left, right := n.child("left"), n.child("right")
		if left == nil || right == nil {
			return "", errors.New("<compute> lacks an operand")
		}
		lhs, err := nestedBound(left)
		if err != nil {
			return "", err
		}
		rhs, err := nestedBound(right)
		if err != nil {
			return "", err
		}
		out := lhs + " " + op + " " + rhs
		if inExpr {
			out = "(" + out + ")"
		}
		return out, nil
	case "maxOf", "minOf":
		var args []string
		for _, c := range n.Children {
			a, err := constraintExpr(c, false)
			if err != nil {
				return "", err
			}
			args = append(args, a)
		}
		return n.tag() + "(" + strings.Join(args, ", ") + ")", nil
	}
	return "", fmt.Errorf("unsupported constraint expression element <%s>", n.tag())
}

func nestedBound(n *xmlNode) (string, error) {
	if v, ok := n.attr("value"); ok {
		return translateValue(v).Text, nil
	}
	if len(n.Children) != 1 {
		return "", fmt.Errorf("<%s> has %d operands", n.tag(), len(n.Children))
	}
	return constraintExpr(n.Children[0], true)
}

// --- translate-aspects.ts ----------------------------------------------------

var privileges = map[string]string{"view": "V", "operate": "O", "manage": "M", "admin": "A"}

// translateAccess returns nil when CHIP states no access.
func translateAccess(n *xmlNode) (*Access, error) {
	def := n.child("access")
	if def == nil {
		return nil, nil
	}
	read, err := def.boolean("read")
	if err != nil {
		return nil, err
	}
	write, hasWrite := def.attr("write")
	a := &Access{}
	switch {
	case read && write == "optional":
		a.Rw = "R[W]"
	case read && write == "true":
		a.Rw = "RW"
	case read:
		a.Rw = "R"
	case write == "true":
		a.Rw = "W"
	case hasWrite && write != "false":
		return nil, fmt.Errorf("unsupported write access %q", write)
	}
	sensitive, err := def.boolean("fabricSensitive")
	if err != nil {
		return nil, err
	}
	scoped, err := def.boolean("fabricScoped")
	if err != nil {
		return nil, err
	}
	switch {
	case sensitive:
		a.Fabric = "S"
	case scoped:
		a.Fabric = "F"
	}
	invoke, err := privilege(def, "invokePrivilege")
	if err != nil {
		return nil, err
	}
	if a.ReadPriv, err = privilege(def, "readPrivilege"); err != nil {
		return nil, err
	}
	if a.ReadPriv == "" {
		a.ReadPriv = invoke
	}
	if a.WritePriv, err = privilege(def, "writePrivilege"); err != nil {
		return nil, err
	}
	if a.WritePriv == "" {
		a.WritePriv = invoke
	}
	if a.Timed, err = def.boolean("timed"); err != nil {
		return nil, err
	}
	return a, nil
}

func privilege(n *xmlNode, attr string) (string, error) {
	v, ok := n.attr(attr)
	if !ok {
		return "", nil
	}
	p, ok := privileges[v]
	if !ok {
		return "", fmt.Errorf("unsupported privilege %q", v)
	}
	return p, nil
}

var qualities = []struct{ attr, flag string }{
	{"nullable", "nullable"},
	{"scene", "scene"},
	{"changeOmitted", "changesOmitted"},
	{"quieterReporting", "quieter"},
	{"largeMessage", "largeMessage"},
	{"singleton", "singleton"},
	{"diagnostics", "diagnostics"},
	{"atomicWrite", "atomic"},
}

var persistence = map[string]string{"nonVolatile": "nonvolatile", "fixed": "fixed"}

// translateQuality returns nil when CHIP states no quality, and the sorted
// flags otherwise (an empty, non-nil set when it states only false flags).
func translateQuality(n *xmlNode) ([]string, error) {
	def := n.child("quality")
	if def == nil {
		return nil, nil
	}
	out := []string{}
	for _, q := range qualities {
		set, err := def.boolean(q.attr)
		if err != nil {
			return nil, err
		}
		if set {
			out = append(out, q.flag)
		}
	}
	if p, ok := def.attr("persistence"); ok {
		flag, ok := persistence[p]
		if !ok {
			return nil, fmt.Errorf("unsupported quality persistence %q", p)
		}
		out = append(out, flag)
	}
	slices.Sort(out)
	return out, nil
}

// --- values.ts -----------------------------------------------------------------

// translateValue is values.ts translateValue: null, booleans and numbers are
// literals, anything else a reference.
func translateValue(text string) *Value {
	switch text {
	case "null":
		return &Value{Kind: "null", Text: "null"}
	case "true", "false":
		return &Value{Kind: "bool", Text: text}
	}
	if n, ok := parseNumber(text); ok {
		return &Value{Kind: "number", Text: n}
	}
	return &Value{Kind: "reference", Text: text}
}
