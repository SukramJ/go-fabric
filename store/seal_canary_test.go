// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/store"
)

// canaryRow reads the seal canary's row: whether it is there, its value and
// its seal mark.
func canaryRow(t *testing.T, db *sql.DB) (present bool, value string, sealed bool) {
	t.Helper()
	err := db.QueryRowContext(t.Context(), `SELECT value, sealed FROM matter_settings WHERE key = ?`, store.SettingSealCanary).Scan(&value, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true, value, sealed
}

// TestSealCanaryMarksTheDatabase (decision 130, 5b-seal R2-2, sec §2b):
// the first NewSealed writes the seal canary — a value sealed under the
// sealer, a reserved settings row — even on a database with nothing to
// seal; a sealer under another key then fails NewSealed with ErrUnseal
// although the database holds no other sealed value; a clear secret row in
// a marked database is refused, not adopted, and stays as it was; a
// NewSealed under the key keeps opening it.
func TestSealCanaryMarksTheDatabase(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	if _, err := store.NewSealed(ctx, db, testSealer(t, 1)); err != nil {
		t.Fatal(err)
	}
	present, value, sealed := canaryRow(t, db)
	if !present || !sealed {
		t.Fatalf("the seal canary after the first NewSealed: present %v, sealed %v", present, sealed)
	}
	if raw, err := base64.StdEncoding.DecodeString(value); err != nil || len(raw) < 28 {
		t.Errorf("the seal canary's value %q is not a sealed value in base64: %v", value, err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 2)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed under another key on a marked database without secrets = %v, want ErrUnseal", err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 1)); err != nil {
		t.Errorf("NewSealed under the key: %v", err)
	}

	// A clear secret row, planted beside the mark: refused, not adopted.
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_settings (key, value, sealed) VALUES ('netcomm.wifi.networks', '[]', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 1)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed over a clear secret row in a marked database = %v, want ErrUnseal", err)
	}
	var v string
	var rowSealed bool
	if err := db.QueryRowContext(ctx, `SELECT value, sealed FROM matter_settings WHERE key = 'netcomm.wifi.networks'`).Scan(&v, &rowSealed); err != nil || v != "[]" || rowSealed {
		t.Errorf("the planted row after the refusal = %q sealed %v, %v; want it unchanged", v, rowSealed, err)
	}
}

// TestNewOnASealedDatabaseWritesNoSecret (decision 130): a Store without
// the sealer on a database that carries the seal canary refuses every write
// of a secret with ErrNoSealer — a new identity, a key set, a resumption
// record, door-lock credential data, an ICD registration, a secret setting
// alone or among others — and still writes what is not a secret.
func TestNewOnASealedDatabaseWritesNoSecret(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	sealed, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	idx, err := sealed.AddFabric(ctx, newFabricRecord(0, 1))
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	for name, err := range map[string]error{
		"UpsertIdentity":     s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: idx, NOC: []byte{1}, PrivateKey: randomSecret(32), IPK: randomSecret(16)}),
		"UpsertGroupKeySet":  s.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: idx, GroupKeySetID: 1, EpochKey0: randomSecret(16), EpochStart0: 1}),
		"UpsertResumption":   s.UpsertResumption(ctx, store.ResumptionRecord{FabricIndex: idx, PeerNodeID: 5, ResumptionID: randomSecret(16), SharedSecret: randomSecret(32)}),
		"SaveDoorLockAuth":   s.SaveDoorLockAuth(ctx, "lock", store.DoorLockAuth{Credentials: []store.DoorLockCredential{{Type: 1, Index: 1, Data: randomSecret(8)}}}),
		"ReplaceICD":         s.ReplaceICDRegistrations(ctx, idx, []store.ICDRegistration{{CheckInNodeID: 1, MonitoredSubject: 1, Key: randomSecret(16)}}),
		"SetSetting":         s.SetSetting(ctx, "netcomm.wifi.networks", "[]"),
		"SetSettings":        s.SetSettings(ctx, map[string]string{"plain": "v", store.SettingDoorLockCredentialKey: "k"}),
		"UpdateCredentials":  s.UpdateFabricCredentials(ctx, idx, store.FabricCredentials{NOC: []byte{2}, PrivateKey: randomSecret(32), NodeID: 3}),
		"SetSetting reserve": s.SetSetting(ctx, store.SettingSealCanary, "x"),
	} {
		want := store.ErrNoSealer
		if name == "SetSetting reserve" {
			if err == nil {
				t.Errorf("%s: a write of the reserved seal canary succeeded", name)
			}
			continue
		}
		if name == "UpdateCredentials" && errors.Is(err, store.ErrIdentityNotFound) {
			continue // no identity to update; the refusal of UpsertIdentity covers the key
		}
		if !errors.Is(err, want) {
			t.Errorf("%s on a sealed database without the sealer = %v, want ErrNoSealer", name, err)
		}
	}
	if _, ok, _ := s.GetSetting(ctx, "plain"); ok {
		t.Error("SetSettings wrote its clear key although it refused the secret one")
	}
	if err := s.SetSetting(ctx, "plain", "v"); err != nil {
		t.Errorf("a clear setting on a sealed database: %v", err)
	}
	if err := s.DeleteSettingsWithPrefix(ctx, "store."); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSettings(ctx, store.SettingSealCanary); err != nil {
		t.Fatal(err)
	}
	if present, _, _ := canaryRow(t, db); !present {
		t.Error("a settings delete removed the seal canary")
	}
}

