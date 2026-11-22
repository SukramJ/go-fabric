// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import "thingfabric.dev/go-fabric/transport/tcp"

// CloseBegun reports whether a Close of c has begun.
func CloseBegun(c *Controller) bool {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	return c.closed
}

// SetTCPListen replaces the TCP listener's constructor New binds with
// until restore runs. A test that calls it does not run in parallel:
// the constructor is the package's.
func SetTCPListen(fn func(tcp.Config) (*tcp.Listener, error)) (restore func()) {
	old := tcpListen
	tcpListen = fn
	return func() { tcpListen = old }
}
