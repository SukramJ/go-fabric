// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"fmt"
)

// The server-subscription methods below give the bridge somewhere durable
// to keep the subscriptions of its CASE sessions, so it can re-establish
// them under their old SubscriptionId after a restart (docs/adr/0008,
// mirroring matter.js SubscriptionsServer's persisted state). The payload
// is the bridge's encoding of one subscription and is opaque here; the
// signatures use only standard types so *Store satisfies the bridge's
// SubscriptionStore port without either package importing the other.

// SaveServerSubscription inserts or replaces the row of subscriptionID.
func (s *Store) SaveServerSubscription(ctx context.Context, subscriptionID uint32, fabricIndex uint8, peerNodeID uint64, payload []byte) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO matter_server_subscriptions (subscription_id, fabric_index, peer_node_id, payload, updated_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(subscription_id) DO UPDATE SET
    fabric_index = excluded.fabric_index,
    peer_node_id = excluded.peer_node_id,
    payload      = excluded.payload,
    updated_at   = CURRENT_TIMESTAMP`,
		int64(subscriptionID), fabricIndex, uint64ToBE(peerNodeID), payload)
	if err != nil {
		return fmt.Errorf("matter store: save server subscription: %w", err)
	}
	return nil
}

// DeleteServerSubscription removes the row of subscriptionID. Idempotent.
func (s *Store) DeleteServerSubscription(ctx context.Context, subscriptionID uint32) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM matter_server_subscriptions WHERE subscription_id = ?`, int64(subscriptionID)); err != nil {
		return fmt.Errorf("matter store: delete server subscription: %w", err)
	}
	return nil
}

// DeleteServerSubscriptionsByFabric removes every row of fabricIndex.
// Called when the fabric is removed.
func (s *Store) DeleteServerSubscriptionsByFabric(ctx context.Context, fabricIndex uint8) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM matter_server_subscriptions WHERE fabric_index = ?`, fabricIndex); err != nil {
		return fmt.Errorf("matter store: delete server subscriptions by fabric: %w", err)
	}
	return nil
}

// LoadServerSubscriptions returns every row's payload, oldest first.
func (s *Store) LoadServerSubscriptions(ctx context.Context) ([][]byte, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT payload FROM matter_server_subscriptions ORDER BY updated_at ASC, subscription_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("matter store: load server subscriptions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // cleanup-only; the scan error is the actionable one

	var out [][]byte
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("matter store: load server subscriptions: scan: %w", err)
		}
		out = append(out, payload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matter store: load server subscriptions: rows: %w", err)
	}
	return out, nil
}

// ClearServerSubscriptions removes every row. The bridge calls it once it
// has loaded the former subscriptions at start-up — matter.js
// SubscriptionsServer.beginRun starts each run's record from empty.
func (s *Store) ClearServerSubscriptions(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM matter_server_subscriptions`); err != nil {
		return fmt.Errorf("matter store: clear server subscriptions: %w", err)
	}
	return nil
}
