// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestWriteTransactionSpansTheChunksOfOneWrite pins the per-exchange write
// transaction: the chunks of one chunked write share it, the final chunk
// takes it out of the table, another exchange gets its own, and an
// abandoned one is pruned after writeTxTTL.
func TestWriteTransactionSpansTheChunksOfOneWrite(t *testing.T) {
	t.Parallel()
	var r exchangeRouting
	now := time.Unix(1000, 0)
	a := mrp.ExchangeKey{SessionID: 1, ExchangeID: 7}
	b := mrp.ExchangeKey{SessionID: 1, ExchangeID: 8}

	first := r.writeTransaction(a, true, now)
	if r.writeTransaction(a, true, now) != first {
		t.Fatal("a second chunk got a different transaction")
	}
	if r.writeTransaction(b, true, now) == first {
		t.Fatal("another exchange shares the transaction")
	}
	if r.writeTransaction(a, false, now) != first {
		t.Fatal("the final chunk got a different transaction")
	}
	if _, ok := r.writeTxs.Load(a); ok {
		t.Fatal("the final chunk left the transaction in the table")
	}
	if r.writeTransaction(a, false, now) == first {
		t.Fatal("a new single-message write reused a finished transaction")
	}
	r.writeTransaction(mrp.ExchangeKey{SessionID: 2, ExchangeID: 1}, true, now.Add(2*writeTxTTL))
	if _, ok := r.writeTxs.Load(b); ok {
		t.Fatal("an abandoned chunked write was not pruned")
	}
}
