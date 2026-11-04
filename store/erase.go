// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"fmt"
)

// erasedTables are the matter_* tables of the schema (store/schema.sql),
// every one the factory reset empties.
var erasedTables = []string{
	"matter_fabrics",
	"matter_node_identities",
	"matter_group_keys",
	"matter_group_key_map",
	"matter_group_table",
	"matter_groupcast_groups",
	"matter_acl_entries",
	"matter_resumption",
	"matter_diagnostics",
	"matter_metadata",
	"matter_settings",
	"matter_persistent_subscriptions",
	"matter_server_subscriptions",
	"matter_ota_default_providers",
	"matter_thermostat_presets",
	"matter_doorlock_users",
	"matter_doorlock_credentials",
	"matter_doorlock_schedules",
	"matter_icd_registrations",
}

// Erase empties every matter_* table in one transaction and seeds the
// next fabric index as the schema does ([Eraser]).
func (s *Store) Erase(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: erase: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range erasedTables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil { //nolint:gosec // table names are the package's constants
			return fmt.Errorf("store: erase %s: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO matter_metadata (key, value) VALUES ('next_fabric_index', 1)`); err != nil {
		return fmt.Errorf("store: erase: seed next fabric index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: erase: %w", err)
	}
	return nil
}
