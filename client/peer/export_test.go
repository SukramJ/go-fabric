// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"log/slog"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// Probe exposes probe to the external test package, whose node proofs
// commission through bridge/bridgetest — which imports this package, so
// they cannot live in it (brief AJ §5).
func Probe(ctx context.Context, mgr *exchange.Manager, s *exchange.Session) bool {
	return probe(ctx, mgr, s, nil, slog.New(slog.DiscardHandler), nil, nil)
}
