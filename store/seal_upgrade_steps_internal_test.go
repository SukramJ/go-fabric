// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

// sealBaselineVersion is the schema step whose secrets [sealedAtBaseline]
// lists: the lists as they stood at v11.
const sealBaselineVersion = "v11"

// sealedAtBaseline is every secret [NewSealed] seals at
// [sealBaselineVersion], in the form [secretEntries] names them. These
// need no upgrade step: the seal canary came in with them as secrets, at
// schema v11, so a database the canary marks never holds them in clear —
// they are exempt by being in v11, nothing else. The Rotating Device
// Identifier unique id ("commissioning.rotating_device_id.unique_id") is
// one of them.
var sealedAtBaseline = []string{
	"setting commissioning.rotating_device_id.unique_id",
	"setting controller.ca",
	"setting controller.ca.fabric",
	"setting door_lock.credential_key",
	"setting icd_management.key",
	"setting netcomm.wifi.networks",
	"setting operational_credentials.pending_noc",
	"setting operational_credentials.pending_noc_update",
	"setting-prefix jointfabric.datastore:",
	"table matter_doorlock_credentials aad(endpoint_key,credential_type,credential_index) form blob",
	"column matter_doorlock_credentials.credential_data",
	"table matter_group_keys aad(fabric_index,group_key_set_id) form blob",
	"column matter_group_keys.epoch_key_0",
	"column matter_group_keys.epoch_key_1",
	"column matter_group_keys.epoch_key_2",
	"table matter_icd_registrations aad(fabric_index,check_in_node_id,monitored_subject) form icd",
	"column matter_icd_registrations.icd_key",
	"table matter_node_identities aad(fabric_index) form blob",
	"column matter_node_identities.private_key",
	"column matter_node_identities.ipk",
	"column matter_node_identities.prior_private_key",
	"table matter_resumption aad(fabric_index,peer_node_id) form blob",
	"column matter_resumption.shared_secret",
	"table matter_settings aad(key) form setting-text",
	"column matter_settings.value",
}

// sealUpgradeSteps names, for every secret added after
// [sealBaselineVersion], the test that proves its upgrade step: a marked
// database that holds the value in clear — written by a sealed store while
// it was not yet a secret — opens under [NewSealed], the value sealed.
// Empty: no secret has been added since.
var sealUpgradeSteps = map[string]string{}

// secretEntries names every secret the store seals, one entry per
// sealedSettings key, sealedSettingPrefixes prefix, secretTables table
// (its aad key columns and its form: either changing is a new secret to a
// sealed row) and secret column.
func secretEntries() []string {
	forms := map[sealForm]string{sealBlob: "blob", sealICD: "icd", sealSettingText: "setting-text"}
	var out []string
	for k := range sealedSettings {
		out = append(out, "setting "+k)
	}
	for _, p := range sealedSettingPrefixes {
		out = append(out, "setting-prefix "+p)
	}
	for _, t := range secretTables {
		form, ok := forms[t.form]
		if !ok {
			form = "unnamed"
		}
		out = append(out, "table "+t.table+" aad("+strings.Join(t.keys, ",")+") form "+form)
		for _, c := range t.secrets {
			out = append(out, "column "+t.table+"."+c)
		}
	}
	return out
}

// TestSecretsAddedAfterV11ShipAnUpgradeStep (decision 157) holds the class
// "a value becomes a secret in a database the canary already marks": in a
// marked database [NewSealed] refuses a clear row of a secret table and a
// clear setting [isSealedSetting] names (openMarked, clearSecretTable), so
// a release that adds a key to sealedSettings, a prefix to
// sealedSettingPrefixes, a table or column to secretTables, or changes a
// table's aad columns or form, meets databases that hold the value in
// clear, or sealed under another aad, and they stop opening. Every such
// entry ships with an upgrade step that seals it there first, and with
// the test that proves the step, named in sealUpgradeSteps; the v11
// baseline is exempt by being in v11.
func TestSecretsAddedAfterV11ShipAnUpgradeStep(t *testing.T) {
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
			t.Errorf("%q is a secret added after %s without an upgrade step: a database the seal canary marks may hold it in clear "+
				"and NewSealed then refuses to open it. Add the step that seals it there, and name the test that proves it in sealUpgradeSteps", e, sealBaselineVersion)
			continue
		}
		if !tests[step] {
			t.Errorf("sealUpgradeSteps names %s for %q, but the package has no such test", step, e)
		}
	}
	for _, e := range sealedAtBaseline {
		if !slices.Contains(current, e) {
			t.Errorf("the %s baseline lists %q, which the store no longer seals in this form: a marked database holds it sealed so, and dropping or changing it needs its own step; update the baseline with that step", sealBaselineVersion, e)
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
