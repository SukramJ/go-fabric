// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"fmt"
)

// SourceKey is the identity of one bridged endpoint's source, as whoever
// owns the model renders it. This package treats it as an opaque token:
// it compares two keys, uses one as a map key, hands it back to the
// [Store] and hashes its String form into the endpoint's UniqueID — and
// never reads anything out of it.
//
// Opaque because the shape of a source identity is the owner's, not
// Matter's. A Homematic host addresses a data point by central, device,
// channel and parameter; another host has no channels at all. Encoding
// one host's coordinates in this package would make every other host
// fake them.
//
// It is an interface rather than a string so the owner can hand its own
// concrete key type straight through and get it back — from [Store] and
// from [Endpoint.SourceKey] — by type assertion. Parsing a rendered key
// back into its parts would be guesswork the moment any component can
// contain the separator, and the owner would be the one guessing about
// its own data.
//
// Two requirements on the implementation:
//
//   - It must be COMPARABLE. The assembler keys maps on it, so a
//     dynamic type containing a slice or map panics at run time. A
//     struct of scalars, or a defined string type, is what fits.
//   - Its String form must be STABLE BYTE-FOR-BYTE across releases. It
//     is the sole input to the UniqueID hash, so a changed rendering
//     re-fingerprints every bridged endpoint and desynchronises each
//     commissioned controller's cached accessory list until every
//     bridged device is removed and re-added by hand.
//
// [StringKey] is the ready-made implementation for an owner whose source
// identity is already one string.
type SourceKey interface {
	fmt.Stringer
}

// StringKey is the trivial [SourceKey] for an owner whose source
// identity is already a single string. Owners with a composite identity
// implement [SourceKey] on their own key type instead, so they can
// recover the parts by type assertion rather than by parsing.
type StringKey string

// String implements [SourceKey].
func (k StringKey) String() string { return string(k) }

// Record is one persisted (source → endpoint id) mapping. EndpointID is
// in [1, 65534]; endpoint 0 is the root bridge endpoint and is never
// stored.
type Record struct {
	Key SourceKey
	// Scope is the [Snapshot.Scope] the source belongs to. Carried
	// because garbage collection lists one scope at a time and the key
	// is opaque here, so a store has no other way to answer
	// ListEndpoints. A store whose own key type already names the scope
	// should read it from there and reject a Scope that disagrees,
	// rather than keep two unchecked sources for one column.
	Scope      string
	EndpointID uint16
	DeviceType uint16
}

// ErrNotFound is what a [Store] returns when a key lookup misses. The
// assembler reads it as "this source has no endpoint id yet" and
// allocates one, so an implementation that returns a different error for
// an absent row makes every assembly fail instead.
var ErrNotFound = errors.New("endpoint: no persisted identity for this source")

// Store persists endpoint identity on the owner's side.
//
// Endpoint numbers are the one piece of bridge state a commissioner
// caches and cannot be told to re-read: a controller keys its accessory
// list on the number, and the Aggregator's Descriptor.PartsList set is
// unchanged by a remove-then-add pair. An implementation must therefore
// allocate monotonically and never reissue a number a removed source
// once held. Mirrors matter.js
// packages/node/src/storage/server/ServerEndpointStores.ts assignNumber,
// which allocates from a persisted counter and never rewinds it when an
// endpoint store is erased.
type Store interface {
	// GetEndpoint returns the record for key, or [ErrNotFound].
	GetEndpoint(ctx context.Context, key SourceKey) (Record, error)
	// UpsertEndpointAssigning writes rec, allocating a fresh endpoint id
	// when rec.EndpointID is 0, and returns the effective id.
	UpsertEndpointAssigning(ctx context.Context, rec Record) (uint16, error)
	// ListEndpoints returns every record in scope, ascending by endpoint
	// id. An empty scope means "every scope".
	ListEndpoints(ctx context.Context, scope string) ([]Record, error)
	// RemoveEndpoint deletes the record for key. Idempotent: a missing
	// row is not an error, because the assembler's garbage collection
	// does not coordinate with concurrent removals.
	RemoveEndpoint(ctx context.Context, key SourceKey) error
}
