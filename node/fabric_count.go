// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"sync"
)

// fabricCountCache is the node's count of the fabrics it holds, the
// counter the bridge, the commissioning window and AdministratorCommissioning
// share (wireCommissioningWindow). The bridge asks it for every chunk wait
// claim of a fabric's session once the claims reach the bridge's threshold
// (bridge chunk_waits.go), under the load that rule exists for, so a count
// is listed from the store once and kept until a fabric is installed or
// removed ([Node.fabricInstalled], [Node.fabricRemoved] call invalidate).
// Review II, R2-P7-req-05, owner decisions 275, 280.
//
// While a fail-safe is armed nothing is kept: AddNOC writes its row
// pending before the installed hook runs, and an AddNOC that fails after
// that rolls the row back without any hook (core
// OperationalCredentials.rollbackFailedAddNOC), so a count listed then
// could outlive the row. A pending row exists only under an armed
// fail-safe; the rollback at the fail-safe's end runs the removed hook.
type fabricCountCache struct {
	list func(context.Context) (int, error)
	// armed reports an armed fail-safe (GeneralCommissioning.FailSafeArmed);
	// nil: never armed.
	armed func() bool

	mu    sync.Mutex
	gen   uint64 // bumped by invalidate: a listing that began before is not kept
	n     int
	valid bool
}

// count returns the fabrics the node holds: the kept count, or a fresh
// listing, kept when no fail-safe was armed before or after it and no
// invalidate ran meanwhile.
func (c *fabricCountCache) count(ctx context.Context) (int, error) {
	c.mu.Lock()
	if c.valid {
		n := c.n
		c.mu.Unlock()
		return n, nil
	}
	gen := c.gen
	c.mu.Unlock()
	keep := !c.failSafeArmed()
	n, err := c.list(ctx)
	if err != nil || !keep || c.failSafeArmed() {
		return n, err
	}
	c.mu.Lock()
	if c.gen == gen {
		c.n, c.valid = n, true
	}
	c.mu.Unlock()
	return n, nil
}

func (c *fabricCountCache) failSafeArmed() bool {
	return c.armed != nil && c.armed()
}

// invalidate drops the kept count: the next count lists the store again.
// Nil-safe.
func (c *fabricCountCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.gen++
	c.valid = false
	c.mu.Unlock()
}
