// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// dispatchWriteRequest stamps the write context with the timed window, as
// the invoke path does, so the dispatcher can answer an attribute with the
// timed access quality outside one with NeedsTimedInteraction (matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts:243-247;
// the dispatcher's own test is endpoint/timed_write_parity_matterjs_test.go).

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
)

// timedStampDispatcher records the timed stamp each Write sees.
type timedStampDispatcher struct {
	im.Dispatcher
	mu           sync.Mutex
	timed, known []bool
}

func (d *timedStampDispatcher) Write(ctx context.Context, path im.ConcreteAttributePath, _ im.AttributeValue) []im.WriteResult {
	timed, known := im.TimedInteractionFromContext(ctx)
	d.mu.Lock()
	d.timed, d.known = append(d.timed, timed), append(d.known, known)
	d.mu.Unlock()
	return []im.WriteResult{{Path: path, Status: im.StatusSuccess}}
}

func TestDispatchWriteRequest_StampsTheTimedWindow(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	d := &timedStampDispatcher{}
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
	req := im.WriteRequest{Writes: []im.AttributeWrite{{Path: im.ConcreteAttributePath{
		Endpoint: 1, Cluster: 0x0006, Attribute: 0x4001, HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}, Value: im.AttributeValue{Value: uint16(5)}}}}
	_ = b.dispatchWriteRequest(context.Background(), src, &message.Header{}, message.ProtocolHeader{ExchangeID: 7}, d, req)
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.known) != 1 || !d.known[0] || d.timed[0] {
		t.Fatalf("untimed write: stamps known %v timed %v, want one known, untimed", d.known, d.timed)
	}
}
