// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// GroupTableEntry is one row of the group table: the endpoints of this
// node that are members of GroupID on FabricIndex, and the group's name
// (Matter §11.2.6.2 GroupInfoMapStruct). Endpoints keeps the order the
// endpoints joined in.
type GroupTableEntry struct {
	FabricIndex uint8
	GroupID     uint16
	GroupName   string
	Endpoints   []uint16
}

// UpsertGroupTableEntry inserts or replaces the row of (FabricIndex,
// GroupID). The fabric must exist (FK). An entry without endpoints is a
// caller error — a group whose last endpoint left is removed instead,
// as matter.js removeEndpoint does.
func (s *Store) UpsertGroupTableEntry(ctx context.Context, e GroupTableEntry) error {
	if len(e.Endpoints) == 0 {
		return fmt.Errorf("matter store: group table entry %d/%d has no endpoints", e.FabricIndex, e.GroupID)
	}
	eps, err := json.Marshal(e.Endpoints)
	if err != nil {
		return fmt.Errorf("matter store: encode group endpoints: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO matter_group_table (fabric_index, group_id, group_name, endpoints_json)
VALUES (?, ?, ?, ?)
ON CONFLICT(fabric_index, group_id) DO UPDATE SET
    group_name     = excluded.group_name,
    endpoints_json = excluded.endpoints_json`,
		e.FabricIndex, e.GroupID, e.GroupName, string(eps)); err != nil {
		return fmt.Errorf("matter store: upsert group table entry: %w", err)
	}
	return nil
}

// RemoveGroupTableEntry deletes the row of (fabricIndex, groupID). A
// missing row is not an error.
func (s *Store) RemoveGroupTableEntry(ctx context.Context, fabricIndex uint8, groupID uint16) error {
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM matter_group_table WHERE fabric_index = ? AND group_id = ?`,
		fabricIndex, groupID); err != nil {
		return fmt.Errorf("matter store: remove group table entry: %w", err)
	}
	return nil
}

// ListGroupTable returns every group table row of fabricIndex ordered by
// group id ascending.
func (s *Store) ListGroupTable(ctx context.Context, fabricIndex uint8) ([]GroupTableEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT fabric_index, group_id, group_name, endpoints_json
FROM matter_group_table WHERE fabric_index = ?
ORDER BY group_id ASC`, fabricIndex)
	if err != nil {
		return nil, fmt.Errorf("matter store: list group table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []GroupTableEntry
	for rows.Next() {
		var (
			e   GroupTableEntry
			eps string
		)
		if err := rows.Scan(&e.FabricIndex, &e.GroupID, &e.GroupName, &eps); err != nil {
			return nil, fmt.Errorf("matter store: list group table: scan: %w", err)
		}
		if err := json.Unmarshal([]byte(eps), &e.Endpoints); err != nil {
			return nil, fmt.Errorf("matter store: list group table: decode endpoints of %d/%d: %w", e.FabricIndex, e.GroupID, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matter store: list group table: rows: %w", err)
	}
	return out, nil
}
