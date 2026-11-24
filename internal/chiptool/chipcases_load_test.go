// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"slices"
	"testing"
)

// swtchDescriptor is the shape /lib/test-descriptor.json gives TC_SWTCH.py
// at chip 6170af84 (the harness image's descriptor, read 2026-11-24): 2.2
// a plain case of run1, 2.3 a multi-run suite of run2 and run3 only (the
// file's CI arguments name test_TC_SWTCH_2_3 in run2 and run3, :40-63),
// 2.6 a plain case of run2. Configs cut to the endpoint.
const swtchDescriptor = `{"name": "chip", "format": 2, "kind": "suite", "members": [
 {"name": "SWTCH", "kind": "suite", "members": [
  {"kind": "py", "name": "2.2", "path": "/src/python_testing/TC_SWTCH.py", "subpath": "test_TC_SWTCH_2_2",
   "config": {"script-args": ["--endpoint", "1"]}},
  {"kind": "suite", "path": "/src/python_testing/TC_SWTCH.py", "name": "2.3", "members": [
   {"kind": "py", "name": "run2", "path": "/src/python_testing/TC_SWTCH.py", "subpath": "test_TC_SWTCH_2_3",
    "config": {"script-args": ["--endpoint", "3"]}},
   {"kind": "py", "name": "run3", "path": "/src/python_testing/TC_SWTCH.py", "subpath": "test_TC_SWTCH_2_3",
    "config": {"script-args": ["--endpoint", "4"]}}]},
  {"kind": "py", "name": "2.6", "path": "/src/python_testing/TC_SWTCH.py", "subpath": "test_TC_SWTCH_2_6",
   "config": {"script-args": ["--endpoint", "3"]}}]},
 {"name": "X", "kind": "suite", "members": [
  {"kind": "suite", "path": "/src/python_testing/TC_X.py", "name": "1.1", "members": [
   {"kind": "py", "name": "run2", "path": "/src/python_testing/TC_X.py", "config": {"script-args": ["--endpoint", "2"]}},
   {"kind": "py", "name": "run1", "path": "/src/python_testing/TC_X.py", "config": {"script-args": ["--endpoint", "1"]}}]}]}]}`

// TestParseChipCasesMultiRun holds decision 648 of review II: a multi-run
// Python case keeps one run — run1 when the descriptor lists it, else the
// lowest-numbered run — so a case CHIP's CI runs only in run2 and run3
// (TC-SWTCH-2.3, 2.4) enters its family instead of being dropped.
func TestParseChipCasesMultiRun(t *testing.T) {
	cases, err := parseChipCases([]byte(swtchDescriptor))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cases["SWTCH"] {
		names = append(names, c.Name)
	}
	if want := []string{"2.2", "2.3", "2.6"}; !slices.Equal(names, want) {
		t.Fatalf("SWTCH cases %v, want %v", names, want)
	}
	if got := cases["SWTCH"][1]; got.ID != "SWTCH/2.3" || got.Subpath != "test_TC_SWTCH_2_3" || !slices.Equal(got.Args, []string{"--endpoint", "3"}) {
		t.Fatalf("SWTCH/2.3 = %+v, want run2's (subpath test_TC_SWTCH_2_3, --endpoint 3)", got)
	}
	if got := cases["X"]; len(got) != 1 || !slices.Equal(got[0].Args, []string{"--endpoint", "1"}) {
		t.Fatalf("X = %+v, want run1's case although run2 is listed first", got)
	}
}
