// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"encoding/binary"
	"fmt"
)

// OTAProviderLocation is one DefaultOTAProviders entry of the OTA Software
// Update Requestor (the ProviderLocation struct).
type OTAProviderLocation struct {
	FabricIndex    uint8
	ProviderNodeID uint64
	Endpoint       uint16
}

// otaRequestorStateKey is the settings key of the requestor's other
// nonvolatile state: matter.js persists activeOtaProviders and
// updateInProgressDetails beside the cluster's attributes
// (OtaSoftwareUpdateRequestorServer.ts:1354, :1362, "This value is
// persisted"). The value is the requestor's own encoding.
const otaRequestorStateKey = "ota_requestor.state"

// ListDefaultOTAProviders returns every fabric's DefaultOTAProviders
// entries, by fabric and then in the order they were written.
func (s *Store) ListDefaultOTAProviders(ctx context.Context) ([]OTAProviderLocation, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT fabric_index, provider_node_id, endpoint FROM matter_ota_default_providers
ORDER BY fabric_index ASC, position ASC`)
	if err != nil {
		return nil, fmt.Errorf("matter store: list default ota providers: %w", err)
	}
	defer rows.Close() //nolint:errcheck // cleanup-only; the scan error is the actionable one

	var out []OTAProviderLocation
	for rows.Next() {
		var (
			e    OTAProviderLocation
			node []byte
		)
		if err := rows.Scan(&e.FabricIndex, &node, &e.Endpoint); err != nil {
			return nil, fmt.Errorf("matter store: list default ota providers: scan: %w", err)
		}
		if len(node) != 8 {
			return nil, fmt.Errorf("matter store: list default ota providers: node id of %d bytes", len(node))
		}
		e.ProviderNodeID = binary.BigEndian.Uint64(node)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matter store: list default ota providers: rows: %w", err)
	}
	return out, nil
}

// ReplaceDefaultOTAProviders replaces fabricIndex's DefaultOTAProviders
// entries with entries, in order — the fabric-scoped list write, which
// leaves every other fabric's entries as they are. The entries' FabricIndex
// is ignored: they belong to fabricIndex.
func (s *Store) ReplaceDefaultOTAProviders(ctx context.Context, fabricIndex uint8, entries []OTAProviderLocation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("matter store: replace default ota providers: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM matter_ota_default_providers WHERE fabric_index = ?`, fabricIndex); err != nil {
		return fmt.Errorf("matter store: replace default ota providers: delete: %w", err)
	}
	for i, e := range entries {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO matter_ota_default_providers (fabric_index, position, provider_node_id, endpoint)
VALUES (?, ?, ?, ?)`, fabricIndex, i, uint64ToBE(e.ProviderNodeID), e.Endpoint); err != nil {
			return fmt.Errorf("matter store: replace default ota providers: insert %d: %w", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("matter store: replace default ota providers: commit: %w", err)
	}
	return nil
}

// LoadOTARequestorState returns what SaveOTARequestorState stored last; ok
// is false when nothing was stored.
func (s *Store) LoadOTARequestorState(ctx context.Context) (state []byte, ok bool, err error) {
	v, ok, err := s.GetSetting(ctx, otaRequestorStateKey)
	if err != nil || !ok {
		return nil, ok, err
	}
	return []byte(v), true, nil
}

// SaveOTARequestorState stores the requestor's encoded state.
func (s *Store) SaveOTARequestorState(ctx context.Context, state []byte) error {
	return s.SetSetting(ctx, otaRequestorStateKey, string(state))
}
