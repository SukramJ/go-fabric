// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
)

// leaveSessionOrder releases the turn the dispatch context carries; the
// order key itself is message.SessionOrderKey (transport/message).
func TestLeaveSessionOrder(t *testing.T) {
	t.Parallel()
	released := false
	ctx := context.WithValue(context.Background(), sessionTurnKey{}, func() { released = true })
	leaveSessionOrder(ctx)
	leaveSessionOrder(context.Background())
	if !released {
		t.Error("leaveSessionOrder did not release the turn")
	}
}
