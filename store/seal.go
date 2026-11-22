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

	"thingfabric.dev/go-fabric/internal/sealseam"
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
// provisioned is the host's, and [Reseal] moves a database to a new key.
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

// maxSealedPlain bounds a value Seal takes: far above any secret the store
// keeps, and low enough that the nonce, the value and the tag fit an int on
// a 32-bit platform.
const maxSealedPlain = 1 << 24

// Seal implements [Sealer]: the nonce, then the sealed value with its tag.
func (a *AESGCMSealer) Seal(plain, aad []byte) ([]byte, error) {
	n := a.aead.NonceSize()
	if len(plain) > maxSealedPlain {
		return nil, errors.New("store: AES-GCM sealer: value too large to seal")
	}
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
// db must carry schema v10 ([Apply] or [Migrations]).
//
// The seal canary. The first NewSealed marks the database with one: a reserved
// matter_settings row holds a known value sealed under s. Every later
// NewSealed opens the canary first, so a sealer under another key fails with
// [ErrUnseal] before anything is written — whether or not the database
// holds a secret yet — and a database that carries the canary has a secret
// row in clear refused with ErrUnseal instead of adopted. A Store without
// a sealer ([New]) refuses to write a secret into a database that carries it
// ([ErrNoSealer]). The canary is not tamper protection: whoever can write the
// file can delete the row, and the database then counts as never sealed.
// [Store.Erase] drops it with every other row — a factory reset is the way
// back to a database without one — and [Reseal] moves it to a new sealer.
//
// The first NewSealed (no canary yet) checks the key on one sealed value the
// database already holds, if any (the first sealed identity, else another
// sealed row or setting), then seals every row it finds in clear — a
// database a Store without a sealer wrote — and writes the canary, in one
// transaction, so a failure (a value that does not open, a Seal error)
// leaves the database as it was; then it rewrites the file and empties the
// -wal ([Scrub]), so the clear values do not stay behind in free pages —
// measured in WAL mode, in the rollback journal's DELETE (SQLite's default),
// TRUNCATE and PERSIST modes and under locking_mode EXCLUSIVE, whose kept
// -journal Scrub truncates; copies taken before the upgrade keep the clear
// values. A scrub that does not finish (a full disk, a reader holding the
// -wal) does not fail NewSealed: the rows are sealed, the Store is
// returned, the scrub stays owed ([Store.ScrubOwed]) and every later
// NewSealed tries it again.
//
// While the store is open it is strict: it writes no secret in clear, and
// a secret row it reads without the seal mark — one whose mark was edited,
// or a clear row planted — is refused with [ErrUnseal], never read as the
// secret. Every Store of one database needs the sealer; a concurrent
// writer without it on the same database is not supported. A Store without
// the sealer reads a sealed value as [ErrNoSealer]. Migration v10's Down
// refuses a database that holds a sealed row.
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
	// write it refused because the row is sealed or the database carries
	// the seal canary. The database was sealed by [NewSealed]: the host
	// opens its store with NewSealed and the same sealer; the way back to a
	// clear database is a factory reset ([Store.Erase]).
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
// fabric with its IPK and operational key; the node's Rotating Device
// Identifier unique id is node's settingRotatingIDUniqueID (node.go) — not
// a key, but whoever reads it computes every RI the node advertises.
// TestSealedSettingsNameTheirSources holds the copies to them. A secret
// added here ships with an upgrade step
// (TestSecretsAddedAfterV11ShipAnUpgradeStep, decision 157).
var sealedSettings = map[string]bool{
	SettingDoorLockCredentialKey:                 true,
	SettingICDKey:                                true,
	"netcomm.wifi.networks":                      true,
	"operational_credentials.pending_noc":        true,
	"operational_credentials.pending_noc_update": true,
	"controller.ca":                              true,
	"controller.ca.fabric":                       true,
	"commissioning.rotating_device_id.unique_id": true,
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

// sealForm is how a table keeps its secret columns.
type sealForm int

const (
	// sealBlob: a BLOB column, the sealed bytes under a sealer, the clear
	// value without one.
	sealBlob sealForm = iota
	// sealICD: as sealBlob under a sealer; without one the key sealed under
	// [SettingICDKey] (icdAEAD), or 16 clear bytes from before v0.12.1.
	sealICD
	// sealSettingText: matter_settings' TEXT value, the sealed bytes in
	// base64; only the keys [isSealedSetting] names are secrets.
	sealSettingText
)

// secretColumns is one table that holds a secret: the key columns of each
// value's aad ([sealAAD]), in order, and the secret columns.
type secretColumns struct {
	table   string
	keys    []string
	secrets []string
	form    sealForm
}

// secretTables is the one list of the secret columns: [NewSealed]'s schema
// check, its key check (the first secret column of each table, in order)
// and its upgrade, and [Reseal], iterate it; every sealColumn and
// openColumn call names one of its columns
// (TestSecretColumnsAreTheSealCallSites). The order is the upgrade's: the
// ICD keys before the settings, because a clear ICD row's key may be sealed
// under SettingICDKey, which is still a clear setting then. A secret
// added here ships with an upgrade step
// (TestSecretsAddedAfterV11ShipAnUpgradeStep, decision 157).
var secretTables = []secretColumns{
	{identityTable, []string{"fabric_index"}, []string{"private_key", "ipk", "prior_private_key"}, sealBlob},
	{"matter_group_keys", []string{"fabric_index", "group_key_set_id"}, groupEpochKeyColumns[:], sealBlob},
	{"matter_resumption", []string{"fabric_index", "peer_node_id"}, []string{"shared_secret"}, sealBlob},
	{doorLockCredentialTable, []string{"endpoint_key", "credential_type", "credential_index"}, []string{"credential_data"}, sealBlob},
	{"matter_icd_registrations", []string{"fabric_index", "check_in_node_id", "monitored_subject"}, []string{"icd_key"}, sealICD},
	{"matter_settings", []string{"key"}, []string{"value"}, sealSettingText},
}

// metadataKeySealScrubOwed marks a sealing whose [Scrub] has not finished:
// set in the sealing's (or [Reseal]'s) transaction, deleted once the scrub
// ran ([Store.ScrubOwed]).
const metadataKeySealScrubOwed = "store.seal_scrub_owed"

// settingSealCanary is the reserved matter_settings row that marks a
// sealed database, the seal canary (decision 130): one known value sealed under the
// sealer, with the aad of a setting of this key. The first [NewSealed]
// writes it, NewSealed opens it first, [Reseal] moves it last, [Store.Erase]
// drops it with every other row. The Settings methods do not write or
// delete it.
const settingSealCanary = "store.seal_canary"

// sealCanaryPlain is the canary's value before sealing.
var sealCanaryPlain = []byte("go-fabric/store/seal/canary/v1")

// readCanary reads the seal canary on q: present is false on a database
// without it.
func readCanary(ctx context.Context, q sqlExecQuerier) (stored string, sealed, present bool, err error) {
	err = q.QueryRowContext(ctx, `SELECT value, sealed FROM matter_settings WHERE key = ?`, settingSealCanary).Scan(&stored, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, fmt.Errorf("read the seal canary: %w", err)
	}
	return stored, sealed, true, nil
}

// openCanary opens the canary under the store's sealer: a canary not
// sealed, one that does not open and one whose value is not the canary's are
// [ErrUnseal].
func (s *Store) openCanary(stored string, sealed bool) error {
	if !sealed {
		return fmt.Errorf("%w: the seal canary is not sealed", ErrUnseal)
	}
	plain, err := s.openSetting(settingSealCanary, stored, true)
	if err != nil {
		return err
	}
	if plain != string(sealCanaryPlain) {
		return fmt.Errorf("%w: the seal canary holds another value", ErrUnseal)
	}
	return nil
}

// writeCanary writes the canary sealed under the store's sealer on tx.
func (s *Store) writeCanary(ctx context.Context, tx *sql.Tx) error {
	out, err := s.sealColumn(sealCanaryPlain, "matter_settings", "value", settingSealCanary)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO matter_settings (key, value, sealed) VALUES (?, ?, 1)`,
		settingSealCanary, base64.StdEncoding.EncodeToString(out)); err != nil {
		return fmt.Errorf("write the seal canary: %w", err)
	}
	return nil
}

// refuseClearSecretWrite is the check a Store without a sealer makes on q
// before it writes a secret in clear: a database that carries the canary
// refuses it with [ErrNoSealer]. A Store with a sealer passes.
func (s *Store) refuseClearSecretWrite(ctx context.Context, q sqlExecQuerier) error {
	if s.sealer != nil {
		return nil
	}
	_, _, marked, err := readCanary(ctx, q)
	if err != nil {
		return err
	}
	if marked {
		return fmt.Errorf("%w: the database is sealed", ErrNoSealer)
	}
	return nil
}

// clearSecretTable names the first table of secretTables that holds a
// secret row without the seal mark — a matter_settings row only for a key
// [isSealedSetting] names — or "" when none does.
func clearSecretTable(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
},
) (string, error) {
	for _, t := range secretTables {
		if t.form != sealSettingText {
			var one int
			err := q.QueryRowContext(ctx, `SELECT 1 FROM `+t.table+` WHERE sealed = 0 LIMIT 1`).Scan(&one) //nolint:gosec // the package's constants
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return "", fmt.Errorf("inspect %s: %w", t.table, err)
			}
			return t.table, nil
		}
		rows, err := q.QueryContext(ctx, `SELECT key FROM matter_settings WHERE sealed = 0`)
		if err != nil {
			return "", fmt.Errorf("inspect matter_settings: %w", err)
		}
		found := ""
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				_ = rows.Close()
				return "", err
			}
			if isSealedSetting(k) || k == settingSealCanary {
				found = fmt.Sprintf("matter_settings %q", k)
				break
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return "", err
		}
		if found != "" {
			return found, nil
		}
	}
	return "", nil
}

// sealClearRows is [NewSealed]'s open. On a database with the canary:
// the canary opened (the key check), a clear secret row refused, nothing
// written. On one without: the key check of openOneSealed, then every clear
// row sealed and the canary written in one transaction, which records the
// scrub owed when it sealed a row. Then, while a scrub is owed, the scrub;
// one that does not finish leaves the owed mark for the next open
// ([Store.ScrubOwed]) and is no error.
func (s *Store) sealClearRows(ctx context.Context) error {
	if err := checkSealMarks(ctx, s.db, "NewSealed"); err != nil {
		return err
	}
	_, _, marked, err := readCanary(ctx, s.db)
	if err != nil {
		return fmt.Errorf("store: NewSealed: %w", err)
	}
	if !marked {
		// The transaction reads the canary again: a NewSealed under another
		// key may have marked the database since.
		if marked, err = s.sealUnmarked(ctx); err != nil {
			return err
		}
	}
	if marked {
		if err := s.openMarked(ctx); err != nil {
			return err
		}
	}
	if owed, err := s.ScrubOwed(ctx); err != nil || !owed {
		return err
	}
	if err := Scrub(ctx, s.db); err != nil {
		// Owed, and visible through ScrubOwed: the rows are sealed, and a
		// full disk or a reader must not keep the node down (decision 132).
		return nil //nolint:nilerr // the owed scrub is a state, not a failure
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM matter_metadata WHERE key = ?`, metadataKeySealScrubOwed)
	return nil
}

// openMarked is NewSealed's open of a database that carries the canary:
// the canary opened under the sealer (the key check) and a clear secret
// row refused; nothing is written.
func (s *Store) openMarked(ctx context.Context) error {
	stored, markSealed, _, err := readCanary(ctx, s.db)
	if err != nil {
		return fmt.Errorf("store: NewSealed: %w", err)
	}
	if err := s.openCanary(stored, markSealed); err != nil {
		return fmt.Errorf("store: NewSealed: the sealer does not open the database's seal canary: %w", err)
	}
	if t, err := clearSecretTable(ctx, s.db); err != nil {
		return fmt.Errorf("store: NewSealed: %w", err)
	} else if t != "" {
		return fmt.Errorf("store: NewSealed: %w: a %s row is not sealed in a sealed database", ErrUnseal, t)
	}
	return nil
}

// sealUnmarked seals a database without the canary in one transaction that
// holds the write lock first: the canary read again (marked reports one a
// concurrent NewSealed wrote meanwhile, and nothing is written), the key
// check, every clear row sealed and the canary written.
func (s *Store) sealUnmarked(ctx context.Context) (marked bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: NewSealed: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// A write first, so the transaction holds the write lock before it
	// reads (UpdateFabricCredentials' reason).
	if _, err := tx.ExecContext(ctx, `UPDATE matter_metadata SET value = value WHERE key = ?`, metadataKeySealScrubOwed); err != nil {
		return false, fmt.Errorf("store: NewSealed: %w", err)
	}
	if _, _, marked, err := readCanary(ctx, tx); err != nil {
		return false, fmt.Errorf("store: NewSealed: %w", err)
	} else if marked {
		return true, nil
	}
	// The key check before the sealing: a wrong key must not seal the
	// clear rows beside values it does not open.
	if err := s.openOneSealed(ctx, tx); err != nil {
		return false, err
	}
	sealed := 0
	for _, t := range secretTables {
		var (
			n   int
			err error
		)
		switch t.form {
		case sealBlob:
			n, err = s.sealTable(ctx, tx, t)
		case sealICD:
			// sealClearICDRows reads SettingICDKey as a store without a
			// sealer: the settings come after it in secretTables.
			n, err = s.sealClearICDRows(ctx, tx)
		case sealSettingText:
			n, err = s.sealClearSettings(ctx, tx)
		}
		if err != nil {
			return false, fmt.Errorf("store: NewSealed: %s: %w", t.table, err)
		}
		sealed += n
	}
	if err := s.writeCanary(ctx, tx); err != nil {
		return false, fmt.Errorf("store: NewSealed: %w", err)
	}
	if sealed > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO matter_metadata (key, value) VALUES (?, 1)`, metadataKeySealScrubOwed); err != nil {
			return false, fmt.Errorf("store: NewSealed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: NewSealed: commit: %w", err)
	}
	return false, nil
}

// ScrubOwed reports whether the database owes the scrub of a sealing or a
// [Reseal]: the values are sealed, but the clear (or old-key) bytes they
// replaced may remain in the file's free pages, its -wal or a kept
// -journal until a [Scrub] of the file finishes — a full disk or a reader
// holding the -wal stops one. [NewSealed] retries the scrub on every open
// while it is owed and opens the Store either way; a host that wants to
// fail closed checks ScrubOwed after NewSealed and stops, and one that
// does not logs it (the node logs one warning).
func (s *Store) ScrubOwed(ctx context.Context) (bool, error) {
	_, owed, err := s.GetMetadataCounter(ctx, metadataKeySealScrubOwed)
	if err != nil {
		return false, fmt.Errorf("store: scrub owed: %w", err)
	}
	return owed, nil
}

// Reseal moves a sealed database from the sealer old to the sealer next —
// the rotation of the host's key — in one transaction: the seal canary
// opened under old (the key check), every sealed value of secretTables
// opened under old and sealed under next, the canary last; then the
// [Scrub] that removes the values sealed under old from the file, with
// the mark of an owed scrub until it finished. A wrong old key or a value
// that does not open under it is [ErrUnseal], and so is a secret row in
// clear (the host opens the database with [NewSealed] under old first,
// which seals it); either leaves the database as it was. Every error after
// the commit is [ErrScrubIncomplete]: the values are sealed under next,
// the scrub stays marked as owed ([Store.ScrubOwed]) and the next NewSealed
// runs it again.
//
// No Store may be open on db while Reseal runs, and no other process may
// write the file: a Store holds no sealer it could swap. db must carry
// schema v10. There is no unseal: the way back to a clear database is a
// restore or a factory reset. The rows of other modules' tables on the
// same file (the controller's node registry, controller/client/sqlstore)
// are not this store's and are not resealed: a controller's database
// moves with controller.Reseal, which reseals both stores in one
// transaction, and a registry alone with sqlstore.Reseal.
//
// The one transaction is atomic only with a journal: under
// journal_mode=OFF SQLite cannot roll it back, and a Reseal that fails
// part-way can leave the file half-moved. journal_mode=OFF is not
// supported (review II, R2-P6-doc-3007).
func Reseal(ctx context.Context, db *sql.DB, old, next Sealer) error {
	if old == nil || next == nil {
		return errors.New("store: Reseal: nil Sealer")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: Reseal: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := resealTx(ctx, tx, old, next); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: Reseal: commit: %w", err)
	}
	if err := Scrub(ctx, db); err != nil {
		return fmt.Errorf("store: Reseal: the values are sealed under the new sealer: %w", err)
	}
	if err := dropScrubOwed(ctx, db); err != nil {
		// The mark stays, so ScrubOwed still says owed and the next
		// NewSealed scrubs again: the same state as an unfinished scrub.
		return fmt.Errorf("store: Reseal: the values are sealed under the new sealer, the owed scrub stays marked: %w: %w", ErrScrubIncomplete, err)
	}
	return nil
}

func init() {
	sealseam.StoreResealTx = func(ctx context.Context, tx *sql.Tx, old, next sealseam.Sealer) error {
		return resealTx(ctx, tx, old, next)
	}
	sealseam.StoreDropScrubOwed = dropScrubOwed
}

// resealTx is [Reseal]'s transaction on tx, without the commit and the
// scrub: the schema check, the write lock, the seal canary opened under old
// (the key check), a clear secret row refused, every sealed value of
// secretTables opened under old and sealed under next, the canary last, and
// the owed scrub marked. controller.Reseal runs it beside the node
// registry's in one transaction ([sealseam.StoreResealTx]).
func resealTx(ctx context.Context, tx *sql.Tx, old, next Sealer) error {
	// A write first, so the transaction holds the write lock before it
	// reads, the schema check included (sealUnmarked's rule).
	if _, err := tx.ExecContext(ctx, `UPDATE matter_metadata SET value = value WHERE key = ?`, metadataKeySealScrubOwed); err != nil {
		return fmt.Errorf("store: Reseal: %w", err)
	}
	if err := checkSealMarks(ctx, tx, "Reseal"); err != nil {
		return err
	}
	from, to := &Store{sealer: old}, &Store{sealer: next}
	stored, markSealed, marked, err := readCanary(ctx, tx)
	if err != nil {
		return fmt.Errorf("store: Reseal: %w", err)
	}
	if marked {
		if err := from.openCanary(stored, markSealed); err != nil {
			return fmt.Errorf("store: Reseal: the old sealer does not open the database's seal canary: %w", err)
		}
	}
	if t, err := clearSecretTable(ctx, tx); err != nil {
		return fmt.Errorf("store: Reseal: %w", err)
	} else if t != "" {
		return fmt.Errorf("store: Reseal: %w: a %s row is not sealed (store.NewSealed seals it)", ErrUnseal, t)
	}
	for _, t := range secretTables {
		if err := resealTable(ctx, tx, from, to, t); err != nil {
			return fmt.Errorf("store: Reseal: %s: %w", t.table, err)
		}
	}
	if err := to.writeCanary(ctx, tx); err != nil {
		return fmt.Errorf("store: Reseal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO matter_metadata (key, value) VALUES (?, 1)`, metadataKeySealScrubOwed); err != nil {
		return fmt.Errorf("store: Reseal: %w", err)
	}
	return nil
}

