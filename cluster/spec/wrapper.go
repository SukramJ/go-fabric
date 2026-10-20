// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "github.com/SukramJ/go-fabric/contract"

// Wrapper holds the server of a generated typed server (<cluster>def.Server
// embeds it): any [contract.ClusterServer] of the cluster, the generated
// [Server] or a self-held one. It exposes the server and nothing else — the
// typed server is not a contract.ClusterServer and cannot be mounted by
// mistake. Mount the underlying server, not the typed one.
type Wrapper struct{ srv contract.ClusterServer }

// NewWrapper returns a wrapper holding srv.
func NewWrapper(srv contract.ClusterServer) Wrapper { return Wrapper{srv: srv} }

// Underlying returns the server the wrapper holds.
func (w Wrapper) Underlying() contract.ClusterServer { return w.srv }
