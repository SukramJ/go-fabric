// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Difference is one disagreement between the snapshot and CHIP's data
// model. Chip and Ours are canonical renderings ("absent" / "present" for
// an element only one side has, "undefined" where one side states nothing).
type Difference struct {
	Path     string `json:"path"`
	Property string `json:"property"`
	Chip     string `json:"chip"`
	Ours     string `json:"ours"`
}

func (d Difference) String() string {
	return fmt.Sprintf("%s %s: chip %q, ours %q", d.Path, d.Property, d.Chip, d.Ours)
}

// Result is what a comparison found.
type Result struct {
	Differences []Difference

	// Normalized counts, per rule, the differences that are representation
	// only and were normalized away (class iii). Rules are the keys of
	// normalizationRules.
	Normalized map[string]int

	// Compared counts the elements compared, per kind.
	Compared map[string]int

	// NotCompared counts, per aspect of notCarried, the CHIP elements that
	// state something the snapshot does not carry.
	NotCompared map[string]int

	// Provisional names the clusters CHIP marks provisional, in id order.
	Provisional []string
}

// Normalization rules: differences of representation, not of meaning. Each
// is a port of a tolerance in matter.js's own comparison
// (support/codegen/src/chipdm/compare.ts) unless it says otherwise.
var normalizationRules = map[string]string{
	"global-attribute":     "Global attributes (0xFFF8 and up) are not restated by CHIP's cluster files.",
	"global-datatype":      "CHIP repeats a global data type in each cluster that uses it; the snapshot defines it once.",
	"fabric-index":         "CHIP omits the FabricIndex of a fabric-scoped value because the specification implies it.",
	"datatype-fabric":      "matter.js marks a fabric-scoped struct by its FabricIndex field, CHIP on the struct.",
	"type-metabase":        "CHIP states the primitive or base type a named type builds on (enum8, map8, uint8); the snapshot names the type.",
	"type-alias":           "The specification and CHIP name the same type differently (matter.js by-design.ts TYPE_ALIASES).",
	"value-table-m":        "An enum value, bitmap bit or status code CHIP marks M where the specification's table has no conformance column.",
	"feature-o":            "A feature CHIP marks O where the specification's feature table has no conformance column.",
	"quality-reportable":   "CHIP does not model matter.js's P (reportable) quality.",
	"null-default":         "CHIP writes an explicit null default for a nullable value whose default the specification leaves implicit.",
	"descriptor-required":  "Every device type requires Descriptor; CHIP's device-type files do not restate it.",
	"composed-device-type": "CHIP models the device types a composed device type contains outside its cluster requirements.",
	"member-access":        "The snapshot carries a struct, command or event field's access only where it is fabric-sensitive.",
	"scoped-global":        "CHIP defines the type globally; matter.js scopes it to the cluster that uses it (compared there).",
	"core-global":          "The Interaction Model status codes, event priority and semantic namespace ids are global types of the Core specification that CHIP's data model XML does not restate.",
	"superset-requirement": "A superset device type: matter.js derives it from the subset type and restates the subset's requirements, CHIP lists only its own.",
	"type-bound":           "The snapshot's effective constraint closes a CHIP lower bound with the upper bound of the type (percent 100, percent100ths 10000).",
	"enum-as-integer":      "CHIP types a value enumN where the snapshot states the integer of the same width.",
}

// notCarried names the CHIP aspects the snapshot does not carry, so they are
// not compared; Result.NotCompared counts the CHIP elements stating one.
var notCarried = map[string]string{
	"cluster classification":           "The snapshot does not record a cluster's classification (role / scope).",
	"cluster provisional status":       "The snapshot does not record that a cluster is provisional; the clusters CHIP marks are listed below.",
	"command quality":                  "The snapshot records no quality for commands (CHIP marks large-message commands L).",
	"event quality":                    "The snapshot records no quality for events.",
	"device-type conditions":           "The snapshot does not record the conditions a device type declares.",
	"device-type element requirements": "The snapshot records a device type's cluster requirements, not its feature / attribute / command / event requirements.",
	"device-type cluster quality":      "The snapshot does not record the quality of a device type's cluster requirement (singleton).",
	"semantic namespaces":              "The snapshot carries no semantic namespaces.",
	"global commands":                  "AtomicRequest / AtomicResponse: matter.js models them in the clusters that use them (load-data-model.ts globalCommands).",
	"definitions outside the snapshot": "CHIP global types matter.js defines in a shared definitions scope (WebRtcTransportDefinitions) that the extractor does not emit.",
	"members CHIP does not state":      "A command, event or data type whose fields CHIP leaves unstated because the specification gives them by reference (Level Control's *WithOnOff commands); the snapshot's fields have nothing to be compared with.",
	"base device type":                 "CHIP's Base Device Type has no id and the snapshot extracts only device types with one, so its cluster requirements (counted), which the harness adds to every device type, are not compared.",
}

