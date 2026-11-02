// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// No build tag: this runs in every `go test ./...`.

// clusterFamilies names the CSA certification family (the TC-<FAMILY>-x.y
// prefix of connectedhomeip's cases at the harness commit 6170af84) of
// every cluster the reference daemon serves. A served cluster outside this
// table fails TestServedClustersHaveAFamily, so a new server cannot enter
// the daemon without someone deciding which family certifies it.
var clusterFamilies = map[uint32]string{
	0x0003: "I", 0x0004: "G", 0x0006: "OO", 0x0008: "LVL",
	0x001D: "DESC", 0x001F: "ACL", 0x0025: "ACT", 0x0028: "BINFO",
	0x002A: "SU", 0x002B: "LCFG", 0x002C: "LTIME", 0x002D: "LUNIT",
	0x002E: "PSCFG", 0x002F: "PS", 0x0030: "CGEN", 0x0031: "CNET",
	0x0032: "DLOG", 0x0033: "DGGEN", 0x0034: "DGSW", 0x0036: "DGWIFI",
	0x0037: "DGETH", 0x0038: "TIMESYNC", 0x0039: "BRBINFO", 0x003B: "SWTCH",
	0x003C: "CADMIN", 0x003E: "OPCREDS", 0x003F: "GRPKEY", 0x0040: "FLABEL",
	0x0041: "ULABEL", 0x0045: "BOOL", 0x0048: "OVENOPSTATE", 0x0049: "OTCCM",
	0x004A: "DRYERCTRL", 0x0050: "MOD", 0x0051: "LWM", 0x0052: "TCCM",
	0x0053: "WASHERCTRL", 0x0054: "RVCRUNM", 0x0055: "RVCCLEANM", 0x0056: "TCTL",
	0x0057: "REFALM", 0x005B: "AIRQUAL", 0x005C: "SMOKECO", 0x005D: "DISHALM",
	0x005E: "MWOM", 0x005F: "MWOCTRL", 0x0060: "OPSTATE", 0x0061: "RVCOPSTATE",
	0x0062: "S", 0x0065: "GC", 0x0071: "HEPAFREMON", 0x0072: "ACFREMON",
	0x0080: "BOOLCFG", 0x0081: "VALCC", 0x0090: "EPM", 0x0091: "EEM",
	0x0094: "EWATERHTR", 0x0095: "SEPR", 0x0098: "DEM", 0x0099: "EEVSE",
	0x009B: "EPREF", 0x009C: "PWRTL", 0x009D: "EEVSEM", 0x009E: "WHM",
	0x009F: "DEMM", 0x00A0: "EGC", 0x0101: "DRLK", 0x0102: "WNCV",
	0x0104: "CLCTRL", 0x0105: "CLDIM", 0x0150: "SEAR", 0x0200: "PCC",
	0x0201: "TSTAT", 0x0202: "FAN", 0x0204: "TSUIC", 0x0300: "CC",
	0x0402: "TMP", 0x0404: "FLW", 0x0405: "RH", 0x0406: "OCC",
	0x040C: "CMOCONC", 0x040D: "CDOCONC", 0x0413: "NDOCONC", 0x0415: "OZCONC",
	0x042A: "PMICONC", 0x042B: "FLDCONC", 0x042C: "PMHCONC", 0x042D: "PMKCONC",
	0x042E: "TVOCCONC", 0x042F: "RNCONC", 0x0430: "SOIL", 0x0700: "SETRF",
	0x0750: "ECOINFO", 0x0B06: "MTRID", 0x0B07: "COMMTR",
}

// surfaceSnapshot is the part of script/surfacecheck/surface.json this test
// reads: the clusters the reference daemon serves.
type surfaceSnapshot struct {
	Effective struct {
		Root      surfaceEndpoint   `json:"root"`
		Endpoints []surfaceEndpoint `json:"endpoints"`
	} `json:"effective"`
}

type surfaceEndpoint struct {
	Clusters []struct {
		ID string `json:"id"`
	} `json:"clusters"`
}

// notRunFamilies returns the family codes the first column of
// docs/certifiability.md's "Families this module does not run yet" table
// names.
func notRunFamilies(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", certDocPath))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	const heading = "## Families this module does not run yet"
	i := strings.Index(doc, heading)
	if i < 0 {
		t.Fatalf("%s has no %q section", certDocPath, heading)
	}
	section := doc[i+len(heading):]
	if j := strings.Index(section, "\n## "); j >= 0 {
		section = section[:j]
	}
	code := regexp.MustCompile(`\b[A-Z][A-Z0-9_]+\b`)
	var out []string
	for line := range strings.SplitSeq(section, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Family") || strings.HasPrefix(line, "| ---") {
			continue
		}
		first := strings.SplitN(strings.TrimPrefix(line, "| "), "|", 2)[0]
		out = append(out, code.FindAllString(first, -1)...)
	}
	return out
}

// TestServedClustersHaveAFamily holds the served surface to the
// certification status (W6-test-03): every cluster the reference daemon
// serves (script/surfacecheck/surface.json, "effective") maps to a family
// the harness runs (chipFamilies) or to a row of docs/certifiability.md's
// "Families this module does not run yet" table, so a served cluster cannot
// go uncertified without the status page saying so.
func TestServedClustersHaveAFamily(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "script", "surfacecheck", "surface.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snap surfaceSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	run := map[string]bool{}
	for _, f := range chipFamilies {
		run[f.name] = true
	}
	notRun := notRunFamilies(t)
	seen := map[uint32]bool{}
	for _, ep := range append([]surfaceEndpoint{snap.Effective.Root}, snap.Effective.Endpoints...) {
		for _, c := range ep.Clusters {
			id, err := strconv.ParseUint(c.ID, 0, 32)
			if err != nil {
				t.Fatalf("surface.json cluster id %q: %v", c.ID, err)
			}
			cid := uint32(id)
			if seen[cid] {
				continue
			}
			seen[cid] = true
			fam, ok := clusterFamilies[cid]
			switch {
			case !ok:
				t.Errorf("the reference daemon serves cluster 0x%04X, which clusterFamilies does not map to a certification family", cid)
			case !run[fam] && !slices.Contains(notRun, fam):
				t.Errorf("the reference daemon serves cluster 0x%04X (family %s): add the family to chipFamilies, "+
					"or a row to %s \"Families this module does not run yet\"", cid, fam, certDocPath)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("surface.json lists no served cluster")
	}
}
