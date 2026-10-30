// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command profilegen generates the controller profiles (concept v11 §C.6,
// brief S5 part 2) from the evidence register
// notes/parity/controller_profiles.md: surface/profiles_gen.go.
//
// The register's header names the profile ids ("`Profile` is the profile
// id (`apple-home`, …)") and its "## Register" table carries the rows,
// columns Profile, Element, Effect, Confidence, Source and Reason. Every
// profile id becomes a profile, with no rule when no row names it. The
// generator fails, naming the line, on a row whose profile the header does
// not name, whose element does not parse or is not in the pin's schema
// (package schema), whose effect or confidence is none of the register's,
// whose source or reason is empty, on a remove or level row that is not
// Reproduced (only a Reproduced row removes, owner decision E.5), on a
// second level row of one profile and on a second row of one profile for
// one element.
//
// It imports package schema for the feature codes and the levels, never
// package surface, so it runs while profiles_gen.go is stale.
//
//	go run ./script/profilegen          # from the module root
package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/schema"
)

const (
	registerPath = "notes/parity/controller_profiles.md"
	goPath       = "surface/profiles_gen.go"
	// tableHeading opens the section of the register table.
	tableHeading = "## Register"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "profilegen:", err)
		os.Exit(1)
	}
}

func run() error {
	doc, err := os.ReadFile(registerPath)
	if err != nil {
		return err
	}
	reg, err := parseRegister(string(doc))
	if err != nil {
		return fmt.Errorf("%s: %w", registerPath, err)
	}
	src, err := goSource(reg)
	if err != nil {
		return err
	}
	return writeIfChanged(goPath, src)
}

// Element kinds of a register row, as surface.Kind names them.
const (
	kindLevel      = "level"
	kindDeviceType = "device-type"
	kindFeature    = "feature"
	kindAttribute  = "attribute"
	kindCommand    = "command"
	kindEvent      = "event"
)

// goKinds are the surface.Kind constants of the element kinds.
var goKinds = map[string]string{
	kindDeviceType: "KindDeviceType", kindFeature: "KindFeature", kindAttribute: "KindAttribute",
	kindCommand: "KindCommand", kindEvent: "KindEvent",
}

// Effects and confidences as the register spells them, with their
// surface constants.
var (
	goEffects     = map[string]string{"remove": "EffectRemove", "warn": "EffectWarn", "level": "EffectLevel"}
	goConfidences = map[string]string{"Reproduced": "Reproduced", "Inherited": "Inherited", "Reported": "Reported"}
)

// rule is one parsed register row.
type rule struct {
	line    int
	profile string
	// cell is the Element cell as the register writes it.
	cell string
	// kind is one of the kind constants; cluster is 0 for a device type
	// and a level row; id is the feature's one-bit mask, the element's id
	// or the device type's.
	kind        string
	cluster, id uint32
	effect      string // remove, warn, level
	level       string // the level of a level row
	confidence  string
	source      string
	reason      string
}

// register is the parsed register: the profile ids of its header, sorted,
// and its rows in table order.
type register struct {
	profiles []string
	rules    []rule
}

var (
	codeSpan    = regexp.MustCompile("`([^`]+)`")
	profileList = regexp.MustCompile("`Profile` is the profile id \\(([^)]*)\\)")
	hexToken    = regexp.MustCompile(`^0x[0-9A-Fa-f]+$`)
)

// parseRegister reads the header's profile ids and the register table.
func parseRegister(doc string) (register, error) {
	var reg register
	head, _, ok := strings.Cut(doc, "\n"+tableHeading+"\n")
	if !ok {
		return reg, fmt.Errorf("no heading %q", tableHeading)
	}
	m := profileList.FindStringSubmatch(strings.Join(strings.Fields(head), " "))
	if m == nil {
		return reg, errors.New("the header names no profile ids (\"`Profile` is the profile id (`…`, …)\")")
	}
	for _, s := range codeSpan.FindAllStringSubmatch(m[1], -1) {
		reg.profiles = append(reg.profiles, s[1])
	}
	slices.Sort(reg.profiles)
	if len(reg.profiles) == 0 || len(slices.Compact(slices.Clone(reg.profiles))) != len(reg.profiles) {
		return reg, fmt.Errorf("the header's profile ids %v are empty or repeat one", reg.profiles)
	}
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == tableHeading {
			start = i
			break
		}
	}
	cols := map[string]int{}
	inTable := false
	for i := start + 1; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "## ") || (inTable && !strings.HasPrefix(l, "|")) {
			break
		}
		if !strings.HasPrefix(l, "|") {
			continue
		}
		cells := splitRow(l)
		if !inTable {
			for c, name := range cells {
				cols[strings.ToLower(name)] = c
			}
			for _, want := range []string{"profile", "element", "effect", "confidence", "source", "reason"} {
				if _, ok := cols[want]; !ok {
					return reg, fmt.Errorf("line %d: the register table has no %q column", i+1, want)
				}
			}
			inTable = true
			continue
		}
		if isSeparator(cells) {
			continue
		}
		if len(cells) != len(cols) {
			return reg, fmt.Errorf("line %d: %d cells, want %d", i+1, len(cells), len(cols))
		}
		cell := func(name string) string { return cells[cols[name]] }
		r, err := parseRule(reg.profiles, i+1, cell)
		if err != nil {
			return reg, err
		}
		reg.rules = append(reg.rules, r)
	}
	if !inTable {
		return reg, errors.New("no register table after " + tableHeading)
	}
	return reg, checkRules(reg.rules)
}

