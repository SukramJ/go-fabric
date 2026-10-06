// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"encoding/json"
	"testing"
)

func TestParseConformance(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ str, key string }{
		"M":                          {"M", "m"},
		"":                           {"", ""},
		"P, O":                       {"P, O", "p,o"},
		"[LT]":                       {"[LT]", "[lt]"},
		"O.a+":                       {"O.a+", "o.a+"},
		"[ElectricalEnergy].a2-":     {"[ElectricalEnergy].a2-", "[electricalenergy].a2-"},
		"LT | DF":                    {"LT | DF", "df|lt"},
		"DF | LT":                    {"DF | LT", "df|lt"},
		"A & B & C":                  {"A & B & C", "a&b&c"},
		"C & (B & A)":                {"C & B & A", "a&b&c"},
		"!(LT | DF)":                 {"!(LT | DF)", "!(df|lt)"},
		"!OFFONLY":                   {"!OFFONLY", "!offonly"},
		"A | B & C":                  {"A | B & C", "a|b&c"},
		"(A | B) & C":                {"(A | B) & C", "(a|b)&c"},
		"A | B ^ C":                  {"A | B ^ C", "(a|b)^c"},
		"StatusCode == Ok, O":        {"StatusCode == Ok, O", "statuscode==ok,o"},
		"Rev >= v3":                  {"Rev >= v3", "rev>=v3"},
		"P, [Rev >= v6]":             {"P, [Rev >= v6]", "p,[rev>=v6]"},
		"NumberOfPrimaries > 0, O":   {"NumberOfPrimaries > 0, O", "numberofprimaries>0,o"},
		"A < 1 | B <= 2 | C >= 3":    {"A < 1 | B <= 2 | C >= 3", "a<1|b<=2|c>=3"},
		"X != null":                  {"X != null", "x!=null"},
		"desc":                       {"desc", "desc"},
		"A B":                        {"A, B", "a,b"},
		"SolicitOffer.VideoStreamID": {"SolicitOffer.VideoStreamID", "solicitoffer.videostreamid"},
		"A.B.C":                      {"A.B.C", "a.b.c"},
		"A.B.a":                      {"A.B.a", "a.b.a"},
		"true | FALSE":               {"true | false", "false|true"},
		"0x10 == 16":                 {"0x10 == 16", "16==16"},
		"Sit or Lit":                 {"Sit | Lit", "lit|sit"},
		"Z, X, D":                    {"Z, X, D", "z,x,d"},
	}
	for in, want := range cases {
		c, err := ParseConformance(in)
		if err != nil {
			t.Errorf("ParseConformance(%q): %v", in, err)
			continue
		}
		if got := c.String(); got != want.str {
			t.Errorf("ParseConformance(%q).String() = %q, want %q", in, got, want.str)
		}
		if got := c.Key(); got != want.key {
			t.Errorf("ParseConformance(%q).Key() = %q, want %q", in, got, want.key)
		}
	}
}

func TestParseConformanceErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"[LT", "LT]", "A &", "(A", "A $ B", "O.", "O.+", "Rev >= 3", "Rev >= vx", "&", "[A &]", "!", "A.a &",
	} {
		if c, err := ParseConformance(in); err == nil {
			t.Errorf("ParseConformance(%q) = %q, want an error", in, c.String())
		}
	}
}

