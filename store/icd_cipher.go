// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

// SettingICDKey holds the key the ICD registrations' ICDToken keys are
// encrypted with at rest: 16 random bytes, hex-encoded, generated on the
// first registration a store keeps and read back afterwards — the
// DoorLock credentials' scheme ([SettingDoorLockCredentialKey],
// cluster/lock NewCredentialCipher: AES-128-GCM, a random nonce ahead of
// the sealed data). matter.js keeps the ICD keys as nonvolatile behaviour
// state in clear (IcdManagementServer.ts:787-791 at 862ad75f); the store
// seals them (ADR 0029). In memory, [ICDRegistration].Key stays clear.
const SettingICDKey = "icd_management.key" //nolint:gosec // the name of a settings key, not a credential

// icdKeySize is the ICDToken's and the at-rest key's size.
const icdKeySize = 16

// errICDKeySealed reports a stored ICD key the store's key does not open.
var errICDKeySealed = errors.New("store: icd key does not decrypt")

// sqlExecQuerier is what the ICD cipher reads and writes the setting on:
// the database or a transaction.
type sqlExecQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// icdAEAD is the cipher of the ICD keys, under the key [SettingICDKey]
// holds — created there when none is stored yet (INSERT OR IGNORE, then
// read back, so two writers agree on one key).
func icdAEAD(ctx context.Context, q sqlExecQuerier) (cipher.AEAD, error) {
	read := func() (string, bool, error) {
		var v string
		err := q.QueryRowContext(ctx, `SELECT value FROM matter_settings WHERE key = ?`, SettingICDKey).Scan(&v)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return v, err == nil, err
	}
	v, ok, err := read()
	if err != nil {
		return nil, fmt.Errorf("store: icd key: %w", err)
	}
	if !ok {
		key := make([]byte, icdKeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("store: icd key: %w", err)
		}
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO matter_settings (key, value) VALUES (?, ?)`, SettingICDKey, hex.EncodeToString(key)); err != nil {
			return nil, fmt.Errorf("store: icd key: %w", err)
		}
		if v, ok, err = read(); err != nil || !ok {
			return nil, fmt.Errorf("store: icd key: not stored: %w", err)
		}
	}
	key, err := hex.DecodeString(v)
	if err != nil || len(key) != icdKeySize {
		return nil, fmt.Errorf("store: icd key: %q is not %d hex bytes", SettingICDKey, icdKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("store: icd key: %w", err)
	}
	return cipher.NewGCM(block)
}

// icdAAD binds a sealed key to its registration: the fabric, the check-in
// node and the monitored subject, so a sealed key moved to another row
// does not open.
func icdAAD(fabricIndex uint8, checkInNodeID, monitoredSubject uint64) []byte {
	b := make([]byte, 17)
	b[0] = fabricIndex
	binary.BigEndian.PutUint64(b[1:], checkInNodeID)
	binary.BigEndian.PutUint64(b[9:], monitoredSubject)
	return b
}

// sealICDKey is the at-rest form of an ICDToken key: the nonce, then the
// sealed key.
func sealICDKey(aead cipher.AEAD, key, aad []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(key)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("store: icd key nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, key, aad), nil
}

// openICDKey is the ICDToken key of a stored value: a sealed one opened,
// or a 16-byte clear one as a database before the sealing kept it
// ([Upgrade] seals those).
func openICDKey(aead cipher.AEAD, stored, aad []byte) ([]byte, error) {
	if len(stored) == icdKeySize {
		return stored, nil
	}
	n := aead.NonceSize()
	if len(stored) < n+aead.Overhead() {
		return nil, errICDKeySealed
	}
	key, err := aead.Open(nil, stored[:n], stored[n:], aad)
	if err != nil {
		return nil, errICDKeySealed
	}
	return key, nil
}

// sealClearICDKeys seals the ICD keys an earlier release stored in clear —
// the rows whose icd_key is 16 bytes — in one transaction: [Upgrade]'s
// step for the sealing. A database without such rows is not touched (no
// key is created for it).
func sealClearICDKeys(ctx context.Context, db *sql.DB) error {
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'matter_icd_registrations'`).Scan(&tables); err != nil {
		return fmt.Errorf("store: seal icd keys: %w", err)
	}
	if tables == 0 {
		return nil // a database before migration v7 holds no ICD keys
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matter_icd_registrations WHERE length(icd_key) = ?`, icdKeySize).Scan(&n); err != nil {
		return fmt.Errorf("store: seal icd keys: %w", err)
	}
	if n == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: seal icd keys: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	aead, err := icdAEAD(ctx, tx)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT fabric_index, position, check_in_node_id, monitored_subject, icd_key FROM matter_icd_registrations
WHERE length(icd_key) = ?`, icdKeySize)
	if err != nil {
		return fmt.Errorf("store: seal icd keys: %w", err)
	}
	type row struct {
		fabric        uint8
		position      int
		node, subject []byte
		key           []byte
	}
	var clear []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.fabric, &r.position, &r.node, &r.subject, &r.key); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: seal icd keys: scan: %w", err)
		}
		clear = append(clear, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("store: seal icd keys: %w", err)
	}
	for _, r := range clear {
		if len(r.node) != 8 || len(r.subject) != 8 {
			return fmt.Errorf("store: seal icd keys: node id of %d, subject of %d bytes", len(r.node), len(r.subject))
		}
		sealed, err := sealICDKey(aead, r.key, icdAAD(r.fabric, binary.BigEndian.Uint64(r.node), binary.BigEndian.Uint64(r.subject)))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE matter_icd_registrations SET icd_key = ? WHERE fabric_index = ? AND position = ?`, sealed, r.fabric, r.position); err != nil {
			return fmt.Errorf("store: seal icd keys: update: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: seal icd keys: commit: %w", err)
	}
	return nil
}