// TestEraseDropsTheSealMark (decision 130, step 2 consequence): Erase is
// the way back — after it the database carries no mark, a Store without a
// sealer writes secrets again and a sealer under another key opens it.
func TestEraseDropsTheSealMark(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Erase(ctx); err != nil {
		t.Fatal(err)
	}
	if present, _, _ := canaryRow(t, db); present {
		t.Error("the seal canary after Erase")
	}
	if err := store.New(db).SetSetting(ctx, "netcomm.wifi.networks", "[]"); err != nil {
		t.Errorf("a clear secret after Erase: %v", err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 2)); err != nil {
		t.Errorf("NewSealed under another key after Erase: %v", err)
	}
}

// TestNewSealedWithAnOwedScrub (R2-P6-err-0108, decision 132): when the
// scrub after the sealing cannot finish — a reader holds the -wal —
// NewSealed returns the sealed, strict Store and no error; ScrubOwed says
// the scrub is owed; the next NewSealed, once the reader is gone, scrubs
// and ScrubOwed says so.
func TestNewSealedWithAnOwedScrub(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "matter.db")
	db, err := store.OpenSQLite(path, walMode)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 0`); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	w := writeSecrets(t, store.New(db))
	reader, err := store.OpenSQLite(path, walMode)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	tx, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var rows int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM matter_settings`).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	s, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil || s == nil {
		t.Fatalf("NewSealed with a reader on the -wal = %v, %v; want the sealed Store and no error", s, err)
	}
	if owed, err := s.ScrubOwed(ctx); err != nil || !owed {
		t.Errorf("ScrubOwed with the reader there = %v, %v; want true", owed, err)
	}
	checkSecretsRead(t, s, w)
	if err := store.New(db).SetSetting(ctx, "netcomm.wifi.networks", "[]"); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("a clear secret write on the database with an owed scrub = %v, want ErrNoSealer", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	again, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if owed, err := again.ScrubOwed(ctx); err != nil || owed {
		t.Errorf("ScrubOwed after the next NewSealed = %v, %v; want false", owed, err)
	}
	if hits := clearSecretsIn(t, filepath.Dir(path), w.secrets); len(hits) > 0 {
		t.Errorf("clear secrets on disk after the owed scrub ran: %v", hits)
	}
}

