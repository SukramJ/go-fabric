// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"

	"thingfabric.dev/go-fabric/contract"
)

// PeerGone ends every session of the requestor (fabricIndex, peerNodeID):
// the port's EndSession with reason for each, and the session leaves
// CurrentSessions whatever the port answers; the error is the port's.
// No End command is sent: the peer is not reachable. It is
// [Provider.MatterDropFabric] narrowed to the pair, and like it runs
// under the provider's command serialisation, so it waits for a command
// in flight and its port call. A pair without a session ends nothing.
//
// gone is the caller's evidence that the requestor is still lost, asked
// once after the serialisation is held and before any port call: false
// ends nothing and PeerGone reports ran false. A loss proven before the
// wait may be over by the time a command in flight lets PeerGone run —
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
func (p *Provider) PeerGone(ctx context.Context, fabricIndex uint8, peerNodeID uint64, reason uint8, gone func() bool) (ran bool, err error) {
	p.op.Lock()
	defer p.op.Unlock()
	if gone != nil && !gone() {
		return false, nil
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
	return true, errors.Join(errs...)
}
