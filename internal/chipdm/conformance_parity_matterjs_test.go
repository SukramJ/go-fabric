// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chipdm

import "testing"

// TestConformanceTextParityMatterJS mirrors
// packages/model/test/aspects/ConformanceTest.ts:18-100 at 862ad75f
// (TEST_DEFINITIONS and TEST_DEFINITIONS2, "parses" and "serializes"):
// every conformance text matter.js parses, parsed by the module's
// conformance parser (the one the CHIP data model cross-check reads chip's
// XML with) and rendered back as matter.js serializes it.
func TestConformanceTextParityMatterJS(t *testing.T) {
	t.Parallel()
	same := []string{
		"M", "O", "P", "D", "X", "Z", "WBL", "AX | WBL", "AX, WBL",
		"HA | OI | AUD | OC & OI", "P, HA | OI | AUD | OC & OI", "P, HA | OI | AUD",
		"[WIRED]", "!AB", "!(LT | DF)", "[!(LT | DF)]", "!(LT & DF)", "!(A | B | C)",
		"AB.a", "AB.a+", "AB.a2", "AB.a2+", "AB.a2-", "AB.a-", "AB == 2", "Mom", "[AB].a",
		"[LF & PA_LF & ABS]", "!USR & (PIN | RID | FGP)", "OperationalStateID >= 128 & OperationalStateID <= 191",
		"SolicitOffer.VideoStreamID", "Foo.Bar.Baz", "[Foo.Bar]", "Foo.Bar & FEAT", "foo.barBaz",
		"Rev >= v3", "[Rev >= v2]", "Rev >= v4, [Rev >= v2]",
		"ContainerType == CMAF", "TriggerType == Motion",
		"TimeSyncCond, O", "[Cooler]", "[TLSCertificatesCond | TLSClientCond].a+, O",
	}
	cases := map[string]string{
		"(AX | WBL)":                    "AX | WBL",
		"HA | OI | AUD | (OC & OI)":     "HA | OI | AUD | OC & OI",
		"P, HA | OI | AUD | (OC & OI)":  "P, HA | OI | AUD | OC & OI",
		"[!(LT)]":                       "[!LT]",
		"!((LT | DF))":                  "!(LT | DF)",
		"RequiresEncodedPixels == True": "RequiresEncodedPixels == true",
		"Enabled == False":              "Enabled == false",
	}
	for _, d := range same {
		cases[d] = d
	}
	for in, want := range cases {
		c, err := ParseConformance(in)
		if err != nil {
			t.Errorf("parses %q: %v", in, err)
			continue
		}
		if got := c.String(); got != want {
			t.Errorf("serializes %q as %q, want %q", in, got, want)
		}
	}
}

// TestConformanceInvalidParityMatterJS mirrors
// packages/model/test/aspects/ConformanceTest.ts:106-155 at 862ad75f: a
// text that is not conformance, a range-form choice, mixed choice
// modifiers and partial optionality are refused.
func TestConformanceInvalidParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"%", "O.a2-4", "O.a2+4", "O.a-4", "AB.a2+-", "AB.a2-+", "[AA] & BB", "[AA] | BB", "[AA] & BB, CC"} {
		if c, err := ParseConformance(in); err == nil {
			t.Errorf("ParseConformance(%q) = %q, want an error", in, c.String())
		}
	}
}

// TestConformanceRevisionParityMatterJS mirrors
// packages/model/test/aspects/ConformanceTest.ts:676-698 at 862ad75f:
// "Rev >= vN" is one revision node, "[Rev >= vN]" an optional group of it,
// "Rev >= vN, [Rev >= vM]" a list, each serialized with its "v".
func TestConformanceRevisionParityMatterJS(t *testing.T) {
	t.Parallel()
	for in, op := range map[string]string{"Rev >= v3": opRevision, "[Rev >= v2]": opOptionalIf, "Rev >= v4, [Rev >= v2]": opOtherwise} {
		c, err := ParseConformance(in)
		if err != nil || c.Op != op || c.String() != in {
			t.Errorf("ParseConformance(%q) = %v (%v), want %s serialized as given", in, c, err, op)
		}
	}
}

// TestConformancePrecedenceParityMatterJS mirrors
// packages/model/test/aspects/ConformanceTest.ts:339-398 at 862ad75f
// (describe "operator precedence") and :180-182 ("Z.Foo" a qualified
// name): == binds tighter than |, a comparison tighter than ==, and a
// range check is the & of two comparisons.
func TestConformancePrecedenceParityMatterJS(t *testing.T) {
	t.Parallel()
	shape := func(in string) *Conf {
		t.Helper()
		c, err := ParseConformance(in)
		if err != nil {
			t.Fatalf("ParseConformance(%q): %v", in, err)
		}
		return c
	}
	if c := shape("TriggerType == Command | TriggerType == Motion"); c.Op != "|" || c.Args[0].Op != "==" || c.Args[1].Op != "==" {
		t.Errorf("groups == higher than |: %s", c.Key())
	}
	if c := shape("A >= B == C"); c.Op != "==" || c.Args[0].Op != ">=" {
		t.Errorf("groups comparisons higher than equality: %s", c.Key())
	}
	c := shape("OperationalStateID >= 128 & OperationalStateID <= 191")
	if c.Op != "&" || c.Args[0].Op != ">=" || c.Args[1].Op != "<=" ||
		c.Args[0].Args[0].Atom != "OperationalStateID" || c.Args[0].Args[1].Atom != "128" || c.Args[1].Args[1].Atom != "191" {
		t.Errorf("groups range check: %s", c.Key())
	}
	if c := shape("Z.Foo"); c.Op != opDot {
		t.Errorf("parses \"Z.Foo\" as a qualified name: op %s", c.Op)
	}
}
