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

// FlushingBatches is the number of batch flushes still running on c.
func FlushingBatches(c *Interaction) int { return int(c.flushing.Load()) }

// SetTimedWrite makes isTimedWrite name (cluster, attribute) timed and
// returns the restore; for a sequential test only (no attribute of the
// snapshot carries the timed quality at the pin).
func SetTimedWrite(cluster, attribute uint32) (restore func()) {
	prev := isTimedWrite
	isTimedWrite = func(c, a uint32) bool { return (c == cluster && a == attribute) || prev(c, a) }
	return func() { isTimedWrite = prev }
}
