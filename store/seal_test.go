// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/storetest"
)

// testSealer is an AES-GCM sealer under a key derived from seed.
func testSealer(t *testing.T, seed byte) *store.AESGCMSealer {
	t.Helper()
	s, err := store.NewAESGCMSealer(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// randomSecret is n random bytes: a value no other byte of the database
// repeats by chance.
func randomSecret(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// openFileDB opens dir/matter.db in WAL mode with the schema applied.
func openFileDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(dir, "matter.db"), walMode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

// sealedSecrets is what writeSecrets stored: the fabric and every clear
// secret, by name.
type sealedSecrets struct {
	fabric  uint8
	secrets map[string][]byte
}

// writeSecrets stores one value of every secret column through s, then an
// UpdateNOC that commits and one that rolls back, so the identity's live
// key is "key 2" and keys 1 and 3 were each a prior or a live key once.
// The resumption record comes last: a rollback deletes the fabric's.
func writeSecrets(t *testing.T, s *store.Store) sealedSecrets {
	t.Helper()
	ctx := t.Context()
	sec := map[string][]byte{
		"key 1": randomSecret(32), "key 2": randomSecret(32), "key 3": randomSecret(32),
		"ipk": randomSecret(16), "epoch key 0": randomSecret(16), "epoch key 1": randomSecret(16),
		"resumption secret": randomSecret(32), "icd key": randomSecret(16), "door lock data": randomSecret(24),
		"door lock key":    []byte(hex.EncodeToString(randomSecret(16))),
		"wifi passphrase":  []byte(hex.EncodeToString(randomSecret(20))),
		"joint fabric key": []byte(hex.EncodeToString(randomSecret(16))),
	}
	idx, err := s.AddFabric(ctx, newFabricRecord(0, 7))
	if err != nil {
		t.Fatal(err)
	}
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	must("UpsertIdentity", s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: idx, NOC: []byte{1}, PrivateKey: sec["key 1"], IPK: sec["ipk"]}))
	must("UpsertGroupKeySet", s.UpsertGroupKeySet(ctx, store.GroupKeySet{
		FabricIndex: idx, GroupKeySetID: 1, EpochKey0: sec["epoch key 0"], EpochStart0: 1, EpochKey1: sec["epoch key 1"], EpochStart1: 2,
	}))
	must("ReplaceICDRegistrations", s.ReplaceICDRegistrations(ctx, idx, []store.ICDRegistration{{CheckInNodeID: 7, MonitoredSubject: 8, Key: sec["icd key"]}}))
	must("SaveDoorLockAuth", s.SaveDoorLockAuth(ctx, "lock", store.DoorLockAuth{Credentials: []store.DoorLockCredential{{Type: 1, Index: 1, Data: sec["door lock data"]}}}))
	must("SetSetting wifi", s.SetSetting(ctx, "netcomm.wifi.networks", `[{"ssid":"home","credentials":"`+string(sec["wifi passphrase"])+`"}]`))
	must("SetSetting joint fabric", s.SetSetting(ctx, "jointfabric.datastore:anchor", `{"groupKeySets":[{"epochKey0":"`+string(sec["joint fabric key"])+`"}]}`))
	must("SetSettings door lock key", s.SetSettings(ctx, map[string]string{store.SettingDoorLockCredentialKey: string(sec["door lock key"]), "plain": "v"}))

	must("UpdateFabricCredentials 2", s.UpdateFabricCredentials(ctx, idx, store.FabricCredentials{NOC: []byte{2}, PrivateKey: sec["key 2"], NodeID: 9}))
	must("CommitFabric", s.CommitFabric(ctx, idx))
	must("UpdateFabricCredentials 3", s.UpdateFabricCredentials(ctx, idx, store.FabricCredentials{NOC: []byte{3}, PrivateKey: sec["key 3"], NodeID: 10}))
	was, err := s.RollbackFabric(ctx, idx)
	if err != nil || was != store.FabricPendingUpdate {
		t.Fatalf("RollbackFabric = %v, %v", was, err)
	}
	// After the rollback, which deletes the fabric's resumption records.
	w := sealedSecrets{fabric: idx, secrets: sec}
	putResumption(t, s, w)
	return w
}

// putResumption stores the fabric's resumption record again, as a
// rollback deletes it.
func putResumption(t *testing.T, s *store.Store, w sealedSecrets) {
	t.Helper()
	if err := s.UpsertResumption(t.Context(), store.ResumptionRecord{
		FabricIndex: w.fabric, PeerNodeID: 5, ResumptionID: bytes.Repeat([]byte{0xab}, 16), SharedSecret: w.secrets["resumption secret"],
	}); err != nil {
		t.Fatalf("UpsertResumption: %v", err)
	}
}

// checkSecretsRead reads every value writeSecrets stored back through s.
func checkSecretsRead(t *testing.T, s *store.Store, w sealedSecrets) {
	t.Helper()
	ctx := t.Context()
	sec := w.secrets
	id, err := s.GetIdentity(ctx, w.fabric)
	if err != nil || !bytes.Equal(id.PrivateKey, sec["key 2"]) || !bytes.Equal(id.IPK, sec["ipk"]) || !bytes.Equal(id.NOC, []byte{2}) {
		t.Errorf("GetIdentity = %x / %x / %x, %v; want key 2, the IPK and NOC 2", id.PrivateKey, id.IPK, id.NOC, err)
	}
	ks, err := s.GetGroupKeySet(ctx, w.fabric, 1)
	if err != nil || !bytes.Equal(ks.EpochKey0, sec["epoch key 0"]) || !bytes.Equal(ks.EpochKey1, sec["epoch key 1"]) || ks.EpochKey2 != nil {
		t.Errorf("GetGroupKeySet = %+v, %v", ks, err)
	}
	if list, err := s.ListGroupKeySets(ctx, w.fabric); err != nil || len(list) != 1 || !bytes.Equal(list[0].EpochKey0, sec["epoch key 0"]) {
		t.Errorf("ListGroupKeySets = %+v, %v", list, err)
	}
	r, err := s.GetResumptionByID(ctx, bytes.Repeat([]byte{0xab}, 16))
	if err != nil || !bytes.Equal(r.SharedSecret, sec["resumption secret"]) {
		t.Errorf("GetResumptionByID = %x, %v", r.SharedSecret, err)
	}
	if r, err := s.GetResumptionByPeer(ctx, w.fabric, 5); err != nil || !bytes.Equal(r.SharedSecret, sec["resumption secret"]) {
		t.Errorf("GetResumptionByPeer = %x, %v", r.SharedSecret, err)
	}
	if regs, err := s.ListICDRegistrations(ctx); err != nil || len(regs) != 1 || !bytes.Equal(regs[0].Key, sec["icd key"]) {
		t.Errorf("ListICDRegistrations = %+v, %v", regs, err)
	}
	if auth, _, err := s.LoadDoorLockAuth(ctx, "lock"); err != nil || len(auth.Credentials) != 1 || !bytes.Equal(auth.Credentials[0].Data, sec["door lock data"]) {
		t.Errorf("LoadDoorLockAuth = %+v, %v", auth, err)
	}
	if v, ok, err := s.GetSetting(ctx, "netcomm.wifi.networks"); err != nil || !ok || !strings.Contains(v, string(sec["wifi passphrase"])) {
		t.Errorf("GetSetting wifi = %q, %v, %v", v, ok, err)
	}
	if v, ok, err := s.GetSetting(ctx, store.SettingDoorLockCredentialKey); err != nil || !ok || v != string(sec["door lock key"]) {
		t.Errorf("GetSetting door lock key = %q, %v, %v", v, ok, err)
	}
	if v, ok, err := s.GetSetting(ctx, "plain"); err != nil || !ok || v != "v" {
		t.Errorf("GetSetting plain = %q, %v, %v", v, ok, err)
	}
	if v, ok, err := s.GetSetting(ctx, "jointfabric.datastore:anchor"); err != nil || !ok || !strings.Contains(v, string(sec["joint fabric key"])) {
		t.Errorf("GetSetting joint fabric = %q, %v, %v", v, ok, err)
	}
}

// clearSecretsIn names each secret whose clear bytes one of dir's files
// holds.
func clearSecretsIn(t *testing.T, dir string, sec map[string][]byte) []string {
	t.Helper()
	var hits []string
	for name, v := range sec {
		if files := filesHolding(t, dir, string(v)); len(files) > 0 {
			hits = append(hits, name+" in "+strings.Join(files, ","))
		}
	}
	return hits
}

// TestSealedStoreLeavesNoClearSecret (R2-P5-port-17, N-store-2): under a
// sealer, after writes of every secret column, an UpdateNOC that commits
// and one that rolls back, no secret's clear bytes are in the database
// file or its -wal — the prior keys included — while the handle is open
// and after Close; and every value reads back.
func TestSealedStoreLeavesNoClearSecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db := openFileDB(t, dir)
	s, err := store.NewSealed(t.Context(), db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	w := writeSecrets(t, s)
	checkSecretsRead(t, s, w)
	if _, ok, err := s.GetSetting(t.Context(), store.SettingICDKey); err != nil || ok {
		t.Errorf("a sealed store created the in-database ICD key: %v, %v", ok, err)
	}
	if hits := clearSecretsIn(t, dir, w.secrets); len(hits) > 0 {
		t.Errorf("clear secrets on disk, handle open: %v", hits)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if hits := clearSecretsIn(t, dir, w.secrets); len(hits) > 0 {
		t.Errorf("clear secrets on disk after Close: %v", hits)
	}
}

// TestUnsealedStoreKeepsSecretsInClear is the red side of the test above:
// without a sealer the keys are on disk, as the store's documentation
// says (the defect R2-P5-port-17 names).
func TestUnsealedStoreKeepsSecretsInClear(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := writeSecrets(t, store.New(openFileDB(t, dir)))
	hits := clearSecretsIn(t, dir, w.secrets)
	for _, want := range []string{"key 1", "key 2", "key 3", "ipk", "resumption secret", "wifi passphrase"} {
		if !slicesContainPrefix(hits, want+" in ") {
			t.Errorf("without a sealer %q is not on disk (hits %v): the test no longer sees the clear form", want, hits)
		}
	}
}

func slicesContainPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// TestSealedStoreWrongKeyOrNoSealer: a sealed value never reads as the
// secret — under another key every reader fails, and so does a Store
// without a sealer.
func TestSealedStoreWrongKeyOrNoSealer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	w := writeSecrets(t, s)
	if _, err := store.NewSealed(ctx, db, testSealer(t, 2)); !errors.Is(err, store.ErrUnseal) {
		t.Fatalf("NewSealed under another key on a sealed database: %v, want ErrUnseal", err)
	}
	// Each read under another key, past NewSealed's check.
	other := store.SealedWithoutCheck(db, testSealer(t, 2))
	for name, c := range map[string]struct {
		st   *store.Store
		want error
	}{"another key": {other, store.ErrUnseal}, "no sealer": {store.New(db), store.ErrNoSealer}} {
		st := c.st
		if id, err := st.GetIdentity(ctx, w.fabric); !errors.Is(err, c.want) || id.PrivateKey != nil {
			t.Errorf("%s: GetIdentity = %x, %v; want %v and no key", name, id.PrivateKey, err, c.want)
		}
		if _, err := st.GetGroupKeySet(ctx, w.fabric, 1); err == nil {
			t.Errorf("%s: GetGroupKeySet opened", name)
		}
		if _, err := st.ListGroupKeySets(ctx, w.fabric); err == nil {
			t.Errorf("%s: ListGroupKeySets opened", name)
		}
		if _, err := st.GetResumptionByPeer(ctx, w.fabric, 5); err == nil {
			t.Errorf("%s: GetResumptionByPeer opened", name)
		}
		if _, err := st.ListICDRegistrations(ctx); err == nil {
			t.Errorf("%s: ListICDRegistrations opened", name)
		}
		if _, _, err := st.LoadDoorLockAuth(ctx, "lock"); err == nil {
			t.Errorf("%s: LoadDoorLockAuth opened", name)
		}
		if v, _, err := st.GetSetting(ctx, "netcomm.wifi.networks"); err == nil {
			t.Errorf("%s: GetSetting wifi = %q", name, v)
		}
		if v, ok, err := st.GetSetting(ctx, "plain"); err != nil || !ok || v != "v" {
			t.Errorf("%s: a setting that is no secret: %q, %v, %v", name, v, ok, err)
		}
	}
	// A Store without the sealer refuses the writes that would leave a
	// sealed identity half clear.
	unsealed := store.New(db)
	if err := unsealed.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: w.fabric, NOC: []byte{4}, PrivateKey: []byte{4}, IPK: []byte{4}}); err == nil {
		t.Error("UpsertIdentity without the sealer over a sealed identity succeeded")
	}
	if err := unsealed.UpdateFabricCredentials(ctx, w.fabric, store.FabricCredentials{NOC: []byte{4}, PrivateKey: []byte{4}, NodeID: 4}); err == nil {
		t.Error("UpdateFabricCredentials without the sealer over a sealed identity succeeded")
	}
	if err := s.UpdateFabricCredentials(ctx, w.fabric, store.FabricCredentials{NOC: []byte{5}, PrivateKey: randomSecret(32), NodeID: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := unsealed.RollbackFabric(ctx, w.fabric); err == nil {
		t.Error("RollbackFabric without the sealer of a sealed pending update succeeded")
	}
	checkPending := func() store.FabricPending {
		f, err := s.GetFabric(ctx, w.fabric)
		if err != nil {
			t.Fatal(err)
		}
		return f.Pending
	}
	if p := checkPending(); p != store.FabricPendingUpdate {
		t.Errorf("a refused rollback changed the fabric: pending %v", p)
	}
	if was, err := s.RollbackFabric(ctx, w.fabric); err != nil || was != store.FabricPendingUpdate {
		t.Fatalf("sealed RollbackFabric = %v, %v", was, err)
	}
	putResumption(t, s, w)
	checkSecretsRead(t, s, w)
}

// TestSealedValueMovedDoesNotOpen: the aad binds table, column and row
// key, so a sealed value copied to another row or column, or to another
// setting, does not open.
func TestSealedValueMovedDoesNotOpen(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	var idx [2]uint8
	for i := range idx {
		if idx[i], err = s.AddFabric(ctx, newFabricRecord(0, byte(10+i))); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: idx[i], NOC: []byte{1}, PrivateKey: randomSecret(32), IPK: randomSecret(16)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_node_identities
SET private_key = (SELECT private_key FROM matter_node_identities WHERE fabric_index = ?) WHERE fabric_index = ?`, idx[1], idx[0]); err != nil {
		t.Fatal(err)
	}
	if id, err := s.GetIdentity(ctx, idx[0]); err == nil {
		t.Errorf("a key moved to another row opened: %x", id.PrivateKey)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_node_identities SET ipk = private_key WHERE fabric_index = ?`, idx[1]); err != nil {
		t.Fatal(err)
	}
	if id, err := s.GetIdentity(ctx, idx[1]); err == nil {
		t.Errorf("a key moved to another column opened: %x", id.IPK)
	}
	if err := s.SetSetting(ctx, "netcomm.wifi.networks", "[]"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_settings (key, value, sealed)
SELECT ?, value, sealed FROM matter_settings WHERE key = 'netcomm.wifi.networks'`, store.SettingDoorLockCredentialKey); err != nil {
		t.Fatal(err)
	}
	if v, _, err := s.GetSetting(ctx, store.SettingDoorLockCredentialKey); err == nil {
		t.Errorf("a sealed setting moved to another key opened: %q", v)
	}
}

// TestNewSealedSealsAClearDatabase: a database a Store without a sealer
// wrote — the ICD keys under the in-database key, a pending update's prior
// key, the secret settings — is sealed whole by NewSealed: no clear row is
// left, every value reads back, the clear bytes are gone from the files,
// and a second NewSealed writes nothing.
func TestNewSealedSealsAClearDatabase(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := t.TempDir()
	db := openFileDB(t, dir)
	unsealed := store.New(db)
	w := writeSecrets(t, unsealed)
	// A pending update, so a prior key is stored in clear too.
	w.secrets["key 4"] = randomSecret(32)
	if err := unsealed.UpdateFabricCredentials(ctx, w.fabric, store.FabricCredentials{NOC: []byte{4}, PrivateKey: w.secrets["key 4"], NodeID: 11}); err != nil {
		t.Fatal(err)
	}
	icdKey, ok, err := unsealed.GetSetting(ctx, store.SettingICDKey)
	if err != nil || !ok {
		t.Fatalf("the clear store's ICD key: %v, %v", ok, err)
	}
	w.secrets["in-database icd key"] = []byte(icdKey)

	s, err := store.NewSealed(ctx, db, testSealer(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"matter_node_identities", "matter_group_keys", "matter_resumption", "matter_doorlock_credentials", "matter_icd_registrations"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE sealed = 0`).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d clear rows after NewSealed, %v", table, n, err)
		}
	}
	var settings int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matter_settings WHERE sealed = 1`).Scan(&settings); err != nil || settings != 5 {
		t.Errorf("sealed settings = %d, %v; want the Wi-Fi networks, the DoorLock key, the ICD key, the joint fabric record and the seal canary", settings, err)
	}
	// The pending update reads back and rolls back under the sealer.
	if id, err := s.GetIdentity(ctx, w.fabric); err != nil || !bytes.Equal(id.PrivateKey, w.secrets["key 4"]) {
		t.Errorf("GetIdentity of the pending update = %x, %v", id.PrivateKey, err)
	}
	if was, err := s.RollbackFabric(ctx, w.fabric); err != nil || was != store.FabricPendingUpdate {
		t.Fatalf("RollbackFabric = %v, %v", was, err)
	}
	putResumption(t, s, w)
	checkSecretsRead(t, s, w)
	if hits := clearSecretsIn(t, dir, w.secrets); len(hits) > 0 {
		t.Errorf("clear secrets on disk after NewSealed: %v", hits)
	}
	if _, owed, err := s.GetMetadataCounter(ctx, "store.seal_scrub_owed"); err != nil || owed {
		t.Errorf("the scrub is still owed: %v, %v", owed, err)
	}

	var before []byte
	if err := db.QueryRowContext(ctx, `SELECT private_key FROM matter_node_identities WHERE fabric_index = ?`, w.fabric).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 3)); err != nil {
		t.Fatal(err)
	}
	var after []byte
	if err := db.QueryRowContext(ctx, `SELECT private_key FROM matter_node_identities WHERE fabric_index = ?`, w.fabric).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a second NewSealed sealed a sealed key again")
	}

	// The database carries the seal canary now: a Store without the sealer
	// writes no clear row into it (decision 130), a clear row planted in
	// the file is refused by the sealed store and by the next NewSealed,
	// which does not adopt it.
	added := randomSecret(16)
	if err := unsealed.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: w.fabric, GroupKeySetID: 2, EpochKey0: added, EpochStart0: 1}); !errors.Is(err, store.ErrNoSealer) {
		t.Fatalf("a clear key set into the sealed database = %v, want ErrNoSealer", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_group_keys (fabric_index, group_key_set_id, security_policy, epoch_key_0, epoch_start_0) VALUES (?, 2, 0, ?, 1)`, w.fabric, added); err != nil {
		t.Fatal(err)
	}
	if ks, err := s.GetGroupKeySet(ctx, w.fabric, 2); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("a clear row under the sealer = %x, %v; want ErrUnseal", ks.EpochKey0, err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 3)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("the next NewSealed over the planted row = %v, want ErrUnseal", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matter_group_keys WHERE sealed = 0`).Scan(&n); err != nil || n != 1 {
		t.Errorf("clear key sets after the refused NewSealed: %d, %v; want the planted one, as it was", n, err)
	}
}

// TestNewSealedRefusesASchemaBeforeV10 and a nil sealer.
func TestNewSealedRefuses(t *testing.T) {
	t.Parallel()
	db := openEmptyDB(t)
	applyUp(t, db, "v9")
	if _, err := store.NewSealed(t.Context(), db, testSealer(t, 1)); err == nil || !strings.Contains(err.Error(), "v10") {
		t.Errorf("NewSealed on a v9 database = %v; want the schema named", err)
	}
	if _, err := store.NewSealed(t.Context(), openTestDB(t), nil); err == nil {
		t.Error("NewSealed with a nil sealer succeeded")
	}
}

// TestMigrationV10DownRefusesSealedRows: v10's Down refuses while a row is
// sealed — a v9 store would read the sealed bytes as the secret — and
// leaves the database as it was; on a clear database it reverts the step
// and keeps the rows.
func TestMigrationV10DownRefusesSealedRows(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	down := store.Migrations()[9]
	if down.Version != "v10" {
		t.Fatalf("step 10 is %s", down.Version)
	}

	db := openTestDB(t)
	s, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	w := writeSecrets(t, s)
	before := fingerprint(t, db)
	if _, err := db.ExecContext(ctx, down.Down); err == nil || !strings.Contains(err.Error(), "v10_down_refuses_sealed_rows") {
		t.Fatalf("v10 down on a sealed database = %v; want the guard's CHECK", err)
	}
	if after := fingerprint(t, db); after != before {
		t.Error("the refused v10 down changed the schema")
	}
	checkSecretsRead(t, s, w)

	plain := openTestDB(t)
	cw := writeSecrets(t, store.New(plain))
	if _, err := plain.ExecContext(ctx, down.Down); err != nil {
		t.Fatalf("v10 down on a clear database: %v", err)
	}
	var key []byte
	if err := plain.QueryRowContext(ctx, `SELECT private_key FROM matter_node_identities WHERE fabric_index = ?`, cw.fabric).Scan(&key); err != nil || !bytes.Equal(key, cw.secrets["key 2"]) {
		t.Errorf("the identity after v10 down = %x, %v", key, err)
	}
}

// TestAESGCMSealer: a sealed value opens under its key and aad only; the
// key must be 32 bytes.
func TestAESGCMSealer(t *testing.T) {
	t.Parallel()
	a, b := testSealer(t, 1), testSealer(t, 2)
	plain, aad := []byte("secret"), []byte("aad")
	sealed, err := a.Seal(plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) {
		t.Error("the sealed value holds the plain one")
	}
	if again, _ := a.Seal(plain, aad); bytes.Equal(again, sealed) {
		t.Error("two seals of one value are equal: the nonce is not random")
	}
	if got, err := a.Open(sealed, aad); err != nil || !bytes.Equal(got, plain) {
		t.Errorf("Open = %q, %v", got, err)
	}
	if _, err := a.Open(sealed, []byte("other")); err == nil {
		t.Error("Open under another aad succeeded")
	}
	if _, err := b.Open(sealed, aad); err == nil {
		t.Error("Open under another key succeeded")
	}
	if _, err := a.Open(sealed[:10], aad); err == nil {
		t.Error("Open of a truncated value succeeded")
	}
	if _, err := store.NewAESGCMSealer(make([]byte, 16)); err == nil {
		t.Error("NewAESGCMSealer accepted a 16-byte key")
	}
	if _, err := a.Seal(make([]byte, 1<<24), aad); err != nil {
		t.Errorf("Seal of a 16 MiB value: %v", err)
	}
	if _, err := a.Seal(make([]byte, 1<<24+1), aad); err == nil {
		t.Error("Seal took a value above its bound")
	}
}

// TestSealedStoreContract holds the sealed Store to the port's contract,
// the table New's Store and store/memstore run.
func TestSealedStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Port {
		t.Helper()
		s, err := store.NewSealed(context.Background(), openTestDB(t), testSealer(t, 9))
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

// TestSealedSettingsNameTheirSources: the setting keys the store seals by
// name are cluster/core's and cluster/jointfabric's unexported constants,
// copied; this holds the copies to their source.
func TestSealedSettingsNameTheirSources(t *testing.T) {
	t.Parallel()
	for file, decl := range map[string]string{
		"core/network_commissioning_wifi.go":      `const wifiNetworksKey = "netcomm.wifi.networks"`,
		"core/operational_credentials_pending.go": `const pendingNOCSettingKey = "operational_credentials.pending_noc"`,
		"jointfabric/datastore.go":                `const settingPrefix = "jointfabric.datastore:"`,
	} {
		b, err := os.ReadFile(filepath.Join("..", "cluster", filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte(decl)) {
			t.Errorf("cluster/%s no longer declares %s: update store's sealedSettings", file, decl)
		}
	}
	b, err := os.ReadFile(filepath.Join("..", "cluster", "core", "operational_credentials_pending.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`const pendingNOCUpdateSettingKey = "operational_credentials.pending_noc_update"`)) {
		t.Error("cluster/core no longer declares pendingNOCUpdateSettingKey as the store seals it")
	}
	b, err = os.ReadFile(filepath.Join("..", "controller", "ca", "persist.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range []string{`const DefaultSettingKey = "controller.ca"`, `func (s SettingStore) FabricKey() string { return s.key() + ".fabric" }`} {
		if !bytes.Contains(b, []byte(decl)) {
			t.Errorf("controller/ca no longer declares %s: update store's sealedSettings", decl)
		}
	}
	b, err = os.ReadFile(filepath.Join("..", "node", "node.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`settingRotatingIDUniqueID\s+= "commissioning\.rotating_device_id\.unique_id"`).Match(b) {
		t.Error("node no longer declares settingRotatingIDUniqueID as the store seals it")
	}
}

// failingSealer seals with an AES-GCM sealer until fail is set, then
// refuses every Seal and Open.
type failingSealer struct {
	inner *store.AESGCMSealer
	fail  bool
}

var errSealerDown = errors.New("sealer down")

func (f *failingSealer) Seal(plain, aad []byte) ([]byte, error) {
	if f.fail {
		return nil, errSealerDown
	}
	return f.inner.Seal(plain, aad)
}

func (f *failingSealer) Open(sealed, aad []byte) ([]byte, error) {
	if f.fail {
		return nil, errSealerDown
	}
	return f.inner.Open(sealed, aad)
}

// TestSealerErrorFailsTheWrite: a sealer that fails fails every write that
// seals, and the write changes nothing; NewSealed over a clear database
// with it leaves every row clear.
func TestSealerErrorFailsTheWrite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	f := &failingSealer{inner: testSealer(t, 4)}
	s, err := store.NewSealed(ctx, db, f)
	if err != nil {
		t.Fatal(err)
	}
	w := writeSecrets(t, s)
	if err := s.UpdateFabricCredentials(ctx, w.fabric, store.FabricCredentials{NOC: []byte{6}, PrivateKey: randomSecret(32), NodeID: 6}); err != nil {
		t.Fatal(err)
	}
	f.fail = true
	for name, err := range map[string]error{
		"UpsertIdentity":          s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: w.fabric, NOC: []byte{7}, PrivateKey: []byte{7}, IPK: randomSecret(16)}),
		"UpdateFabricCredentials": s.UpdateFabricCredentials(ctx, w.fabric, store.FabricCredentials{NOC: []byte{7}, PrivateKey: []byte{7}, NodeID: 7}),
		"UpsertGroupKeySet":       s.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: w.fabric, GroupKeySetID: 3, EpochKey0: randomSecret(16)}),
		"UpsertResumption": s.UpsertResumption(ctx, store.ResumptionRecord{
			FabricIndex: w.fabric, PeerNodeID: 9, ResumptionID: bytes.Repeat([]byte{1}, 16), SharedSecret: bytes.Repeat([]byte{1}, 32),
		}),
		"ReplaceICDRegistrations": s.ReplaceICDRegistrations(ctx, w.fabric, []store.ICDRegistration{{CheckInNodeID: 1, MonitoredSubject: 1, Key: []byte{7}}}),
		"SaveDoorLockAuth":        s.SaveDoorLockAuth(ctx, "lock", store.DoorLockAuth{Credentials: []store.DoorLockCredential{{Type: 1, Index: 1, Data: []byte{7}}}}),
		"SetSetting":              s.SetSetting(ctx, "netcomm.wifi.networks", "[]"),
		"SetSettings":             s.SetSettings(ctx, map[string]string{store.SettingDoorLockCredentialKey: "00"}),
	} {
		if !errors.Is(err, errSealerDown) {
			t.Errorf("%s with a failing sealer = %v, want its error", name, err)
		}
	}
	if _, err := s.RollbackFabric(ctx, w.fabric); !errors.Is(err, errSealerDown) {
		t.Errorf("RollbackFabric with a failing sealer = %v, want its error", err)
	}
	f.fail = false
	if was, err := s.RollbackFabric(ctx, w.fabric); err != nil || was != store.FabricPendingUpdate {
		t.Fatalf("RollbackFabric = %v, %v", was, err)
	}
	putResumption(t, s, w)
	checkSecretsRead(t, s, w)

	clearDB := openTestDB(t)
	writeSecrets(t, store.New(clearDB))
	if _, err := store.NewSealed(ctx, clearDB, &failingSealer{fail: true}); !errors.Is(err, errSealerDown) {
		t.Errorf("NewSealed with a failing sealer = %v", err)
	}
	var sealed int
	if err := clearDB.QueryRowContext(ctx, `SELECT
    (SELECT COUNT(*) FROM matter_node_identities WHERE sealed = 1) +
    (SELECT COUNT(*) FROM matter_settings WHERE sealed = 1)`).Scan(&sealed); err != nil || sealed != 0 {
		t.Errorf("a failed NewSealed sealed %d rows, %v; want none", sealed, err)
	}
}

// TestSealedStoreRefusesClearRows (Fable F1): after NewSealed the store
// is strict — a secret row whose seal mark was flipped to 0, or a clear row
// planted or written by a store without the sealer, is ErrUnseal on every
// read and every write that reads the row; never the stored bytes.
func TestSealedStoreRefusesClearRows(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s := sealedOver(t, db)
	w := writeSecrets(t, s)
	flip := func(table string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE `+table+` SET sealed = 0`); err != nil {
			t.Fatal(err)
		}
	}
	flip("matter_node_identities")
	if id, err := s.GetIdentity(ctx, w.fabric); !errors.Is(err, store.ErrUnseal) || id.PrivateKey != nil {
		t.Errorf("GetIdentity of a flipped mark = %x, %v; want ErrUnseal and no key", id.PrivateKey, err)
	}
	if err := s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: w.fabric, NOC: []byte{8}, PrivateKey: randomSecret(32), IPK: randomSecret(16)}); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("UpsertIdentity over a flipped mark = %v; want ErrUnseal", err)
	}
	if err := s.UpdateFabricCredentials(ctx, w.fabric, store.FabricCredentials{NOC: []byte{8}, PrivateKey: randomSecret(32), NodeID: 8}); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("UpdateFabricCredentials over a flipped mark = %v; want ErrUnseal", err)
	}
	flip("matter_resumption")
	if r, err := s.GetResumptionByPeer(ctx, w.fabric, 5); !errors.Is(err, store.ErrUnseal) || r.SharedSecret != nil {
		t.Errorf("GetResumptionByPeer of a flipped mark = %x, %v; want ErrUnseal", r.SharedSecret, err)
	}
	flip("matter_group_keys")
	if _, err := s.ListGroupKeySets(ctx, w.fabric); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("ListGroupKeySets of a flipped mark = %v; want ErrUnseal", err)
	}
	flip("matter_icd_registrations")
	if _, err := s.ListICDRegistrations(ctx); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("ListICDRegistrations of a flipped mark = %v; want ErrUnseal", err)
	}
	flip("matter_doorlock_credentials")
	if _, _, err := s.LoadDoorLockAuth(ctx, "lock"); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("LoadDoorLockAuth of a flipped mark = %v; want ErrUnseal", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_settings SET sealed = 0 WHERE key = 'netcomm.wifi.networks'`); err != nil {
		t.Fatal(err)
	}
	if v, _, err := s.GetSetting(ctx, "netcomm.wifi.networks"); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("GetSetting of a flipped mark = %q, %v; want ErrUnseal", v, err)
	}

	// Planted clear rows: the seal canary deleted from the file (not tamper
	// protection, decision 130), a store without the sealer writes them.
	other := openTestDB(t)
	sealed := sealedOver(t, other)
	pw := writeSecrets(t, sealed)
	if _, err := other.ExecContext(ctx, `DELETE FROM matter_settings WHERE key = ?`, store.SettingSealCanary); err != nil {
		t.Fatal(err)
	}
	unsealed := store.New(other)
	for name, err := range map[string]error{
		"UpsertGroupKeySet": unsealed.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: pw.fabric, GroupKeySetID: 1, EpochKey0: randomSecret(16), EpochStart0: 1}),
		"SetSetting":        unsealed.SetSetting(ctx, store.SettingDoorLockCredentialKey, "00"),
		"UpsertResumption": unsealed.UpsertResumption(ctx, store.ResumptionRecord{
			FabricIndex: pw.fabric, PeerNodeID: 5, ResumptionID: bytes.Repeat([]byte{0xab}, 16), SharedSecret: randomSecret(32),
		}),
	} {
		if err != nil {
			t.Fatalf("plant %s: %v", name, err)
		}
	}
	if _, err := sealed.GetGroupKeySet(ctx, pw.fabric, 1); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("a planted key set = %v; want ErrUnseal", err)
	}
	if _, _, err := sealed.GetSetting(ctx, store.SettingDoorLockCredentialKey); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("a planted secret setting = %v; want ErrUnseal", err)
	}
	if _, err := sealed.GetResumptionByID(ctx, bytes.Repeat([]byte{0xab}, 16)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("a planted resumption record = %v; want ErrUnseal", err)
	}
	if v, ok, err := sealed.GetSetting(ctx, "plain"); err != nil || !ok || v != "v" {
		t.Errorf("a setting that is no secret under the sealer = %q, %v, %v", v, ok, err)
	}
	// A planted clear identity row with a pending update is refused by the
	// rollback too.
	if _, err := other.ExecContext(ctx, `UPDATE matter_node_identities SET sealed = 0`); err != nil {
		t.Fatal(err)
	}
	if err := unsealed.UpdateFabricCredentials(ctx, pw.fabric, store.FabricCredentials{NOC: []byte{10}, PrivateKey: randomSecret(32), NodeID: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := sealed.RollbackFabric(ctx, pw.fabric); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("RollbackFabric of a clear row under the sealer = %v; want ErrUnseal", err)
	}
}

// TestReadSideLengthChecks: a secret of the wrong length is refused on
// read, with or without a sealer — the lengths the write side enforces
// (the resumption secret here, the IPK by AddNOC, the epoch keys by
// KeySetWrite).
func TestReadSideLengthChecks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s := store.New(db)
	w := writeSecrets(t, s)
	for _, step := range []struct {
		name, sql string
		read      func() error
	}{
		{"shared_secret", `UPDATE matter_resumption SET shared_secret = X'01'`, func() error {
			_, err := s.GetResumptionByPeer(ctx, w.fabric, 5)
			return err
		}},
		{"ipk", `UPDATE matter_node_identities SET ipk = X'01'`, func() error {
			_, err := s.GetIdentity(ctx, w.fabric)
			return err
		}},
		{"private_key", `UPDATE matter_node_identities SET ipk = X'000102030405060708090a0b0c0d0e0f', private_key = X''`, func() error {
			_, err := s.GetIdentity(ctx, w.fabric)
			return err
		}},
		{"epoch_key_1", `UPDATE matter_group_keys SET epoch_key_1 = X'0102'`, func() error {
			_, err := s.GetGroupKeySet(ctx, w.fabric, 1)
			return err
		}},
	} {
		if _, err := db.ExecContext(ctx, step.sql); err != nil {
			t.Fatal(err)
		}
		if err := step.read(); err == nil || !strings.Contains(err.Error(), step.name) {
			t.Errorf("%s of the wrong length read = %v; want an error naming it", step.name, err)
		}
	}
}

// TestNewSealedRefusesACorruptKeyColumn (Fable F8): a row whose key column
// holds a value of no key type fails NewSealed with an error, not a panic.
func TestNewSealedRefusesACorruptKeyColumn(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	w := writeSecrets(t, store.New(db))
	if _, err := db.ExecContext(ctx, `UPDATE matter_resumption SET peer_node_id = 1.5 WHERE fabric_index = ?`, w.fabric); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 1)); err == nil || !strings.Contains(err.Error(), "key part") {
		t.Errorf("NewSealed over a REAL peer_node_id = %v; want the aad's error", err)
	}
}

// sealedOver is a sealed store over db, which has nothing to seal yet.
func sealedOver(t *testing.T, db *sql.DB) *store.Store {
	t.Helper()
	s, err := store.NewSealed(t.Context(), db, testSealer(t, 5))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestNewSealedLeavesNoClearSecretPerJournalMode (Fable F4, sec Q2): the
// sealing upgrade's and Reseal's residue claims in the journal modes:
// WAL (TestNewSealedSealsAClearDatabase too), the rollback journal's DELETE
// (SQLite's default), TRUNCATE, PERSIST, MEMORY and OFF forms, and under
// locking_mode(EXCLUSIVE), which keeps a rollback -journal between
// transactions as PERSIST does, the default, PERSIST, TRUNCATE, WAL and
// MEMORY forms: after NewSealed over a clear database no clear secret, and
// after Reseal no value sealed under the old key, is in any file of the
// directory, the handle open and after Close. An EXCLUSIVE handle is one connection: the lock it holds keeps
// every other connection of a pool out.
func TestNewSealedLeavesNoClearSecretPerJournalMode(t *testing.T) {
	t.Parallel()
	for _, mode := range [][]string{
		nil,
		{"journal_mode(DELETE)"},
		{"journal_mode(TRUNCATE)"},
		{"journal_mode(PERSIST)"},
		{walMode},
		{"journal_mode(MEMORY)"},
		{"journal_mode(OFF)"},
		{"locking_mode(EXCLUSIVE)"},
		{"locking_mode(EXCLUSIVE)", "journal_mode(PERSIST)"},
		{"locking_mode(EXCLUSIVE)", "journal_mode(TRUNCATE)"},
		{"locking_mode(EXCLUSIVE)", walMode},
		{"locking_mode(EXCLUSIVE)", "journal_mode(MEMORY)"},
	} {
		name := strings.Join(mode, ",")
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			dir := t.TempDir()
			db, err := store.OpenSQLite(filepath.Join(dir, "matter.db"), mode...)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			if slices.Contains(mode, "locking_mode(EXCLUSIVE)") {
				db.SetMaxOpenConns(1)
			}
			if err := store.Apply(ctx, db); err != nil {
				t.Fatal(err)
			}
			w := writeSecrets(t, store.New(db))
			icdKey, _, err := store.New(db).GetSetting(ctx, store.SettingICDKey)
			if err != nil {
				t.Fatal(err)
			}
			w.secrets["in-database icd key"] = []byte(icdKey)
			if hits := clearSecretsIn(t, dir, w.secrets); len(hits) == 0 {
				t.Fatal("before NewSealed no clear secret is on disk: the test does not see the clear form")
			}
			s, err := store.NewSealed(ctx, db, testSealer(t, 6))
			if err != nil {
				t.Fatal(err)
			}
			checkSecretsRead(t, s, w)
			if hits := clearSecretsIn(t, dir, w.secrets); len(hits) > 0 {
				t.Errorf("clear secrets on disk after NewSealed, handle open: %v", hits)
			}
			// Reseal in the same mode: no value sealed under the old key is
			// left in any file (decision 131).
			var oldKey, oldSecret []byte
			if err := db.QueryRowContext(ctx, `SELECT private_key FROM matter_node_identities`).Scan(&oldKey); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `SELECT shared_secret FROM matter_resumption`).Scan(&oldSecret); err != nil {
				t.Fatal(err)
			}
			if err := store.Reseal(ctx, db, testSealer(t, 6), testSealer(t, 7)); err != nil {
				t.Fatal(err)
			}
			old := map[string][]byte{"old sealed key": oldKey, "old sealed resumption": oldSecret}
			if hits := clearSecretsIn(t, dir, old); len(hits) > 0 {
				t.Errorf("old-key values on disk after Reseal, handle open: %v", hits)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if hits := clearSecretsIn(t, dir, w.secrets); len(hits) > 0 {
				t.Errorf("clear secrets on disk after NewSealed and Close: %v", hits)
			}
			if hits := clearSecretsIn(t, dir, old); len(hits) > 0 {
				t.Errorf("old-key values on disk after Reseal and Close: %v", hits)
			}
		})
	}
}

// TestListGroupKeySetsLeavesAMalformedRowOut (Fable R2-1): a key set whose
// stored epoch key has a length a write refuses — an earlier release's
// row, or an edit of the file — does not take the fabric's other key sets
// down: ListGroupKeySets lists the well-formed ones, the IPK's set 0 among
// them, and GetGroupKeySet of the bad row returns the error.
func TestListGroupKeySetsLeavesAMalformedRowOut(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s := store.New(db)
	idx, err := s.AddFabric(ctx, newFabricRecord(0, 31))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint16{0, 1, 2} {
		if err := s.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: idx, GroupKeySetID: id, EpochKey0: randomSecret(16), EpochStart0: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_group_keys SET epoch_key_1 = X'0102', epoch_start_1 = 2 WHERE group_key_set_id = 1`); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListGroupKeySets(ctx, idx)
	if err != nil || len(list) != 2 || list[0].GroupKeySetID != 0 || list[1].GroupKeySetID != 2 {
		t.Errorf("ListGroupKeySets with a malformed set 1 = %+v, %v; want sets 0 and 2", list, err)
	}
	if _, err := s.GetGroupKeySet(ctx, idx, 1); err == nil || !strings.Contains(err.Error(), "epoch_key_1") {
		t.Errorf("GetGroupKeySet of the malformed set = %v; want its length error", err)
	}
}

