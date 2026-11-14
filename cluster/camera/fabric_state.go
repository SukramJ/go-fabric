// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/contract"
)

// endReasonUnknown is WebRTCEndReasonEnum UnknownReason, the reason a
// session of a removed fabric ends with: no other value of the enum names
// the fabric's removal (parity/schema.json, WebRTCEndReasonEnum 12; the
// highest value, [maxEndReason]).
const endReasonUnknown uint8 = maxEndReason

// Compile-time assertions: the three servers whose lists are
// fabric-sensitive ("S") implement the removal contract.
var (
	_ contract.FabricScopedState = (*Provider)(nil)
	_ contract.FabricScopedState = (*Requestor)(nil)
	_ contract.FabricScopedState = (*PushTransport)(nil)
)

// heldFabrics returns the distinct fabric indices of entries, ascending.
func heldFabrics[T any](entries []T, fabric func(T) uint8) []uint8 {
	var out []uint8
	for _, e := range entries {
		if f := fabric(e); !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// MatterDropFabric implements [contract.FabricScopedState]: every session
// of the removed fabric ends — the port's EndSession with UnknownReason,
// as a requestor's EndSession command ends it — and leaves
// CurrentSessions whatever the port answers; the error is the port's.
// matter.js strips the fabric's CurrentSessions entries on its deletion
// (FabricScopedDataHandler.ts:41-73 at db021da0); the camera's stack is
// told, so it stops streaming to the removed fabric's peer.
func (p *Provider) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	p.mu.Lock()
	var ids []uint16
	for _, s := range p.sessions {
		if s.FabricIndex == fabricIndex {
			ids = append(ids, s.Id)
		}
	}
	p.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if err := p.port.EndSession(ctx, id, endReasonUnknown); err != nil {
			errs = append(errs, fmt.Errorf("camera: EndSession %d of removed fabric %d: %w", id, fabricIndex, err))
		}
		p.removeSession(contract.OriginUnknown, id)
	}
	return errors.Join(errs...)
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// of CurrentSessions.
func (p *Provider) MatterHeldFabrics() []uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return heldFabrics(p.sessions, func(s ProviderSession) uint8 { return s.FabricIndex })
}

// MatterDropFabric implements [contract.FabricScopedState]: the removed
// fabric's sessions leave CurrentSessions, as matter.js strips them
// (FabricScopedDataHandler.ts:41-73, on fabrics.events.deleting at
// node/server/ServerEnvironment.ts:188-196, db021da0), and the port's End
// is told of each with UnknownReason, after it left, as of a peer's End,
// so the host tears the stream down.
func (r *Requestor) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	var gone []WebRTCSession
	err := r.change(contract.OriginUnknown, func(sessions []WebRTCSession) ([]WebRTCSession, bool) {
		kept := slices.DeleteFunc(slices.Clone(sessions), func(s WebRTCSession) bool {
			if s.FabricIndex == fabricIndex {
				gone = append(gone, s)
				return true
			}
			return false
		})
		return kept, len(gone) > 0
	})
	if err != nil {
		return err
	}
	if r.port != nil {
		for _, s := range gone {
			r.port.End(ctx, s, endReasonUnknown)
		}
	}
	return nil
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// of CurrentSessions.
func (r *Requestor) MatterHeldFabrics() []uint8 {
	return heldFabrics(r.Sessions(), func(s WebRTCSession) uint8 { return s.FabricIndex })
}

// MatterDropFabric implements [contract.FabricScopedState]: every
// connection of the removed fabric is deallocated through the port and
// leaves CurrentConnections whatever the port answers, its expiry timer
// with it; the error is the port's. matter.js strips the fabric's
// CurrentConnections entries on its deletion
// (FabricScopedDataHandler.ts:41-73 at db021da0); the port is told, so
// the camera stops uploading for the removed fabric.
func (p *PushTransport) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	p.mu.Lock()
	var ids []uint16
	for _, c := range p.conns {
		if c.FabricIndex == fabricIndex {
			ids = append(ids, c.ConnectionId)
		}
	}
	p.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if err := p.port.DeallocatePushTransport(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("camera: DeallocatePushTransport %d of removed fabric %d: %w", id, fabricIndex, err))
		}
		p.remove(id)
	}
	return errors.Join(errs...)
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// of CurrentConnections.
func (p *PushTransport) MatterHeldFabrics() []uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return heldFabrics(p.conns, func(c TransportConfiguration) uint8 { return c.FabricIndex })
}
