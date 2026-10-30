// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/surface"
)

// header is a register head naming two profile ids, as the column
// contract of notes/parity/controller_profiles.md writes it.
const header = "# Controller profiles\n\nColumn contract: `Profile` is the profile id (`alpha`,\n`beta`); `Element` is …\n\n"

const tableHead = "## Register\n\n| Profile | Element | Effect | Confidence | Source | Reason |\n| --- | --- | --- | --- | --- | --- |\n"

func doc(rows ...string) string {
	return header + tableHead + strings.Join(rows, "\n") + "\n\nRows are added only with their evidence.\n"
}

// TestCellForms parses every Element cell form, every effect and every
// confidence the column contract names.
func TestCellForms(t *testing.T) {
	reg, err := parseRegister(doc(
		"| alpha | cluster 0x0201 feature MSCH | warn | Reported | s | r |",
		"| alpha | cluster 0x0201 attribute 0x0000 | remove | Reproduced | s | r |",
		"| alpha | cluster 0x0201 command 0x0000 | warn | Inherited | s | r |",
		"| alpha | cluster 0x0028 event 0x0000 | warn | **Reported** | s | r |",
		"| beta | `device-type 0x0301` | warn | Reported | s \\| t | r |",
		"| beta | level | level 1.4 | Reproduced | s | r |",
	))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(reg.profiles, ",") != "alpha,beta" || len(reg.rules) != 6 {
		t.Fatalf("%+v", reg)
	}
	want := []struct {
		kind        string
		cluster, id uint32
		effect      string
	}{
		{kindFeature, 0x0201, 0x80, "warn"},
		{kindAttribute, 0x0201, 0, "remove"},
		{kindCommand, 0x0201, 0, "warn"},
		{kindEvent, 0x0028, 0, "warn"},
		{kindDeviceType, 0, 0x0301, "warn"},
		{kindLevel, 0, 0, "level"},
	}
	for i, w := range want {
		r := reg.rules[i]
		if r.kind != w.kind || r.cluster != w.cluster || r.id != w.id || r.effect != w.effect {
			t.Errorf("row %d: %+v, want %+v", i, r, w)
		}
	}
	if reg.rules[3].confidence != "Reported" || reg.rules[4].source != "s | t" || reg.rules[5].level != "1.4" {
		t.Fatalf("cells: %+v", reg.rules)
	}
	src, err := goSource(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`Name:  "beta",`, `Level: "1.4",`, "Effect:     EffectLevel,", "Kind: KindDeviceType, Cluster: 0x0000, ID: 0x301"} {
		if !bytes.Contains(src, []byte(s)) {
			t.Errorf("generated source lacks %q:\n%s", s, src)
		}
	}
}

// TestRegisterRules: every rule of the register's contract fails the
// parse, naming the line.
func TestRegisterRules(t *testing.T) {
	for _, c := range []struct {
		name, doc, want string
	}{
		{"unknown profile", doc("| gamma | level | level 1.4 | Reproduced | s | r |"), "profile \"gamma\""},
		{"Reported remove", doc("| alpha | cluster 0x0201 feature MSCH | remove | Reported | s | r |"), "needs the confidence Reproduced"},
		{"Inherited level", doc("| alpha | level | level 1.4 | Inherited | s | r |"), "needs the confidence Reproduced"},
		{"empty source", doc("| alpha | cluster 0x0201 feature MSCH | warn | Reported |  | r |"), "must not be empty"},
		{"empty reason", doc("| alpha | cluster 0x0201 feature MSCH | warn | Reported | s |  |"), "must not be empty"},
		{"confidence", doc("| alpha | cluster 0x0201 feature MSCH | warn | Measured | s | r |"), "confidence \"Measured\""},
		{"effect", doc("| alpha | cluster 0x0201 feature MSCH | hide | Reported | s | r |"), "effect \"hide\""},
		{"level on an element", doc("| alpha | cluster 0x0201 feature MSCH | level 1.4 | Reproduced | s | r |"), "a level effect"},
		{"warn on level", doc("| alpha | level | warn | Reported | s | r |"), "a level effect"},
		{"unknown level", doc("| alpha | level | level 0.9 | Reproduced | s | r |"), "level \"0.9\""},
		{"unknown feature", doc("| alpha | cluster 0x0201 feature NOPE | warn | Reported | s | r |"), "no feature \"NOPE\""},
		{"unknown cluster", doc("| alpha | cluster 0xFFF1 feature MSCH | warn | Reported | s | r |"), "not in the pin's schema"},
		{"unknown attribute", doc("| alpha | cluster 0x0201 attribute 0xFFF0 | warn | Reported | s | r |"), "no attribute 0xFFF0"},
		{"unknown device type", doc("| alpha | device-type 0xFFF1 | warn | Reported | s | r |"), "device type 0xFFF1"},
		{"bad kind", doc("| alpha | cluster 0x0201 bit 0x1 | warn | Reported | s | r |"), "kind \"bit\""},
		{"bad hex", doc("| alpha | cluster 201 feature MSCH | warn | Reported | s | r |"), "no 0x-prefixed hex id"},
		{"bad hex id", doc("| alpha | cluster 0x0201 attribute zz | warn | Reported | s | r |"), "no 0x-prefixed hex id"},
		{"bad device type hex", doc("| alpha | device-type 0x | warn | Reported | s | r |"), "no 0x-prefixed hex id"},
		{"overflow", doc("| alpha | cluster 0x1FFFFFFFF feature MSCH | warn | Reported | s | r |"), "out of range"},
		{"bad cluster hex", doc("| alpha | cluster 0xZZ feature MSCH | warn | Reported | s | r |"), "no 0x-prefixed hex id"},
		{"element form", doc("| alpha | thermostat | warn | Reported | s | r |"), "want level"},
		{"cells", doc("| alpha | level | level 1.4 | Reproduced | s |"), "5 cells, want 6"},
		{"second row", doc(
			"| alpha | cluster 0x0201 feature MSCH | warn | Reported | s | r |",
			"| alpha | cluster 0x0201 feature MSCH | warn | Inherited | s | r |"), "has a row for"},
		{"second level", doc(
			"| alpha | level | level 1.4 | Reproduced | s | r |",
			"| alpha | level | level 1.4.1 | Reproduced | s | r |"), "has a row for"},
		{"no heading", header + "| a |\n", "no heading"},
		{"no ids", "# x\n\n" + tableHead, "names no profile ids"},
		{"repeated id", "`Profile` is the profile id (`a`, `a`)\n\n" + tableHead, "repeat one"},
		{"no table", header + "## Register\n\nNone.\n", "no register table"},
		{"no column", header + "## Register\n\n| Profile | Element |\n| --- | --- |\n", "no \"effect\" column"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseRegister(c.doc)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
		})
	}
	// An empty table is a register with names and no rows.
	reg, err := parseRegister(doc())
	if err != nil || len(reg.rules) != 0 || len(reg.profiles) != 2 {
		t.Fatalf("%+v %v", reg, err)
	}
}