// TestNewSealedClassifiesAClearICDRowUnderASealedKey (Fable R2-3): an ICD
// row whose mark was flipped while the in-database ICD key is sealed fails
// NewSealed with ErrUnseal — an edited file — not ErrNoSealer, which would
// tell a host with the sealer to go and find one.
func TestNewSealedClassifiesAClearICDRowUnderASealedKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	writeSecrets(t, store.New(db)) // the ICD key is created as a clear setting
	if _, err := store.NewSealed(ctx, db, testSealer(t, 7)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_icd_registrations SET sealed = 0`); err != nil {
		t.Fatal(err)
	}
	_, err := store.NewSealed(ctx, db, testSealer(t, 7))
	if !errors.Is(err, store.ErrUnseal) || errors.Is(err, store.ErrNoSealer) {
		t.Errorf("NewSealed over a flipped ICD row = %v; want ErrUnseal only", err)
	}
}

// TestNewSealedChecksTheKey (seal round 3): NewSealed opens one sealed
// value before it seals anything, so a sealer under another key fails it
// with ErrUnseal whichever sealed value the database holds — the first
// identity, another sealed row, a sealed setting — and leaves a clear row
// it would have sealed as it was. A database with no sealed value opens
// under any key.
func TestNewSealedChecksTheKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	if _, err := store.NewSealed(ctx, openTestDB(t), testSealer(t, 2)); err != nil {
		t.Errorf("NewSealed on an empty database: %v", err)
	}
	for name, write := range map[string]func(*store.Store) error{
		"identity": func(s *store.Store) error {
			idx, err := s.AddFabric(ctx, newFabricRecord(0, 7))
			if err != nil {
				return err
			}
			return s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: idx, NOC: []byte{1}, PrivateKey: randomSecret(32), IPK: randomSecret(16)})
		},
		"group key set": func(s *store.Store) error {
			idx, err := s.AddFabric(ctx, newFabricRecord(0, 7))
			if err != nil {
				return err
			}
			return s.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: idx, GroupKeySetID: 1, EpochKey0: randomSecret(16), EpochStart0: 1})
		},
		"door lock credential": func(s *store.Store) error {
			return s.SaveDoorLockAuth(ctx, "lock", store.DoorLockAuth{Credentials: []store.DoorLockCredential{{Type: 1, Index: 1, Data: randomSecret(24)}}})
		},
		"setting": func(s *store.Store) error { return s.SetSetting(ctx, "netcomm.wifi.networks", "[]") },
	} {
		db := openTestDB(t)
		s, err := store.NewSealed(ctx, db, testSealer(t, 1))
		if err != nil {
			t.Fatal(err)
		}
		if err := write(s); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := store.NewSealed(ctx, db, testSealer(t, 2)); !errors.Is(err, store.ErrUnseal) {
			t.Errorf("%s: NewSealed under another key = %v, want ErrUnseal", name, err)
		}
		if _, err := store.NewSealed(ctx, db, testSealer(t, 1)); err != nil {
			t.Errorf("%s: NewSealed under the key = %v", name, err)
		}
	}

	// A clear secret beside a sealed one: the wrong key seals nothing.
	db := openTestDB(t)
	s, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, store.SettingICDKey, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_settings (key, value, sealed) VALUES ('netcomm.wifi.networks', '[]', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 2)); !errors.Is(err, store.ErrUnseal) {
		t.Fatalf("NewSealed under another key = %v, want ErrUnseal", err)
	}
	if v, ok, err := store.New(db).GetSetting(ctx, "netcomm.wifi.networks"); err != nil || !ok || v != "[]" {
		t.Errorf("the clear row after a refused NewSealed: %q %v %v; want it clear and unchanged", v, ok, err)
	}
}