// dropScrubOwed deletes the mark of an owed scrub once a [Scrub] finished.
func dropScrubOwed(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM matter_metadata WHERE key = ?`, metadataKeySealScrubOwed); err != nil {
		return fmt.Errorf("store: drop the owed scrub's mark: %w", err)
	}
	return nil
}

// resealTable opens every sealed value of one table under from and seals
// it under to, on tx; matter_settings' values in their base64 form. The
// canary is left to the caller.
func resealTable(ctx context.Context, tx *sql.Tx, from, to *Store, t secretColumns) error {
	where := ` WHERE sealed = 1`
	if t.form == sealSettingText {
		where += ` AND key <> '` + settingSealCanary + `'`
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+strings.Join(t.keys, ", ")+`, `+strings.Join(t.secrets, ", ")+ //nolint:gosec // the package's constants
		` FROM `+t.table+where)
	if err != nil {
		return err
	}
	type row struct {
		key  []any
		vals [][]byte
	}
	var held []row
	for rows.Next() {
		r := row{key: make([]any, len(t.keys)), vals: make([][]byte, len(t.secrets))}
		dest := make([]any, 0, len(t.keys)+len(t.secrets))
		for i := range r.key {
			dest = append(dest, &r.key[i])
		}
		for i := range r.vals {
			dest = append(dest, &r.vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			_ = rows.Close()
			return err
		}
		held = append(held, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	set := make([]string, 0, len(t.secrets))
	for _, c := range t.secrets {
		set = append(set, c+" = ?")
	}
	cond := make([]string, 0, len(t.keys))
	for _, k := range t.keys {
		cond = append(cond, k+" = ?")
	}
	update := `UPDATE ` + t.table + ` SET ` + strings.Join(set, ", ") + ` WHERE ` + strings.Join(cond, " AND ") //nolint:gosec // the package's constants
	for _, r := range held {
		args := make([]any, 0, len(t.secrets)+len(t.keys))
		for i, c := range t.secrets {
			v := r.vals[i]
			if v == nil {
				args = append(args, nil)
				continue
			}
			if t.form == sealSettingText {
				if v, err = base64.StdEncoding.DecodeString(string(v)); err != nil {
					return fmt.Errorf("%w: %s.%s: %w", ErrUnseal, t.table, c, err)
				}
			}
			plain, err := from.openColumn(v, true, t.table, c, r.key...)
			if err != nil {
				return err
			}
			out, err := to.sealColumn(plain, t.table, c, r.key...)
			if err != nil {
				return err
			}
			if t.form == sealSettingText {
				args = append(args, base64.StdEncoding.EncodeToString(out))
			} else {
				args = append(args, out)
			}
		}
		args = append(args, r.key...)
		if _, err := tx.ExecContext(ctx, update, args...); err != nil {
			return err
		}
	}
	return nil
}

// checkSealMarks is the schema check of [NewSealed] and [Reseal]: every
// table of secretTables carries the seal mark of migration v10.
func checkSealMarks(ctx context.Context, db sqlExecQuerier, op string) error {
	for _, t := range secretTables {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'sealed'`, t.table).Scan(&n); err != nil {
			return fmt.Errorf("store: %s: inspect %s: %w", op, t.table, err)
		}
		if n == 0 {
			return fmt.Errorf("store: %s: %s has no seal mark: the database needs schema v10 (store.Apply or store.Migrations)", op, t.table)
		}
	}
	return nil
}

