// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"

	"thingfabric.dev/go-fabric/contract"
)

// PeerGone is [Provider.EndLostPeer] without the count of the sessions
// it ended.
//
// Deprecated: use [Provider.EndLostPeer], which also reports how many of
// the pair's sessions it ended, so a caller reports an end only when one
// happened; PeerGone keeps its behaviour until it goes and is removable in
// v0.29.0 (review II, R5-doc-5304, decision 679).
func (p *Provider) PeerGone(ctx context.Context, fabricIndex uint8, peerNodeID uint64, reason uint8, gone func() bool) (ran bool, err error) {
	_, ran, err = p.EndLostPeer(ctx, fabricIndex, peerNodeID, reason, gone)
	return ran, err
}

// EndLostPeer ends every session of the requestor (fabricIndex,
// peerNodeID): the port's EndSession with reason for each, and the session
// leaves CurrentSessions whatever the port answers; the error is the
// port's. ended is the number of the pair's sessions it ended; 0 when ran
// is false or the pair held none, and a caller that reports the end, as
// the node's log does, reports it only for ended > 0 (review II,
// R5-doc-5304). No End command is sent: the peer is not reachable. It is
// [Provider.MatterDropFabric] narrowed to the pair, and like it runs under
// the provider's command serialisation, so it waits for a command in
// flight and its port call — unless ctx ends first: then, as when ctx
// ended before the call, it ends nothing and reports ran false with no
// error. A pair without a session ends nothing.
//
// gone is the caller's evidence that the requestor is still lost, asked
// once after the serialisation is held and before any port call: false
// ends nothing and EndLostPeer reports ran false. A loss proven before the
// wait may be over by the time a command in flight lets EndLostPeer run —
// the peer establishes a new session meanwhile — and only a check under
// the serialisation sees that (#590 item 2, owner decision 632). A nil
// gone ends unconditionally: the call itself is the evidence. gone runs
// with the serialisation held; it must not call back into the provider.
//
// A node calls it when it has proven the requestor lost — an exchange the
// node opened went unanswered — with gone reporting that it holds no
// session with the peer in either of its stacks (#590 item 2, owner
// decisions 611, 612, 632); a host that runs the server outside a node may
// call it on its own evidence. It is not the media plane's verdict: a
// viewer idle on Matter keeps its sessions, and the host ends a session
// whose peer connection failed with [Provider.RemoveSession] or
// [Provider.End].
func (p *Provider) EndLostPeer(ctx context.Context, fabricIndex uint8, peerNodeID uint64, reason uint8, gone func() bool) (ended int, ran bool, err error) {
	if p.op.LockContext(ctx) != nil {
		return 0, false, nil
	}
	defer p.op.Unlock()
	if gone != nil && !gone() {
		return 0, false, nil
	}
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
	return len(ids), true, errors.Join(errs...)
}