// parseRule parses one row; cell returns a column's trimmed text.
func parseRule(profiles []string, line int, cell func(string) string) (rule, error) {
	r := rule{
		line: line, profile: strings.Trim(cell("profile"), "` "), cell: strings.Trim(cell("element"), "` "),
		confidence: strings.Trim(cell("confidence"), "*` "), source: cell("source"), reason: cell("reason"),
	}
	fail := func(format string, a ...any) (rule, error) {
		return rule{}, fmt.Errorf("line %d: "+format, append([]any{line}, a...)...)
	}
	if !slices.Contains(profiles, r.profile) {
		return fail("profile %q is none the header names (%s)", r.profile, strings.Join(profiles, ", "))
	}
	if err := parseElement(&r); err != nil {
		return fail("element %q: %v", r.cell, err)
	}
	effect := strings.Fields(strings.Trim(cell("effect"), "` "))
	switch {
	case len(effect) == 1 && (effect[0] == "remove" || effect[0] == "warn"):
		r.effect = effect[0]
	case len(effect) == 2 && effect[0] == "level":
		r.effect, r.level = "level", effect[1]
		if !slices.Contains(schema.Levels(), schema.Level(r.level)) {
			return fail("level %q is none of package schema's levels", r.level)
		}
	default:
		return fail("effect %q is not remove, warn or level <level>", cell("effect"))
	}
	if (r.effect == "level") != (r.kind == kindLevel) {
		return fail("a level effect needs the element \"level\", and the element \"level\" a level effect")
	}
	if _, ok := goConfidences[r.confidence]; !ok {
		return fail("confidence %q is not Reproduced, Inherited or Reported", r.confidence)
	}
	if r.source == "" || r.reason == "" {
		return fail("the source and the reason must not be empty")
	}
	if r.effect != "warn" && r.confidence != "Reproduced" {
		return fail("a %s row needs the confidence Reproduced, not %s: only a Reproduced row removes (owner decision E.5)", r.effect, r.confidence)
	}
	return r, nil
}

// parseElement reads r.cell: "level", "device-type <hex>", "cluster <hex>
// feature <code>" or "cluster <hex> attribute|command|event <hex>",
// checked against the pin's schema.
func parseElement(r *rule) error {
	f := strings.Fields(r.cell)
	pin := schema.Levels()[len(schema.Levels())-1]
	switch {
	case len(f) == 1 && f[0] == kindLevel:
		r.kind = kindLevel
		return nil
	case len(f) == 2 && f[0] == kindDeviceType:
		id, err := hexID(f[1])
		if err != nil {
			return err
		}
		if id > 0xFFFF || !schema.DeviceTypeLevels(schema.DeviceTypeID(id)).Has(pin) {
			return fmt.Errorf("device type %s is not in the pin's schema", f[1])
		}
		r.kind, r.id = kindDeviceType, id
		return nil
	case len(f) == 4 && f[0] == "cluster":
	default:
		return errors.New("want level, device-type <hex>, cluster <hex> feature <code> or cluster <hex> attribute|command|event <hex>")
	}
	c, err := hexID(f[1])
	if err != nil {
		return err
	}
	if !schema.ClusterLevels(c).Has(pin) {
		return fmt.Errorf("cluster %s is not in the pin's schema", f[1])
	}
	r.cluster, r.kind = c, f[2]
	if f[2] == kindFeature {
		for _, feat := range schema.ClusterFeatures(c) {
			if feat.Name == f[3] && feat.Bit < 32 {
				r.id = uint32(1) << feat.Bit
				return nil
			}
		}
		return fmt.Errorf("cluster %s has no feature %q in the pin's schema", f[1], f[3])
	}
	levels := map[string]func(uint32, uint32) schema.LevelSet{
		kindAttribute: schema.ClusterAttributeLevels, kindCommand: schema.ClusterCommandLevels, kindEvent: schema.ClusterEventLevels,
	}[f[2]]
	if levels == nil {
		return fmt.Errorf("kind %q is not feature, attribute, command or event", f[2])
	}
	if r.id, err = hexID(f[3]); err != nil {
		return err
	}
	if !levels(c, r.id).Has(pin) {
		return fmt.Errorf("cluster %s has no %s %s in the pin's schema", f[1], f[2], f[3])
	}
	return nil
}

