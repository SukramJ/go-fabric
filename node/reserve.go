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

// applyReservations runs ops in order. keep are the keys an erasure leaves
// the endpoint record of ([Aggregator.erase]).
func (a *Aggregator) applyReservations(ctx context.Context, ops []reservationOp, keep []string) error {
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
			errs = append(errs, a.erase(ctx, op, keep))
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
// nonvolatile values and UserLabel lists stored for them. The endpoint
// records of keep stay: a served endpoint the batch held back keeps its
// number across the erasure (review II, R3-store-3702, decision 560).
//
// A Forget is several store writes. Before the first, the keys are
// recorded as a forget in progress (forgetPendingKey); the record goes
// once every write landed, and the next New finishes a forget that a
// refused write or a crash left half done ([Aggregator.finishForgets]), so
// a key's number never survives its values and reservation (review II,
// R2-P6-store-2406). A store that refuses the record changes nothing.
// The record carries the keys of keep among keys, so a forget a crash
// left half done spares the held endpoint's record too and the endpoint
// keeps its number across the restart (decision 568).
func (a *Aggregator) erase(ctx context.Context, op reservationOp, keep []string) error {
	reserved, err := a.n.persisted.reservedTree(ctx, op.key)
	if err != nil {
		return err
	}
	keys := slices.Compact(slices.Sorted(slices.Values(slices.Concat([]string{op.key}, op.tree, reserved))))
	kept := slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return !slices.Contains(keep, k) })
	if err := a.markForget(ctx, pendingForget{Keys: keys, Keep: kept}, true); err != nil {
		return err
	}
	return a.forgetKeys(ctx, keys, kept)
}

// forgetKeys is erase's writes for keys, recorded as a forget in progress:
// the reservations released, the values erased, the endpoint records
// removed but those of keep — and then the record of the forget. A refused
// release leaves the records, as before, for the forget's next run.
func (a *Aggregator) forgetKeys(ctx context.Context, keys, keep []string) error {
	var errs []error
	for _, k := range keys {
		if err := a.n.persisted.release(ctx, k); err != nil {
			errs = append(errs, err)
		}
	}
	released := len(errs) == 0
	errs = append(errs, a.n.nonVolatile.forget(ctx, keys))
	if released {
		gone := slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return slices.Contains(keep, k) })
		errs = append(errs, a.removeRecords(ctx, "", gone))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return a.markForget(ctx, pendingForget{Keys: keys}, false)
}

// finishForgets runs again every forget a refused write or a crash left
// half done (erase), at New, before the host adds anything.
func (a *Aggregator) finishForgets(ctx context.Context) error {
	pending, err := a.n.persisted.pendingForgets(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, f := range pending {
		errs = append(errs, a.forgetKeys(ctx, f.Keys, f.Keep))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("node: finish a forget: %w", err)
	}
	return nil
}

// forgetPendingKey is the setting the forgets in progress are recorded
// under: a JSON list with one element per forget. An element is the
// forget's key list, or — when the forget keeps the endpoint records of
// some of them — an object {"keys": [...], "keep": [...]} (decision 568).
// The bare list is the format before keep: a record written then reads
// as before, and a forget that keeps nothing is still written in it.
const forgetPendingKey = "aggregator.forget_pending"

// pendingForget is one forget in progress: the keys it erases and, of
// them, the keys whose endpoint records it leaves ([Aggregator.erase]).
type pendingForget struct {
	Keys []string `json:"keys"`
	Keep []string `json:"keep,omitempty"`
}

// MarshalJSON writes a forget that keeps nothing as the bare key list.
func (f pendingForget) MarshalJSON() ([]byte, error) {
	if len(f.Keep) == 0 {
		return json.Marshal(f.Keys)
	}
	type object pendingForget
	return json.Marshal(object(f))
}

// UnmarshalJSON reads both element forms of forgetPendingKey.
func (f *pendingForget) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &f.Keys); err == nil {
		f.Keep = nil
		return nil
	}
	type object pendingForget
	var o object
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*f = pendingForget(o)
	return nil
}

