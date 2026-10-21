// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
)

// The address book's lifespans — node/client/Peers.ts:73-74 at 0ceb202d.
const (
	// DefaultTTL is the lifespan of a discovered address or node whose
	// TTL is unknown (DEFAULT_TTL, Peers.ts:73).
	DefaultTTL = 15 * time.Minute
	// ExpirationInterval is the period of the cull (EXPIRATION_INTERVAL,
	// Peers.ts:74).
	ExpirationInterval = time.Minute
)

// expirationOf is when a lifespan ends — Peers.ts:959-966: never without
// a discovery time (ok false), DefaultTTL after it when the TTL is
// unknown.
func expirationOf(discoveredAt time.Time, ttl time.Duration) (time.Time, bool) {
	if discoveredAt.IsZero() {
		return time.Time{}, false
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return discoveredAt.Add(ttl), true
}

// manageExpiration arms the expiry timer while the registry has nodes and
// cancels it when it has none — Peers.#manageExpiration (Peers.ts:615-634).
func (r *Registry) manageExpiration() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.timer != nil {
		if len(r.nodes) == 0 {
			r.timer.Stop()
			r.timer = nil
		}
		return
	}
	if len(r.nodes) == 0 {
		return
	}
	r.timer = r.clock.AfterFunc(ExpirationInterval, r.onExpirationIntervalElapsed)
}

// cancelExpiration is Peers.#cancelExpiration (Peers.ts:529-534).
func (r *Registry) cancelExpiration() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
}

// onExpirationIntervalElapsed runs the cull under the mutex, logs its
// error and re-arms the timer — Peers.ts:636-649, :709-711.
func (r *Registry) onExpirationIntervalElapsed() {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return
	}
	func() {
		r.op.Lock()
		defer r.op.Unlock()
		if err := r.cull(r.ctx); err != nil {
			r.log.Warn("matter.controller.client.registry.cull_failed", slog.String("err", err.Error()))
		}
		r.mu.Lock()
		r.timer = nil
		r.mu.Unlock()
	}()
	r.manageExpiration()
}

// cull is Peers.#cullExpiredNodesAndAddresses (Peers.ts:651-712), per
// node: a closing node or one with a fabric operation is skipped
// (:656-661); nothing changes without addresses or for a commissioned
// node with exactly one (:669-672); expired addresses go, one without a
// discovery time stays (:674-682); an uncommissioned node with no address
// left, or past its own lifespan, is deleted (:684-690); a commissioned
// node whose every address expired keeps the freshest ones (:692-702);
// the list is written back when it shrank (:704-707).
func (r *Registry) cull(ctx context.Context) error {
	now := r.clock.Now()
	for _, n := range r.Nodes() {
		if n.isClosing() || r.operationOf(n) != nil {
			continue
		}
		rec := n.Record()
		addresses := rec.Addresses
		commissioned := rec.PeerAddress != nil
		if addresses == nil || (commissioned && len(addresses) == 1) {
			continue
		}
		kept := make([]NetworkAddress, 0, len(addresses))
		for _, a := range addresses {
			if exp, ok := expirationOf(a.DiscoveredAt, a.TTL); !ok || exp.After(now) {
				kept = append(kept, a)
			}
		}
		if !commissioned {
			exp, _ := expirationOf(rec.DiscoveredAt, rec.TTL) // zero without a discovery time: expired (`?? 0`, :686)
			if len(kept) == 0 || !exp.After(now) {
				if err := r.deleteNode(ctx, n, nil); err != nil {
					return err
				}
				continue
			}
		}
		if commissioned && len(addresses) > 0 && len(kept) == 0 {
			var freshest time.Time
			for _, a := range addresses {
				if exp, _ := expirationOf(a.DiscoveredAt, a.TTL); exp.After(freshest) {
					freshest = exp
				}
			}
			for _, a := range addresses {
				if exp, _ := expirationOf(a.DiscoveredAt, a.TTL); exp.Equal(freshest) {
					kept = append(kept, a)
				}
			}
		}
		if len(kept) != len(addresses) {
			n.updateRecord(func(rec *Record) { rec.Addresses = kept })
		}
	}
	return nil
}

// ForDevice is the node of a device the scanner found, created when the
// registry has none — Peers.forDescriptor (Peers.ts:358-377). A node
// matches by the device's instance id (Factory.find :942-952,
// RemoteDescriptor.is RemoteDescriptor.ts:37-50); a match under decommission is waited for
// and the search repeated (:360-368); a match is refreshed from the device
// (:369-375, the record's applyDevice); otherwise a new node is created
// with an allocated id (:376). The host runs discovery.Scanner and hands
// the device over; Commission commissions the node.
func (r *Registry) ForDevice(ctx context.Context, d discovery.Device) (*Node, error) {
	for {
		n, op, err := r.findOrCreate(d)
		if op == nil {
			return n, err
		}
		select {
		case <-op.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// findOrCreate is one search of ForDevice under the registry's operation
// mutex, so two sightings of one device find or create one node: matter.js
// runs factory.find and factory.create in one turn of its event loop
// (Peers.ts:371-389 at 862ad75f), a second forDescriptor finding the node
// the first created. A match under decommission is returned as the
// operation to wait for.
func (r *Registry) findOrCreate(d discovery.Device) (*Node, *fabricOp, error) {
	r.op.Lock()
	defer r.op.Unlock()
	if n := r.find(d.InstanceID); n != nil {
		if op := r.operationOf(n); op != nil && op.kind == opDecommission {
			return nil, op, nil
		}
		now := r.clock.Now()
		n.updateRecord(func(rec *Record) { rec.applyDevice(&d, now) })
		return n, nil, nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, nil, ErrClosed
	}
	id := r.allocateIDLocked()
	r.mu.Unlock()
	n, err := r.build(id, nil)
	if err != nil {
		return nil, nil, err
	}
	now := r.clock.Now()
	n.updateRecord(func(rec *Record) { rec.applyDevice(&d, now) })
	r.add(n)
	return n, nil, nil
}

// find is Factory.find with RemoteDescriptor.is: the first node (not
// closing) whose record has the non-empty device identifier. The
// peer-address branch of RemoteDescriptor.is (:38-40) needs both sides to
// have one, and a discovery.Device has none, so it never applies here.
func (r *Registry) find(deviceIdentifier string) *Node {
	if deviceIdentifier == "" {
		return nil
	}
	for _, n := range r.Nodes() {
		if !n.isClosing() && n.Record().DeviceIdentifier == deviceIdentifier {
			return n
		}
	}
	return nil
}
