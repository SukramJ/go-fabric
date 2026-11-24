// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"sync"

	"thingfabric.dev/go-fabric/cluster/spec"
)

// serial runs a command handler under op: a server's commands, and the
// timer expiries and fabric removals that act through its port, run one
// at a time from their checks through the port call to their commit — as
// chip's handlers do on its single-threaded stack — so no two of them
// pass a bound or a state check that only one of them may (review II,
// R2-P6-conc-1609). The host's methods never take op: a port may call
// them while a command waits on it.
func serial(op sync.Locker, fn spec.CommandHandler) spec.CommandHandler {
	return func(ctx context.Context, fields any) (any, error) {
		op.Lock()
		defer op.Unlock()
		return fn(ctx, fields)
	}
}

// opLock is a command serialisation that a caller may also take with a
// context ([opLock.LockContext]): a wait that the caller's end must end —
// a node's peer-gone keeper, which Stop waits for — gives up when the
// context ends instead of outliving it behind a port call that does not
// return (R5-conc-5301). Lock and Unlock are a mutex's; the zero value is
// unlocked.
type opLock struct {
	once sync.Once
	slot chan struct{}
}

func (l *opLock) ch() chan struct{} {
	l.once.Do(func() { l.slot = make(chan struct{}, 1) })
	return l.slot
}

// Lock takes the serialisation, waiting as long as it is held.
func (l *opLock) Lock() { l.ch() <- struct{}{} }

// Unlock releases it; releasing one not held panics, as a mutex's does.
func (l *opLock) Unlock() {
	select {
	case <-l.ch():
	default:
		panic("camera: unlock of an unlocked opLock")
	}
}

// LockContext takes the serialisation unless ctx ends first; an ended
// ctx takes nothing, also when the serialisation is free.
func (l *opLock) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case l.ch() <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
