// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
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
		"door lock key":   []byte(hex.EncodeToString(randomSecret(16))),
		"wifi passphrase": []byte(hex.EncodeToString(randomSecret(20))),
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
	other, err := store.NewSealed(ctx, db, testSealer(t, 2))
	if err != nil {
		t.Fatalf("NewSealed under another key on a sealed database: %v", err)
	}
	for name, st := range map[string]*store.Store{"another key": other, "no sealer": store.New(db)} {
		if id, err := st.GetIdentity(ctx, w.fabric); err == nil || id.PrivateKey != nil {
			t.Errorf("%s: GetIdentity = %x, %v; want an error and no key", name, id.PrivateKey, err)
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
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matter_settings WHERE sealed = 1`).Scan(&settings); err != nil || settings != 3 {
		t.Errorf("sealed settings = %d, %v; want the Wi-Fi networks, the DoorLock key and the ICD key", settings, err)
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

	// A clear row a Store without the sealer adds is read as it is and
	// sealed by the next NewSealed.
	added := randomSecret(16)
	if err := unsealed.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: w.fabric, GroupKeySetID: 2, EpochKey0: added, EpochStart0: 1}); err != nil {
		t.Fatal(err)
	}
	if ks, err := s.GetGroupKeySet(ctx, w.fabric, 2); err != nil || !bytes.Equal(ks.EpochKey0, added) {
		t.Errorf("a clear row under the sealer = %x, %v", ks.EpochKey0, err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 3)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matter_group_keys WHERE sealed = 0`).Scan(&n); err != nil || n != 0 {
		t.Errorf("clear key sets after the next NewSealed: %d, %v", n, err)
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

// TestSealedSettingsNameClusterCoreKeys: the setting keys the store seals
// by name are cluster/core's unexported constants, copied; this holds the
// copies to their source.
func TestSealedSettingsNameClusterCoreKeys(t *testing.T) {
	t.Parallel()
	for file, decl := range map[string]string{
		"network_commissioning_wifi.go":      `const wifiNetworksKey = "netcomm.wifi.networks"`,
		"operational_credentials_pending.go": `const pendingNOCSettingKey = "operational_credentials.pending_noc"`,
	} {
		b, err := os.ReadFile(filepath.Join("..", "cluster", "core", file))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte(decl)) {
			t.Errorf("cluster/core/%s no longer declares %s: update store's sealedSettings", file, decl)
		}
	}
	b, err := os.ReadFile(filepath.Join("..", "cluster", "core", "operational_credentials_pending.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`const pendingNOCUpdateSettingKey = "operational_credentials.pending_noc_update"`)) {
		t.Error("cluster/core no longer declares pendingNOCUpdateSettingKey as the store seals it")
	}
}