// committed parses the module's own register from the module root.
func committed(t *testing.T) register {
	t.Helper()
	t.Chdir(filepath.Join("..", ".."))
	b, err := os.ReadFile(registerPath)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := parseRegister(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// TestRegisterIsCurrent is the drift guard: the generated file is what
// the committed register generates.
func TestRegisterIsCurrent(t *testing.T) {
	reg := committed(t)
	want, err := goSource(reg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale: run go run ./script/profilegen", goPath)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
}

// TestCommittedRegister holds the committed register to its contract and
// the generated profiles to exactly its rows.
func TestCommittedRegister(t *testing.T) {
	reg := committed(t)
	known := map[string]bool{}
	for _, n := range reg.profiles {
		known[n] = true
	}
	for _, r := range reg.rules {
		switch {
		case !known[r.profile]:
			t.Errorf("line %d: unknown profile %q", r.line, r.profile)
		case r.kind == "":
			t.Errorf("line %d: no element", r.line)
		case goConfidences[r.confidence] == "":
			t.Errorf("line %d: no confidence", r.line)
		case strings.TrimSpace(r.source) == "":
			t.Errorf("line %d: no source", r.line)
		case r.effect == "remove" && r.confidence != "Reproduced":
			t.Errorf("line %d: a remove row with confidence %s; only a Reproduced row removes", r.line, r.confidence)
		}
	}
	profiles := surface.Profiles()
	if len(profiles) != len(reg.profiles) {
		t.Fatalf("%d generated profiles, %d in the register", len(profiles), len(reg.profiles))
	}
	n := 0
	for i, p := range profiles {
		if p.Name != reg.profiles[i] {
			t.Fatalf("profile %d: %q, want %q", i, p.Name, reg.profiles[i])
		}
		var rows []rule
		for _, r := range reg.rules {
			if r.profile == p.Name {
				rows = append(rows, r)
			}
		}
		if len(p.Rules) != len(rows) {
			t.Fatalf("profile %s: %d rules, %d rows", p.Name, len(p.Rules), len(rows))
		}
		for j, g := range p.Rules {
			r := rows[j]
			if g.Element.String() != r.cell || g.Effect.String() != r.effect || string(g.Level) != r.level ||
				g.Confidence.String() != r.confidence || g.Source != r.source || g.Reason != r.reason {
				t.Errorf("profile %s rule %d: %+v, row %+v", p.Name, j, g, r)
			}
			if g.Effect == surface.EffectLevel && p.Level != g.Level {
				t.Errorf("profile %s: level %q, its row %q", p.Name, p.Level, g.Level)
			}
			n++
		}
	}
	if n != len(reg.rules) {
		t.Fatalf("%d generated rules, %d register rows", n, len(reg.rules))
	}
}

func TestWriteIfChanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	for range 2 {
		if err := writeIfChanged(p, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Fatalf("%q", b)
	}
}
