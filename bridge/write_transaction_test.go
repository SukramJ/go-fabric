// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
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

// TestWriteTransactionTTLIsIdleTime: the TTL counts from the last chunk,
// so a chunked write whose chunks arrive within it keeps its transaction —
// and with it the list-ADD marker — however long it runs in all.
func TestWriteTransactionTTLIsIdleTime(t *testing.T) {
	t.Parallel()
	var r exchangeRouting
	now := time.Unix(1000, 0)
	a := mrp.ExchangeKey{SessionID: 1, ExchangeID: 7}
	sweep := mrp.ExchangeKey{SessionID: 2, ExchangeID: 1}
	first := r.writeTransaction(a, true, now)
	step := writeTxTTL * 3 / 4
	for i := 1; i <= 3; i++ { // 2.25 TTLs in all, each gap within one
		at := now.Add(time.Duration(i) * step)
		r.writeTransaction(sweep, true, at)
		if got := r.writeTransaction(a, true, at); got != first {
			t.Fatalf("chunk %d after %v got a fresh transaction", i+1, time.Duration(i)*step)
		}
	}
}

// TestChunkedWriteCapIsPerSession: the bound on open chunked writes counts
// one session's writes only (R2-P6-test-0802) — a session at
// [maxChunkedWritesPerSession] does not take another session's places.
func TestChunkedWriteCapIsPerSession(t *testing.T) {
	t.Parallel()
	var r exchangeRouting
	now := time.Unix(1000, 0)
	for i := range uint16(maxChunkedWritesPerSession) {
		if r.writeTransaction(mrp.ExchangeKey{SessionID: 1, ExchangeID: 10 + i}, true, now) == nil {
			t.Fatalf("chunked write %d of session 1 refused", i)
		}
	}
	if r.writeTransaction(mrp.ExchangeKey{SessionID: 1, ExchangeID: 99}, true, now) != nil {
		t.Fatal("session 1 opened one more than its bound")
	}
	if r.writeTransaction(mrp.ExchangeKey{SessionID: 2, ExchangeID: 10}, true, now) == nil {
		t.Fatal("session 2's first chunked write refused while session 1 is at its bound")
	}
}

// TestSessionCloseEndsOnlyItsChunkedWrites: dropping a session's chunked
// writes leaves another session's open writes in the table
// (R2-P6-test-0802).
func TestSessionCloseEndsOnlyItsChunkedWrites(t *testing.T) {
	t.Parallel()
	var r exchangeRouting
	now := time.Unix(1000, 0)
	a := []mrp.ExchangeKey{{SessionID: 1, ExchangeID: 10}, {SessionID: 1, ExchangeID: 11}}
	b := []mrp.ExchangeKey{{SessionID: 2, ExchangeID: 10}, {SessionID: 2, ExchangeID: 11}}
	held := map[mrp.ExchangeKey]*im.WriteTransaction{}
	for _, k := range append(append([]mrp.ExchangeKey(nil), a...), b...) {
		tx := r.writeTransaction(k, true, now)
		if tx == nil {
			t.Fatalf("%+v refused", k)
		}
		held[k] = tx
	}
	r.dropSessionWriteTxs(1)
	for _, k := range a {
		if _, ok := r.writeTxs.Load(k); ok {
			t.Errorf("%+v survived its session's close", k)
		}
	}
	for _, k := range b {
		if r.writeTransaction(k, true, now) != held[k] {
			t.Errorf("%+v lost its transaction to another session's close", k)
		}
	}
}

// TestWriteTransactionKeptAtExactlyItsTTL: a chunked write idle for exactly
// writeTxTTL keeps its transaction; one idle a moment longer loses it
// (R2-P6-bound-0813).
func TestWriteTransactionKeptAtExactlyItsTTL(t *testing.T) {
	t.Parallel()
	var r exchangeRouting
	now := time.Unix(1000, 0)
	a := mrp.ExchangeKey{SessionID: 1, ExchangeID: 7}
	sweep := mrp.ExchangeKey{SessionID: 2, ExchangeID: 1}
	r.writeTransaction(a, true, now)
	r.writeTransaction(sweep, true, now.Add(writeTxTTL))
	if _, ok := r.writeTxs.Load(a); !ok {
		t.Fatal("a chunked write idle for exactly writeTxTTL was pruned")
	}
	r.writeTransaction(sweep, true, now.Add(writeTxTTL+time.Nanosecond))
	if _, ok := r.writeTxs.Load(a); ok {
		t.Fatal("a chunked write idle past writeTxTTL was kept")
	}
}