// openOneSealed is [NewSealed]'s key check: it opens one sealed value the
// database already holds — the first sealed identity's private key, else
// the first sealed value of the other sealed tables, else the first sealed
// setting — so a sealer under another key, or a value changed outside the
// store, fails NewSealed with [ErrUnseal] instead of the first read later.
// A database with no sealed value passes. The key parts are read as the
// driver returns them, as sealTable seals them.
func (s *Store) openOneSealed(ctx context.Context, q sqlExecQuerier) error {
	for _, t := range secretTables {
		if t.form == sealSettingText {
			continue
		}
		p := struct {
			table, column string
			keys          []string
		}{t.table, t.secrets[0], t.keys}
		cols := strings.Join(p.keys, ", ")
		key := make([]any, len(p.keys))
		dest := make([]any, 0, len(key)+1)
		for i := range key {
			dest = append(dest, &key[i])
		}
		var val []byte
		dest = append(dest, &val)
		err := q.QueryRowContext(ctx, `SELECT `+cols+`, `+p.column+` FROM `+p.table+ //nolint:gosec // the package's constants
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
	err := q.QueryRowContext(ctx, `SELECT key, value FROM matter_settings WHERE sealed = 1 ORDER BY key LIMIT 1`).Scan(&key, &val)
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
// many rows it sealed. The table and the columns are secretTables'.
func (s *Store) sealTable(ctx context.Context, tx *sql.Tx, t secretColumns) (int, error) {
	table, keys, secrets := t.table, t.keys, t.secrets
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
