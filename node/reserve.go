// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
)

// Endpoint-number reservation per stable key (brief CK §2).
//
// matter.js reserves an endpoint number per endpoint store: "Each number
// is reserved for one store. An installed endpoint keeps its number"
// (packages/node/src/storage/server/ServerEndpointStores.ts:20-22 at
// 862ad75f), a closed endpoint keeps its reservation
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

// release drops the reservation key holds, under every key of its tree;
// it returns the tree (nil without a reservation).
func (p persisted) release(ctx context.Context, key string) ([]string, error) {
	tree, err := p.reservedTree(ctx, key)
	if err != nil || tree == nil {
		return nil, err
	}
	var errs []error
	for _, k := range tree {
		errs = append(errs, p.st.SetSetting(ctx, endpointReservedKeyRoot+k, ""))
	}
	return tree, errors.Join(errs...)
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
			_, err := p.release(ctx, op.key)
			errs = append(errs, err)
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
// tree, the tree it had in the set, and the key itself.
func (a *Aggregator) erase(ctx context.Context, op reservationOp) error {
	tree, err := a.n.persisted.release(ctx, op.key)
	if err != nil {
		return err
	}
	keys := slices.Concat([]string{op.key}, op.tree, tree)
	return a.removeRecords(ctx, "", keys)
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
		if _, err := a.n.persisted.release(ctx, rec.Key.String()); err != nil {
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
// would. It is the counterpart of [Aggregator.Remove], which keeps them —
// matter.js's `delete()` beside `close()` (endpoint/Endpoint.ts:987-992,
// :1024-1032 at 862ad75f; ServerEndpointInitializer.ts:59-65
// eraseDescendant). A key no partition holds and nothing is stored for is
// no error; a key another partition holds is [ErrUnknownKey].
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
	op := reservationOp{kind: opErase, key: k}
	if removed != nil {
		op.tree = reservationTree(removed.Spec())
	}
	b.ops = append(b.ops, op)
	return nil
}
