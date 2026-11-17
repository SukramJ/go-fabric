// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestChunkStoredAfterTheSessionClosedHoldsNothing (N-req3-1): a chunk
// that announces more and reaches its transaction after its session's
// close dropped the table — so it stores a fresh entry — gives back its
// transaction and holds when the chunk ends, not at a later write's sweep.
func TestChunkStoredAfterTheSessionClosedHoldsNothing(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	const sid, other = 0x77, 0x78
	b.mu.Lock()
	b.sessions = sessionLookupFunc(func(id uint16) (*channel.Session, bool) { return nil, id != sid })
	b.mu.Unlock()
	now := b.clk().Now()
	p := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}

	b.SessionClosed(sid)
	late := mrp.ExchangeKey{SessionID: sid, ExchangeID: 1}
	tx := b.routing.writeTransaction(late, true, now)
	if tx == nil {
		t.Fatal("no transaction")
	}
	im.HandleWriteRequest(im.WithWriteTransaction(im.WithAuthModePASE(t.Context()), tx), holdDispatcher{}, im.WriteRequest{
		MoreChunkedMessages: true,
		Writes:              []im.AttributeWrite{{Path: p, Value: im.AttributeValue{Value: "x"}}},
	})
	b.endWriteChunk(late, tx, true)

	if n := b.routing.openWriteTxs(sid); n != 0 {
		t.Errorf("the closed session keeps %d chunked writes", n)
	}
	otherTx := b.routing.writeTransaction(mrp.ExchangeKey{SessionID: other, ExchangeID: 2}, false, now)
	resp := im.HandleWriteRequest(im.WithWriteTransaction(im.WithAuthModePASE(t.Context()), otherTx), holdDispatcher{}, im.WriteRequest{
		Writes: []im.AttributeWrite{{Path: p, Value: im.AttributeValue{Value: "x"}}},
	})
	if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != im.StatusSuccess {
		t.Errorf("another session's write answered %+v, want SUCCESS (the closed session's hold released)", resp.Responses)
	}
}