type comparer struct {
	ours, chip *Model
	res        *Result
	chipBases  map[string]*Cluster
	chipGlobal map[string]bool
	ourGlobal  map[string]*Element
	cluster    *Cluster // the snapshot cluster under comparison
	qualified  map[string]bool
}

// Compare compares the snapshot (ours) with CHIP's data model.
func Compare(ours, chip *Model) *Result {
	c := &comparer{
		ours: ours, chip: chip,
		res:        &Result{Normalized: map[string]int{}, Compared: map[string]int{}, NotCompared: map[string]int{}},
		chipBases:  map[string]*Cluster{},
		chipGlobal: map[string]bool{},
		ourGlobal:  map[string]*Element{},
	}
	// A cluster with an id of its own may still be the base of another, as
	// Operational State is (compare.ts #bases).
	for _, cl := range slices.Concat(chip.Clusters, chip.BaseClusters) {
		c.chipBases[canonicalName(cl.Name)] = cl
	}
	for _, g := range chip.Globals {
		c.chipGlobal[canonicalName(g.Name)] = true
	}
	for _, g := range ours.Globals {
		c.ourGlobal[canonicalName(g.Name)] = g
	}
	for aspect, n := range chip.Uncompared {
		c.notCompared(aspect, n)
	}
	c.compareClusters()
	c.compareDeviceTypes()
	c.compareGlobals()
	slices.SortFunc(c.res.Differences, func(a, b Difference) int {
		return strings.Compare(a.Path+"\x00"+a.Property, b.Path+"\x00"+b.Property)
	})
	return c.res
}

func (c *comparer) report(path []string, property, chip, ours string) {
	c.res.Differences = append(c.res.Differences, Difference{
		Path: strings.Join(path, "."), Property: property, Chip: chip, Ours: ours,
	})
}

func (c *comparer) notCompared(aspect string, n int) {
	if _, ok := notCarried[aspect]; !ok {
		panic("chipdm: unknown uncompared aspect " + aspect)
	}
	if n > 0 {
		c.res.NotCompared[aspect] += n
	}
}

func (c *comparer) normalize(rule string) {
	if _, ok := normalizationRules[rule]; !ok {
		panic("chipdm: unknown normalization rule " + rule)
	}
	c.res.Normalized[rule]++
}

// value reports a difference of one property; a value CHIP does not state
// is no opinion.
func (c *comparer) value(path []string, property, chip, ours string) {
	if chip == "" || chip == ours {
		return
	}
	if ours == "" {
		ours = "undefined"
	}
	c.report(path, property, chip, ours)
}

func hexID(id *uint32) string {
	if id == nil {
		return ""
	}
	return "0x" + strconv.FormatUint(uint64(*id), 16)
}

// --- clusters ----------------------------------------------------------------

func (c *comparer) compareClusters() {
	ours := map[uint32]*Cluster{}
	for _, cl := range c.ours.Clusters {
		ours[*cl.ID] = cl
	}
	seen := map[uint32]bool{}
	for _, chip := range c.chip.Clusters {
		seen[*chip.ID] = true
		cl := ours[*chip.ID]
		if cl == nil {
			c.report([]string{chip.Name}, "cluster", "present", "absent")
			continue
		}
		c.res.Compared["cluster"]++
		if chip.Classification != "" {
			c.notCompared("cluster classification", 1)
		}
		if chip.Provisional {
			c.notCompared("cluster provisional status", 1)
			c.res.Provisional = append(c.res.Provisional, cl.Name)
		}
		c.cluster = cl
		path := []string{cl.Name}
		c.value(path, "name", canonicalName(chip.Name), canonicalName(cl.Name))
		c.value(path, "revision", strconv.Itoa(chip.Revision), strconv.Itoa(cl.Revision))
		c.members(path, c.effective(chip), cl)
	}
	for _, cl := range c.ours.Clusters {
		if !seen[*cl.ID] {
			c.report([]string{cl.Name}, "cluster", "absent", "present")
		}
	}
}

