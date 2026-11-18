// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// classIISection is the findings register's section that lists the class
// (ii) acknowledgements: CHIP's data model right, the snapshot wrong.
const classIISection = "### CHIP data model cross-check — class (ii)"

// classIIEntry is one entry of that section: a bullet whose first code span
// is the acknowledgement's path.
var classIIEntry = regexp.MustCompile("(?m)^- `([^`]+)`")

// TestClassIIEntriesAreInTheFindingsRegister ties the register to the
// table (R2-P6-doc-2817): every ClassCHIP acknowledgement is a bullet of
// the section ("- `<path>` …"), and every bullet names one. The section
// said each entry leaves "the table, and this list" when the snapshot is
// corrected, and listed none.
func TestClassIIEntriesAreInTheFindingsRegister(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "notes", "parity", "matter_behaviour_findings.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(raw), classIISection+"\n")
	if !ok {
		t.Fatalf("notes/parity/matter_behaviour_findings.md has no %q", classIISection)
	}
	if i := strings.Index(section, "\n### "); i >= 0 {
		section = section[:i]
	}
	matches := classIIEntry.FindAllStringSubmatch(section, -1)
	listed := make([]string, 0, len(matches))
	for _, m := range matches {
		listed = append(listed, m[1])
	}
	var table []string
	for _, a := range Acknowledged {
		if a.Class == ClassCHIP {
			table = append(table, a.Path)
		}
	}
	for _, p := range table {
		if !slices.Contains(listed, p) {
			t.Errorf("acknowledged.go's class (ii) entry %s is not a bullet (- `%s` …) of the register's section %q", p, p, classIISection)
		}
	}
	for _, p := range listed {
		if !slices.Contains(table, p) {
			t.Errorf("the register's section lists %s, which is no class (ii) entry of acknowledged.go — remove it, or the entry was dropped", p)
		}
	}
}
