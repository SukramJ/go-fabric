// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"encoding/binary"
	"fmt"
)

// ICDRegistration is one registered Check-In client of an ICD node: the
// RegisteredClients entry of the ICD Management cluster (quality "N",
// fabric-scoped) together with its ICDToken key, which matter.js keeps
// beside the attribute in the server's nonvolatile icdKeys
// (IcdManagementServer.ts:777-791).
type ICDRegistration struct {
	FabricIndex      uint8
	CheckInNodeID    uint64
	MonitoredSubject uint64
	// Key is the 16-byte ICDToken the client registered.
	Key []byte
	// ClientType is ClientTypeEnum: 0 Permanent, 1 Ephemeral.
	ClientType uint8
}

// metadataKeyICDCounter is the matter_metadata key of the ICDCounter
// attribute (quality "N C", icd-management.element.ts:52).
const metadataKeyICDCounter = "icd_management.counter"

// ListICDRegistrations returns every fabric's registrations, by fabric and
// then in the order they first registered.
func (s *Store) ListICDRegistrations(ctx context.Context) ([]ICDRegistration, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT fabric_index, check_in_node_id, monitored_subject, icd_key, client_type FROM matter_icd_registrations
ORDER BY fabric_index ASC, position ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list icd registrations: %w", err)
	}
	defer rows.Close() //nolint:errcheck // cleanup-only; the scan error is the actionable one

	var out []ICDRegistration
	for rows.Next() {
		var (
			r             ICDRegistration
			node, subject []byte
		)
		if err := rows.Scan(&r.FabricIndex, &node, &subject, &r.Key, &r.ClientType); err != nil {
			return nil, fmt.Errorf("store: list icd registrations: scan: %w", err)
		}
		if len(node) != 8 || len(subject) != 8 {
			return nil, fmt.Errorf("store: list icd registrations: node id of %d, subject of %d bytes", len(node), len(subject))
		}
		r.CheckInNodeID = binary.BigEndian.Uint64(node)
		r.MonitoredSubject = binary.BigEndian.Uint64(subject)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list icd registrations: rows: %w", err)
	}
	return out, nil
}

// ReplaceICDRegistrations replaces fabricIndex's registrations with regs,
// in order, leaving every other fabric's as they are. The entries'
// FabricIndex is ignored: they belong to fabricIndex. A removed fabric
// takes its registrations along.
func (s *Store) ReplaceICDRegistrations(ctx context.Context, fabricIndex uint8, regs []ICDRegistration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: replace icd registrations: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM matter_icd_registrations WHERE fabric_index = ?`, fabricIndex); err != nil {
		return fmt.Errorf("store: replace icd registrations: delete: %w", err)
	}
	for i, r := range regs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO matter_icd_registrations (fabric_index, position, check_in_node_id, monitored_subject, icd_key, client_type)
VALUES (?, ?, ?, ?, ?, ?)`, fabricIndex, i, uint64ToBE(r.CheckInNodeID), uint64ToBE(r.MonitoredSubject), r.Key, r.ClientType); err != nil {
			return fmt.Errorf("store: replace icd registrations: insert %d: %w", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: replace icd registrations: commit: %w", err)
	}
	return nil
}

// LoadICDCounter returns the ICDCounter SaveICDCounter stored last; ok is
// false before the first save.
func (s *Store) LoadICDCounter(ctx context.Context) (counter uint32, ok bool, err error) {
	v, ok, err := s.GetMetadataCounter(ctx, metadataKeyICDCounter)
	if err != nil || !ok {
		return 0, ok, err
	}
	return uint32(v), true, nil //nolint:gosec // G115: SaveICDCounter stores a uint32
}

// SaveICDCounter stores the ICDCounter.
func (s *Store) SaveICDCounter(ctx context.Context, counter uint32) error {
	return s.SetMetadataCounter(ctx, metadataKeyICDCounter, uint64(counter))
}
