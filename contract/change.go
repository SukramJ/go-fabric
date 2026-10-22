// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import "context"

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
	// OriginUnknown: the server did not say — a server that cannot tell
	// where a change came from, or one that reports through
	// [ChangeNotifier].
	OriginUnknown Origin = iota
	// OriginHostSet: the host set the state (the server's SetAttributes,
	// a typed server's Set) outside a command. A Set while the server runs
	// a command — the host's own handler included — is reported as
	// [OriginCommand], as matter.js commits a handler's state write with
	// the command's transaction (BD-Matter-CommandOrigin-PerServerFlag).
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
