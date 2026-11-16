// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Sealer seals the secrets a [Store] made by [NewSealed] keeps at rest.
// The host supplies it, and its key lives outside the database: in a file
// of its own, the operating system's keychain, a TPM or a KMS. Only then
// does sealing protect a copy of the database. SQLite rewrites a row on
// every change and keeps the old page in its free list and in the -wal
// until the page is reused, so an UpdateNOC's replaced key, a removed
// fabric's IPK or a resumption secret overwritten an hour ago is still in
// the file; a key kept in the same file would be in every copy beside the
// values it seals. [AESGCMSealer] is a correct default; how the key is
// provisioned and rotated is the host's (the store rotates nothing).
//
// Seal and Open are called with the same aad for one value: the table, the
// column and the row's key, so a sealed value moved to another row or
// column does not open. Both must be safe for concurrent use.
type Sealer interface {
	// Seal returns plain sealed and bound to aad.
	Seal(plain, aad []byte) ([]byte, error)
	// Open returns the plain value of a value Seal sealed under the same
	// key and aad, and an error for every other value.
	Open(sealed, aad []byte) ([]byte, error)
}

// AESGCMSealer is the stdlib [Sealer]: AES-256-GCM under the host's key,
// with a random 96-bit nonce ahead of each sealed value. A node seals a
// few values per commissioning, resumption and settings change, far below
// the 2^32 messages a random GCM nonce is good for under one key.
type AESGCMSealer struct {
	aead cipher.AEAD
}

// aesGCMSealerKeySize is AES-256's key size.
const aesGCMSealerKeySize = 32

