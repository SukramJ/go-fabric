// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

// CloseBegun reports whether a Close of c has begun.
func CloseBegun(c *Controller) bool {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	return c.closed
}
