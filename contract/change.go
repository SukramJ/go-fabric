// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import (
	"context"
	"sync"
)

// Origin names where a change of a cluster server's state came from: the
// host's own Set, a controller's write, or a command (with the work the
// server does on its own after it — a transition's steps, a countdown).
// It is the Go form of the session matter.js hands every `$Changed`
// observer with the new and the old value
// (packages/node/src/behavior/state/managed/Datasource.ts:393-395
// ValueObserver, :1473-1476 the notification's params): a host tells its
// own changes from a controller's by it, without keeping a copy of what
// it set.
type Origin uint8

// The origins of a change.
const (
	// OriginUnknown: the server did not say — a host's own server that
	// reports through [AttributeChangeNotifier] without
	// [DeliverChange], or one that reports through [ChangeNotifier].
	OriginUnknown Origin = iota
	// OriginHostSet: the host set the state (the server's SetAttributes,
	// a typed server's Set), a command handler of the host's included.
	OriginHostSet
	// OriginControllerWrite: a controller wrote the attribute.
	OriginControllerWrite
	// OriginCommand: a command the server ran, and what the server does
	// on its own after one.
	OriginCommand
)

// String names the origin.
func (o Origin) String() string {
	switch o {
	case OriginHostSet:
		return "host set"
	case OriginControllerWrite:
		return "controller write"
	case OriginCommand:
		return "command"
	default:
		return "unknown"
	}
}

// Change is one change a cluster server reports: the attributes that
// changed and where the change came from.
type Change struct {
	// Attributes are the ids of the attributes that changed, ascending.
	Attributes []uint32
	// Origin is where the change came from.
	Origin Origin
}

// originKey is the context key of a request's origin.
type originKey struct{}

// WithOrigin returns ctx carrying the origin of the request it serves: the
// dispatcher stamps a controller's write with [OriginControllerWrite] and
// a command with [OriginCommand] before it hands them to a cluster
// server, so a server that reports a change from inside the request reads
// where it came from with [OriginFrom].
func WithOrigin(ctx context.Context, origin Origin) context.Context {
	return context.WithValue(ctx, originKey{}, origin)
}

// OriginFrom returns the origin ctx carries ([WithOrigin]); a context
// without one is the host's ([OriginHostSet]): a change outside a
// controller's request is the host's own.
func OriginFrom(ctx context.Context) Origin {
	if ctx == nil {
		return OriginHostSet
	}
	if o, ok := ctx.Value(originKey{}).(Origin); ok {
		return o
	}
	return OriginHostSet
}

// delivering maps the first element of an attribute-id slice being
// delivered to a listener to the origin of its change.
var delivering sync.Map // *uint32 → Origin

// DeliverChange calls listener with attrIDs, the change's origin readable
// by [OriginOf] from inside the call. attrIDs must be the listener's own
// slice — a fresh one per listener, as an [AttributeChangeNotifier]
// hands each of its listeners — so the origin travels with this delivery
// and no other. This is how the origin is carried without a second
// notifier interface: [AttributeChangeNotifier] stays as it is, and a
// listener that wants the origin asks for it with the slice it was
// handed.
func DeliverChange(origin Origin, attrIDs []uint32, listener func(attrIDs []uint32)) {
	if len(attrIDs) == 0 {
		listener(attrIDs)
		return
	}
	key := &attrIDs[0]
	delivering.Store(key, origin)
	defer delivering.Delete(key)
	listener(attrIDs)
}

// OriginOf returns the origin of the change attrIDs reports, called from
// inside the listener [DeliverChange] handed attrIDs to; [OriginUnknown]
// for a slice delivered otherwise, or after the listener returned.
func OriginOf(attrIDs []uint32) Origin {
	if len(attrIDs) == 0 {
		return OriginUnknown
	}
	v, _ := delivering.Load(&attrIDs[0])
	o, _ := v.(Origin) // only DeliverChange stores, always an Origin; absent is OriginUnknown
	return o
}
