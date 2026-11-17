// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// serial runs a command handler under op: a server's commands, and the
// timer expiries and fabric removals that act through its port, run one
// at a time from their checks through the port call to their commit — as
// chip's handlers do on its single-threaded stack — so no two of them
// pass a bound or a state check that only one of them may (review II,
// R2-P6-conc-1609). The host's methods never take op: a port may call
// them while a command waits on it.
func serial(op *sync.Mutex, fn spec.CommandHandler) spec.CommandHandler {
	return func(ctx context.Context, fields any) (any, error) {
		op.Lock()
		defer op.Unlock()
		return fn(ctx, fields)
	}
}