// TestReseal (decision 131, N5b-seal-1): Reseal moves every sealed value
// and the seal canary from the old sealer to the new one in one transaction
// and scrubs: the new key opens the database and reads every secret, the
// old one no longer does, and none of the values sealed under the old key
// is left in the files. A wrong old key, a clear secret row and a nil
// sealer are refused and change nothing.
func TestReseal(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := t.TempDir()
	db := openFileDB(t, dir)
	old, next := testSealer(t, 1), testSealer(t, 2)
	s, err := store.NewSealed(ctx, db, old)
	if err != nil {
		t.Fatal(err)
	}
	w := writeSecrets(t, s)
	var oldKey, oldResumption []byte
	if err := db.QueryRowContext(ctx, `SELECT private_key FROM matter_node_identities`).Scan(&oldKey); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT shared_secret FROM matter_resumption`).Scan(&oldResumption); err != nil {
		t.Fatal(err)
	}
	_, oldCanary, _ := canaryRow(t, db)

	if err := store.Reseal(ctx, db, nil, next); err == nil {
		t.Error("Reseal with a nil old sealer succeeded")
	}
	if err := store.Reseal(ctx, db, testSealer(t, 3), next); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("Reseal under a wrong old key = %v, want ErrUnseal", err)
	}
	if _, value, _ := canaryRow(t, db); value != oldCanary {
		t.Error("a refused Reseal changed the seal canary")
	}

	if err := store.Reseal(ctx, db, old, next); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSealed(ctx, db, old); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed under the old key after Reseal = %v, want ErrUnseal", err)
	}
	s2, err := store.NewSealed(ctx, db, next)
	if err != nil {
		t.Fatal(err)
	}
	checkSecretsRead(t, s2, w)
	if owed, err := s2.ScrubOwed(ctx); err != nil || owed {
		t.Errorf("ScrubOwed after Reseal = %v, %v", owed, err)
	}
	for name, v := range map[string][]byte{"old sealed key": oldKey, "old sealed resumption": oldResumption, "old seal canary": []byte(oldCanary)} {
		if files := filesHolding(t, dir, string(v)); len(files) > 0 {
			t.Errorf("%s still in %v after Reseal", name, files)
		}
	}
	if hits := clearSecretsIn(t, dir, w.secrets); len(hits) > 0 {
		t.Errorf("clear secrets on disk after Reseal: %v", hits)
	}

	// A clear secret row: refused, nothing moved.
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_settings (key, value, sealed) VALUES (?, 'k', 0)`, store.SettingDoorLockCredentialKey+".planted"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_settings SET sealed = 0, value = 'clear' WHERE key = 'netcomm.wifi.networks'`); err != nil {
		t.Fatal(err)
	}
	if err := store.Reseal(ctx, db, next, old); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("Reseal over a clear secret row = %v, want ErrUnseal", err)
	}
	if _, err := store.SealedWithoutCheck(db, next).GetIdentity(ctx, w.fabric); err != nil {
		t.Errorf("the identity after a refused Reseal does not open under the current key: %v", err)
	}
}

// TestSealCanaryAndResealRefusals: the refusals around the seal canary
// and Reseal — a canary edited to clear or to another value fails
// NewSealed with ErrUnseal; Reseal refuses a schema before v10 and a nil
// new sealer, fails with a sealer that cannot seal and leaves the database
// as it was, refuses a sealed setting whose value is not base64, and
// reseals a sealed database that carries no canary (one Erase emptied and
// a sealed store then wrote to), marking it; ScrubOwed and Reseal on a
// closed handle return an error.
func TestSealCanaryAndResealRefusals(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	db := openTestDB(t)
	if _, err := store.NewSealed(ctx, db, testSealer(t, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_settings SET sealed = 0 WHERE key = ?`, store.SettingSealCanary); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSealed(ctx, db, testSealer(t, 1)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed over a clear canary = %v, want ErrUnseal", err)
	}
	other := openTestDB(t)
	s2, err := store.NewSealed(ctx, other, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	// A sealed setting value moved onto the canary's row: it opens under
	// the key only with its own aad, so the canary does not open.
	if err := s2.SetSetting(ctx, "netcomm.wifi.networks", "[]"); err != nil {
		t.Fatal(err)
	}
	if _, err := other.ExecContext(ctx, `UPDATE matter_settings SET value = (SELECT value FROM matter_settings WHERE key = 'netcomm.wifi.networks') WHERE key = ?`, store.SettingSealCanary); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSealed(ctx, other, testSealer(t, 1)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed over a canary holding another value = %v, want ErrUnseal", err)
	}

	v9 := openEmptyDB(t)
	applyUp(t, v9, "v9")
	if err := store.Reseal(ctx, v9, testSealer(t, 1), testSealer(t, 2)); err == nil || !strings.Contains(err.Error(), "v10") {
		t.Errorf("Reseal on a v9 database = %v; want the schema named", err)
	}
	fresh := openTestDB(t)
	if err := store.Reseal(ctx, fresh, testSealer(t, 1), nil); err == nil {
		t.Error("Reseal with a nil new sealer succeeded")
	}

	// A sealed database without the canary: Erase, then a sealed store's
	// writes. Reseal opens every value under the old key and marks it.
	unmarked := openTestDB(t)
	s3, err := store.NewSealed(ctx, unmarked, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s3.Erase(ctx); err != nil {
		t.Fatal(err)
	}
	w := writeSecrets(t, s3)
	down := &failingSealer{inner: testSealer(t, 2), fail: true}
	if err := store.Reseal(ctx, unmarked, testSealer(t, 1), down); !errors.Is(err, errSealerDown) {
		t.Errorf("Reseal with a new sealer that cannot seal = %v, want its error", err)
	}
	if present, _, _ := canaryRow(t, unmarked); present {
		t.Error("a failed Reseal wrote the canary")
	}
	checkSecretsRead(t, s3, w)
	if err := store.Reseal(ctx, unmarked, testSealer(t, 1), testSealer(t, 2)); err != nil {
		t.Fatal(err)
	}
	if present, _, sealed := canaryRow(t, unmarked); !present || !sealed {
		t.Error("Reseal of a database without the canary did not mark it")
	}
	s4, err := store.NewSealed(ctx, unmarked, testSealer(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	checkSecretsRead(t, s4, w)

	// A sealed setting whose value is not base64.
	if _, err := unmarked.ExecContext(ctx, `UPDATE matter_settings SET value = '%%%' WHERE key = 'netcomm.wifi.networks'`); err != nil {
		t.Fatal(err)
	}
	if err := store.Reseal(ctx, unmarked, testSealer(t, 2), testSealer(t, 3)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("Reseal over a sealed setting that is not base64 = %v, want ErrUnseal", err)
	}

	closed := openTestDB(t)
	cs, err := store.NewSealed(ctx, closed, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Close()
	if _, err := cs.ScrubOwed(ctx); err == nil {
		t.Error("ScrubOwed on a closed handle: want an error")
	}
	if err := store.Reseal(ctx, closed, testSealer(t, 1), testSealer(t, 2)); err == nil {
		t.Error("Reseal on a closed handle: want an error")
	}
}

// TestResealOwedMarkStaysTruthful (Fable read R3): when the owed mark cannot
// be deleted after Reseal's commit and scrub, Reseal says
// ErrScrubIncomplete — the values are under the new key, ScrubOwed still
// reports the scrub, and the new key opens the database.
func TestResealOwedMarkStaysTruthful(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s, err := store.NewSealed(ctx, db, testSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	w := writeSecrets(t, s)
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER keep_owed BEFORE DELETE ON matter_metadata
WHEN OLD.key = 'store.seal_scrub_owed' BEGIN SELECT RAISE(ABORT, 'kept'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.Reseal(ctx, db, testSealer(t, 1), testSealer(t, 2)); !errors.Is(err, store.ErrScrubIncomplete) {
		t.Fatalf("Reseal whose owed mark stays = %v, want ErrScrubIncomplete", err)
	}
	next, err := store.NewSealed(ctx, db, testSealer(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	if owed, err := next.ScrubOwed(ctx); err != nil || !owed {
		t.Errorf("ScrubOwed after the mark stayed = %v, %v; want true", owed, err)
	}
	checkSecretsRead(t, next, w)
}