// chipMembers is a CHIP cluster's members with its base chain resolved.
type chipMembers struct {
	features, attributes, commands, events, datatypes []*Element
}

// effective resolves CHIP's inheritance: a derived cluster states only its
// delta over the base (compare.ts #effectiveChildren, inherit).
func (c *comparer) effective(cl *Cluster) chipMembers {
	var out chipMembers
	lists := []struct {
		dst  *[]*Element
		pick func(*Cluster) []*Element
	}{
		{&out.features, func(x *Cluster) []*Element { return x.Features }},
		{&out.attributes, func(x *Cluster) []*Element { return x.Attributes }},
		{&out.commands, func(x *Cluster) []*Element { return x.Commands }},
		{&out.events, func(x *Cluster) []*Element { return x.Events }},
		{&out.datatypes, func(x *Cluster) []*Element { return x.Datatypes }},
	}
	for _, l := range lists {
		index := map[string]int{}
		visited := map[*Cluster]bool{}
		for cur := cl; cur != nil && !visited[cur]; {
			visited[cur] = true
			for _, e := range l.pick(cur) {
				key := canonicalName(e.Name)
				i, ok := index[key]
				switch {
				case !ok:
					index[key] = len(*l.dst)
					*l.dst = append(*l.dst, e)
				case cur != cl:
					(*l.dst)[i] = inherit((*l.dst)[i], e)
				}
			}
			if cur.Base == "" {
				break
			}
			cur = c.chipBases[canonicalName(cur.Base)]
		}
	}
	return out
}

// inherit merges a derived CHIP element over the base element it refines:
// what the derived element states wins, the rest is the base's.
func inherit(derived, base *Element) *Element {
	out := *base
	if derived.ID != nil {
		out.ID = derived.ID
	}
	out.Name = derived.Name
	for _, s := range []struct {
		dst *string
		src string
	}{
		{&out.Kind, derived.Kind},
		{&out.Type, derived.Type},
		{&out.EntryType, derived.EntryType},
		{&out.Constraint, derived.Constraint},
		{&out.Direction, derived.Direction},
		{&out.Response, derived.Response},
		{&out.Priority, derived.Priority},
	} {
		if s.src != "" {
			*s.dst = s.src
		}
	}
	if derived.Conformance != nil {
		out.Conformance = derived.Conformance
	}
	if derived.Access != nil {
		out.Access = derived.Access
	}
	if derived.Quality != nil {
		out.Quality = derived.Quality
	}
	if derived.Default != nil {
		out.Default = derived.Default
	}
	index := map[string]int{}
	out.Fields = nil
	for _, f := range base.Fields {
		index[canonicalName(f.Name)] = len(out.Fields)
		out.Fields = append(out.Fields, f)
	}
	for _, f := range derived.Fields {
		if i, ok := index[canonicalName(f.Name)]; ok {
			out.Fields[i] = inherit(f, out.Fields[i])
			continue
		}
		out.Fields = append(out.Fields, f)
	}
	return &out
}

type elemCtx struct {
	kind         string // attribute, command, event, …
	inValueTable bool
	isFeature    bool
	isDatatype   bool
	isMember     bool
}

func (c *comparer) members(path []string, chip chipMembers, cl *Cluster) {
	// Features by code.
	features := map[string]*Element{}
	for _, f := range cl.Features {
		features[canonicalName(f.Name)] = f
	}
	seenFeatures := map[string]bool{}
	for _, f := range chip.features {
		key := canonicalName(f.Name)
		seenFeatures[key] = true
		ours := features[key]
		if ours == nil {
			c.report(append(slices.Clone(path), f.Name), "feature", "present", "absent")
			continue
		}
		c.res.Compared["feature"]++
		c.element(append(slices.Clone(path), ours.Name), f, ours, elemCtx{isFeature: true})
	}
	for _, f := range cl.Features {
		if !seenFeatures[canonicalName(f.Name)] {
			c.report(append(slices.Clone(path), f.Name), "feature", "absent", "present")
		}
	}

	c.byID(path, "attribute", chip.attributes, cl.Attributes, func(e *Element) string { return hexID(e.ID) })
	c.byID(path, "command", chip.commands, cl.Commands, func(e *Element) string {
		dir := e.Direction
		if dir == "" {
			dir = "request"
		}
		return dir + ":" + hexID(e.ID)
	})
	c.byID(path, "event", chip.events, cl.Events, func(e *Element) string { return hexID(e.ID) })
	c.datatypes(path, chip.datatypes, cl.Datatypes)
}

