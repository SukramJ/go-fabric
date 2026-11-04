// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// holdDispatcher accepts every write.
type holdDispatcher struct{}

func (holdDispatcher) Read(context.Context, im.ConcreteAttributePath) []im.ReadResult { return nil }

func (holdDispatcher) Write(_ context.Context, p im.ConcreteAttributePath, _ im.AttributeValue) []im.WriteResult {
	return []im.WriteResult{{Path: p, Status: im.StatusSuccess}}
}

func (holdDispatcher) Invoke(context.Context, im.ConcreteCommandPath, any) im.InvokeResult {
	return im.InvokeResult{}
}

// TestWriteTransactionsShareHolds holds the bridge's side of matter.js's
// #writingExchanges (InteractionServer.ts:180, :505-508): the write
// transactions of two exchanges share one hold table, so a chunked write on
// one exchange answers the other's write of its attribute BUSY, and a
// chunked write abandoned past writeTxTTL releases what it held.
func TestWriteTransactionsShareHolds(t *testing.T) {
	t.Parallel()
	var r exchangeRouting
	nodeLabel := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	write := func(tx *im.WriteTransaction, more bool) im.StatusCode {
		resp := im.HandleWriteRequest(im.WithWriteTransaction(context.Background(), tx), holdDispatcher{}, im.WriteRequest{
			MoreChunkedMessages: more,
			Writes:              []im.AttributeWrite{{Path: nodeLabel, Value: im.AttributeValue{Value: "x"}}},
		})
		if len(resp.Responses) != 1 {
			t.Fatalf("responses %+v, want one", resp.Responses)
		}
		return resp.Responses[0].Status.Status
	}
	now := time.Now()
	holder := mrp.ExchangeKey{SessionID: 1, ExchangeID: 10}
	other := mrp.ExchangeKey{SessionID: 2, ExchangeID: 10}
	if s := write(r.writeTransaction(holder, true, now), true); s != im.StatusSuccess {
		t.Fatalf("first chunk %v", s)
	}
	if s := write(r.writeTransaction(other, false, now), false); s != im.StatusBusy {
		t.Fatalf("competing write %v, want BUSY", s)
	}
	later := now.Add(writeTxTTL + time.Second)
	if s := write(r.writeTransaction(other, false, later), false); s != im.StatusSuccess {
		t.Fatalf("write after the abandoned chunked write expired %v, want SUCCESS", s)
	}
}
