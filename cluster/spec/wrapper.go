// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "github.com/SukramJ/go-fabric/contract"

// Wrapper holds the server of a generated typed server (<cluster>def.Server
// embeds it): any [contract.ClusterServer] of the cluster, the generated
// [Server] or a self-held one, and the queue its asynchronous callbacks
// run on. It exposes the server and nothing else — the typed server is not
// a contract.ClusterServer and cannot be mounted by mistake. Mount the
// underlying server, not the typed one.
type Wrapper struct {
	srv   contract.ClusterServer
	queue Queue
}

// Queue runs the callbacks handed to it one after the other, in the order
// they were handed, on a goroutine its owner starts and stops: a device's
// (device.Device), for the `…Async` variants of the typed server's
// callbacks. Enqueue never blocks and never runs fn on the caller's
// goroutine; a queue that is not running drops fn.
type Queue interface {
	Enqueue(fn func())
}

// NewWrapper returns a wrapper holding srv, without a queue: the
// asynchronous callbacks of its typed server answer [ErrNoQueue].
func NewWrapper(srv contract.ClusterServer) Wrapper { return Wrapper{srv: srv} }

// NewWrapperOn returns a wrapper holding srv whose asynchronous callbacks
// run on q.
func NewWrapperOn(srv contract.ClusterServer, q Queue) Wrapper {
	return Wrapper{srv: srv, queue: q}
}

// Underlying returns the server the wrapper holds.
func (w Wrapper) Underlying() contract.ClusterServer { return w.srv }