func (c *comparer) byID(path []string, kind string, chip, ours []*Element, key func(*Element) string) {
	index := map[string]*Element{}
	for _, e := range ours {
		index[key(e)] = e
	}
	seen := map[string]bool{}
	for _, e := range chip {
		k := key(e)
		seen[k] = true
		o := index[k]
		if o == nil {
			c.report(append(slices.Clone(path), e.Name), kind, "present", "absent")
			continue
		}
		c.res.Compared[kind]++
		c.element(append(slices.Clone(path), o.Name), e, o, elemCtx{kind: kind})
	}
	for _, o := range ours {
		if seen[key(o)] {
			continue
		}
		if kind == "attribute" && o.ID != nil && *o.ID >= 0xFFF8 {
			c.normalize("global-attribute")
			continue
		}
		if o.Name == "FabricIndex" {
			c.normalize("fabric-index")
			continue
		}
		c.report(append(slices.Clone(path), o.Name), kind, "absent", "present")
	}
}

func (c *comparer) isGlobalName(name string) bool {
	key := canonicalName(name)
	if c.chipGlobal[key] || c.ourGlobal[key] != nil {
		return true
	}
	if alias, ok := typeAliases[key]; ok && (c.chipGlobal[alias] || c.ourGlobal[alias] != nil) {
		return true
	}
	if ours, ok := chipToOurs[key]; ok && c.ourGlobal[ours] != nil {
		return true
	}
	return false
}

func (c *comparer) datatypes(path []string, chip, ours []*Element) {
	index := map[string]*Element{}
	for _, o := range ours {
		index[canonicalName(o.Name)] = o
	}
	seen := map[string]bool{}
	for _, e := range chip {
		key := canonicalName(e.Name)
		seen[key] = true
		o := index[key]
		if o == nil {
			if c.isGlobalName(e.Name) {
				c.normalize("global-datatype")
				continue
			}
			c.report(append(slices.Clone(path), e.Name), "datatype", "present", "absent")
			continue
		}
		c.res.Compared["datatype"]++
		c.element(append(slices.Clone(path), o.Name), e, o, elemCtx{isDatatype: true})
	}
	for _, o := range ours {
		key := canonicalName(o.Name)
		if seen[key] {
			continue
		}
		if c.isGlobalName(o.Name) {
			c.normalize("global-datatype")
			continue
		}
		c.report(append(slices.Clone(path), o.Name), "datatype", "absent", "present")
	}
}

// element compares one element and its members (compare.ts #element).
func (c *comparer) element(path []string, chip, ours *Element, ctx elemCtx) {
	ourName, chipName := canonicalName(ours.Name), canonicalName(chip.Name)
	if typeAliases[ourName] == chipName && chipName != "" {
		c.normalize("type-alias")
	} else {
		c.value(path, "name", chipName, ourName)
	}
	if chip.Kind == "" || chip.Type != "" {
		c.compareType(path, "type", chip.Type, ours.Type, ours.Metatype, ours.Primitive)
	}
	if chip.Response != "" {
		c.value(path, "response", canonicalName(chip.Response), canonicalName(ours.Response))
	}
	if chip.Priority != "" {
		c.value(path, "priority", canonicalName(chip.Priority), canonicalName(ours.Priority))
	}
	c.value(path, "id", hexID(chip.ID), hexID(ours.ID))
	c.conformance(path, chip, ours, ctx)
	c.constraint(path, chip, ours)
	c.access(path, chip, ours, ctx)
	c.quality(path, chip, ours, ctx)
	c.defaults(path, chip, ours)
	if chip.EntryType != "" && ours.Entry != nil {
		c.compareType(path, "entry type", chip.EntryType, ours.Entry.Type, ours.Entry.Metatype, ours.Entry.Primitive)
	}
	switch {
	case len(chip.Fields) > 0:
		c.fields(path, chip, ours)
	case len(ours.Fields) > 0 && (ctx.kind == "command" || ctx.kind == "event" || ctx.isDatatype):
		// CHIP states no member where the specification gives the members
		// by reference to another element (Level Control's *WithOnOff
		// commands take "the same fields as" their counterparts). No
		// opinion, as in matter.js (compare.ts #element), but counted, so
		// the gap stays visible.
		c.notCompared("members CHIP does not state", 1)
	}
}

