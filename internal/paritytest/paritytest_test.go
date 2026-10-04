// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package paritytest

import "testing"

func TestConformance(t *testing.T) {
	t.Parallel()
	names := map[string]bool{"SPD": true, "Pause": true}
	cases := []struct {
		expr              string
		required, allowed bool
	}{
		{"M", true, true},
		{"O", false, true},
		{"X", false, false},
		{"D", false, false},
		{"SPD", true, true},
		{"[SPD]", false, true},
		{"AUTO", false, false},
		{"AUTO, [SPD]", false, true},
		{"PRSCONST, [AUTO]", false, false},
		{"Pause, O", true, true},
		{"Start, O", false, true},
		{"Pause | Stop | Start | Resume", true, true},
		{"Stop | Start", false, false},
		{"O.a+", false, true},
		{"SPD.a+", true, true},
		{"[Rev >= v2]", false, true},
		{"", false, false},
	}
	for _, tc := range cases {
		if r, a := Conformance(tc.expr, names); r != tc.required || a != tc.allowed {
			t.Errorf("Conformance(%q) = (%v, %v), want (%v, %v)", tc.expr, r, a, tc.required, tc.allowed)
		}
	}
}

func TestClusterSnapshot(t *testing.T) {
	t.Parallel()
	c := ClusterSnapshot(t, 0x0060)
	if c.Name != "OperationalState" || c.Revision == 0 {
		t.Fatalf("0x0060 = %+v", c)
	}
	if a := c.Attribute(t, "CountdownTime"); a.ID != 0x0002 || a.Quality != "X Q" {
		t.Errorf("CountdownTime = %+v", a)
	}
	if cmd := c.Command(t, "Resume"); cmd.Conformance != "Pause, O" || cmd.Response != "OperationalCommandResponse" {
		t.Errorf("Resume = %+v", cmd)
	}
	if e := c.Event(t, "OperationalError"); e.Priority != "critical" {
		t.Errorf("OperationalError = %+v", e)
	}
	if f := ClusterSnapshot(t, 0x0054).Features; len(f) == 0 {
		t.Error("RvcRunMode carries no features")
	}
}

// fatalRecorder records Fatalf instead of stopping the test.
type fatalRecorder struct {
	testing.TB
	failed bool
}

func (r *fatalRecorder) Helper()               {}
func (r *fatalRecorder) Fatalf(string, ...any) { r.failed = true }

func TestLookupsFailOnUnknownNames(t *testing.T) {
	t.Parallel()
	c := ClusterSnapshot(t, 0x0060)
	for name, lookup := range map[string]func(testing.TB){
		"attribute": func(tb testing.TB) { c.Attribute(tb, "Nope") },
		"command":   func(tb testing.TB) { c.Command(tb, "Nope") },
		"event":     func(tb testing.TB) { c.Event(tb, "Nope") },
		"cluster":   func(tb testing.TB) { ClusterSnapshot(tb, 0xFFFF_0000) },
	} {
		rec := &fatalRecorder{TB: t}
		lookup(rec)
		if !rec.failed {
			t.Errorf("unknown %s did not fail", name)
		}
	}
}