// checkRules rejects a second level row of one profile and a second row of
// one profile for one element.
func checkRules(rules []rule) error {
	type key struct {
		profile, kind string
		cluster, id   uint32
	}
	seen := map[key]int{}
	for _, r := range rules {
		k := key{r.profile, r.kind, r.cluster, r.id}
		if prev, ok := seen[k]; ok {
			return fmt.Errorf("line %d: profile %q has a row for %q already (line %d)", r.line, r.profile, r.cell, prev)
		}
		seen[k] = r.line
	}
	return nil
}

func hexID(s string) (uint32, error) {
	if !hexToken.MatchString(s) {
		return 0, fmt.Errorf("%q is no 0x-prefixed hex id", s)
	}
	v, err := strconv.ParseUint(s[2:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", s, err)
	}
	return uint32(v), nil
}

// splitRow splits a Markdown table row into trimmed cells; "\|" is a pipe
// inside a cell.
func splitRow(l string) []string {
	l = strings.TrimSpace(l)
	l = strings.TrimPrefix(l, "|")
	l = strings.TrimSuffix(l, "|")
	const escaped = "\x00"
	l = strings.ReplaceAll(l, `\|`, escaped)
	parts := strings.Split(l, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(strings.ReplaceAll(parts[i], escaped, "|"))
	}
	return parts
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

// goSource renders surface/profiles_gen.go: one profile per header id,
// sorted by name, its rules in table order.
func goSource(reg register) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Code generated by script/profilegen from notes/parity/controller_profiles.md — DO NOT EDIT.
// Run ` + "`go run ./script/profilegen`" + ` to regenerate.

package surface

// registerProfiles are the controller profiles of the evidence register
// notes/parity/controller_profiles.md, sorted by name: every profile id
// the register's header names, with the register's rows in table order.
var registerProfiles = []Profile{
`)
	for _, name := range reg.profiles {
		var rules []rule
		level := ""
		for _, r := range reg.rules {
			if r.profile == name {
				rules = append(rules, r)
				if r.effect == "level" {
					level = r.level
				}
			}
		}
		if len(rules) == 0 {
			fmt.Fprintf(&b, "{Name: %q},\n", name)
			continue
		}
		fmt.Fprintf(&b, "{\nName: %q,\n", name)
		if level != "" {
			fmt.Fprintf(&b, "Level: %q,\n", level)
		}
		b.WriteString("Rules: []ProfileRule{\n")
		for _, r := range rules {
			fmt.Fprintf(&b, "{ // %s\n", r.cell)
			if r.kind != kindLevel {
				fmt.Fprintf(&b, "Element: ElementPattern{Kind: %s, Cluster: 0x%04X, ID: 0x%X},\n", goKinds[r.kind], r.cluster, r.id)
			}
			fmt.Fprintf(&b, "Effect: %s,\n", goEffects[r.effect])
			if r.level != "" {
				fmt.Fprintf(&b, "Level: %q,\n", r.level)
			}
			fmt.Fprintf(&b, "Confidence: %s,\nSource: %q,\nReason: %q,\n},\n", goConfidences[r.confidence], r.source, r.reason)
		}
		b.WriteString("},\n")
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

// writeIfChanged writes content to path only when it differs from what is
// on disk, keeping mtimes stable on repeated runs.
func writeIfChanged(path string, content []byte) error {
	old, err := os.ReadFile(path) //nolint:gosec // G304: the generator's own fixed output path
	if err == nil && bytes.Equal(old, content) {
		fmt.Printf("  (unchanged: %s)\n", path)
		return nil
	}
	return os.WriteFile(path, content, 0o600) //nolint:gosec // G703: the generator's own fixed output path
}
