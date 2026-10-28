// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// QueuedInvokes is the number of commands waiting in the batch lanes of c,
// the test seam of the batcher's queue.
func QueuedInvokes(c *Interaction) int {
	c.batchMu.Lock()
	defer c.batchMu.Unlock()
	n := 0
	for _, lane := range c.lanes {
		n += len(lane.pending)
	}
	return n
}