func (c *comparer) fields(path []string, chip, ours *Element) {
	inValueTable := ours.Metatype == "enum" || ours.Metatype == "bitmap" ||
		chip.Kind == "enum" || chip.Kind == "bitmap"
	index := map[string]*Element{}
	for _, f := range ours.Fields {
		index[canonicalName(f.Name)] = f
	}
	seen := map[string]bool{}
	for _, f := range chip.Fields {
		key := canonicalName(f.Name)
		seen[key] = true
		o := index[key]
		if o == nil {
			c.report(append(slices.Clone(path), f.Name), "field", "present", "absent")
			continue
		}
		c.res.Compared["field"]++
		c.element(append(slices.Clone(path), o.Name), f, o, elemCtx{inValueTable: inValueTable, isMember: true})
	}
	for _, o := range ours.Fields {
		if seen[canonicalName(o.Name)] {
			continue
		}
		if o.Name == "FabricIndex" {
			c.normalize("fabric-index")
			continue
		}
		c.report(append(slices.Clone(path), o.Name), "field", "absent", "present")
	}
}

// typeName is compare.ts typeName: the canonical last segment.
func typeName(t string) string {
	if i := strings.LastIndex(t, "."); i >= 0 {
		t = t[i+1:]
	}
	return canonicalName(t)
}

// metabaseNames are the names of the type a snapshot type builds on, as
// matter.js's ValueModel.metabase would name it: the primitive, and for an
// enum or a bitmap the enumN / mapN base of that width.
func metabaseNames(metatype, primitive string) []string {
	p := canonicalName(primitive)
	if p == "" {
		return nil
	}
	out := []string{p}
	width := strings.TrimPrefix(p, "uint")
	if width != p {
		switch metatype {
		case "enum":
			out = append(out, "enum"+width)
		case "bitmap":
			out = append(out, "map"+width, "bitmap"+width)
		}
	}
	return out
}

// compareType is compare.ts #type.
func (c *comparer) compareType(path []string, property, chipType, ourType, metatype, primitive string) {
	chip := typeName(chipType)
	if chip == "" {
		return
	}
	ours := typeName(ourType)
	if chip == ours {
		return
	}
	if slices.Contains(metabaseNames(metatype, primitive), chip) {
		c.normalize("type-metabase")
		return
	}
	if ours != "" && typeAliases[ours] == chip {
		c.normalize("type-alias")
		return
	}
	if width, ok := strings.CutPrefix(chip, "enum"); ok && metatype == "integer" && canonicalName(primitive) == "uint"+width {
		c.normalize("enum-as-integer")
		return
	}
	c.value(path, property, chip, ours)
}

func (c *comparer) conformance(path []string, chip, ours *Element, ctx elemCtx) {
	if chip.Conformance == nil {
		return
	}
	chipKey, ourKey := chip.Conformance.Key(), ours.Conformance.Key()
	if chipKey == ourKey {
		return
	}
	if ourKey == "" {
		switch {
		case ctx.inValueTable && chipKey == "m":
			c.normalize("value-table-m")
			return
		case ctx.isFeature && chipKey == "o":
			c.normalize("feature-o")
			return
		}
	}
	c.value(path, "conformance", chipKey, ourKey)
}

var (
	powers       = regexp.MustCompile(`(\d+)\^(\d+)`)
	whitespace   = regexp.MustCompile(`\s+`)
	constraintRm = strings.NewReplacer("%", "", "(", "", ")", "", "[all]", "")
)

// normalizeConstraint is compare.ts normalizeAspect for constraints: case
// and white space do not count, powers are computed, a percentage's unit
// and parentheses are dropped, an unconstrained list entry states nothing.
func normalizeConstraint(text string) string {
	t := whitespace.ReplaceAllString(strings.ToLower(text), "")
	t = powers.ReplaceAllStringFunc(t, func(m string) string {
		parts := powers.FindStringSubmatch(m)
		base, _ := new(big.Int).SetString(parts[1], 10)
		exp, _ := new(big.Int).SetString(parts[2], 10)
		return new(big.Int).Exp(base, exp, nil).String()
	})
	return constraintRm.Replace(t)
}

