// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
)

// Endpoint-number reservation per stable key (brief CK §2).
//
// matter.js reserves an endpoint number per endpoint store: "Each number
// is reserved for one store. An installed endpoint keeps its number"
// (packages/node/src/storage/server/ServerEndpointStores.ts:20-22 at
// b25ed960), a closed endpoint keeps its reservation
// (packages/node/src/node/server/ServerEndpointInitializer.ts:67-69), an
// endpoint that returns gets its stored number back
// (ServerEndpointStores.ts:117-123), and only an erased store frees it
// (ServerEndpointInitializer.ts:59-65 eraseDescendant →
// eraseStoreForEndpoint). Aggregator.Remove is the close, Aggregator.Forget
// the erase: a removed key keeps its record in the endpoint store, and a
// reservation in the settings table names the keys of its tree, so a
// Forget of the removed key erases its parts' numbers too.

// endpointReservedKeyRoot prefixes the reservation of one stable key; its
// value is the JSON list of every stable key of the removed endpoint's
// tree (the endpoint first, then its parts), "" when none is held.
const endpointReservedKeyRoot = "endpoint_number_reserved."

// reservationTree is the stable keys of spec and its parts, depth first;
// a part without a key has no number to keep and is skipped.
func reservationTree(spec endpoint.Spec) []string {
	var out []string
	var walk func(s *endpoint.Spec)
	walk = func(s *endpoint.Spec) {
		if k, ok := keyString(s.StableKey); ok {
			out = append(out, k)
		}
		for i := range s.Parts {
			walk(&s.Parts[i])
		}
	}
	walk(&spec)
	return out
}

// reservedTree returns the tree a reservation of key holds, nil for none.
func (p persisted) reservedTree(ctx context.Context, key string) ([]string, error) {
	v, ok, err := p.st.GetSetting(ctx, endpointReservedKeyRoot+key)
	if err != nil || !ok || v == "" {
		return nil, err
	}
	var tree []string
	if err := json.Unmarshal([]byte(v), &tree); err != nil {
		return nil, fmt.Errorf("node: endpoint reservation of %q: %w", key, err)
	}
	return tree, nil
}

// reserve records tree's reservation under each of its keys.
func (p persisted) reserve(ctx context.Context, tree []string) error {
	if len(tree) == 0 {
		return nil
	}
	v, err := json.Marshal(tree)
	if err != nil {
		return err
	}
	var errs []error
	for _, k := range tree {
		errs = append(errs, p.st.SetSetting(ctx, endpointReservedKeyRoot+k, string(v)))
	}
	return errors.Join(errs...)
}

// release drops the reservation key holds, under every key of its tree.
func (p persisted) release(ctx context.Context, key string) error {
	tree, err := p.reservedTree(ctx, key)
	if err != nil || tree == nil {
		return err
	}
	var errs []error
	for _, k := range tree {
		errs = append(errs, p.st.SetSetting(ctx, endpointReservedKeyRoot+k, ""))
	}
	return errors.Join(errs...)
}

// reservationOp is one change a [Batch] makes to the reservations, applied
// in call order once fn returned: reserve a removed endpoint's tree,
// release a re-added key's reservation, or erase a forgotten key's
// stored numbers.
type reservationOp struct {
	kind reservationKind
	key  string
	// tree is the removed (or forgotten) endpoint's keys; nil for a
	// release and for a forgotten key that was not in the set.
	tree []string
}

type reservationKind uint8

const (
	opReserve reservationKind = iota
	opRelease
	opErase
)