func TestConfFromAST(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`{"type":"M"}`:    "m",
		`{"type":"desc"}`: "desc",
		`{"type":"optionalIf","param":{"type":"!","param":{"type":"name","param":"OFFONLY"}}}`:                                     "[!offonly]",
		`{"type":"otherwise","param":[{"type":"P"},{"type":"O"}]}`:                                                                 "p,o",
		`{"type":"choice","param":{"name":"a","num":2,"orMore":true,"expr":{"type":"O"}}}`:                                         "o.a2+",
		`{"type":"|","param":{"lhs":{"type":"name","param":"B"},"rhs":{"type":"name","param":"A"}}}`:                               "a|b",
		`{"type":"==","param":{"lhs":{"type":"name","param":"S"},"rhs":{"type":"value","param":5}}}`:                               "s==5",
		`{"type":"!=","param":{"lhs":{"type":"name","param":"S"},"rhs":{"type":"value","param":null}}}`:                            "s!=null",
		`{"type":"==","param":{"lhs":{"type":"name","param":"S"},"rhs":{"type":"value","param":true}}}`:                            "s==true",
		`{"type":"==","param":{"lhs":{"type":"name","param":"S"},"rhs":{"type":"name","param":{"type":"reference","name":"Ok"}}}}`: "s==ok",
		`{"type":"revision","param":2}`: "rev>=v2",
		`{"type":".","param":{"lhs":{"type":"name","param":"A"},"rhs":{"type":"name","param":"B"}}}`: "a.b",
	}
	for in, want := range cases {
		c, err := confFromAST(json.RawMessage(in))
		if err != nil {
			t.Errorf("confFromAST(%s): %v", in, err)
			continue
		}
		if got := c.Key(); got != want {
			t.Errorf("confFromAST(%s).Key() = %q, want %q", in, got, want)
		}
	}
	if c, err := confFromAST(json.RawMessage(`{"type":"empty"}`)); err != nil || c != nil {
		t.Errorf("empty = %v, %v", c, err)
	}
	for _, bad := range []string{
		`[]`, `{"type":"bogus"}`, `{"type":"name","param":[1]}`, `{"type":"name","param":{"x":1}}`, `{"type":"name","param":`,
		`{"type":"revision","param":"x"}`, `{"type":"!","param":{"type":"bogus"}}`, `{"type":"otherwise","param":{}}`,
		`{"type":"otherwise","param":[{"type":"bogus"}]}`, `{"type":"choice","param":[]}`,
		`{"type":"choice","param":{"name":"a","expr":{"type":"bogus"}}}`, `{"type":"&","param":[]}`,
		`{"type":"&","param":{"lhs":{"type":"bogus"},"rhs":{"type":"M"}}}`,
		`{"type":"&","param":{"lhs":{"type":"M"},"rhs":{"type":"bogus"}}}`,
	} {
		if _, err := confFromAST(json.RawMessage(bad)); err == nil {
			t.Errorf("confFromAST(%s) succeeded", bad)
		}
	}
}

func TestConfRendering(t *testing.T) {
	t.Parallel()
	var nilConf *Conf
	if nilConf.Key() != "" || nilConf.String() != "" {
		t.Error("a nil conformance renders as nothing")
	}
	broken := &Conf{Op: opNot}
	if got := broken.String(); got != "!??" {
		t.Errorf("an operand-less NOT renders %q", got)
	}
	choiceOfBinary := &Conf{Op: opChoice, Choice: "a", ChoiceNum: 1, OrLess: true, Args: []*Conf{
		{Op: opOr, Args: []*Conf{{Op: opName, Atom: "A"}, {Op: opName, Atom: "B"}}},
	}}
	if got := choiceOfBinary.String(); got != "(A | B).a-" {
		t.Errorf("choice = %q", got)
	}
	notDot := &Conf{Op: opNot, Args: []*Conf{{Op: opDot, Args: []*Conf{{Op: opName, Atom: "A"}, {Op: opName, Atom: "B"}}}}}
	if got := notDot.Key(); got != "!a.b" {
		t.Errorf("not dot = %q", got)
	}
	nested := &Conf{Op: opEQ, Args: []*Conf{{Op: opAnd, Args: []*Conf{{Op: opName, Atom: "A"}, {Op: opName, Atom: "B"}}}, {Op: opValue, Atom: "1"}}}
	if got := nested.String(); got != "(A & B) == 1" {
		t.Errorf("nested = %q", got)
	}
}

func TestParseNumber(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"10": "10", "0x0A": "10", "-0b11": "-3", "2.50": "2.5"} {
		if got, ok := parseNumber(in); !ok || got != want {
			t.Errorf("parseNumber(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "-", "x", ".5", "5.", "1..2", "0xg", "0b2", "1e5"} {
		if got, ok := parseNumber(in); ok {
			t.Errorf("parseNumber(%q) = %q", in, got)
		}
	}
}
