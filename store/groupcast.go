// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"fmt"
)

// GroupcastGroup is one row of matter_groupcast_groups: the Groupcast
// cluster's properties of one group of a fabric. Mirrors matter.js
// GroupcastServer GroupPropertiesStruct (GroupId, McastAddrPolicy,
// HasAuxiliaryAcl, FabricIndex).
type GroupcastGroup struct {
	FabricIndex uint8
	GroupID     uint16
	// McastAddrPolicy is MulticastAddrPolicyEnum: 0 IanaAddr, 1 PerGroup.
	McastAddrPolicy uint8
	// HasAuxiliaryACL reports whether the group's listener endpoints carry
	// an auxiliary access control entry.
	HasAuxiliaryACL bool
}

// UpsertGroupcastGroup inserts or replaces the row of (FabricIndex,
// GroupID). The fabric must exist (FK).
func (s *Store) UpsertGroupcastGroup(ctx context.Context, g GroupcastGroup) error {
	aux := 0
	if g.HasAuxiliaryACL {
		aux = 1
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO matter_groupcast_groups (fabric_index, group_id, mcast_addr_policy, has_auxiliary_acl)
VALUES (?, ?, ?, ?)
ON CONFLICT(fabric_index, group_id) DO UPDATE SET
    mcast_addr_policy = excluded.mcast_addr_policy,
    has_auxiliary_acl = excluded.has_auxiliary_acl`,
		g.FabricIndex, g.GroupID, g.McastAddrPolicy, aux); err != nil {
		return fmt.Errorf("matter store: upsert groupcast group: %w", err)
	}
	return nil
}

// RemoveGroupcastGroup deletes the row of (fabricIndex, groupID). A
// missing row is not an error.
func (s *Store) RemoveGroupcastGroup(ctx context.Context, fabricIndex uint8, groupID uint16) error {
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM matter_groupcast_groups WHERE fabric_index = ? AND group_id = ?`,
		fabricIndex, groupID); err != nil {
		return fmt.Errorf("matter store: remove groupcast group: %w", err)
	}
	return nil
}

// ListGroupcastGroups returns every row of fabricIndex ordered by group id.
func (s *Store) ListGroupcastGroups(ctx context.Context, fabricIndex uint8) ([]GroupcastGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT fabric_index, group_id, mcast_addr_policy, has_auxiliary_acl
FROM matter_groupcast_groups WHERE fabric_index = ?
ORDER BY group_id ASC`, fabricIndex)
	if err != nil {
		return nil, fmt.Errorf("matter store: list groupcast groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []GroupcastGroup
	for rows.Next() {
		var (
			g   GroupcastGroup
			aux int
		)
		if err := rows.Scan(&g.FabricIndex, &g.GroupID, &g.McastAddrPolicy, &aux); err != nil {
			return nil, fmt.Errorf("matter store: list groupcast groups: scan: %w", err)
		}
		g.HasAuxiliaryACL = aux != 0
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matter store: list groupcast groups: rows: %w", err)
	}
	return out, nil
}