// applyReservations runs ops in order.
func (a *Aggregator) applyReservations(ctx context.Context, ops []reservationOp) error {
	p := a.n.persisted
	var errs []error
	for _, op := range ops {
		switch op.kind {
		case opReserve:
			errs = append(errs, p.reserve(ctx, op.tree))
		case opRelease:
			// A returning key is no longer forgotten: a forget left half
			// done for it must not erase it at the next New.
			errs = append(errs, p.release(ctx, op.key), a.cancelForget(ctx, op.key))
		case opErase:
			errs = append(errs, a.erase(ctx, op))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("node: endpoint number reservation: %w", err)
	}
	return nil
}

// erase frees every stored number of a forgotten key: its reservation's
// tree, the tree it had in the set, and the key itself. The nonvolatile
// persister lets go of the servers it met at those keys and erases the
// nonvolatile values and UserLabel lists stored for them.
//
// A Forget is several store writes. Before the first, the keys are
// recorded as a forget in progress (forgetPendingKey); the record goes
// once every write landed, and the next New finishes a forget that a
// refused write or a crash left half done ([Aggregator.finishForgets]), so
// a key's number never survives its values and reservation (review II,
// R2-P6-store-2406). A store that refuses the record changes nothing.
func (a *Aggregator) erase(ctx context.Context, op reservationOp) error {
	reserved, err := a.n.persisted.reservedTree(ctx, op.key)
	if err != nil {
		return err
	}
	keys := slices.Compact(slices.Sorted(slices.Values(slices.Concat([]string{op.key}, op.tree, reserved))))
	if err := a.markForget(ctx, keys, true); err != nil {
		return err
	}
	return a.forgetKeys(ctx, keys)
}

// forgetKeys is erase's writes for keys, recorded as a forget in progress:
// the reservations released, the values erased, the endpoint records
// removed — and then the record of the forget. A refused release leaves
// the records, as before, for the forget's next run.
func (a *Aggregator) forgetKeys(ctx context.Context, keys []string) error {
	var errs []error
	for _, k := range keys {
		if err := a.n.persisted.release(ctx, k); err != nil {
			errs = append(errs, err)
		}
	}
	released := len(errs) == 0
	errs = append(errs, a.n.nonVolatile.forget(ctx, keys))
	if released {
		errs = append(errs, a.removeRecords(ctx, "", keys))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return a.markForget(ctx, keys, false)
}

// finishForgets runs again every forget a refused write or a crash left
// half done (erase), at New, before the host adds anything.
func (a *Aggregator) finishForgets(ctx context.Context) error {
	pending, err := a.n.persisted.pendingForgets(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, keys := range pending {
		errs = append(errs, a.forgetKeys(ctx, keys))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("node: finish a forget: %w", err)
	}
	return nil
}

// forgetPendingKey is the setting the forgets in progress are recorded
// under: a JSON list of their key sets.
const forgetPendingKey = "aggregator.forget_pending"

// pendingForgets returns the key sets of the forgets in progress.
func (p persisted) pendingForgets(ctx context.Context) ([][]string, error) {
	v, ok, err := p.st.GetSetting(ctx, forgetPendingKey)
	if err != nil {
		return nil, fmt.Errorf("node: load %s: %w", forgetPendingKey, err)
	}
	if !ok || v == "" {
		return nil, nil
	}
	var pending [][]string
	if err := json.Unmarshal([]byte(v), &pending); err != nil {
		return nil, fmt.Errorf("node: %s: %w", forgetPendingKey, err)
	}
	return pending, nil
}

// cancelForget drops key from every forget in progress (and a forget left
// with no key): a key added again after a forget that did not finish is
// the new device's, and the next New finishing that forget must not erase
// it (review II, R2-P6-store-2406, coordinator decision on facade Q3).
func (a *Aggregator) cancelForget(ctx context.Context, key string) error {
	a.forgetMu.Lock()
	defer a.forgetMu.Unlock()
	pending, err := a.n.persisted.pendingForgets(ctx)
	if err != nil || len(pending) == 0 {
		return err
	}
	changed := false
	kept := pending[:0]
	for _, keys := range pending {
		if i := slices.Index(keys, key); i >= 0 {
			keys = slices.Delete(slices.Clone(keys), i, i+1)
			changed = true
		}
		if len(keys) > 0 {
			kept = append(kept, keys)
		}
	}
	if !changed {
		return nil
	}
	return a.storeForgets(ctx, kept)
}

// storeForgets writes the forgets in progress; a.forgetMu is held.
func (a *Aggregator) storeForgets(ctx context.Context, pending [][]string) error {
	v := ""
	if len(pending) > 0 {
		b, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		v = string(b)
	}
	return a.n.persisted.st.SetSetting(ctx, forgetPendingKey, v)
}

// markForget records keys as a forget in progress (on) or done (off); the
// record is read and written under a.forgetMu.
func (a *Aggregator) markForget(ctx context.Context, keys []string, on bool) error {
	a.forgetMu.Lock()
	defer a.forgetMu.Unlock()
	p := a.n.persisted
	pending, err := p.pendingForgets(ctx)
	if err != nil {
		return err
	}
	pending = slices.DeleteFunc(pending, func(k []string) bool { return slices.Equal(k, keys) })
	if on {
		pending = append(pending, keys)
	}
	return a.storeForgets(ctx, pending)
}

// removeRecords deletes the endpoint store's records of scope ("" is
// every scope) whose key is one of keys.
func (a *Aggregator) removeRecords(ctx context.Context, scope string, keys []string) error {
	store := a.n.cfg.Store.Endpoints
	if store == nil {
		return nil
	}
	recs, err := store.ListEndpoints(ctx, scope)
	if err != nil {
		return err
	}
	var errs []error
	for _, rec := range recs {
		if rec.Key != nil && slices.Contains(keys, rec.Key.String()) {
			errs = append(errs, store.RemoveEndpoint(ctx, rec.Key))
		}
	}
	return errors.Join(errs...)
}

// eraseScope frees every number stored for a retired scope: the
// reservations of its stored identities, then the identities themselves.
func (a *Aggregator) eraseScope(ctx context.Context, scope string) error {
	store := a.n.cfg.Store.Endpoints
	if store == nil {
		return nil
	}
	recs, err := store.ListEndpoints(ctx, scope)
	if err != nil {
		return fmt.Errorf("node: retire %s: %w", scope, err)
	}
	var errs []error
	for _, rec := range recs {
		if rec.Key == nil {
			continue
		}
		if err := a.n.persisted.release(ctx, rec.Key.String()); err != nil {
			errs = append(errs, err)
		}
		errs = append(errs, store.RemoveEndpoint(ctx, rec.Key))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("node: retire %s: %w", scope, err)
	}
	return nil
}

// Forget removes the endpoint with key from the default partition if it
// holds it, and frees the endpoint numbers stored for key and for the
// parts it had: a later Add of key gets a fresh number, as a new device
// would. It erases what the node stored for those keys' servers too — the
// nonvolatile attribute values the module holds and the UserLabel lists —
// so the later Add starts from its declaration. It is the counterpart of [Aggregator.Remove], which keeps them —
// matter.js's `delete()` beside `close()` (endpoint/Endpoint.ts:987-992,
// :1024-1032 at db021da0; ServerEndpointInitializer.ts:59-65
// eraseDescendant). A key no partition holds and nothing is stored for is
// no error; a key another partition holds is [ErrUnknownKey]. A key the
// specification level withheld loses its withheld record in
// [Node.Surface].
func (a *Aggregator) Forget(ctx context.Context, key device.SourceKey) error {
	return a.Partition(Scope).Forget(ctx, key)
}

// Forget forgets key in the partition; see [Aggregator.Forget].
func (p *Partition) Forget(ctx context.Context, key device.SourceKey) error {
	return p.Batch(ctx, func(b *Batch) error { return b.Forget(key) })
}

// Forget removes the endpoint with key from the batch's partition if it
// holds it and frees the numbers stored for key once fn returned; see
// [Aggregator.Forget].
func (b *Batch) Forget(key device.SourceKey) error {
	if key == nil {
		return ErrNoStableKey
	}
	k := key.String()
	a := b.a
	a.mu.Lock()
	part, i := a.findLocked(k)
	if part != nil && part.scope != b.scope {
		a.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	var removed Endpoint
	if part != nil {
		removed = part.eps[i]
		b.dropLocked(part, i, k)
	}
	a.mu.Unlock()
	// A device the level withheld has no endpoint and no number, only
	// its record in the surface report; Forget drops that too.
	a.n.dropWithheld(k)
	op := reservationOp{kind: opErase, key: k}
	if removed != nil {
		op.tree = reservationTree(removed.Spec())
	}
	b.ops = append(b.ops, op)
	return nil
}

// splitErasures splits a batch's reservation ops into the ones applied
// before its reassembly and the erasures of forgotten keys applied after
// it ([Partition.Batch]).
func splitErasures(ops []reservationOp) (pre, erase []reservationOp) {
	for _, op := range ops {
		if op.kind == opErase {
			erase = append(erase, op)
			continue
		}
		pre = append(pre, op)
	}
	return pre, erase
}

// readdedInErasure finds the endpoints a batch added (its opRelease keys,
// still in scope's partition) that an erasure of the same batch would
// reach: one whose own key or a part key lies in a forgotten key's
// erasure set (the key, its tree in the set, its reservation's tree —
// what [Aggregator.erase] erases). held is their keys, for
// [Aggregator.holdBack]; cancel is the stable keys they serve inside an
// erasure set, for [Aggregator.cancelForget]. A device keyed as a part of
// the forgotten device is served after the erasure, as a forgotten key
// added again is, so the erasure never takes what an endpoint already
// serves (review II, R2-C2-store-2401). Run after the batch's pre ops:
// a re-added key's release has then dropped the reservation it shared.
func (a *Aggregator) readdedInErasure(ctx context.Context, scope string, ops, erase []reservationOp) (held, cancel []string, err error) {
	if len(erase) == 0 {
		return nil, nil, nil
	}
	forgotten := map[string]bool{}
	var errs []error
	for _, op := range erase {
		forgotten[op.key] = true
		for _, k := range op.tree {
			forgotten[k] = true
		}
		reserved, err := a.n.persisted.reservedTree(ctx, op.key)
		errs = append(errs, err)
		for _, k := range reserved {
			forgotten[k] = true
		}
	}
	a.mu.Lock()
	part := a.partLocked(scope)
	for _, op := range ops {
		if op.kind != opRelease || slices.Contains(held, op.key) {
			continue
		}
		i := slices.IndexFunc(part.eps, func(ep Endpoint) bool {
			ek, err := keyOf(ep.Spec())
			return err == nil && ek == op.key
		})
		if i < 0 {
			continue
		}
		hit := false
		for _, k := range reservationTree(part.eps[i].Spec()) {
			if forgotten[k] {
				hit = true
				if !slices.Contains(cancel, k) {
					cancel = append(cancel, k)
				}
			}
		}
		if hit {
			held = append(held, op.key)
		}
	}
	a.mu.Unlock()
	return held, cancel, errors.Join(errs...)
}

// heldEndpoint is an endpoint [Aggregator.holdBack] took out of its
// partition: where it was and its device's detach.
type heldEndpoint struct {
	key    string
	at     int
	ep     Endpoint
	detach func()
}

// holdBack takes the endpoints with keys out of scope's partition until
// the returned restore puts them back where they were; restore reports
// whether it put any back. A key another goroutine added meanwhile keeps
// that endpoint, and the held one is unbound; on a stopped node nothing is
// put back (its Stop unbound every device it held, the held ones among
// them). nil restore for no key held.
func (a *Aggregator) holdBack(scope string, keys []string) (restore func() bool) {
	if len(keys) == 0 {
		return nil
	}
	a.mu.Lock()
	part := a.partLocked(scope)
	var held []heldEndpoint
	for _, k := range keys {
		i := slices.IndexFunc(part.eps, func(ep Endpoint) bool {
			ek, err := keyOf(ep.Spec())
			return err == nil && ek == k
		})
		if i < 0 {
			continue
		}
		held = append(held, heldEndpoint{key: k, at: i, ep: part.eps[i], detach: a.detach[k]})
		part.eps = slices.Delete(part.eps, i, i+1)
	}
	a.mu.Unlock()
	if len(held) == 0 {
		return nil
	}
	return func() bool {
		var unbind []func()
		put := false
		a.mu.Lock()
		part := a.partLocked(scope)
		for j := len(held) - 1; j >= 0; j-- {
			h := held[j]
			if a.closed {
				continue
			}
			if p, _ := a.findLocked(h.key); p != nil {
				unbind = append(unbind, h.detach)
				continue
			}
			part.eps = slices.Insert(part.eps, min(h.at, len(part.eps)), h.ep)
			put = true
		}
		a.mu.Unlock()
		for _, d := range unbind {
			if d != nil {
				d()
			}
		}
		return put
	}
}