// NewAESGCMSealer returns an [AESGCMSealer] under key, which must be 32
// bytes. The key is the host's: generated once (crypto/rand), kept outside
// the database, never stored by the store.
func NewAESGCMSealer(key []byte) (*AESGCMSealer, error) {
	if len(key) != aesGCMSealerKeySize {
		return nil, fmt.Errorf("store: AES-GCM sealer: key of %d bytes, want %d", len(key), aesGCMSealerKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("store: AES-GCM sealer: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("store: AES-GCM sealer: %w", err)
	}
	return &AESGCMSealer{aead: aead}, nil
}

// Seal implements [Sealer]: the nonce, then the sealed value with its tag.
func (a *AESGCMSealer) Seal(plain, aad []byte) ([]byte, error) {
	n := a.aead.NonceSize()
	out := make([]byte, n, n+len(plain)+a.aead.Overhead())
	if _, err := rand.Read(out); err != nil {
		return nil, fmt.Errorf("store: AES-GCM sealer: nonce: %w", err)
	}
	return a.aead.Seal(out, out, plain, aad), nil
}

// Open implements [Sealer].
func (a *AESGCMSealer) Open(sealed, aad []byte) ([]byte, error) {
	n := a.aead.NonceSize()
	if len(sealed) < n+a.aead.Overhead() {
		return nil, errors.New("store: AES-GCM sealer: sealed value too short")
	}
	plain, err := a.aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, fmt.Errorf("store: AES-GCM sealer: %w", err)
	}
	return plain, nil
}

// NewSealed returns a Store backed by db, as [New] does, whose secret
// columns s seals: the operational private keys (private_key, and
// prior_private_key while an UpdateNOC is pending) and the IPK of
// matter_node_identities, the epoch keys of matter_group_keys, the shared
// secret of matter_resumption, the ICDToken keys of
// matter_icd_registrations, the DoorLock credential data of
// matter_doorlock_credentials, and these settings of matter_settings: the
// Wi-Fi networks with their credentials (netcomm.wifi.networks), the
// DoorLock credential key ([SettingDoorLockCredentialKey]), the ICD key
// ([SettingICDKey]) and the two settings a release before schema v8 kept
// an uncommitted AddNOC's or UpdateNOC's credentials in
// (operational_credentials.pending_noc, …_update), and every setting
// under the prefix jointfabric.datastore: (the JointFabricDatastore record,
// which carries the joint fabric's group key sets). Each row carries its
// seal mark (the sealed column, schema v10). Under s the ICD keys are
// sealed by s, not under [SettingICDKey], and DoorLock credential data,
// which cluster/lock encrypts under [SettingDoorLockCredentialKey] before
// the store sees it, is sealed again, with that key sealed too.
//
// Sealing protects the confidentiality of the secrets in a copy of the
// database. It does not authenticate the database: the non-secret columns
// and the seal mark are not protected, and an older sealed value of the
// same row, written back, opens — there is no tamper or rollback
// detection.
//
// db must carry schema v10 ([Apply] or [Migrations]). NewSealed first
// opens one sealed value the database already holds (the first sealed
// identity, else another sealed row or setting): a sealer under another
// key — or that value changed outside the store — fails NewSealed with
// [ErrUnseal] before anything is written; a database with no sealed value
// opens. Before NewSealed returns it seals every row it finds in clear — a database a Store
// without a sealer wrote — in one transaction, so a failure (a value that
// does not open, a Seal error) leaves the database as it was; then it
// rewrites the file and empties the -wal ([Scrub]), so the clear values do
// not stay behind in free pages — measured in WAL mode and in the rollback
// journal's DELETE (SQLite's default) and TRUNCATE modes; in PERSIST mode
// the kept -journal still holds clear pages, and copies taken before the
// upgrade keep the clear values. A scrub that does not finish is
// [ErrScrubIncomplete], and the next NewSealed tries again. A database
// with no clear row is not written to.
//
// After that, while the store is open, it is strict: it writes no secret
// in clear, and a secret row it reads without the seal mark — one whose
// mark was edited, or a clear row planted or written by a Store without
// the sealer — is refused with [ErrUnseal], never read as the secret. A
// later NewSealed adopts every clear row it finds: it cannot tell such a
// row from one a Store without a sealer wrote, and seals it. Every Store of one
// database needs the sealer; a concurrent writer without it on the same
// database is not supported. A Store without the sealer reads a sealed
// value as [ErrNoSealer] and refuses a write that would leave a sealed row
// half clear. Migration v10's Down refuses a database that holds a sealed
// row.
func NewSealed(ctx context.Context, db *sql.DB, s Sealer) (*Store, error) {
	if s == nil {
		return nil, errors.New("store: NewSealed: nil Sealer")
	}
	st := &Store{db: db, sealer: s}
	if err := st.sealClearRows(ctx); err != nil {
		return nil, err
	}
	return st, nil
}

var (
	// ErrNoSealer is a sealed secret a Store without a sealer read, or a
	// write it refused because the row is sealed. The database was sealed
	// by [NewSealed]: the host opens its store with NewSealed and the same
	// sealer; there is no way back to a clear database.
	ErrNoSealer = errors.New("store: the value is sealed and the store has no sealer (store.NewSealed)")
	// ErrUnseal is a secret a sealed Store could not open: the sealer's key
	// is not the one the value was sealed under, the value was moved from
	// another row or column, or the row was changed outside the store (a
	// clear secret row, a sealed value cut short). The host checks it hands
	// the right key; with the right key the database was tampered with,
	// and the row's secret is lost — a factory reset or a restore from a
	// backup sealed under that key.
	ErrUnseal = errors.New("store: a sealed value does not open")
)

// sealAADPrefix opens every aad, naming the scheme and its version.
const sealAADPrefix = "go-fabric/store/seal/v1"

// sealAAD is the aad of one secret value: the table, the column and the
// row's key, each length-prefixed and the key parts typed, so no two
// cells share one. An integer key part is its 8-byte big-endian value; a
// text or blob part its bytes. A key part of another type — a key column
// that holds NULL or a REAL — is an error.
func sealAAD(table, column string, key ...any) ([]byte, error) {
	b := []byte(sealAADPrefix)
	add := func(tag byte, p []byte) {
		b = append(b, tag)
		b = binary.AppendUvarint(b, uint64(len(p)))
		b = append(b, p...)
	}
	add('t', []byte(table))
	add('c', []byte(column))
	for _, k := range key {
		switch v := k.(type) {
		case int64:
			add('i', binary.BigEndian.AppendUint64(nil, uint64(v))) //nolint:gosec // a bit pattern, not a value
		case uint8:
			add('i', binary.BigEndian.AppendUint64(nil, uint64(v)))
		case uint16:
			add('i', binary.BigEndian.AppendUint64(nil, uint64(v)))
		case string:
			add('s', []byte(v))
		case []byte:
			add('b', v)
		default:
			return nil, fmt.Errorf("store: seal aad of %s.%s: key part of type %T", table, column, k)
		}
	}
	return b, nil
}

// sealColumn is the stored form of one secret value: sealed under the
// store's sealer, or as it is without one. nil (SQL NULL) stays nil.
func (s *Store) sealColumn(plain []byte, table, column string, key ...any) ([]byte, error) {
	if plain == nil || s.sealer == nil {
		return plain, nil
	}
	aad, err := sealAAD(table, column, key...)
	if err != nil {
		return nil, err
	}
	out, err := s.sealer.Seal(plain, aad)
	if err != nil {
		return nil, fmt.Errorf("store: seal %s.%s: %w", table, column, err)
	}
	return out, nil
}

// openColumn is the value of one stored secret: without a sealer the
// clear value of a row that is not sealed; under a sealer the opened value
// of a sealed row. A sealed value without a sealer is [ErrNoSealer]; a
// value that does not open, and a clear one under a sealer, are
// [ErrUnseal]. NULL stays nil.
func (s *Store) openColumn(stored []byte, sealed bool, table, column string, key ...any) ([]byte, error) {
	switch {
	case stored == nil:
		return nil, nil
	case !sealed && s.sealer == nil:
		return stored, nil
	case !sealed:
		return nil, fmt.Errorf("%w: %s.%s is not sealed under a sealer", ErrUnseal, table, column)
	case s.sealer == nil:
		return nil, fmt.Errorf("%w: %s.%s", ErrNoSealer, table, column)
	}
	aad, err := sealAAD(table, column, key...)
	if err != nil {
		return nil, err
	}
	plain, err := s.sealer.Open(stored, aad)
	if err != nil {
		return nil, fmt.Errorf("%w: %s.%s: %w", ErrUnseal, table, column, err)
	}
	return plain, nil
}

// clearRowUnderSealer is the error of a secret row a sealed store finds
// without the seal mark.
func clearRowUnderSealer(table string) error {
	return fmt.Errorf("%w: a %s row is not sealed under a sealer", ErrUnseal, table)
}

// sealedSettings are the matter_settings keys a sealer seals: the values
// that are secrets. The cluster/core and cluster/jointfabric keys are
// their packages' unexported constants (cluster/core
// network_commissioning_wifi.go wifiNetworksKey,
// operational_credentials_pending.go pendingNOCSettingKey and
// pendingNOCUpdateSettingKey; cluster/jointfabric datastore.go
// settingPrefix); the controller's CA keys are controller/ca's
// SettingStore defaults (persist.go DefaultSettingKey, and FabricKey's
// ".fabric") — the CA with its root (and ICAC) key, and the controller's
// fabric with its IPK and operational key. TestSealedSettingsNameTheirSources
// holds the copies to them.
var sealedSettings = map[string]bool{
	SettingDoorLockCredentialKey:                 true,
	SettingICDKey:                                true,
	"netcomm.wifi.networks":                      true,
	"operational_credentials.pending_noc":        true,
	"operational_credentials.pending_noc_update": true,
	"controller.ca":                              true,
	"controller.ca.fabric":                       true,
}

// sealedSettingPrefixes seal every key that starts with one of them; the
// aad keeps the whole key.
var sealedSettingPrefixes = []string{"jointfabric.datastore:"}

// isSealedSetting reports whether key holds a secret.
func isSealedSetting(key string) bool {
	if sealedSettings[key] {
		return true
	}
	for _, p := range sealedSettingPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// sealSetting is the stored form of a setting: a secret's sealed bytes in
// base64 under a sealer (the column is TEXT), every other value as it is.
func (s *Store) sealSetting(key, value string) (stored string, sealed bool, err error) {
	if s.sealer == nil || !isSealedSetting(key) {
		return value, false, nil
	}
	out, err := s.sealColumn([]byte(value), "matter_settings", "value", key)
	if err != nil {
		return "", false, err
	}
	return base64.StdEncoding.EncodeToString(out), true, nil
}

// openSetting is the value of a stored setting; under a sealer a secret
// setting that is not sealed is [ErrUnseal].
func (s *Store) openSetting(key, stored string, sealed bool) (string, error) {
	if !sealed {
		if s.sealer != nil && isSealedSetting(key) {
			return "", fmt.Errorf("%w: matter_settings %q is not sealed under a sealer", ErrUnseal, key)
		}
		return stored, nil
	}
	if s.sealer == nil {
		return "", fmt.Errorf("%w: matter_settings %q", ErrNoSealer, key)
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", fmt.Errorf("%w: matter_settings %q: %w", ErrUnseal, key, err)
	}
	plain, err := s.openColumn(raw, true, "matter_settings", "value", key)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// readSetting reads one setting on q — the database or a transaction —
// opened.
func (s *Store) readSetting(ctx context.Context, q sqlExecQuerier, key string) (value string, ok bool, err error) {
	var sealed bool
	err = q.QueryRowContext(ctx, `SELECT value, sealed FROM matter_settings WHERE key = ?`, key).Scan(&value, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if value, err = s.openSetting(key, value, sealed); err != nil {
		return "", false, err
	}
	return value, true, nil
}

// sealedTables are the tables with a seal mark; [NewSealed] needs it on
// each.
var sealedTables = []string{
	"matter_node_identities", "matter_group_keys", "matter_resumption",
	"matter_settings", "matter_doorlock_credentials", "matter_icd_registrations",
}

// metadataKeySealScrubOwed marks a sealing whose [Scrub] has not finished:
// set in the sealing's transaction, deleted once the scrub ran.
const metadataKeySealScrubOwed = "store.seal_scrub_owed"

// sealClearRows is [NewSealed]'s upgrade: every clear row sealed in one
// transaction, then the scrub that removes the clear values' old pages.
func (s *Store) sealClearRows(ctx context.Context) error {
	for _, table := range sealedTables {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'sealed'`, table).Scan(&n); err != nil {
			return fmt.Errorf("store: NewSealed: inspect %s: %w", table, err)
		}
		if n == 0 {
			return fmt.Errorf("store: NewSealed: %s has no seal mark: the database needs schema v10 (store.Apply or store.Migrations)", table)
		}
	}
	// The key check before the sealing: a wrong key must not seal the
	// clear rows beside values it does not open.
	if err := s.openOneSealed(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: NewSealed: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// A write first, so the transaction holds the write lock before it
	// reads (UpdateFabricCredentials' reason).
	if _, err := tx.ExecContext(ctx, `UPDATE matter_metadata SET value = value WHERE key = ?`, metadataKeySealScrubOwed); err != nil {
		return fmt.Errorf("store: NewSealed: %w", err)
	}
	sealed := 0
	for _, t := range []struct {
		table         string
		keys, secrets []string
	}{
		{"matter_node_identities", []string{"fabric_index"}, []string{"private_key", "ipk", "prior_private_key"}},
		{"matter_group_keys", []string{"fabric_index", "group_key_set_id"}, []string{"epoch_key_0", "epoch_key_1", "epoch_key_2"}},
		{"matter_resumption", []string{"fabric_index", "peer_node_id"}, []string{"shared_secret"}},
		{"matter_doorlock_credentials", []string{"endpoint_key", "credential_type", "credential_index"}, []string{"credential_data"}},
	} {
		n, err := s.sealTable(ctx, tx, t.table, t.keys, t.secrets)
		if err != nil {
			return fmt.Errorf("store: NewSealed: %s: %w", t.table, err)
		}
		sealed += n
	}
	// The ICD keys before the settings: a clear row's key may be sealed
	// under SettingICDKey, which is still a clear setting here
	// (sealClearICDRows reads it as a store without a sealer).
	n, err := s.sealClearICDRows(ctx, tx)
	if err != nil {
		return fmt.Errorf("store: NewSealed: matter_icd_registrations: %w", err)
	}
	sealed += n
	if n, err = s.sealClearSettings(ctx, tx); err != nil {
		return fmt.Errorf("store: NewSealed: matter_settings: %w", err)
	}
	sealed += n
	if sealed > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO matter_metadata (key, value) VALUES (?, 1)`, metadataKeySealScrubOwed); err != nil {
			return fmt.Errorf("store: NewSealed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: NewSealed: commit: %w", err)
	}
	if _, owed, err := s.GetMetadataCounter(ctx, metadataKeySealScrubOwed); err != nil {
		return fmt.Errorf("store: NewSealed: %w", err)
	} else if !owed {
		return nil
	}
	if err := Scrub(ctx, s.db); err != nil {
		return fmt.Errorf("store: NewSealed: the clear values are sealed: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM matter_metadata WHERE key = ?`, metadataKeySealScrubOwed); err != nil {
		return fmt.Errorf("store: NewSealed: %w", err)
	}
	return nil
}

// sealedProbes are the secret columns [Store.openOneSealed] tries, in
// order, with the key columns of their aad: the identity's private key
// first, then the other sealed tables'.
var sealedProbes = []struct {
	table, column string
	keys          []string
}{
	{identityTable, "private_key", []string{"fabric_index"}},
	{"matter_group_keys", "epoch_key_0", []string{"fabric_index", "group_key_set_id"}},
	{"matter_resumption", "shared_secret", []string{"fabric_index", "peer_node_id"}},
	{doorLockCredentialTable, "credential_data", []string{"endpoint_key", "credential_type", "credential_index"}},
	{"matter_icd_registrations", "icd_key", []string{"fabric_index", "check_in_node_id", "monitored_subject"}},
}

// openOneSealed is [NewSealed]'s key check: it opens one sealed value the
// database already holds — the first sealed identity's private key, else
// the first sealed value of the other sealed tables, else the first sealed
// setting — so a sealer under another key, or a value changed outside the
// store, fails NewSealed with [ErrUnseal] instead of the first read later.
// A database with no sealed value passes. The key parts are read as the
// driver returns them, as sealTable seals them.
func (s *Store) openOneSealed(ctx context.Context) error {
	for _, p := range sealedProbes {
		cols := strings.Join(p.keys, ", ")
		key := make([]any, len(p.keys))
		dest := make([]any, 0, len(key)+1)
		for i := range key {
			dest = append(dest, &key[i])
		}
		var val []byte
		dest = append(dest, &val)
		err := s.db.QueryRowContext(ctx, `SELECT `+cols+`, `+p.column+` FROM `+p.table+ //nolint:gosec // the package's constants
			` WHERE sealed = 1 AND `+p.column+` IS NOT NULL ORDER BY `+cols+` LIMIT 1`).Scan(dest...)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("store: NewSealed: read a sealed %s value: %w", p.table, err)
		}
		if _, err := s.openColumn(val, true, p.table, p.column, key...); err != nil {
			return fmt.Errorf("store: NewSealed: the sealer does not open the database's sealed values: %w", err)
		}
		return nil
	}
	var key, val string
	err := s.db.QueryRowContext(ctx, `SELECT key, value FROM matter_settings WHERE sealed = 1 ORDER BY key LIMIT 1`).Scan(&key, &val)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: NewSealed: read a sealed setting: %w", err)
	}
	if _, err := s.openSetting(key, val, true); err != nil {
		return fmt.Errorf("store: NewSealed: the sealer does not open the database's sealed values: %w", err)
	}
	return nil
}

// sealTable seals the clear rows of one table: each non-NULL secret column
// sealed under its row's key columns, and the row marked. It returns how
// many rows it sealed. table and the columns are the package's constants.
func (s *Store) sealTable(ctx context.Context, tx *sql.Tx, table string, keys, secrets []string) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+strings.Join(keys, ", ")+`, `+strings.Join(secrets, ", ")+ //nolint:gosec // the package's constants
		` FROM `+table+` WHERE sealed = 0`)
	if err != nil {
		return 0, err
	}
	type row struct {
		key  []any
		vals [][]byte
	}
	var plain []row
	for rows.Next() {
		r := row{key: make([]any, len(keys)), vals: make([][]byte, len(secrets))}
		dest := make([]any, 0, len(keys)+len(secrets))
		for i := range r.key {
			dest = append(dest, &r.key[i])
		}
		for i := range r.vals {
			dest = append(dest, &r.vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			_ = rows.Close()
			return 0, err
		}
		plain = append(plain, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	set := make([]string, 0, len(secrets)+1)
	for _, c := range secrets {
		set = append(set, c+" = ?")
	}
	set = append(set, "sealed = 1")
	where := make([]string, 0, len(keys))
	for _, k := range keys {
		where = append(where, k+" = ?")
	}
	update := `UPDATE ` + table + ` SET ` + strings.Join(set, ", ") + ` WHERE ` + strings.Join(where, " AND ") //nolint:gosec // the package's constants
	for _, r := range plain {
		args := make([]any, 0, len(secrets)+len(keys))
		for i, c := range secrets {
			v, err := s.sealColumn(r.vals[i], table, c, r.key...)
			if err != nil {
				return 0, err
			}
			args = append(args, v)
		}
		args = append(args, r.key...)
		if _, err := tx.ExecContext(ctx, update, args...); err != nil {
			return 0, err
		}
	}
	return len(plain), nil
}

// sealClearICDRows seals the clear ICD registrations: each key opened from
// the form the store keeps without a sealer (under [SettingICDKey], or 16
// clear bytes from before v0.12.1) and sealed by the sealer.
func (s *Store) sealClearICDRows(ctx context.Context, tx *sql.Tx) (int, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT fabric_index, position, check_in_node_id, monitored_subject, icd_key FROM matter_icd_registrations
WHERE sealed = 0`)
	if err != nil {
		return 0, err
	}
	type row struct {
		fabric        uint8
		position      int64
		node, subject []byte
		key           []byte
	}
	var plain []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.fabric, &r.position, &r.node, &r.subject, &r.key); err != nil {
			_ = rows.Close()
			return 0, err
		}
		plain = append(plain, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	var aead cipher.AEAD
	for _, r := range plain {
		if len(r.node) != 8 || len(r.subject) != 8 {
			return 0, fmt.Errorf("store: icd registration: node id of %d, subject of %d bytes", len(r.node), len(r.subject))
		}
		key := r.key
		if len(key) != icdKeySize {
			if aead == nil {
				// Read as a store without a sealer: the key is a clear
				// setting until sealClearSettings seals it below.
				if aead, err = (&Store{}).icdAEAD(ctx, tx); err != nil {
					if errors.Is(err, ErrNoSealer) {
						// The key is sealed but this row is not: an edited
						// file, not a missing sealer.
						return 0, fmt.Errorf("%w: a clear ICD row under a sealed %s (%v)", ErrUnseal, SettingICDKey, err) //nolint:errorlint // one sentinel, ErrUnseal
					}
					return 0, err
				}
			}
			if key, err = openICDKey(aead, key, icdAAD(r.fabric, binary.BigEndian.Uint64(r.node), binary.BigEndian.Uint64(r.subject))); err != nil {
				return 0, fmt.Errorf("store: icd registration of fabric %d at %d: %w", r.fabric, r.position, err)
			}
		}
		sealed, err := s.sealColumn(key, "matter_icd_registrations", "icd_key", r.fabric, r.node, r.subject)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE matter_icd_registrations SET icd_key = ?, sealed = 1 WHERE fabric_index = ? AND position = ?`,
			sealed, r.fabric, r.position); err != nil {
			return 0, err
		}
	}
	return len(plain), nil
}

// sealClearSettings seals the clear values of the secret settings.
func (s *Store) sealClearSettings(ctx context.Context, tx *sql.Tx) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT key, value FROM matter_settings WHERE sealed = 0`)
	if err != nil {
		return 0, err
	}
	due := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if isSealedSetting(k) {
			due[k] = v
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	n := 0
	for key, v := range due {
		stored, _, err := s.sealSetting(key, v)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE matter_settings SET value = ?, sealed = 1 WHERE key = ?`, stored, key); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}
