// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"fmt"
	"slices"
	"strings"
)

// ReportInput is what the generated report states.
type ReportInput struct {
	Snapshot       SnapshotInfo
	SnapshotSHA256 string
	Chip           Provenance
	HarnessCommit  string
	Result         *Result
	Reconciliation Reconciliation
	Acknowledged   []Acknowledgement
}

var classTitles = []struct {
	class Class
	title string
}{
	{ClassMatterJS, "(i) matter.js is right or deliberately different"},
	{ClassCHIP, "(ii) CHIP is right, the snapshot is wrong"},
	{ClassHarness, "(iv) the harness excuses it"},
}

// Render produces the generated block of docs/chip-datamodel-crosscheck.md.
func (in ReportInput) Render() string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	esc := func(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

	p("### Provenance\n\n")
	p("| Side | Source |\n| --- | --- |\n")
	p("| snapshot | `parity/schema.json`, matter.js `%s`, Matter %s, SHA-256 `%s` |\n",
		in.Snapshot.SourceCommit, in.Snapshot.Revision, in.SnapshotSHA256)
	p("| CHIP | connectedhomeip `%s`, `data_model/%s` (git tree `%s`) |\n", in.Chip.Commit, in.Chip.DataModel, in.Chip.Tree)
	p("| CHIP's source | specification `%s` (`%s`), %s |\n", in.Chip.SpecTag, in.Chip.SpecSHA, in.Chip.Scraper)
	p("| read | at test time from a connectedhomeip checkout; nothing of it is committed (ADR 0015) |\n")
	match := "the same commit"
	if in.HarnessCommit != in.Chip.Commit {
		match = "**a different commit** (`" + in.HarnessCommit + "`)"
	}
	p("| harness | the Makefile's `CHIP_TEST_IMAGE_COMMIT` is %s |\n\n", match)

	p("### Compared\n\n| Element | Compared |\n| --- | ---: |\n")
	total := 0
	for _, k := range sortedKeys(in.Result.Compared) {
		p("| %s | %d |\n", k, in.Result.Compared[k])
		total += in.Result.Compared[k]
	}
	p("| **total** | **%d** |\n\n", total)

	counts := in.Reconciliation.CountByClass(in.Acknowledged)
	normalized := 0
	for _, n := range in.Result.Normalized {
		normalized += n
	}
	p("### Differences by class\n\n| Class | Differences |\n| --- | ---: |\n")
	p("| (i) matter.js right or deliberate | %d |\n", counts[ClassMatterJS])
	p("| (ii) CHIP right, snapshot wrong | %d |\n", counts[ClassCHIP])
	p("| (iii) representation, normalized in code | %d |\n", normalized)
	p("| (iv) excused by the harness | %d |\n", counts[ClassHarness])
	p("| unexplained | %d |\n\n", len(in.Reconciliation.Unexplained))

	p("### (iii) Normalization rules\n\n| Rule | Applied | What it normalizes |\n| --- | ---: | --- |\n")
	for _, k := range sortedKeys(normalizationRules) {
		p("| `%s` | %d | %s |\n", k, in.Result.Normalized[k], esc(normalizationRules[k]))
	}
	p("\n### Not compared\n\nWhat CHIP states and the snapshot does not carry, counted in CHIP's model.\n\n")
	p("| Aspect | CHIP elements | Why |\n| --- | ---: | --- |\n")
	for _, k := range sortedKeys(notCarried) {
		p("| %s | %d | %s |\n", k, in.Result.NotCompared[k], esc(notCarried[k]))
	}

	p("\n### Provisional clusters\n\nCHIP marks these cluster ids provisional. A certification run rejects a provisional " +
		"cluster on a device under test (`device_conformance_tests.py` `check_conformance`), whatever the snapshot says.\n\n")
	for _, name := range in.Result.Provisional {
		p("- %s\n", name)
	}

	for _, ct := range classTitles {
		var rows []int
		for i := range in.Acknowledged {
			if in.Acknowledged[i].Class == ct.class {
				rows = append(rows, i)
			}
		}
		p("\n### %s\n\n", ct.title)
		if ct.class == ClassCHIP {
			for _, i := range rows {
				a := in.Acknowledged[i]
				p("- **%s** (%s): CHIP `%s`, snapshot `%s` — %d difference(s).\n", a.Path, a.Property, a.Chip, a.Ours,
					len(in.Reconciliation.Explained[i]))
				p("  - Why: %s.\n  - Source: %s.\n  - Impact: %s\n", a.Reason, a.Source, a.Impact)
			}
			continue
		}
		p("| Path | Property | CHIP | snapshot | n | Reason | Source |\n| --- | --- | --- | --- | ---: | --- | --- |\n")
		for _, i := range rows {
			a := in.Acknowledged[i]
			p("| %s | %s | `%s` | `%s` | %d | %s | %s |\n", esc(a.Path), a.Property, esc(a.Chip), esc(a.Ours),
				len(in.Reconciliation.Explained[i]), esc(a.Reason), esc(a.Source))
		}
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