// pendingForgets returns the forgets in progress.
func (p persisted) pendingForgets(ctx context.Context) ([]pendingForget, error) {
	v, ok, err := p.st.GetSetting(ctx, forgetPendingKey)
	if err != nil {
		return nil, fmt.Errorf("node: load %s: %w", forgetPendingKey, err)
	}
	if !ok || v == "" {
		return nil, nil
	}
	var pending []pendingForget
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
	for _, f := range pending {
		if i := slices.Index(f.Keys, key); i >= 0 {
			f.Keys = slices.Delete(slices.Clone(f.Keys), i, i+1)
			f.Keep = slices.DeleteFunc(slices.Clone(f.Keep), func(k string) bool { return k == key })
			changed = true
		}
		if len(f.Keys) > 0 {
			kept = append(kept, f)
		}
	}
	if !changed {
		return nil
	}
	return a.storeForgets(ctx, kept)
}

// storeForgets writes the forgets in progress; a.forgetMu is held.
func (a *Aggregator) storeForgets(ctx context.Context, pending []pendingForget) error {
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

// markForget records f as a forget in progress (on) or done (off), a
// forget known by its keys; the record is read and written under
// a.forgetMu.
func (a *Aggregator) markForget(ctx context.Context, f pendingForget, on bool) error {
	a.forgetMu.Lock()
	defer a.forgetMu.Unlock()
	p := a.n.persisted
	pending, err := p.pendingForgets(ctx)
	if err != nil {
		return err
	}
	pending = slices.DeleteFunc(pending, func(p pendingForget) bool { return slices.Equal(p.Keys, f.Keys) })
	if on {
		pending = append(pending, f)
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
// no error; a key another partition holds is [ErrUnknownKey], and so is a
// part key another endpoint, of any partition, serves: the part is that
// endpoint's, and a Forget of it changes nothing (decision 567). A key
// the specification level withheld loses its withheld record in
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
// [Aggregator.Forget]. Only a key another endpoint still serves is
// refused (decision 575, narrowing 567): a key of the forgotten device —
// its tree or its reservation's — that no endpoint serves any more may be
// taken by a different device the batch adds, before or after the
// Forget, and the erasure then spares that device's endpoint
// ([Aggregator.servedInErasure]); the forgotten device added again starts
// new with its parts.
func (b *Batch) Forget(key device.SourceKey) error {
	if key == nil {
		return ErrNoStableKey
	}
	k := key.String()
	a := b.a
	reserved, err := a.n.persisted.reservedTree(b.ctx, k)
	if err != nil {
		return err
	}
	a.mu.Lock()
	part, i := a.findLocked(k)
	if part != nil && part.scope != b.scope {
		a.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	var removed Endpoint
	if part != nil {
		removed = part.eps[i]
	}
	// A part key another endpoint serves is that endpoint's: refused as
	// a key another partition holds is, before anything changed (review
	// II, R3-store-3702 edge, decision 567).
	if a.servesExceptLocked(k, removed) {
		a.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if part != nil {
		b.dropLocked(part, i, k)
	}
	a.mu.Unlock()
	// A device the level withheld has no endpoint and no number, only
	// its record in the surface report; Forget drops that too.
	a.n.dropWithheld(k)
	// The erasure reads the reservation again after fn, but an Add of k
	// in the batch has released it by then: the tree reserved now is in
	// the op, so a device removed before the batch and added again in it
	// starts new with its parts too (Fable N2 of the G5c diff read,
	// decision 576).
	tree := slices.Clone(reserved)
	if removed != nil {
		tree = append(tree, reservationTree(removed.Spec())...)
	}
	op := reservationOp{kind: opErase, key: k}
	if len(tree) > 0 {
		op.tree = slices.Compact(slices.Sorted(slices.Values(tree)))
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

// servedInErasure finds every endpoint the set serves, in any partition,
// that an erasure of the batch would reach: one whose own key or a part
// key lies in a forgotten key's erasure set (the key, its tree in the set,
// its reservation's tree — what [Aggregator.erase] erases). It does not
// matter how the endpoint came to serve the key — an Add or a Replace of
// this batch, an Add of an earlier call, another partition's endpoint:
// each is held out of the reassembly before the erasure and served again
// after it, as a forgotten key added again is, so the erasure never takes
// what an endpoint serves (review II, R2-C2-store-2401, R3-store-3702).
// held is their partitions and keys, for [Aggregator.holdBack]; cancel is
// the stable keys they serve inside an erasure set, for
// [Aggregator.cancelForget]; keep is cancel without the forgotten keys
// marked fresh, the keys whose endpoint records the erasure leaves, so a
// held endpoint is served again at the number it had (decision 560) while
// a forgotten key added again in the batch starts new, and every key of
// its endpoint's tree with it. It only reads the partition list: a
// partition retired meanwhile stays retired (R3-store-3701). Run after the
// batch's pre ops: a re-added key's release has then dropped the
// reservation it shared.
func (a *Aggregator) servedInErasure(ctx context.Context, erase []reservationOp) (held []heldKey, cancel, keep []string, err error) {
	if len(erase) == 0 {
		return nil, nil, nil, nil
	}
	forgotten := map[string]bool{}
	fresh := map[string]bool{}
	var errs []error
	for _, op := range erase {
		// No other endpoint serves a forgotten key when its Forget
		// runs (decisions 567, 575): served again, it is a new device.
		forgotten[op.key] = true
		fresh[op.key] = true
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
	for _, part := range a.parts {
		for _, ep := range part.eps {
			spec := ep.Spec()
			key, err := keyOf(spec)
			if err != nil {
				continue
			}
			hit := false
			tree := reservationTree(spec)
			if fresh[key] {
				// A forgotten device the batch adds again starts new
				// with its parts: none of its keys keeps a record, so
				// a part is not served at the forgotten part's number
				// either (R3-store-3702 repair, decision 560).
				for _, k := range tree {
					fresh[k] = true
				}
			}
			for _, k := range tree {
				if forgotten[k] {
					hit = true
					if !slices.Contains(cancel, k) {
						cancel = append(cancel, k)
					}
				}
			}
			if hit {
				held = append(held, heldKey{scope: part.scope, key: key})
			}
		}
	}
	a.mu.Unlock()
	for _, k := range cancel {
		if !fresh[k] {
			keep = append(keep, k)
		}
	}
	return held, cancel, keep, errors.Join(errs...)
}

// heldKey names an endpoint to hold back: its partition and its key.
type heldKey struct{ scope, key string }

// heldEndpoint is an endpoint [Aggregator.holdBack] took out of its
// partition: where it was and its device's detach.
type heldEndpoint struct {
	scope  string
	key    string
	at     int
	ep     Endpoint
	detach func()
}

// holdBack takes the endpoints keys names out of their partitions until
// the returned restore puts them back where they were; restore reports
// whether it put any back. A key another goroutine added meanwhile keeps
// that endpoint, and the held one is unbound; so is one whose partition
// was retired meanwhile — the retirement is not undone (R3-store-3701) —
// and on a stopped node nothing is put back (its Stop unbound every
// device it held, the held ones among them). nil restore for no key held.
func (a *Aggregator) holdBack(keys []heldKey) (restore func() bool) {
	if len(keys) == 0 {
		return nil
	}
	a.mu.Lock()
	var held []heldEndpoint
	for _, hk := range keys {
		part := a.findPartLocked(hk.scope)
		if part == nil {
			continue
		}
		i := slices.IndexFunc(part.eps, func(ep Endpoint) bool {
			ek, err := keyOf(ep.Spec())
			return err == nil && ek == hk.key
		})
		if i < 0 {
			continue
		}
		held = append(held, heldEndpoint{scope: hk.scope, key: hk.key, at: i, ep: part.eps[i], detach: a.detach[hk.key]})
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
		for j := len(held) - 1; j >= 0; j-- {
			h := held[j]
			if a.closed {
				continue
			}
			part := a.findPartLocked(h.scope)
			if p, _ := a.findLocked(h.key); p != nil || part == nil {
				unbind = append(unbind, h.detach)
				if p == nil && a.detach[h.key] != nil {
					a.detachSetLocked(h.key, nil)
				}
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
