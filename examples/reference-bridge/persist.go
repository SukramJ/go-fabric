// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"strconv"
)

// The node opens the daemon's SQLite file itself (node.Store.Path: the
// pure-Go modernc.org/sqlite driver, foreign keys on — the module's
// cascade-delete on fabric removal is a schema-level FOREIGN KEY — and a
// busy timeout, so a contended endpoint-id allocation waits rather than
// fails) and applies both schemas on every boot. What stays here is the
// daemon's own state in the node's settings table.

// settings is the slice of the node's store (node.Node.Store) the daemon
// keeps its own values in.
type settings interface {
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
}

// blindPositionKey is the settings key the blind's lift position is kept
// under.
const blindPositionKey = "blind.position"

// restoreBlind seeds the blind's position from the settings table and
// persists every position it reaches. Call it before the blind's server is
// built (fleet.endpoints): the server starts at the seeded position.
func restoreBlind(ctx context.Context, st settings, b *demoBlind, logger *slog.Logger) {
	if v, ok, err := st.GetSetting(ctx, blindPositionKey); err == nil && ok {
		if pos, perr := strconv.ParseUint(v, 10, 16); perr == nil && pos <= 10000 {
			b.position = uint16(pos)
		}
	}
	b.persist = func(pos uint16) {
		if err := st.SetSetting(context.WithoutCancel(ctx), blindPositionKey, strconv.FormatUint(uint64(pos), 10)); err != nil {
			logger.Warn("blind.persist", slog.Any("err", err))
		}
	}
}
