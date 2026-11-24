// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"

	"thingfabric.dev/go-fabric/contract"
)

// EndReasonUnknown is WebRTCEndReasonEnum UnknownReason, the reason a
// node passes to [Provider.PeerGone] and the one a removed fabric's
// sessions end with ([Provider.MatterDropFabric]): no other value of the
// enum names the requestor's loss (parity/schema.json, WebRTCEndReasonEnum
// 12).
const EndReasonUnknown = endReasonUnknown

// PeerGone ends every session of the requestor (fabricIndex, peerNodeID):
// the port's EndSession with reason for each, and the session leaves
// CurrentSessions whatever the port answers; the error is the port's.
// No End command is sent: the peer is not reachable. It is
// [Provider.MatterDropFabric] narrowed to the pair, and like it runs
// under the provider's command serialisation, so it waits for a command
// in flight and its port call. A pair without a session ends nothing.
//
// A node calls it when it has proven the requestor lost — an exchange the
// node opened went unanswered — and holds no session with the peer in
// either of its stacks (#590 item 2, owner decisions 611, 612); a host
// that runs the server outside a node may call it on its own evidence.
// It is not the media plane's verdict: a viewer idle on Matter keeps its
// sessions, and the host ends a session whose peer connection failed
// with [Provider.RemoveSession] or [Provider.End].
func (p *Provider) PeerGone(ctx context.Context, fabricIndex uint8, peerNodeID uint64, reason uint8) error {
	p.op.Lock()
	defer p.op.Unlock()
	p.mu.Lock()
	var ids []uint16
	for _, s := range p.sessions {
		if s.FabricIndex == fabricIndex && s.PeerNodeId == peerNodeID {
			ids = append(ids, s.Id)
		}
	}
	p.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if err := p.port.EndSession(ctx, id, reason); err != nil {
			errs = append(errs, fmt.Errorf("camera: EndSession %d of lost peer %d:%#x: %w", id, fabricIndex, peerNodeID, err))
		}
		if err := p.removeSession(contract.OriginUnknown, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