func (c *comparer) constraint(path []string, chip, ours *Element) {
	chipText := normalizeConstraint(chip.Constraint)
	if chipText == "" {
		return
	}
	ourText := normalizeConstraint(ours.Constraint)
	if lower, ok := strings.CutPrefix(chipText, "min"); ok {
		if bound, known := typeBounds[typeName(ours.Type)]; known && ourText == lower+"to"+bound {
			c.normalize("type-bound")
			return
		}
	}
	c.value(path, "constraint", chipText, ourText)
}

// typeBounds are the upper bounds of the types whose range matter.js folds
// into an element's effective constraint.
var typeBounds = map[string]string{"percent": "100", "percent100ths": "10000"}

// access compares facet by facet: CHIP states only the facets the
// specification lists (compare.ts #access).
func (c *comparer) access(path []string, chip, ours *Element, ctx elemCtx) {
	ca := chip.Access
	if ca == nil {
		return
	}
	oa := ours.Access
	if oa == nil {
		oa = &Access{}
	}
	if ctx.isMember && (ca.Rw != "" || ca.ReadPriv != "" || ca.WritePriv != "" || ca.Timed) && ours.Access == nil {
		c.normalize("member-access")
	} else {
		c.value(path, "access rw", ca.Rw, oa.Rw)
		c.value(path, "access read privilege", ca.ReadPriv, oa.ReadPriv)
		c.value(path, "access write privilege", ca.WritePriv, oa.WritePriv)
	}
	chipFabric, ourFabric := or(ca.Fabric, "absent"), or(oa.Fabric, "absent")
	if ctx.isDatatype && oa.Fabric == "" {
		if ca.Fabric != "" {
			c.normalize("datatype-fabric")
		}
	} else {
		c.value(path, "access fabric", chipFabric, ourFabric)
	}
	if !ctx.isMember || ours.Access != nil {
		c.value(path, "access timed", timedText(ca.Timed), timedText(oa.Timed))
	}
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func timedText(t bool) string {
	if t {
		return "T"
	}
	return "not timed"
}

func (c *comparer) quality(path []string, chip, ours *Element, ctx elemCtx) {
	if chip.Quality == nil {
		return
	}
	if ours.Quality == nil && (ctx.kind == "command" || ctx.kind == "event") {
		c.notCompared(ctx.kind+" quality", 1)
		return
	}
	chipText := strings.Join(chip.Quality, " ")
	var flags []string
	reportable := false
	for _, q := range ours.Quality {
		if q == "reportable" {
			reportable = true
			continue
		}
		flags = append(flags, q)
	}
	ourText := strings.Join(flags, " ")
	if chipText == ourText {
		if reportable {
			c.normalize("quality-reportable")
		}
		return
	}
	if chipText == "" {
		chipText = "none"
	}
	if ourText == "" {
		ourText = "none"
	}
	c.value(path, "quality", chipText, ourText)
}

// defaults is compare.ts #default with defaultKey: CHIP names the enum entry
// a default refers to where the snapshot holds its value, and states
// temperatures and percentages in their encoded units.
func (c *comparer) defaults(path []string, chip, ours *Element) {
	if chip.Default == nil {
		return
	}
	chipKey := c.defaultKey(chip.Default, ours)
	if chipKey == "" {
		return
	}
	ourKey := c.defaultKey(ours.Default, ours)
	if chipKey == "null" && ourKey == "" && slices.Contains(ours.Quality, "nullable") {
		c.normalize("null-default")
		return
	}
	c.value(path, "default", chipKey, ourKey)
}

var unspecified = map[string]bool{"ms": true, "desc": true, "empty": true}

func (c *comparer) defaultKey(v *Value, model *Element) string {
	if v == nil {
		return ""
	}
	if v.Kind == "reference" {
		if id := c.memberID(model, v.Text); id != "" {
			return id
		}
	}
	switch v.Kind {
	case "number":
		return v.Text
	case "bool":
		if v.Text == "true" {
			return "1"
		}
		return "0"
	case "null":
		return "null"
	case "celsius", "percent":
		if n, ok := scaled(v, typeName(model.Type)); ok {
			return n
		}
	case "list":
		// FieldValue.serialize of a list is its elements joined; an empty
		// list states nothing.
		if v.Text == "[]" {
			return ""
		}
	}
	text := strings.ToLower(strings.TrimSpace(v.Text))
	var b strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '+' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if unspecified[out] {
		return ""
	}
	return out
}

// scaled is FieldValue.numericValue for celsius and percent.
func scaled(v *Value, typ string) (string, bool) {
	f, err := strconv.ParseFloat(v.Text, 64)
	if err != nil {
		return "", false
	}
	var factor float64
	switch {
	case v.Kind == "celsius" && (typ == "temperature" || typ == "tempdiff" || typ == "temperaturedifference"):
		factor = 100
	case v.Kind == "celsius" && (typ == "tempu8" || typ == "unsignedtemperature" || typ == "temps8" || typ == "signedtemperature"):
		factor = 10
	case v.Kind == "percent" && typ == "percent100ths":
		factor = 100
	case v.Kind == "percent" && typ == "percent":
		factor = 1
	default:
		return "", false
	}
	return strconv.FormatFloat(f*factor, 'f', -1, 64), true
}

// memberID resolves a default naming an enum entry (or bit) of the
// element's type to that entry's id (matter.js ValueModel.member).
func (c *comparer) memberID(model *Element, name string) string {
	want := canonicalName(name)
	lookup := func(fields []*Element) string {
		for _, f := range fields {
			if canonicalName(f.Name) == want && f.ID != nil {
				return strconv.FormatUint(uint64(*f.ID), 10)
			}
		}
		return ""
	}
	if id := lookup(model.Fields); id != "" {
		return id
	}
	typ := typeName(model.Type)
	if typ == "" {
		return ""
	}
	if c.cluster != nil {
		for _, d := range c.cluster.Datatypes {
			if canonicalName(d.Name) == typ {
				return lookup(d.Fields)
			}
		}
	}
	if g := c.ourGlobal[typ]; g != nil {
		return lookup(g.Fields)
	}
	return ""
}

// --- device types ----------------------------------------------------------------

func (c *comparer) compareDeviceTypes() {
	c.cluster = nil
	ours := map[uint32]*DeviceType{}
	for _, dt := range c.ours.DeviceTypes {
		ours[*dt.ID] = dt
	}
	seen := map[uint32]bool{}
	for _, chip := range c.chip.DeviceTypes {
		if chip.ID == nil {
			c.notCompared("base device type", len(chip.Requirements))
			continue
		}
		seen[*chip.ID] = true
		dt := ours[*chip.ID]
		if dt == nil {
			c.report([]string{chip.Name}, "deviceType", "present", "absent")
			continue
		}
		c.res.Compared["deviceType"]++
		path := []string{dt.Name}
		c.value(path, "name", canonicalName(chip.Name), canonicalName(dt.Name))
		c.value(path, "revision", strconv.Itoa(chip.Revision), strconv.Itoa(dt.Revision))
		c.value(path, "classification", chip.Classification, dt.Classification)
		c.requirements(path, chip, dt, c.chipSuperset(chip))
	}
	for _, dt := range c.ours.DeviceTypes {
		if !seen[*dt.ID] {
			c.report([]string{dt.Name}, "deviceType", "absent", "present")
		}
	}
}

// chipSuperset is the CHIP device type a superset device type extends.
func (c *comparer) chipSuperset(dt *DeviceType) *DeviceType {
	if dt.Superset == "" {
		return nil
	}
	want := canonicalName(dt.Superset)
	for _, other := range c.chip.DeviceTypes {
		if canonicalName(other.Name) == want {
			return other
		}
	}
	return nil
}

func (c *comparer) requirements(path []string, chip, ours, superset *DeviceType) {
	key := func(r *Requirement) string { return r.Side + ":" + canonicalName(r.Name) }
	index := map[string]*Requirement{}
	for _, r := range ours.Requirements {
		index[key(r)] = r
	}
	seen := map[string]bool{}
	for _, r := range chip.Requirements {
		k := key(r)
		seen[k] = true
		o := index[k]
		if o == nil {
			c.report(append(slices.Clone(path), r.Name+" ("+r.Side+")"), "requirement", "present", "absent")
			continue
		}
		c.res.Compared["requirement"]++
		p := append(slices.Clone(path), o.Name+" ("+o.Side+")")
		c.value(p, "id", hexID(&r.ID), hexID(&o.ID))
		if r.Conformance != nil {
			c.value(p, "conformance", r.Conformance.Key(), o.Conformance.Key())
		}
	}
	for _, o := range ours.Requirements {
		if seen[key(o)] {
			continue
		}
		if o.Name == "Descriptor" {
			c.normalize("descriptor-required")
			continue
		}
		if o.Side == "deviceType" {
			c.normalize("composed-device-type")
			continue
		}
		if c.inherited(superset, key(o)) {
			c.normalize("superset-requirement")
			continue
		}
		c.report(append(slices.Clone(path), o.Name+" ("+o.Side+")"), "requirement", "absent", "present")
	}
}

// inherited reports whether a requirement is one of the superset chain's.
func (c *comparer) inherited(superset *DeviceType, key string) bool {
	visited := map[*DeviceType]bool{}
	for dt := superset; dt != nil && !visited[dt]; dt = c.chipSuperset(dt) {
		visited[dt] = true
		for _, r := range dt.Requirements {
			if r.Side+":"+canonicalName(r.Name) == key {
				return true
			}
		}
	}
	return false
}

// --- global data types ----------------------------------------------------------

func (c *comparer) compareGlobals() {
	c.cluster = nil
	seen := map[string]bool{}
	for _, chip := range c.chip.Globals {
		key := canonicalName(chip.Name)
		ours := c.ourGlobal[key]
		if ours == nil {
			if alias, ok := chipToOurs[key]; ok {
				ours = c.ourGlobal[alias]
			}
		}
		if ours == nil {
			if c.definitionScoped(key) {
				// matter.js defines the type in a shared definitions scope
				// (WebRtcTransportDefinitions) the snapshot does not emit;
				// only references to it ("WebRtcTransportDefinitions.X")
				// reach the snapshot.
				c.notCompared("definitions outside the snapshot", 1)
				continue
			}
			if scoped := c.scopedDatatype(key); scoped != nil {
				c.normalize("scoped-global")
				c.res.Compared["global datatype"]++
				c.element([]string{chip.Name}, chip, scoped, elemCtx{isDatatype: true})
				continue
			}
			c.report([]string{chip.Name}, "global datatype", "present", "absent")
			continue
		}
		seen[canonicalName(ours.Name)] = true
		c.res.Compared["global datatype"]++
		c.element([]string{ours.Name}, chip, ours, elemCtx{isDatatype: true})
	}
	for _, g := range c.ours.Globals {
		key := canonicalName(g.Name)
		if seen[key] {
			continue
		}
		if coreGlobals[key] {
			c.normalize("core-global")
			continue
		}
		c.report([]string{g.Name}, "global datatype", "absent", "present")
	}
}

// definitionScoped reports whether a snapshot type reference names the type
// in a scope that is not a cluster of the snapshot.
func (c *comparer) definitionScoped(key string) bool {
	if c.qualified == nil {
		c.qualified = map[string]bool{}
		clusters := map[string]bool{}
		for _, cl := range c.ours.Clusters {
			clusters[cl.Name] = true
		}
		var walk func(e *Element)
		walk = func(e *Element) {
			if e == nil {
				return
			}
			if scope, name, ok := strings.Cut(e.Type, "."); ok && !clusters[scope] {
				c.qualified[canonicalName(name)] = true
			}
			walk(e.Entry)
			for _, f := range e.Fields {
				walk(f)
			}
		}
		for _, cl := range c.ours.Clusters {
			for _, list := range [][]*Element{cl.Attributes, cl.Commands, cl.Events, cl.Datatypes} {
				for _, e := range list {
					walk(e)
				}
			}
		}
	}
	return c.qualified[key]
}

// coreGlobals are the snapshot's global types CHIP's data model XML does not
// restate: the Interaction Model status codes, event priority and the
// semantic namespace ids.
var coreGlobals = map[string]bool{"status": true, "priority": true, "namespace": true}

// scopedDatatype is the snapshot's definition of a type a cluster defines
// where CHIP defines it globally (compare.ts #scopedDatatype); the first
// cluster in id order that defines it.
func (c *comparer) scopedDatatype(key string) *Element {
	for _, cl := range c.ours.Clusters {
		for _, d := range cl.Datatypes {
			if canonicalName(d.Name) == key {
				return d
			}
		}
	}
	return nil
}

// typeAliases is matter.js by-design.ts TYPE_ALIASES: our (the
// specification's) name of a type → CHIP's, both canonical.
var typeAliases = map[string]string{
	"attribid":     "attributeid",
	"currency":     "currencystruct",
	"endpointno":   "endpointid",
	"locationdesc": "locationdescriptorstruct",
	"price":        "pricestruct",
	"semtag":       "semantictagstruct",
	"systimems":    "systemtimems",
	"systimeus":    "systemtimeus",
}

var chipToOurs = func() map[string]string {
	out := map[string]string{}
	for ours, chip := range typeAliases {
		out[chip] = ours
	}
	return out
}()
