// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"
)

// panickingConnector fails a query by panicking.
type panickingConnector struct{}

func (panickingConnector) ConnectOtaProvider(context.Context, uint8, uint64) (OtaProviderSession, error) {
	panic("connector panics")
}

// TestOTARequestor_QueryRunningClearedOnPanic: a query that panics still
// clears queryRunning, so the next timer queries again — matter.js clears
// it in a finally block (OtaSoftwareUpdateRequestorServer.ts:576-581 at
// 0ceb202d).
func TestOTARequestor_QueryRunningClearedOnPanic(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, func(c *OTARequestorConfig) { c.Connector = panickingConnector{} })
	h.addActive(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the query did not panic")
			}
		}()
		h.r.performUpdateQuery(context.Background())
	}()
	h.r.mu.Lock()
	running := h.r.queryRunning
	h.r.mu.Unlock()
	if running {
		t.Fatal("queryRunning is still set after the query panicked")
	}
}
