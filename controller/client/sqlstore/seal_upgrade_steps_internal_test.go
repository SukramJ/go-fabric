// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlstore

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

// sealBaselineVersion is the schema step whose secrets [sealedAtBaseline]
// lists: the list as it stood when the seal canary came in, at v3.
const sealBaselineVersion = "v3"

// sealedAtBaseline is every secret [NewSealed] seals at
// [sealBaselineVersion], in the form [secretEntries] names them, and the
// aad scheme they are sealed under. These need no upgrade step: a
// registry the canary marks never holds them in clear — they are exempt
// by being in v3, nothing else.
var sealedAtBaseline = []string{
	"aad go-fabric/controller/client/sqlstore/seal/v1 (matter_client_values, value, node_id, endpoint, cluster, attribute)",
	"value cluster 0xFFFFFFFC attribute 0",
	"value cluster 0xFFFFFFFD attribute 0",
	"value cluster 0xFFFFFFFD attribute 1",
}

// sealUpgradeSteps names, for every secret added after
// [sealBaselineVersion], the test that proves its upgrade step: a marked
// registry that holds the value in clear — written by a sealed store while
// it was not yet a secret — opens under [NewSealed], the value sealed.
// Empty: no secret has been added since.
var sealUpgradeSteps = map[string]string{}

// secretEntries names every secret the store seals, one entry per
// secretValues entry, and the aad scheme of matter_client_values: a
// changed scheme is a new secret to every sealed row.
func secretEntries() []string {
	out := make([]string, 0, 1+len(secretValues))
	out = append(out, "aad "+sealAADPrefix+" (matter_client_values, value, node_id, endpoint, cluster, attribute)")
	for _, v := range secretValues {
		out = append(out, fmt.Sprintf("value cluster 0x%X attribute %d", v.cluster, v.attribute))
	}
	return out
}

// TestSecretsAddedAfterV3ShipAnUpgradeStep (decision 157, store's
// TestSecretsAddedAfterV11ShipAnUpgradeStep for the registry) holds the
// class "a value becomes a secret in a registry the canary already
// marks": in a marked registry [NewSealed] refuses a clear secret value
// (openMarked, clearSecretValue), so a release that adds an entry to
// secretValues, or changes the aad scheme, meets registries that hold the
// value in clear, or sealed under another aad, and they stop opening.
// Every such entry ships with an upgrade step that seals it there first,
// and with the test that proves the step, named in sealUpgradeSteps; the
// v3 baseline is exempt by being in v3.
func TestSecretsAddedAfterV3ShipAnUpgradeStep(t *testing.T) {
	t.Parallel()
	if !slices.Contains(migrationVersions, sealBaselineVersion) {
		t.Fatalf("the baseline names schema step %s, which migrationVersions does not hold", sealBaselineVersion)
	}
	tests := testFuncNames(t)
	current := secretEntries()
	for _, e := range current {
		if slices.Contains(sealedAtBaseline, e) {
			if step, ok := sealUpgradeSteps[e]; ok {
				t.Errorf("sealUpgradeSteps names %s for %q, which the %s baseline already holds: drop the entry", step, e, sealBaselineVersion)
			}
			continue
		}
		step, ok := sealUpgradeSteps[e]
		if !ok {
			t.Errorf("%q is a secret added after %s without an upgrade step: a registry the seal canary marks may hold it in clear "+
				"and NewSealed then refuses to open it. Add the step that seals it there, and name the test that proves it in sealUpgradeSteps", e, sealBaselineVersion)
			continue
		}
		if !tests[step] {
			t.Errorf("sealUpgradeSteps names %s for %q, but the package has no such test", step, e)
		}
	}
	for _, e := range sealedAtBaseline {
		if !slices.Contains(current, e) {
			t.Errorf("the %s baseline lists %q, which the store no longer seals in this form: a marked registry holds it sealed so, and dropping or changing it needs its own step; update the baseline with that step", sealBaselineVersion, e)
		}
	}
	for e := range sealUpgradeSteps {
		if !slices.Contains(current, e) {
			t.Errorf("sealUpgradeSteps names %q, which the store does not seal", e)
		}
	}
}

// testFuncNames returns the names of the package's Test functions.
func testFuncNames(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	names := make(map[string]bool)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				names[fn.Name.Name] = true
			}
		}
	}
	return names
}
